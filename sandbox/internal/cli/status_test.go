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
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/cli"
	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

type reportFixture struct {
	t                                 *testing.T
	worktree, state, root, data, name string
	deps                              seam.Deps
	execs, streams                    []seam.Cmd
	out, err                          bytes.Buffer
}

func newReportFixture(t *testing.T, name string, apps []string, port int) *reportFixture {
	t.Helper()
	base := t.TempDir()
	f := &reportFixture{t: t, name: name, worktree: filepath.Join(base, name), state: filepath.Join(base, "state")}
	f.root = filepath.Join(f.state, "ikigenba", "sandbox")
	f.data = filepath.Join(f.root, name)
	if err := os.MkdirAll(f.root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.worktree, 0700); err != nil {
		t.Fatal(err)
	}
	recorded := make([]map[string]any, 0, len(apps))
	for _, app := range apps {
		recorded = append(recorded, map[string]any{"name": app, "default": false})
	}
	content, err := json.Marshal(map[string]any{"sandboxes": []any{map[string]any{"name": name, "port": port, "worktree": f.worktree, "apps": recorded}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.root, "registry.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	f.deps = seam.Deps{Dir: f.worktree, EUID: 1000, Getenv: func(key string) string {
		if key == "HOME" {
			return base
		}
		if key == "XDG_STATE_HOME" {
			return f.state
		}
		return ""
	}}
	f.deps.Exec = func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		f.execs = append(f.execs, cmd)
		if cmd.Path == "git" {
			return seam.Result{Stdout: []byte(f.worktree + "\n")}, nil
		}
		return seam.Result{Stdout: []byte("active\n")}, nil
	}
	f.deps.Stream = func(_ context.Context, cmd seam.Cmd, _ io.Writer) (seam.Result, error) {
		f.streams = append(f.streams, cmd)
		return seam.Result{}, nil
	}
	return f
}
func (f *reportFixture) run(ctx context.Context, args []string, in io.Reader) int {
	f.t.Helper()
	f.out.Reset()
	f.err.Reset()
	f.execs = nil
	f.streams = nil
	return runReportChecked(ctx, f.t, args, in, &f.out, &f.err, f.deps)
}
func (f *reportFixture) expect(code, want int, out, err string) {
	f.t.Helper()
	if code != want || f.out.String() != out || f.err.String() != err {
		f.t.Fatalf("got code=%d stdout=%q stderr=%q; want %d %q %q", code, f.out.String(), f.err.String(), want, out, err)
	}
}
func (f *reportFixture) writeToken(value string, mode os.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(f.data, 0700); err != nil {
		f.t.Fatal(err)
	}
	path := filepath.Join(f.data, "token")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		f.t.Fatal(err)
	}
}
func stateCmd(unit string) seam.Cmd {
	return seam.Cmd{Path: "systemctl", Dir: "/", Args: []string{"--user", "show", "--property=ActiveState", "--value", unit}}
}

// R-LN0K-NZH7 R-XYED-YQM0 R-RULZ-2TLJ R-XZMA-CICP
func TestStatusRecordedApps(t *testing.T) {
	for _, apps := range [][]string{{"dummy", "auth"}, {}, {"a-long-app"}} {
		t.Run(strings.Join(apps, "-"), func(t *testing.T) {
			f := newReportFixture(t, "wip", apps, 7400)
			code := f.run(context.Background(), []string{"status"}, nil)
			want := "UNIT   STATE\nnginx  active\n"
			units := []seam.Cmd{{Path: "git", Dir: f.worktree, Args: []string{"rev-parse", "--show-toplevel"}}, stateCmd("sandbox-wip-nginx.service")}
			if len(apps) == 2 {
				want += "auth   active\ndummy  active\n"
				units = append(units, stateCmd("sandbox-wip-auth.service"), stateCmd("sandbox-wip-dummy.service"))
			}
			if len(apps) == 1 {
				want = "UNIT        STATE\nnginx       active\na-long-app  active\n"
				units = append(units, stateCmd("sandbox-wip-a-long-app.service"))
			}
			f.expect(code, 0, want, "")
			if !reflect.DeepEqual(f.execs, units) || len(f.streams) != 0 {
				t.Fatalf("calls: %#v %#v", f.execs, f.streams)
			}
		})
	}
}

// R-XUQO-TFDX R-XZMA-CICP
func TestStatusStatesVerbatim(t *testing.T) {
	f := newReportFixture(t, "wip", []string{"a", "b", "c", "d", "dummy"}, 7400)
	states := []string{"active", "inactive", "activating", "deactivating", "maintenance", "failed"}
	git := f.deps.Exec
	n := 0
	f.deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "git" {
			return git(ctx, cmd)
		}
		state := states[n]
		n++
		return seam.Result{Stdout: []byte(state + "\n")}, nil
	}
	f.expect(f.run(context.Background(), []string{"status"}, nil), 0, "UNIT   STATE\nnginx  active\na      inactive\nb      activating\nc      deactivating\nd      maintenance\ndummy  failed\n", "")
	n = 0
	for idx := range states {
		states[idx] = "inactive"
	}
	f.expect(f.run(context.Background(), []string{"status"}, nil), 0, "UNIT   STATE\nnginx  inactive\na      inactive\nb      inactive\nc      inactive\nd      inactive\ndummy  inactive\n", "")
}

// R-XVYL-774M R-XX6H-KYVB R-Y0U6-QA3E
func TestStatusFailures(t *testing.T) {
	cases := []struct {
		unit   string
		result seam.Result
		err    error
		want   string
	}{
		{"auth", seam.Result{}, errors.New("boom"), "sandbox: systemctl --user: boom\n"},
		{"auth", seam.Result{ExitCode: 1}, nil, "sandbox: systemctl --user: exit status 1\n"},
		{"nginx", seam.Result{ExitCode: 1, Output: []byte("Failed to connect to bus: No medium found\n")}, nil, "sandbox: systemctl --user: exit status 1\n\n> Failed to connect to bus: No medium found\n"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			f := newReportFixture(t, "wip", []string{"dummy", "auth"}, 7400)
			git := f.deps.Exec
			f.deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
				if cmd.Path == "git" {
					return git(ctx, cmd)
				}
				unit := cmd.Args[len(cmd.Args)-1]
				if unit == "sandbox-wip-"+tc.unit+".service" {
					return tc.result, tc.err
				}
				if unit == "sandbox-wip-dummy.service" {
					if tc.err != nil {
						return seam.Result{ExitCode: 1}, nil
					}
					return seam.Result{}, errors.New("later")
				}
				return seam.Result{Stdout: []byte("active\n")}, nil
			}
			f.expect(f.run(context.Background(), []string{"status"}, nil), 1, "", tc.want)
		})
	}
}

func runReportChecked(ctx context.Context, t testing.TB, args []string, in io.Reader, out, err io.Writer, deps seam.Deps) int {
	t.Helper()
	code := cli.Run(ctx, args, in, out, err, deps)
	if code < 0 || code > 3 {
		t.Fatalf("exit code out of domain: %d", code)
	}
	return code
}
