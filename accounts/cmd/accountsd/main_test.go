package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/linux"
)

// R-XWM7-5NUE
func TestBinaryDispatch(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "accountsd")
	build := exec.CommandContext(t.Context(), "go", "build", "-o")
	build.Args = append(build.Args, bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	link := filepath.Join(dir, linux.SessionName)
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		args []string
		code int
		diag string
	}{
		{name: "argument", path: bin, args: []string{"extra"}, code: daemon.ExitUsage, diag: daemon.DiagArguments},
		{name: "missing URL", path: bin, code: daemon.ExitUsage, diag: daemon.DiagURL},
		{name: "session helper", path: link, args: []string{"ignored"}, code: daemon.ExitSuccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", `exec "$0" "$@"`)
			cmd.Args = append(cmd.Args, tc.path)
			cmd.Args = append(cmd.Args, tc.args...)
			cmd.Env = []string{}
			cmd.Dir = t.TempDir()
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			code := 0
			if err := cmd.Run(); err != nil {
				var status *exec.ExitError
				if !errors.As(err, &status) {
					t.Fatal(err)
				}
				code = status.ExitCode()
			}
			want := ""
			if tc.diag != "" {
				want = "accountsd: " + tc.diag + "\n"
			}
			if code != tc.code || stdout.Len() != 0 || stderr.String() != want {
				t.Errorf("exit %d, stdout %q, stderr %q, want exit %d and stderr %q", code, &stdout, &stderr, tc.code, want)
			}
		})
	}
}
