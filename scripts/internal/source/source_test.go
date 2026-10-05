package source_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

const repo = "rep_0123456789abcdef"

type fixture struct {
	t                           *testing.T
	root, repos, dir, exe, link string
	env                         []string
	g                           *git.Git
	l                           *limits.Limits
	s                           *source.Source
	timers                      atomic.Int64
	after                       func(time.Duration) <-chan time.Time
}

func newFixture(t *testing.T, maximum int64) *fixture {
	t.Helper()
	d := t.TempDir()
	exe, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	repos := filepath.Join(d, "repos")
	if e = os.Mkdir(repos, 0700); e != nil {
		t.Fatal(e)
	}
	f := &fixture{t: t, root: d, repos: repos, dir: filepath.Join(repos, repo+".git"), exe: exe}
	f.env = []string{"HOME=" + d, "XDG_CONFIG_HOME=" + d, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	bin := filepath.Join(d, "bin")
	if e = os.Mkdir(bin, 0700); e != nil {
		t.Fatal(e)
	}
	f.link = filepath.Join(bin, "git")
	if e = os.Symlink(exe, f.link); e != nil {
		t.Fatal(e)
	}
	f.g, e = git.Find(bin, func() []string { return append([]string{}, f.env...) })
	if e != nil {
		t.Fatal(e)
	}
	v := settings.Defaults()
	v.TreeMaxBytes = maximum
	f.l = limits.New(v, limits.Clock{After: func(d time.Duration) <-chan time.Time {
		f.timers.Add(1)
		if f.after != nil {
			return f.after(d)
		}
		return make(chan time.Time)
	}})
	f.s = source.New(source.Config{Repos: repos, Git: f.g, Limits: f.l})
	return f
}
func (f *fixture) run(input string, args ...string) string {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.exe, args...)
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	if e != nil {
		f.t.Fatalf("git %v: %v %s", args, e, stderr.String())
	}
	return strings.TrimSuffix(string(b), "\n")
}
func (f *fixture) bare() { f.t.Helper(); f.run("", "init", "--bare", "--initial-branch=main", f.dir) }
func (f *fixture) blob(data string) string {
	return f.run(data, "--git-dir="+f.dir, "hash-object", "-w", "--stdin")
}
func (f *fixture) tree(entries ...string) string {
	input := strings.Join(entries, "\n")
	if len(entries) > 0 {
		input += "\n"
	}
	return f.run(input, "--git-dir="+f.dir, "mktree")
}
func (f *fixture) commit(tree string) string {
	sha := f.run("fixture\n", "--git-dir="+f.dir, "commit-tree", tree)
	f.run("", "--git-dir="+f.dir, "update-ref", "refs/heads/main", sha)
	return sha
}
func (f *fixture) config(key, value string) {
	f.run("", "config", "--file", filepath.Join(f.dir, "config"), key, value)
}
func (f *fixture) simple() string {
	f.bare()
	return f.commit(f.tree("100644 blob " + f.blob("print('hello')\n") + "\tmain.py"))
}
func result(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error %v want %v", err, want)
	}
}
func generic(t *testing.T, err error) {
	t.Helper()
	var ge *git.Error
	if err == nil || errors.As(err, &ge) {
		t.Fatalf("generic failure %v", err)
	}
	for _, sent := range []error{source.ErrRepositoryMissing, source.ErrNoCommit, git.ErrNotFound, limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrHalted} {
		if errors.Is(err, sent) {
			t.Fatalf("unexpected sentinel %v", err)
		}
	}
}

