package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appkitweb "appkit/web"

	"wiki/internal/wiki"
)

// R-RYDN-YNR5
func TestEveryRenderedDocumentCarriesBrandIconLinks(t *testing.T) {
	root := testWWWRoot(t)
	site, err := appkitweb.Load(root)
	if err != nil {
		t.Fatalf("load committed site: %v", err)
	}
	var documents []string
	for _, pattern := range []string{"*.html", "*.tmpl"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("enumerate committed documents matching %s: %v", pattern, err)
		}
		documents = append(documents, matches...)
	}

	want := []string{
		`<link rel="icon" href="/srv/wiki/static/favicon.ico" sizes="any">`,
		`<link rel="icon" href="/srv/wiki/static/favicon-32.png" type="image/png">`,
		`<link rel="apple-touch-icon" href="/srv/wiki/static/apple-touch-icon.png">`,
	}
	rendered := 0
	for _, document := range documents {
		name := strings.TrimSuffix(filepath.Base(document), filepath.Ext(document))
		if name == "layout" {
			continue
		}
		rendered++
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			data := map[string]any{
				"Base":    wiki.Mount,
				"Mount":   wiki.Mount,
				"Service": wiki.App,
				"Subject": map[string]any{"Title": "Brand icon probe"},
			}
			if err := site.Render(rec, name, data); err != nil {
				t.Fatalf("render committed document %s: %v", document, err)
			}
			for _, link := range want {
				if !bytes.Contains(rec.Body.Bytes(), []byte(link)) {
					t.Errorf("rendered document %s missing %q", document, link)
				}
			}
		})
	}
	if rendered == 0 {
		t.Fatal("no committed rendered documents found")
	}
}

// R-RZLK-CFHU
func TestAssembledRouterServesBrandIconAssets(t *testing.T) {
	conn := migratedDB(t, context.Background())
	defer conn.Close()
	handler := buildSpecTestHandler(t, conn, newSpec(staticConfig(wiki.Config{})))

	assets := map[string]string{
		"favicon.ico":          "image/x-icon",
		"favicon-32.png":       "image/png",
		"apple-touch-icon.png": "image/png",
	}
	for name, contentType := range assets {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/"+name, nil))
			result := rec.Result()
			defer result.Body.Close()
			if result.StatusCode != http.StatusOK {
				t.Fatalf("GET /static/%s status = %d, want 200", name, result.StatusCode)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("GET /static/%s returned an empty body", name)
			}
			if got := result.Header.Get("Content-Type"); got != contentType {
				t.Fatalf("GET /static/%s Content-Type = %q, want %q", name, got, contentType)
			}
		})
	}
}
