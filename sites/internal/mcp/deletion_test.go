package mcp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sites/internal/sites"
	"testing"
)

func writeDeletionFixture(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, "public", "demo", filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileDeleteCommitsThenRemovesAndRecordsSha(t *testing.T) {
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	writeDeletionFixture(t, root, "about.html", "about")
	result := call(t, h, "file_delete", map[string]any{"site": "demo", "path": "about.html"})
	calls := transport.snapshot()
	changes := decodedCommitChanges(t, calls[0])
	site, err := h.store.Get(context.Background(), "demo")
	_, statErr := os.Stat(filepath.Join(root, "public", "demo", "about.html"))
	// R-FOU8-MX0M
	if result.IsError || len(calls) != 1 || calls[0].body["message"] != "delete about.html" || len(changes) != 1 || !changes["about.html"].Delete || !errors.Is(statErr, os.ErrNotExist) || err != nil || site.RepoSha != "write-sha" {
		t.Fatalf("result=%+v calls=%+v changes=%+v stat=%v site=%+v err=%v", result, calls, changes, statErr, site, err)
	}
}

func TestFileDeleteRefusesDirectoryWithoutPlaneCall(t *testing.T) {
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	writeDeletionFixture(t, root, "qr/index.html", "unchanged")
	result := callErr(t, h, "file_delete", map[string]any{"site": "demo", "path": "qr"})
	got, err := os.ReadFile(filepath.Join(root, "public", "demo", "qr", "index.html"))
	// R-FQ25-0ORB
	if result["code"] != "validation" || len(transport.snapshot()) != 0 || err != nil || !bytes.Equal(got, []byte("unchanged")) {
		t.Fatalf("result=%+v calls=%+v file=%q err=%v", result, transport.snapshot(), got, err)
	}
}

func TestFileDeleteReportsMissingWithoutPlaneCall(t *testing.T) {
	transport := &commitRecordingTransport{}
	h, _, _ := newWritePathHandler(t, transport)
	result := callErr(t, h, "file_delete", map[string]any{"site": "demo", "path": "nope.html"})
	// R-FRA1-EGI0
	if result["code"] != "not_found" || len(transport.snapshot()) != 0 {
		t.Fatalf("result=%+v calls=%+v", result, transport.snapshot())
	}
}

func TestFileDeleteFailedCommitPreservesFileAndSha(t *testing.T) {
	transport := &commitRecordingTransport{status: http.StatusBadRequest}
	h, root, _ := newWritePathHandler(t, transport)
	writeDeletionFixture(t, root, "about.html", "unchanged")
	if err := h.store.SetRepoSha(context.Background(), "demo", "before-sha"); err != nil {
		t.Fatal(err)
	}
	result := callErr(t, h, "file_delete", map[string]any{"site": "demo", "path": "about.html"})
	got, readErr := os.ReadFile(filepath.Join(root, "public", "demo", "about.html"))
	site, getErr := h.store.Get(context.Background(), "demo")
	// R-FSHX-S88P
	if result["code"] != "internal" || !bytes.Equal(got, []byte("unchanged")) || readErr != nil || getErr != nil || site.RepoSha != "before-sha" {
		t.Fatalf("result=%+v file=%q readErr=%v site=%+v getErr=%v", result, got, readErr, site, getErr)
	}
}

func TestFileDeleteMapsVersionPlaneFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport *commitRecordingTransport
		want      string
	}{
		{"unavailable", &commitRecordingTransport{err: errors.New("connection refused")}, "source_unavailable"},
		{"rejected", &commitRecordingTransport{status: http.StatusBadRequest}, "internal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, root, _ := newWritePathHandler(t, tc.transport)
			writeDeletionFixture(t, root, "about.html", "unchanged")
			result := callErr(t, h, "file_delete", map[string]any{"site": "demo", "path": "about.html"})
			// R-FTPU-5ZZE
			if result["code"] != tc.want {
				t.Fatalf("code=%v want=%s result=%+v", result["code"], tc.want, result)
			}
		})
	}
}

