package cache_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
)

const repo = "rep_0123456789abcdef"
const site = "sit_0123456789abcdef"

type fixture struct {
	t                              *testing.T
	base, binary, repos, root, dir string
	env                            []string
	g                              *git.Git
	l                              *limits.Limits
	c                              *cache.Cache
	operations                     atomic.Int64
}

func newFixture(t *testing.T, byteLimit int64) *fixture {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, base: t.TempDir(), binary: binary}
	f.repos = filepath.Join(f.base, "repos")
	f.root = filepath.Join(f.base, "cache")
	f.dir = filepath.Join(f.repos, repo+".git")
	f.env = []string{"HOME=" + f.base, "XDG_CONFIG_HOME=" + f.base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	f.g, err = git.Find(filepath.Dir(binary), func() []string { return f.env })
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Defaults()
	s.SiteMaxBytes = byteLimit
	f.l = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { f.operations.Add(1); return make(chan time.Time) }})
	f.open(nil, nil)
	f.run(nil, "init", "--bare", "--initial-branch=main", f.dir)
	f.run(nil, "config", "--file", filepath.Join(f.dir, "config"), "ikigenba.id", repo)
	f.run(nil, "config", "--file", filepath.Join(f.dir, "config"), "ikigenba.name", "fixture")
	f.run(nil, "config", "--file", filepath.Join(f.dir, "config"), "ikigenba.owner", "u_fixture")
	f.run(nil, "config", "--file", filepath.Join(f.dir, "config"), "ikigenba.created", "2000-01-01T00:00:00Z")
	return f
}
func (f *fixture) open(joined, unpacked func(string, string)) {
	f.t.Helper()
	var err error
	f.c, err = cache.Open(cache.Config{Root: f.root, Repos: f.repos, Git: f.g, Limits: f.l, Joined: joined, Unpacked: unpacked})
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) run(in io.Reader, args ...string) string {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Env = f.env
	cmd.Stdin = in
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}
func (f *fixture) blob(data string) string {
	return f.run(strings.NewReader(data), "--git-dir="+f.dir, "hash-object", "-w", "--stdin")
}
func (f *fixture) tree(entries string) string {
	return f.run(strings.NewReader(entries), "--git-dir="+f.dir, "mktree")
}
func (f *fixture) commit(tree string) string {
	sha := f.run(strings.NewReader("fixture\n"), "--git-dir="+f.dir, "commit-tree", tree)
	f.run(nil, "--git-dir="+f.dir, "update-ref", "refs/heads/main", sha)
	return sha
}
func (f *fixture) file(data string) string {
	return f.commit(f.tree("100644 blob " + f.blob(data) + "\tindex.html\n"))
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func absent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", p, err)
	}
}
func entries(t *testing.T, p string) []os.DirEntry {
	t.Helper()
	es, err := os.ReadDir(p)
	if os.IsNotExist(err) {
		return nil
	}
	must(t, err)
	return es
}
func wait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not complete")
		var zero T
		return zero
	}
}

// R-W0UB-LYRE R-ADX7-SQQD R-CFLW-J1N3 R-AW7P-JAUS
func TestOpenAndPaths(t *testing.T) {
	f := newFixture(t, 1024)
	must(t, os.WriteFile(filepath.Join(f.root, "keep"), []byte("unchanged"), 0600))
	c, err := cache.Open(cache.Config{Root: f.root, Repos: filepath.Join(f.base, "absent"), Git: f.g, Limits: f.l})
	must(t, err)
	if c.RepoDir(repo) != filepath.Join(f.base, "absent", repo+".git") || c.Dir(site, "sha") != filepath.Join(f.root, site, "sha") {
		t.Fatal("paths")
	}
	got, err := readFile(filepath.Join(f.root, "keep"))
	must(t, err)
	if string(got) != "unchanged" {
		t.Fatal("changed entry")
	}
	blocked := filepath.Join(f.base, "file")
	must(t, os.WriteFile(blocked, []byte("file"), 0600))
	c, err = cache.Open(cache.Config{Root: filepath.Join(blocked, "child"), Git: f.g, Limits: f.l})
	if err == nil || c != nil {
		t.Fatal("opened below file")
	}
	got, err = readFile(blocked)
	must(t, err)
	if string(got) != "file" {
		t.Fatal("changed blocker")
	}
}

// R-AGD0-KA7R R-AXFL-X2LH R-AZVE-OM2V
func TestValidation(t *testing.T) {
	if cache.RepoPrefix != "rep_" || !cache.ValidRepo(repo) {
		t.Fatal("repository declaration")
	}
	for _, s := range []string{"", "rep_", "rep_0123456789abcdeF", "rep_0123456789abcdef0", "../" + repo} {
		if cache.ValidRepo(s) {
			t.Fatalf("valid repo %q", s)
		}
	}
	for _, s := range []string{"main", "v1", "v1.0", "refs/heads/main", "a/b/c", "-foo", "@a", "é"} {
		if !cache.ValidRef(s) {
			t.Fatalf("invalid ref %q", s)
		}
	}
	for _, s := range []string{"..bad", "", "a b", "a~", "a^", "a:b", "a?", "a*", "a[", "a\\b", "a/", "/a", "a//b", "a.lock", ".a", "a/.b", "a.", "a@{b", "@", "a\tb", "a\x7fb"} {
		if cache.ValidRef(s) {
			t.Fatalf("valid ref %q", s)
		}
	}
}

// R-APOF-7MQ2 R-AIST-BTP5 R-CWOH-VU0T
func TestReasons(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{{cache.ErrRepositoryMissing, "repository_missing"}, {cache.ErrNoCommit, "commit_missing"}, {limits.ErrTooLarge, "too_large"}, {limits.ErrTimedOut, "timed_out"}, {limits.ErrHalted, "git_failed"}, {git.ErrNotFound, "git_failed"}, {&git.Error{Status: 128}, "git_failed"}, {errors.New("hostile"), "git_failed"}}
	for i, c := range cases {
		if c.err == nil || cache.Reason(fmt.Errorf("wrapped: %w", c.err)) != c.want {
			t.Fatal(c)
		}
		if i < 2 {
			for j, d := range cases {
				if i != j && errors.Is(c.err, d.err) {
					t.Fatal("sentinels overlap")
				}
			}
		}
	}
	if cache.ReasonRepositoryMissing != "repository_missing" || cache.ReasonCommitMissing != "commit_missing" || cache.ReasonTooLarge != "too_large" || cache.ReasonTimedOut != "timed_out" || cache.ReasonGitFailed != "git_failed" {
		t.Fatal("constants")
	}
}

