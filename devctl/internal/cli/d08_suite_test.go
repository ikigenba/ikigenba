package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const cliSuiteSHA = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

func TestSuiteDispatchThroughCLI(t *testing.T) {
	// R-R22C-FF2C R-G683-IC4D
	fixture := newCLISuiteFixture(t)
	fixture.apps = []string{"crm"}
	assertResult(t, invokeWithDeps(fixture.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	if fixture.cloudCalls != 0 {
		t.Fatalf("suite Cloud calls = %d", fixture.cloudCalls)
	}
	gotVersion := invoke("--version")
	assertResult(t, gotVersion, 0, version+"\n", "")
	var metadata map[string]string
	if err := json.Unmarshal(fixture.releaseJSON, &metadata); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"sha": cliSuiteSHA, "built": "2026-10-08T14:03:12Z", "devctl": strings.TrimSuffix(gotVersion.stdout, "\n")}
	if !reflect.DeepEqual(metadata, want) {
		t.Fatalf("release metadata = %#v, want %#v", metadata, want)
	}
}

func TestSuiteRunsOnlyHostManifestsThroughCLI(t *testing.T) {
	// R-6AHP-M80N
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "crm"}
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	if !reflect.DeepEqual(f.manifestCalls, map[string]int{"auth": 1, "crm": 1}) {
		t.Fatalf("manifest calls %v", f.manifestCalls)
	}
	for _, c := range f.commands {
		if !filepath.IsAbs(c.Path) {
			continue
		}
		app := filepath.Base(c.Dir)
		if c.Path != f.compilerOutputs[app] || c.Path == f.releaseOutputs[app] || !slices.Equal(c.Args, []string{"manifest"}) {
			t.Fatalf("unexpected executable run %#v", c)
		}
	}
}

func TestSuiteRejectsNamesThroughCLI(t *testing.T) {
	// R-4XK6-M5A6
	for _, names := range [][]string{{"0app", "host"}, {"0app", "seed"}, {"0app", "Crm"}, {"0app", "opsctl"}, {"0app", "turns"}, {"0app", "host", "opsctl", "seed"}} {
		t.Run(strings.Join(names, "_"), func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = names
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: '"+names[1]+"' is not a usable app name\n")
			f.assertNoCompile(t)
		})
	}
}

func TestSuiteRejectsForbiddenDirectoriesThroughCLI(t *testing.T) {
	// R-4YS2-ZX0V
	cases := []struct {
		apps  []string
		files map[string]string
		want  string
	}{
		{[]string{"auth", "crm"}, map[string]string{"crm/sbin/tool": "tool"}, "crm: sbin/ is not allowed in a release"},
		{[]string{"auth", "crm"}, map[string]string{"crm/include/x.h": "header"}, "crm: include/ is not allowed in a release"},
		{[]string{"auth", "crm"}, map[string]string{"crm/sbin/tool": "tool", "auth/include/x.h": "header"}, "auth: include/ is not allowed in a release"},
		{[]string{"auth", "crm", "host"}, map[string]string{"crm/sbin/tool": "tool", "auth/include/x.h": "header"}, "'host' is not a usable app name"},
	}
	for _, test := range cases {
		t.Run(test.want, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = test.apps
			f.files = test.files
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: "+test.want+"\n")
			f.assertNoCompile(t)
		})
	}
}

func TestSuiteStopsOnCompilerFailureThroughCLI(t *testing.T) {
	// R-D1I9-ZIEJ
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "dashboard", "events"}
	f.failCompile = "dashboard"
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if !reflect.DeepEqual(f.compiled, []string{"auth", "auth", "dashboard"}) || f.tarCalls != 0 {
		t.Fatalf("compiled %q, tar calls %d", f.compiled, f.tarCalls)
	}
}

