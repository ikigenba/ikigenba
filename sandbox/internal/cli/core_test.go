package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-XWS5-WXD5 R-XY02-AP3U
func TestCorePublicEntry(t *testing.T) {
	var entry func(context.Context, []string, io.Reader, io.Writer, io.Writer, seam.Deps) int
	version := &Version
	if entry = Run; !regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(*version) {
		t.Fatalf("invalid source version %q", Version)
	}
	var out, err bytes.Buffer
	code := entry(context.Background(), []string{"version"}, nil, &out, &err, seam.Deps{})
	if code < 0 || code > 3 || code != 0 || out.String() != Version+"\n" || err.Len() != 0 {
		t.Fatalf("%d %q %q", code, out.String(), err.String())
	}
	// A second call after the first proves Run returned instead of terminating its caller.
	out.Reset()
	code = runChecked(context.Background(), t, []string{"version"}, nil, &out, &err, seam.Deps{})
	if code != 0 || out.String() != Version+"\n" {
		t.Fatal("second call failed")
	}
}

type coreFixture struct {
	base, worktree, state, config, root string
	deps                                seam.Deps
}

func coreWrite(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func coreExecutable(t testing.TB, path string) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := root.Chmod(filepath.Base(path), 0700); err != nil {
		t.Fatal(err)
	}
}
func newCoreFixture(t testing.TB) *coreFixture {
	t.Helper()
	base := t.TempDir()
	f := &coreFixture{base: base, worktree: filepath.Join(base, "wip"), state: filepath.Join(base, "state"), config: filepath.Join(base, "config")}
	f.root = filepath.Join(f.state, "ikigenba", "sandbox")
	coreWrite(t, filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), "app = \"dummy\"\n")
	coreWrite(t, filepath.Join(f.root, "registry.json"), fmt.Sprintf(`{"sandboxes":[{"name":"wip","port":7400,"worktree":%q,"apps":[{"name":"dummy","default":false}]}]}`, f.worktree))
	coreWrite(t, filepath.Join(f.root, "wip", "token"), "ikp_"+strings.Repeat("a", 32))
	f.deps = seam.Deps{Dir: f.worktree, EUID: 1000, Getenv: func(key string) string {
		switch key {
		case "HOME":
			return f.base
		case "XDG_STATE_HOME":
			return f.state
		case "XDG_CONFIG_HOME":
			return f.config
		}
		return ""
	}, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		switch cmd.Path {
		case "git":
			return seam.Result{Stdout: []byte(f.worktree + "\n")}, nil
		case "systemctl":
			return seam.Result{Stdout: []byte("inactive\n")}, nil
		case "go":
			for i, arg := range cmd.Args {
				if arg == "-o" && i+1 < len(cmd.Args) {
					coreWrite(t, cmd.Args[i+1], "binary")
				}
			}
		}
		return seam.Result{}, nil
	}, Stream: func(_ context.Context, _ seam.Cmd, w io.Writer) (seam.Result, error) {
		_, err := io.WriteString(w, "journal bytes\n")
		return seam.Result{}, err
	}}
	return f
}
func coreCall(t testing.TB, args []string, deps seam.Deps) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := runChecked(context.Background(), t, args, strings.NewReader("ikp_"+strings.Repeat("b", 32)+"\n"), &out, &err, deps)
	return code, out.String(), err.String()
}

// R-Y5BG-LBK0
func TestNilEnvironment(t *testing.T) {
	f := newCoreFixture(t)
	t.Setenv("HOME", f.base)
	t.Setenv("XDG_STATE_HOME", f.state)
	deps := f.deps
	deps.Getenv = nil
	c1, o1, e1 := coreCall(t, []string{"ls"}, deps)
	deps.Getenv = func(string) string { return "" }
	c2, o2, e2 := coreCall(t, []string{"ls"}, deps)
	if c1 != c2 || o1 != o2 || e1 != e2 || strings.Contains(o1, "wip") {
		t.Fatalf("nil (%d,%q,%q) empty (%d,%q,%q)", c1, o1, e1, c2, o2, e2)
	}
}

