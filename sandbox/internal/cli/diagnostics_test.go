package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-Y39Z-HTKS R-11I9-YSC0 R-0CKX-GER5
func TestExternalDiagnostics(t *testing.T) {
	for _, c := range []struct{ output, want string }{{"Failed to connect to bus: No medium found\n", "\n> Failed to connect to bus: No medium found\n"}, {"", ""}, {"a\n\nb", "\n> a\n> \n> b\n"}, {"a\nb\n", "\n> a\n> b\n"}, {"\n", "\n> \n"}} {
		t.Run(c.output, func(t *testing.T) {
			state := t.TempDir()
			root := filepath.Join(state, "ikigenba", "sandbox")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte(`{"sandboxes":[{"name":"wip","port":7400,"worktree":"`+state+`","apps":[]}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			deps := seam.Deps{Dir: state, EUID: 1000, Getenv: func(key string) string {
				if key == "XDG_STATE_HOME" {
					return state
				}
				return ""
			}, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
				if cmd.Path != "systemctl" {
					t.Fatalf("unexpected %v", cmd)
				}
				return seam.Result{ExitCode: 1, Output: []byte(c.output)}, nil
			}}
			var out, err bytes.Buffer
			code := runChecked(context.Background(), t, []string{"ls"}, forbiddenReader{t}, &out, &err, deps)
			want := "sandbox: systemctl --user: exit status 1\n" + c.want
			if code != 1 || out.Len() != 0 || err.String() != want {
				t.Fatalf("%d %q %q want %q", code, out.String(), err.String(), want)
			}
		})
	}
}

// R-YJ4O-GU7T
func TestRunnerDiagnostics(t *testing.T) {
	for _, message := range []string{"boom", "chdir /tmp/a\nb: no such file or directory"} {
		deps := seam.Deps{Dir: t.TempDir(), EUID: 1000, Exec: func(context.Context, seam.Cmd) (seam.Result, error) { return seam.Result{}, errors.New(message) }, Getenv: func(string) string { t.Fatal("getenv after failed git"); return "" }}
		var out, err bytes.Buffer
		code := runChecked(context.Background(), t, []string{"url"}, forbiddenReader{t}, &out, &err, deps)
		want := "sandbox: git rev-parse --show-toplevel: " + expectedPrintedName(message) + "\n"
		if code != 1 || out.Len() != 0 || err.String() != want {
			t.Fatalf("%d %q %q", code, out.String(), err.String())
		}
	}
}

// R-AE6H-QEEG
func TestFileDiagnostics(t *testing.T) {
	state := t.TempDir()
	registry := filepath.Join(state, "ikigenba", "sandbox", "registry.json")
	if err := os.MkdirAll(registry, 0700); err != nil {
		t.Fatal(err)
	}
	deps := seam.Deps{EUID: 1000, Getenv: func(key string) string {
		if key == "XDG_STATE_HOME" {
			return state
		}
		return ""
	}, Exec: func(context.Context, seam.Cmd) (seam.Result, error) { t.Fatal("exec"); return seam.Result{}, nil }}
	var out, err bytes.Buffer
	code := runChecked(context.Background(), t, []string{"ls"}, forbiddenReader{t}, &out, &err, deps)
	want := "sandbox: " + registry + ": is a directory\n"
	if code != 1 || out.Len() != 0 || err.String() != want {
		t.Fatalf("%d %q %q want %q", code, out.String(), err.String(), want)
	}
}

// R-YBTA-67RN
func TestTypedNamesDiagnostics(t *testing.T) {
	for _, name := range []string{"a\nb", "café"} {
		deps := seam.Deps{EUID: 1000, Dir: t.TempDir(), Getenv: func(string) string { t.Fatal("getenv for invalid name"); return "" }, Exec: func(context.Context, seam.Cmd) (seam.Result, error) { t.Fatal("exec"); return seam.Result{}, nil }}
		var out, err bytes.Buffer
		code := runChecked(context.Background(), t, []string{"down", name}, forbiddenReader{t}, &out, &err, deps)
		want := "sandbox: no sandbox '" + expectedPrintedName(name) + "'\n\nrun 'sandbox ls' to see every sandbox\n"
		if code != 2 || out.Len() != 0 || err.String() != want {
			t.Fatalf("%d %q %q", code, out.String(), err.String())
		}
	}
}