// R-QRJA-2NXS R-B4R0-7P1N R-B5YW-LGSC R-B76S-Z8J1 R-B8EP-D09Q R-D1K3-EWZL
func TestRepositoryReads(t *testing.T) {
	f := newFixture(t, 1024)
	ctx := context.Background()
	sha := f.file("hello")
	owner, err := f.c.Owner(ctx, repo)
	must(t, err)
	if owner != "u_fixture" {
		t.Fatal(owner)
	}
	f.run(nil, "config", "--file", filepath.Join(f.dir, "config"), "--add", "ikigenba.owner", "u_last")
	owner, err = f.c.Owner(ctx, repo)
	must(t, err)
	if owner != "u_last" {
		t.Fatal(owner)
	}
	global := filepath.Join(f.base, "global")
	must(t, os.WriteFile(global, []byte("[ikigenba]\nowner = u_global\n"), 0600))
	f.env = append(f.env, "GIT_CONFIG_GLOBAL="+global)
	f.run(nil, "config", "--file", filepath.Join(f.dir, "config"), "--unset-all", "ikigenba.owner")
	owner, err = f.c.Owner(ctx, repo)
	must(t, err)
	if owner != "" {
		t.Fatal("global owner leaked")
	}
	f.run(nil, "--git-dir="+f.dir, "tag", "light", sha)
	f.run(nil, "--git-dir="+f.dir, "tag", "-a", "annotated", "-m", "fixture", sha)
	f.run(nil, "--git-dir="+f.dir, "update-ref", "refs/heads/-foo", sha)
	for _, ref := range []string{"main", "light", "annotated", sha, sha[:12], "-foo"} {
		got, e := f.c.Resolve(ctx, repo, ref)
		must(t, e)
		if got != sha {
			t.Fatal(ref, got)
		}
	}
	tree := f.run(nil, "--git-dir="+f.dir, "rev-parse", "main^{tree}")
	for _, ref := range []string{"absent", "..bad", "", "--all", strings.Repeat("0", 40), tree} {
		_, e := f.c.Resolve(ctx, repo, ref)
		if !errors.Is(e, cache.ErrNoCommit) {
			t.Fatal(ref, e)
		}
	}
	for _, r := range []string{"bad", "rep_1111111111111111"} {
		n := f.operations.Load()
		_, e := f.c.Owner(ctx, r)
		if !errors.Is(e, cache.ErrRepositoryMissing) {
			t.Fatal(e)
		}
		_, e = f.c.Resolve(ctx, r, "main")
		if !errors.Is(e, cache.ErrRepositoryMissing) || n != f.operations.Load() {
			t.Fatal(e, "git started")
		}
	}
	dir := filepath.Join(f.repos, "rep_1111111111111111.git")
	must(t, os.MkdirAll(dir, 0700))
	owner, err = f.c.Owner(ctx, "rep_1111111111111111")
	must(t, err)
	if owner != "" {
		t.Fatal(owner)
	}
	_, err = f.c.Resolve(ctx, "rep_1111111111111111", "main")
	var ge *git.Error
	if !errors.As(err, &ge) || ge.Status != 128 {
		t.Fatal(err)
	}
	if len(entries(t, f.root)) != 0 {
		t.Fatal("reads wrote cache")
	}
}

type snapshot struct {
	mode  os.FileMode
	size  int64
	mtime time.Time
	bytes string
}

func snap(t *testing.T, root string) map[string]snapshot {
	t.Helper()
	out := make(map[string]snapshot)
	must(t, filepath.Walk(root, func(p string, st os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		b := ""
		if st.Mode().IsRegular() {
			data, err := readFile(p)
			if err != nil {
				return err
			}
			b = string(data)
		}
		out[p] = snapshot{st.Mode(), st.Size(), st.ModTime(), b}
		return nil
	}))
	return out
}

// R-TP37-94N7 R-TNVA-VCWI R-7PSE-YBDY R-TMNE-HL5T
func TestReadsAreBoundedReadOnlyAndSilent(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("bytes")
	trace := filepath.Join(f.base, "trace")
	f.env = append(f.env, "GIT_TRACE="+trace)
	before := snap(t, f.repos)
	// Redirect the process streams; cache calls are synchronous and tests are not parallel.
	stdout, stderr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	must(t, err)
	os.Stdout = w
	os.Stderr = w
	_, ownerErr := f.c.Owner(context.Background(), repo)
	_, resolveErr := f.c.Resolve(context.Background(), repo, "main")
	unpackErr := f.c.Unpack(context.Background(), site, repo, sha)
	os.Stdout = stdout
	os.Stderr = stderr
	must(t, w.Close())
	data, readErr := io.ReadAll(r)
	must(t, r.Close())
	must(t, readErr)
	must(t, ownerErr)
	must(t, resolveErr)
	must(t, unpackErr)
	if len(data) != 0 {
		t.Fatal(string(data))
	}
	after := snap(t, f.repos)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("repository changed")
	}
	traceBytes, err := readFile(trace)
	must(t, err)
	s := string(traceBytes)
	if strings.Count(s, "built-in: git") != 3 || !strings.Contains(s, "config --file "+filepath.Join(f.dir, "config")+" --get ikigenba.owner") || !strings.Contains(s, "rev-parse --verify -q --end-of-options 'main^{commit}'") || !strings.Contains(s, "archive --format=tar "+sha) || f.operations.Load() != 3 {
		t.Fatal(s, f.operations.Load())
	}
	n := f.operations.Load()
	must(t, f.c.Unpack(context.Background(), site, "bad", sha))
	_, err = f.c.Tree(context.Background(), site, "bad", sha)
	must(t, err)
	if f.operations.Load() != n {
		t.Fatal("cached tree starts git")
	}
}

// R-BC2E-IBHT R-BDAA-W38I R-LX6D-TGK3 R-CI1P-AL4H R-CZ4A-NDI7
func TestWholeArchiveAndHook(t *testing.T) {
	f := newFixture(t, 15)
	blob := f.blob("hello")
	nested := f.tree("100644 blob " + blob + "\tindex.html\n")
	link := f.blob("index.html")
	tree := f.tree("100644 blob " + blob + "\t.env\n040000 tree " + nested + "\tabout\n100644 blob " + blob + "\tindex.html\n120000 blob " + link + "\tlink.html\n")
	sha := f.commit(tree)
	trace := filepath.Join(f.base, "trace2")
	f.env = append(f.env, "GIT_TRACE2_EVENT="+trace)
	count := 0
	f.open(nil, func(gotSite, gotSHA string) {
		count++
		if gotSite != site || gotSHA != sha {
			t.Fatal("hook args")
		}
		absent(t, f.c.Dir(site, sha))
		es := entries(t, filepath.Join(f.root, site))
		if len(es) != 1 || !strings.HasPrefix(es[0].Name(), ".") {
			t.Fatal("staging", es)
		}
		for _, p := range []string{"index.html", ".env", "about/index.html"} {
			b, e := readFile(filepath.Join(f.root, site, es[0].Name(), p))
			must(t, e)
			if string(b) != "hello" {
				t.Fatal(p)
			}
		}
		b, e := readFile(trace)
		must(t, e)
		var archiveSID string
		exits := make(map[string]int)
		decoder := json.NewDecoder(strings.NewReader(string(b)))
		for {
			var event struct {
				Event string   `json:"event"`
				SID   string   `json:"sid"`
				Argv  []string `json:"argv"`
				Code  int      `json:"code"`
			}
			e := decoder.Decode(&event)
			if errors.Is(e, io.EOF) {
				break
			}
			must(t, e)
			if event.Event == "start" {
				for _, arg := range event.Argv {
					if arg == "archive" {
						archiveSID = event.SID
					}
				}
			}
			if event.Event == "exit" {
				exits[event.SID] = event.Code
			}
		}
		code, ok := exits[archiveSID]
		if archiveSID == "" || !ok || code != 0 {
			t.Fatal("archive not exited", string(b))
		}
	})
	f.l.Drain()
	must(t, f.c.Unpack(context.Background(), site, repo, sha))
	if count != 1 {
		t.Fatal(count)
	}
	assertTree(t, f.c.Dir(site, sha), map[string]treeEntry{".env": {"file", "hello"}, "about": {"dir", ""}, "about/index.html": {"file", "hello"}, "index.html": {"file", "hello"}, "link.html": {"link", "index.html"}})
	linkname, err := os.Readlink(filepath.Join(f.c.Dir(site, sha), "link.html"))
	must(t, err)
	if linkname != "index.html" {
		t.Fatal(linkname)
	}
	before := snap(t, f.c.Dir(site, sha))
	must(t, f.c.Unpack(context.Background(), site, "invalid", sha))
	if count != 1 || fmt.Sprint(before) != fmt.Sprint(snap(t, f.c.Dir(site, sha))) {
		t.Fatal("existing tree changed")
	}
}

