package cli

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func (f *upFixture) commandDirs(app string, names ...string) {
	f.t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(f.worktree, app, "cmd", name), 0700); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *upFixture) buildCommand(app, name string) seam.Cmd {
	return seam.Cmd{
		Path: "go",
		Args: []string{"build", "-o", filepath.Join(f.data, "stage", "bin", name), "./cmd/" + name},
		Dir:  filepath.Join(f.worktree, app),
	}
}

func (f *upFixture) chmodDir(path string, mode os.FileMode) {
	f.t.Helper()
	if err := upChmod(path, mode); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		if err := upChmod(path, 0700); err != nil {
			f.t.Error(err)
		}
	})
}

func (f *upFixture) requireOnlyToplevel() {
	f.t.Helper()
	want := []seam.Cmd{{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: f.worktree}}
	if !slices.EqualFunc(f.calls, want, sameUpCommand) {
		f.t.Fatalf("runs after refusal: %+v", f.calls)
	}
}

func sameUpCommand(a, b seam.Cmd) bool {
	return a.Path == b.Path && a.Dir == b.Dir && slices.Equal(a.Args, b.Args)
}

func TestUpDiscoversExtraCommandDirectories(t *testing.T) {
	// R-6DJ0-7CDF R-6EQW-L444
	for _, authCmd := range []string{"absent", "file"} {
		t.Run(authCmd, func(t *testing.T) {
			f := newUpFixture(t, "dummy", "auth")
			f.commandDirs("dummy", "dummy", "dummyd")
			f.commandDirs("docs", "tool")
			cmd := filepath.Join(f.worktree, "dummy", "cmd")
			f.put(filepath.Join(cmd, "notes"), "not a command", 0600)
			if err := os.Symlink("dummyd", filepath.Join(cmd, "alias")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("missing", filepath.Join(cmd, "gone")); err != nil {
				t.Fatal(err)
			}
			if authCmd == "file" {
				f.put(filepath.Join(f.worktree, "auth", "cmd"), "not a directory", 0600)
			}
			if code, _, diagnostic := f.run("up"); code != 0 {
				t.Fatalf("up: %d %s", code, diagnostic)
			}
			var builds []seam.Cmd
			for _, c := range f.calls {
				if c.Path == "go" {
					builds = append(builds, c)
				}
			}
			want := []seam.Cmd{f.buildCommand("auth", "auth"), f.buildCommand("dummy", "dummy"), f.buildCommand("dummy", "alias"), f.buildCommand("dummy", "dummyd")}
			if !slices.EqualFunc(builds, want, sameUpCommand) {
				t.Fatalf("builds %+v, want %+v", builds, want)
			}
		})
	}
}

func TestUpReportsFirstUnlistableCommandDirectory(t *testing.T) {
	// R-6H6P-CNLI R-6S5S-SL9R
	for _, first := range []string{"dummy", "auth"} {
		t.Run(first, func(t *testing.T) {
			f := newUpFixture(t, "dummy", "auth", "worker")
			f.commandDirs("auth", "worker")
			f.commandDirs("dummy", "dummy")
			f.chmodDir(filepath.Join(f.worktree, "dummy", "cmd"), 0300)
			if first == "auth" {
				f.chmodDir(filepath.Join(f.worktree, "auth", "cmd"), 0300)
			}
			code, out, diagnostic := f.run("up")
			want := "sandbox: " + filepath.Join(f.worktree, first, "cmd") + ": permission denied\n"
			if code != 1 || out != "" || diagnostic != want {
				t.Fatalf("up: %d %q %q, want %q", code, out, diagnostic, want)
			}
			f.requireOnlyToplevel()
		})
	}
}

func TestUpRefusesDuplicateProgramNames(t *testing.T) {
	// R-6IEL-QFC7 R-6EQW-L444 R-6S5S-SL9R R-6TDP-6D0G
	for _, test := range []struct {
		name            string
		auth, dummy     []string
		worker, badTOML bool
		want            string
	}{
		{name: "app and extra", auth: []string{"auth", "dummy"}, dummy: []string{"dummy"}, want: "'dummy': auth/cmd/dummy, dummy/cmd/dummy"},
		{name: "two extras", auth: []string{"auth", "worker"}, dummy: []string{"dummy", "worker"}, want: "'worker': auth/cmd/worker, dummy/cmd/worker"},
		{name: "three programs", auth: []string{"auth", "worker"}, dummy: []string{"dummy", "worker"}, worker: true, want: "'worker': auth/cmd/worker, dummy/cmd/worker, worker/cmd/worker"},
		{name: "two collisions", auth: []string{"auth", "dummy", "worker"}, dummy: []string{"dummy", "worker"}, want: "'dummy': auth/cmd/dummy, dummy/cmd/dummy"},
		{name: "missing main directory", auth: []string{"auth", "dummy"}, want: "'dummy': auth/cmd/dummy, dummy/cmd/dummy"},
		{name: "control characters", auth: []string{"a\nb"}, dummy: []string{"a\nb"}, want: `'a\x0ab': auth/cmd/a\x0ab, dummy/cmd/a\x0ab`},
		{name: "before manifests", auth: []string{"worker"}, dummy: []string{"worker"}, badTOML: true, want: "'worker': auth/cmd/worker, dummy/cmd/worker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, state := range []string{"new", "up"} {
				t.Run(state, func(t *testing.T) {
					f := newUpFixture(t, "auth", "dummy")
					if state == "up" {
						if code, _, diagnostic := f.run("up"); code != 0 {
							t.Fatalf("initial up: %d %s", code, diagnostic)
						}
						f.active = true
						f.calls = nil
					}
					f.commandDirs("auth", test.auth...)
					f.commandDirs("dummy", test.dummy...)
					if test.worker {
						f.put(filepath.Join(f.worktree, "worker", "etc", "manifest.toml"), "app = \"worker\"\n", 0644)
					}
					if test.badTOML {
						f.put(filepath.Join(f.worktree, "Dummy_2", "etc", "manifest.toml"), "[invalid", 0644)
					}
					before := upRefusalEntries(t, f.root)
					var deployed map[string]string
					if f.active {
						deployed = upStable(t, f, false)
					}
					code, out, diagnostic := f.run("up")
					want := "sandbox: more than one program named " + test.want + "\n"
					if code != 2 || out != "" || diagnostic != want {
						t.Fatalf("up: %d %q %q, want %q", code, out, diagnostic, want)
					}
					f.requireOnlyToplevel()
					upCheckRefusalSnapshot(t, f, before)
					if f.active {
						if !maps.Equal(upStable(t, f, false), deployed) {
							t.Fatal("refusal changed deployed files or registry")
						}
					} else {
						upMissing(t, filepath.Join(filepath.Dir(f.data), "registry.json"))
					}
				})
			}
		})
	}
}

