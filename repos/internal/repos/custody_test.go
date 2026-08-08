package repos

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sequenceClock struct {
	now  time.Time
	step time.Duration
}

func (c *sequenceClock) Now() time.Time {
	now := c.now
	c.now = c.now.Add(c.step)
	return now
}

func newTestCustody(t *testing.T) (*Custody, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := &sequenceClock{now: time.Unix(1700000000, 1), step: time.Nanosecond}
	custody, err := NewCustody(root, NewCommandGit("git", root), clock)
	if err != nil {
		t.Fatal(err)
	}
	return custody, root
}

func TestInitCreatesEmptyBareRepositoryOnMain(t *testing.T) {
	// R-J4Y9-H96C
	ctx := context.Background()
	custody, _ := newTestCustody(t)
	if err := custody.Init(ctx, "code", "demo"); err != nil {
		t.Fatal(err)
	}
	path, _ := custody.Path("code", "demo")
	if got := strings.TrimSpace(runGit(t, path, "rev-parse", "--is-bare-repository")); got != "true" {
		t.Fatalf("bare = %q, want true", got)
	}
	if got := strings.TrimSpace(runGit(t, path, "symbolic-ref", "HEAD")); got != "refs/heads/main" {
		t.Fatalf("HEAD = %q, want refs/heads/main", got)
	}
	if got := strings.TrimSpace(runGit(t, "", "ls-remote", path)); got != "" {
		t.Fatalf("empty repository advertised refs:\n%s", got)
	}
}

func TestPathRejectsEveryUnsafeKeyWithoutWriting(t *testing.T) {
	// R-J665-V0X1
	custody, root := newTestCustody(t)
	parent := filepath.Dir(root)
	for _, key := range []struct{ kind, name string }{
		{"nope", "safe"}, {"code", ""}, {"code", "."}, {"code", ".."},
		{"code", "../escape"}, {"code", "a/b"}, {"code", "Upper"}, {"code", strings.Repeat("a", 65)},
	} {
		if _, err := custody.Path(key.kind, key.name); !errors.Is(err, ErrValidation) {
			t.Errorf("Path(%q, %q) error = %v, want ErrValidation", key.kind, key.name, err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("validation wrote under state root: %#v", entries)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape path exists or stat failed: %v", err)
	}
}

func TestRenamePreservesHistoryAndRefusesMissingOrOccupiedTargets(t *testing.T) {
	// R-J7E2-8SNQ
	ctx := context.Background()
	custody, _ := newTestCustody(t)
	if err := custody.Init(ctx, "code", "old"); err != nil {
		t.Fatal(err)
	}
	oldPath, _ := custody.Path("code", "old")
	tip := seedCommits(t, oldPath, 2)
	if err := custody.Rename(ctx, "code", "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old path still exists: %v", err)
	}
	newPath, _ := custody.Path("code", "new")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", newPath, clone)
	if got := strings.TrimSpace(runGit(t, clone, "rev-list", "--count", "HEAD")); got != "2" {
		t.Fatalf("commit count = %s, want 2", got)
	}
	if got := strings.TrimSpace(runGit(t, clone, "rev-parse", "HEAD")); got != tip {
		t.Fatalf("tip = %s, want %s", got, tip)
	}
	if err := custody.Init(ctx, "code", "occupied"); err != nil {
		t.Fatal(err)
	}
	if err := custody.Rename(ctx, "code", "new", "occupied"); !errors.Is(err, ErrConflict) {
		t.Fatalf("occupied rename error = %v, want ErrConflict", err)
	}
	if !custody.Exists("code", "new") || !custody.Exists("code", "occupied") {
		t.Fatal("occupied rename moved one of the repositories")
	}
	if err := custody.Rename(ctx, "code", "missing", "unused"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing rename error = %v, want ErrNotFound", err)
	}
}

func TestArchivePreservesHistoryAndAllowsDistinctRecreation(t *testing.T) {
	// R-J8LY-MKEF
	ctx := context.Background()
	custody, root := newTestCustody(t)
	if err := custody.Init(ctx, "scripts", "worker"); err != nil {
		t.Fatal(err)
	}
	live, _ := custody.Path("scripts", "worker")
	tip := seedCommits(t, live, 2)
	first, err := custody.Archive(ctx, "scripts", "worker")
	if err != nil {
		t.Fatal(err)
	}
	assertArchivedHistory(t, filepath.Join(root, filepath.FromSlash(first)), tip, "2")
	if _, err := os.Stat(live); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("live path after archive: %v", err)
	}
	if err := custody.Init(ctx, "scripts", "worker"); err != nil {
		t.Fatal(err)
	}
	if refs, err := custody.Refs(ctx, "scripts", "worker"); err != nil || len(refs) != 0 {
		t.Fatalf("recreated repository refs=%v err=%v, want empty", refs, err)
	}
	second, err := custody.Archive(ctx, "scripts", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("archive paths collided: %q", first)
	}
	for _, rel := range []string{first, second} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil || !info.IsDir() {
			t.Fatalf("archive %s missing: info=%v err=%v", rel, info, err)
		}
	}
}

func TestEnsureHooksRestoresExecutableIdenticalHookAndSweepRepairsAll(t *testing.T) {
	// R-J9TV-0C54
	ctx := context.Background()
	custody, _ := newTestCustody(t)
	for _, name := range []string{"one", "two"} {
		if err := custody.Init(ctx, "sites", name); err != nil {
			t.Fatal(err)
		}
	}
	one, _ := custody.Path("sites", "one")
	hook := filepath.Join(one, "hooks", "pre-receive")
	want, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(hook); info.Mode().Perm() != 0o755 {
		t.Fatalf("initial hook mode = %o", info.Mode().Perm())
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if err := custody.EnsureHooks(ctx, "sites", "one"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(hook)
	if string(got) != string(want) {
		t.Fatal("restored hook content differs")
	}
	if info, _ := os.Stat(hook); info.Mode().Perm() != 0o755 {
		t.Fatalf("restored hook mode = %o", info.Mode().Perm())
	}
	for _, name := range []string{"one", "two"} {
		path, _ := custody.Path("sites", name)
		if err := os.Remove(filepath.Join(path, "hooks", "pre-receive")); err != nil {
			t.Fatal(err)
		}
	}
	if err := custody.SweepHooks(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		path, _ := custody.Path("sites", name)
		if info, err := os.Stat(filepath.Join(path, "hooks", "pre-receive")); err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("sweep did not repair %s: info=%v err=%v", name, info, err)
		}
	}
}

func seedCommits(t *testing.T, bare string, count int) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, "", "clone", bare, work)
	for i := 0; i < count; i++ {
		if err := os.WriteFile(filepath.Join(work, "history.txt"), []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, work, "add", "history.txt")
		runGit(t, work, "commit", "-m", "commit")
	}
	runGit(t, work, "push", "origin", "main")
	return strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
}

func assertArchivedHistory(t *testing.T, archived, tip, count string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "archive-clone")
	runGit(t, "", "clone", archived, clone)
	if got := strings.TrimSpace(runGit(t, clone, "rev-list", "--count", "HEAD")); got != count {
		t.Fatalf("archived commit count = %s, want %s", got, count)
	}
	if got := strings.TrimSpace(runGit(t, clone, "rev-parse", "HEAD")); got != tip {
		t.Fatalf("archived tip = %s, want %s", got, tip)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Repos Test", "GIT_AUTHOR_EMAIL=repos@example.test",
		"GIT_COMMITTER_NAME=Repos Test", "GIT_COMMITTER_EMAIL=repos@example.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
