package store_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

var fixedNow = time.Date(2024, 7, 8, 9, 10, 11, 987654321, time.FixedZone("east", 3600))

type fixture struct {
	cfg    store.Config
	source string
	d      **db.DB
	g      *git.Git
	env    []string
	path   string
	base   string
}

func setup(t *testing.T) fixture {
	t.Helper()
	base := t.TempDir()
	path, err := exec.LookPath("git")
	must(t, err)
	env := []string{"PATH=" + filepath.Dir(path), "HOME=" + base, "XDG_CONFIG_HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid", "GIT_AUTHOR_DATE=2001-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2001-01-01T00:00:00Z"}
	g, err := git.Find(filepath.Dir(path), func() []string { return append([]string(nil), env...) })
	must(t, err)
	random := make([]byte, 8*128)
	for i := 0; i < 128; i++ {
		random[i*8+7] = byte(i + 1)
	}
	return fixture{source: filepath.Join(base, "catalog", "repos.db"), d: new(*db.DB), cfg: store.Config{Root: filepath.Join(base, "root"), Git: g, Now: func() time.Time { return fixedNow }, Rand: bytes.NewReader(random)}, g: g, env: env, path: path, base: base}
}
func (f fixture) open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(testContext(t), f.handle(t), f.cfg)
	must(t, err)

	return s
}
func (f fixture) handle(t *testing.T) *db.DB {
	t.Helper()
	if *f.d == nil {
		d, err := db.Open(testContext(t), db.Config{Path: f.source, Migrations: repos.Migrations(), Now: f.cfg.Now})
		must(t, err)
		*f.d = d
		t.Cleanup(func() { must(t, d.Close()) })
	}
	return *f.d
}
func (f fixture) close(t *testing.T) { t.Helper(); must(t, (*f.d).Close()); *f.d = nil }
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func create(t *testing.T, s *store.Store, owner, name string) store.Repo {
	t.Helper()
	r, err := s.Create(testContext(t), owner, name)
	must(t, err)
	return r
}
func all(t *testing.T, s *store.Store) []store.Repo {
	t.Helper()
	r, err := s.All(testContext(t))
	must(t, err)
	return r
}
func same(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func write(t *testing.T, path, text string) {
	t.Helper()
	must(t, os.WriteFile(path, []byte(text), 0600))
}
func (f fixture) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.path, args...)
	cmd.Dir = dir
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func (f fixture) identity(t *testing.T, id, name, owner, created string) string {
	t.Helper()
	dir := filepath.Join(f.cfg.Root, id+".git")
	must(t, os.MkdirAll(f.cfg.Root, 0700))
	f.git(t, f.base, "init", "--bare", "--template=", "--initial-branch="+store.DefaultBranch, dir)
	for _, p := range [][2]string{{"id", id}, {"name", name}, {"owner", owner}, {"created", created}} {
		f.git(t, f.base, "config", "--file", filepath.Join(dir, "config"), "ikigenba."+p[0], p[1])
	}
	return dir
}

type entry struct {
	Mode  os.FileMode
	Bytes string
}

func snapshot(t *testing.T, path string) map[string]entry {
	t.Helper()
	result := map[string]entry{}
	root, err := os.OpenRoot(path)
	if os.IsNotExist(err) {
		return result
	}
	must(t, err)
	defer func() { must(t, root.Close()) }()
	err = fs.WalkDir(root.FS(), ".", func(name string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		value := entry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			body, err := root.ReadFile(name)
			if err != nil {
				return err
			}
			value.Bytes = string(body)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			value.Bytes, err = root.Readlink(name)
			if err != nil {
				return err
			}
		}
		result[name] = value
		return nil
	})
	must(t, err)
	return result
}

func absent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if !os.IsNotExist(err) {
		t.Fatalf("%s still exists: %v", path, err)
	}
}
func writer(t *testing.T) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := new(telemetry.Capture)
	w := telemetry.New(telemetry.Config{Service: "repos", Sink: capture, Now: func() time.Time { return fixedNow }, Rand: bytes.NewReader(make([]byte, 128)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	return w, capture
}
func events(t *testing.T, w *telemetry.Writer, c *telemetry.Capture) []telemetry.Event {
	t.Helper()
	must(t, w.Flush(testContext(t)))
	return c.Events()
}
func damaged(t *testing.T, s *store.Store, r store.Repo) {
	t.Helper()
	must(t, os.Remove(filepath.Join(s.Dir(r.ID), "HEAD")))
}
func (f fixture) commit(t *testing.T, dir, ref string) string {
	t.Helper()
	tree := f.git(t, dir, "mktree")
	sha := f.git(t, dir, "commit-tree", tree[:len(tree)-1], "-m", "fixture")
	f.git(t, dir, "update-ref", ref, sha[:len(sha)-1])
	return sha[:len(sha)-1]
}
func id(n int) string { return fmt.Sprintf("%s%016x", store.IDPrefix, n) }

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
