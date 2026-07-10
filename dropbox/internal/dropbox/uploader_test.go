package dropbox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func uploadTestClient(rt http.RoundTripper) *Client {
	c := NewClient(Config{}, &http.Client{Transport: rt})
	c.token.accessTok = "test-token"
	c.token.expiry = time.Now().Add(time.Hour)
	return c
}

func enqueueTestUpload(t *testing.T, svc *Service, row UploadQueueRow) {
	t.Helper()
	if err := svc.inTx(context.Background(), func(tx *sql.Tx) error {
		return svc.Store.EnqueueUpload(tx, row)
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

func dueUpload(t *testing.T, svc *Service, now time.Time) UploadQueueRow {
	t.Helper()
	tx, err := svc.DB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	rows, err := svc.Store.DueUploads(tx, now.Format(time.RFC3339Nano))
	if err != nil || len(rows) != 1 {
		t.Fatalf("due rows = %+v, %v", rows, err)
	}
	return rows[0]
}

func TestUploaderSuccessPersistsRevClearsQueueAndSuppressesEcho(t *testing.T) {
	// R-KKM6-83XW
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	conn := openStoreDB(t)
	mirror, err := NewMirror(t.TempDir() + "/mirror")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("the current local bytes")
	hash, size, err := mirror.WriteFrom("/notes/report.md", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var uploads int
	svc := &Service{DB: conn, Store: NewStore(), Mirror: mirror, Now: func() time.Time { return now }}
	svc.Client = uploadTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/2/files/upload" {
			t.Fatalf("request path = %q, want upload", r.URL.Path)
		}
		uploads++
		body, _ := io.ReadAll(r.Body)
		if string(body) != string(data) {
			t.Fatalf("uploaded bytes = %q, want %q", body, data)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"rev":"push-rev"}`))}, nil
	}))
	if err := svc.inTx(context.Background(), func(tx *sql.Tx) error {
		if err := svc.Store.UpsertFile(tx, "/notes/report.md", "local-rev", hash, size, now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return svc.Store.EnqueueUpload(tx, UploadQueueRow{Path: "/notes/report.md", Op: "put", EnqueuedAt: now.Format(time.RFC3339Nano), NextAttemptAt: now.Format(time.RFC3339Nano)})
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.drainUploads(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if uploads != 1 {
		t.Fatalf("uploads = %d, want 1", uploads)
	}
	row, err := svc.Content("/notes/report.md", nil)
	if err != nil || row.Rev != "push-rev" {
		t.Fatalf("file after upload = %+v, %v", row, err)
	}
	tx, _ := conn.Begin()
	queued, err := svc.Store.DueUploads(tx, now.Add(time.Hour).Format(time.RFC3339Nano))
	tx.Rollback()
	if err != nil || len(queued) != 0 {
		t.Fatalf("queue after successful upload = %+v, %v", queued, err)
	}

	// The Dropbox delta carrying the returned revision is deduplicated before
	// download or event emission: a pushed write does not round-trip as a pull.
	fc := newFakeClient()
	sink := &capturingSink{}
	svc.Outbox = sink
	eng := NewEngine(svc, EngineOptions{Client: fc})
	if err := applyEntries(t, eng, DeltaEntry{Tag: TagFile, PathDisplay: "/notes/report.md", PathLower: "/notes/report.md", Rev: "push-rev", ContentHash: hash, Size: uint64(size)}); err != nil {
		t.Fatalf("apply echo delta: %v", err)
	}
	if len(sink.events) != 0 || fc.downloadCalls[foldPath("/notes/report.md")] != 0 {
		t.Fatalf("echo caused events/downloads: events=%+v downloads=%d", sink.events, fc.downloadCalls[foldPath("/notes/report.md")])
	}
}

func TestUploaderFailureBacksOffThenRetainsPoisonedRow(t *testing.T) {
	// R-KLU2-LVOL
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	conn := openStoreDB(t)
	mirror, err := NewMirror(t.TempDir() + "/mirror")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mirror.WriteFrom("/broken.txt", bytes.NewBufferString("broken")); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: conn, Store: NewStore(), Mirror: mirror, Now: func() time.Time { return now }, Client: uploadTestClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Dropbox is unavailable")
	}))}
	enqueueTestUpload(t, svc, UploadQueueRow{Path: "/broken.txt", Op: "put", EnqueuedAt: now.Format(time.RFC3339Nano), NextAttemptAt: now.Format(time.RFC3339Nano)})

	for attempt := 1; attempt <= uploaderPoisonThreshold; attempt++ {
		row := dueUpload(t, svc, now)
		if err := svc.uploadRow(context.Background(), row); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		tx, _ := conn.Begin()
		var got UploadQueueRow
		err := tx.QueryRow(`SELECT path, op, dest, origin, enqueued_at, attempts, next_attempt_at, state, last_error FROM upload_queue WHERE path = ?`, "/broken.txt").Scan(&got.Path, &got.Op, &got.Dest, &got.Origin, &got.EnqueuedAt, &got.Attempts, &got.NextAttemptAt, &got.State, &got.LastError)
		tx.Rollback()
		if err != nil || got.Attempts != attempt || !got.LastError.Valid {
			t.Fatalf("attempt %d row = %+v, %v", attempt, got, err)
		}
		if attempt < uploaderPoisonThreshold {
			if got.State != "pending" || got.NextAttemptAt <= now.Format(time.RFC3339Nano) {
				t.Fatalf("attempt %d did not apply future pending backoff: %+v", attempt, got)
			}
			now = now.Add(time.Hour)
		} else if got.State != "failed" {
			t.Fatalf("poison row state = %q, want failed", got.State)
		}
	}
}

func TestHealthIncludesUploadBacklogAndAge(t *testing.T) {
	// R-KN1Y-ZNFA
	now := time.Date(2026, 7, 10, 12, 0, 30, 0, time.UTC)
	conn := openStoreDB(t)
	svc := &Service{DB: conn, Store: NewStore(), Now: func() time.Time { return now }}
	enqueueTestUpload(t, svc, UploadQueueRow{Path: "/pending.txt", Op: "delete", EnqueuedAt: now.Add(-30 * time.Second).Format(time.RFC3339Nano), NextAttemptAt: now.Format(time.RFC3339Nano)})
	enqueueTestUpload(t, svc, UploadQueueRow{Path: "/failed.txt", Op: "delete", EnqueuedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano), NextAttemptAt: now.Format(time.RFC3339Nano)})
	if err := svc.inTx(context.Background(), func(tx *sql.Tx) error {
		return svc.Store.FailUpload(tx, "/failed.txt", "permanent", now.Format(time.RFC3339Nano), true)
	}); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	info, err := svc.Health("owner@example.test", "client")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if info.PendingUploads != 1 || info.FailedUploads != 1 || info.OldestPendingAgeSeconds != 30 {
		t.Fatalf("upload health = %+v", info)
	}
}