// R-Y6JC-Z3AP
func TestNilExec(t *testing.T) {
	dir := t.TempDir()
	scripts := t.TempDir()
	record := filepath.Join(scripts, "record")
	git := filepath.Join(scripts, "git")
	coreWrite(t, git, "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+record+"'\nexit 128\n")
	coreExecutable(t, git)
	t.Setenv("PATH", scripts)
	code, out, err := coreCall(t, []string{"url"}, seam.Deps{Dir: dir, EUID: 1000})
	if code != 2 || out != "" || err != "sandbox: '"+dir+"' is not inside a git checkout\n" {
		t.Fatalf("%d %q %q", code, out, err)
	}
	data, readErr := os.ReadFile(filepath.Clean(record))
	if readErr != nil || string(data) != "rev-parse\n--show-toplevel\n" {
		t.Fatalf("script %q %v", data, readErr)
	}
}

// R-Y7R9-CV1E
func TestNilStream(t *testing.T) {
	f := newCoreFixture(t)
	scripts := t.TempDir()
	journal := filepath.Join(scripts, "journalctl")
	coreWrite(t, journal, "#!/bin/sh\nprintf 'real stream bytes\\n'\n")
	coreExecutable(t, journal)
	t.Setenv("PATH", scripts)
	f.deps.Stream = nil
	code, out, err := coreCall(t, []string{"logs"}, f.deps)
	if code != 0 || out != "real stream bytes\n" || err != "" {
		t.Fatalf("%d %q %q", code, out, err)
	}
}

type coreEntry struct {
	mode fs.FileMode
	data string
}

func coreSnapshot(t testing.TB, root string) map[string]coreEntry {
	t.Helper()
	result := map[string]coreEntry{}
	scope, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := scope.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := coreEntry{mode: info.Mode()}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !d.IsDir() {
			data, err := scope.ReadFile(relative)
			if err != nil {
				return err
			}
			entry.data = string(data)
		}
		result[relative] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func coreRestore(t testing.TB, root string, snapshot map[string]coreEntry) {
	t.Helper()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for path, entry := range snapshot {
		absolute := filepath.Join(root, path)
		if entry.mode.IsDir() {
			if err := os.MkdirAll(absolute, entry.mode.Perm()); err != nil {
				t.Fatal(err)
			}
		} else {
			coreWrite(t, absolute, entry.data)
			if err := os.Chmod(absolute, entry.mode.Perm()); err != nil {
				t.Fatal(err)
			}
		}
	}
}

var coreCommands = [][]string{{"up"}, {"down"}, {"down", "wip"}, {"wipe"}, {"wipe", "wip"}, {"ls"}, {"url"}, {"status"}, {"logs"}, {"logs", "-n", "5", "-f", "dummy"}, {"token"}, {"token", "set"}, {"version"}}

func coreReadyForCommand(f *coreFixture, args []string) {
	if args[0] == "url" {
		exec := f.deps.Exec
		f.deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path == "systemctl" {
				return seam.Result{Stdout: []byte("active\n")}, nil
			}
			return exec(ctx, cmd)
		}
	}
}

// R-ACYL-CMNR
func TestEveryCommandIgnoresProcessLocations(t *testing.T) {
	sentinel := newCoreFixture(t)
	before := coreSnapshot(t, sentinel.base)
	for _, args := range coreCommands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := newCoreFixture(t)
			coreReadyForCommand(f, args)
			if args[0] == "up" {
				coreWrite(t, filepath.Join(f.worktree, "dummy", "etc", "manifest.toml"), "app=\"dummy\"\nsecrets=[\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_SECRET\"]\n")
				getenv := f.deps.Getenv
				f.deps.Getenv = func(key string) string {
					if key == "GOOGLE_LOCALHOST_CLIENT_ID" || key == "GOOGLE_LOCALHOST_CLIENT_SECRET" {
						return "injected-secret"
					}
					return getenv(key)
				}
			}
			input := coreSnapshot(t, f.base)
			c1, o1, e1 := coreCall(t, args, f.deps)
			if c1 != 0 {
				t.Fatalf("baseline command failed: %d %q %q", c1, o1, e1)
			}
			coreRestore(t, f.base, input)
			for _, key := range []string{"GOOGLE_LOCALHOST_CLIENT_ID", "GOOGLE_LOCALHOST_CLIENT_SECRET", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"} {
				t.Setenv(key, "sentinel secret")
			}
			t.Setenv("HOME", sentinel.base)
			t.Setenv("XDG_CONFIG_HOME", sentinel.config)
			t.Setenv("XDG_STATE_HOME", sentinel.state)
			t.Chdir(sentinel.worktree)
			c2, o2, e2 := coreCall(t, args, f.deps)
			if c1 != c2 || o1 != o2 || e1 != e2 {
				t.Fatalf("baseline (%d,%q,%q) sentinel (%d,%q,%q)", c1, o1, e1, c2, o2, e2)
			}
			if !reflect.DeepEqual(before, coreSnapshot(t, sentinel.base)) {
				t.Fatal("sentinel changed")
			}
		})
	}
}

// R-YSHJ-UYN7
func TestConcurrentCallsMatchLoneCalls(t *testing.T) {
	type call struct {
		args     []string
		fixture  *coreFixture
		code     int
		out, err string
	}
	calls := make([]call, 0, len(coreCommands)*2)
	for repeat := 0; repeat < 2; repeat++ {
		for _, args := range coreCommands {
			f := newCoreFixture(t)
			coreReadyForCommand(f, args)
			snapshot := coreSnapshot(t, f.base)
			code, out, err := coreCall(t, args, f.deps)
			if code != 0 {
				t.Fatalf("lone command %v failed: %d %q %q", args, code, out, err)
			}
			coreRestore(t, f.base, snapshot)
			calls = append(calls, call{args, f, code, out, err})
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, c := range calls {
		wg.Go(func() {
			<-start
			code, out, err := coreCall(t, c.args, c.fixture.deps)
			if code != c.code || out != c.out || err != c.err {
				t.Errorf("%v concurrent (%d,%q,%q), lone (%d,%q,%q)", c.args, code, out, err, c.code, c.out, c.err)
			}
		})
	}
	close(start)
	wg.Wait()
}