// R-HR8G-RMCB R-HXBY-OH1S R-9FAK-A3ZU
func TestIdentifiers(t *testing.T) {
	if source.RepoPrefix != "rep_" {
		t.Fatal(source.RepoPrefix)
	}
	for _, s := range []string{repo, "rep_0000000000000000", "rep_ffffffffffffffff"} {
		if !source.ValidRepo(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", "rep_0123456789abcde", "rep_0123456789abcdef0", "rep_0123456789abcdeF", "rep_0123456789abcdeg", "scr_0123456789abcdef", repo + " "} {
		if source.ValidRepo(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"main", "rel-a", "rel.a", "refs/heads/main", "a/b/c", "-foo", "@a", "é"} {
		if !source.ValidRef(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"..bad", "", "a b", "a~", "a^", "a:b", "a?", "a*", "a[", "a\\b", "a/", "/a", "a//b", "a.lock", ".a", "a/.b", "a.", "a@{b", "@", "a\tb", "a/b.lock"} {
		if source.ValidRef(s) {
			t.Fatal(s)
		}
	}
	for i := 0; i < 32; i++ {
		if source.ValidRef("a" + string(rune(i)) + "b") {
			t.Fatalf("control %d", i)
		}
	}
	if source.ValidRef("a\x7fb") {
		t.Fatal("DEL")
	}
}

// R-HSGD-5E30 R-HTO9-J5TP R-IRTG-8QL5
func TestReasons(t *testing.T) {
	sent := []error{source.ErrRepositoryMissing, source.ErrNoCommit, git.ErrNotFound, limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrHalted}
	for i, a := range sent[:2] {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range sent {
			if i != j && errors.Is(a, b) {
				t.Fatal("sentinels overlap")
			}
		}
	}
	cases := []struct {
		err    error
		reason string
	}{{source.ErrRepositoryMissing, store.ReasonRepositoryMissing}, {source.ErrNoCommit, store.ReasonCommitMissing}, {limits.ErrTooLarge, store.ReasonTooLarge}, {limits.ErrTimedOut, store.ReasonTimedOut}, {git.ErrNotFound, store.ReasonGitFailed}, {limits.ErrHalted, store.ReasonGitFailed}, {context.Canceled, store.ReasonGitFailed}, {&git.Error{Status: 128, Stderr: "fixture"}, store.ReasonGitFailed}, {errors.New("disk"), store.ReasonGitFailed}}
	for _, c := range cases {
		if got := source.Reason(fmt.Errorf("wrapped: %w", c.err)); got != c.reason {
			t.Fatalf("%v: %s", c.err, got)
		}
	}
}

// R-HOSO-02UX R-HUW5-WXKE R-HW42-APB3
func TestConstruction(t *testing.T) {
	f := newFixture(t, 1024)
	for _, kind := range []string{"absent", "file", "directory"} {
		p := filepath.Join(f.root, kind)
		if kind == "file" {
			if e := os.WriteFile(p, []byte("unchanged"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		if kind == "directory" {
			if e := os.Mkdir(p, 0700); e != nil {
				t.Fatal(e)
			}
		}
		before := snapshot(t, f.root)
		s := source.New(source.Config{Repos: p, Git: f.g, Limits: f.l})
		if s == nil || s.RepoDir("anything") != filepath.Join(p, "anything.git") {
			t.Fatal("constructor")
		}
		if !reflect.DeepEqual(before, snapshot(t, f.root)) || f.timers.Load() != 0 {
			t.Fatal("constructor touched state")
		}
	}
}

// R-HQ0K-DULM R-I3FG-LBR9 R-IBYR-9PY4 R-ID6N-NHOT R-IQLJ-UYUG
func TestPreflight(t *testing.T) {
	f := newFixture(t, 1024)
	ctx := context.Background()
	for _, r := range []string{"../bad", repo} {
		_, e := f.s.Owner(ctx, r)
		result(t, e, source.ErrRepositoryMissing)
		if n, ok := f.s.Name(ctx, r); n != "" || ok {
			t.Fatal("missing name")
		}
		_, e = f.s.Resolve(ctx, r, "main")
		result(t, e, source.ErrRepositoryMissing)
		dest := filepath.Join(f.root, "missing-tree")
		result(t, f.s.Archive(ctx, r, strings.Repeat("a", 40), dest), source.ErrRepositoryMissing)
		if _, e = os.Stat(dest); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("created tree")
		}
	}
	if e := os.WriteFile(f.dir, []byte("file"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.Resolve(ctx, repo, "main")
	result(t, e, source.ErrRepositoryMissing)
	if e = os.Remove(f.dir); e != nil {
		t.Fatal(e)
	}
	sha := f.simple()
	if got, e := f.s.Resolve(ctx, repo, "main"); e != nil || got != sha {
		t.Fatalf("late repository %s %v", got, e)
	}
	base := f.timers.Load()
	for _, bad := range []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("A", 40), "--all"} {
		dest := filepath.Join(f.root, "bad-sha")
		result(t, f.s.Archive(ctx, repo, bad, dest), source.ErrNoCommit)
		if _, e = os.Stat(dest); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("created tree")
		}
	}
	dir := filepath.Join(f.root, "exists")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "keep"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	before := snapshot(t, dir)
	generic(t, f.s.Archive(ctx, repo, sha, dir))
	if !reflect.DeepEqual(before, snapshot(t, dir)) {
		t.Fatal("existing dir changed")
	}
	generic(t, f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "absent-parent", "tree")))
	if f.timers.Load() != base {
		t.Fatal("preflight called operation")
	}
	_, e = f.s.Owner(ctx, repo)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.s.Name(ctx, repo)
	if e = f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "tree")); e != nil {
		t.Fatal(e)
	}
	if f.timers.Load() != base+3 {
		t.Fatalf("operation count %d", f.timers.Load())
	}
}

// R-I4NC-Z3HY R-I5V9-CV8N R-I735-QMZC
func TestConfigReads(t *testing.T) {
	f := newFixture(t, 1024)
	f.bare()
	ctx := context.Background()
	global := filepath.Join(f.root, "global")
	if e := os.WriteFile(global, []byte("[ikigenba]\nowner = global\nname = global\n"), 0600); e != nil {
		t.Fatal(e)
	}
	f.env = append(f.env, "GIT_CONFIG_GLOBAL="+global)
	if o, e := f.s.Owner(ctx, repo); e != nil || o != "" {
		t.Fatalf("%q %v", o, e)
	}
	if n, ok := f.s.Name(ctx, repo); ok || n != "" {
		t.Fatal("global name")
	}
	f.config("ikigenba.owner", "first")
	f.run("", "config", "--file", filepath.Join(f.dir, "config"), "--add", "ikigenba.owner", "second\n")
	if o, e := f.s.Owner(ctx, repo); e != nil || o != "second\n" {
		t.Fatalf("last owner %q %v", o, e)
	}
	for _, name := range []string{"alpha", "beta", ""} {
		f.config("ikigenba.name", name)
		if n, ok := f.s.Name(ctx, repo); n != name || ok != (name != "") {
			t.Fatalf("name %q %v", n, ok)
		}
	}
	if e := os.Remove(filepath.Join(f.dir, "config")); e != nil {
		t.Fatal(e)
	}
	if o, e := f.s.Owner(ctx, repo); e != nil || o != "" {
		t.Fatalf("absent config %q %v", o, e)
	}
	if n, ok := f.s.Name(ctx, repo); n != "" || ok {
		t.Fatal("absent config name")
	}
	if e := os.WriteFile(filepath.Join(f.dir, "config"), []byte("[broken"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.Owner(ctx, repo)
	var ge *git.Error
	if !errors.As(e, &ge) || ge.Status == 1 {
		t.Fatal(e)
	}
	if n, ok := f.s.Name(ctx, repo); n != "" || ok {
		t.Fatal("broken config name")
	}
}

// R-I8B2-4EQ1 R-I9IY-I6GQ
func TestResolve(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.simple()
	f.run("", "--git-dir="+f.dir, "tag", "light", sha)
	f.run("", "--git-dir="+f.dir, "tag", "-a", "annotated", "-m", "fixture", sha)
	f.run("", "--git-dir="+f.dir, "update-ref", "refs/heads/-foo", sha)
	for _, ref := range []string{"main", "light", "annotated", sha, sha[:10], "-foo"} {
		got, e := f.s.Resolve(context.Background(), repo, ref)
		if e != nil || got != sha {
			t.Fatalf("%q: %q %v", ref, got, e)
		}
	}
	tree := f.run("", "--git-dir="+f.dir, "rev-parse", sha+"^{tree}")
	for _, ref := range []string{"unknown", "..bad", "", "--all", strings.Repeat("0", 40), tree} {
		got, e := f.s.Resolve(context.Background(), repo, ref)
		if got != "" {
			t.Fatal(got)
		}
		result(t, e, source.ErrNoCommit)
	}
	other := "rep_1111111111111111"
	if e := os.Mkdir(f.s.RepoDir(other), 0700); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.Resolve(context.Background(), other, "main")
	var ge *git.Error
	if !errors.As(e, &ge) || ge.Status != 128 || !strings.HasPrefix(ge.Stderr, "fatal: not a git repository") {
		t.Fatal(e)
	}
}

// snapshot observes fixtures only, never the checkout.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := make(map[string]string)
	e := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		value := fmt.Sprintf("%v %d %v", info.Mode(), info.Size(), info.ModTime())
		if info.Mode().IsRegular() {
			value += read(t, p)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			b, e := os.Readlink(p)
			if e != nil {
				return e
			}
			value += b
		}
		rel, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		m[rel] = value
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func list(t *testing.T, dir string) []string {
	t.Helper()
	var a []string
	e := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p != dir {
			r, e := filepath.Rel(dir, p)
			if e != nil {
				return e
			}
			a = append(a, filepath.ToSlash(r))
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(a)
	return a
}
func read(t *testing.T, p string) string {
	t.Helper()
	root, e := os.OpenRoot(filepath.Dir(p))
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := root.Close(); e != nil {
			t.Error(e)
		}
	}()
	b, e := root.ReadFile(filepath.Base(p))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

// R-IEEK-19FI R-I27K-7K0K
func TestArchiveAndRepositoryUnchanged(t *testing.T) {
	f := newFixture(t, 1024)
	f.bare()
	blob := f.blob("main bytes\x00\n")
	child := f.tree("100644 blob " + f.blob("package") + "\t__init__.py")
	sha := f.commit(f.tree("100644 blob "+f.blob("secret")+"\t.env", "120000 blob "+f.blob("main.py")+"\tlink.py", "100755 blob "+blob+"\tmain.py", "040000 tree "+child+"\tpkg"))
	f.config("ikigenba.owner", "owner")
	f.config("ikigenba.name", "fixture")
	before := snapshot(t, f.repos)
	ctx := context.Background()
	if _, e := f.s.Owner(ctx, repo); e != nil {
		t.Fatal(e)
	}
	if _, ok := f.s.Name(ctx, repo); !ok {
		t.Fatal("name")
	}
	if _, e := f.s.Resolve(ctx, repo, "main"); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(f.root, "tree")
	if e := f.s.Archive(ctx, repo, sha, dest); e != nil {
		t.Fatal(e)
	}
	if got := list(t, dest); !reflect.DeepEqual(got, []string{".env", "link.py", "main.py", "pkg", "pkg/__init__.py"}) {
		t.Fatal(got)
	}
	if read(t, filepath.Join(dest, "main.py")) != "main bytes\x00\n" || read(t, filepath.Join(dest, "pkg", "__init__.py")) != "package" || read(t, filepath.Join(dest, ".env")) != "secret" {
		t.Fatal("altered bytes")
	}
	if link, e := os.Readlink(filepath.Join(dest, "link.py")); e != nil || link != "main.py" {
		t.Fatalf("%q %v", link, e)
	}
	if !reflect.DeepEqual(before, snapshot(t, f.repos)) {
		t.Fatal("source changed repository")
	}
	empty := f.commit(f.tree())
	emptyDest := filepath.Join(f.root, "empty")
	if e := f.s.Archive(ctx, repo, empty, emptyDest); e != nil {
		t.Fatal(e)
	}
	if len(list(t, emptyDest)) != 0 {
		t.Fatal("empty tree")
	}
	sub := f.commit(f.tree("160000 commit " + sha + "\tsubmodule"))
	subDest := filepath.Join(f.root, "sub")
	if e := f.s.Archive(ctx, repo, sub, subDest); e != nil {
		t.Fatal(e)
	}
	if got := list(t, subDest); !reflect.DeepEqual(got, []string{"submodule"}) {
		t.Fatal(got)
	}
	// Fixtures changed the repo for additional commits; now observe source success and failure only.
	before = snapshot(t, f.repos)
	_, _ = f.s.Resolve(ctx, repo, "missing")
	_, _ = f.s.Owner(ctx, repo)
	_, _ = f.s.Name(ctx, repo)
	_ = f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "again"))
	_ = f.s.Archive(ctx, repo, sha, dest)
	if !reflect.DeepEqual(before, snapshot(t, f.repos)) {
		t.Fatal("source changed repository")
	}
}

// R-9GIG-NVQJ R-9HQD-1NH8 R-XK4V-J670 R-9IY9-FF7X R-IFMG-F167
func TestArchiveSizeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		a, b  int
		want  error
		paths []string
	}{{"boundary", 1024, 0, nil, []string{"a.txt", "link"}}, {"over", 600, 600, limits.ErrTooLarge, []string{"a.txt"}}, {"pipe", 1025, 262144, limits.ErrTooLarge, nil}, {"first", 65536, 0, limits.ErrTooLarge, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 1024)
			f.bare()
			marker := "SCRIPTS_ARCHIVE_TEST=" + f.root
			f.env = append(f.env, marker)
			entries := []string{"100644 blob " + f.blob(strings.Repeat("a", tc.a)) + "\ta.txt"}
			if tc.b > 0 {
				entries = append(entries, "100644 blob "+f.blob(strings.Repeat("b", tc.b))+"\tb.txt")
			}
			if tc.want == nil {
				entries = append(entries, "120000 blob "+f.blob("a.txt")+"\tlink")
			}
			sha := f.commit(f.tree(entries...))
			dest := filepath.Join(f.root, "tree")
			e := f.s.Archive(context.Background(), repo, sha, dest)
			if tc.want == nil {
				if e != nil {
					t.Fatal(e)
				}
			} else {
				result(t, e, tc.want)
				var ge *git.Error
				if errors.As(e, &ge) {
					t.Fatal("size failure carries git error")
				}
			}
			if got := list(t, dest); !reflect.DeepEqual(got, tc.paths) {
				t.Fatalf("paths %v want %v", got, tc.paths)
			}
			if len(tc.paths) > 0 && read(t, filepath.Join(dest, "a.txt")) != strings.Repeat("a", tc.a) {
				t.Fatal("partial prior file")
			}
			assertGone(t, marker)
		})
	}
}

