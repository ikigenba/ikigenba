package artifacts

import (
	"appkit/server"
	"artifacts/internal/testutil"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"registry"
	"testing"
)

// R-441L-GJ14
func TestContentHolderStreamsPublicAndPrivateBytesAndHidesUnknownID(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	publicBody := []byte("public plane bytes")
	privateBody := []byte{0, 1, 2, 0xff, 'p'}
	publicID := fx.uploadArtifact(t, "public.txt", "public", publicBody)
	privateID := fx.uploadArtifact(t, "private.bin", "private", privateBody)
	handler := fx.contentHandler()

	for _, tc := range []struct {
		id, contentType string
		body            []byte
	}{
		{publicID, "text/plain; charset=utf-8", publicBody},
		{privateID, "application/octet-stream", privateBody},
	} {
		response := contentRequest(handler, tc.id, nil)
		if response.StatusCode != http.StatusOK || !bytes.Equal(response.body, tc.body) {
			t.Fatalf("content %q = %d/%v, want 200/%v", tc.id, response.StatusCode, response.body, tc.body)
		}
		if response.header.Get("Content-Length") != stringInt64(int64(len(tc.body))) {
			t.Errorf("content %q length = %q", tc.id, response.header.Get("Content-Length"))
		}
		if response.header.Get("Content-Type") != tc.contentType {
			t.Errorf("content %q type = %q, want %q", tc.id, response.header.Get("Content-Type"), tc.contentType)
		}
	}
	if unknown := contentRequest(handler, "unknown", nil); unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown content status = %d, want 404", unknown.StatusCode)
	}
}

// R-46HE-82II
func TestContentHolderCrossedNginxMarkerReturnsNotFound(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	id := fx.uploadArtifact(t, "private.txt", "private", []byte("secret"))
	response := contentRequest(fx.contentHandler(), id, map[string]string{"X-Forwarded-Proto": "https"})
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("crossed-nginx content = %d, want 404", response.StatusCode)
	}
}

// R-47PA-LU97
func TestArtifactReferenceFetchMatchesStoredHashAndSize(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	body := []byte("reference bytes\x00exact")
	id := fx.uploadArtifact(t, "reference.bin", "private", body)
	artifact, err := fx.store.GetArtifact(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	listener := listenOnArtifactPort(t)
	server := &http.Server{Handler: fx.contentHandler()}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()

	reference := fx.service.Reference(artifact)
	wantURL := registry.BaseURL("artifacts") + "/content?id=" + id
	if reference.ContentURL != wantURL || reference.ContentHash != artifact.ContentHash || reference.Size != artifact.Size {
		t.Fatalf("reference = %#v, want url/hash/size %q/%q/%d", reference, wantURL, artifact.ContentHash, artifact.Size)
	}
	response, err := http.Get(reference.ContentURL)
	if err != nil {
		t.Fatal(err)
	}
	fetched, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("reference fetch = %d, %v", response.StatusCode, readErr)
	}
	digest := sha256.Sum256(fetched)
	if got := hex.EncodeToString(digest[:]); got != reference.ContentHash {
		t.Fatalf("fetched hash = %q, want %q", got, reference.ContentHash)
	}
}

type contentRouter struct{ mux *http.ServeMux }

func (rt *contentRouter) HandleLoopback(pattern string, handler http.Handler) {
	rt.mux.Handle(pattern, server.LoopbackOnly(handler))
}

func (fx *uploadFixture) contentHandler() http.Handler {
	router := &contentRouter{mux: http.NewServeMux()}
	fx.service.MountContent(router)
	return router.mux
}

type contentResponse struct {
	StatusCode int
	header     http.Header
	body       []byte
}

func contentRequest(handler http.Handler, id string, headers map[string]string) contentResponse {
	request, _ := http.NewRequest(http.MethodGet, "/content?id="+id, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := &responseCapture{header: make(http.Header)}
	handler.ServeHTTP(recorder, request)
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return contentResponse{StatusCode: recorder.status, header: recorder.header, body: recorder.body.Bytes()}
}

type responseCapture struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *responseCapture) Header() http.Header         { return r.header }
func (r *responseCapture) WriteHeader(status int)      { r.status = status }
func (r *responseCapture) Write(p []byte) (int, error) { return r.body.Write(p) }

func listenOnArtifactPort(t *testing.T) net.Listener {
	t.Helper()
	port, ok := registry.Port("artifacts")
	if !ok {
		t.Fatal("artifacts port is absent from registry")
	}
	unlock, err := testutil.LockRegistryPort(port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unlock(); err != nil {
			t.Errorf("release artifacts registry port lock: %v", err)
		}
	})
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on artifacts registry port: %v", err)
	}
	return listener
}
