package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

func gitInput(t *testing.T, h *harness, dir, input string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.gitPath, args...)
	cmd.Env = h.env
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, out)
	}
	return strings.TrimSpace(string(out))
}
func commitFile(t *testing.T, h *harness, p, body string) string {
	t.Helper()
	blob := gitInput(t, h, p, body, "hash-object", "-w", "--stdin")
	tree := gitInput(t, h, p, "100644 blob "+blob+"\tindex.html\n", "mktree")
	sha := gitInput(t, h, p, "fixture\n", "commit-tree", tree)
	h.git(t, p, "update-ref", "refs/heads/main", sha)
	return sha
}
func configureCache(t *testing.T, h *harness, s settings.Settings, unpacked func(string, string)) {
	t.Helper()
	g, e := git.Find(filepath.Dir(h.gitPath), func() []string { return h.env })
	if e != nil {
		t.Fatal(e)
	}
	l := limits.New(s, limits.Clock{After: func(d time.Duration) <-chan time.Time { return h.after(d) }})
	c, e := cache.Open(cache.Config{Root: h.cacheRoot, Repos: h.reposRoot, Git: g, Limits: l, Unpacked: unpacked})
	if e != nil {
		t.Fatal(e)
	}
	h.cfg.Cache = c
	h.cfg.Limits = l // Register captured the original config: tests rebuild their server using the shared helper below.
	resetServer(t, h)
}

func assertPublishedObject(t *testing.T, h *harness, r mcp.Result, original store.Site, sha string) {
	t.Helper()
	expected := original
	expected.Commit = sha
	expected.Published = h.now.UTC().Truncate(time.Second)
	actual, e := h.cfg.Store.Find(context.Background(), original.Owner, original.Name)
	if e != nil || !reflect.DeepEqual(expected, actual) {
		t.Fatalf("record %+v want %+v error %v", actual, expected, e)
	}
	created := expected.Created.UTC().Format("2006-01-02T15:04:05Z")
	published := expected.Published.UTC().Format("2006-01-02T15:04:05Z")
	want, e := json.Marshal(tools.Site{ID: expected.ID, Name: expected.Name, Slug: expected.Slug, URL: "https://sites.example.invalid/" + expected.Slug + "/", Repo: expected.Repo, Ref: expected.Ref, Visibility: expected.Visibility, Listed: expected.Listed, Commit: &sha, Created: created, Published: &published})
	if e != nil {
		t.Fatal(e)
	}
	if got := resultObject(t, r); string(got) != string(want) {
		t.Fatalf("object %s want %s", got, want)
	}
}
func outsideSiteSnapshot(t *testing.T, h *harness, id string) map[string]diskEntry {
	t.Helper()
	all := diskSnapshot(t, h.cacheRoot, false)
	for p := range all {
		if p == id || strings.HasPrefix(p, id+string(filepath.Separator)) {
			delete(all, p)
		}
	}
	return all
}