// R-BC2E-IBHT
func TestEmptyTreeAndSubmodule(t *testing.T) {
	f := newFixture(t, 1024)
	empty := f.tree("")
	sha := f.commit(empty)
	must(t, f.c.Unpack(context.Background(), site, repo, sha))
	if len(entries(t, f.c.Dir(site, sha))) != 0 {
		t.Fatal("empty tree")
	}
	sub := f.tree("160000 commit " + sha + "\tsubmodule\n")
	sha = f.commit(sub)
	must(t, f.c.Unpack(context.Background(), site, repo, sha))
	es := entries(t, f.c.Dir(site, sha))
	if len(es) != 1 || !es[0].IsDir() || len(entries(t, filepath.Join(f.c.Dir(site, sha), "submodule"))) != 0 {
		t.Fatal("submodule")
	}
}

// R-CKHI-24LV R-XHH3-YMVR R-QQBD-OW73
func TestSizeFailuresAndDiskFailure(t *testing.T) {
	for _, size := range []int{1024, 1025} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f := newFixture(t, 1024)
			sha := f.file(strings.Repeat("x", size))
			err := f.c.Unpack(context.Background(), site, repo, sha)
			if size == 1024 {
				must(t, err)
			} else {
				if !errors.Is(err, limits.ErrTooLarge) {
					t.Fatal(err)
				}
				absent(t, filepath.Join(f.root, site))
			}
		})
	}
	f := newFixture(t, 1024)
	blob := f.blob(strings.Repeat("x", 513))
	sha := f.commit(f.tree("100644 blob " + blob + "\ta\n100644 blob " + blob + "\tb\n"))
	if err := f.c.Unpack(context.Background(), site, repo, sha); !errors.Is(err, limits.ErrTooLarge) {
		t.Fatal(err)
	}
	absent(t, filepath.Join(f.root, site))
	old := f.file("old")
	must(t, f.c.Unpack(context.Background(), site, repo, old))
	sha = f.file("new")
	object := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+":index.html")
	must(t, os.Remove(filepath.Join(f.dir, "objects", object[:2], object[2:])))
	err := f.c.Unpack(context.Background(), site, repo, sha)
	var ge *git.Error
	if !errors.As(err, &ge) || ge.Status == 0 || ge.Stderr == "" {
		t.Fatal(err)
	}
	absent(t, f.c.Dir(site, sha))
	if len(entries(t, filepath.Join(f.root, site))) != 1 {
		t.Fatal("failed unpack debris")
	}
	b, e := readFile(filepath.Join(f.c.Dir(site, old), "index.html"))
	must(t, e)
	if string(b) != "old" {
		t.Fatal("old tree changed")
	}
	sha = f.file("writable")
	parent := filepath.Join(f.root, site)
	must(t, chmodDir(parent, 0500))
	t.Cleanup(func() { _ = chmodDir(parent, 0700) })
	before := snap(t, f.root)
	err = f.c.Unpack(context.Background(), site, repo, sha)
	if err == nil || errors.As(err, &ge) || cache.Reason(err) != cache.ReasonGitFailed || fmt.Sprint(before) != fmt.Sprint(snap(t, f.root)) {
		t.Fatal("disk failure", err)
	}
}

// R-CKHI-24LV
func TestHeaderSizeWinsLaterGitFailure(t *testing.T) {
	f := newFixture(t, 1024)
	a := f.blob(strings.Repeat("x", 65536))
	b := f.blob("missing")
	sha := f.commit(f.tree("100644 blob " + a + "\ta\n100644 blob " + b + "\tb\n"))
	must(t, os.Remove(filepath.Join(f.dir, "objects", b[:2], b[2:])))
	err := f.c.Unpack(context.Background(), site, repo, sha)
	var ge *git.Error
	if !errors.Is(err, limits.ErrTooLarge) || errors.As(err, &ge) {
		t.Fatal(err)
	}
	absent(t, filepath.Join(f.root, site))
}

// R-V9WH-81V9
func TestHostileTrees(t *testing.T) {
	for _, kind := range []string{"link-parent", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 1024)
			outside := filepath.Join(f.base, "outside")
			must(t, os.Mkdir(outside, 0700))
			blob := f.blob("hello")
			sub := f.tree("100644 blob " + blob + "\tf\n")
			var tree string
			if kind == "link-parent" {
				link := f.blob(outside)
				tree = f.tree("120000 blob " + link + "\td\n040000 tree " + sub + "\td\n")
			} else {
				tree = f.tree("100644 blob " + blob + "\tf\n100644 blob " + blob + "\tf\n")
			}
			sha := f.commit(tree)
			err := f.c.Unpack(context.Background(), site, repo, sha)
			var ge *git.Error
			if err == nil || errors.As(err, &ge) || errors.Is(err, cache.ErrNoCommit) || errors.Is(err, limits.ErrTooLarge) {
				t.Fatal(err)
			}
			absent(t, filepath.Join(outside, "f"))
			absent(t, filepath.Join(f.root, site))
		})
	}
}

// R-BJDS-SXXZ R-B9ML-QS0F R-BPHA-PSNG
func TestInvalidAndMissingGit(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	for _, bad := range []string{"", "..", "sit_0123456789abcdeF"} {
		if f.c.Unpack(context.Background(), bad, repo, sha) == nil {
			t.Fatal(bad)
		}
		if _, err := f.c.Tree(context.Background(), bad, repo, sha); err == nil {
			t.Fatal(bad)
		}
		if f.c.Prune(bad, sha) == nil || f.c.Remove(bad) == nil {
			t.Fatal(bad)
		}
	}
	for _, bad := range []string{"", sha[:39], strings.ToUpper(sha)} {
		if !errors.Is(f.c.Unpack(context.Background(), site, repo, bad), cache.ErrNoCommit) {
			t.Fatal(bad)
		}
		if _, err := f.c.Tree(context.Background(), site, repo, bad); !errors.Is(err, cache.ErrNoCommit) {
			t.Fatal(err)
		}
	}
	must(t, f.c.Unpack(context.Background(), site, repo, sha))
	bin := filepath.Join(f.base, "bin")
	must(t, os.Mkdir(bin, 0700))
	link := filepath.Join(bin, "git")
	must(t, os.Symlink(f.binary, link))
	g, err := git.Find(bin, func() []string { return f.env })
	must(t, err)
	f.g = g
	f.open(nil, nil)
	must(t, os.Remove(link))
	n := f.operations.Load()
	f.l.Drain()
	dir, err := f.c.Tree(context.Background(), site, "invalid", sha)
	must(t, err)
	if dir != f.c.Dir(site, sha) || n != f.operations.Load() {
		t.Fatal("existing tree")
	}
	_, err = f.c.Owner(context.Background(), repo)
	if !errors.Is(err, git.ErrNotFound) {
		t.Fatal(err)
	}
	_, err = f.c.Resolve(context.Background(), repo, "main")
	if !errors.Is(err, git.ErrNotFound) {
		t.Fatal(err)
	}
	newSHA := f.file("other")
	err = f.c.Unpack(context.Background(), site, repo, newSHA)
	if !errors.Is(err, git.ErrNotFound) {
		t.Fatal(err)
	}
}

