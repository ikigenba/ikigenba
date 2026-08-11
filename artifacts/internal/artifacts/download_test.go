package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// R-3WQ7-5WKY
func TestPublicDownloadStreamsExactBodyAndMetadata(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	pdfID := fx.uploadArtifact(t, "quarterly report.pdf", "public", []byte("%PDF-exact\x00body"))
	unknownID := fx.uploadArtifact(t, "sample.unknownext", "public", []byte{0, 1, 2, 0xff})
	handler := fx.downloadHandler()

	for _, tc := range []struct {
		id, filename, contentType string
		body                      []byte
	}{
		{pdfID, "quarterly report.pdf", "application/pdf", []byte("%PDF-exact\x00body")},
		{unknownID, "sample.unknownext", "application/octet-stream", []byte{0, 1, 2, 0xff}},
	} {
		response := downloadRequest(t, handler, "/f/", tc.id, tc.filename, nil)
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), tc.body) {
			t.Fatalf("GET %s status/body = %d/%v, want 200/%v", tc.filename, response.Code, response.Body.Bytes(), tc.body)
		}
		if got := response.Header().Get("Content-Length"); got != stringInt64(int64(len(tc.body))) {
			t.Errorf("Content-Length = %q, want %d", got, len(tc.body))
		}
		if got := response.Header().Get("Content-Type"); got != tc.contentType {
			t.Errorf("Content-Type = %q, want %q", got, tc.contentType)
		}
		if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="`+tc.filename+`"` {
			t.Errorf("Content-Disposition = %q", got)
		}
	}
}

// R-3XY3-JOBN
func TestPrivateDownloadIsIdentityGatedAndStreamsExactBody(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	body := []byte("private evidence")
	id := fx.uploadArtifact(t, "evidence.txt", "private", body)
	handler := fx.downloadHandler()

	authorized := downloadRequest(t, handler, "/p/", id, "evidence.txt", map[string]string{"X-Owner-Id": "owner-1"})
	if authorized.Code != http.StatusOK || !bytes.Equal(authorized.Body.Bytes(), body) {
		t.Fatalf("authorized GET = %d/%q, want 200/%q", authorized.Code, authorized.Body.Bytes(), body)
	}
	unauthorized := downloadRequest(t, handler, "/p/", id, "evidence.txt", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous private GET = %d, want 401", unauthorized.Code)
	}
	if fx.router.identityWraps != 1 {
		t.Fatalf("private route identity-gate wraps = %d, want 1", fx.router.identityWraps)
	}
}

// R-3Z5Z-XG2C
func TestDownloadFailuresHideTierIdAndFilenameMismatch(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	publicID := fx.uploadArtifact(t, "public.txt", "public", []byte("public"))
	privateID := fx.uploadArtifact(t, "private.txt", "private", []byte("private"))
	handler := fx.downloadHandler()
	identity := map[string]string{"X-Owner-Id": "owner-1"}

	publicFailures := []*httptest.ResponseRecorder{
		downloadRequest(t, handler, "/f/", privateID, "private.txt", nil),
		downloadRequest(t, handler, "/f/", "unknown-id", "public.txt", nil),
		downloadRequest(t, handler, "/f/", publicID, "wrong.txt", nil),
	}
	privateFailures := []*httptest.ResponseRecorder{
		downloadRequest(t, handler, "/p/", publicID, "public.txt", identity),
		downloadRequest(t, handler, "/p/", "unknown-id", "private.txt", identity),
		downloadRequest(t, handler, "/p/", privateID, "wrong.txt", identity),
	}
	assertIdenticalNotFound(t, publicFailures)
	assertIdenticalNotFound(t, privateFailures)
}

// R-40DW-B7T1
func TestPublicDownloadRejectsIdentityButAllowsForwardedProto(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	id := fx.uploadArtifact(t, "public.txt", "public", []byte("public"))
	handler := fx.downloadHandler()

	rejected := downloadRequest(t, handler, "/f/", id, "public.txt", map[string]string{"X-Owner-Id": "owner-1"})
	if rejected.Code != http.StatusNotFound {
		t.Fatalf("identity-bearing public GET = %d, want 404", rejected.Code)
	}
	accepted := downloadRequest(t, handler, "/f/", id, "public.txt", map[string]string{"X-Forwarded-Proto": "https"})
	if accepted.Code != http.StatusOK || accepted.Body.String() != "public" {
		t.Fatalf("forwarded-proto public GET = %d/%q", accepted.Code, accepted.Body.String())
	}
}

// R-41LS-OZJQ
func TestVisibilityFlipMovesStableIdBetweenDownloadTiers(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	body := []byte("stable bytes")
	id := fx.uploadArtifact(t, "stable.bin", "public", body)
	handler := fx.downloadHandler()
	identity := map[string]string{"X-Owner-Id": "owner-1"}

	if got := downloadRequest(t, handler, "/f/", id, "stable.bin", nil); got.Code != http.StatusOK {
		t.Fatalf("initial public GET = %d", got.Code)
	}
	fx.setVisibility(t, id, "private")
	if got := downloadRequest(t, handler, "/f/", id, "stable.bin", nil); got.Code != http.StatusNotFound {
		t.Fatalf("old public URL after flip = %d, want 404", got.Code)
	}
	private := downloadRequest(t, handler, "/p/", id, "stable.bin", identity)
	if private.Code != http.StatusOK || !bytes.Equal(private.Body.Bytes(), body) {
		t.Fatalf("private URL after flip = %d/%q", private.Code, private.Body.Bytes())
	}
	fx.setVisibility(t, id, "public")
	restored := downloadRequest(t, handler, "/f/", id, "stable.bin", nil)
	if restored.Code != http.StatusOK || !bytes.Equal(restored.Body.Bytes(), body) {
		t.Fatalf("restored public URL for id %q = %d/%q", id, restored.Code, restored.Body.Bytes())
	}
}

// R-42TP-2RAF
func TestCompletedDownloadsIncrementCountExactlyOnce(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	id := fx.uploadArtifact(t, "counted.bin", "public", []byte("count me"))
	handler := fx.downloadHandler()

	for range 2 {
		if got := downloadRequest(t, handler, "/f/", id, "counted.bin", nil); got.Code != http.StatusOK {
			t.Fatalf("completed GET = %d", got.Code)
		}
	}
	if got := downloadRequest(t, handler, "/f/", id, "wrong.bin", nil); got.Code != http.StatusNotFound {
		t.Fatalf("wrong-filename GET = %d, want 404", got.Code)
	}
	artifact, err := fx.store.GetArtifact(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.DownloadCount != 2 {
		t.Fatalf("download_count = %d, want 2", artifact.DownloadCount)
	}
}

func TestDownloadRejectsMalformedPathsAndMethodsAndEncodesUnicodeFilename(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	id := fx.uploadArtifact(t, "résumé.pdf", "public", []byte("cv"))
	handler := fx.downloadHandler()

	unicode := downloadRequest(t, handler, "/f/", id, "résumé.pdf", nil)
	if got := unicode.Header().Get("Content-Disposition"); got != `attachment; filename="download"; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf` {
		t.Fatalf("unicode Content-Disposition = %q", got)
	}
	for _, path := range []string{"/f/", "/f/" + id, "/f/" + id + "/résumé.pdf/extra"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("malformed path %q = %d, want 404", path, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/f/"+id+"/r%C3%A9sum%C3%A9.pdf", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST download = %d, want 405", response.Code)
	}
}

type downloadRouter struct {
	mux           *http.ServeMux
	identityWraps int
}

func (rt *downloadRouter) Handle(pattern string, handler http.Handler) {
	rt.mux.Handle(pattern, handler)
}

func (rt *downloadRouter) RequireIdentity(next http.Handler) http.Handler {
	rt.identityWraps++
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Owner-Id") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (fx *uploadFixture) downloadHandler() http.Handler {
	router := &downloadRouter{mux: http.NewServeMux()}
	fx.service.MountDownloads(router)
	fx.router = router
	return router.mux
}

func (fx *uploadFixture) uploadArtifact(t *testing.T, filename, visibility string, body []byte) string {
	t.Helper()
	pending := fx.mint(t, filename, visibility)
	response := fx.put(t, pending.Token, body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("seed upload = %d: %s", response.Code, response.Body.String())
	}
	var idResponse struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &idResponse); err != nil {
		t.Fatal(err)
	}
	return idResponse.ID
}

func (fx *uploadFixture) setVisibility(t *testing.T, id, visibility string) {
	t.Helper()
	if _, err := fx.conn.Exec(`UPDATE artifacts SET visibility = ? WHERE id = ?`, visibility, id); err != nil {
		t.Fatal(err)
	}
}

func downloadRequest(t *testing.T, handler http.Handler, prefix, id, filename string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	path := prefix + url.PathEscape(id) + "/" + url.PathEscape(filename)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertIdenticalNotFound(t *testing.T, responses []*httptest.ResponseRecorder) {
	t.Helper()
	for index, response := range responses {
		if response.Code != http.StatusNotFound {
			t.Errorf("failure %d = %d, want 404", index, response.Code)
		}
		if response.Body.String() != responses[0].Body.String() {
			t.Errorf("failure %d body = %q, want %q", index, response.Body.String(), responses[0].Body.String())
		}
	}
}
