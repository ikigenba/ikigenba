package repos

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"registry"
)

func TestContentReturnsCommittedBytesAndDistinctMissingDetails(t *testing.T) {
	// R-JJL2-2I2O
	service, repository := newReadTestService(t, "content")
	commitReadTree(t, repository, map[string]string{"index.html": "<h1>hello</h1>\n"})
	wantRev := strings.TrimSpace(runGit(t, repository, "rev-parse", "main"))

	response := serveRead(t, ContentHandler(service), "/content?kind=sites&name=content&path=index.html")
	if response.Code != http.StatusOK || response.Body.String() != "<h1>hello</h1>\n" {
		t.Fatalf("content status=%d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Repos-Rev") != wantRev {
		t.Fatalf("X-Repos-Rev = %q, want real git head %q", response.Header().Get("X-Repos-Rev"), wantRev)
	}
	if response.Header().Get("Content-Length") != fmt.Sprint(len("<h1>hello</h1>\n")) || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content length/type = %q/%q", response.Header().Get("Content-Length"), response.Header().Get("Content-Type"))
	}
	wantBlob := strings.TrimSpace(runGit(t, repository, "rev-parse", "main:index.html"))
	if response.Header().Get("X-Repos-Blob") != wantBlob {
		t.Fatalf("X-Repos-Blob = %q, want %q", response.Header().Get("X-Repos-Blob"), wantBlob)
	}

	missingPath := serveRead(t, ContentHandler(service), "/content?kind=sites&name=content&path=missing.html")
	missingRef := serveRead(t, ContentHandler(service), "/content?kind=sites&name=content&path=index.html&ref=unknown")
	if missingPath.Code != http.StatusNotFound || missingRef.Code != http.StatusNotFound {
		t.Fatalf("missing path/ref statuses = %d/%d", missingPath.Code, missingRef.Code)
	}
	if !strings.Contains(missingPath.Body.String(), "missing.html") || !strings.Contains(missingRef.Body.String(), `ref "unknown"`) {
		t.Fatalf("missing details path=%q ref=%q", missingPath.Body.String(), missingRef.Body.String())
	}
	if missingPath.Body.String() == missingRef.Body.String() {
		t.Fatal("missing path and ref returned indistinguishable details")
	}
}

func TestContentRevisionPinRejectsStaleWithoutStreaming(t *testing.T) {
	// R-JKSY-G9TD
	service, repository := newReadTestService(t, "pin")
	first := commitReadTree(t, repository, map[string]string{"note.txt": "first"})
	second := commitReadTree(t, repository, map[string]string{"note.txt": "second"})

	stale := serveRead(t, ContentHandler(service), "/content?kind=sites&name=pin&path=note.txt&rev="+first)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale pin status=%d body=%q", stale.Code, stale.Body.String())
	}
	if strings.Contains(stale.Body.String(), "second") || strings.Contains(stale.Body.String(), "first") {
		t.Fatalf("stale pin streamed file bytes: %q", stale.Body.String())
	}
	current := serveRead(t, ContentHandler(service), "/content?kind=sites&name=pin&path=note.txt&rev="+second)
	if current.Code != http.StatusOK || current.Body.String() != "second" {
		t.Fatalf("current pin status=%d body=%q", current.Code, current.Body.String())
	}
}

func TestListRecursesWithRealBlobMetadataAndHandlesEmptyRepository(t *testing.T) {
	// R-JM0U-U1K2
	service, repository := newReadTestService(t, "listing")
	files := map[string]string{"a.txt": "a", "sub/b.txt": "bee", "sub/c/d.txt": "dddd"}
	rev := commitReadTree(t, repository, files)
	response := serveRead(t, ListHandler(service), "/list?kind=sites&name=listing")
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%q", response.Code, response.Body.String())
	}
	var result ListResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Rev != rev {
		t.Fatalf("list rev=%q, want %q", result.Rev, rev)
	}
	wantTypes := map[string]string{"a.txt": "file", "sub": "dir", "sub/b.txt": "file", "sub/c": "dir", "sub/c/d.txt": "file"}
	if len(result.Entries) != len(wantTypes) {
		t.Fatalf("entries=%#v", result.Entries)
	}
	for _, entry := range result.Entries {
		if entry.Type != wantTypes[entry.Path] {
			t.Errorf("entry %q type=%q, want %q", entry.Path, entry.Type, wantTypes[entry.Path])
		}
		if entry.Type == "file" {
			if entry.Size != int64(len(files[entry.Path])) {
				t.Errorf("entry %q size=%d", entry.Path, entry.Size)
			}
			wantBlob := strings.TrimSpace(runGit(t, repository, "rev-parse", "main:"+entry.Path))
			if entry.BlobSHA != wantBlob {
				t.Errorf("entry %q blob=%q, want %q", entry.Path, entry.BlobSHA, wantBlob)
			}
		}
	}

	sub := serveRead(t, ListHandler(service), "/list?kind=sites&name=listing&path=sub")
	var subResult ListResult
	if err := json.Unmarshal(sub.Body.Bytes(), &subResult); err != nil {
		t.Fatal(err)
	}
	for _, entry := range subResult.Entries {
		if !strings.HasPrefix(entry.Path, "sub/") {
			t.Fatalf("sub listing escaped subtree: %#v", subResult.Entries)
		}
	}
	if got := entryPaths(subResult.Entries); !reflect.DeepEqual(got, []string{"sub/b.txt", "sub/c", "sub/c/d.txt"}) {
		t.Fatalf("sub paths=%v", got)
	}

	emptyService, _ := newReadTestService(t, "empty")
	empty := serveRead(t, ListHandler(emptyService), "/list?kind=sites&name=empty")
	var emptyResult ListResult
	if empty.Code != http.StatusOK || json.Unmarshal(empty.Body.Bytes(), &emptyResult) != nil || emptyResult.Rev != "" || len(emptyResult.Entries) != 0 {
		t.Fatalf("empty list status=%d result=%#v body=%q", empty.Code, emptyResult, empty.Body.String())
	}
}

