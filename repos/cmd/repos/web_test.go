package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appweb "appkit/web"
)

func TestChassisStaticServesVendoredTokensAndFont(t *testing.T) {
	// R-G448-1TTM
	mux := http.NewServeMux()
	mux.Handle("GET /static/", loadReposWWW(t).Static())
	for _, test := range []struct {
		path        string
		contentType string
		prefix      []byte
	}{
		{path: "/static/tokens.css", contentType: "text/css; charset=utf-8", prefix: []byte("/*")},
		{path: "/static/fonts/space-grotesk.woff2", contentType: "font/woff2", prefix: []byte("wOF2")},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d", test.path, response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("GET %s Content-Type = %q, want %q", test.path, got, test.contentType)
			}
			if !bytes.HasPrefix(response.Body.Bytes(), test.prefix) {
				t.Fatalf("GET %s body does not begin with %q", test.path, test.prefix)
			}
		})
	}
}

func loadReposWWW(t *testing.T) *appweb.Site {
	t.Helper()
	site, err := appweb.Load(filepath.Join("..", "..", "share", "www"))
	if err != nil {
		t.Fatal(err)
	}
	return site
}
