package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const gitDoorActor = "owner@example.test"

type gitDoorFixture struct {
	service *Service
	custody *Custody
	db      *sql.DB
	bare    string
	tip     string
	server  *httptest.Server
}

func newGitDoorFixture(t *testing.T) *gitDoorFixture {
	t.Helper()
	conn, store := newTestStore(t)
	custody, _ := newTestCustody(t)
	if err := custody.Init(context.Background(), "code", "demo"); err != nil {
		t.Fatal(err)
	}
	bare, _ := custody.Path("code", "demo")
	tip := seedCommits(t, bare, 3)
	service := serviceWithOutbox(t, conn, store, custody)
	door := GitDoorHandler(service)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Owner-Id", "owner-1")
		request.Header.Set("X-Owner-Email", gitDoorActor)
		door.ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)
	return &gitDoorFixture{service: service, custody: custody, db: conn, bare: bare, tip: tip, server: server}
}

func (f *gitDoorFixture) remote() string { return f.server.URL + "/git/code/demo.git" }

func (f *gitDoorFixture) clone(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", f.remote(), dir)
	return dir
}

func TestGitDoorClonesCompleteHistoryWithRealGit(t *testing.T) {
	// R-JZFR-1IPP
	f := newGitDoorFixture(t)
	clone := f.clone(t)
	if got := strings.TrimSpace(runGit(t, clone, "rev-list", "--count", "HEAD")); got != "3" {
		t.Fatalf("clone commit count = %s, want 3", got)
	}
	if got := strings.TrimSpace(runGit(t, clone, "rev-parse", "HEAD")); got != f.tip {
		t.Fatalf("clone tip = %s, want origin main %s", got, f.tip)
	}
}

func TestGitDoorPushCreatesFetchableBranch(t *testing.T) {
	// R-K0NN-FAGE
	f := newGitDoorFixture(t)
	clone := f.clone(t)
	sha := commitFile(t, clone, "work.txt", "work", "work commit")
	runGit(t, clone, "push", "origin", "HEAD:refs/heads/ikigenba/work")
	if got := strings.TrimSpace(runGit(t, f.bare, "rev-parse", "refs/heads/ikigenba/work")); got != sha {
		t.Fatalf("bare work ref = %s, want %s", got, sha)
	}
	fresh := f.clone(t)
	runGit(t, fresh, "fetch", "origin", "ikigenba/work")
	if got := strings.TrimSpace(runGit(t, fresh, "rev-parse", "FETCH_HEAD")); got != sha {
		t.Fatalf("fresh fetch = %s, want %s", got, sha)
	}
}

func TestGitDoorOwnerRejectsForcePushToMain(t *testing.T) {
	// R-K1VJ-T273
	f := newGitDoorFixture(t)
	clone := f.clone(t)
	runGit(t, clone, "checkout", "--detach", "HEAD~1")
	divergent := commitFile(t, clone, "divergent.txt", "divergent", "divergent")
	output, err := gitCommand(clone, "push", "--force", "origin", divergent+":refs/heads/main")
	if err == nil || !strings.Contains(output, "force-push to main is rejected") {
		t.Fatalf("force push error=%v output=%q, want hook rejection", err, output)
	}
	if got := strings.TrimSpace(runGit(t, f.bare, "rev-parse", "refs/heads/main")); got != f.tip {
		t.Fatalf("main after rejected force push = %s, want %s", got, f.tip)
	}
}

func TestGitDoorOwnerAllowsFastForwardMain(t *testing.T) {
	// R-K33G-6TXS
	f := newGitDoorFixture(t)
	clone := f.clone(t)
	sha := commitFile(t, clone, "forward.txt", "forward", "forward")
	runGit(t, clone, "push", "origin", "HEAD:main")
	if got := strings.TrimSpace(runGit(t, f.bare, "rev-parse", "main")); got != sha {
		t.Fatalf("main = %s, want fast-forward %s", got, sha)
	}
}