func TestUpStopsOnExtraCommandBuildFailure(t *testing.T) {
	// R-6M2A-VQKA R-6NA7-9IAZ R-YYEQ-N40X
	for _, name := range []string{"dummyd", "beta"} {
		for _, result := range []string{"exit", "runner"} {
			t.Run(name+"/"+result, func(t *testing.T) {
				f := newUpFixture(t, "dummy")
				f.commandDirs("dummy", "dummy", "beta", "dummyd")
				var atFailure int
				buildOutput := "# github.com/ikigenba/ikigenba/dummy/cmd/" + name + "\ncmd/" + name + "/main.go:12:2: undefined: serve\n"
				f.exec = func(c seam.Cmd) (seam.Result, error) {
					if !sameUpCommand(c, f.buildCommand("dummy", name)) {
						return f.answer(c)
					}
					atFailure = len(f.calls)
					if result == "runner" {
						return seam.Result{}, errors.New("build runner failed")
					}
					return seam.Result{ExitCode: 1, Output: []byte(buildOutput)}, nil
				}
				code, out, diagnostic := f.run("up")
				want := "sandbox: build dummy: build runner failed\n"
				if result == "exit" {
					want = "sandbox: build dummy: exit status 1\n\n> " + strings.ReplaceAll(strings.TrimSuffix(buildOutput, "\n"), "\n", "\n> ") + "\n"
				}
				if code != 1 || out != "" || diagnostic != want {
					t.Fatalf("up: %d %q %q, want %q", code, out, diagnostic, want)
				}
				if atFailure == 0 || len(f.calls) != atFailure {
					t.Fatalf("runs after failed build: %+v", f.calls)
				}
			})
		}
	}
}

