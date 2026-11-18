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
	// R-24KV-V7X0
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "crm"}
	f.files = map[string]string{"crm/cmd/crmd/main.go": "package main\n"}
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	wantRuns := make(map[string]int, len(f.apps))
	for _, app := range f.apps {
		wantRuns[f.compilerOutputs[app]]++
	}
	runs := make(map[string]int, len(f.apps))
	for _, c := range f.commands {
		if c.Path == "git" || c.Path == "go" || c.Path == "tar" {
			continue
		}
		if wantRuns[c.Path] == 0 || f.extraOutputs[c.Path] || !slices.Equal(c.Args, []string{"manifest"}) {
			t.Fatalf("unexpected executable run %#v", c)
		}
		for _, path := range f.releaseOutputs {
			if c.Path == path {
				t.Fatalf("executed release binary %#v", c)
			}
		}
		runs[c.Path]++
	}
	if !reflect.DeepEqual(runs, wantRuns) {
		t.Fatalf("manifest runs %v, want %v", runs, wantRuns)
	}
}

func TestSuiteRejectsUnusableExtraCommandNamesThroughCLI(t *testing.T) {
	// R-20X6-PWOX R-23CZ-HG6B
	for _, name := range []string{"...", ".hidden", "_x", "Tool", "host"} {
		t.Run(name, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = []string{"auth", "crm"}
			f.files = map[string]string{"crm/cmd/" + name + "/README.md": "no Go files"}
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: crm: cmd/"+name+" is not a usable command name\n")
			f.assertNoCompile(t)
		})
	}
	for _, test := range []struct {
		name      string
		apps      []string
		files     map[string]string
		omitShare bool
		want      string
	}{
		{
			name: "first directory", apps: []string{"auth", "crm"},
			files: map[string]string{"crm/cmd/Tool/README.md": "", "crm/cmd/.hidden/README.md": ""},
			want:  "crm: cmd/.hidden is not a usable command name",
		},
		{
			name: "first app", apps: []string{"auth", "crm"},
			files: map[string]string{"auth/cmd/_x/README.md": "", "crm/cmd/.../README.md": ""},
			want:  "auth: cmd/_x is not a usable command name",
		},
		{
			name: "forbidden directory", apps: []string{"auth", "crm"},
			files: map[string]string{"crm/cmd/.../README.md": "", "crm/sbin/tool": "tool"},
			want:  "crm: sbin/ is not allowed in a release",
		},
		{
			name: "missing share", apps: []string{"auth", "crm"}, omitShare: true,
			files: map[string]string{"crm/cmd/.../README.md": ""},
			want:  "turns/share/ is missing at the commit",
		},
		{
			name: "invalid app name", apps: []string{"auth", "crm", "host"},
			files: map[string]string{"crm/cmd/.../README.md": ""},
			want:  "'host' is not a usable app name",
		},
		{
			name: "broken app manifest", apps: []string{"auth", "crm"},
			files: map[string]string{"auth/cmd/.../README.md": "", "crm/etc/manifest.toml": "app = \"crm\"\nport = 8080\n"},
			want:  "crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = test.apps
			f.files = test.files
			f.omitShare = test.omitShare
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: "+test.want+"\n")
			f.assertNoCompile(t)
		})
	}
}

func TestSuiteAcceptsUsableExtraCommandNamesThroughCLI(t *testing.T) {
	// R-20X6-PWOX
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "crm"}
	f.files = map[string]string{
		"crm/cmd/...":              "regular file",
		"crm/cmd/opsctl/main.go":   "package main\n",
		"crm/cmd/testdata/main.go": "package main\n",
		"crm/cmd/turns/main.go":    "package main\n",
		"opsctl/cmd/.x/README.md":  "",
		"turns/cmd/.x/README.md":   "",
	}
	f.mutate = func(dir string) {
		if err := os.Symlink("crm", filepath.Join(dir, "crm", "cmd", ".x")); err != nil {
			t.Fatal(err)
		}
	}
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	var packages []string
	for _, c := range f.commands {
		if c.Path != "go" {
			continue
		}
		if slices.Contains(c.Args, "./cmd/...") || slices.Contains(c.Args, "./cmd/.x") {
			t.Fatalf("compile named an excluded command: %#v", c)
		}
		pkg := c.Args[len(c.Args)-1]
		if pkg == "./cmd/"+filepath.Base(c.Dir) {
			continue
		}
		if c.Dir != filepath.Join(f.worktree, "crm") {
			t.Fatalf("extra compile outside crm: %#v", c)
		}
		packages = append(packages, pkg)
	}
	if want := []string{"./cmd/opsctl", "./cmd/testdata", "./cmd/turns"}; !slices.Equal(packages, want) {
		t.Fatalf("extra packages %q, want %q", packages, want)
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
	// R-WI1B-ENCZ
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "dashboard", "events"}
	f.failCompile = "dashboard"
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if !reflect.DeepEqual(f.compiled, []string{"auth", "auth", "dashboard"}) || f.tarCalls != 0 {
		t.Fatalf("compiled %q, tar calls %d", f.compiled, f.tarCalls)
	}
}

