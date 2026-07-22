package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sitesdomain "sites/internal/sites"
)

func TestLandingTemplateRendersVisibilityEnumsVerbatim(t *testing.T) {
	rec := httptest.NewRecorder()
	view := landingView{
		Service: "sites",
		Version: "phase37",
		Sites: []siteRow{
			{Slug: "atlas", URL: "https://suite.test/public/atlas/", Visibility: "public", CreatedBy: "alice@example.com", CreatedAt: "2026-07-20T10:00:00Z"},
			{Slug: "vault", URL: "https://suite.test/private/vault/", Visibility: "private", CreatedBy: "bob@example.com", CreatedAt: "2026-07-20T11:00:00Z"},
			{Slug: "quiet", URL: "https://suite.test/public/quiet/", Visibility: "unlisted", CreatedBy: "carol@example.com", CreatedAt: "2026-07-20T12:00:00Z"},
		},
	}
	if err := loadWWW(t).Render(rec, "landing.html", view); err != nil {
		t.Fatalf("render landing.html: %v", err)
	}

	// R-HK3X-22SM
	body := rec.Body.String()
	for _, want := range []string{
		`<a href="https://suite.test/public/atlas/">atlas</a></td>` +
			`<td data-label="Visibility"><span class="visibility">public</span></td>` +
			`<td data-label="Creator">alice@example.com</td>` +
			`<td data-label="Created">2026-07-20T10:00:00Z</td>`,
		`<a href="https://suite.test/private/vault/">vault</a></td>` +
			`<td data-label="Visibility"><span class="visibility">private</span></td>` +
			`<td data-label="Creator">bob@example.com</td>` +
			`<td data-label="Created">2026-07-20T11:00:00Z</td>`,
		`<a href="https://suite.test/public/quiet/">quiet</a></td>` +
			`<td data-label="Visibility"><span class="visibility">unlisted</span></td>` +
			`<td data-label="Creator">carol@example.com</td>` +
			`<td data-label="Created">2026-07-20T12:00:00Z</td>`,
	} {
		if !strings.Contains(compactLandingHTML(body), want) {
			t.Fatalf("landing HTML missing enum row %q:\n%s", want, body)
		}
	}
}

func TestLandingTemplateJSONIslandUsesVisibilityEnum(t *testing.T) {
	store := newLandingTestStore(t,
		landingSeed{name: "atlas", visibility: sitesdomain.Public, createdAt: "2026-07-20T10:00:00Z"},
		landingSeed{name: "quiet", visibility: sitesdomain.Unlisted, createdAt: "2026-07-20T12:00:00Z"},
		landingSeed{name: "vault", visibility: sitesdomain.Private, createdAt: "2026-07-20T11:00:00Z"},
	)
	rec := httptest.NewRecorder()
	landingHandler(store, loadWWW(t), "sites", "phase37", "https://suite.test/").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	// R-HLBT-FUJB
	rows := parseLandingIsland(t, rec.Body.String())
	if len(rows) != 3 {
		t.Fatalf("island row count = %d, want 3: %#v", len(rows), rows)
	}
	wantVisibility := map[string]string{"atlas": "public", "quiet": "unlisted", "vault": "private"}
	for _, row := range rows {
		if _, exists := row["public"]; exists {
			t.Fatalf("island row retains retired public key: %#v", row)
		}
		for _, key := range []string{"slug", "url", "visibility", "createdBy", "createdAt", "createdAtSort"} {
			if _, exists := row[key]; !exists {
				t.Fatalf("island row missing %q: %#v", key, row)
			}
		}
		if len(row) != 6 {
			t.Fatalf("island row keys = %#v, want exactly six contract keys", row)
		}
		var slug, visibility, createdAtSort string
		if err := json.Unmarshal(row["slug"], &slug); err != nil {
			t.Fatalf("decode slug: %v", err)
		}
		if err := json.Unmarshal(row["visibility"], &visibility); err != nil {
			t.Fatalf("decode visibility: %v", err)
		}
		if err := json.Unmarshal(row["createdAtSort"], &createdAtSort); err != nil {
			t.Fatalf("decode createdAtSort: %v", err)
		}
		if visibility != wantVisibility[slug] {
			t.Fatalf("visibility for %q = %q, want %q", slug, visibility, wantVisibility[slug])
		}
		if _, err := time.Parse(time.RFC3339, createdAtSort); err != nil {
			t.Fatalf("createdAtSort for %q is not RFC3339: %q: %v", slug, createdAtSort, err)
		}
	}

	empty := httptest.NewRecorder()
	landingHandler(newLandingTestStore(t), loadWWW(t), "sites", "phase37", "https://suite.test/").ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := parseLandingIsland(t, empty.Body.String()); len(got) != 0 || got == nil {
		t.Fatalf("empty island = %#v, want parsed []", got)
	}
}

func TestLandingHandlerMapsUnlistedSiteToPublicURL(t *testing.T) {
	store := newLandingTestStore(t, landingSeed{name: "Z", visibility: sitesdomain.Unlisted})
	baseURL := "https://suite.test/srv/sites/"
	rec := httptest.NewRecorder()
	landingHandler(store, loadWWW(t), "sites", "phase37", baseURL).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	// R-HMJP-TMA0
	wantURL := baseURL + "public/Z/"
	if !strings.Contains(rec.Body.String(), `<a href="`+wantURL+`">Z</a>`) {
		t.Fatalf("unlisted site anchor does not use public URL %q:\n%s", wantURL, rec.Body.String())
	}
	rows := parseLandingIsland(t, rec.Body.String())
	if len(rows) != 1 {
		t.Fatalf("island rows = %#v, want one unlisted site", rows)
	}
	var islandURL, visibility string
	if err := json.Unmarshal(rows[0]["url"], &islandURL); err != nil {
		t.Fatalf("decode island URL: %v", err)
	}
	if err := json.Unmarshal(rows[0]["visibility"], &visibility); err != nil {
		t.Fatalf("decode island visibility: %v", err)
	}
	if islandURL != wantURL || visibility != "unlisted" {
		t.Fatalf("island URL/visibility = %q/%q, want %q/unlisted", islandURL, visibility, wantURL)
	}
}

func parseLandingIsland(t *testing.T, body string) []map[string]json.RawMessage {
	t.Helper()
	match := regexp.MustCompile(`(?s)<script type="application/json" id="sites-data">(.*?)</script>`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("landing HTML missing sites data island:\n%s", body)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(match[1]), &rows); err != nil {
		t.Fatalf("parse sites data island: %v\n%s", err, match[1])
	}
	return rows
}

func compactLandingHTML(value string) string {
	return regexp.MustCompile(`>\s+<`).ReplaceAllString(value, "><")
}