func TestPublishSuccessPruningAndOwnerIndependence(t *testing.T) {
	// R-LQH0-5NL3 R-XJWW-Q6D5 R-NXH2-X0YO R-NBIW-15M6
	h := newHarness(t)
	p := h.repo(t, "rep_0123456789abcdef", "alice")
	first := commitFile(t, h, p, "old")
	s := h.add(t, "alice", "blog", true)
	other := h.add(t, "bob", "other", true)
	h.call(t, "alice", "apex", `{"name":"blog"}`)
	sentinel := filepath.Join(h.cacheRoot, "other-cache", "kept")
	if e := os.MkdirAll(filepath.Dir(sentinel), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(sentinel, []byte("kept bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("kept", filepath.Join(filepath.Dir(sentinel), "link")); e != nil {
		t.Fatal(e)
	}
	outside := outsideSiteSnapshot(t, h, s.ID)
	repos := diskSnapshot(t, h.reposRoot, true)
	start := len(h.capture.Events())
	r := h.call(t, "alice", "publish", `{"name":"blog"}`)
	assertPublishedObject(t, h, r, s, first)
	assertToolTrail(t, h, start, "publish", "ok", map[string]int{"site.published": 1})
	h.now = h.now.Add(time.Hour)
	start = len(h.capture.Events())
	again := h.call(t, "alice", "publish", `{"name":"blog"}`)
	assertPublishedObject(t, h, again, s, first)
	assertToolTrail(t, h, start, "publish", "ok", map[string]int{"site.published": 1})
	if !reflect.DeepEqual(repos, diskSnapshot(t, h.reposRoot, true)) {
		t.Fatal("publish changed repository")
	}
	if !reflect.DeepEqual(outside, outsideSiteSnapshot(t, h, s.ID)) {
		t.Fatal("publish changed other cached trees")
	}
	second := commitFile(t, h, p, "new")
	h.git(t, p, "update-ref", "refs/tags/alternate", second)
	h.git(t, p, "config", "ikigenba.owner", "bob")
	repos = diskSnapshot(t, h.reposRoot, true)
	start = len(h.capture.Events())
	alternate := h.call(t, "alice", "publish", `{"name":"blog","ref":"alternate"}`)
	assertPublishedObject(t, h, alternate, s, second)
	assertToolTrail(t, h, start, "publish", "ok", map[string]int{"site.published": 1})
	x, e := h.cfg.Store.Find(context.Background(), "alice", "blog")
	if e != nil {
		t.Fatal(e)
	}
	expected := s
	expected.Commit = second
	expected.Published = h.now.UTC().Truncate(time.Second)
	if !reflect.DeepEqual(expected, x) {
		t.Fatalf("record %+v expected %+v", x, expected)
	}
	y, e := h.cfg.Store.Find(context.Background(), "bob", "other")
	if e != nil || !reflect.DeepEqual(y, other) {
		t.Fatal("other record changed")
	}
	ap, ok, e := h.cfg.Store.Apex(context.Background())
	if e != nil || !ok || ap.ID != s.ID {
		t.Fatal("apex changed")
	}
	entries, e := os.ReadDir(filepath.Join(h.cacheRoot, s.ID))
	if e != nil || len(entries) != 1 || entries[0].Name() != second {
		t.Fatalf("trees %v %v", entries, e)
	}
	b, e := os.ReadFile(filepath.Join(h.cfg.Cache.Dir(s.ID, second), "index.html"))
	if e != nil || string(b) != "new" {
		t.Fatal("wrong installed tree")
	}
	if !reflect.DeepEqual(repos, diskSnapshot(t, h.reposRoot, true)) {
		t.Fatal("repository changed")
	}
	if !reflect.DeepEqual(outside, outsideSiteSnapshot(t, h, s.ID)) {
		t.Fatal("publish changed outside site entries")
	}
	h.git(t, p, "config", "--unset", "ikigenba.owner")
	resultObject(t, h.call(t, "alice", "publish", `{"name":"blog"}`))
}

func TestPublishRefusalOrderLimitsAndDiskFailure(t *testing.T) {
	// R-9KCD-7BMZ R-LBU7-KEOR R-LO17-E43P R-9FGR-O8O7 R-9HWK-FS5L
	for _, which := range []string{"missing-site", "missing-repo", "no-ref", "malformed-ref", "large", "resolve-timeout", "archive-timeout", "disk", "archive-git"} {
		t.Run(which, func(t *testing.T) {
			h := newHarness(t)
			p := h.repo(t, "rep_0123456789abcdef", "alice")
			commitFile(t, h, p, "body")
			blob := h.git(t, p, "rev-parse", "main:index.html")
			s := h.add(t, "alice", "blog", true)
			arg := `{"name":"blog"}`
			want := ""
			calls := 0
			sconf := settings.Defaults()
			sconf.OperationSeconds = 17
			switch which {
			case "missing-site":
				arg = `{"name":"missing","ref":"..bad"}`
				want = "no site named 'missing'"
			case "missing-repo":
				if e := os.RemoveAll(p); e != nil {
					t.Fatal(e)
				}
				want = "repository '" + s.Repo + "' is unavailable"
			case "no-ref":
				arg = `{"name":"blog","ref":"absent"}`
				want = "no commit for 'absent'"
			case "malformed-ref":
				arg = `{"name":"blog","ref":"..bad"}`
				want = "no commit for '..bad'"
			case "large":
				sconf.SiteMaxBytes = 3
				want = "site exceeds 3 bytes"
			case "resolve-timeout", "archive-timeout":
				want = "git took longer than 17 seconds"
			case "disk":
				parent, e := os.OpenRoot(filepath.Dir(h.cacheRoot))
				if e != nil {
					t.Fatal(e)
				}
				if e := parent.Chmod(filepath.Base(h.cacheRoot), 0500); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { _ = parent.Chmod(filepath.Base(h.cacheRoot), 0700); _ = parent.Close() })
				want = "git failed"
			case "archive-git":
				want = "git failed\n\n> "
			}
			h.after = func(time.Duration) <-chan time.Time {
				calls++
				ch := make(chan time.Time, 1)
				if which == "resolve-timeout" || which == "archive-timeout" && calls == 2 {
					ch <- h.now
				}
				if which == "archive-git" && calls == 2 {
					if e := os.Remove(filepath.Join(p, "objects", blob[:2], blob[2:])); e != nil {
						t.Fatal(e)
					}
				}
				return ch
			}
			configureCache(t, h, sconf, nil)
			before := catalogSnapshot(t, h)
			trees := diskSnapshot(t, h.cacheRoot, false)
			r := h.call(t, "alice", "publish", arg)
			got := refusal(t, r)
			if which == "archive-git" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, h.gitPath, "--git-dir="+p, "archive", "--format=tar", "main")
				cmd.Env = h.env
				var stderr strings.Builder
				cmd.Stderr = &stderr
				if e := cmd.Run(); e == nil {
					t.Fatal("fixture archive succeeded")
				}
				lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
				if len(lines) < 2 {
					t.Fatalf("expected multi-line git failure, got %q", stderr.String())
				}
				expected := "git failed\n\n> " + strings.Join(lines, "\n> ")
				if got != expected {
					t.Fatalf("got %q want %q", got, expected)
				}
			} else if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
			if !reflect.DeepEqual(before, catalogSnapshot(t, h)) || !reflect.DeepEqual(trees, diskSnapshot(t, h.cacheRoot, false)) {
				t.Fatal("refusal changed catalog or trees")
			}
			if which == "missing-site" || which == "missing-repo" {
				if calls != 0 {
					t.Fatal("early refusal ran git")
				}
			}
		})
	}
}