// R-9GIG-NVQJ R-IJA5-KCEA R-IFMG-F167
func TestMissingBlobAndSizePrecedence(t *testing.T) {
	for _, max := range []int64{1024, 200000} {
		t.Run(strconv.FormatInt(max, 10), func(t *testing.T) {
			f := newFixture(t, max)
			f.bare()
			a := f.blob(strings.Repeat("a", 65536))
			b := f.blob("later missing content")
			sha := f.commit(f.tree("100644 blob "+a+"\ta.txt", "100644 blob "+b+"\tb.txt"))
			if e := os.Remove(filepath.Join(f.dir, "objects", b[:2], b[2:])); e != nil {
				t.Fatal(e)
			}
			dest := filepath.Join(f.root, "tree")
			e := f.s.Archive(context.Background(), repo, sha, dest)
			var ge *git.Error
			if max == 1024 {
				result(t, e, limits.ErrTooLarge)
				if errors.As(e, &ge) || len(list(t, dest)) != 0 {
					t.Fatal("size precedence")
				}
			} else {
				if !errors.As(e, &ge) || !strings.Contains(ge.Stderr, b) {
					t.Fatalf("git failure %v", e)
				}
				partial := read(t, filepath.Join(dest, "a.txt"))
				if !strings.HasPrefix(strings.Repeat("a", 65536), partial) {
					t.Fatal("not a prefix")
				}
			}
		})
	}
}

