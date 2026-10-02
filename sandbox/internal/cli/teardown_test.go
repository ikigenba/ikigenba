package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/cli"
	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

type teardownFixture struct {
	t                                 *testing.T
	base, root, data, units, worktree string
	deps                              seam.Deps
	calls                             []seam.Cmd
	lookups                           []seam.Cmd
	lookupAllowed                     bool
	out, err                          bytes.Buffer
	answer                            func(seam.Cmd, int) (seam.Result, error)
}

var teardownNames = []string{"sandbox-wip-nginx.service", "sandbox-wip-auth.socket", "sandbox-wip-auth.service", "sandbox-wip-dummy.socket", "sandbox-wip-dummy.service"}
var generatedNames = []string{"bin", "stage", "env", "services.json", "nginx"}

func newTeardownFixture(t *testing.T, present bool) *teardownFixture {
	t.Helper()
	base := t.TempDir()
	f := &teardownFixture{t: t, base: base, root: filepath.Join(base, "state/ikigenba/sandbox"), units: filepath.Join(base, "config/systemd/user"), worktree: filepath.Join(base, "wip")}
	f.data = filepath.Join(f.root, "wip")
	f.write(filepath.Join(f.root, "registry.json"), f.registryBytes())
	f.deps = seam.Deps{EUID: 1000, Dir: filepath.Join(base, "outside"), Getenv: func(key string) string {
		switch key {
		case "HOME":
			return base
		case "XDG_STATE_HOME":
			return filepath.Join(base, "state")
		case "XDG_CONFIG_HOME":
			return filepath.Join(base, "config")
		}
		return ""
	}}
	f.deps.Exec = func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "git" {
			f.lookups = append(f.lookups, cmd)
			if !f.lookupAllowed {
				t.Fatalf("unexpected checkout lookup: %#v", cmd)
			}
			return seam.Result{Stdout: []byte(f.worktree + "\n")}, nil
		}
		f.calls = append(f.calls, cmd)
		if f.answer != nil {
			return f.answer(cmd, len(f.calls)-1)
		}
		return seam.Result{Stdout: []byte("inactive\n")}, nil
	}
	f.deps.Stream = func(_ context.Context, cmd seam.Cmd, _ io.Writer) (seam.Result, error) {
		t.Fatalf("unexpected streaming call: %#v", cmd)
		return seam.Result{}, nil
	}
	if present {
		for _, unit := range teardownNames {
			f.write(filepath.Join(f.units, unit), []byte("unit"))
		}
		for _, name := range generatedNames {
			p := filepath.Join(f.data, name, "child")
			if name == "services.json" {
				p = filepath.Join(f.data, name)
			}
			f.write(p, []byte("generated"))
		}
		f.write(filepath.Join(f.data, "stage/bin/dummy"), []byte("staged"))
		f.write(filepath.Join(f.data, "apps/auth/state/value"), []byte("kept"))
		f.write(filepath.Join(f.data, "token"), []byte("secret"))
	}
	return f
}
func (f *teardownFixture) registryBytes() []byte {
	f.t.Helper()
	b, err := json.Marshal(map[string]any{"sandboxes": []any{
		map[string]any{"name": "wip", "port": 7400, "worktree": f.worktree, "apps": []any{map[string]any{"name": "dummy", "default": false}, map[string]any{"name": "auth", "default": true}}},
		map[string]any{"name": "other", "port": 7401, "worktree": filepath.Join(f.base, "other"), "apps": []any{}},
	}})
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}
func (f *teardownFixture) write(p string, b []byte) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *teardownFixture) run(args ...string) int {
	f.t.Helper()
	f.calls = nil
	f.lookups = nil
	f.lookupAllowed = len(args) == 1 && (args[0] == "down" || args[0] == "wipe")
	f.out.Reset()
	f.err.Reset()
	code := teardownRunChecked(context.Background(), f.t, args, nil, &f.out, &f.err, f.deps)
	var want []seam.Cmd
	if f.lookupAllowed {
		want = []seam.Cmd{{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: f.deps.Dir}}
	}
	if !reflect.DeepEqual(f.lookups, want) {
		f.t.Fatalf("checkout lookups: %#v; want %#v", f.lookups, want)
	}
	return code
}
func teardownRunChecked(ctx context.Context, t testing.TB, args []string, in io.Reader, out, stderr io.Writer, deps seam.Deps) int {
	t.Helper()
	code := cli.Run(ctx, args, in, out, stderr, deps)
	if code < 0 || code > 3 {
		t.Fatalf("exit code out of domain: %d", code)
	}
	return code
}
func (f *teardownFixture) expect(code, want int, diagnostic string) {
	f.t.Helper()
	if code != want || f.out.Len() != 0 || f.err.String() != diagnostic {
		f.t.Fatalf("code=%d stdout=%q stderr=%q; want %d and %q", code, f.out.String(), f.err.String(), want, diagnostic)
	}
}
func (f *teardownFixture) gone() {
	f.t.Helper()
	for _, unit := range teardownNames {
		assertAbsent(f.t, filepath.Join(f.units, unit))
	}
	for _, name := range generatedNames {
		assertAbsent(f.t, filepath.Join(f.data, name))
	}
}
func assertAbsent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s: expected absence, got %v", p, err)
	}
}
func assertPresent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
}
func stopCmd(unit string) seam.Cmd {
	return seam.Cmd{Path: "systemctl", Dir: "/", Args: []string{"--user", "stop", unit}}
}
func reloadCmd() seam.Cmd {
	return seam.Cmd{Path: "systemctl", Dir: "/", Args: []string{"--user", "daemon-reload"}}
}
func resetCmd(unit string) seam.Cmd {
	return seam.Cmd{Path: "systemctl", Dir: "/", Args: []string{"--user", "reset-failed", unit}}
}
func teardownSequence(stops bool) []seam.Cmd {
	var calls []seam.Cmd
	for _, u := range teardownNames {
		calls = append(calls, stateCmd(u))
	}
	if stops {
		for _, u := range teardownNames {
			calls = append(calls, stopCmd(u))
		}
	}
	calls = append(calls, reloadCmd())
	for _, u := range teardownNames {
		calls = append(calls, stateCmd(u))
	}
	return calls
}

