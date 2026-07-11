package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestLandingHandlerRendersHiddenCopyButtonsForEachSiteURL(t *testing.T) {
	store := newLandingTestStore(t, landingSeed{name: "X", public: true}, landingSeed{name: "Y", public: false})
	baseURL := "https://suite.example/srv/sites/"
	rec := httptest.NewRecorder()
	landingHandler(store, loadWWW(t), "sites", "phase28", baseURL).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	// R-NM1L-GSYE
	for _, row := range []struct {
		slug string
		url  string
	}{
		{"X", baseURL + "public/X/"},
		{"Y", baseURL + "private/Y/"},
	} {
		anchor := `<td data-label="Slug"><a href="` + row.url + `">` + row.slug + `</a></td>`
		if !strings.Contains(body, anchor) {
			t.Fatalf("landing HTML missing %s slug anchor %q:\n%s", row.slug, anchor, body)
		}
		copyCell := regexp.MustCompile(`(?s)<td data-label="Copy"><button type="button" class="copy-btn js-only" data-url="` + regexp.QuoteMeta(row.url) + `" hidden>.*?<span class="copy-label">Copy</span></button></td>`)
		if !copyCell.MatchString(body) {
			t.Fatalf("landing HTML missing hidden copy button for %s URL %q:\n%s", row.slug, row.url, body)
		}
	}
	if !strings.Contains(body, ".copy-btn {") {
		t.Fatalf("landing HTML lacks a local CSS rule for the copy button:\n%s", body)
	}
}
