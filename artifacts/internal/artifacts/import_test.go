package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"registry"
)

// R-4PZS-CEDM
func TestImportCreatedEventExistsOnlyForCommittedArtifact(t *testing.T) {
	fx := newUploadFixture(t, 8)
	fx.enableOutbox(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /missing", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	mux.HandleFunc("GET /large", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bytes.Repeat([]byte("x"), 9)) })
	server := startArtifactPortServer(t, mux)
	defer server.Close()

	artifact, err := fx.service.Import(context.Background(), testIdentity(), registry.BaseURL("artifacts")+"/ok", "import.txt", "private", "imported")
	if err != nil {
		t.Fatal(err)
	}
	rows := artifactOutboxRows(t, fx.conn)
	if len(rows) != 1 || rows[0].Kind != "created" || rows[0].Subject != "/"+artifact.ID {
		t.Fatalf("successful import outbox = %#v", rows)
	}
	var payload createdPayload
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != artifact.ID || payload.Filename != "import.txt" || payload.Description != "imported" || payload.Visibility != "private" || payload.OwnerID != testIdentity().OwnerID || payload.OwnerEmail != testIdentity().OwnerEmail || payload.Size != 2 || len(payload.ContentHash) != 64 || payload.ContentURL != fx.service.Reference(artifact).ContentURL {
		t.Fatalf("import created payload = %#v", payload)
	}
	for _, attempt := range []struct{ source, filename string }{
		{"not-a-url", "invalid.txt"},
		{registry.BaseURL("artifacts") + "/missing", "missing.txt"},
		{registry.BaseURL("artifacts") + "/large", "large.txt"},
	} {
		_, _ = fx.service.Import(context.Background(), testIdentity(), attempt.source, attempt.filename, "private", "")
	}
	if got := len(artifactOutboxRows(t, fx.conn)); got != 1 {
		t.Fatalf("refused imports left %d outbox rows, want 1", got)
	}
}

// R-48X6-ZLZW
func TestImportStoresExactLoopbackBytesAndServesPublicDownload(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	fixture := []byte{0, 4, 8, 15, 16, 23, 42, 0xff}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /source", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(fixture) })
	fx.service.MountDownloads(&downloadRouter{mux: mux})
	server := startArtifactPortServer(t, mux)
	defer server.Close()

	artifact, err := fx.service.Import(context.Background(), testIdentity(), registry.BaseURL("artifacts")+"/source", "imported.bin", "public", "from holder")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Filename != "imported.bin" || artifact.Visibility != "public" || artifact.Description != "from holder" || artifact.OwnerID != testIdentity().OwnerID || artifact.Size != int64(len(fixture)) {
		t.Fatalf("imported artifact = %#v", artifact)
	}
	stored, err := fx.blobs.Open(artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedBody, readErr := io.ReadAll(stored)
	_ = stored.Close()
	if readErr != nil || !bytes.Equal(storedBody, fixture) {
		t.Fatalf("stored import = %v, %v; want %v", storedBody, readErr, fixture)
	}
	downloadURL := registry.BaseURL("artifacts") + "/f/" + url.PathEscape(artifact.ID) + "/" + url.PathEscape(artifact.Filename)
	response, err := http.Get(downloadURL)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(downloaded, fixture) {
		t.Fatalf("download imported artifact = %d/%v/%v", response.StatusCode, downloaded, readErr)
	}
}

// R-4A53-DDQL
func TestImportRejectsUnconfinedURLsBeforeNetworkIO(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if isRegistryPort(port) {
		t.Fatalf("canary unexpectedly received registry-assigned port %d", port)
	}
	var connections atomic.Int64
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			_ = connection.Close()
		}
	}()

	for _, tc := range []struct{ name, source string }{
		{"non-loopback", fmt.Sprintf("http://0.0.0.0:%d/source", port)},
		{"unassigned-port", fmt.Sprintf("http://127.0.0.1:%d/source", port)},
		{"https", fmt.Sprintf("https://127.0.0.1:%d/source", port)},
		{"relative", "/content?id=foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fx.service.Import(context.Background(), testIdentity(), tc.source, "safe.txt", "private", "")
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Import(%q) error = %v, want ValidationError", tc.source, err)
			}
		})
	}
	if got := connections.Load(); got != 0 {
		t.Fatalf("invalid imports made %d network connections, want zero", got)
	}
	if got := rowCount(t, fx.conn, "artifacts"); got != 0 {
		t.Fatalf("invalid imports stored %d artifacts", got)
	}
}

// R-4BCZ-R5HA
func TestImportSourceFailuresAndOversizeStoreNothing(t *testing.T) {
	fx := newUploadFixture(t, 8)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /missing", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	mux.HandleFunc("GET /dropped", func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("loopback response writer cannot hijack")
			return
		}
		connection, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 20\r\n\r\nshort")
		_ = buffer.Flush()
		_ = connection.Close()
	})
	mux.HandleFunc("GET /large", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bytes.Repeat([]byte("x"), 9)) })
	server := startArtifactPortServer(t, mux)
	defer server.Close()

	for _, tc := range []struct{ path, code string }{
		{"/missing", ImportSourceUnavailable},
		{"/dropped", ImportSourceUnavailable},
		{"/large", ImportTooLarge},
	} {
		_, err := fx.service.Import(context.Background(), testIdentity(), registry.BaseURL("artifacts")+tc.path, "failed.bin", "private", "")
		var importErr *ImportError
		if !errors.As(err, &importErr) || importErr.Code != tc.code {
			t.Errorf("Import(%s) error = %v, want %s", tc.path, err, tc.code)
		}
	}
	if got := rowCount(t, fx.conn, "artifacts"); got != 0 {
		t.Fatalf("failed imports stored %d artifacts", got)
	}
	entries, err := os.ReadDir(filepath.Join(fx.blobs.Root, "blobs"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed imports left blob files: %v", entries)
	}
}

func startArtifactPortServer(t *testing.T, handler http.Handler) *http.Server {
	t.Helper()
	server := &http.Server{Handler: handler}
	listener := listenOnArtifactPort(t)
	go func() { _ = server.Serve(listener) }()
	return server
}