// R-IKI1-Y44Z
func TestHostileTree(t *testing.T) {
	f := newFixture(t, 1024)
	f.bare()
	outside := filepath.Join(f.root, "outside")
	if e := os.Mkdir(outside, 0700); e != nil {
		t.Fatal(e)
	}
	before := snapshot(t, outside)
	child := f.tree("100644 blob " + f.blob("evil") + "\tf")
	link := f.blob(outside)
	tree := f.tree("120000 blob "+link+"\td", "040000 tree "+child+"\td")
	sha := f.commit(tree)
	e := f.s.Archive(context.Background(), repo, sha, filepath.Join(f.root, "tree"))
	generic(t, e)
	if !reflect.DeepEqual(before, snapshot(t, outside)) {
		t.Fatal("escaped tree")
	}
	duplicate := f.commit(f.tree("100644 blob "+f.blob("one")+"\tf", "100644 blob "+f.blob("two")+"\tf"))
	generic(t, f.s.Archive(context.Background(), repo, duplicate, filepath.Join(f.root, "duplicates")))
}

// R-IAQU-VY7F R-I735-QMZC R-IQLJ-UYUG
func TestGitRemoved(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.simple()
	if e := os.Remove(f.link); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	_, e := f.s.Owner(ctx, repo)
	result(t, e, git.ErrNotFound)
	_, e = f.s.Resolve(ctx, repo, "main")
	result(t, e, git.ErrNotFound)
	result(t, f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "tree")), git.ErrNotFound)
	if n, ok := f.s.Name(ctx, repo); n != "" || ok {
		t.Fatal("name after removal")
	}
	if f.timers.Load() != 4 {
		t.Fatalf("start attempts %d", f.timers.Load())
	}
}
func markerProcesses(t *testing.T, marker string) []int {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	if e != nil {
		t.Fatal(e)
	}
	var found []int
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if e == nil && bytes.Contains(b, []byte(marker+"\x00")) {
			found = append(found, pid)
		}
	}
	return found
}
func awaitGit(t *testing.T, marker string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if len(markerProcesses(t, marker)) > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("git did not start")
		default:
			runtime.Gosched()
		}
	}
}
func assertGone(t *testing.T, marker string) {
	t.Helper()
	if p := markerProcesses(t, marker); len(p) > 0 {
		t.Fatalf("git remains %v", p)
	}
}
func waitError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case e := <-done:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("git did not exit")
		return nil
	}
}
func (f *fixture) hold() string {
	f.t.Helper()
	fifo := filepath.Join(f.root, "trace")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		f.t.Fatal(e)
	}
	marker := "SCRIPTS_HELD_GIT=" + f.root
	f.env = append(f.env, "GIT_TRACE="+fifo, marker)
	return marker
}

