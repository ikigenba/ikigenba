package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const cliSuiteSHA = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

func TestSuiteDispatchThroughCLI(t *testing.T) {
	// R-FBQL-Y2L0 R-FQDE-JBHC R-G683-IC4D
	fixture := newCLISuiteFixture(t)
	fixture.apps = []string{"crm"}
	// A developer app wins even when git could resolve its name as a commit.
	perApp := newCLIBuildFixture(t)
	assertResult(t, invokeWithDeps(perApp.deps(), "build", "crm"), 0, "crm/dist/crm-"+cliSuiteSHA+".tar.xz\n", "")
	for _, args := range perApp.gitArgs {
		if len(args) > 1 && args[0] == "rev-parse" && args[1] == "--verify" {
			t.Fatalf("per-app resolved commit: %q", args)
		}
	}
	if perApp.cloudCalls != 0 {
		t.Fatalf("per-app Cloud calls = %d", perApp.cloudCalls)
	}
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

func TestSuiteRejectsNamesThroughCLI(t *testing.T) {
	// R-GCBL-F6TU
	for _, names := range [][]string{{"0app", "host"}, {"0app", "seed"}, {"0app", "Crm"}, {"0app", "opsctl"}, {"0app", "host", "opsctl", "seed"}} {
		t.Run(strings.Join(names, "_"), func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = names
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: '"+names[1]+"' is not a usable app name\n")
			f.assertNoCompile(t)
		})
	}
}

func TestSuiteRejectsForbiddenDirectoriesThroughCLI(t *testing.T) {
	// R-FQTV-74T4
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
	// R-G1CH-Z95L
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "dashboard", "events"}
	f.failCompile = "dashboard"
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if !reflect.DeepEqual(f.compiled, []string{"auth", "dashboard"}) || f.tarCalls != 0 {
		t.Fatalf("compiled %q, tar calls %d", f.compiled, f.tarCalls)
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
	// R-FWGW-G66T R-G2KE-D0WA
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
	// R-FS1R-KWJT R-HZ5O-K5OE
	for _, operand := range []string{"bogus", "main", "HEAD", "HEAD~1"} {
		t.Run(operand, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.noCommit = true
			assertResult(t, invokeWithDeps(f.deps(), "build", operand), 2, "", "devctl: '"+operand+"' is neither an app in the checkout nor a commit\n")
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
	failCompile                      string
	noCommit, resolveFails, tarFails bool
	stale, removeFails               bool
	cloudCalls, tarCalls, removals   int
	compiled                         []string
	compilerOutputs                  map[string]string
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
	return seam.Deps{Dir: f.root, EUID: 1, Exec: f.exec, Now: func() time.Time {
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
			if c.Args[len(c.Args)-1] != "refs/tags/r1^{commit}" {
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
		if app == f.failCompile {
			return seam.Result{ExitCode: 1, Stderr: []byte("# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\ncmd/dashboard/main.go:41:2: undefined: render\n")}, nil
		}
		for i, arg := range c.Args {
			if arg == "-o" && i+1 < len(c.Args) {
				f.write(c.Args[i+1], "binary "+app)
				if f.compilerOutputs == nil {
					f.compilerOutputs = make(map[string]string)
				}
				f.compilerOutputs[app] = c.Args[i+1]
				return seam.Result{}, nil
			}
		}
		f.t.Fatalf("go lacks output: %q", c.Args)
	case "tar":
		for _, app := range f.apps {
			if f.manifestCalls[app] != 1 {
				f.t.Fatalf("manifest calls for %s = %d, want one", app, f.manifestCalls[app])
			}
		}
		if f.manifestCalls["opsctl"] != 0 {
			f.t.Fatal("opsctl binary was executed")
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
		if !reflect.DeepEqual(c.Args, []string{"manifest"}) {
			f.t.Fatalf("unexpected binary: %#v", c)
		}
		app := filepath.Base(c.Path)
		if app == "opsctl" || f.compilerOutputs[app] == "" || c.Path != f.compilerOutputs[app] {
			f.t.Fatalf("manifest path = %q, want compiled app binary %q", c.Path, f.compilerOutputs[app])
		}
		if f.manifestCalls == nil {
			f.manifestCalls = make(map[string]int)
		}
		f.manifestCalls[app]++
		if f.manifestCalls[app] != 1 {
			f.t.Fatalf("duplicate manifest call for %s", app)
		}
		if f.stale {
			return seam.Result{Stdout: []byte("stale")}, nil
		}
		return seam.Result{Stdout: []byte("app = \"" + app + "\"\n")}, nil
	}
	return seam.Result{}, errors.New("unexpected process")
}
