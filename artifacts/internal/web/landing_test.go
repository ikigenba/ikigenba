package web

import (
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

	appkitdb "appkit/db"
	"artifacts/internal/artifacts"
	"artifacts/internal/db"
)

// R-53EO-JVJ9
func TestLandingRendersEveryArtifactAndExplicitEmptyState(t *testing.T) {
	svc := testService(t)
	seedArtifact(t, svc.Store, db.CreateArtifactParams{ID: "private-id", OwnerID: "hidden-owner", OwnerEmail: "private@example.com", Filename: "private notes.txt", Description: "notes", Visibility: "private", Size: 1200, ContentHash: "private-hash", CreatedAt: testTime})
	seedArtifact(t, svc.Store, db.CreateArtifactParams{ID: "public-id", OwnerID: "hidden-owner", OwnerEmail: "public@example.com", Filename: "public.pdf", Description: "report", Visibility: "public", Size: 2048, ContentHash: "public-hash", CreatedAt: testTime.Add(time.Hour)})
	for range 3 {
		if changed, err := svc.Store.IncrementDownloadCount(context.Background(), "private-id"); err != nil || !changed {
			t.Fatalf("increment private download count = %v, %v", changed, err)
		}
	}

	body := renderLanding(t, svc, nil)
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

	empty := renderLanding(t, testService(t), nil)
	if bytes.Count(empty, []byte(`<tr data-artifact-id=`)) != 0 || !bytes.Contains(empty, []byte(`<div class="empty-state">No artifacts stored.</div>`)) {
		t.Fatalf("empty inventory lacks explicit empty state or contains rows:\n%s", empty)
	}
}

// R-54MK-XN9Y
func TestLandingIslandMatchesRowsAndUsesMachineValues(t *testing.T) {
	svc := testService(t)
	seedArtifact(t, svc.Store, db.CreateArtifactParams{ID: "artifact-one", OwnerID: "owner", OwnerEmail: "owner@example.com", Filename: "one.bin", Description: "first", Visibility: "public", Size: 1234567, ContentHash: "hash", CreatedAt: testTime})
	body := renderLanding(t, svc, nil)
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
	if empty := decodeIsland(t, renderLanding(t, testService(t), nil)); len(empty) != 0 {
		t.Fatalf("empty island = %#v, want []", empty)
	}
}

func TestLandingEscapesUserMarkupAndKeepsIslandValid(t *testing.T) {
	svc := testService(t)
	filename := `<script id="injected">alert(1)</script>.txt`
	description := `<script>alert("description")</script>`
	seedArtifact(t, svc.Store, db.CreateArtifactParams{ID: "hostile", OwnerID: "owner", OwnerEmail: "owner@example.com", Filename: filename, Description: description, Visibility: "private", Size: 1, ContentHash: "hash", CreatedAt: testTime})
	body := renderLanding(t, svc, nil)
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

// R-59I6-GQ8Q
func TestLandingConformsToCanonicalShellAndServesVerbatimAssets(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "..", "..", "cron", "share", "www", "landing.html"))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("landing.html")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := normalizeArtifactsTemplate(string(committed)), normalizeCronTemplate(string(canonical)); got != want {
		t.Fatalf("landing template differs from cron outside named slots\n--- artifacts ---\n%s\n--- cron ---\n%s", got, want)
	}

	for _, name := range []string{"tokens.css", "fonts/space-grotesk.woff2", "fonts/ibm-plex-sans.woff2", "fonts/ibm-plex-mono-400.woff2", "fonts/ibm-plex-mono-500.woff2"} {
		want, err := os.ReadFile(filepath.Join("..", "..", "..", "github", "internal", "web", "static", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/static/"+name, nil)
		response := httptest.NewRecorder()
		StaticHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
			t.Errorf("embedded asset %s status=%d equal=%v", name, response.Code, bytes.Equal(response.Body.Bytes(), want))
		}
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
	body := renderLanding(t, testService(t), request)
	if !bytes.Contains(body, []byte(version)) {
		t.Fatalf("landing page does not contain committed version %q:\n%s", version, body)
	}
}

var testTime = time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC)

func testService(t *testing.T) *artifacts.Service {
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
	return &artifacts.Service{Store: db.NewStore(conn, func() string { return "generated" })}
}

func seedArtifact(t *testing.T, store *db.Store, params db.CreateArtifactParams) {
	t.Helper()
	if _, err := store.CreateArtifact(context.Background(), params); err != nil {
		t.Fatal(err)
	}
}

func renderLanding(t *testing.T, svc *artifacts.Service, request *http.Request) []byte {
	t.Helper()
	if request == nil {
		request = httptest.NewRequest(http.MethodGet, "/", nil)
	}
	response := httptest.NewRecorder()
	LandingHandler(svc, "artifacts", strings.TrimSpace(string(mustRead(t, filepath.Join("..", "..", "VERSION"))))).ServeHTTP(response, request)
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