// R-D0C7-158W
func TestRelativeRepositories(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("relative")
	t.Chdir(f.base)
	c, err := cache.Open(cache.Config{Root: f.root, Repos: "repos", Git: f.g, Limits: f.l})
	must(t, err)
	owner, err := c.Owner(context.Background(), repo)
	must(t, err)
	if owner != "u_fixture" {
		t.Fatal(owner)
	}
	got, err := c.Resolve(context.Background(), repo, "main")
	must(t, err)
	if got != sha {
		t.Fatal(got)
	}
	must(t, c.Unpack(context.Background(), site, repo, sha))
}

// R-CT0S-QISQ R-UQPP-5LCD R-UT5H-X4TR
func TestLazyRebuildAndRecovery(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	saved := f.dir + ".saved"
	must(t, os.Rename(f.dir, saved))
	_, err := f.c.Tree(context.Background(), site, repo, sha)
	if !errors.Is(err, cache.ErrRepositoryMissing) {
		t.Fatal(err)
	}
	must(t, os.Rename(saved, f.dir))
	for i := 0; i < 3; i++ {
		dir, e := f.c.Tree(context.Background(), site, repo, sha)
		must(t, e)
		if dir != f.c.Dir(site, sha) {
			t.Fatal(dir)
		}
		n := f.operations.Load()
		_, e = f.c.Tree(context.Background(), site, repo, sha)
		must(t, e)
		if n != f.operations.Load() {
			t.Fatal("cached git")
		}
		must(t, os.RemoveAll(f.root))
	}
	_, err = f.c.Tree(context.Background(), site, repo, strings.Repeat("0", 40))
	if !errors.Is(err, cache.ErrNoCommit) {
		t.Fatal(err)
	}
}

// R-CU8P-4AJF R-CVGL-I2A4 R-BUCW-8VM8 R-BPHA-PSNG
func TestSharedRebuildAndCancellation(t *testing.T) {
	f := newFixture(t, 1024)
	old := f.file("old")
	must(t, f.c.Unpack(context.Background(), site, repo, old))
	sha := f.file("new")
	held := make(chan struct{})
	release := make(chan struct{})
	joined := make(chan struct{}, 3)
	f.open(func(string, string) { joined <- struct{}{} }, func(_, s string) {
		if s == sha {
			close(held)
			<-release
		}
	})
	firstCtx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("visitor left")
	first := make(chan error, 1)
	go func() { _, e := f.c.Tree(firstCtx, site, repo, sha); first <- e }()
	wait(t, held)
	secondCtx, cancelSecond := context.WithCancelCause(context.Background())
	second := make(chan error, 1)
	go func() { _, e := f.c.Tree(secondCtx, site, repo, sha); second <- e }()
	wait(t, joined)
	cancel(cause)
	if err := wait(t, first); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	cancelSecond(cause)
	if err := wait(t, second); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	f.l.Drain()
	third := make(chan error, 1)
	go func() { _, e := f.c.Tree(context.Background(), site, repo, sha); third <- e }()
	wait(t, joined)
	n := f.operations.Load()
	_, err := f.c.Tree(context.Background(), site, "invalid", old)
	must(t, err)
	if n != f.operations.Load() {
		t.Fatal("present tree blocked")
	}
	_, err = f.c.Tree(context.Background(), "sit_1111111111111111", repo, sha)
	if !errors.Is(err, limits.ErrDraining) {
		t.Fatal(err)
	}
	close(release)
	must(t, wait(t, third))
	if f.operations.Load() != 3 {
		t.Fatal("unexpected git count", f.operations.Load())
	}
}

// R-CQKZ-YZBC R-CRSW-CR21 R-VHXY-ASJ4
func TestPruneRemoveAndCompetingInstall(t *testing.T) {
	for _, mode := range []string{"prune", "remove", "compete"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, 1024)
			old := f.file("old")
			must(t, f.c.Unpack(context.Background(), site, repo, old))
			sha := f.file("new")
			held := make(chan struct{})
			release := make(chan struct{})
			f.open(nil, func(_, s string) {
				if s == sha {
					close(held)
					<-release
				}
			})
			result := make(chan error, 1)
			go func() { result <- f.c.Unpack(context.Background(), site, repo, sha) }()
			wait(t, held)
			switch mode {
			case "prune":
				must(t, os.Mkdir(filepath.Join(f.root, site, ".stale"), 0700))
				must(t, f.c.Prune(site, old))
				if len(entries(t, filepath.Join(f.root, site))) != 2 {
					t.Fatal("prune removed live staging")
				}
			case "remove":
				must(t, f.c.Remove(site))
				if len(entries(t, filepath.Join(f.root, site))) != 1 {
					t.Fatal("remove live staging")
				}
			case "compete":
				must(t, os.Mkdir(f.c.Dir(site, sha), 0700))
				must(t, os.WriteFile(filepath.Join(f.c.Dir(site, sha), "winner"), []byte("winner"), 0600))
			}
			close(release)
			must(t, wait(t, result))
			switch mode {
			case "remove":
				absent(t, filepath.Join(f.root, site))
			case "prune":
				absent(t, f.c.Dir(site, sha))
				if len(entries(t, filepath.Join(f.root, site))) != 1 {
					t.Fatal("debris")
				}
			default:
				es := entries(t, f.c.Dir(site, sha))
				if len(es) != 1 || es[0].Name() != "winner" {
					t.Fatal("winner changed")
				}
			}
		})
	}
	f := newFixture(t, 1024)
	must(t, f.c.Prune(site, "missing"))
	must(t, f.c.Remove(site))
	absent(t, filepath.Join(f.root, site))
	sha := f.file("old")
	must(t, f.c.Unpack(context.Background(), site, repo, sha))
	must(t, chmodDir(filepath.Join(f.root, site), 0500))
	t.Cleanup(func() { _ = chmodDir(filepath.Join(f.root, site), 0700) })
	if f.c.Prune(site, "other") == nil || f.c.Remove(site) == nil {
		t.Fatal("removal permission")
	}
}

