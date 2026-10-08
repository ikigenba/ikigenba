package build_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const suiteSHA = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

type suiteMember struct {
	Bytes string
	Mode  uint32
}
type suiteFixture struct {
	t           *testing.T
	root        string
	deps        seam.Deps
	commands    []seam.Cmd
	files       map[string]suiteMember
	apps        []string
	mutate      func(string)
	fail        string
	runnerError error
	removeFail  bool
	cancel      context.CancelFunc
	worktrees   []string
	members     map[string]suiteMember
	compiled    map[string]string
}

func newSuite(t *testing.T) *suiteFixture {
	f := &suiteFixture{t: t, root: t.TempDir(), apps: []string{"crm"}, files: map[string]suiteMember{}, compiled: map[string]string{}}
	f.deps = seam.Deps{Dir: f.root, EUID: 1000, Getenv: func(string) string { return "" }, Exec: f.exec, Now: func() time.Time { return time.Date(2026, 10, 8, 9, 3, 12, 900000000, time.FixedZone("test", -5*3600)) }, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud call")
		return cloud.Clients{}, nil
	}}
	return f
}
func (f *suiteFixture) put(root, relative, contents string, mode os.FileMode) {
	f.t.Helper()
	p := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), mode); err != nil {
		f.t.Fatal(err)
	}
}
func (f *suiteFixture) app(root, name, manifest string) {
	f.put(root, name+"/etc/manifest.toml", manifest, 0o644)
	f.put(root, name+"/cmd/"+name+"/main.go", "package main\n", 0o644)
}
func suiteManifest(name string) string { return fmt.Sprintf("app = %q\n", name) }
func (f *suiteFixture) exec(ctx context.Context, c seam.Cmd) (seam.Result, error) {
	f.commands = append(f.commands, c)
	if c.Path == "git" {
		switch c.Args[0] {
		case "rev-parse":
			if reflect.DeepEqual(c.Args, []string{"rev-parse", "--show-toplevel"}) {
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			if f.fail == "resolve" {
				if f.runnerError != nil {
					return seam.Result{}, f.runnerError
				}
				return seam.Result{ExitCode: 1}, nil
			}
			if c.Args[1] == "--verify" || c.Args[1] == "HEAD" {
				return seam.Result{Stdout: []byte(suiteSHA + "\n")}, nil
			}
		case "status":
			return seam.Result{}, nil
		case "worktree":
			if c.Args[1] == "add" {
				if f.fail == "add" {
					return seam.Result{}, f.runnerError
				}
				dir := c.Args[3]
				if !filepath.IsAbs(dir) || !strings.HasPrefix(dir, filepath.Join(f.root, "dist")+string(os.PathSeparator)) {
					f.t.Fatalf("worktree not absolute under dist: %q", dir)
				}
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					f.t.Fatalf("worktree existed: %q", dir)
				}
				if c.Args[4] != suiteSHA {
					f.t.Fatalf("worktree sha %q", c.Args[4])
				}
				f.worktrees = append(f.worktrees, dir)
				for _, name := range f.apps {
					f.app(dir, name, suiteManifest(name))
				}
				for p, m := range f.files {
					f.put(dir, p, m.Bytes, os.FileMode(m.Mode))
				}
				if f.mutate != nil {
					f.mutate(dir)
				}
				return seam.Result{}, nil
			}
			if c.Args[1] == "remove" {
				if ctx.Err() != nil {
					f.t.Fatalf("cleanup context cancelled: %v", ctx.Err())
				}
				if f.removeFail {
					return seam.Result{ExitCode: 1, Stderr: []byte("remove failed\n")}, nil
				}
				return seam.Result{}, os.RemoveAll(c.Args[3])
			}
		}
	}
	if c.Path == "go" {
		name := filepath.Base(c.Dir)
		if f.fail == "compile "+name {
			if f.cancel != nil {
				f.cancel()
			}
			return seam.Result{ExitCode: 7, Stderr: []byte("compiler failed\n")}, nil
		}
		for i, arg := range c.Args {
			if arg == "-o" {
				f.put(filepath.Dir(c.Args[i+1]), filepath.Base(c.Args[i+1]), "binary "+name, 0o600)
				f.compiled[name] = c.Args[i+1]
				return seam.Result{}, nil
			}
		}
	}
	if c.Path == "tar" {
		if f.fail == "tar" {
			return seam.Result{ExitCode: 8, Stderr: []byte("archive failed\n")}, nil
		}
		if len(c.Args) != 6 || c.Args[0] != "-cJf" || c.Args[2] != "-C" || c.Args[4] != "--" || c.Args[5] != suiteSHA {
			f.t.Fatalf("archive cmd %#v", c)
		}
		f.members = map[string]suiteMember{}
		err := filepath.WalkDir(filepath.Join(c.Args[3], suiteSHA), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && !d.Type().IsRegular() {
				f.t.Fatalf("nonregular member %s", path)
			}
			rel, err := filepath.Rel(c.Args[3], path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			m := suiteMember{Mode: uint32(info.Mode().Perm())}
			if !d.IsDir() {
				root, err := os.OpenRoot(filepath.Dir(path))
				if err != nil {
					return err
				}
				data, readErr := root.ReadFile(filepath.Base(path))
				closeErr := root.Close()
				if readErr != nil {
					return readErr
				}
				if closeErr != nil {
					return closeErr
				}
				m.Bytes = string(data)
			} else {
				m.Mode |= uint32(os.ModeDir)
			}
			f.members[filepath.ToSlash(rel)] = m
			return nil
		})
		if err != nil {
			return seam.Result{}, err
		}
		data, err := json.Marshal(f.members)
		if err != nil {
			return seam.Result{}, err
		}
		return seam.Result{}, os.WriteFile(c.Args[1], data, 0o600)
	}
	name := filepath.Base(c.Path)
	if reflect.DeepEqual(c.Args, []string{"manifest"}) {
		if name == "opsctl" || f.compiled[name] == "" || c.Path != f.compiled[name] {
			f.t.Fatalf("manifest did not run the compiled app binary: %#v; compiled outputs %#v", c, f.compiled)
		}
		if f.fail == "manifest "+name {
			return seam.Result{ExitCode: 9, Stderr: []byte("manifest failed\n")}, nil
		}
		if f.fail == "stale" {
			return seam.Result{Stdout: []byte("app = \"crm\"\n# stale\n")}, nil
		}
		return seam.Result{Stdout: []byte(suiteManifest(name))}, nil
	}
	f.t.Fatalf("unexpected process %#v", c)
	return seam.Result{}, nil
}
func (f *suiteFixture) run(ctx context.Context, operand string) (string, error) {
	var out bytes.Buffer
	err := build.Run(ctx, []string{operand}, "test-version", &out, f.deps)
	return out.String(), err
}
func (f *suiteFixture) count(path string) int {
	n := 0
	for _, c := range f.commands {
		if c.Path == path {
			n++
		}
	}
	return n
}
func (f *suiteFixture) assertCleanup() {
	f.t.Helper()
	var adds, removes []string
	for _, c := range f.commands {
		if c.Path == "git" && c.Args[0] == "worktree" {
			if c.Args[1] == "add" {
				adds = append(adds, c.Args[3])
			} else {
				removes = append(removes, c.Args[3])
			}
		}
	}
	if !reflect.DeepEqual(adds, removes) {
		f.t.Fatalf("worktrees added %v removed %v", adds, removes)
	}
	if len(f.commands) == 0 || f.commands[len(f.commands)-1].Path != "git" || f.commands[len(f.commands)-1].Args[1] != "remove" {
		f.t.Fatalf("cleanup not final process: %#v", f.commands)
	}
}

