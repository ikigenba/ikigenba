package dropbox

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingEventSink struct{ err error }

func (s failingEventSink) AppendFileEvent(*sql.Tx, FileEvent) error { return s.err }
func (failingEventSink) Ring()                                      {}

func writeRequest(t *testing.T, h http.Handler, method, path string, body []byte, clientID string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{"path": {path}}
	req := httptest.NewRequest(method, "/content?"+q.Encode(), bytes.NewReader(body))
	req.Header.Set("X-Client-Id", clientID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWriteHandler_PutThenContentReturnsExactIndexedBytes(t *testing.T) {
	// R-K4RH-93AV
	svc, _, _ := newContentService(t)
	data := []byte("# exactly these bytes\n")
	put := writeRequest(t, svc.WriteHandler(), http.MethodPut, "/a/x.md", data, "notes")
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %q", put.Code, put.Body.String())
	}
	var response struct {
		Path        string `json:"path"`
		Size        int64  `json:"size"`
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if response.Path != "/a/x.md" || response.Size != int64(len(data)) || response.ContentHash != ContentHash(data) {
		t.Fatalf("PUT response = %+v, want path, exact size, and hash", response)
	}
	get := httptest.NewRecorder()
	svc.ContentHandler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/content?path=%2Fa%2Fx.md", nil))
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), data) {
		t.Fatalf("GET after PUT = %d %q, want exact bytes", get.Code, get.Body.Bytes())
	}
}

func TestWrite_ClampsParentSegmentsInsideMirrorRoot(t *testing.T) {
	// R-K5ZD-MV1K
	svc, _, mirror := newContentService(t)
	row, err := svc.Write(context.Background(), "../outside.md", bytes.NewBufferString("inside"), "writer")
	if err != nil || row.Path != "/outside.md" {
		t.Fatalf("Write parent path = %+v, %v; want clamped canonical path", row, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(mirror.Root()), "outside.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escaped file stat error = %v, want not exist", err)
	}
	got, err := os.ReadFile(filepath.Join(mirror.Root(), "outside.md"))
	if err != nil || string(got) != "inside" {
		t.Fatalf("clamped mirror bytes = %q, %v", got, err)
	}
}

func TestDeleteHandler_IsIdempotentAndDoesNotEmitForAbsentPath(t *testing.T) {
	// R-K77A-0MS9
	svc, _, _ := newContentService(t)
	sink := &capturingSink{}
	svc.Outbox = sink
	if _, err := svc.Write(context.Background(), "/a/remove.md", bytes.NewBufferString("remove"), "writer"); err != nil {
		t.Fatal(err)
	}
	before := len(sink.events)
	for range 2 {
		rec := writeRequest(t, svc.WriteHandler(), http.MethodDelete, "/a/remove.md", nil, "writer")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE status = %d, body %q", rec.Code, rec.Body.String())
		}
	}
	if got := len(sink.events); got != before+1 || sink.events[before].Kind != KindDelete {
		t.Fatalf("events after idempotent deletes = %+v, want one delete", sink.events)
	}
}

func TestMoveHandler_RelocatesFileAndEmitsPathLifecycle(t *testing.T) {
	// R-K8F6-EEIY
	svc, _, _ := newContentService(t)
	sink := &capturingSink{}
	svc.Outbox = sink
	if _, err := svc.Write(context.Background(), "/from.md", bytes.NewBufferString("move me"), "writer"); err != nil {
		t.Fatal(err)
	}
	before := len(sink.events)
	q := url.Values{"from": {"/from.md"}, "to": {"/to.md"}}
	rec := httptest.NewRecorder()
	svc.MoveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/move?"+q.Encode(), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("move status = %d, body %q", rec.Code, rec.Body.String())
	}
	if _, err := svc.Content("/from.md", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("from content error = %v, want not found", err)
	}
	if _, err := svc.Content("/to.md", nil); err != nil {
		t.Fatalf("to content: %v", err)
	}
	if got := sink.events[before:]; len(got) != 2 || got[0].Kind != KindDelete || got[0].Path != "/from.md" || got[1].Kind != KindCreate || got[1].Path != "/to.md" {
		t.Fatalf("move events = %+v, want deleted(from), created(to)", got)
	}
}

