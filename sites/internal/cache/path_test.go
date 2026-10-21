package cache_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
)

// R-03O2-9PLC R-6RHZ-4KQL
func TestPathGrammar(t *testing.T) {
	for _, s := range []string{"a", "a/b/c", ".hidden", "a/.b", "...", "a..b", "-a", "a b", "é", "a\tb", "a\x00b"} {
		if !cache.ValidPath(s) {
			t.Fatalf("refused %q", s)
		}
	}
	for _, s := range []string{"", "/", "/a", "a/", "a//b", "a/b/", "//", ".", "..", "./a", "a/.", "a/./b", "a/../b", "a/.."} {
		if cache.ValidPath(s) {
			t.Fatalf("accepted %q", s)
		}
	}
}

// R-07BR-F0TF R-6SPV-ICHA
func TestPathAddressAndEarlyRefusal(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	for _, p := range []string{"site", "other", strings.Repeat("deep/", 200) + "leaf"} {
		want := filepath.Join(f.root, site, fmt.Sprintf("%s-%x", sha, sha256.Sum256([]byte(p))))
		if f.c.Dir(site, sha, p) != want {
			t.Fatal("path digest")
		}
	}
	n := f.operations.Load()
	for _, p := range []string{"a//b", "a/", "/a", "a/../b", "a\x00b"} {
		// Even an existing directory cannot bypass plain-path validation.
		must(t, os.MkdirAll(f.c.Dir(site, sha, p), 0700))
		before := snap(t, f.root)
		for _, r := range []string{repo, "invalid"} {
			if e := f.c.Unpack(context.Background(), site, r, sha, p); !errors.Is(e, cache.ErrNoDirectory) {
				t.Fatal(p, e)
			}
			if _, e := f.c.Tree(context.Background(), site, r, sha, p); !errors.Is(e, cache.ErrNoDirectory) {
				t.Fatal(p, e)
			}
		}
		if fmt.Sprint(before) != fmt.Sprint(snap(t, f.root)) {
			t.Fatal("refusal changed cache")
		}
		must(t, os.RemoveAll(filepath.Join(f.root, site)))
	}
	if n != f.operations.Load() {
		t.Fatal("refusal changed cache or started git")
	}
}

func pathCommit(f *fixture) string {
	blob := f.blob("nested bytes")
	sub := f.tree("100644 blob " + blob + "\tf\n")
	link := f.blob("../top.txt")
	old := f.file("old")
	dir := f.tree("100644 blob " + blob + "\tindex.html\n040000 tree " + sub + "\tsub\n120000 blob " + link + "\tup\n160000 commit " + old + "\tm\n")
	top := "100644 blob " + blob + "\ttop.txt\n120000 blob " + f.blob("site") + "\tlink\n160000 commit " + old + "\tmodule\n"
	for _, name := range []string{"site", ".hidden", "a b", "é", "a^b", "c:d"} {
		top += "040000 tree " + dir + "\t" + name + "\n"
	}
	return f.commit(f.tree(top))
}