// R-IMXU-PNMD R-IO5R-3FD2 R-IPDN-H73R R-I735-QMZC
func TestCancellation(t *testing.T) {
	for _, operation := range []string{"owner", "resolve", "archive", "name"} {
		for _, kind := range []string{"deadline", "halt", "caller", "ready", "before-halt", "before-caller"} {
			t.Run(operation+"/"+kind, func(t *testing.T) {
				f := newFixture(t, 1024)
				sha := f.simple()
				marker := f.hold()
				timer := make(chan time.Time, 1)
				f.after = func(time.Duration) <-chan time.Time { return timer }
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				cause := errors.New("request gone")
				want := limits.ErrTimedOut
				switch kind {
				case "halt", "before-halt":
					want = limits.ErrHalted
				case "caller", "before-caller":
					want = cause
				}
				switch kind {
				case "ready":
					timer <- time.Time{}
				case "before-halt":
					f.l.Halt()
				case "before-caller":
					cancel(cause)
				}
				done := make(chan error, 1)
				go func() {
					var e error
					switch operation {
					case "owner":
						_, e = f.s.Owner(ctx, repo)
					case "resolve":
						_, e = f.s.Resolve(ctx, repo, "main")
					case "archive":
						e = f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "tree"))
					case "name":
						n, ok := f.s.Name(ctx, repo)
						if ok || n != "" {
							e = errors.New("name returned")
						}
					}
					done <- e
				}()
				switch kind {
				case "deadline", "halt", "caller":
					awaitGit(t, marker)
					switch kind {
					case "deadline":
						timer <- time.Time{}
					case "halt":
						f.l.Halt()
					case "caller":
						cancel(cause)
					}
				}
				e := waitError(t, done)
				if operation == "name" {
					if e != nil {
						t.Fatal(e)
					}
				} else {
					result(t, e, want)
				}
				assertGone(t, marker)
			})
		}
	}
}