type teardownSnapshot struct {
	mode    os.FileMode
	ino     uint64
	content string
}

func snapshotTeardown(t *testing.T, p string) map[string]teardownSnapshot {
	t.Helper()
	parent := filepath.Dir(p)
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	got := map[string]teardownSnapshot{}
	err = filepath.Walk(p, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		value := teardownSnapshot{mode: info.Mode(), ino: info.Sys().(*syscall.Stat_t).Ino}
		if info.Mode().IsRegular() {
			rel, e := filepath.Rel(parent, p)
			if e != nil {
				return e
			}
			b, e := root.ReadFile(rel)
			if e != nil {
				return e
			}
			value.content = string(b)
		}
		got[p] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func requireUnprivileged(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("permission test requires an ordinary user")
	}
}
func chmodTeardown(t *testing.T, p string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

// R-OB3F-0G6F R-ODJ7-RZNT R-DHIM-Z6C7 R-OIET-B2ML R-DJYF-QPTL R-DL6C-4HKA R-DNM4-W11O R-ONAE-U5LD R-OPQ7-LP2R R-53P6-QDDU R-DR9U-1C9R R-DSHQ-F40G R-OS60-D8K5
func TestDownSequenceAndPreservation(t *testing.T) {
	f := newTeardownFixture(t, true)
	kept := snapshotTeardown(t, filepath.Join(f.data, "apps"))
	token := snapshotTeardown(t, filepath.Join(f.data, "token"))
	reg := snapshotTeardown(t, filepath.Join(f.root, "registry.json"))
	f.answer = func(_ seam.Cmd, index int) (seam.Result, error) {
		for _, name := range generatedNames {
			assertPresent(t, filepath.Join(f.data, name))
		}
		if index < 10 {
			for _, u := range teardownNames {
				assertPresent(t, filepath.Join(f.units, u))
			}
		} else {
			for _, u := range teardownNames {
				assertAbsent(t, filepath.Join(f.units, u))
			}
		}
		return seam.Result{Stdout: []byte("inactive\n")}, nil
	}
	f.expect(f.run("down", "wip"), 0, "")
	if !reflect.DeepEqual(f.calls, teardownSequence(true)) {
		t.Fatalf("calls: %#v", f.calls)
	}
	f.gone()
	assertPresent(t, f.data)
	if !reflect.DeepEqual(kept, snapshotTeardown(t, filepath.Join(f.data, "apps"))) || !reflect.DeepEqual(token, snapshotTeardown(t, filepath.Join(f.data, "token"))) || !reflect.DeepEqual(reg, snapshotTeardown(t, filepath.Join(f.root, "registry.json"))) {
		t.Fatal("kept entries changed")
	}
}

// R-LI4Z-4WIF
func TestDownStopSelection(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "dangling", "directory", "active", "failed", "missing"} {
		t.Run(kind, func(t *testing.T) {
			f := newTeardownFixture(t, false)
			unit := teardownNames[0]
			p := filepath.Join(f.units, unit)
			switch kind {
			case "regular":
				f.write(p, []byte("unit"))
			case "symlink":
				f.write(filepath.Join(f.base, "target"), []byte("unit"))
				if err := os.MkdirAll(f.units, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.base, "target"), p); err != nil {
					t.Fatal(err)
				}
			case "dangling":
				if err := os.MkdirAll(f.units, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.base, "absent"), p); err != nil {
					t.Fatal(err)
				}
			case "directory":
				f.write(filepath.Join(p, "file"), []byte("child"))
			}
			f.answer = func(_ seam.Cmd, index int) (seam.Result, error) {
				state := "inactive"
				if index == 0 && (kind == "active" || kind == "failed") {
					state = kind
				}
				return seam.Result{Stdout: []byte(state + "\n")}, nil
			}
			f.expect(f.run("down", "wip"), 0, "")
			assertAbsent(t, p)
			stops := 0
			for _, c := range f.calls {
				if c.Args[1] == "stop" {
					stops++
					if !reflect.DeepEqual(c, stopCmd(unit)) {
						t.Fatalf("wrong stop %#v", c)
					}
				}
			}
			want := 0
			if kind == "regular" || kind == "symlink" || kind == "active" || kind == "failed" {
				want = 1
			}
			if stops != want {
				t.Fatalf("stops=%d want=%d", stops, want)
			}
		})
	}
}