func TestUpRenamesAllStagedBinaries(t *testing.T) {
	// R-69VB-215C R-6OI3-NA1O
	f := newUpFixture(t, "dummy")
	f.commandDirs("dummy", "dummy", "dummyd")
	names := []string{"dummy", "dummyd"}
	f.registry(registryApp{Name: "dummy"})
	f.active = true
	for _, name := range names {
		bin := filepath.Join(f.data, "bin", name)
		f.put(bin, "old:"+name, 0755)
		if err := os.Link(bin, filepath.Join(f.root, name+".old")); err != nil {
			t.Fatal(err)
		}
	}
	staged := make(map[string]os.FileInfo, len(names))
	f.exec = func(c seam.Cmd) (seam.Result, error) {
		if c.Path == "nginx" {
			for _, name := range names {
				info, err := os.Stat(filepath.Join(f.data, "stage", "bin", name))
				if err != nil {
					t.Fatal(err)
				}
				staged[name] = info
			}
		}
		return f.answer(c)
	}
	if code, _, diagnostic := f.run("up"); code != 0 {
		t.Fatalf("up: %d %s", code, diagnostic)
	}
	for _, name := range names {
		bin := filepath.Join(f.data, "bin", name)
		info, err := os.Stat(bin)
		if err != nil {
			t.Fatal(err)
		}
		if staged[name] == nil || !os.SameFile(staged[name], info) {
			t.Fatalf("%s is not the staged file", bin)
		}
		if got := upRead(t, bin); got != "new:"+name {
			t.Fatalf("%s has unexpected content %q", bin, got)
		}
		if got := upRead(t, filepath.Join(f.root, name+".old")); got != "old:"+name {
			t.Fatalf("old binary %s was changed to %q", name, got)
		}
	}
}

func TestUpPrunesStaleBinariesBeforeDaemonReload(t *testing.T) {
	// R-6PQ0-11SD
	f := newUpFixture(t, "auth", "dummy")
	f.commandDirs("dummy", "dummy", "dummyd")
	if code, _, diagnostic := f.run("up"); code != 0 {
		t.Fatalf("initial up: %d %s", code, diagnostic)
	}
	if err := os.Remove(filepath.Join(f.worktree, "dummy", "cmd", "dummyd")); err != nil {
		t.Fatal(err)
	}
	f.active = true
	f.put(filepath.Join(f.data, "bin", "old", "nested", "file"), "old", 0600)
	f.put(filepath.Join(f.data, "env", "stale.env"), "old", 0600)
	checked := false
	f.exec = func(c seam.Cmd) (seam.Result, error) {
		if c.Path == "systemctl" && slices.Equal(c.Args, []string{"--user", "daemon-reload"}) {
			checked = true
			for dir, want := range map[string][]string{"bin": {"auth", "dummy"}, "env": {"auth.env", "dummy.env"}} {
				entries, err := os.ReadDir(filepath.Join(f.data, dir))
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, entry := range entries {
					got = append(got, entry.Name())
				}
				if !slices.Equal(got, want) {
					t.Fatalf("%s at daemon-reload: %v, want %v", dir, got, want)
				}
			}
		}
		return f.answer(c)
	}
	if code, _, diagnostic := f.run("up"); code != 0 || !checked {
		t.Fatalf("up: %d %s, reload seen: %t", code, diagnostic, checked)
	}
}

func TestUpExtraCommandOnlyAddsItsBuildAndBinary(t *testing.T) {
	// R-6ULL-K4R5
	f := newUpFixture(t, "auth", "dummy")
	f.commandDirs("dummy", "dummy")
	if code, _, diagnostic := f.run("up"); code != 0 {
		t.Fatalf("initial up: %d %s", code, diagnostic)
	}
	f.active = true
	before := upStable(t, f, false)
	var staged map[string]string
	f.exec = func(c seam.Cmd) (seam.Result, error) {
		if c.Path == "nginx" {
			staged = upSnapshot(t, filepath.Join(f.data, "stage"))
		}
		return f.answer(c)
	}
	f.calls = nil
	code, baselineOut, baselineErr := f.run("up")
	if code != 0 {
		t.Fatalf("up without extra: %d %s", code, baselineErr)
	}
	baselineCalls, baselineStaged := f.calls, staged
	if !maps.Equal(upStable(t, f, false), before) {
		t.Fatal("baseline redeploy changed the starting files")
	}

	f.commandDirs("dummy", "dummyd")
	f.calls = nil
	staged = nil
	code, out, diagnostic := f.run("up")
	if code != 0 || out != baselineOut || diagnostic != baselineErr {
		t.Fatalf("up with extra: %d %q %q, baseline %q %q", code, out, diagnostic, baselineOut, baselineErr)
	}
	extra := f.buildCommand("dummy", "dummyd")
	otherCalls := slices.DeleteFunc(f.calls, func(c seam.Cmd) bool { return sameUpCommand(c, extra) })
	if len(otherCalls)+1 != len(f.calls) || !slices.EqualFunc(otherCalls, baselineCalls, sameUpCommand) {
		t.Fatalf("runs with extra differ from baseline: %+v", otherCalls)
	}
	delete(staged, filepath.Join("bin", "dummyd"))
	deployed := upStable(t, f, false)
	delete(deployed, filepath.Join("bin", "dummyd"))
	if !maps.Equal(staged, baselineStaged) || !maps.Equal(deployed, before) {
		t.Fatal("extra command changed other staged or deployed entries")
	}
}