// R-6XLH-1FG2 R-0JIR-8Q8D R-0AZG-KC1I R-0LYK-09PR R-10LC-LIM3 R-6WDK-NNPD R-7019-SYXG
func TestDirectoryArchiveAndChecks(t *testing.T) {
	f := newFixture(t, 1024)
	sha := pathCommit(f)
	ctx := context.Background()
	trace := filepath.Join(f.base, "trace")
	f.env = append(f.env, "GIT_TRACE="+trace)
	for _, p := range []string{"missing", "top.txt", "link", "module", "link/sub"} {
		n := f.operations.Load()
		if e := f.c.Unpack(ctx, site, repo, sha, p); !errors.Is(e, cache.ErrNoDirectory) {
			t.Fatal(p, e)
		}
		if f.operations.Load() != n+1 {
			t.Fatal("directory refusal ran archive")
		}
		absent(t, filepath.Join(f.root, site))
	}
	want := map[string]treeEntry{"index.html": {"file", "nested bytes"}, "sub": {"dir", ""}, "sub/f": {"file", "nested bytes"}, "up": {"link", "../top.txt"}, "m": {"dir", ""}}
	for _, p := range []string{"site", ".hidden", "a b", "é", "a^b", "c:d", "site/sub"} {
		pWant := want
		if p == "site/sub" {
			pWant = map[string]treeEntry{"f": {"file", "nested bytes"}}
		}
		calls := 0
		f.open(nil, func(s, h string) {
			calls++
			if s != site || h != sha {
				t.Fatal("hook args")
			}
			absent(t, f.c.Dir(site, sha, p))
			found := false
			for _, e := range entries(t, filepath.Join(f.root, site)) {
				if strings.HasPrefix(e.Name(), ".") {
					assertTree(t, filepath.Join(f.root, site, e.Name()), pWant)
					found = true
				}
			}
			if !found {
				t.Fatal("no staging")
			}
		})
		n := f.operations.Load()
		must(t, f.c.Unpack(ctx, site, repo, sha, p))
		if calls != 1 || f.operations.Load() != n+2 {
			t.Fatal("directory reads", calls, f.operations.Load()-n)
		}
		assertTree(t, f.c.Dir(site, sha, p), pWant)
		must(t, f.c.Unpack(ctx, site, "invalid", sha, p))
		got, e := f.c.Tree(ctx, site, "invalid", sha, p)
		must(t, e)
		if got != f.c.Dir(site, sha, p) || calls != 1 || f.operations.Load() != n+2 {
			t.Fatal("cached path started git")
		}
	}
	b, e := readFile(trace)
	must(t, e)
	if strings.Contains(string(b), "config --file") || !strings.Contains(string(b), sha+":site/") || !strings.Contains(string(b), "archive --format=tar "+sha+":site") {
		t.Fatal(string(b))
	}
}

// R-RTSA-GH9H R-DTQT-JY0X R-0N6G-E1GG R-0T9Y-AW5X R-7196-6QO5
func TestDirectoryRebuildSizeAndPrune(t *testing.T) {
	f := newFixture(t, 1024)
	b := f.blob(strings.Repeat("x", 1024))
	d := f.tree("100644 blob " + b + "\tf\n")
	sha := f.commit(f.tree("040000 tree " + d + "\td\n040000 tree " + d + "\te\n100644 blob " + f.blob(strings.Repeat("z", 4096)) + "\tbig\n"))
	must(t, os.RemoveAll(f.root))
	got, e := f.c.Tree(context.Background(), site, repo, sha, "d")
	must(t, e)
	if got != f.c.Dir(site, sha, "d") {
		t.Fatal(got)
	}
	assertTree(t, got, map[string]treeEntry{"f": {"file", strings.Repeat("x", 1024)}})
	must(t, f.c.Unpack(context.Background(), site, repo, sha, "e"))
	if _, e = f.c.Tree(context.Background(), site, repo, sha, ""); !errors.Is(e, limits.ErrTooLarge) {
		t.Fatal(e)
	}
	must(t, f.c.Prune(site, sha, "d"))
	absent(t, f.c.Dir(site, sha, "e"))
	absent(t, f.c.Dir(site, sha, ""))
	if len(entries(t, filepath.Join(f.root, site))) != 1 {
		t.Fatal("prune retained other path")
	}
	assertTree(t, got, map[string]treeEntry{"f": {"file", strings.Repeat("x", 1024)}})
}

// R-04VY-NHC1 R-063V-192Q R-1315-D23H
func TestDirectoryErrorVocabulary(t *testing.T) {
	if cache.ReasonDirectoryMissing != "directory_missing" || cache.Reason(fmt.Errorf("wrapped: %w", cache.ErrNoDirectory)) != cache.ReasonDirectoryMissing {
		t.Fatal("directory reason")
	}
	all := []error{cache.ErrRepositoryMissing, cache.ErrNoCommit, cache.ErrNoDirectory, git.ErrNotFound, limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrDraining, limits.ErrHalted}
	for i, a := range all {
		if a == nil {
			t.Fatal("nil error")
		}
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Fatal("same error")
			}
		}
	}
	for _, lower := range []error{limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrHalted, git.ErrNotFound} {
		if cache.Reason(errors.Join(lower, cache.ErrNoDirectory)) != cache.ReasonDirectoryMissing {
			t.Fatal("priority")
		}
	}
	for _, higher := range []error{cache.ErrRepositoryMissing, cache.ErrNoCommit} {
		if cache.Reason(errors.Join(cache.ErrNoDirectory, higher)) != cache.Reason(higher) {
			t.Fatal("priority")
		}
	}
}