// R-ID6N-NHOT R-ILPY-BVVO
func TestUnwritableTree(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.simple()
	parent := filepath.Join(f.root, "parent")
	if e := os.Mkdir(parent, 0500); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := chmodFixture(t, parent, 0700); e != nil {
			t.Error(e)
		}
	})
	generic(t, f.s.Archive(context.Background(), repo, sha, filepath.Join(parent, "tree")))
	if f.timers.Load() != 0 {
		t.Fatal("operation before mkdir")
	}
	marker := f.hold()
	dest := filepath.Join(f.root, "tree")
	done := make(chan error, 1)
	go func() { done <- f.s.Archive(context.Background(), repo, sha, dest) }()
	awaitGit(t, marker)
	if e := chmodFixture(t, dest, 0500); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := chmodFixture(t, dest, 0700); e != nil {
			t.Error(e)
		}
	})
	// Opening the trace FIFO releases git's trace write after permissions were removed.
	fifo, e := os.Open(filepath.Join(f.root, "trace"))
	if e != nil {
		t.Fatal(e)
	}
	go func() { _, _ = io.Copy(io.Discard, fifo); _ = fifo.Close() }()
	generic(t, waitError(t, done))
	assertGone(t, marker)
}

// R-I0ZN-TS9V
func TestExactGitArguments(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.simple()
	trace := filepath.Join(f.root, "trace-file")
	f.env = append(f.env, "GIT_TRACE="+trace)
	ctx := context.Background()
	_, _ = f.s.Owner(ctx, repo)
	_, _ = f.s.Name(ctx, repo)
	_, _ = f.s.Resolve(ctx, repo, "main")
	if e := f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "tree")); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSuffix(read(t, trace), "\n"), "\n")
	if len(lines) != 4 || f.timers.Load() != 4 {
		t.Fatalf("commands %v", lines)
	}
	suffixes := []string{"built-in: git config --file " + f.dir + "/config --get ikigenba.owner", "built-in: git config --file " + f.dir + "/config --get ikigenba.name", "built-in: git rev-parse --verify -q --end-of-options 'main^{commit}'", "built-in: git archive --format=tar " + sha}
	for i, line := range lines {
		if !strings.HasSuffix(line, suffixes[i]) {
			t.Fatalf("trace %q want suffix %q", line, suffixes[i])
		}
	}
}