func TestSuiteToolCompilerFailuresThroughCLI(t *testing.T) {
	// R-WI1B-ENCZ
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
	// R-WI1B-ENCZ
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
	// R-25SS-8ZNP
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
			outputIndex := slices.Index(c.Args, "-o")
			if len(c.Args) == 0 || c.Args[0] != "build" || !slices.Contains(c.Args, "-buildvcs=false") ||
				c.Args[len(c.Args)-1] != "./cmd/"+names[i] || outputIndex < 0 || outputIndex+1 >= len(c.Args) || c.Args[outputIndex+1] != output {
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

func TestSuiteExtraCompileOrderThroughCLI(t *testing.T) {
	// R-25SS-8ZNP
	f := newCLISuiteFixture(t)
	f.apps = []string{"auth", "crm"}
	f.files = map[string]string{
		"crm/cmd/crmd/main.go": "package main\n", "crm/cmd/crmctl/main.go": "package main\n",
	}
	assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
	var steps []string
	for _, c := range f.commands {
		switch c.Path {
		case "go":
			step := c.Args[len(c.Args)-1]
			if suiteCompilerOutput(c.Args) == f.compilerOutputs[filepath.Base(c.Dir)] {
				step += " (host)"
			}
			steps = append(steps, step)
		case f.compilerOutputs["crm"]:
			steps = append(steps, "crm manifest")
		}
	}
	want := []string{
		"./cmd/auth", "./cmd/auth (host)", "./cmd/crm", "./cmd/crm (host)", "crm manifest",
		"./cmd/crmctl", "./cmd/crmd", "./cmd/opsctl", "./cmd/turns",
	}
	if !slices.Equal(steps, want) {
		t.Fatalf("build steps %q, want %q", steps, want)
	}
}

func TestSuiteDiscoversExtraCommandDirectoriesThroughCLI(t *testing.T) {
	// R-23CZ-HG6B
	for _, file := range []struct{ name, contents string }{
		{"README.md", "no Go files"},
		{"pkg.go", "package pkg\n"},
	} {
		t.Run(file.name, func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = []string{"auth", "crm"}
			f.files = map[string]string{
				"crm/cmd/crmd/main.go": "package main\n", "crm/cmd/zz/" + file.name: file.contents,
				"crm/cmd/notes.txt":            "regular file",
				"opsctl/cmd/opshelper/main.go": "package main\n", "turns/cmd/turnsd/main.go": "package main\n",
			}
			f.mutate = func(dir string) {
				if err := os.Symlink("crmd", filepath.Join(dir, "crm", "cmd", "link")); err != nil {
					t.Fatal(err)
				}
			}
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 0, "dist/"+cliSuiteSHA+".tar.xz\n", "")
			var packages []string
			for _, c := range f.commands {
				if c.Path != "go" {
					continue
				}
				for _, excluded := range []string{"./cmd/notes.txt", "./cmd/link", "./cmd/opshelper", "./cmd/turnsd"} {
					if slices.Contains(c.Args, excluded) {
						t.Fatalf("compile named an excluded command: %#v", c)
					}
				}
				pkg := c.Args[len(c.Args)-1]
				if pkg == "./cmd/"+filepath.Base(c.Dir) {
					continue
				}
				packages = append(packages, pkg)
				env := slices.Clone(c.Env)
				slices.Sort(env)
				wantEnv := []string{"CGO_ENABLED=0", "GOARCH=arm64", "GOOS=linux", "GOWORK=off"}
				outputIndex := slices.Index(c.Args, "-o")
				if c.Dir != filepath.Join(f.worktree, "crm") || !slices.Equal(env, wantEnv) ||
					c.Args[0] != "build" || !slices.Contains(c.Args, "-buildvcs=false") || !slices.Contains(c.Args, "-buildmode=exe") ||
					outputIndex < 0 || outputIndex+1 >= len(c.Args) || !f.extraOutputs[c.Args[outputIndex+1]] {
					t.Fatalf("extra compile %#v, want env %q", c, wantEnv)
				}
				for _, arg := range c.Args {
					if strings.HasPrefix(arg, "-ldflags") {
						t.Fatalf("linker flags in extra compile %#v", c)
					}
				}
			}
			if want := []string{"./cmd/crmd", "./cmd/zz"}; !slices.Equal(packages, want) {
				t.Fatalf("extra packages %q, want %q", packages, want)
			}
		})
	}
}