// R-CO57-7FTY R-GF0D-G2J5
func TestImmediateDeadlinesAndHalt(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	s := settings.Defaults()
	f.l = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- time.Time{}; return ch }})
	f.open(nil, nil)
	for _, call := range []func() error{func() error { _, e := f.c.Owner(context.Background(), repo); return e }, func() error { _, e := f.c.Resolve(context.Background(), repo, "main"); return e }, func() error { return f.c.Unpack(context.Background(), site, repo, sha) }, func() error { _, e := f.c.Tree(context.Background(), site, repo, sha); return e }} {
		if err := call(); !errors.Is(err, limits.ErrTimedOut) {
			t.Fatal(err)
		}
	}
	f.l.Halt()
	_, err := f.c.Resolve(context.Background(), repo, "main")
	if !errors.Is(err, limits.ErrHalted) {
		t.Fatal(err)
	}
	_, err = f.c.Tree(context.Background(), site, repo, sha)
	if !errors.Is(err, limits.ErrHalted) {
		t.Fatal(err)
	}
	absent(t, filepath.Join(f.root, site))
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("caller done")
	f.l = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { cancel(cause); return make(chan time.Time) }})
	f.open(nil, nil)
	_, err = f.c.Resolve(ctx, repo, "main")
	if !errors.Is(err, cause) {
		t.Fatal(err)
	}
}

func readFile(p string) ([]byte, error) {
	f, err := os.OpenInRoot(filepath.Dir(p), filepath.Base(p))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
func chmodDir(p string, mode os.FileMode) error {
	r, err := os.OpenRoot(p)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return r.Chmod(".", mode)
}

type gitEvent struct {
	Event string   `json:"event"`
	SID   string   `json:"sid"`
	Argv  []string `json:"argv"`
	Code  int      `json:"code"`
}
type heldGit struct {
	events  *os.File
	hold    string
	decoder *json.Decoder
	pidfd   int
}

func (f *fixture) holdGit() *heldGit {
	f.t.Helper()
	eventPath := filepath.Join(f.base, "events-fifo")
	holdPath := filepath.Join(f.base, "hold-fifo")
	must(f.t, syscall.Mkfifo(eventPath, 0600))
	must(f.t, syscall.Mkfifo(holdPath, 0600))
	f.env = append(f.env, "GIT_TRACE2_EVENT="+eventPath, "GIT_TRACE="+holdPath)
	return &heldGit{hold: holdPath}
}
func (h *heldGit) started(t *testing.T, command string) {
	t.Helper()
	ready := make(chan error, 1)
	go func() {
		var err error
		h.events, err = os.OpenFile(filepath.Join(filepath.Dir(h.hold), "events-fifo"), os.O_RDONLY, 0600)
		if err != nil {
			ready <- err
			return
		}
		h.decoder = json.NewDecoder(h.events)
		for {
			var event gitEvent
			if err = h.decoder.Decode(&event); err != nil {
				ready <- err
				return
			}
			if event.Event == "start" {
				for _, arg := range event.Argv {
					if arg == command && event.SID != "" {
						marker := strings.LastIndex(event.SID, "-P")
						if marker < 0 {
							ready <- errors.New("trace SID has no PID")
							return
						}
						pid, e := strconv.ParseInt(event.SID[marker+2:], 16, 32)
						if e != nil {
							ready <- e
							return
						}
						pidNumber := int(pid)
						fd, _, errno := syscall.Syscall(434, uintptr(pidNumber), 0, 0)
						if errno != 0 {
							ready <- errno
							return
						}
						h.pidfd = int(fd)
						ready <- nil
						return
					}
				}
			}
		}
	}()
	must(t, wait(t, ready))
	t.Cleanup(func() { _ = h.events.Close(); _ = syscall.Close(h.pidfd) })
}
func (h *heldGit) release(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(h.hold, os.O_RDWR, 0600)
	must(t, err)
	t.Cleanup(func() { _ = f.Close() })
}
func (h *heldGit) exited(t *testing.T) {
	t.Helper()
	var fds syscall.FdSet
	if h.pidfd < 0 || h.pidfd >= len(fds.Bits)*64 {
		t.Fatal("pidfd out of select range")
	}
	fds.Bits[h.pidfd/64] |= int64(1) << uint(h.pidfd%64)
	n, err := syscall.Select(h.pidfd+1, &fds, nil, nil, &syscall.Timeval{})
	must(t, err)
	if n != 1 {
		t.Fatal("git had not exited when cache answered")
	}
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(h.events); done <- err }()
	must(t, wait(t, done))
}

// R-CO57-7FTY R-CPD3-L7KN R-GF0D-G2J5
func TestRunningGitStop(t *testing.T) {
	for _, method := range []string{"owner", "resolve", "unpack", "rebuild"} {
		for _, stop := range []string{"deadline", "halt", "cancel"} {
			if method == "rebuild" && stop == "cancel" {
				continue
			}
			t.Run(method+"/"+stop, func(t *testing.T) {
				f := newFixture(t, 1024)
				sha := f.file("hello")
				held := f.holdGit()
				timers := make(chan chan time.Time, 4)
				f.l = limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { timer := make(chan time.Time, 1); timers <- timer; return timer }})
				f.open(nil, nil)
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(context.Canceled)
				cause := errors.New("caller left")
				result := make(chan error, 1)
				go func() {
					var err error
					switch method {
					case "owner":
						_, err = f.c.Owner(ctx, repo)
					case "resolve":
						_, err = f.c.Resolve(ctx, repo, "main")
					case "unpack":
						err = f.c.Unpack(ctx, site, repo, sha)
					case "rebuild":
						_, err = f.c.Tree(ctx, site, repo, sha)
					}
					result <- err
				}()
				command := "archive"
				if method == "owner" {
					command = "config"
				}
				if method == "resolve" || method == "rebuild" {
					command = "rev-parse"
				}
				held.started(t, command)
				if method == "unpack" {
					absent(t, f.c.Dir(site, sha))
					for _, e := range entries(t, filepath.Join(f.root, site)) {
						if !strings.HasPrefix(e.Name(), ".") {
							t.Fatal("non-staging tree while archive running")
						}
					}
				}
				timer := wait(t, timers)
				var want error
				switch stop {
				case "deadline":
					timer <- time.Time{}
					want = limits.ErrTimedOut
				case "halt":
					f.l.Halt()
					held.exited(t)
					want = limits.ErrHalted
				case "cancel":
					cancel(cause)
					want = cause
				}
				err := wait(t, result)
				if !errors.Is(err, want) {
					t.Fatal(err)
				}
				if stop != "halt" {
					held.exited(t)
				}
				absent(t, filepath.Join(f.root, site))
			})
		}
	}
}

// R-BUCW-8VM8 R-CVGL-I2A4 R-CU8P-4AJF
func TestCancellationWhileSharedGitRuns(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	held := f.holdGit()
	joined := make(chan struct{}, 3)
	f.open(func(string, string) { joined <- struct{}{} }, nil)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	cause := errors.New("visitor left")
	first := make(chan error, 1)
	go func() { _, err := f.c.Tree(ctx, site, repo, sha); first <- err }()
	held.started(t, "rev-parse")
	joinCtx, joinCancel := context.WithCancelCause(context.Background())
	defer joinCancel(context.Canceled)
	second := make(chan error, 1)
	go func() { _, err := f.c.Tree(joinCtx, site, repo, sha); second <- err }()
	wait(t, joined)
	cancel(cause)
	joinCancel(cause)
	if err := wait(t, first); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err := wait(t, second); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	f.l.Drain()
	third := make(chan error, 1)
	go func() { _, err := f.c.Tree(context.Background(), site, repo, sha); third <- err }()
	wait(t, joined)
	held.release(t)
	must(t, wait(t, third))
	n := f.operations.Load()
	if n != 2 {
		t.Fatal("expected one resolve and one archive", n)
	}
	_, err := f.c.Tree(context.Background(), site, repo, sha)
	must(t, err)
	if n != f.operations.Load() {
		t.Fatal("later request ran git")
	}
}