func TestPublishCatalogFailureRollsBackOnlyNewTree(t *testing.T) {
	// R-QTZ2-U7F6 R-9FGR-O8O7
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			h := newHarness(t)
			p := h.repo(t, "rep_0123456789abcdef", "alice")
			sha := commitFile(t, h, p, "body")
			s := h.add(t, "alice", "blog", true)
			if existing {
				if e := h.cfg.Cache.Unpack(context.Background(), s.ID, s.Repo, sha); e != nil {
					t.Fatal(e)
				}
			}
			catalogBefore := catalogSnapshot(t, h)
			before := diskSnapshot(t, h.cacheRoot, false)
			configureCache(t, h, settings.Defaults(), func(string, string) {
				if e := h.cfg.Store.Close(); e != nil {
					t.Fatal(e)
				}
			})
			if existing {
				h.after = func(time.Duration) <-chan time.Time {
					if e := h.cfg.Store.Close(); e != nil {
						t.Fatal(e)
					}
					return make(chan time.Time)
				}
			}
			if got := refusal(t, h.call(t, "alice", "publish", `{"name":"blog"}`)); got != store.Unreachable {
				t.Fatal(got)
			}
			if !reflect.DeepEqual(before, diskSnapshot(t, h.cacheRoot, false)) {
				t.Fatal("catalog failure changed trees")
			}
			if !reflect.DeepEqual(catalogBefore, closedCatalogSnapshot(t, h)) {
				t.Fatal("catalog failure changed catalog")
			}
		})
	}
}

