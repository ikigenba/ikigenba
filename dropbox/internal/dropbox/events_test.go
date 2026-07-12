package dropbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eventplane/outbox"
	"registry"
)

func TestEventsSamplesUseRegistryDropBoxContentOrigin(t *testing.T) {
	// R-QKGB-OPNE
	wantPrefix := registry.BaseURL("dropbox") + "/content?path="
	wantURL := "http://127.0.0.1:3200/content?path=%2Fnotes%2Fmeeting.md"
	wantKinds := map[string]bool{
		KindCreate: true,
		KindModify: true,
		KindDelete: true,
	}

	for _, entry := range Events {
		if !wantKinds[entry.Kind] {
			t.Fatalf("unexpected event kind %q", entry.Kind)
		}
		delete(wantKinds, entry.Kind)
		if entry.Subject != "/<mirror path>" {
			t.Fatalf("%s subject = %q, want mirror-path description", entry.Kind, entry.Subject)
		}

		sample, ok := entry.Sample.(filePayload)
		if !ok {
			t.Fatalf("%s sample type = %T, want filePayload", entry.Kind, entry.Sample)
		}
		if !strings.HasPrefix(sample.ContentURL, wantPrefix) {
			t.Fatalf("%s content_url = %q, want prefix %q", entry.Kind, sample.ContentURL, wantPrefix)
		}
		if sample.ContentURL != wantURL {
			t.Fatalf("%s content_url = %q, want %q", entry.Kind, sample.ContentURL, wantURL)
		}
		if sample.Origin != OriginDropbox {
			t.Fatalf("%s origin = %q, want %q", entry.Kind, sample.Origin, OriginDropbox)
		}
	}
	for kind := range wantKinds {
		t.Fatalf("missing event kind %q", kind)
	}
}

func TestProducerRoutesFileEventsAndDropsPayloadDiscriminator(t *testing.T) {
	// R-QB5T-GLB6
	conn := openStoreDB(t)
	mirror, err := NewMirror(t.TempDir() + "/mirror")
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	ob, err := outbox.New(conn, outbox.Options{Source: "dropbox", Registry: Events})
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	svc := NewService(conn)
	svc.Mirror = mirror
	svc.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	svc.Outbox = NewOutboxProducer(ob, "http://127.0.0.1:3200")
	fc := newFakeClient()
	eng := NewEngine(svc, EngineOptions{Client: fc, MaxEntryRetries: 3, Backoff: time.Millisecond})

	created := fc.addFile("/notes/meeting.md", "r1", ContentHash([]byte("first")), []byte("first"))
	if err := applyEntries(t, eng, created); err != nil {
		t.Fatalf("download apply: %v", err)
	}
	if _, err := svc.Write(context.Background(), "/notes/meeting.md", bytes.NewBufferString("second"), "writer"); err != nil {
		t.Fatalf("service overwrite: %v", err)
	}
	a := fc.addFile("/folder/a.md", "ra", ContentHash([]byte("a")), []byte("a"))
	b := fc.addFile("/folder/b.md", "rb", ContentHash([]byte("b")), []byte("b"))
	if err := applyEntries(t, eng, a, b); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	if err := applyEntries(t, eng, deletedEntry("/folder")); err != nil {
		t.Fatalf("folder delete: %v", err)
	}

	rows, err := conn.Query(`SELECT kind, subject, payload FROM outbox ORDER BY seq`)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var got []struct {
		kind, subject string
		payload       string
	}
	for rows.Next() {
		var row struct{ kind, subject, payload string }
		if err := rows.Scan(&row.kind, &row.subject, &row.payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox: %v", err)
	}
	if len(got) != 6 || got[0].kind != KindCreate || got[0].subject != "/notes/meeting.md" || got[1].kind != KindModify || got[1].subject != "/notes/meeting.md" || got[4].kind != KindDelete || got[5].kind != KindDelete || got[4].subject == got[5].subject {
		t.Fatalf("outbox routes = %+v, want create/modify and distinct delete subjects", got)
	}
	for _, row := range got {
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.payload), &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if len(payload) != 7 || payload["event"] != nil {
			t.Fatalf("payload = %v, want exactly file fields without event", payload)
		}
		for _, key := range []string{"path", "rev", "content_hash", "size", "content_url", "origin", "occurred_at"} {
			if payload[key] == nil {
				t.Fatalf("payload missing %q: %v", key, payload)
			}
		}
	}
}

func TestFeedUsesCanonicalKindAndSubjectEnvelope(t *testing.T) {
	// R-QETI-LWJ9
	conn := openStoreDB(t)
	ob, err := outbox.New(conn, outbox.Options{Source: "dropbox", Registry: Events})
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	producer := NewOutboxProducer(ob, "http://127.0.0.1:3200")
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ev := FileEvent{Kind: KindCreate, Path: "/notes/meeting.md", Rev: "r1", ContentHash: "hash", Size: 7, OccurredAt: "2026-06-03T12:00:00.000000000Z", Origin: OriginDropbox}
	if err := producer.AppendFileEvent(tx, ev); err != nil {
		t.Fatalf("append producer event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	producer.Ring()

	srv := httptest.NewServer(ob.FeedHandler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new feed request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("feed request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("feed status = %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	var eventLine, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: dropbox:") {
			eventLine = line
		}
		if eventLine != "" && strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	cancel()
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("read feed: %v", err)
	}
	if eventLine != "event: dropbox:create/notes/meeting.md" {
		t.Fatalf("event line = %q", eventLine)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(dataLine), &envelope); err != nil {
		t.Fatalf("decode envelope %q: %v", dataLine, err)
	}
	if envelope["kind"] != KindCreate || envelope["subject"] != "/notes/meeting.md" || envelope["type"] != nil {
		t.Fatalf("envelope = %v, want kind/subject and no type", envelope)
	}
}