func TestSuiteStopsExtraCompilesOnFailureThroughCLI(t *testing.T) {
	// R-WI1B-ENCZ
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprintf("stale=%v", stale), func(t *testing.T) {
			f := newCLISuiteFixture(t)
			f.apps = []string{"auth", "crm", "events"}
			f.files = map[string]string{
				"crm/cmd/crmctl/main.go": "package main\n", "crm/cmd/crmd/main.go": "package main\n",
			}
			f.failExtra = "./cmd/crmctl"
			dist := filepath.Join(f.root, "dist")
			earlier := cliSuiteSHA + ".tar.xz"
			f.write(filepath.Join(dist, earlier), "earlier release")
			want := []string{"./cmd/auth", "./cmd/auth", "./cmd/crm", "./cmd/crm"}
			if stale {
				f.staleApp = "crm"
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit\n")
			} else {
				assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: build crm: exit status 1\n\n> cmd/crmctl/main.go:1:9: undefined: run\n")
				want = append(want, "./cmd/crmctl")
			}
			var packages []string
			for _, c := range f.commands {
				if c.Path == "go" {
					packages = append(packages, c.Args[len(c.Args)-1])
				}
			}
			if !slices.Equal(packages, want) || f.manifestCalls["crm"] != 1 || f.tarCalls != 0 {
				t.Fatalf("compile packages %q, want %q, manifest calls %v, tar calls %d", packages, want, f.manifestCalls, f.tarCalls)
			}
			entries, err := os.ReadDir(dist)
			if err != nil || len(entries) != 1 || entries[0].Name() != earlier {
				t.Fatalf("dist entries %v, %v", entries, err)
			}
			root, err := os.OpenRoot(dist)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			})
			data, err := root.ReadFile(earlier)
			if err != nil || string(data) != "earlier release" {
				t.Fatalf("earlier release %q, %v", data, err)
			}
		})
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
	mutate                           func(string)
	failCompile, failManifestCompile string
	failExtra, staleApp              string
	noCommit, resolveFails, tarFails bool
	stale, removeFails               bool
	manifestFails, manifestNewline   bool
	omitTurns, omitShare, shareFile  bool
	cloudCalls, tarCalls, removals   int
	compiled                         []string
	commands                         []seam.Cmd
	compilerOutputs                  map[string]string
	manifestApps                     map[string]string
	releaseOutputs                   map[string]string
	extraOutputs                     map[string]bool
	manifestCalls                    map[string]int
	releaseJSON                      []byte
}

func newCLISuiteFixture(t *testing.T) *cliSuiteFixture {
	t.Helper()
	return &cliSuiteFixture{
		t: t, root: t.TempDir(),
		compilerOutputs: map[string]string{}, manifestApps: map[string]string{}, releaseOutputs: map[string]string{}, extraOutputs: map[string]bool{},
	}
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
			if f.mutate != nil {
				f.mutate(f.worktree)
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
		pkg := c.Args[len(c.Args)-1]
		isExtra := pkg != "./cmd/"+app
		isManifest := !isExtra && f.releaseOutputs[app] != ""
		if isExtra && pkg == f.failExtra {
			f.write(suiteCompilerOutput(c.Args), "partial extra binary")
			return seam.Result{ExitCode: 1, Stderr: []byte("cmd/crmctl/main.go:1:9: undefined: run\n")}, nil
		}
		if isManifest && app == f.failManifestCompile {
			f.write(suiteCompilerOutput(c.Args), "partial manifest binary")
			return seam.Result{ExitCode: 1, Stderr: []byte("cmd/dashboard/main.go:41:2: undefined: render\n")}, nil
		}
		if !isManifest && !isExtra && app == f.failCompile {
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
			contents := "binary " + app
			switch {
			case isExtra:
				contents = "extra binary " + pkg
				f.extraOutputs[output] = true
			case isManifest:
				contents = "manifest binary " + app
				f.compilerOutputs[app] = output
				f.manifestApps[output] = app
			default:
				f.releaseOutputs[app] = output
			}
			f.write(output, contents)
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
		if f.extraOutputs[c.Path] {
			return seam.Result{}, errors.New("cannot execute extra binary on build host")
		}
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
		app, ok := f.manifestApps[c.Path]
		if !ok || app == "opsctl" || app == "turns" {
			f.t.Fatalf("manifest path = %q, compiled outputs = %v", c.Path, f.compilerOutputs)
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
		if f.stale || app == f.staleApp {
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
