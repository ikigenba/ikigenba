package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLandingHandlerRendersServiceVersionAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	LandingHandler("sites", "9.9.9-test").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	// R-LAND-3C9K
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// R-LAND-5E2M
	if !strings.Contains(body, "sites") {
		t.Fatalf("body does not contain service name: %q", body)
	}
	// R-LAND-7G4P
	if !strings.Contains(body, "9.9.9-test") {
		t.Fatalf("body does not contain version: %q", body)
	}
	// R-LAND-9J6R
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
}

func TestExactRootRouteDispatchesWithoutShadowingSiblings(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", LandingHandler("sites", "1.2.3"))
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("mcp"))
	})

	root := httptest.NewRecorder()
	mux.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	rootBody := root.Body.String()
	// R-ROUT-4Q8B
	if root.Code != http.StatusOK || !strings.Contains(rootBody, "sites") || !strings.Contains(rootBody, "1.2.3") {
		t.Fatalf("GET / returned status %d body %q, want landing page", root.Code, rootBody)
	}

	mcp := httptest.NewRecorder()
	mux.ServeHTTP(mcp, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	// R-ROUT-6S1D
	if mcp.Code != http.StatusAccepted || strings.Contains(mcp.Body.String(), "sites") {
		t.Fatalf("POST /mcp returned status %d body %q, want sibling handler", mcp.Code, mcp.Body.String())
	}

	nope := httptest.NewRecorder()
	mux.ServeHTTP(nope, httptest.NewRequest(http.MethodGet, "/nope", nil))
	// R-ROUT-8U3F
	if nope.Code == http.StatusOK || strings.Contains(nope.Body.String(), "sites") {
		t.Fatalf("GET /nope returned status %d body %q, want not found without landing page", nope.Code, nope.Body.String())
	}
}

func TestStaticHandlerServesTokensCSS(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", StaticHandler())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/tokens.css", nil))
	body := rec.Body.String()

	// R-ASST-3H7N
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("GET /static/tokens.css returned status %d Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, `url("/srv/sites/static/fonts/space-grotesk.woff2")`) {
		t.Fatalf("tokens.css does not point at embedded service font path: %q", body)
	}
	for _, want := range []string{
		"--color-background:",
		"--space-4: 4px;",
		"--type-display-size: 56px;",
		"--type-display-line: 1.04;",
		"--type-label-size: 12px;",
		"--type-label-weight: 500;",
		"border-radius: var(--radius-tight);",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("tokens.css missing Carbon landing token %q in: %q", want, body)
		}
	}
}

func TestLandingHTMLReferencesOwnEmbeddedStaticPath(t *testing.T) {
	rec := httptest.NewRecorder()
	LandingHandler("sites", "asset-test").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	// R-ASST-5K9Q
	if !strings.Contains(body, `/srv/sites/static/tokens.css`) {
		t.Fatalf("landing HTML does not reference embedded static path: %q", body)
	}
	if strings.Contains(body, "dashboard") || strings.Contains(body, "://") {
		t.Fatalf("landing HTML references a cross-service or remote asset URL: %q", body)
	}
}

func TestStaticHandlerServesEmbeddedFonts(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", StaticHandler())

	// R-ASST-7M2S
	for _, font := range []string{
		"space-grotesk.woff2",
		"ibm-plex-sans.woff2",
		"ibm-plex-mono-400.woff2",
		"ibm-plex-mono-500.woff2",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/fonts/"+font, nil))

		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" {
			t.Fatalf("GET %s returned status %d Content-Type %q", font, rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("GET %s returned an empty body", font)
		}
	}
}