func TestHaltedAndCanceledToolsEndWithoutResult(t *testing.T) {
	// R-LUQL-1X2P
	for _, which := range []string{"halt-create", "halt-publish", "cancel-create", "cancel-publish", "cancel-archive"} {
		t.Run(which, func(t *testing.T) {
			h := newHarness(t)
			p := h.repo(t, "rep_0123456789abcdef", "alice")
			commitFile(t, h, p, "body")
			h.add(t, "alice", "blog", true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tool := "publish"
			args := `{"name":"blog"}`
			if which == "halt-create" || which == "cancel-create" {
				tool = "create"
				args = `{"name":"docs","repo":"rep_0123456789abcdef"}`
			}
			if strings.HasPrefix(which, "halt") {
				h.cfg.Limits.Halt()
			} else {
				runs := 0
				h.after = func(time.Duration) <-chan time.Time {
					runs++
					if which != "cancel-archive" || runs == 2 {
						cancel()
					}
					return make(chan time.Time)
				}
			}
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
			r := httptest.NewRequest(http.MethodPost, "http://sites.example.invalid/mcp", strings.NewReader(body)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-Id", "alice")
			r.Header.Set("X-Request-Id", "req_11223344556677889900aabbccddeeff")
			rec := httptest.NewRecorder()
			ended := make(chan bool, 1)
			wrapped := identity.Require(h.server)
			go func() {
				returned := false
				defer func() { ended <- returned }()
				wrapped.ServeHTTP(rec, r)
				returned = true
			}()
			select {
			case returned := <-ended:
				if returned {
					t.Fatal("ServeHTTP returned")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("goroutine did not end")
			}
			if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
				t.Fatalf("wrote response %s %v", rec.Body.String(), rec.Header())
			}
			if e := h.cfg.Telemetry.Flush(context.Background()); e != nil {
				t.Fatal(e)
			}
			for _, e := range h.capture.Events() {
				if e.Name == "tool.called" || strings.HasPrefix(e.Name, "site.") {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestCommittedPublishAndDeleteSucceedDespiteCleanupFailure(t *testing.T) {
	// R-LZ0A-U1RY
	for _, tool := range []string{"publish", "delete"} {
		t.Run(tool, func(t *testing.T) {
			h := newHarness(t)
			p := h.repo(t, "rep_0123456789abcdef", "alice")
			old := commitFile(t, h, p, "old")
			s := h.add(t, "alice", "blog", true)
			resultObject(t, h.call(t, "alice", "publish", `{"name":"blog"}`))
			oldDir := h.cfg.Cache.Dir(s.ID, old)
			parent, e := os.OpenRoot(filepath.Dir(oldDir))
			if e != nil {
				t.Fatal(e)
			}
			if e := parent.Chmod(filepath.Base(oldDir), 0500); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = parent.Chmod(filepath.Base(oldDir), 0700); _ = parent.Close() })
			next := commitFile(t, h, p, "new")
			r := h.call(t, "alice", tool, `{"name":"blog"}`)
			o := resultObject(t, r)
			if tool == "publish" {
				var v struct{ Commit string }
				if e := json.Unmarshal(o, &v); e != nil || v.Commit != next {
					t.Fatalf("%s %v", o, e)
				}
				x, e := h.cfg.Store.Find(context.Background(), "alice", "blog")
				if e != nil || x.Commit != next {
					t.Fatal("catalog did not publish")
				}
			} else {
				var v struct {
					Deleted bool
					ID      string
				}
				if e := json.Unmarshal(o, &v); e != nil || !v.Deleted || v.ID != s.ID {
					t.Fatal(string(o))
				}
			}
			if _, e := os.Stat(filepath.Join(oldDir, "index.html")); e != nil {
				t.Fatal("could not remove entries must remain", e)
			}
		})
	}
}

func TestPublishRefusesHostileRealGitArchive(t *testing.T) {
	// R-LO17-E43P
	h := newHarness(t)
	p := h.repo(t, "rep_0123456789abcdef", "alice")
	blob := gitInput(t, h, p, "target", "hash-object", "-w", "--stdin")
	inner := gitInput(t, h, p, "100644 blob "+blob+"\tf\n", "mktree")
	tree := gitInput(t, h, p, "120000 blob "+blob+"\td\n040000 tree "+inner+"\td\n", "mktree")
	sha := gitInput(t, h, p, "fixture\n", "commit-tree", tree)
	h.git(t, p, "update-ref", "refs/heads/main", sha)
	h.add(t, "alice", "blog", true)
	before := diskSnapshot(t, h.cacheRoot, false)
	if got := refusal(t, h.call(t, "alice", "publish", `{"name":"blog"}`)); got != "git failed" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(before, diskSnapshot(t, h.cacheRoot, false)) {
		t.Fatal("hostile tree left entries")
	}
}

func TestGitMutationsToolCalledTrail(t *testing.T) {
	// R-XVCP-61ZC
	h := newHarness(t)
	p := h.repo(t, "rep_0123456789abcdef", "alice")
	commitFile(t, h, p, "body")
	for _, c := range []struct{ tool, args, outcome, site string }{{"create", `{"name":"docs","repo":"rep_0123456789abcdef"}`, "ok", "site.created"}, {"create", `{"name":"docs","repo":"rep_0123456789abcdef"}`, "error", ""}, {"publish", `{"name":"docs"}`, "ok", "site.published"}, {"publish", `{"name":"docs","ref":"absent"}`, "error", ""}} {
		start := len(h.capture.Events())
		r := h.call(t, "alice", c.tool, c.args)
		if r.IsError() != (c.outcome == "error") {
			t.Fatal("unexpected result")
		}
		sites := map[string]int{}
		if c.site != "" {
			sites[c.site] = 1
		}
		assertToolTrail(t, h, start, c.tool, c.outcome, sites)
	}
}