func TestMoveHandler_MissingSourceReturnsNotFoundWithoutMutation(t *testing.T) {
	// R-BZLK-E9VW
	svc, conn, _ := newContentService(t)
	sink := &capturingSink{}
	svc.Outbox = sink
	if _, err := svc.Write(context.Background(), "/kept.md", bytes.NewBufferString("keep"), "writer"); err != nil {
		t.Fatal(err)
	}
	beforeEvents := len(sink.events)
	var beforeUploads int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM upload_queue`).Scan(&beforeUploads); err != nil {
		t.Fatalf("count uploads before move: %v", err)
	}

	q := url.Values{"from": {"/missing.md"}, "to": {"/moved.md"}}
	move := httptest.NewRecorder()
	svc.MoveHandler().ServeHTTP(move, httptest.NewRequest(http.MethodPost, "/move?"+q.Encode(), nil))
	if move.Code != http.StatusNotFound || !strings.Contains(move.Body.String(), "dropbox: not found") || !strings.Contains(move.Body.String(), "/missing.md") {
		t.Fatalf("missing move = %d %q, want 404 with missing-path detail", move.Code, move.Body.String())
	}
	if _, err := svc.Content("/kept.md", nil); err != nil {
		t.Fatalf("kept entry after missing move: %v", err)
	}
	if _, err := svc.Content("/moved.md", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("move destination after missing move = %v, want not found", err)
	}
	if got := len(sink.events); got != beforeEvents {
		t.Fatalf("events after missing move = %d, want %d", got, beforeEvents)
	}
	var afterMoveUploads int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM upload_queue`).Scan(&afterMoveUploads); err != nil {
		t.Fatalf("count uploads after move: %v", err)
	}
	if afterMoveUploads != beforeUploads {
		t.Fatalf("uploads after missing move = %d, want %d", afterMoveUploads, beforeUploads)
	}

	deleted := writeRequest(t, svc.WriteHandler(), http.MethodDelete, "/also-missing.md", nil, "writer")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete absent path = %d %q, want 204", deleted.Code, deleted.Body.String())
	}
	if got := len(sink.events); got != beforeEvents {
		t.Fatalf("events after absent delete = %d, want %d", got, beforeEvents)
	}
	var afterDeleteUploads int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM upload_queue`).Scan(&afterDeleteUploads); err != nil {
		t.Fatalf("count uploads after delete: %v", err)
	}
	if afterDeleteUploads != beforeUploads {
		t.Fatalf("uploads after absent delete = %d, want %d", afterDeleteUploads, beforeUploads)
	}
}

func TestWriteHandler_ValidationErrorsCarryDomainDetail(t *testing.T) {
	// R-C0TG-S1ML
	svc, _, _ := newContentService(t)
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "empty path", path: "", want: "path is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := writeRequest(t, svc.WriteHandler(), http.MethodPut, tc.path, []byte("nope"), "writer")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("PUT %q status = %d, want 400; body %q", tc.path, rec.Code, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.want) || body == "validation error\n" {
				t.Fatalf("PUT %q body = %q, want domain detail %q", tc.path, body, tc.want)
			}
		})
	}
}

func TestStatHandler_ReturnsFileDirectoryAndNotFound(t *testing.T) {
	// R-K9N2-S69N
	svc, _, _ := newContentService(t)
	if err := svc.Mkdir(context.Background(), "/folder", "writer"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Write(context.Background(), "/folder/file.md", bytes.NewBufferString("file"), "writer"); err != nil {
		t.Fatal(err)
	}
	for path, wantKind := range map[string]EntryKind{"/folder": KindDir, "/folder/file.md": KindFile} {
		q := url.Values{"path": {path}}
		rec := httptest.NewRecorder()
		svc.StatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stat?"+q.Encode(), nil))
		var entry Entry
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &entry) != nil || entry.Kind != wantKind {
			t.Fatalf("stat %q = %d %+v, want %s", path, rec.Code, entry, wantKind)
		}
	}
	rec := httptest.NewRecorder()
	svc.StatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stat?path=%2Fmissing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing stat status = %d, want 404", rec.Code)
	}
}

func TestWrite_UsesIndexPresenceForLifecycleAndThreadsOrigin(t *testing.T) {
	// R-KAUZ-5Y0C
	// R-KO9V-DF5Z
	svc, _, _ := newContentService(t)
	sink := &capturingSink{}
	svc.Outbox = sink
	if _, err := svc.Write(context.Background(), "/same.md", bytes.NewBufferString("one"), "caller-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Write(context.Background(), "/same.md", bytes.NewBufferString("two"), "caller-b"); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %+v, want created then modified", sink.events)
	}
	if sink.events[0].Kind != KindCreate || sink.events[0].Origin != "caller-a" || sink.events[1].Kind != KindModify || sink.events[1].Origin != "caller-b" {
		t.Fatalf("write events = %+v, want lifecycle discriminator and caller origins", sink.events)
	}
}

func TestWriteHandler_LogsUnexpectedMutationFailureBeforeInternalError(t *testing.T) {
	// R-58GQ-0R7J
	svc, _, _ := newContentService(t)
	underlying := errors.New("forced event append failure")
	svc.Outbox = failingEventSink{err: underlying}
	var logs bytes.Buffer
	svc.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	rec := writeRequest(t, svc.WriteHandler(), http.MethodPut, "/logged.txt", []byte("bytes"), "writer")
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != "internal error\n" {
		t.Fatalf("failed PUT = %d %q, want bare 500", rec.Code, rec.Body.String())
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatalf("decode error log %q: %v", logs.String(), err)
	}
	if record["level"] != "ERROR" || record["route"] != "PUT /content" || !strings.Contains(fmt.Sprint(record["err"]), underlying.Error()) {
		t.Fatalf("error log = %v, want ERROR route and underlying error", record)
	}
}
