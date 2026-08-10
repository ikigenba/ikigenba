package bintest

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type bumpFixture struct {
	repo   string
	origin string
	app    string
	bump   string
}

func newBumpFixture(t *testing.T) bumpFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	origin := filepath.Join(root, "origin.git")
	app := "example"
	if err := os.MkdirAll(filepath.Join(repo, app), 0o755); err != nil {
		t.Fatalf("make fixture app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, app, "VERSION"), []byte("v1.2.3\n"), 0o644); err != nil {
		t.Fatalf("write fixture VERSION: %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Bump Test")
	runGit(t, repo, "config", "user.email", "bump@example.invalid")
	runGit(t, repo, "add", app+"/VERSION")
	runGit(t, repo, "commit", "-m", "initial version")
	runGit(t, root, "init", "--bare", origin)
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "push", "-u", "origin", "main")
	return bumpFixture{
		repo:   repo,
		origin: origin,
		app:    app,
		bump:   filepath.Join(repoRoot(t), "bin", "bump"),
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f bumpFixture) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runScript(t, []string{"BUMP_REPO_ROOT=" + f.repo}, f.bump, append([]string{f.app}, args...)...)
}

func (f bumpFixture) version(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.repo, f.app, "VERSION"))
	if err != nil {
		t.Fatalf("read fixture VERSION: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func (f bumpFixture) head(t *testing.T) string {
	t.Helper()
	return runGit(t, f.repo, "rev-parse", "HEAD")
}

func (f bumpFixture) originHead(t *testing.T) string {
	t.Helper()
	return runGit(t, f.origin, "rev-parse", "refs/heads/main")
}

func (f bumpFixture) writeChangelog(t *testing.T, heading string) {
	t.Helper()
	content := "# Changelog\n\n" + heading + "\n\n- fixture entry\n"
	if err := os.WriteFile(filepath.Join(f.repo, f.app, "CHANGELOG.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture changelog: %v", err)
	}
}

// R-CKLX-X89X
func TestBumpRefusesMissingChangelogWithoutChangingVersionOrCommit(t *testing.T) {
	f := newBumpFixture(t)
	before := f.head(t)
	out, err := f.run(t, "patch")
	if err == nil {
		t.Fatalf("bump without CHANGELOG.md succeeded:\n%s", out)
	}
	if !strings.Contains(out, "author its new top section for v1.2.4") {
		t.Fatalf("refusal did not tell operator how to fix changelog: %q", out)
	}
	if got := f.version(t); got != "v1.2.3" {
		t.Fatalf("VERSION after refusal = %q, want v1.2.3", got)
	}
	if got := f.head(t); got != before {
		t.Fatalf("HEAD after refusal = %s, want unchanged %s", got, before)
	}
}

// R-CLTU-B00M
func TestBumpRefusesMismatchedTopChangelogHeading(t *testing.T) {
	f := newBumpFixture(t)
	f.writeChangelog(t, "## v1.2.3 — previous release")
	before := f.head(t)
	out, err := f.run(t, "patch")
	if err == nil {
		t.Fatalf("bump with stale changelog heading succeeded:\n%s", out)
	}
	if !strings.Contains(out, "top section must be v1.2.4") {
		t.Fatalf("refusal did not name required version: %q", out)
	}
	if got := f.version(t); got != "v1.2.3" {
		t.Fatalf("VERSION after refusal = %q, want v1.2.3", got)
	}
	if got := f.head(t); got != before {
		t.Fatalf("HEAD after refusal = %s, want unchanged %s", got, before)
	}
}

// R-CN1Q-ORRB
func TestBumpCommitsExactlyVersionAndMatchingChangelog(t *testing.T) {
	f := newBumpFixture(t)
	f.writeChangelog(t, "## v1.2.3 — previous release")
	runGit(t, f.repo, "add", f.app+"/CHANGELOG.md")
	runGit(t, f.repo, "commit", "-m", "add changelog")
	runGit(t, f.repo, "push", "origin", "main")
	f.writeChangelog(t, "## v1.2.4 — release date is author-owned")
	unrelated := filepath.Join(f.repo, "unrelated.txt")
	if err := os.WriteFile(unrelated, []byte("do not commit\n"), 0o644); err != nil {
		t.Fatalf("write unrelated edit: %v", err)
	}
	before := f.head(t)
	out, err := f.run(t, "patch")
	if err != nil {
		t.Fatalf("bump with matching changelog failed: %v\n%s", err, out)
	}
	if got := f.version(t); got != "v1.2.4" {
		t.Fatalf("VERSION after bump = %q, want v1.2.4", got)
	}
	if got := runGit(t, f.repo, "rev-list", "--count", before+"..HEAD"); got != "1" {
		t.Fatalf("bump created %s commits, want 1", got)
	}
	paths := strings.Fields(runGit(t, f.repo, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"))
	sort.Strings(paths)
	want := []string{f.app + "/CHANGELOG.md", f.app + "/VERSION"}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("bump commit paths = %v, want exactly %v", paths, want)
	}
	if got := f.originHead(t); got != f.head(t) {
		t.Fatalf("origin main = %s, want pushed HEAD %s", got, f.head(t))
	}
	if status := runGit(t, f.repo, "status", "--short", "--", "unrelated.txt"); status != "?? unrelated.txt" {
		t.Fatalf("unrelated edit status = %q, want it left uncommitted", status)
	}
}

// R-CO9N-2JI0
func TestBumpDryRunSkipsChangelogGateAndAllWrites(t *testing.T) {
	for _, tc := range []struct {
		name    string
		heading string
	}{
		{name: "absent"},
		{name: "mismatched", heading: "## v1.2.3 — stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBumpFixture(t)
			if tc.heading != "" {
				f.writeChangelog(t, tc.heading)
			}
			before := f.head(t)
			originBefore := f.originHead(t)
			out, err := f.run(t, "patch", "--dry-run")
			if err != nil {
				t.Fatalf("dry-run failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "v1.2.3 -> v1.2.4") {
				t.Fatalf("dry-run did not print next version: %q", out)
			}
			if got := f.version(t); got != "v1.2.3" {
				t.Fatalf("VERSION after dry-run = %q, want v1.2.3", got)
			}
			if got := f.head(t); got != before {
				t.Fatalf("HEAD after dry-run = %s, want unchanged %s", got, before)
			}
			if got := f.originHead(t); got != originBefore {
				t.Fatalf("origin main after dry-run = %s, want unchanged %s", got, originBefore)
			}
		})
	}
}
