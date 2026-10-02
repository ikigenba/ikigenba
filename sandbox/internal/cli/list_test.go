package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-ISE7-90EG R-PRJ3-GL2A R-PSQZ-UCSZ R-PTYW-84JO
func TestListSortedTable(t *testing.T) {
	f := newTeardownFixture(t, false)
	if err := os.MkdirAll(f.worktree, 0700); err != nil {
		t.Fatal(err)
	}
	f.answer = func(c seam.Cmd, _ int) (seam.Result, error) {
		state := "inactive"
		if c.Args[4] == "sandbox-wip-nginx.service" {
			state = "active"
		}
		return seam.Result{Stdout: []byte(state + "\n")}, nil
	}
	code := f.run("ls")
	want := "NAME   PORT  STATE  WORKTREE\nother  7401  down   " + filepath.Join(f.base, "other") + " (gone)\nwip    7400  up     " + f.worktree + "\n"
	if code != 0 || f.out.String() != want || f.err.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, f.out.String(), f.err.String())
	}
	if !reflect.DeepEqual(f.calls, []seam.Cmd{stateCmd("sandbox-other-nginx.service"), stateCmd("sandbox-wip-nginx.service")}) {
		t.Fatalf("calls %#v", f.calls)
	}
	assertAbsent(t, filepath.Join(f.root, "registry.json.lock"))
	assertAbsent(t, filepath.Join(f.root, "wip.lock"))
}

// R-PQB7-2TBL
func TestListEmpty(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "empty"}[exists], func(t *testing.T) {
			f := newTeardownFixture(t, false)
			p := filepath.Join(f.root, "registry.json")
			if exists {
				f.write(p, []byte(`{"sandboxes":[]}`))
			} else if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			f.expect(f.run("ls"), 0, "")
			if len(f.calls) != 0 {
				t.Fatalf("calls %#v", f.calls)
			}
		})
	}
}

// R-E28X-H9Y0
func TestListWorktreeMarkers(t *testing.T) {
	requireUnprivileged(t)
	for _, kind := range []string{"existing", "removed", "dangling", "below-file", "denied"} {
		t.Run(kind, func(t *testing.T) {
			f := newTeardownFixture(t, false)
			tree := filepath.Join(f.base, "tree")
			switch kind {
			case "existing":
				if err := os.Mkdir(tree, 0700); err != nil {
					t.Fatal(err)
				}
			case "dangling":
				if err := os.Symlink(filepath.Join(f.base, "gone"), tree); err != nil {
					t.Fatal(err)
				}
			case "below-file":
				f.write(tree, []byte("file"))
				tree = filepath.Join(tree, "child")
			case "denied":
				if err := os.Mkdir(tree, 0700); err != nil {
					t.Fatal(err)
				}
				denied := tree
				chmodTeardown(t, denied, 0000)
				t.Cleanup(func() { chmodTeardown(t, denied, 0700) })
				tree = filepath.Join(tree, "child")
			}
			b, err := json.Marshal(map[string]any{"sandboxes": []any{map[string]any{"name": "wip", "port": 7400, "worktree": tree, "apps": []any{}}}})
			if err != nil {
				t.Fatal(err)
			}
			f.write(filepath.Join(f.root, "registry.json"), b)
			code := f.run("ls")
			if code != 0 || f.err.Len() != 0 {
				t.Fatalf("code=%d stderr=%q", code, f.err.String())
			}
			suffix := tree
			if kind == "removed" || kind == "dangling" || kind == "below-file" {
				suffix += " (gone)"
			}
			want := "NAME  PORT  STATE  WORKTREE\nwip   7400  down   " + suffix + "\n"
			if f.out.String() != want {
				t.Fatalf("stdout=%q want=%q", f.out.String(), want)
			}
		})
	}
}

// R-ISE7-90EG R-E3GT-V1OP R-DW5F-KF8J
func TestListFailures(t *testing.T) {
	for _, runnerError := range []bool{false, true} {
		t.Run(map[bool]string{true: "runner", false: "exit"}[runnerError], func(t *testing.T) {
			f := newTeardownFixture(t, false)
			original, err := os.ReadFile(filepath.Join(f.root, "registry.json"))
			if err != nil {
				t.Fatal(err)
			}
			f.answer = func(c seam.Cmd, _ int) (seam.Result, error) {
				unit := c.Args[4]
				if runnerError {
					return seam.Result{}, errors.New(unit)
				}
				return seam.Result{ExitCode: 1, Output: []byte(unit + "\n")}, nil
			}
			want := "sandbox: systemctl --user: exit status 1\n\n> sandbox-other-nginx.service\n"
			if runnerError {
				want = "sandbox: systemctl --user: sandbox-other-nginx.service\n"
			}
			f.expect(f.run("ls"), 1, want)
			if !reflect.DeepEqual(f.calls, []seam.Cmd{stateCmd("sandbox-other-nginx.service")}) {
				t.Fatalf("calls %#v", f.calls)
			}
			after, err := os.ReadFile(filepath.Join(f.root, "registry.json"))
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("registry changed")
			}
			if strings.Contains(f.err.String(), "wip") {
				t.Fatal("reported wrong unit")
			}
		})
	}
}
