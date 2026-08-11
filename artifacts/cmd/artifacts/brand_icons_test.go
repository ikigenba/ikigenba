package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"

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