// R-HNKR-MB48
func TestProcessStreamsUntouched(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.simple()
	out, e := os.CreateTemp(f.root, "stdout")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := out.Close(); e != nil {
			t.Error(e)
		}
	}()
	errout, e := os.CreateTemp(f.root, "stderr")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := errout.Close(); e != nil {
			t.Error(e)
		}
	}()
	oldout, olderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errout
	defer func() { os.Stdout, os.Stderr = oldout, olderr }()
	ctx := context.Background()
	_, _ = f.g.Output(ctx, "", "--git-dir="+filepath.Join(f.root, "missing"), "rev-parse", "main")
	_ = f.g.Command(ctx, "", "--git-dir="+filepath.Join(f.root, "missing"), "rev-parse", "main").Run()
	_, _ = f.s.Owner(ctx, repo)
	_, _ = f.s.Name(ctx, repo)
	_, _ = f.s.Resolve(ctx, repo, "missing")
	_ = f.s.Archive(ctx, repo, strings.Repeat("0", 40), filepath.Join(f.root, "failed-tree"))
	_ = f.s.Archive(ctx, repo, sha, filepath.Join(f.root, "tree"))
	_ = f.s.RepoDir(repo)
	if st, e := out.Stat(); e != nil || st.Size() != 0 {
		t.Fatal("wrote stdout")
	}
	if st, e := errout.Stat(); e != nil || st.Size() != 0 {
		t.Fatal("wrote stderr")
	}
}

func chmodFixture(t *testing.T, p string, mode os.FileMode) error {
	t.Helper()
	r, e := os.OpenRoot(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer func() {
		if e := r.Close(); e != nil {
			t.Error(e)
		}
	}()
	return r.Chmod(filepath.Base(p), mode)
}

// R-I27K-7K0K
func TestArchivePreservesWholeRepositoryRoot(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.simple()
	alias := filepath.Join(f.root, "repos-alias")
	if e := os.Symlink(f.repos, alias); e != nil {
		t.Fatal(e)
	}
	externalAlias := filepath.Join(f.repos, "outside")
	if e := os.Symlink(f.root, externalAlias); e != nil {
		t.Fatal(e)
	}
	for _, dest := range []string{f.repos, filepath.Join(f.repos, "new-tree"), filepath.Join(alias, "new-tree"), f.root, filepath.Join(f.dir, "new-tree"), alias + "/../repos/new-tree"} {
		before := snapshot(t, f.repos)
		if e := f.s.Archive(context.Background(), repo, sha, dest); e == nil {
			t.Fatalf("overlapping destination accepted: %s", dest)
		}
		if !reflect.DeepEqual(before, snapshot(t, f.repos)) {
			t.Fatalf("repository root changed: %s", dest)
		}
	}
	aliasedSource := source.New(source.Config{Repos: alias, Git: f.g, Limits: f.l})
	beforeAlias := snapshot(t, f.repos)
	if e := aliasedSource.Archive(context.Background(), repo, sha, filepath.Join(f.repos, "aliased-root-tree")); e == nil {
		t.Fatal("aliased root destination accepted")
	}
	if !reflect.DeepEqual(beforeAlias, snapshot(t, f.repos)) {
		t.Fatal("aliased repository root changed")
	}
	// A pathname through an alias under Repos that physically lands outside is safe.
	dest := filepath.Join(externalAlias, "safe-tree")
	before := snapshot(t, f.repos)
	if e := f.s.Archive(context.Background(), repo, sha, dest); e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(f.root, "safe-tree", "main.py")) != "print('hello')\n" {
		t.Fatal("safe archive bytes")
	}
	if !reflect.DeepEqual(before, snapshot(t, f.repos)) {
		t.Fatal("safe extraction changed root")
	}
}
