package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appkitserver "appkit/server"
)

func TestEveryShippedHTMLDocumentLinksBrandIcons(t *testing.T) {
	// R-RYDN-YNR5
	root := wwwRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("enumerate shipped web documents: %v", err)
	}

	var documents []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext == ".html" || ext == ".tmpl" {
			documents = append(documents, entry.Name())
		}
	}
	if len(documents) == 0 {
		t.Fatal("committed web tree contains no HTML documents")
	}

	site := loadWWW(t)
	for _, document := range documents {
		t.Run(document, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := site.Render(rec, document, landingData("dropbox", "test")); err != nil {
				t.Fatalf("render %s through web site: %v", document, err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("render %s status = %d, want %d", document, rec.Code, http.StatusOK)
			}
			head := htmlHead(t, rec.Body.String())
			for _, link := range []string{
				`<link rel="icon" sizes="any" href="static/favicon.ico">`,
				`<link rel="icon" type="image/png" href="static/favicon-32.png">`,
				`<link rel="apple-touch-icon" href="static/apple-touch-icon.png">`,
			} {
				if !strings.Contains(head, link) {
					t.Errorf("rendered %s head missing %q:\n%s", document, link, head)
				}
			}
		})
	}
}

func TestAssembledRouterServesBrandIcons(t *testing.T) {
	// R-RZLK-CFHU
	srv, err := appkitserver.New(appkitserver.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/dropbox/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test",
		Service:    "dropbox",
		WWW:        loadWWW(t),
	})
	if err != nil {
		t.Fatalf("assemble dropbox router: %v", err)
	}

	for _, tc := range []struct {
		name        string
		path        string
		contentType string
	}{
		{name: "ico", path: "/static/favicon.ico", contentType: "image/x-icon"},
		{name: "favicon png", path: "/static/favicon-32.png", contentType: "image/png"},
		{name: "apple touch png", path: "/static/apple-touch-icon.png", contentType: "image/png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d; body=%q", tc.path, rec.Code, http.StatusOK, rec.Body.String())
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("GET %s returned an empty body", tc.path)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Fatalf("GET %s Content-Type = %q, want %q", tc.path, got, tc.contentType)
			}
		})
	}
}
