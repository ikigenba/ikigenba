package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestAppCSSDefinesAvatarHoverAffordance(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	rule := cssRule(t, body, `.identity .avatar:hover`)

	// R-VVY7-ADWO
	if !strings.Contains(rule, `background: var(--accent-700);`) {
		t.Errorf("avatar hover rule does not darken to accent-700:\n%s", rule)
	}
	if !strings.Contains(body, `transition: background var(--duration-fast) var(--ease-standard);`) {
		t.Errorf("avatar rule missing background transition:\n%s", body)
	}
}

func bannerChrome(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `<header class="banner">`)
	if start < 0 {
		t.Fatalf("body missing banner header:\n%s", body)
	}
	end := strings.Index(body[start:], `</header>`)
	if end < 0 {
		t.Fatalf("banner header is not closed:\n%s", body[start:])
	}
	return body[start : start+end+len(`</header>`)]
}

func cssRule(t *testing.T, css, selector string) string {
	t.Helper()

	start := strings.Index(css, selector+" {")
	if start < 0 {
		t.Fatalf("CSS missing %s rule:\n%s", selector, css)
	}
	end := strings.Index(css[start:], `}`)
	if end < 0 {
		t.Fatalf("CSS rule %s is not closed:\n%s", selector, css[start:])
	}
	return css[start : start+end+len(`}`)]
}