// R-0ZDG-7QVE R-11T8-ZACS R-72H2-KIEU R-0T9Y-AW5X
func TestSameCommitDifferentPathsAndSharedWaiters(t *testing.T) {
	f := newFixture(t, 1024)
	sha := pathCommit(f)
	entered := make(chan struct{})
	release := make(chan struct{})
	joined := make(chan struct{}, 2)
	var hooks atomic.Int64
	f.open(func(string, string) { joined <- struct{}{} }, func(string, string) {
		if hooks.Add(1) == 1 {
			close(entered)
			<-release
		}
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	first := make(chan error, 1)
	go func() { _, e := f.c.Tree(ctx, site, repo, sha, "site"); first <- e }()
	wait(t, entered)
	// A distinct path of the same commit must complete while the first is blocked.
	type result struct {
		dir string
		err error
	}
	independent := make(chan result, 1)
	go func() {
		d, e := f.c.Tree(context.Background(), site, repo, sha, "site/sub")
		independent <- result{d, e}
	}()
	r := wait(t, independent)
	must(t, r.err)
	got := r.dir
	if got != f.c.Dir(site, sha, "site/sub") {
		t.Fatal(got)
	}
	f.l.Drain()
	second := make(chan error, 1)
	go func() { _, e := f.c.Tree(context.Background(), site, repo, sha, "site"); second <- e }()
	wait(t, joined)
	cause := errors.New("caller ended")
	cancel(cause)
	if e := wait(t, first); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	// Pruning one path discards the running tree of the same sha at another path.
	must(t, f.c.Prune(site, sha, "site/sub"))
	close(release)
	must(t, wait(t, second))
	absent(t, f.c.Dir(site, sha, "site"))
	assertTree(t, got, map[string]treeEntry{"f": {"file", "nested bytes"}})
	if f.operations.Load() != 6 {
		t.Fatal("unexpected git runs", f.operations.Load())
	}
	if _, e := f.c.Tree(context.Background(), site, repo, sha, "site"); !errors.Is(e, limits.ErrDraining) {
		t.Fatal(e)
	}
}

// R-6XLH-1FG2 R-6YTD-F76R R-73OY-YA5J
func TestDirectoryFailuresLeaveNoTree(t *testing.T) {
	for _, mode := range []string{"not-git", "missing-blob", "disk"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, 1024)
			sha := pathCommit(f)
			switch mode {
			case "not-git":
				must(t, os.RemoveAll(f.dir))
				must(t, os.Mkdir(f.dir, 0700))
			case "missing-blob":
				object := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+":site/index.html")
				must(t, os.Remove(filepath.Join(f.dir, "objects", object[:2], object[2:])))
			case "disk":
				must(t, chmodDir(f.root, 0500))
				t.Cleanup(func() { _ = chmodDir(f.root, 0700) })
			}
			before := snap(t, f.root)
			e := f.c.Unpack(context.Background(), site, repo, sha, "site")
			var ge *git.Error
			if mode == "disk" {
				if e == nil || errors.As(e, &ge) || cache.Reason(e) != cache.ReasonGitFailed {
					t.Fatal(e)
				}
				for _, sentinel := range []error{cache.ErrRepositoryMissing, cache.ErrNoCommit, cache.ErrNoDirectory, git.ErrNotFound, limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrHalted} {
					if errors.Is(e, sentinel) {
						t.Fatal(e)
					}
				}
			} else if !errors.As(e, &ge) || ge.Status == 0 || ge.Stderr == "" {
				t.Fatal(e)
			}
			absent(t, filepath.Join(f.root, site))
			// A failed check/archive leaves no entries. A disk refusal also preserves metadata.
			if mode == "disk" && fmt.Sprint(before) != fmt.Sprint(snap(t, f.root)) {
				t.Fatal("disk refusal changed cache")
			}
		})
	}
}
