package main

import (
	"artifacts/internal/db"
	"bytes"
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// R-53EO-JVJ9
func TestLandingRendersEveryArtifactAndExplicitEmptyState(t *testing.T) {
	router, store := assembledWebRouter(t)
	seedArtifact(t, store, db.CreateArtifactParams{ID: "private-id", OwnerID: "hidden-owner", OwnerEmail: "private@example.com", Filename: "private notes.txt", Description: "notes", Visibility: "private", Size: 1200, ContentHash: "private-hash", CreatedAt: testTime})
	seedArtifact(t, store, db.CreateArtifactParams{ID: "public-id", OwnerID: "hidden-owner", OwnerEmail: "public@example.com", Filename: "public.pdf", Description: "report", Visibility: "public", Size: 2048, ContentHash: "public-hash", CreatedAt: testTime.Add(time.Hour)})
	for range 3 {
		if changed, err := store.IncrementDownloadCount(context.Background(), "private-id"); err != nil || !changed {
			t.Fatalf("increment private download count = %v, %v", changed, err)
		}
	}

	body := renderLanding(t, router, nil)
	if rows := bytes.Count(body, []byte(`<tr data-artifact-id=`)); rows != 2 {
		t.Fatalf("rendered rows = %d, want 2\n%s", rows, body)
	}
	for _, want := range []string{
		`href="/srv/artifacts/p/private-id/private%20notes.txt"`,
		`href="/srv/artifacts/f/public-id/public.pdf"`,
		`private@example.com`, `public@example.com`, `>private<`, `>public<`,
		`data-label="Downloads">3</td>`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("landing page lacks %q\n%s", want, body)
		}
	}
	if bytes.Contains(body, []byte("hidden-owner")) {
		t.Fatalf("landing page exposed owner_id:\n%s", body)
	}

	emptyRouter, _ := assembledWebRouter(t)
	empty := renderLanding(t, emptyRouter, nil)
	if bytes.Count(empty, []byte(`<tr data-artifact-id=`)) != 0 || !bytes.Contains(empty, []byte(`<div class="empty-state">No artifacts stored.</div>`)) {
		t.Fatalf("empty inventory lacks explicit empty state or contains rows:\n%s", empty)
	}
}

// R-54MK-XN9Y
func TestLandingIslandMatchesRowsAndUsesMachineValues(t *testing.T) {
	router, store := assembledWebRouter(t)
	seedArtifact(t, store, db.CreateArtifactParams{ID: "artifact-one", OwnerID: "owner", OwnerEmail: "owner@example.com", Filename: "one.bin", Description: "first", Visibility: "public", Size: 1234567, ContentHash: "hash", CreatedAt: testTime})
	body := renderLanding(t, router, nil)
	rows := decodeIsland(t, body)
	if len(rows) != 1 {
		t.Fatalf("island rows = %#v, want one", rows)
	}
	row := rows[0]
	for _, field := range []string{"id", "filename", "description", "url", "visibility", "sizeBytes", "createdAt", "createdAtSort", "downloads"} {
		if _, exists := row[field]; !exists {
			t.Errorf("island row lacks declared field %q: %#v", field, row)
		}
	}
	if _, numeric := row["sizeBytes"].(float64); !numeric {
		t.Fatalf("sizeBytes is %T, want JSON number", row["sizeBytes"])
	}
	if _, err := time.Parse(time.RFC3339, row["createdAtSort"].(string)); err != nil {
		t.Fatalf("createdAtSort = %q: %v", row["createdAtSort"], err)
	}
	href := regexp.MustCompile(`<tr data-artifact-id="artifact-one">(?s:.*?)<a href="([^"]+)"`).FindSubmatch(body)
	if len(href) != 2 || html.UnescapeString(string(href[1])) != row["url"] {
		t.Fatalf("row href = %q, island URL = %v", href, row["url"])
	}
	emptyRouter, _ := assembledWebRouter(t)
	if empty := decodeIsland(t, renderLanding(t, emptyRouter, nil)); len(empty) != 0 {
		t.Fatalf("empty island = %#v, want []", empty)
	}
}

func TestLandingEscapesUserMarkupAndKeepsIslandValid(t *testing.T) {
	router, store := assembledWebRouter(t)
	filename := `<script id="injected">alert(1)</script>.txt`
	description := `<script>alert("description")</script>`
	seedArtifact(t, store, db.CreateArtifactParams{ID: "hostile", OwnerID: "owner", OwnerEmail: "owner@example.com", Filename: filename, Description: description, Visibility: "private", Size: 1, ContentHash: "hash", CreatedAt: testTime})
	body := renderLanding(t, router, nil)
	if bytes.Contains(body, []byte(filename)) || bytes.Contains(body, []byte(description)) {
		t.Fatalf("page contains executable user markup:\n%s", body)
	}
	for _, escaped := range []string{`&lt;script id=&#34;injected&#34;&gt;alert(1)&lt;/script&gt;.txt`, `&lt;script&gt;alert(&#34;description&#34;)&lt;/script&gt;`} {
		if !bytes.Contains(body, []byte(escaped)) {
			t.Errorf("page lacks escaped text %q\n%s", escaped, body)
		}
	}
	rows := decodeIsland(t, body)
	if len(rows) != 1 || rows[0]["filename"] != filename || rows[0]["description"] != description {
		t.Fatalf("island did not safely round-trip hostile text: %#v", rows)
	}
}