func TestSuiteToolCompilerFailuresThroughCLI(t *testing.T) {
	// R-D1I9-ZIEJ
	for _, tool := range []string{"opsctl", "turns"} {
		t.Run(tool, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = []string{"crm"}
			f.failCompile = tool
			want := "devctl: build " + tool + ": exit status 1\n"
			compiled := []string{"crm", "crm", "opsctl"}
			if tool == "turns" {
				want += "\n> cmd/turns/main.go:7:1: syntax error\n"
				compiled = append(compiled, "turns")
			}
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", want)
			if !reflect.DeepEqual(f.compiled, compiled) || f.tarCalls != 0 || f.removals != 1 {
				t.Fatalf("compiled %q, tar %d, removals %d", f.compiled, f.tarCalls, f.removals)
			}
		})
	}
}

func TestSuiteStopsOnManifestBuildFailureThroughCLI(t *testing.T) {
	// R-D1I9-ZIEJ
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "dashboard", "events"}
	f.failManifestCompile = "dashboard"
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: build dashboard: exit status 1\n\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if !reflect.DeepEqual(f.compiled, []string{"auth", "auth", "dashboard", "dashboard"}) || f.manifestCalls["dashboard"] != 0 || f.tarCalls != 0 {
		t.Fatalf("compiled %q, manifest calls %v, tar calls %d", f.compiled, f.manifestCalls, f.tarCalls)
	}
	for _, c := range f.commands {
		if c.Path == "go" {
			if _, err := os.Stat(suiteCompilerOutput(c.Args)); !os.IsNotExist(err) {
				t.Fatalf("compile output remains after failure: %#v: %v", c, err)
			}
		}
	}
}

func TestSuiteCompileOrderThroughCLI(t *testing.T) {
	// R-699T-8G9Y
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "dummy", "events", "mcp", "repos", "scripts", "sites", "telemetry"}
	f.write(filepath.Join(f.root, "go.work"), "go 1.26\n")
	deps := f.deps()
	exec := deps.Exec
	var compiles []seam.Cmd
	deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "go" {
			compiles = append(compiles, c)
		}
		return exec(ctx, c)
	}
	assertResult(t, invokeWithDeps(deps, "build", "release-87"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	var names []string
	for _, app := range f.apps {
		names = append(names, app, app)
	}
	names = append(names, "opsctl", "turns")
	if len(compiles) != len(names) || !reflect.DeepEqual(f.compiled, names) {
		t.Fatalf("compile names %q, commands %#v", f.compiled, compiles)
	}
	for i, c := range compiles {
		goos, goarch, output := "linux", "arm64", f.releaseOutputs[names[i]]
		if i < len(f.apps)*2 && i%2 == 1 {
			goos, goarch, output = runtime.GOOS, runtime.GOARCH, f.compilerOutputs[names[i]]
		}
		env := slices.Clone(c.Env)
		slices.Sort(env)
		wantEnv := []string{"CGO_ENABLED=0", "GOARCH=" + goarch, "GOOS=" + goos, "GOWORK=off"}
		wantArgs := []string{"build", "-buildvcs=false", "-o", output, "./cmd/" + names[i]}
		if c.Dir != filepath.Join(f.worktree, names[i]) || !slices.Equal(env, wantEnv) {
			t.Fatalf("compile command %#v, want env %q", c, wantEnv)
		}
		if i < len(f.apps)*2 && i%2 == 1 {
			if !slices.Equal(c.Args, wantArgs) {
				t.Fatalf("manifest args %q, want %q", c.Args, wantArgs)
			}
		} else {
			if len(c.Args) == 0 || c.Args[0] != "build" || !slices.Contains(c.Args, "-buildvcs=false") || !slices.Contains(c.Args, "./cmd/"+names[i]) || suiteCompilerOutput(c.Args) != output {
				t.Fatalf("release compile command %#v", c)
			}
			for _, arg := range c.Args {
				if strings.HasPrefix(arg, "-ldflags") {
					t.Fatalf("linker flags in compile command %#v", c)
				}
			}
		}
	}
}

func TestSuiteMissingTurnsShareThroughCLI(t *testing.T) {
	// R-517V-RGI9
	for _, missing := range []string{"turns", "share", "share file", "forbidden app"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s existing dist=%v", missing, existing), func(t *testing.T) {
				f := newCLISuiteFixture(t)
				f.apps = []string{"crm"}
				f.omitTurns = missing == "turns"
				f.omitShare = true
				f.shareFile = missing == "share file"
				want := "turns/share/ is missing at the commit"
				if missing == "forbidden app" {
					f.files = map[string]string{"crm/sbin/tool": "tool"}
					want = "crm: sbin/ is not allowed in a release"
				}
				if existing {
					f.write(filepath.Join(f.root, "dist", "keep"), "keep")
					f.write(filepath.Join(f.root, "dist", cliSuiteSHA+".tar.xz"), "earlier release")
				}
				// A developer's copy cannot fill the missing commit content.
				f.write(filepath.Join(f.root, "turns", "share", "local"), "uncommitted")
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: "+want+"\n")
				f.assertNoCompile(t)
				if f.removals != 1 {
					t.Fatalf("removals %d", f.removals)
				}
				entries, err := os.ReadDir(filepath.Join(f.root, "dist"))
				if !existing {
					if !os.IsNotExist(err) {
						t.Fatalf("new dist remains: %v", err)
					}
				} else {
					if err != nil || len(entries) != 2 || entries[0].Name() != cliSuiteSHA+".tar.xz" || entries[1].Name() != "keep" {
						t.Fatalf("dist entries %v, %v", entries, err)
					}
					root, err := os.OpenRoot(filepath.Join(f.root, "dist"))
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := root.Close(); err != nil {
							t.Error(err)
						}
					})
					for name, want := range map[string]string{"keep": "keep", cliSuiteSHA + ".tar.xz": "earlier release"} {
						data, err := root.ReadFile(name)
						if err != nil || string(data) != want {
							t.Fatalf("prior %s: %q, %v", name, data, err)
						}
					}
				}
			})
		}
	}
}