// R-XHH3-YMVR
func TestFailedUnpackRetainsExistingEmptyDirectory(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	blob := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+":index.html")
	must(t, os.Remove(filepath.Join(f.dir, "objects", blob[:2], blob[2:])))
	parent := filepath.Join(f.root, site)
	must(t, os.Mkdir(parent, 0710))
	before := snap(t, f.root)
	if err := f.c.Unpack(context.Background(), site, repo, sha); err == nil {
		t.Fatal("expected damaged archive failure")
	}
	if fmt.Sprint(before) != fmt.Sprint(snap(t, f.root)) {
		t.Fatal("preexisting empty site directory changed")
	}
}

// R-B4R0-7P1N R-B8EP-D09Q R-QRJA-2NXS R-APOF-7MQ2 R-CWOH-VU0T
func TestErrorDetailsAndPriorities(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	ctx := context.Background()
	must(t, os.WriteFile(filepath.Join(f.dir, "config"), []byte("[malformed\n"), 0600))
	_, rawErr := f.g.Output(ctx, "", "config", "--file", filepath.Join(f.dir, "config"), "--get", "ikigenba.owner")
	owner, err := f.c.Owner(ctx, repo)
	var raw, actual *git.Error
	if owner != "" || !errors.As(rawErr, &raw) || !errors.As(err, &actual) || actual.Status != raw.Status || actual.Stderr != raw.Stderr || actual.Status <= 1 {
		t.Fatal(owner, rawErr, err)
	}
	must(t, os.WriteFile(filepath.Join(f.dir, "config"), []byte("[core]\n bare = true\n"), 0600))
	got, err := f.c.Resolve(ctx, repo, "absent")
	if got != "" || !errors.Is(err, cache.ErrNoCommit) {
		t.Fatal(got, err)
	}
	missing := "rep_1111111111111111"
	file := f.c.RepoDir(missing)
	must(t, os.WriteFile(file, []byte("not a directory"), 0600))
	n := f.operations.Load()
	for _, r := range []string{missing, "invalid"} {
		owner, err = f.c.Owner(ctx, r)
		if owner != "" || !errors.Is(err, cache.ErrRepositoryMissing) {
			t.Fatal(owner, err)
		}
		got, err = f.c.Resolve(ctx, r, "main")
		if got != "" || !errors.Is(err, cache.ErrRepositoryMissing) {
			t.Fatal(got, err)
		}
		err = f.c.Unpack(ctx, site, r, sha)
		if !errors.Is(err, cache.ErrRepositoryMissing) {
			t.Fatal(err)
		}
	}
	if n != f.operations.Load() {
		t.Fatal("invalid repository started git")
	}
	must(t, os.Remove(file))
	f.run(nil, "init", "--bare", "--initial-branch=main", file)
	f.run(nil, "config", "--file", filepath.Join(file, "config"), "ikigenba.owner", "u_restored")
	owner, err = f.c.Owner(ctx, missing)
	must(t, err)
	if owner != "u_restored" {
		t.Fatal(owner)
	}
	sentinels := []error{cache.ErrRepositoryMissing, cache.ErrNoCommit, git.ErrNotFound, limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrDraining, limits.ErrHalted}
	for i, a := range sentinels {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range sentinels {
			if i != j && (errors.Is(a, b) || errors.Is(b, a)) {
				t.Fatal("overlapping sentinels")
			}
		}
	}
	ordered := []error{cache.ErrRepositoryMissing, cache.ErrNoCommit, limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrHalted}
	for i, a := range ordered {
		for _, b := range ordered[i+1:] {
			if cache.Reason(errors.Join(b, a)) != cache.Reason(a) {
				t.Fatal("reason priority", a, b)
			}
		}
	}
}

// R-TMNE-HL5T R-7PSE-YBDY R-B9ML-QS0F
func TestFailingCallsAreSilentAndCountAttempts(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	tree := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+"^{tree}")
	cfg := filepath.Join(f.dir, "config")
	original, err := readFile(cfg)
	must(t, err)
	must(t, os.WriteFile(cfg, []byte("[malformed\n"), 0600))
	stdout, stderr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	must(t, err)
	os.Stdout = w
	os.Stderr = w
	_, ownerErr := f.c.Owner(context.Background(), repo)
	must(t, os.WriteFile(cfg, original, 0600))
	_, resolveErr := f.c.Resolve(context.Background(), repo, tree)
	os.Stdout = stdout
	os.Stderr = stderr
	must(t, w.Close())
	output, err := io.ReadAll(r)
	must(t, err)
	must(t, r.Close())
	if ownerErr == nil || !errors.Is(resolveErr, cache.ErrNoCommit) || len(output) != 0 {
		t.Fatal(ownerErr, resolveErr, string(output))
	}
	bin := filepath.Join(f.base, "bin")
	must(t, os.Mkdir(bin, 0700))
	link := filepath.Join(bin, "git")
	must(t, os.Symlink(f.binary, link))
	f.g, err = git.Find(bin, func() []string { return f.env })
	must(t, err)
	f.open(nil, nil)
	must(t, os.Remove(link))
	n := f.operations.Load()
	_, err = f.c.Owner(context.Background(), repo)
	if !errors.Is(err, git.ErrNotFound) {
		t.Fatal(err)
	}
	_, err = f.c.Resolve(context.Background(), repo, "main")
	if !errors.Is(err, git.ErrNotFound) {
		t.Fatal(err)
	}
	err = f.c.Unpack(context.Background(), site, repo, sha)
	if !errors.Is(err, git.ErrNotFound) || f.operations.Load() != n+3 {
		t.Fatal(err, f.operations.Load()-n)
	}
	_, err = f.c.Tree(context.Background(), site, repo, sha)
	if !errors.Is(err, git.ErrNotFound) || cache.Reason(err) != cache.ReasonGitFailed || f.operations.Load() != n+4 {
		t.Fatal("Tree start failure", err, f.operations.Load()-n)
	}
	n = f.operations.Load()
	for _, call := range []func() error{func() error { return f.c.Unpack(context.Background(), "invalid", repo, sha) }, func() error { return f.c.Unpack(context.Background(), site, repo, "invalid") }, func() error { return f.c.Unpack(context.Background(), site, "invalid", sha) }} {
		if call() == nil {
			t.Fatal("invalid call succeeded")
		}
	}
	f.l.Drain()
	_, err = f.c.Tree(context.Background(), site, repo, sha)
	if !errors.Is(err, limits.ErrDraining) || n != f.operations.Load() {
		t.Fatal("no-op calls started operation", err)
	}
}

