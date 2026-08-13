package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appkitdb "appkit/db"
	appserver "appkit/server"
	appweb "appkit/web"
	"artifacts/internal/db"
)

// R-RYDN-YNR5
func TestEveryServedDocumentCarriesBrandIconLinks(t *testing.T) {
	router, _ := assembledWebRouter(t)
	documents, err := filepath.Glob(filepath.Join(wwwRoot(t), "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) == 0 {
		t.Fatal("committed document set is empty")
	}
	routes := map[string]string{"landing.html": "/"}
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
	router, _ := assembledWebRouter(t)
	assets := map[string]string{
		"favicon.ico":          "image/x-icon",
		"favicon-32.png":       "image/png",
		"apple-touch-icon.png": "image/png",
	}
	for name, contentType := range assets {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/"+name, nil))
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
	router, _ := assembledWebRouter(t)
	request := func(path string) (int, string, []byte) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response.Code, response.Header().Get("Content-Type"), response.Body.Bytes()
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

func assembledWebRouter(t *testing.T) (http.Handler, *db.Store) {
	t.Helper()
	t.Setenv("IKIGENBA_ROOT", t.TempDir())
	t.Setenv("ARTIFACTS_MAX_UPLOAD_BYTES", "209715200")
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
	store := db.NewStore(conn, nil)
	www, err := appweb.Load(wwwRoot(t))
	if err != nil {
		t.Fatalf("load committed share/www: %v", err)
	}
	spec := artifactsSpec()
	srv, err := appserver.New(appserver.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/artifacts/mcp",
		AuthServer: "https://int.ikigenba.com/",
		Version:    strings.TrimSpace(string(mustRead(t, filepath.Join("..", "..", "VERSION")))),
		Service:    "artifacts",
		Events:     spec.Events,
		WWW:        www,
		DB:         conn,
		Register:   spec.Handlers,
	})
	if err != nil {
		t.Fatalf("assemble artifacts router: %v", err)
	}
	return srv.Handler, store
}

func wwwRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatal(err)
	}
	return root
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