func TestSuiteRemovalFailureThroughCLI(t *testing.T) {
	// R-G8NW-9VLR
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = []string{"crm"}
			f.removeFails = true
			f.stale = stale
			final := filepath.Join(f.root, "dist", cliSuiteSHA+".tar.xz")
			f.write(final, "earlier artifact")
			if stale {
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit\n")
			} else {
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: git worktree remove --force "+f.worktree+": exit status 1\n\n> could not remove worktree\n")
			}
			contents, err := os.ReadFile(filepath.Clean(final))
			if err != nil || string(contents) != "earlier artifact" {
				t.Fatalf("earlier artifact = %q, %v", contents, err)
			}
			if f.removals != 1 {
				t.Fatalf("removals = %d, want 1", f.removals)
			}
		})
	}
}

func TestSuiteManifestErrorsThroughCLI(t *testing.T) {
	// R-FWGW-G66T R-D2Q6-DA58
	t.Run("port", func(t *testing.T) {
		f := newCLISuiteFixture(t)
		f.apps = []string{"crm"}
		f.files = map[string]string{"crm/etc/manifest.toml": "app = \"crm\"\nport = 8080\n"}
		assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket\n")
		f.assertNoCompile(t)
	})
	for _, worktreeStale := range []bool{false, true} {
		t.Run(fmt.Sprint(worktreeStale), func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = []string{"crm"}
			if worktreeStale {
				f.files = map[string]string{"crm/etc/manifest.toml": "app = \"crm\"\n# worktree differs\n"}
				f.write(filepath.Join(f.root, "crm", "etc", "manifest.toml"), "app = \"crm\"\n")
			} else {
				f.write(filepath.Join(f.root, "crm", "etc", "manifest.toml"), "app = \"crm\"\n# developer differs\n")
			}
			if worktreeStale {
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit\n")
			} else {
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
			}
		})
	}
}