func TestGitDoorPublishesMovedBranchesAndNoRejectedUpdate(t *testing.T) {
	// R-K4BC-KLOH
	f := newGitDoorFixture(t)
	clone := f.clone(t)
	sha := commitFile(t, clone, "two.txt", "two", "two branches")
	runGit(t, clone, "push", "origin", "HEAD:refs/heads/feature/a", "HEAD:refs/heads/feature/b")
	events := readPushEvents(t, f.db)
	if len(events) != 2 {
		t.Fatalf("push events = %#v, want exactly two", events)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Branch < events[j].Branch })
	for i, branch := range []string{"feature/a", "feature/b"} {
		want := PushPayload{Kind: "code", Name: "demo", Branch: branch, SHA: sha, OldSHA: "", Actor: gitDoorActor}
		if events[i] != want {
			t.Errorf("event %d = %#v, want %#v", i, events[i], want)
		}
	}
	if _, err := f.db.Exec(`DELETE FROM outbox`); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, "checkout", "--detach", "HEAD~2")
	divergent := commitFile(t, clone, "refused.txt", "refused", "refused")
	if output, err := gitCommand(clone, "push", "--force", "origin", divergent+":refs/heads/main"); err == nil {
		t.Fatalf("refused push succeeded: %s", output)
	}
	if events := readPushEvents(t, f.db); len(events) != 0 {
		t.Fatalf("rejected push events = %#v, want none", events)
	}
}

func TestGitDoorAllowsForceRewriteOutsideMain(t *testing.T) {
	// R-K5J8-YDF6
	f := newGitDoorFixture(t)
	clone := f.clone(t)
	first := commitFile(t, clone, "first.txt", "first", "first work")
	runGit(t, clone, "push", "origin", "HEAD:refs/heads/ikigenba/work")
	runGit(t, clone, "checkout", "--detach", "HEAD~2")
	divergent := commitFile(t, clone, "rewrite.txt", "rewrite", "rewrite work")
	if divergent == first {
		t.Fatal("divergent commit unexpectedly equals first work commit")
	}
	runGit(t, clone, "push", "--force", "origin", divergent+":refs/heads/ikigenba/work")
	if got := strings.TrimSpace(runGit(t, f.bare, "rev-parse", "refs/heads/ikigenba/work")); got != divergent {
		t.Fatalf("rewritten work ref = %s, want %s", got, divergent)
	}
}