func TestSuiteArchiveIsolationAndMetadata(t *testing.T) {
	// R-FN66-1TL1
	// R-FUR4-9R61
	// R-G683-IC4D
	f := newSuite(t)
	f.app(f.root, "extra", suiteManifest("extra"))
	f.app(f.root, "crm", suiteManifest("crm"))
	f.put(f.root, "crm/etc/nginx.conf", "developer", 0o644)
	f.put(f.root, "crm/share/icon.svg", "developer icon", 0o644)
	f.put(f.root, "crm/etc/local.conf", "uncommitted", 0o644)
	f.files = map[string]suiteMember{
		"crm/etc/nginx.conf": {Bytes: "commit nginx", Mode: 0o644}, "crm/share/icon.svg": {Bytes: "commit icon", Mode: 0o644}, "crm/share/doc/a.md": {Bytes: "doc", Mode: 0o644}, "crm/libexec/helper": {Bytes: "helper", Mode: 0o740}, "crm/lib/x.so": {Bytes: "library", Mode: 0o640}, "crm/internal/x.go": {Bytes: "source", Mode: 0o644}, "crm/specs/s.md": {Bytes: "spec", Mode: 0o644}, "crm/go.mod": {Bytes: "module", Mode: 0o644}, "crm/AGENTS.md": {Bytes: "instructions", Mode: 0o644}, "opsctl/cmd/opsctl/main.go": {Bytes: "package main", Mode: 0o644}, "opsctl/etc/x.conf": {Bytes: "config", Mode: 0o644}, "opsctl/go.mod": {Bytes: "module", Mode: 0o644}}
	out, err := f.run(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if out != build.ReleaseFile(suiteSHA)+"\n" {
		t.Fatalf("stdout %q", out)
	}
	regular := map[string]string{}
	for p, m := range f.members {
		if os.FileMode(m.Mode).IsDir() {
			continue
		}
		regular[strings.TrimPrefix(p, suiteSHA+"/")] = m.Bytes
		if strings.Contains(p, "test-version") {
			t.Fatalf("version in member %q", p)
		}
	}
	meta := regular["release.json"]
	delete(regular, "release.json")
	want := map[string]string{"crm/bin/crm": "binary crm", "crm/etc/manifest.toml": suiteManifest("crm"), "crm/etc/nginx.conf": "commit nginx", "crm/share/icon.svg": "commit icon", "crm/share/doc/a.md": "doc", "crm/libexec/helper": "helper", "crm/lib/x.so": "library", "opsctl/bin/opsctl": "binary opsctl"}
	if !reflect.DeepEqual(regular, want) {
		t.Fatalf("regular members %#v want %#v", regular, want)
	}
	for p, m := range f.members {
		if os.FileMode(m.Mode).IsDir() {
			prefix := p + "/"
			ancestor := false
			for file := range f.members {
				if strings.HasPrefix(file, prefix) {
					ancestor = true
				}
			}
			if !ancestor {
				t.Fatalf("extra directory %q", p)
			}
		}
	}
	for _, p := range []string{"crm/bin/crm", "opsctl/bin/opsctl", "crm/libexec/helper"} {
		if f.members[suiteSHA+"/"+p].Mode&0o100 == 0 {
			t.Fatalf("not owner executable %s", p)
		}
	}
	for _, p := range []string{"crm/etc/nginx.conf", "crm/lib/x.so", "crm/etc/manifest.toml", "release.json"} {
		if f.members[suiteSHA+"/"+p].Mode&0o100 != 0 {
			t.Fatalf("unexpected executable %s", p)
		}
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(meta), &metadata); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(metadata, map[string]string{"sha": suiteSHA, "built": "2026-10-08T14:03:12Z", "devctl": "test-version"}) {
		t.Fatalf("metadata %#v", metadata)
	}
}

func TestSuiteCommitFormsHaveSameRelease(t *testing.T) {
	// R-I0DK-XXF3
	var previous map[string]suiteMember
	for _, operand := range []string{"r1", suiteSHA, "4b22285"} {
		f := newSuite(t)
		out, err := f.run(context.Background(), operand)
		if err != nil {
			t.Fatal(err)
		}
		if out != build.ReleaseFile(suiteSHA)+"\n" {
			t.Fatalf("stdout %q", out)
		}
		var git []seam.Cmd
		for _, c := range f.commands {
			if c.Path == "git" {
				git = append(git, c)
			}
		}
		object := operand
		if operand == "r1" {
			object = "refs/tags/r1"
		}
		if len(git) != 4 || !reflect.DeepEqual(git[0].Args, []string{"rev-parse", "--show-toplevel"}) || !reflect.DeepEqual(git[1].Args, []string{"rev-parse", "--verify", "--quiet", "--end-of-options", object + "^{commit}"}) || !reflect.DeepEqual(git[2].Args, []string{"worktree", "add", "--detach", f.worktrees[0], suiteSHA}) || !reflect.DeepEqual(git[3].Args, []string{"worktree", "remove", "--force", f.worktrees[0]}) {
			t.Fatalf("git commands %#v", git)
		}
		if previous != nil && !reflect.DeepEqual(previous, f.members) {
			t.Fatal("operand changed release")
		}
		previous = f.members
	}
}
func TestSuiteCompileCommands(t *testing.T) {
	// R-I1LH-BP5S
	// R-FLHT-08IK
	f := newSuite(t)
	f.apps = []string{"auth", "dummy", "events", "mcp", "repos", "scripts", "sites", "telemetry"}
	f.put(f.root, "go.work", "go 1.26\n", 0o644)
	_, err := f.run(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	var compiles []seam.Cmd
	for _, c := range f.commands {
		if c.Path == "go" {
			compiles = append(compiles, c)
		}
	}
	manifestCounts := map[string]int{}
	for _, c := range f.commands {
		for name, binary := range f.compiled {
			if c.Path == binary {
				if name == "opsctl" || !reflect.DeepEqual(c.Args, []string{"manifest"}) {
					t.Fatalf("unexpected staged executable call %#v", c)
				}
				manifestCounts[name]++
			}
		}
	}
	for _, name := range f.apps {
		if manifestCounts[name] != 1 {
			t.Fatalf("%s manifest calls = %d, want exactly one", name, manifestCounts[name])
		}
	}
	names := append(append([]string{}, f.apps...), "opsctl")
	if len(compiles) != len(names) {
		t.Fatalf("compile count %d", len(compiles))
	}
	for i, c := range compiles {
		if c.Dir != filepath.Join(f.worktrees[0], names[i]) || !reflect.DeepEqual(c.Env, []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOWORK=off"}) || len(c.Args) != 5 || c.Args[0] != "build" || c.Args[1] != "-buildvcs=false" || c.Args[2] != "-o" || c.Args[4] != "./cmd/"+names[i] {
			t.Fatalf("compile %#v", c)
		}
	}
}

func TestSuiteCleanupEveryOutcome(t *testing.T) {
	// R-FLY9-O1UC
	// R-FWGW-G66T
	// R-G1CH-Z95L
	for _, failure := range []string{"", "port", "reserved", "sbin", "stale", "compile crm", "manifest crm", "compile opsctl"} {
		t.Run(failure, func(t *testing.T) {
			f := newSuite(t)
			f.fail = failure
			switch failure {
			case "port":
				f.mutate = func(dir string) { f.put(dir, "crm/etc/manifest.toml", suiteManifest("crm")+"port = 1234\n", 0o644) }
			case "reserved":
				f.apps = []string{"host"}
			case "sbin":
				f.files["crm/sbin/tool"] = suiteMember{Bytes: "tool", Mode: 0o700}
			}
			out, err := f.run(context.Background(), "r1")
			if failure == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || out != "" {
					t.Fatalf("stdout %q error %v", out, err)
				}
			}
			f.assertCleanup()
			switch failure {
			case "port":
				var manifest *checkout.ManifestError
				if !errors.As(err, &manifest) || manifest.Error() != "crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket" || f.count("go") != 0 {
					t.Fatalf("manifest error %v commands %#v", err, f.commands)
				}
			case "reserved", "sbin":
				if f.count("go") != 0 {
					t.Fatal("compiled refused app")
				}
			case "stale":
				var stale *build.StaleManifestError
				if !errors.As(err, &stale) || stale.App != "crm" {
					t.Fatalf("error %v", err)
				}
			case "compile crm", "manifest crm", "compile opsctl":
				var process *build.ProcessError
				if !errors.As(err, &process) {
					t.Fatalf("error %v", err)
				}
				label := failure
				if strings.HasPrefix(label, "compile ") {
					label = "build " + strings.TrimPrefix(label, "compile ")
				} else {
					label = "crm manifest"
				}
				if process.Label != label {
					t.Fatalf("label %q", process.Label)
				}
			}
			if failure != "" && f.count("tar") != 0 {
				t.Fatal("archived after failure")
			}
		})
	}
	t.Run("cancelled compile", func(t *testing.T) {
		f := newSuite(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.cancel = cancel
		f.fail = "compile crm"
		_, err := f.run(ctx, "r1")
		if err == nil {
			t.Fatal("expected compile failure")
		}
		f.assertCleanup()
	})
	t.Run("failed removal unique directories", func(t *testing.T) {
		f := newSuite(t)
		f.removeFail = true
		for range 2 {
			out, err := f.run(context.Background(), "r1")
			if err == nil || out != "" {
				t.Fatalf("stdout %q err %v", out, err)
			}
		}
		if len(f.worktrees) != 2 || f.worktrees[0] == f.worktrees[1] {
			t.Fatalf("worktrees %v", f.worktrees)
		}
		for _, dir := range f.worktrees {
			if _, err := os.Stat(dir); err != nil {
				t.Fatal(err)
			}
		}
		f.assertCleanup()
	})
}

func TestSuiteNoCommitOrResolveErrorStops(t *testing.T) {
	// R-HZ5O-K5OE
	// R-FS1R-KWJT
	for _, operand := range []string{"bogus", "main", "HEAD", "HEAD~1"} {
		f := newSuite(t)
		f.fail = "resolve"
		out, err := f.run(context.Background(), operand)
		var usage *build.UsageError
		if !errors.As(err, &usage) || usage.Message != "'"+operand+"' is neither an app in the checkout nor a commit" || usage.Help != "" || out != "" {
			t.Fatalf("operand %s stdout %q error %#v", operand, out, err)
		}
		if len(f.commands) > 2 || f.count("git") != len(f.commands) {
			t.Fatalf("commands %#v", f.commands)
		}
		if _, err := os.Stat(filepath.Join(f.root, "dist")); !os.IsNotExist(err) {
			t.Fatalf("dist changed: %v", err)
		}
	}
	f := newSuite(t)
	f.fail = "resolve"
	f.runnerError = errors.New("resolution runner failure")
	out, err := f.run(context.Background(), "r1")
	if out != "" || !errors.Is(err, f.runnerError) || err.Error() != "git rev-parse: resolution runner failure" || len(f.commands) != 2 {
		t.Fatalf("output %q error %v commands %#v", out, err, f.commands)
	}
}

func TestSuiteAddErrorStopsAndCleansDist(t *testing.T) {
	// R-FLY9-O1UC
	// R-FZMP-SU4T
	f := newSuite(t)
	f.fail = "add"
	f.runnerError = errors.New("worktree add failed")
	out, err := f.run(context.Background(), "r1")
	if out != "" || !errors.Is(err, f.runnerError) || err.Error() != "git worktree add --detach "+f.commands[2].Args[3]+" "+suiteSHA+": worktree add failed" || len(f.commands) != 3 {
		t.Fatalf("output %q error %v commands %#v", out, err, f.commands)
	}
	if _, err := os.Stat(filepath.Join(f.root, "dist")); !os.IsNotExist(err) {
		t.Fatalf("dist remains: %v", err)
	}
}

func TestSuitePublishAndFailurePreservation(t *testing.T) {
	// R-FZMP-SU4T
	// R-G8NW-9VLR
	// R-GB3P-1F35
	for _, failure := range []string{"", "stale", "compile crm", "compile opsctl", "tar", "remove", "stale remove"} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s existing=%v", failure, exists), func(t *testing.T) {
				f := newSuite(t)
				f.put(f.root, "auth/unchanged", "keep", 0o644)
				f.deps.Dir = filepath.Join(f.root, "auth")
				if exists {
					f.put(f.root, build.ReleaseFile(suiteSHA), "earlier archive", 0o600)
					f.put(f.root, "dist/keep", "keep dist", 0o644)
				}
				f.fail = failure
				if strings.Contains(failure, "remove") {
					f.removeFail = true
					if failure == "stale remove" {
						f.fail = "stale"
					} else {
						f.fail = ""
					}
				}
				out, err := f.run(context.Background(), "r1")
				final, readErr := os.ReadFile(filepath.Join(f.root, build.ReleaseFile(suiteSHA)))
				if failure == "" {
					if err != nil || out != build.ReleaseFile(suiteSHA)+"\n" || readErr != nil || string(final) == "earlier archive" {
						t.Fatalf("output %q err %v final %q", out, err, final)
					}
				} else {
					if err == nil || out != "" {
						t.Fatalf("output %q err %v", out, err)
					}
					if exists {
						if readErr != nil || string(final) != "earlier archive" {
							t.Fatalf("earlier archive changed %q error %v", final, readErr)
						}
					} else if !os.IsNotExist(readErr) {
						t.Fatalf("archive published on failure: %v", readErr)
					}
				}
				if failure == "tar" {
					var process *build.ProcessError
					if !errors.As(err, &process) || process.Status != 8 || process.Stderr != "archive failed\n" {
						t.Fatalf("tar error %#v", err)
					}
				}
				if failure == "remove" {
					var git *checkout.GitError
					if !errors.As(err, &git) {
						t.Fatalf("cleanup error %v", err)
					}
				}
				if failure == "stale remove" {
					var stale *build.StaleManifestError
					if !errors.As(err, &stale) {
						t.Fatalf("earlier failure lost: %v", err)
					}
				}
				if data, _ := os.ReadFile(filepath.Join(f.root, "auth/unchanged")); string(data) != "keep" {
					t.Fatal("checkout outside dist changed")
				}
				entries, dirErr := os.ReadDir(filepath.Join(f.root, "dist"))
				if !f.removeFail {
					var names []string
					for _, entry := range entries {
						names = append(names, entry.Name())
					}
					var want []string
					if exists {
						want = append(want, "keep")
					}
					if failure == "" || exists {
						want = append(want, suiteSHA+".tar.xz")
					}
					sort.Strings(want)
					if !reflect.DeepEqual(names, want) {
						t.Fatalf("dist members %v want %v", names, want)
					}
					if failure != "" && !exists && !os.IsNotExist(dirErr) {
						t.Fatalf("new dist remains after failure %v", dirErr)
					}
				}
				f.assertCleanup()
			})
		}
	}
}

