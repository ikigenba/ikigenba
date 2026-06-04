package dropbox

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// newContentService wires a Service over a real temp DB + mirror, seeds one
// indexed file with bytes on disk, and returns the service plus the canonical
// display path it indexed.
func newContentService(t *testing.T) (*Service, *sql.DB, *Mirror) {
	t.Helper()
	conn := openStoreDB(t)
	mirror, err := NewMirror(t.TempDir())
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	svc := NewService(conn)
	svc.Mirror = mirror
	svc.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	return svc, conn, mirror
}

// seedFile writes bytes to the mirror and upserts a matching index row.
func seedFile(t *testing.T, svc *Service, conn *sql.DB, mirror *Mirror, display, rev string, data []byte) {
	t.Helper()
	if err := mirror.Write(display, data); err != nil {
		t.Fatalf("mirror write: %v", err)
	}
	withTx(t, conn, func(tx *sql.Tx) {
		if err := svc.Store.UpsertFile(tx, display, rev, "hash-"+rev, int64(len(data)), svc.now()); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	})
}

func TestContentHandler_ServesBytesURLEncodedPath(t *testing.T) {
	svc, conn, mirror := newContentService(t)
	want := []byte("hello dropbox content")
	// A path containing a space — exercises percent-encoding on the query.
	display := "/inbox/my report.pdf"
	seedFile(t, svc, conn, mirror, display, "rev1", want)

	h := svc.ContentHandler()
	q := url.Values{}
	q.Set("path", display) // url.Values.Encode percent-encodes the space
	req := httptest.NewRequest(http.MethodGet, "/content?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestContentHandler_CaseInsensitiveResolve(t *testing.T) {
	svc, conn, mirror := newContentService(t)
	want := []byte("case folded bytes")
	seedFile(t, svc, conn, mirror, "/Inbox/Report.PDF", "rev1", want)

	h := svc.ContentHandler()
	q := url.Values{}
	q.Set("path", "/inbox/report.pdf") // case-mismatched query must still resolve
	req := httptest.NewRequest(http.MethodGet, "/content?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestContentHandler_StaleRevIs409(t *testing.T) {
	svc, conn, mirror := newContentService(t)
	seedFile(t, svc, conn, mirror, "/inbox/report.pdf", "rev1", []byte("bytes"))

	h := svc.ContentHandler()
	q := url.Values{}
	q.Set("path", "/inbox/report.pdf")
	q.Set("rev", "rev-stale")
	req := httptest.NewRequest(http.MethodGet, "/content?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestContentHandler_MatchingRevServes(t *testing.T) {
	svc, conn, mirror := newContentService(t)
	want := []byte("exact bytes")
	seedFile(t, svc, conn, mirror, "/inbox/report.pdf", "rev1", want)

	h := svc.ContentHandler()
	q := url.Values{}
	q.Set("path", "/inbox/report.pdf")
	q.Set("rev", "rev1")
	req := httptest.NewRequest(http.MethodGet, "/content?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestContentHandler_IdentityHeaderGuardIs404(t *testing.T) {
	svc, conn, mirror := newContentService(t)
	seedFile(t, svc, conn, mirror, "/inbox/report.pdf", "rev1", []byte("bytes"))
	h := svc.ContentHandler()

	// Even for a VALID path, an nginx-injected identity header means the request
	// was proxied through the public front door → 404 (mirrors feed.go:50).
	for _, hdr := range []string{"X-Forwarded-Proto", "X-Owner-Email"} {
		q := url.Values{}
		q.Set("path", "/inbox/report.pdf")
		req := httptest.NewRequest(http.MethodGet, "/content?"+q.Encode(), nil)
		req.Header.Set(hdr, "set")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("with %s present: status = %d, want 404", hdr, rec.Code)
		}
	}
}

func TestContentHandler_UnknownPathIs404(t *testing.T) {
	svc, _, _ := newContentService(t)
	h := svc.ContentHandler()

	q := url.Values{}
	q.Set("path", "/inbox/does-not-exist.pdf")
	req := httptest.NewRequest(http.MethodGet, "/content?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestContentHandler_MissingPathParamIs404(t *testing.T) {
	svc, _, _ := newContentService(t)
	h := svc.ContentHandler()

	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