// R-OFZ0-JJ57 R-YALD-SG0Y
func TestDownResetFailedInterleaving(t *testing.T) {
	f := newTeardownFixture(t, false)
	reloaded := false
	f.answer = func(c seam.Cmd, _ int) (seam.Result, error) {
		if c.Args[1] == "daemon-reload" {
			reloaded = true
		}
		state := "inactive"
		if reloaded && c.Args[1] == "show" && (c.Args[4] == teardownNames[2] || c.Args[4] == teardownNames[3]) {
			state = "failed"
		}
		return seam.Result{Stdout: []byte(state + "\n")}, nil
	}
	f.expect(f.run("down", "wip"), 0, "")
	want := teardownSequence(false)[:6]
	want = append(want, stateCmd(teardownNames[0]), stateCmd(teardownNames[1]), stateCmd(teardownNames[2]), resetCmd(teardownNames[2]), stateCmd(teardownNames[3]), resetCmd(teardownNames[3]), stateCmd(teardownNames[4]))
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls %#v", f.calls)
	}
}

// R-OULT-4S1J R-OVTP-IJS8 R-DL6C-4HKA
func TestDownAbsentAndNamedContexts(t *testing.T) {
	for _, kind := range []string{"up", "down", "gone", "outside"} {
		t.Run(kind, func(t *testing.T) {
			f := newTeardownFixture(t, kind == "up")
			if kind != "gone" {
				if err := os.MkdirAll(f.worktree, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "up" {
				f.answer = func(_ seam.Cmd, index int) (seam.Result, error) {
					state := "inactive"
					if index < 5 {
						state = "active"
					}
					return seam.Result{Stdout: []byte(state + "\n")}, nil
				}
			}
			if kind != "outside" {
				f.deps.Dir = f.worktree
			}
			f.expect(f.run("down", "wip"), 0, "")
			if kind != "up" {
				assertAbsent(t, f.data)
				assertAbsent(t, f.units)
				if !reflect.DeepEqual(f.calls, teardownSequence(false)) {
					t.Fatalf("calls %#v", f.calls)
				}
			}
		})
	}
}

// R-OX1L-WBIX R-OY9I-A39M R-OZHE-NV0B R-P0PB-1MR0 R-DTPM-SVR5 R-P353-T68E R-Y9DH-EOA9 R-OS60-D8K5
func TestDownProgramFailuresAndRetry(t *testing.T) {
	for _, point := range []int{0, 1, 5, 9, 10, 11, 13, 14} {
		for _, runnerError := range []bool{false, true} {
			t.Run(strconv.Itoa(point)+map[bool]string{false: "exit", true: "runner"}[runnerError], func(t *testing.T) {
				f := newTeardownFixture(t, true)
				reg := snapshotTeardown(t, filepath.Join(f.root, "registry.json"))
				reloaded := false
				var failed seam.Cmd
				f.answer = func(cmd seam.Cmd, index int) (seam.Result, error) {
					if index == point {
						failed = cmd
						if runnerError {
							return seam.Result{}, errors.New("boom")
						}
						output := "failure\n"
						if point == 5 {
							output = "Failed to stop sandbox-wip-nginx.service: Transport endpoint is not connected\n"
						}
						return seam.Result{ExitCode: 1, Output: []byte(output)}, nil
					}
					if cmd.Args[1] == "daemon-reload" {
						reloaded = true
					}
					state := "inactive"
					if point == 14 && reloaded && cmd.Args[1] == "show" && cmd.Args[4] == teardownNames[2] {
						state = "failed"
					}
					return seam.Result{Stdout: []byte(state + "\n")}, nil
				}
				code := f.run("down", "wip")
				action := failed.Args[1]
				if action == "daemon-reload" {
					action = "systemctl --user daemon-reload"
				} else {
					action += " " + failed.Args[len(failed.Args)-1]
				}
				want := "sandbox: " + action + ": boom\n"
				if !runnerError {
					output := "failure"
					if point == 5 {
						output = "Failed to stop sandbox-wip-nginx.service: Transport endpoint is not connected"
					}
					want = "sandbox: " + action + ": exit status 1\n\n> " + output + "\n"
				}
				f.expect(code, 1, want)
				if len(f.calls) != point+1 {
					t.Fatalf("continued after failure: %d", len(f.calls))
				}
				for _, name := range generatedNames {
					assertPresent(t, filepath.Join(f.data, name))
				}
				if point <= 9 {
					for _, u := range teardownNames {
						assertPresent(t, filepath.Join(f.units, u))
					}
				}
				if !reflect.DeepEqual(reg, snapshotTeardown(t, filepath.Join(f.root, "registry.json"))) {
					t.Fatal("registry changed")
				}
				f.answer = nil
				f.expect(f.run("down", "wip"), 0, "")
				f.gone()
				found := false
				for _, c := range f.calls {
					if reflect.DeepEqual(c, reloadCmd()) {
						found = true
					}
				}
				if !found {
					t.Fatal("retry omitted reload")
				}
			})
		}
	}
}

// R-Y85L-0WJK R-P4D0-6XZ3 R-TWRP-I1T0 R-Y9DH-EOA9 R-DSHQ-F40G R-OS60-D8K5 R-OX1L-WBIX
func TestDownRemovalFailuresAndRetry(t *testing.T) {
	requireUnprivileged(t)
	for _, kind := range []string{"unit-entry", "unit-directory", "generated"} {
		t.Run(kind, func(t *testing.T) {
			f := newTeardownFixture(t, true)
			kept := snapshotTeardown(t, filepath.Join(f.data, "apps"))
			token := snapshotTeardown(t, filepath.Join(f.data, "token"))
			reg := snapshotTeardown(t, filepath.Join(f.root, "registry.json"))
			blocked := f.units
			failed := filepath.Join(f.units, teardownNames[0])
			wantCalls := 10
			switch kind {
			case "unit-entry":
				wantCalls = 9
				p := filepath.Join(f.units, teardownNames[1])
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				blocked = filepath.Join(p, "locked")
				f.write(filepath.Join(blocked, "file"), []byte("locked"))
				failed = p
			case "generated":
				blocked = filepath.Join(f.data, "bin")
				failed = blocked
				wantCalls = 16
			}
			chmodTeardown(t, blocked, 0500)
			t.Cleanup(func() {
				if _, err := os.Stat(blocked); err == nil {
					chmodTeardown(t, blocked, 0700)
				}
			})
			f.expect(f.run("down", "wip"), 1, "sandbox: "+failed+": permission denied\n")
			if len(f.calls) != wantCalls {
				t.Fatalf("calls %d", len(f.calls))
			}
			if kind == "unit-entry" {
				assertAbsent(t, filepath.Join(f.units, teardownNames[0]))
				for _, u := range teardownNames[2:] {
					assertPresent(t, filepath.Join(f.units, u))
				}
			}
			remaining := generatedNames
			if kind == "generated" {
				remaining = generatedNames[1:]
			}
			for _, entry := range remaining {
				assertPresent(t, filepath.Join(f.data, entry))
			}
			if !reflect.DeepEqual(kept, snapshotTeardown(t, filepath.Join(f.data, "apps"))) || !reflect.DeepEqual(token, snapshotTeardown(t, filepath.Join(f.data, "token"))) || !reflect.DeepEqual(reg, snapshotTeardown(t, filepath.Join(f.root, "registry.json"))) {
				t.Fatal("kept entries changed")
			}
			chmodTeardown(t, blocked, 0700)
			f.expect(f.run("down", "wip"), 0, "")
			f.gone()
		})
	}
}

// R-Y85L-0WJK
func TestDownRemovesWholeUnitEntries(t *testing.T) {
	f := newTeardownFixture(t, false)
	f.write(filepath.Join(f.units, teardownNames[4], "child"), []byte("file"))
	if err := os.Symlink(filepath.Join(f.base, "absent"), filepath.Join(f.units, teardownNames[0])); err != nil {
		t.Fatal(err)
	}
	f.expect(f.run("down", "wip"), 0, "")
	f.gone()
}

// R-IR6A-V8NR R-P98L-Q0XV R-PAGI-3SOK R-DXDB-Y6Z8 R-PCWA-VC5Y
func TestWipeStateGuard(t *testing.T) {
	for _, answer := range []string{"active", "reloading", "refreshing", "exit", "runner"} {
		for _, named := range []bool{false, true} {
			t.Run(answer+map[bool]string{true: "named", false: "checkout"}[named], func(t *testing.T) {
				f := newTeardownFixture(t, true)
				beforeRoot := snapshotTeardown(t, f.data)
				beforeUnits := snapshotTeardown(t, f.units)
				reg := snapshotTeardown(t, filepath.Join(f.root, "registry.json"))
				f.answer = func(_ seam.Cmd, _ int) (seam.Result, error) {
					if answer == "runner" {
						return seam.Result{}, errors.New("boom")
					}
					if answer == "exit" {
						return seam.Result{ExitCode: 1, Output: []byte("failure\n")}, nil
					}
					return seam.Result{Stdout: []byte(answer + "\n")}, nil
				}
				args := []string{"wipe", "wip"}
				if !named {
					f.deps.Dir = f.worktree
					args = []string{"wipe"}
				}
				code := f.run(args...)
				wantCode := 2
				want := "sandbox: sandbox 'wip' is up\n\nrun 'sandbox down wip' first\n"
				if answer == "exit" {
					wantCode = 1
					want = "sandbox: systemctl --user: exit status 1\n\n> failure\n"
				}
				if answer == "runner" {
					wantCode = 1
					want = "sandbox: systemctl --user: boom\n"
				}
				f.expect(code, wantCode, want)
				if !reflect.DeepEqual(f.calls, []seam.Cmd{stateCmd(teardownNames[0])}) {
					t.Fatalf("calls %#v", f.calls)
				}
				if !reflect.DeepEqual(beforeRoot, snapshotTeardown(t, f.data)) || !reflect.DeepEqual(beforeUnits, snapshotTeardown(t, f.units)) || !reflect.DeepEqual(reg, snapshotTeardown(t, filepath.Join(f.root, "registry.json"))) {
					t.Fatal("wipe refusal changed files")
				}
			})
		}
	}
}

// R-SGZQ-J9GL R-PHRW-EF4Q R-PIZS-S6VF R-PK7P-5YM4 R-PMNH-XI3I R-PNVE-B9U7 R-DYL8-BYPX
func TestWipeTeardownAndPreservation(t *testing.T) {
	for _, kind := range []string{"present", "absent", "checkout", "gone", "outside"} {
		t.Run(kind, func(t *testing.T) {
			present := kind != "absent"
			f := newTeardownFixture(t, present)
			if kind != "gone" {
				f.write(filepath.Join(f.worktree, "source"), []byte("untouched"))
			}
			f.write(filepath.Join(f.root, "other/state/file"), []byte("other data"))
			f.write(filepath.Join(f.root, "other.lock"), []byte("other lock"))
			other := snapshotTeardown(t, filepath.Join(f.root, "other"))
			lock := snapshotTeardown(t, filepath.Join(f.root, "other.lock"))
			var tree map[string]teardownSnapshot
			if kind != "gone" {
				tree = snapshotTeardown(t, f.worktree)
			}
			var apps, token map[string]teardownSnapshot
			if present {
				apps = snapshotTeardown(t, filepath.Join(f.data, "apps"))
				token = snapshotTeardown(t, filepath.Join(f.data, "token"))
			}
			f.answer = func(_ seam.Cmd, _ int) (seam.Result, error) {
				b, err := os.ReadFile(filepath.Join(f.root, "registry.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(b, f.registryBytes()) {
					t.Fatal("registry changed during program runs")
				}
				if present && (!reflect.DeepEqual(apps, snapshotTeardown(t, filepath.Join(f.data, "apps"))) || !reflect.DeepEqual(token, snapshotTeardown(t, filepath.Join(f.data, "token")))) {
					t.Fatal("kept data changed during program runs")
				}
				return seam.Result{Stdout: []byte("inactive\n")}, nil
			}
			args := []string{"wipe", "wip"}
			if kind == "checkout" {
				f.deps.Dir = f.worktree
				args = []string{"wipe"}
			}
			f.expect(f.run(args...), 0, "")
			want := append([]seam.Cmd{stateCmd(teardownNames[0])}, teardownSequence(present)...)
			if !reflect.DeepEqual(f.calls, want) {
				t.Fatalf("calls %#v", f.calls)
			}
			assertAbsent(t, f.data)
			var reg struct {
				Sandboxes []struct {
					Name     string `json:"name"`
					Port     int    `json:"port"`
					Worktree string `json:"worktree"`
					Apps     []any  `json:"apps"`
				} `json:"sandboxes"`
			}
			b, err := os.ReadFile(filepath.Join(f.root, "registry.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(b, &reg); err != nil {
				t.Fatal(err)
			}
			if len(reg.Sandboxes) != 1 || reg.Sandboxes[0].Name != "other" || reg.Sandboxes[0].Port != 7401 || reg.Sandboxes[0].Worktree != filepath.Join(f.base, "other") || len(reg.Sandboxes[0].Apps) != 0 {
				t.Fatalf("registry %s", b)
			}
			if !reflect.DeepEqual(other, snapshotTeardown(t, filepath.Join(f.root, "other"))) || !reflect.DeepEqual(lock, snapshotTeardown(t, filepath.Join(f.root, "other.lock"))) {
				t.Fatal("other sandbox changed")
			}
			if kind != "gone" && !reflect.DeepEqual(tree, snapshotTeardown(t, f.worktree)) {
				t.Fatal("worktree changed")
			}
		})
	}
}

// R-PGK0-0NE1
func TestWipeTeardownFailureMatchesDown(t *testing.T) {
	for _, point := range []int{0, 5, 10, 11} {
		t.Run(strconv.Itoa(point), func(t *testing.T) {
			f := newTeardownFixture(t, true)
			apps := snapshotTeardown(t, filepath.Join(f.data, "apps"))
			token := snapshotTeardown(t, filepath.Join(f.data, "token"))
			reg := snapshotTeardown(t, filepath.Join(f.root, "registry.json"))
			fail := func(index int) seam.Result {
				if index == point {
					return seam.Result{ExitCode: 1, Output: []byte("failure\n")}
				}
				return seam.Result{Stdout: []byte("inactive\n")}
			}
			f.answer = func(_ seam.Cmd, index int) (seam.Result, error) { return fail(index), nil }
			down := f.run("down", "wip")
			diagnostic := f.err.String()
			for _, u := range teardownNames {
				f.write(filepath.Join(f.units, u), []byte("unit"))
			}
			f.answer = func(_ seam.Cmd, index int) (seam.Result, error) {
				if index == 0 {
					return seam.Result{Stdout: []byte("inactive\n")}, nil
				}
				return fail(index - 1), nil
			}
			f.expect(f.run("wipe", "wip"), down, diagnostic)
			if !reflect.DeepEqual(apps, snapshotTeardown(t, filepath.Join(f.data, "apps"))) || !reflect.DeepEqual(token, snapshotTeardown(t, filepath.Join(f.data, "token"))) || !reflect.DeepEqual(reg, snapshotTeardown(t, filepath.Join(f.root, "registry.json"))) {
				t.Fatal("failed teardown changed kept entries")
			}
		})
	}
}

// R-PLFL-JQCT R-50OX-YW52 R-Y6XO-N4SV
func TestWipeRemovalAndRegistryFailuresRetry(t *testing.T) {
	requireUnprivileged(t)
	for _, kind := range []string{"data", "registry", "never-created"} {
		t.Run(kind, func(t *testing.T) {
			f := newTeardownFixture(t, kind != "never-created")
			registryPath := filepath.Join(f.root, "registry.json")
			registryRoot, err := os.OpenRoot(f.root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := registryRoot.Close(); err != nil {
					t.Fatal(err)
				}
			})
			original := f.registryBytes()
			if kind == "data" {
				p := filepath.Join(f.data, "apps/auth/state")
				chmodTeardown(t, p, 0500)
				t.Cleanup(func() {
					if _, err := os.Stat(p); err == nil {
						chmodTeardown(t, p, 0700)
					}
				})
				f.expect(f.run("wipe", "wip"), 1, "sandbox: "+f.data+": permission denied\n")
				b, err := registryRoot.ReadFile("registry.json")
				if err != nil || !bytes.Equal(b, original) {
					t.Fatalf("registry changed %s %v", b, err)
				}
				chmodTeardown(t, p, 0700)
			}
			if kind == "registry" {
				f.answer = func(_ seam.Cmd, index int) (seam.Result, error) {
					if index == 16 {
						if err := os.Remove(registryPath); err != nil {
							t.Fatal(err)
						}
						f.write(filepath.Join(registryPath, "child"), []byte("blocked"))
					}
					return seam.Result{Stdout: []byte("inactive\n")}, nil
				}
				code := f.run("wipe", "wip")
				if code != 1 || f.out.Len() != 0 || !strings.HasPrefix(f.err.String(), "sandbox: "+registryPath+": ") {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, f.out.String(), f.err.String())
				}
				assertAbsent(t, f.data)
				if err := os.RemoveAll(registryPath); err != nil {
					t.Fatal(err)
				}
				f.write(registryPath, original)
				f.answer = nil
			}
			f.expect(f.run("wipe", "wip"), 0, "")
			assertAbsent(t, f.data)
			b, err := registryRoot.ReadFile("registry.json")
			if err != nil {
				t.Fatal(err)
			}
			var registry map[string][]map[string]any
			if err := json.Unmarshal(b, &registry); err != nil {
				t.Fatal(err)
			}
			entries := registry["sandboxes"]
			if len(entries) != 1 || entries[0]["name"] != "other" || entries[0]["port"] != float64(7401) || entries[0]["worktree"] != filepath.Join(f.base, "other") || len(entries[0]["apps"].([]any)) != 0 {
				t.Fatalf("registry after retry: %s", b)
			}
		})
	}
}
