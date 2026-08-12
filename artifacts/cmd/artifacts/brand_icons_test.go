package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	appkitdb "appkit/db"
	artifactdata "artifacts/internal/artifacts"
	"artifacts/internal/db"
	artifactweb "artifacts/internal/web"
)

// R-RYDN-YNR5
func TestEveryServedDocumentCarriesBrandIconLinks(t *testing.T) {
	router := assembledWebRouter(t)
	documents, err := filepath.Glob(filepath.Join("..", "..", "internal", "web", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) == 0 {
		t.Fatal("committed document set is empty")
	}
	routes := map[string]string{"landing.html": "/srv/artifacts/"}
	expected := []map[string]string{
		{"rel": "icon", "href": "static/favicon.ico", "sizes": "any"},
		{"rel": "icon", "href": "static/favicon-32.png", "type": "image/png"},
		{"rel": "apple-touch-icon", "href": "static/apple-touch-icon.png"},
	}
	for _, document := range documents {
		name := filepath.Base(document)
		route, ok := routes[name]
		if !ok {
			t.Fatalf("committed document %s has no assembled-router proof route", name)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s for %s status = %d: %s", route, name, response.Code, response.Body.String())
		}
		head := regexp.MustCompile(`(?is)<head\b[^>]*>(.*?)</head>`).FindSubmatch(response.Body.Bytes())
		if len(head) != 2 {
			t.Fatalf("served %s has no single head element", name)
		}
		links := regexp.MustCompile(`(?i)<link\b[^>]*>`).FindAll(head[1], -1)
		for _, want := range expected {
			if !containsLink(links, want) {
				t.Errorf("served %s head lacks link attributes %v\n%s", name, want, head[1])
			}
		}
	}
}

// R-RZLK-CFHU
func TestBrandIconAssetsThroughAssembledRouter(t *testing.T) {
	router := assembledWebRouter(t)
	assets := map[string]string{
		"favicon.ico":          "image/x-icon",
		"favicon-32.png":       "image/png",
		"apple-touch-icon.png": "image/png",
	}
	for name, contentType := range assets {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/srv/artifacts/static/"+name, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			if response.Body.Len() == 0 {
				t.Fatal("body is empty")
			}
			if got := response.Header().Get("Content-Type"); got != contentType {
				t.Fatalf("Content-Type = %q, want %q", got, contentType)
			}
		})
	}
}

// R-8MFA-HUNC
func TestRootFaviconMatchesStaticPathThroughCompositionRoot(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"cache", "state"} {
		if err := os.MkdirAll(filepath.Join(root, "artifacts", directory), 0o755); err != nil {
			t.Fatalf("create artifacts %s directory: %v", directory, err)
		}
	}
	binary := filepath.Join(root, "artifacts-bin")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build artifacts: %v\n%s", err, output)
	}

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, binary, "serve")
	command.Env = append(os.Environ(),
		"IKIGENBA_DOMAIN=int.ikigenba.com",
		"IKIGENBA_ROOT="+root,
		"ARTIFACTS_IP=127.0.0.1",
		fmt.Sprintf("ARTIFACTS_PORT=%d", port),
		"ARTIFACTS_MAX_UPLOAD_BYTES=209715200",
	)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start artifacts: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
		}
	}()
	waitForHealth(t, port, done, &stdout, &stderr)

	request := func(path string) (int, string, []byte) {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
		response, err := (&http.Client{Timeout: 5 * time.Second}).Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read GET %s: read=%v close=%v", path, readErr, closeErr)
		}
		return response.StatusCode, response.Header.Get("Content-Type"), body
	}

	rootStatus, rootContentType, rootBody := request("/favicon.ico")
	if rootStatus != http.StatusOK {
		t.Fatalf("GET /favicon.ico status = %d, want 200", rootStatus)
	}
	if rootContentType != "image/x-icon" {
		t.Fatalf("GET /favicon.ico Content-Type = %q, want %q", rootContentType, "image/x-icon")
	}
	staticStatus, _, staticBody := request("/static/favicon.ico")
	if staticStatus != http.StatusOK {
		t.Fatalf("GET /static/favicon.ico status = %d, want 200", staticStatus)
	}
	if !bytes.Equal(rootBody, staticBody) {
		t.Fatalf("GET /favicon.ico body differs from GET /static/favicon.ico: root=%d bytes static=%d bytes", len(rootBody), len(staticBody))
	}
}

func assembledWebRouter(t *testing.T) http.Handler {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	svc := &artifactdata.Service{Store: db.NewStore(conn, nil)}
	mux := http.NewServeMux()
	mux.Handle("GET /srv/artifacts/{$}", artifactweb.LandingHandler(svc, "artifacts", "v0.1.0"))
	mux.Handle("GET /srv/artifacts/static/", http.StripPrefix("/srv/artifacts/", artifactweb.StaticHandler()))
	return mux
}

func containsLink(links [][]byte, want map[string]string) bool {
	attributePattern := regexp.MustCompile(`([[:alnum:]-]+)\s*=\s*"([^"]*)"`)
	for _, link := range links {
		attributes := make(map[string]string)
		for _, match := range attributePattern.FindAllSubmatch(link, -1) {
			attributes[string(match[1])] = string(match[2])
		}
		matches := true
		for name, value := range want {
			if attributes[name] != value {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
