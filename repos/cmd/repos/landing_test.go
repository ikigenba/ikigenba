package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"repos/internal/repos"
	"strings"
	"testing"
	"time"

	appdb "appkit/db"
	reposdb "repos/internal/db"
)

func TestLandingHeadingAndCopyExplainTheGitCustodian(t *testing.T) {
	// R-RQAW-IFLQ
	// R-RRIS-W7CF
	// R-S4XP-3OI2
	fixture := newLandingFixture(t)
	fixture.add(t, "code", "demo", "owner-a", 0, false)
	body := fixture.render(t)

	heading := `<h1 id="page-title">repos <span class="version">v9.8.7-test</span></h1>`
	for _, want := range []string{
		heading,
		`<div class="eyebrow">Git custodian</div>`,
		`<p>Holds one git repository per versioned artifact and answers ordinary git clients through the suite gateway.</p>`,
		`<a class="home" href="/">Home</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing is missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"POST /mcp", "<dl", "<dt>Service", "<dt>Version", "<dt>API", "Repository session runner", "Repos clones repositories"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("landing retained stale canonical content %q:\n%s", forbidden, body)
		}
	}
}

func TestLandingControlsTableAndPagerHaveSemanticOrderAndStyles(t *testing.T) {
	// R-RSQP-9Z34
	// R-RTYL-NQTT
	// R-RV6I-1IKI
	// R-RWEE-FAB7
	fixture := newLandingFixture(t)
	fixture.add(t, "code", "demo", "owner-a", 0, false)
	body := fixture.render(t)

	lead := strings.Index(body, "Holds one git repository")
	controls := strings.Index(body, `<div class="controls js-only" hidden>`)
	table := strings.Index(body, `<table class="repo-table">`)
	pager := strings.Index(body, `<div class="pager js-only" hidden>`)
	if lead < 0 || controls <= lead || table <= controls || pager <= table {
		t.Fatalf("document order lead=%d controls=%d table=%d pager=%d", lead, controls, table, pager)
	}
	for _, want := range []string{
		`<input type="search" class="input js-only" id="repo-search" aria-label="Search repositories" placeholder="Search repositories" hidden>`,
		`<button type="button" class="btn btn-ghost js-only" id="repo-clear" hidden>Clear</button>`,
		`<p class="no-match js-only" hidden>No repositories match.</p>`,
		`<div class="pager js-only" hidden>`,
		`<span id="pager-label">Page 1 of 1</span>`,
		`<button type="button" class="btn btn-secondary" id="pager-prev">Prev</button>`,
		`<button type="button" class="btn btn-secondary" id="pager-next">Next</button>`,
		`<th scope="col" data-sort-key="key">Name</th><th scope="col">Sha</th><th scope="col"><span class="sr-only">Copy</span></th>`,
		`[hidden] { display: none !important; }`,
		`.repo-table th[data-sort-key] { cursor: pointer; }`,
		`.repo-table th[aria-sort="ascending"]::after { content: " ▲"; }`,
		`.repo-table th[aria-sort="descending"]::after { content: " ▼"; }`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing is missing control or style %q", want)
		}
	}
	markup := body[strings.Index(body, "</style>")+len("</style>"):]
	if strings.Contains(markup, "aria-sort=") {
		t.Errorf("server markup carries an active aria-sort state:\n%s", markup)
	}
	for _, class := range []string{"input", "js-only", "btn", "btn-ghost", "btn-secondary", "copy-btn", "copy-label", "sr-only"} {
		if !strings.Contains(body, "."+class) {
			t.Errorf("control class %q has no stylesheet rule", class)
		}
	}
	copyStart := strings.Index(body, `<button type="button" class="copy-btn js-only"`)
	if copyStart < 0 {
		t.Fatal("landing has no copy button")
	}
	copyEnd := strings.Index(body[copyStart:], "</button>")
	if copyEnd < 0 || !strings.Contains(body[copyStart:copyStart+copyEnd], " hidden") {
		t.Errorf("copy button is not hidden in server markup")
	}
}

func TestLandingRowsUseRealGitTipsAndShareClonePathsWithIsland(t *testing.T) {
	// R-RXMA-T21W
	// R-RYU7-6TSL
	// R-S023-KLJA
	// R-S19Z-YD9Z
	fixture := newLandingFixture(t)
	codeSHA := fixture.add(t, "code", "alpha", "owner-a", 1, false)
	sitesSHA := fixture.add(t, "sites", "beta", "owner-b", 2, false)
	fixture.add(t, "prompts", "empty", "owner-a", 0, false)
	body := fixture.render(t)
	rows := decodeLandingIsland(t, body)
	if len(rows) != 3 {
		t.Fatalf("island rows = %#v, want three", rows)
	}
	want := []repoRow{
		{Kind: "code", Name: "alpha", Key: "code/alpha", Sha: codeSHA[:7], ClonePath: "/srv/repos/git/code/alpha.git"},
		{Kind: "prompts", Name: "empty", Key: "prompts/empty", Sha: "", ClonePath: "/srv/repos/git/prompts/empty.git"},
		{Kind: "sites", Name: "beta", Key: "sites/beta", Sha: sitesSHA[:7], ClonePath: "/srv/repos/git/sites/beta.git"},
	}
	for index, row := range rows {
		if row != want[index] {
			t.Errorf("island row %d = %#v, want %#v", index, row, want[index])
		}
		for _, visible := range []string{`<td data-label="Name">` + row.Key + `</td>`, `data-clone-path="` + row.ClonePath + `" hidden`} {
			if !strings.Contains(body, visible) {
				t.Errorf("visible row for %s is missing %q", row.Key, visible)
			}
		}
	}
	for _, wantSHA := range []string{codeSHA[:7], sitesSHA[:7]} {
		if !strings.Contains(body, `<td data-label="Sha" class="sha">`+wantSHA+`</td>`) {
			t.Errorf("table is missing abbreviated real git tip %s", wantSHA)
		}
	}
	if !strings.Contains(body, `<td data-label="Sha" class="sha">—</td>`) {
		t.Errorf("empty repository does not render an em-dash:\n%s", body)
	}
}

func TestLandingNeverRendersArchivedRepositories(t *testing.T) {
	// R-S2HW-C50O
	fixture := newLandingFixture(t)
	fixture.add(t, "code", "live", "owner-a", 0, false)
	fixture.add(t, "code", "buried", "owner-a", 0, true)
	body := fixture.render(t)
	if strings.Contains(body, "code/buried") || strings.Contains(body, "buried.git") {
		t.Fatalf("archived repository leaked into landing:\n%s", body)
	}
	if !strings.Contains(body, "code/live") {
		t.Fatalf("live repository is absent:\n%s", body)
	}
}

func TestLandingEmptyRepositoryListStillRendersHeadingAndJSONArray(t *testing.T) {
	// R-S65L-HG8R
	fixture := newLandingFixture(t)
	body := fixture.render(t)
	if !strings.Contains(body, `<h1 id="page-title">repos <span class="version">v9.8.7-test</span></h1>`) {
		t.Fatalf("empty landing lost heading:\n%s", body)
	}
	rows := decodeLandingIsland(t, body)
	if rows == nil || len(rows) != 0 {
		t.Fatalf("empty island = %#v, want non-nil empty array", rows)
	}
}

type landingFixture struct {
	conn    *sql.DB
	store   *repos.Store
	custody *repos.Custody
}

func newLandingFixture(t *testing.T) *landingFixture {
	t.Helper()
	migrations, err := reposdb.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	custody, err := repos.NewCustody(state, repos.NewCommandGit("git", state), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &landingFixture{conn: conn, store: repos.NewStore(conn), custody: custody}
}

func (fixture *landingFixture) add(t *testing.T, kind, name, owner string, commits int, archived bool) string {
	t.Helper()
	ctx := context.Background()
	if err := fixture.custody.Init(ctx, kind, name); err != nil {
		t.Fatal(err)
	}
	tx, err := fixture.conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := fixture.store.InsertRepository(ctx, tx, repos.Repository{Kind: kind, Name: name, OwnerID: owner, OwnerEmail: owner + "@example.test", DefaultBranch: "main", CreatedAt: created}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if archived {
		if err := fixture.store.ArchiveRepository(ctx, tx, kind, name, created.Add(time.Hour), kind+"/"+name+".git"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if commits == 0 {
		return ""
	}
	bare, err := fixture.custody.Path(kind, name)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "work")
	runLandingGit(t, "", "clone", "file://"+bare, work)
	for index := 0; index < commits; index++ {
		if err := os.WriteFile(filepath.Join(work, "history.txt"), []byte(strings.Repeat("x", index+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		runLandingGit(t, work, "add", "history.txt")
		runLandingGit(t, work, "commit", "-m", "commit")
	}
	runLandingGit(t, work, "push", "origin", "main")
	return strings.TrimSpace(runLandingGit(t, work, "rev-parse", "HEAD"))
}

func (fixture *landingFixture) render(t *testing.T) string {
	t.Helper()
	handler := landingHandler(loadReposWWW(t), fixture.store, fixture.custody, "repos", "v9.8.7-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET / status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func decodeLandingIsland(t *testing.T, body string) []repoRow {
	t.Helper()
	const startTag = `<script type="application/json" id="repos-data">`
	start := strings.Index(body, startTag)
	if start < 0 {
		t.Fatalf("landing has no repository island:\n%s", body)
	}
	start += len(startTag)
	end := strings.Index(body[start:], "</script>")
	if end < 0 {
		t.Fatalf("repository island is not closed:\n%s", body)
	}
	var rows []repoRow
	if err := json.Unmarshal([]byte(html.UnescapeString(body[start:start+end])), &rows); err != nil {
		t.Fatalf("decode repository island: %v", err)
	}
	return rows
}

func runLandingGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Repos Landing Test", "GIT_AUTHOR_EMAIL=repos@example.test",
		"GIT_COMMITTER_NAME=Repos Landing Test", "GIT_COMMITTER_EMAIL=repos@example.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