func TestGitDoorAdvertisesAndConcealsUnavailableRepositories(t *testing.T) {
	// R-K6R5-C55V
	f := newGitDoorFixture(t)
	response, err := f.server.Client().Get(f.remote() + "/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-git-upload-pack-advertisement" {
		t.Fatalf("advertisement status=%d type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(string(body), "001e# service=git-upload-pack\n") {
		t.Fatalf("advertisement first pkt-line = %q", body)
	}

	unknownBody := getBodyWithStatus(t, f.server.Client(), f.server.URL+"/git/code/missing.git/info/refs?service=git-upload-pack", http.StatusNotFound)
	invalidBody := getBodyWithStatus(t, f.server.Client(), f.server.URL+"/git/code/../escape.git/info/refs?service=git-upload-pack", http.StatusNotFound)
	if _, err := f.custody.Archive(context.Background(), "code", "demo"); err != nil {
		t.Fatal(err)
	}
	archivedBody := getBodyWithStatus(t, f.server.Client(), f.remote()+"/info/refs?service=git-upload-pack", http.StatusNotFound)
	if unknownBody != invalidBody || unknownBody != archivedBody {
		t.Fatalf("concealment bodies differ: unknown=%q invalid=%q archived=%q", unknownBody, invalidBody, archivedBody)
	}
}

func TestGitDoorNeverServesAnonymousRequests(t *testing.T) {
	// R-K7Z1-PWWK
	f := newGitDoorFixture(t)
	doorServer := httptest.NewServer(GitDoorHandler(f.service))
	defer doorServer.Close()

	request, _ := http.NewRequest(http.MethodGet, doorServer.URL+"/git/code/demo.git/info/refs?service=git-upload-pack", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err := doorServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("crossed unidentified status = %d, want 401", response.StatusCode)
	}

	response, err = doorServer.Client().Get(doorServer.URL + "/git/code/demo.git/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") != gitBasicChallenge {
		t.Fatalf("loopback status=%d challenge=%q", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}

	clone := filepath.Join(t.TempDir(), "anonymous")
	output, err := gitCommand("", "clone", doorServer.URL+"/git/code/demo.git", clone)
	if err == nil {
		t.Fatalf("anonymous clone succeeded: %s", output)
	}
}

type runGitDoorFixture struct {
	service *Service
	store   *Store
	custody *Custody
	clock   *sequenceClock
	server  *httptest.Server
	tip     string
	token   string
}

func newRunGitDoorFixture(t *testing.T) *runGitDoorFixture {
	t.Helper()
	ctx := context.Background()
	conn, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 18, 0, 0, 0, time.UTC)}
	custody := testCustodyWithClock(t, clock)
	for _, name := range []string{"demo", "other"} {
		if err := custody.Init(ctx, "code", name); err != nil {
			t.Fatal(err)
		}
		inTx(t, conn, func(tx *sql.Tx) error {
			return store.InsertRepository(ctx, tx, Repository{Kind: "code", Name: name, OwnerID: "owner", OwnerEmail: "owner@example.test", DefaultBranch: "main", CreatedAt: clock.now})
		})
	}
	demo, _ := custody.Path("code", "demo")
	tip := seedCommits(t, demo, 3)
	other, _ := custody.Path("code", "other")
	seedCommits(t, other, 1)
	service := serviceWithOutbox(t, conn, store, custody)
	minted, response := mintRunToken(t, service, "code", "demo")
	if response.Code != http.StatusOK {
		t.Fatalf("mint status=%d body=%q", response.Code, response.Body.String())
	}
	server := httptest.NewServer(GitDoorHandler(service))
	t.Cleanup(server.Close)
	return &runGitDoorFixture{service: service, store: store, custody: custody, clock: clock, server: server, tip: tip, token: minted.Token}
}

func (f *runGitDoorFixture) remote(t *testing.T, name string) string {
	t.Helper()
	parsed, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword("run", f.token)
	parsed.Path = "/git/code/" + name + ".git"
	return parsed.String()
}

func TestRunTokenClonesFullHistoryAndIsForbiddenFromOtherRepository(t *testing.T) {
	// R-KAEU-HGDY
	f := newRunGitDoorFixture(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", f.remote(t, "demo"), clone)
	if got := strings.TrimSpace(runGit(t, clone, "rev-list", "--count", "HEAD")); got != "3" {
		t.Fatalf("run-token clone commit count=%s, want 3", got)
	}
	if got := strings.TrimSpace(runGit(t, clone, "rev-parse", "HEAD")); got != f.tip {
		t.Fatalf("run-token clone tip=%s, want %s", got, f.tip)
	}

	wrongClone := filepath.Join(t.TempDir(), "wrong-clone")
	output, err := gitCommand("", "clone", f.remote(t, "other"), wrongClone)
	if err == nil || !strings.Contains(output, "403") {
		t.Fatalf("cross-repository clone error=%v output=%q, want 403", err, output)
	}
	if _, statErr := os.Stat(filepath.Join(wrongClone, ".git")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cross-repository clone created a git checkout: %v", statErr)
	}
}

func TestRunTokenExpiresAtValidationWithoutSweep(t *testing.T) {
	// R-KBMQ-V84N
	f := newRunGitDoorFixture(t)
	f.clock.now = f.clock.now.Add(time.Hour + time.Nanosecond)
	request, err := http.NewRequest(http.MethodGet, f.remote(t, "demo")+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") != gitBasicChallenge {
		t.Fatalf("expired token status=%d challenge=%q, want 401 Basic", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}
}

func TestRunTokenPushesWorkBranchButCannotFastForwardMain(t *testing.T) {
	// R-KCUN-8ZVC
	f := newRunGitDoorFixture(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", f.remote(t, "demo"), clone)
	sha := commitFile(t, clone, "work.txt", "work", "run work")
	runGit(t, clone, "push", "origin", "HEAD:refs/heads/ikigenba/work")
	assertRef(t, f.custody, "refs/heads/ikigenba/work", sha)

	output, err := gitCommand(clone, "push", "origin", "HEAD:main")
	if err == nil || !strings.Contains(output, "repos: this token may not push to main") {
		t.Fatalf("run main push error=%v output=%q, want hook rejection", err, output)
	}
	assertRef(t, f.custody, "refs/heads/main", f.tip)
}

func commitFile(t *testing.T, dir, name, contents, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", name)
	runGit(t, dir, "commit", "-m", message)
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
}

func gitCommand(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Repos Test", "GIT_AUTHOR_EMAIL=repos@example.test",
		"GIT_COMMITTER_NAME=Repos Test", "GIT_COMMITTER_EMAIL=repos@example.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	return string(output), err
}

func readPushEvents(t *testing.T, db *sql.DB) []PushPayload {
	t.Helper()
	rows, err := db.Query(`SELECT payload FROM outbox WHERE kind = 'push'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []PushPayload
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var event PushPayload
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func getBodyWithStatus(t *testing.T, client *http.Client, url string, status int) string {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("GET %s status=%d body=%q, want %d", url, response.StatusCode, body, status)
	}
	return fmt.Sprintf("%s", body)
}
