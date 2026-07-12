package dropbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

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

func TestWrite_RejectsMirrorEscapeWithoutCreatingFiles(t *testing.T) {
	// R-K5ZD-MV1K
	svc, _, mirror := newContentService(t)
	_, err := svc.Write(context.Background(), "../outside.md", bytes.NewBufferString("nope"), "writer")
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrPathEscape) {
		t.Fatalf("Write escape error = %v, want validation path escape", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(mirror.Root()), "outside.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escaped file stat error = %v, want not exist", err)
	}
	entries, err := os.ReadDir(mirror.Root())
	if err != nil || len(entries) != 0 {
		t.Fatalf("mirror entries = %v, %v; want none", entries, err)
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