func TestSuiteCommitRefusalsThroughCLI(t *testing.T) {
	// R-FS1R-KWJT R-R5Q1-KQAF
	for _, operand := range []string{"bogus", "crm", "main", "HEAD", "HEAD~1"} {
		t.Run(operand, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.noCommit = true
			f.write(filepath.Join(f.root, "crm", "cmd", "crm", "main.go"), "package main\n")
			f.write(filepath.Join(f.root, "crm", "etc", "manifest.toml"), `app = "crm"`+"\n")
			assertResult(t, invokeWithDeps(f.deps(), "build", operand), 2, "", "devctl: '"+operand+"' is not a commit\n")
			f.assertNoCompile(t)
			if f.worktree != "" {
				t.Fatal("worktree added after refused commit")
			}
			if _, err := os.Stat(filepath.Join(f.root, "dist")); !os.IsNotExist(err) {
				t.Fatalf("dist stat = %v, want absent", err)
			}
		})
	}
	f := newCLISuiteFixture(t)
	f.resolveFails = true
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: git rev-parse: cannot start git\n")
	if f.worktree != "" {
		t.Fatal("worktree added after resolution failure")
	}
}

func TestSuiteArchiveFailureThroughCLI(t *testing.T) {
	// R-GB3P-1F35
	f := newCLISuiteFixture(t)
	f.apps = []string{"crm"}
	f.tarFails = true
	final := filepath.Join(f.root, "dist", cliSuiteSHA+".tar.xz")
	f.write(final, "earlier artifact")
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: archive release: exit status 7\n\n> compression failed\n")
	data, err := os.ReadFile(filepath.Clean(final))
	if err != nil || string(data) != "earlier artifact" {
		t.Fatalf("artifact %q, %v", data, err)
	}
	if f.removals != 1 {
		t.Fatalf("worktree removals = %d", f.removals)
	}
	if _, err := os.Stat(f.worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree stat = %v", err)
	}
}