// R-OSZJ-I2D8
func TestLandingConformsToCanonicalShellAndChassisServesAssets(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "..", "..", "cron", "share", "www", "landing.html"))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(wwwRoot(t), "landing.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := normalizeArtifactsTemplate(string(committed)), normalizeCronTemplate(string(canonical)); got != want {
		t.Fatalf("landing template differs from cron outside named slots\n--- artifacts ---\n%s\n--- cron ---\n%s", got, want)
	}

	router, _ := assembledWebRouter(t)
	for _, asset := range []struct{ name, contentType string }{
		{name: "tokens.css", contentType: "text/css; charset=utf-8"},
		{name: "fonts/space-grotesk.woff2", contentType: "font/woff2"},
		{name: "fonts/ibm-plex-sans.woff2", contentType: "font/woff2"},
		{name: "fonts/ibm-plex-mono-400.woff2", contentType: "font/woff2"},
		{name: "fonts/ibm-plex-mono-500.woff2", contentType: "font/woff2"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/static/"+asset.name, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != asset.contentType || response.Body.Len() == 0 {
			t.Errorf("chassis asset %s status=%d content-type=%q bytes=%d", asset.name, response.Code, response.Header().Get("Content-Type"), response.Body.Len())
		}
	}
}

// R-OU7F-VU3X
func TestAssembledRouterRendersSeededArtifactFromDiskSite(t *testing.T) {
	router, store := assembledWebRouter(t)
	seedArtifact(t, store, db.CreateArtifactParams{ID: "wired", OwnerID: "owner", OwnerEmail: "owner@example.com", Filename: "composition-proof.txt", Description: "assembled route", Visibility: "private", Size: 42, ContentHash: "hash", CreatedAt: testTime})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`>composition-proof.txt</a>`)) {
		t.Fatalf("assembled GET / status=%d lacks seeded row:\n%s", response.Code, response.Body.String())
	}
}

// R-5AQ2-UHZF
func TestLandingNeedsNoIdentityHeadersAndShowsCommittedVersion(t *testing.T) {
	versionBody, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBody))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	router, _ := assembledWebRouter(t)
	body := renderLanding(t, router, request)
	if !bytes.Contains(body, []byte(version)) {
		t.Fatalf("landing page does not contain committed version %q:\n%s", version, body)
	}
}

var testTime = time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC)

func seedArtifact(t *testing.T, store *db.Store, params db.CreateArtifactParams) {
	t.Helper()
	if _, err := store.CreateArtifact(context.Background(), params); err != nil {
		t.Fatal(err)
	}
}

func renderLanding(t *testing.T, router http.Handler, request *http.Request) []byte {
	t.Helper()
	if request == nil {
		request = httptest.NewRequest(http.MethodGet, "/", nil)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("landing status = %d: %s", response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

func decodeIsland(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	match := regexp.MustCompile(`<script type="application/json" id="artifacts-data">(?s:(.*?))</script>`).FindSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("landing page has no JSON island:\n%s", body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(match[1], &rows); err != nil {
		t.Fatalf("parse island: %v\n%s", err, match[1])
	}
	if rows == nil {
		t.Fatal("island encoded null, want []")
	}
	return rows
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func normalizeCronTemplate(value string) string {
	value = regexp.MustCompile(`<title>.*</title>`).ReplaceAllString(value, `<title>[[title]]</title>`)
	value = regexp.MustCompile(`      <div class="eyebrow">.*</div>`).ReplaceAllString(value, `      <div class="eyebrow">[[eyebrow]]</div>`)
	value = regexp.MustCompile(`      <p>.*</p>`).ReplaceAllString(value, `      <p>[[description]]</p>`)
	return regexp.MustCompile(`(?s)    <dl aria-label="Service details">.*?    </dl>`).ReplaceAllString(value, `    [[inventory]]`)
}

func normalizeArtifactsTemplate(value string) string {
	value = regexp.MustCompile(`<title>.*</title>`).ReplaceAllString(value, `<title>[[title]]</title>`)
	value = regexp.MustCompile(`      <div class="eyebrow">.*</div>`).ReplaceAllString(value, `      <div class="eyebrow">[[eyebrow]]</div>`)
	value = regexp.MustCompile(`      <p>.*</p>`).ReplaceAllString(value, `      <p>[[description]]</p>`)
	value = regexp.MustCompile(`\n      <div class="version">.*</div>`).ReplaceAllString(value, ``)
	return regexp.MustCompile(`(?s)    <!-- inventory:start -->.*?    <!-- inventory:end -->`).ReplaceAllString(value, `    [[inventory]]`)
}