func TestRmdirCommitsWholeSubtreeOnceAndRemovesOnlyIt(t *testing.T) {
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	writeDeletionFixture(t, root, "qr/index.html", "qr")
	writeDeletionFixture(t, root, "qr/app.css", "css")
	writeDeletionFixture(t, root, "index.html", "home")
	result := call(t, h, "rmdir", map[string]any{"site": "demo", "path": "qr"})
	calls := transport.snapshot()
	changes := decodedCommitChanges(t, calls[0])
	entries, readErr := os.ReadDir(filepath.Join(root, "public", "demo"))
	site, getErr := h.store.Get(context.Background(), "demo")
	// R-FUXQ-JRQ3
	if result.IsError || result.StructuredContent["files"] != float64(2) || len(calls) != 1 || len(changes) != 2 || !changes["qr/index.html"].Delete || !changes["qr/app.css"].Delete || readErr != nil || len(entries) != 1 || entries[0].Name() != "index.html" || getErr != nil || site.RepoSha != "write-sha" {
		t.Fatalf("result=%+v calls=%+v changes=%+v entries=%+v readErr=%v site=%+v getErr=%v", result, calls, changes, entries, readErr, site, getErr)
	}
}

func TestRmdirRefusesRegularFileWithoutPlaneCall(t *testing.T) {
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	writeDeletionFixture(t, root, "qr", "unchanged")
	result := callErr(t, h, "rmdir", map[string]any{"site": "demo", "path": "qr"})
	got, err := os.ReadFile(filepath.Join(root, "public", "demo", "qr"))
	// R-FW5M-XJGS
	if result["code"] != "validation" || len(transport.snapshot()) != 0 || err != nil || !bytes.Equal(got, []byte("unchanged")) {
		t.Fatalf("result=%+v calls=%+v file=%q err=%v", result, transport.snapshot(), got, err)
	}
}

func TestRmdirFailedCommitPreservesSubtreeAndSha(t *testing.T) {
	transport := &commitRecordingTransport{status: http.StatusBadRequest}
	h, root, _ := newWritePathHandler(t, transport)
	writeDeletionFixture(t, root, "qr/index.html", "html")
	writeDeletionFixture(t, root, "qr/app.css", "css")
	if err := h.store.SetRepoSha(context.Background(), "demo", "before-sha"); err != nil {
		t.Fatal(err)
	}
	result := callErr(t, h, "rmdir", map[string]any{"site": "demo", "path": "qr"})
	html, htmlErr := os.ReadFile(filepath.Join(root, "public", "demo", "qr", "index.html"))
	css, cssErr := os.ReadFile(filepath.Join(root, "public", "demo", "qr", "app.css"))
	site, getErr := h.store.Get(context.Background(), "demo")
	// R-FXDJ-BB7H
	if result["code"] != "internal" || htmlErr != nil || cssErr != nil || string(html) != "html" || string(css) != "css" || getErr != nil || site.RepoSha != "before-sha" {
		t.Fatalf("result=%+v html=%q/%v css=%q/%v site=%+v/%v", result, html, htmlErr, css, cssErr, site, getErr)
	}
}

func TestDeletionCommitsPublishRootPrefixedPaths(t *testing.T) {
	transport := &commitRecordingTransport{}
	version := sites.NewVersionClient("http://repos.test", &http.Client{Transport: transport})
	h, root := newTestHandlerWithVersion(t, version)
	if _, err := h.store.Create(context.Background(), "demo", "demo", testOwnerID, testOwner, sites.Public, "public"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "public", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDeletionFixture(t, root, "about.html", "about")
	callOK(t, h, "file_delete", map[string]any{"site": "demo", "path": "about.html"})
	writeDeletionFixture(t, root, "blog/a.html", "blog")
	callOK(t, h, "rmdir", map[string]any{"site": "demo", "path": "blog"})
	calls := transport.snapshot()
	fileChanges := decodedCommitChanges(t, calls[0])
	dirChanges := decodedCommitChanges(t, calls[1])
	_, fileErr := os.Stat(filepath.Join(root, "public", "demo", "about.html"))
	_, dirErr := os.Stat(filepath.Join(root, "public", "demo", "blog"))
	// R-FYLF-P2Y6
	if len(calls) != 2 || !fileChanges["public/about.html"].Delete || !dirChanges["public/blog/a.html"].Delete || !errors.Is(fileErr, os.ErrNotExist) || !errors.Is(dirErr, os.ErrNotExist) {
		t.Fatalf("calls=%+v fileChanges=%+v dirChanges=%+v fileErr=%v dirErr=%v", calls, fileChanges, dirChanges, fileErr, dirErr)
	}
}