func TestSuiteOutputFromSubdirectoryThroughCLI(t *testing.T) {
	// R-FZMP-SU4T
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "crm"}
	deps := f.deps()
	deps.Dir = filepath.Join(f.root, "auth")
	f.write(filepath.Join(deps.Dir, "keep"), "unchanged")
	final := filepath.Join(f.root, "dist", cliSuiteSHA+".tar.xz")
	f.write(final, "earlier artifact")
	assertResult(t, invokeWithDeps(deps, "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	data, err := os.ReadFile(filepath.Clean(final))
	if err != nil || string(data) != "fake archive" {
		t.Fatalf("published artifact %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Join(f.root, "dist"))
	if err != nil || len(entries) != 1 || entries[0].Name() != cliSuiteSHA+".tar.xz" {
		t.Fatalf("dist entries %v, %v", entries, err)
	}
	data, err = os.ReadFile(filepath.Join(deps.Dir, "keep"))
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("outside dist changed: %q, %v", data, err)
	}
}

type cliSuiteFixture struct {
	t                                *testing.T
	root, worktree                   string
	apps                             []string
	files                            map[string]string
	failCompile, failManifestCompile string
	noCommit, resolveFails, tarFails bool
	stale, removeFails               bool
	manifestFails, manifestNewline   bool
	omitTurns, omitShare, shareFile  bool
	cloudCalls, tarCalls, removals   int
	compiled                         []string
	commands                         []seam.Cmd
	compilerOutputs                  map[string]string
	releaseOutputs                   map[string]string
	manifestCalls                    map[string]int
	releaseJSON                      []byte
}

func newCLISuiteFixture(t *testing.T) *cliSuiteFixture {
	t.Helper()
	return &cliSuiteFixture{t: t, root: t.TempDir()}
}
func (f *cliSuiteFixture) write(path, contents string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *cliSuiteFixture) deps() seam.Deps {
	return seam.Deps{Dir: f.root, EUID: 1, Getenv: func(string) string { return "" }, Exec: f.exec, Now: func() time.Time {
		return time.Date(2026, 10, 8, 9, 3, 12, 900000000, time.FixedZone("offset", -5*3600))
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		f.cloudCalls++
		return cloud.Clients{}, errors.New("unexpected Cloud")
	}}
}
func (f *cliSuiteFixture) assertNoCompile(t *testing.T) {
	t.Helper()
	if len(f.compiled) != 0 || f.tarCalls != 0 {
		t.Fatalf("compiled %q, tar %d", f.compiled, f.tarCalls)
	}
}
func (f *cliSuiteFixture) exec(_ context.Context, c seam.Cmd) (seam.Result, error) {
	f.commands = append(f.commands, c)
	switch c.Path {
	case "git":
		switch {
		case reflect.DeepEqual(c.Args, []string{"rev-parse", "--show-toplevel"}):
			return seam.Result{Stdout: []byte(f.root + "\n")}, nil
		case len(c.Args) > 1 && c.Args[0] == "rev-parse" && c.Args[1] == "--verify":
			if f.resolveFails {
				return seam.Result{}, errors.New("cannot start git")
			}
			if f.noCommit {
				if strings.HasPrefix(c.Args[len(c.Args)-1], "refs/tags/") {
					return seam.Result{ExitCode: 1}, nil
				}
				return seam.Result{Stdout: []byte(cliSuiteSHA + "\n")}, nil
			}
			if c.Args[len(c.Args)-1] != "refs/tags/r1^{commit}" && c.Args[len(c.Args)-1] != "refs/tags/release-87^{commit}" {
				f.t.Fatalf("unexpected resolution: %q", c.Args)
			}
			return seam.Result{Stdout: []byte(cliSuiteSHA + "\n")}, nil
		case len(c.Args) == 5 && reflect.DeepEqual(c.Args[:3], []string{"worktree", "add", "--detach"}):
			f.worktree = c.Args[3]
			if c.Args[4] != cliSuiteSHA {
				f.t.Fatalf("worktree sha: %q", c.Args)
			}
			for _, app := range f.apps {
				f.write(filepath.Join(f.worktree, app, "cmd", app, "main.go"), "package main\n")
				f.write(filepath.Join(f.worktree, app, "etc", "manifest.toml"), "app = \""+app+"\"\n")
			}
			for path, contents := range f.files {
				f.write(filepath.Join(f.worktree, path), contents)
			}
			f.write(filepath.Join(f.worktree, "opsctl", "cmd", "opsctl", "main.go"), "package main\n")
			if !f.omitTurns {
				f.write(filepath.Join(f.worktree, "turns", "cmd", "turns", "main.go"), "package main\n")
				f.write(filepath.Join(f.worktree, "turns", "go.mod"), "module fixture\n")
				if f.shareFile {
					f.write(filepath.Join(f.worktree, "turns", "share"), "not a directory")
				} else if !f.omitShare {
					f.write(filepath.Join(f.worktree, "turns", "share", "systemd", "turns.service"), "service from commit")
					f.write(filepath.Join(f.worktree, "turns", "share", "systemd", "turns.timer"), "timer from commit")
				}
			}
			return seam.Result{}, nil
		case len(c.Args) == 4 && reflect.DeepEqual(c.Args[:3], []string{"worktree", "remove", "--force"}):
			f.removals++
			if c.Args[3] != f.worktree {
				f.t.Fatalf("remove worktree: %q", c.Args)
			}
			if f.removeFails {
				return seam.Result{ExitCode: 1, Stderr: []byte("could not remove worktree\n")}, nil
			}
			return seam.Result{}, os.RemoveAll(f.worktree)
		default:
			f.t.Fatalf("unexpected git: %q", c.Args)
		}
	case "go":
		app := filepath.Base(c.Dir)
		f.compiled = append(f.compiled, app)
		isManifest := f.compilerOutputs[app] != ""
		if isManifest && app == f.failManifestCompile {
			f.write(suiteCompilerOutput(c.Args), "partial manifest binary")
			return seam.Result{ExitCode: 1, Stderr: []byte("cmd/dashboard/main.go:41:2: undefined: render\n")}, nil
		}
		if !isManifest && app == f.failCompile {
			stderr := "# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\ncmd/dashboard/main.go:41:2: undefined: render\n"
			if app == "opsctl" {
				stderr = ""
			}
			if app == "turns" {
				stderr = "cmd/turns/main.go:7:1: syntax error\n"
			}
			return seam.Result{ExitCode: 1, Stderr: []byte(stderr)}, nil
		}
		if output := suiteCompilerOutput(c.Args); output != "" {
			if f.compilerOutputs == nil {
				f.compilerOutputs = make(map[string]string)
				f.releaseOutputs = make(map[string]string)
			}
			contents := "binary " + app
			if isManifest {
				contents = "manifest binary " + app
			} else {
				f.releaseOutputs[app] = output
			}
			f.write(output, contents)
			f.compilerOutputs[app] = output
			return seam.Result{}, nil
		}
		f.t.Fatalf("go lacks output: %q", c.Args)
	case "tar":
		for _, app := range f.apps {
			if f.manifestCalls[app] != 1 {
				f.t.Fatalf("manifest calls for %s = %d, want one", app, f.manifestCalls[app])
			}
		}
		if f.manifestCalls["opsctl"] != 0 || f.manifestCalls["turns"] != 0 {
			f.t.Fatal("tool binary was executed")
		}
		f.tarCalls++
		if f.tarFails {
			return seam.Result{ExitCode: 7, Stderr: []byte("compression failed\n")}, nil
		}
		if len(c.Args) < 4 || c.Args[0] != "-cJf" || c.Args[2] != "-C" {
			f.t.Fatalf("unexpected tar: %q", c.Args)
		}
		data, err := os.ReadFile(filepath.Join(c.Args[3], cliSuiteSHA, "release.json"))
		if err != nil {
			f.t.Fatal(err)
		}
		f.releaseJSON = data
		f.write(c.Args[1], "fake archive")
		return seam.Result{}, nil
	default:
		for _, output := range f.releaseOutputs {
			if c.Path == output {
				return seam.Result{}, errors.New("cannot execute release binary on build host")
			}
		}
		if reflect.DeepEqual(c.Args, []string{"--version"}) {
			return seam.Result{ExitCode: 1}, nil
		}
		if !reflect.DeepEqual(c.Args, []string{"manifest"}) {
			f.t.Fatalf("unexpected binary: %#v", c)
		}
		app := filepath.Base(c.Path)
		if app == "opsctl" || app == "turns" || f.compilerOutputs[app] == "" || c.Path != f.compilerOutputs[app] {
			f.t.Fatalf("manifest path = %q, want compiled app binary %q", c.Path, f.compilerOutputs[app])
		}
		if f.manifestCalls == nil {
			f.manifestCalls = make(map[string]int)
		}
		f.manifestCalls[app]++
		if f.manifestCalls[app] != 1 {
			f.t.Fatalf("duplicate manifest call for %s", app)
		}
		if f.manifestFails {
			return seam.Result{ExitCode: 1, Stderr: []byte("boom")}, nil
		}
		if f.manifestNewline {
			return seam.Result{Stdout: []byte("app = \"" + app + "\"\n\n")}, nil
		}
		if f.stale {
			return seam.Result{Stdout: []byte("stale")}, nil
		}
		return seam.Result{Stdout: []byte("app = \"" + app + "\"\n")}, nil
	}
	return seam.Result{}, errors.New("unexpected process")
}

func suiteCompilerOutput(args []string) string {
	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) {
			return args[i+1]
		}
		if output, ok := strings.CutPrefix(arg, "-o="); ok {
			return output
		}
	}
	return ""
}
