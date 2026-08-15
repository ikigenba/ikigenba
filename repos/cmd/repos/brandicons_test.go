package main

import (
	"eventplane/outbox"
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

func TestEveryServedDocumentCarriesBrandIconLinks(t *testing.T) {
	// R-RYDN-YNR5
	wwwRoot := filepath.Join("..", "..", "share", "www")
	entries, err := os.ReadDir(wwwRoot)
	if err != nil {
		t.Fatal(err)
	}
	site := loadReposWWW(t)
	documentCount := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".html" {
			continue
		}
		documentCount++
		t.Run(entry.Name(), func(t *testing.T) {
			response := httptest.NewRecorder()
			view := landingView{Service: "repos", Version: "test", Repos: []repoRow{}, ReposData: "[]"}
			if err := site.Render(response, entry.Name(), view); err != nil {
				t.Fatalf("render %s: %v", entry.Name(), err)
			}
			body := response.Body.String()
			headStart := strings.Index(body, "<head>")
			headEnd := strings.Index(body, "</head>")
			if headStart < 0 || headEnd <= headStart {
				t.Fatalf("rendered %s has no complete head: %s", entry.Name(), body)
			}
			head := body[headStart:headEnd]
			for _, link := range []string{
				`<link rel="icon" sizes="any" href="static/favicon.ico">`,
				`<link rel="icon" type="image/png" href="static/favicon-32.png">`,
				`<link rel="apple-touch-icon" href="static/apple-touch-icon.png">`,
			} {
				if !strings.Contains(head, link) {
					t.Errorf("rendered %s head is missing %s", entry.Name(), link)
				}
			}
		})
	}
	if documentCount == 0 {
		t.Fatal("committed www tree contains no HTML documents")
	}
}

func TestAssembledRouterServesBrandIconAssets(t *testing.T) {
	// R-RZLK-CFHU
	handler := assembledBrandIconHandler(t)
	for _, test := range []struct {
		name        string
		contentType string
	}{
		{name: "favicon.ico", contentType: "image/x-icon"},
		{name: "favicon-32.png", contentType: "image/png"},
		{name: "apple-touch-icon.png", contentType: "image/png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := "/static/" + test.name
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
			}
			if response.Body.Len() == 0 {
				t.Fatalf("GET %s returned an empty body", path)
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("GET %s Content-Type = %q, want %q", path, got, test.contentType)
			}
		})
	}
}

func assembledBrandIconHandler(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("REPOS_STATE_DIR", t.TempDir())
	t.Setenv("REPOS_GIT_BIN", "git")
	t.Setenv("REPOS_MAX_COMMIT_BYTES", "")

	fixture := newLandingFixture(t)
	spec := reposSpec()
	producer, err := outbox.New(fixture.conn, outbox.Options{
		Source: spec.App, Registry: spec.Events,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := appkitserver.New(appkitserver.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "http://localhost/srv/repos/mcp",
		AuthServer: "http://localhost",
		Version:    "test",
		Service:    spec.App,
		Feed:       producer.FeedHandler(),
		FeedPath:   spec.Feed,
		Register:   spec.Handlers,
		Health:     spec.Health,
		Events:     spec.Events,
		WWW:        loadReposWWW(t),
		DB:         fixture.conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Producer(producer); err != nil {
		t.Fatal(err)
	}
	return server.Handler
}