func TestStatBuildsCanonicalFetchableContentURL(t *testing.T) {
	// R-JN8R-7TAR
	service, repository := newReadTestService(t, "metadata")
	contents := "some metadata\n"
	commitReadTree(t, repository, map[string]string{"docs/a file.txt": contents})
	requestPath := "/stat?kind=sites&name=metadata&path=docs%2Fa+file.txt&ref=main"
	response := serveRead(t, StatHandler(service), requestPath)
	if response.Code != http.StatusOK {
		t.Fatalf("stat status=%d body=%q", response.Code, response.Body.String())
	}
	var entry ReadEntry
	if err := json.Unmarshal(response.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	wantValues := url.Values{"kind": {"sites"}, "name": {"metadata"}, "path": {"docs/a file.txt"}, "ref": {"main"}}
	wantURL := registry.BaseURL("repos") + "/content?" + wantValues.Encode()
	if entry.ContentURL != wantURL || entry.Size != int64(len(contents)) {
		t.Fatalf("stat entry=%#v, want URL %q and size %d", entry, wantURL, len(contents))
	}
	wantBlob := strings.TrimSpace(runGit(t, repository, "rev-parse", "main:docs/a file.txt"))
	if entry.BlobSHA != wantBlob {
		t.Fatalf("stat blob=%q, want %q", entry.BlobSHA, wantBlob)
	}
	fetched := serveRead(t, ContentHandler(service), entry.ContentURL)
	if fetched.Code != http.StatusOK || fetched.Body.String() != contents {
		t.Fatalf("fetch canonical URL status=%d body=%q", fetched.Code, fetched.Body.String())
	}
	missing := serveRead(t, StatHandler(service), "/stat?kind=sites&name=metadata&path=absent")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "absent") {
		t.Fatalf("missing stat status=%d body=%q", missing.Code, missing.Body.String())
	}
}

func TestArchiveContainsOnlyRequestedRefTreeBytes(t *testing.T) {
	// R-JOGN-LL1G
	service, repository := newReadTestService(t, "bundle")
	firstFiles := map[string]string{"one.txt": "old one", "dir/two.bin": "\x00\x01old"}
	first := commitReadTree(t, repository, firstFiles)
	currentFiles := map[string]string{"one.txt": "new one", "dir/two.bin": "\x00\x01new", "three": "3"}
	commitReadTree(t, repository, currentFiles)

	for ref, wantFiles := range map[string]map[string]string{"main": currentFiles, first: firstFiles} {
		response := serveRead(t, ArchiveHandler(service), "/archive?kind=sites&name=bundle&ref="+ref)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/x-tar" {
			t.Fatalf("archive %s status=%d type=%q body=%q", ref, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
		gotFiles := readTarFiles(t, response.Body.Bytes())
		if !reflect.DeepEqual(gotFiles, wantFiles) {
			t.Fatalf("archive %s files=%#v, want %#v", ref, gotFiles, wantFiles)
		}
		wantRev := strings.TrimSpace(runGit(t, repository, "rev-parse", ref))
		if response.Header().Get("X-Repos-Rev") != wantRev {
			t.Fatalf("archive %s rev=%q, want %q", ref, response.Header().Get("X-Repos-Rev"), wantRev)
		}
		for member := range gotFiles {
			if member == ".git" || strings.HasPrefix(member, ".git/") || strings.Contains(member, "refs/") || strings.Contains(member, "objects/") {
				t.Fatalf("archive leaked VCS metadata member %q", member)
			}
		}
	}

	emptyService, _ := newReadTestService(t, "empty-bundle")
	empty := serveRead(t, ArchiveHandler(emptyService), "/archive?kind=sites&name=empty-bundle")
	if empty.Code != http.StatusNotFound {
		t.Fatalf("empty archive status=%d body=%q", empty.Code, empty.Body.String())
	}
}

func TestReadHandlersMapInvalidRepositoryKeysToDetailedBadRequests(t *testing.T) {
	service, _ := newReadTestService(t, "validation")
	for name, handler := range map[string]http.Handler{
		"content": ContentHandler(service),
		"list":    ListHandler(service),
		"stat":    StatHandler(service),
		"archive": ArchiveHandler(service),
	} {
		response := serveRead(t, handler, "/"+name+"?kind=invalid&name=validation&path=a.txt")
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), ErrValidation.Error()) || !strings.HasSuffix(response.Body.String(), "\n") {
			t.Errorf("%s invalid key status=%d body=%q", name, response.Code, response.Body.String())
		}
	}
}

func newReadTestService(t *testing.T, name string) (*Service, string) {
	t.Helper()
	custody, _ := newTestCustody(t)
	if err := custody.Init(context.Background(), "sites", name); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil)
	service.SetCustody(custody)
	repository, err := custody.Path("sites", name)
	if err != nil {
		t.Fatal(err)
	}
	return service, repository
}

func commitReadTree(t *testing.T, repository string, files map[string]string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, "", "clone", repository, work)
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(work, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, contents := range files {
		fullPath := filepath.Join(work, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-m", "read fixture")
	runGit(t, work, "push", "origin", "main")
	return strings.TrimSpace(runGit(t, repository, "rev-parse", "main"))
}

func serveRead(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func entryPaths(entries []ReadEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	sort.Strings(paths)
	return paths
}

func readTarFiles(t *testing.T, contents []byte) map[string]string {
	t.Helper()
	files := make(map[string]string)
	reader := tar.NewReader(bytes.NewReader(contents))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = string(body)
	}
}