// R-CT0S-QISQ R-CKHI-24LV R-QQBD-OW73 R-V9WH-81V9 R-LX6D-TGK3
func TestRebuildArchiveFailuresAndHooks(t *testing.T) {
	for _, kind := range []string{"too-large", "hostile", "disk", "git-failed", "commit-missing", "not-git"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 1024)
			sha := f.file("hello")
			switch kind {
			case "too-large":
				sha = f.file(strings.Repeat("x", 1025))
			case "hostile":
				blob := f.blob("x")
				sha = f.commit(f.tree("100644 blob " + blob + "\tf\n100644 blob " + blob + "\tf\n"))
			case "disk":
				must(t, chmodDir(f.root, 0500))
				t.Cleanup(func() { _ = chmodDir(f.root, 0700) })
			case "git-failed":
				blob := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+":index.html")
				must(t, os.Remove(filepath.Join(f.dir, "objects", blob[:2], blob[2:])))
			case "commit-missing":
				sha = strings.Repeat("0", 40)
			case "not-git":
				must(t, os.RemoveAll(f.dir))
				must(t, os.Mkdir(f.dir, 0700))
			}
			var hookCalls atomic.Int64
			f.open(nil, func(string, string) { hookCalls.Add(1) })
			path, err := f.c.Tree(context.Background(), site, repo, sha)
			if err == nil || path != "" || hookCalls.Load() != 0 {
				t.Fatal(path, err, hookCalls.Load())
			}
			var ge *git.Error
			switch kind {
			case "too-large":
				if !errors.Is(err, limits.ErrTooLarge) {
					t.Fatal(err)
				}
			case "commit-missing":
				if !errors.Is(err, cache.ErrNoCommit) || f.operations.Load() != 1 {
					t.Fatal(err)
				}
			case "git-failed", "not-git":
				if !errors.As(err, &ge) || ge.Status == 0 || ge.Stderr == "" {
					t.Fatal(err)
				}
			default:
				if errors.As(err, &ge) || cache.Reason(err) != cache.ReasonGitFailed {
					t.Fatal(err)
				}
			}
			absent(t, f.c.Dir(site, sha))
			if len(entries(t, filepath.Join(f.root, site))) != 0 {
				t.Fatal("failure left staging")
			}
		})
	}
	f := newFixture(t, 1024)
	blob := f.blob("hello")
	sub := f.tree("100644 blob " + blob + "\tf\n")
	sha := f.commit(f.tree("100644 blob " + blob + "\ta\n040000 tree " + sub + "\td\n"))
	var calls int
	f.open(nil, func(s, c string) {
		calls++
		if s != site || c != sha {
			t.Fatal("hook args")
		}
		absent(t, f.c.Dir(site, sha))
		es := entries(t, filepath.Join(f.root, site))
		if len(es) != 1 || !strings.HasPrefix(es[0].Name(), ".") {
			t.Fatal("staging")
		}
		for _, name := range []string{"a", "d/f"} {
			b, e := readFile(filepath.Join(f.root, site, es[0].Name(), name))
			must(t, e)
			if string(b) != "hello" {
				t.Fatal(name)
			}
		}
	})
	dir, err := f.c.Tree(context.Background(), site, repo, sha)
	must(t, err)
	if calls != 1 {
		t.Fatal(calls)
	}
	es := entries(t, dir)
	if len(es) != 2 || es[0].Name() != "a" || es[1].Name() != "d" {
		t.Fatal("extra or missing final entries")
	}
	for _, name := range []string{"a", "d/f"} {
		b, e := readFile(filepath.Join(dir, name))
		must(t, e)
		if string(b) != "hello" {
			t.Fatal(name)
		}
	}
	_, err = f.c.Tree(context.Background(), site, repo, sha)
	must(t, err)
	if calls != 1 {
		t.Fatal("present Tree called hook")
	}
	assertTree(t, dir, map[string]treeEntry{"a": {"file", "hello"}, "d": {"dir", ""}, "d/f": {"file", "hello"}})
}

// R-D0C7-158W
func TestRelativeRepositoriesFollowCurrentDirectory(t *testing.T) {
	first := newFixture(t, 1024)
	one := first.file("first")
	second := newFixture(t, 1024)
	two := second.file("second")
	second.run(nil, "config", "--file", filepath.Join(second.dir, "config"), "ikigenba.owner", "u_second")
	c, err := cache.Open(cache.Config{Root: first.root, Repos: "repos", Git: first.g, Limits: first.l})
	must(t, err)
	for _, step := range []struct{ base, owner, sha, body string }{{first.base, "u_fixture", one, "first"}, {second.base, "u_second", two, "second"}, {first.base, "u_fixture", one, "first"}} {
		t.Chdir(step.base)
		owner, e := c.Owner(context.Background(), repo)
		must(t, e)
		if owner != step.owner {
			t.Fatal(owner, step.owner)
		}
		sha, e := c.Resolve(context.Background(), repo, "main")
		must(t, e)
		if sha != step.sha {
			t.Fatal(sha, step.sha)
		}
		must(t, c.Unpack(context.Background(), site, repo, sha))
		b, e := readFile(filepath.Join(c.Dir(site, sha), "index.html"))
		must(t, e)
		if string(b) != step.body {
			t.Fatal(string(b))
		}
	}
}

// R-CQKZ-YZBC R-CRSW-CR21 R-CVGL-I2A4 R-LX6D-TGK3
func TestRunningRebuildPruneRemoveAndIndependentKeys(t *testing.T) {
	for _, mode := range []string{"prune", "remove"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, 1024)
			old := f.file("old")
			must(t, f.c.Unpack(context.Background(), site, repo, old))
			sha := f.file("new")
			held := make(chan struct{})
			release := make(chan struct{})
			f.open(nil, func(s, c string) {
				if s == site && c == sha {
					close(held)
					<-release
				}
			})
			result := make(chan error, 1)
			go func() { _, err := f.c.Tree(context.Background(), site, repo, sha); result <- err }()
			wait(t, held)
			if mode == "prune" {
				must(t, f.c.Prune(site, old))
				if len(entries(t, filepath.Join(f.root, site))) != 2 {
					t.Fatal("live staging lost")
				}
			} else {
				must(t, f.c.Remove(site))
				if len(entries(t, filepath.Join(f.root, site))) != 1 {
					t.Fatal("live staging lost")
				}
			}
			close(release)
			must(t, wait(t, result))
			if mode == "remove" {
				absent(t, filepath.Join(f.root, site))
			} else {
				absent(t, f.c.Dir(site, sha))
				b, err := readFile(filepath.Join(f.c.Dir(site, old), "index.html"))
				must(t, err)
				if string(b) != "old" {
					t.Fatal("kept tree changed")
				}
			}
			// A rebuild beginning after the prune/remove is independent and installs normally.
			f.open(nil, nil)
			_, err := f.c.Tree(context.Background(), site, repo, sha)
			must(t, err)
		})
	}
	f := newFixture(t, 1024)
	one := f.file("one")
	two := f.file("two")
	held := make(chan struct{})
	release := make(chan struct{})
	f.open(nil, func(s, c string) {
		if s == site && c == one {
			close(held)
			<-release
		}
	})
	first := make(chan error, 1)
	go func() { _, err := f.c.Tree(context.Background(), site, repo, one); first <- err }()
	wait(t, held)
	for _, key := range []struct{ site, sha string }{{"sit_1111111111111111", one}, {site, two}} {
		result := make(chan error, 1)
		go func() { _, err := f.c.Tree(context.Background(), key.site, repo, key.sha); result <- err }()
		must(t, wait(t, result))
	}
	close(release)
	must(t, wait(t, first))
}