func TestSuiteManifestUsesWorktree(t *testing.T) {
	// R-G2KE-D0WA
	for _, stale := range []bool{false, true} {
		f := newSuite(t)
		developer := suiteManifest("crm") + "# developer\n"
		if stale {
			developer = suiteManifest("crm")
			f.fail = "stale"
		}
		f.app(f.root, "crm", developer)
		out, err := f.run(context.Background(), "r1")
		if stale {
			var staleErr *build.StaleManifestError
			if !errors.As(err, &staleErr) || out != "" {
				t.Fatalf("out %q err %v", out, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSuiteDoesNotUseRootConfiguration(t *testing.T) {
	// R-G9VS-NNCG
	var previous map[string]suiteMember
	for _, rootFile := range []string{"", "malformed", "{\"root\":\"test.invalid\",\"region\":\"us-east-1\"}"} {
		f := newSuite(t)
		if rootFile != "" {
			f.put(f.root, "infra/terraform.tfvars.json", rootFile, 0o644)
		}
		out, err := f.run(context.Background(), "r1")
		if err != nil || out != build.ReleaseFile(suiteSHA)+"\n" {
			t.Fatalf("stdout %q error %v", out, err)
		}
		if previous != nil && !reflect.DeepEqual(previous, f.members) {
			t.Fatal("root config changed release")
		}
		previous = f.members
	}
}

func TestSuitePublicAPI(t *testing.T) {
	// R-VL50-AX22 R-VNKT-2GJG
	_ = build.Release(struct {
		SHA       string
		File      string
		Manifests []checkout.Manifest
	}{})
	f := newSuite(t)
	f.apps = []string{"auth", "dummy"}
	auth := suiteManifest("auth") + "secrets = [\"GOOGLE_CLIENT_ID\", \"GOOGLE_CLIENT_SECRET\"]\n"
	f.files["auth/etc/manifest.toml"] = suiteMember{Bytes: auth, Mode: 0o644}
	original := f.deps.Exec
	f.deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if filepath.Base(c.Path) == "auth" && reflect.DeepEqual(c.Args, []string{"manifest"}) {
			f.commands = append(f.commands, c)
			return seam.Result{Stdout: []byte(auth)}, nil
		}
		return original(ctx, c)
	}
	c := &checkout.Checkout{Root: f.root, Deps: f.deps}
	r, err := build.Suite(context.Background(), c, suiteSHA, "test-version")
	want := []checkout.Manifest{{App: "auth", Secrets: []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"}}, {App: "dummy", Secrets: []string{}}}
	if err != nil || r.SHA != suiteSHA || r.File != build.ReleaseFile(suiteSHA) || !reflect.DeepEqual(r.Manifests, want) {
		t.Fatalf("release %#v error %v", r, err)
	}
	for _, cmd := range f.commands {
		if cmd.Path == "git" && cmd.Args[0] == "rev-parse" {
			t.Fatal("resolved/opened checkout")
		}
	}
	first := f.members
	f.commands = nil
	_, err = f.run(context.Background(), suiteSHA)
	if err != nil || !reflect.DeepEqual(first, f.members) {
		t.Fatalf("CLI differs %v", err)
	}
	f = newSuite(t)
	f.apps = []string{"dashboard"}
	f.fail = "compile dashboard"
	_, err = build.Suite(context.Background(), &checkout.Checkout{Root: f.root, Deps: f.deps}, suiteSHA, "test-version")
	var process *build.ProcessError
	if !errors.As(err, &process) || process.Label != "build dashboard" {
		t.Fatalf("error %v", err)
	}
}