// R-CVGL-I2A4 R-CT0S-QISQ
func TestSharedFailure(t *testing.T) {
	f := newFixture(t, 1024)
	sha := strings.Repeat("0", 40)
	entered := make(chan struct{})
	release := make(chan struct{})
	joined := make(chan struct{})
	var calls atomic.Int64
	f.l = limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return make(chan time.Time)
	}})
	f.open(func(string, string) { close(joined) }, nil)
	one := make(chan error, 1)
	two := make(chan error, 1)
	go func() { _, err := f.c.Tree(context.Background(), site, repo, sha); one <- err }()
	wait(t, entered)
	go func() { _, err := f.c.Tree(context.Background(), site, repo, sha); two <- err }()
	wait(t, joined)
	close(release)
	first, second := wait(t, one), wait(t, two)
	if !errors.Is(first, cache.ErrNoCommit) || !errors.Is(second, cache.ErrNoCommit) || cache.Reason(first) != cache.Reason(second) || calls.Load() != 1 {
		t.Fatal(first, second, calls.Load())
	}
}

// R-VHXY-ASJ4
func TestCompetingInstalledTreeWinsOwnGitFailure(t *testing.T) {
	for _, method := range []string{"unpack", "tree"} {
		t.Run(method, func(t *testing.T) {
			f := newFixture(t, 1024)
			sha := f.file("hello")
			blob := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+":index.html")
			held := f.holdGit()
			result := make(chan error, 1)
			go func() {
				if method == "unpack" {
					result <- f.c.Unpack(context.Background(), site, repo, sha)
				} else {
					dir, err := f.c.Tree(context.Background(), site, repo, sha)
					if err == nil && dir != f.c.Dir(site, sha) {
						err = errors.New("wrong tree path")
					}
					result <- err
				}
			}()
			command := "archive"
			object := blob
			if method == "tree" {
				command = "rev-parse"
				object = sha
			}
			held.started(t, command)
			must(t, os.Remove(filepath.Join(f.dir, "objects", object[:2], object[2:])))
			must(t, os.MkdirAll(f.c.Dir(site, sha), 0700))
			must(t, os.WriteFile(filepath.Join(f.c.Dir(site, sha), "winner"), []byte("winner"), 0600))
			held.release(t)
			must(t, wait(t, result))
			held.exited(t)
			es := entries(t, filepath.Join(f.root, site))
			if len(es) != 1 || es[0].Name() != sha {
				t.Fatal("own debris", es)
			}
			b, err := readFile(filepath.Join(f.c.Dir(site, sha), "winner"))
			must(t, err)
			if string(b) != "winner" {
				t.Fatal("installed tree modified")
			}
		})
	}
}

// R-CO57-7FTY R-CPD3-L7KN R-7PSE-YBDY
func TestRebuildArchiveHasOwnDeadline(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	baseEnv := append([]string{}, f.env...)
	held := f.holdGit()
	archiveEnv := append([]string{}, f.env...)
	f.env = baseEnv
	timer := make(chan time.Time, 1)
	var attempts atomic.Int64
	f.l = limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time {
		if attempts.Add(1) == 2 {
			f.env = archiveEnv
			return timer
		}
		return make(chan time.Time)
	}})
	joined := make(chan struct{})
	f.open(func(string, string) { close(joined) }, nil)
	one, two := make(chan error, 1), make(chan error, 1)
	go func() { _, err := f.c.Tree(context.Background(), site, repo, sha); one <- err }()
	held.started(t, "archive")
	absent(t, f.c.Dir(site, sha))
	es := entries(t, filepath.Join(f.root, site))
	if len(es) != 1 || !strings.HasPrefix(es[0].Name(), ".") {
		t.Fatal("tree visible before whole", es)
	}
	go func() { _, err := f.c.Tree(context.Background(), site, repo, sha); two <- err }()
	wait(t, joined)
	timer <- time.Time{}
	if err := wait(t, one); !errors.Is(err, limits.ErrTimedOut) {
		t.Fatal(err)
	}
	held.exited(t)
	if err := wait(t, two); !errors.Is(err, limits.ErrTimedOut) {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatal("expected separate check and archive operations", attempts.Load())
	}
	absent(t, filepath.Join(f.root, site))
}

type treeEntry struct{ kind, content string }

func assertTree(t *testing.T, root string, want map[string]treeEntry) {
	t.Helper()
	got := make(map[string]treeEntry)
	must(t, filepath.Walk(root, func(p string, st os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		entry := treeEntry{}
		switch {
		case st.IsDir():
			entry.kind = "dir"
		case st.Mode()&os.ModeSymlink != 0:
			entry.kind = "link"
			entry.content, e = os.Readlink(p)
		case st.Mode().IsRegular():
			entry.kind = "file"
			var b []byte
			b, e = readFile(p)
			entry.content = string(b)
		default:
			entry.kind = "other"
		}
		if e != nil {
			return e
		}
		got[filepath.ToSlash(rel)] = entry
		return nil
	}))
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tree entries got %v want %v", got, want)
	}
}

// R-TMNE-HL5T R-TNVA-VCWI R-XHH3-YMVR
func TestArchiveFailuresAreSilentAndReadOnly(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	blob := f.run(nil, "--git-dir="+f.dir, "rev-parse", sha+":index.html")
	must(t, os.Remove(filepath.Join(f.dir, "objects", blob[:2], blob[2:])))
	before := snap(t, f.repos)
	stdout, stderr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	must(t, err)
	os.Stdout = w
	os.Stderr = w
	unpackErr := f.c.Unpack(context.Background(), site, repo, sha)
	_, treeErr := f.c.Tree(context.Background(), site, repo, sha)
	os.Stdout = stdout
	os.Stderr = stderr
	must(t, w.Close())
	output, err := io.ReadAll(r)
	must(t, err)
	must(t, r.Close())
	var a, b *git.Error
	if !errors.As(unpackErr, &a) || !errors.As(treeErr, &b) || a.Stderr == "" || b.Stderr == "" || len(output) != 0 {
		t.Fatal(unpackErr, treeErr, string(output))
	}
	if fmt.Sprint(before) != fmt.Sprint(snap(t, f.repos)) {
		t.Fatal("failed archive changed repository")
	}
	absent(t, filepath.Join(f.root, site))
}

// R-XHH3-YMVR
func TestFailedInstallPreservesPreexistingEntries(t *testing.T) {
	f := newFixture(t, 1024)
	sha := f.file("hello")
	must(t, os.Mkdir(filepath.Join(f.root, site), 0700))
	must(t, os.WriteFile(f.c.Dir(site, sha), []byte("preexisting file"), 0600))
	before := snap(t, f.root)
	err := f.c.Unpack(context.Background(), site, repo, sha)
	if err == nil {
		t.Fatal("installed directory over preexisting file")
	}
	if fmt.Sprint(before) != fmt.Sprint(snap(t, f.root)) {
		t.Fatal("failed install changed preexisting entries")
	}
}
