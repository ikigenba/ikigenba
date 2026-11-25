package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/linux"
)

// R-KKV0-8QID
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
	mcpLink := filepath.Join(dir, linux.MCPName)
	if err := os.Symlink(bin, mcpLink); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "unused.sock")
	ping := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"" + boxmcp.MethodPing + "\"}\n"
	var wantPing bytes.Buffer
	if err := boxmcp.Run(t.Context(), strings.NewReader(ping), &wantPing, func(name string) string {
		if name == boxmcp.EnvSocket {
			return socket
		}
		return ""
	}); err != nil || wantPing.Len() == 0 {
		t.Fatalf("in-process ping: %v, stdout %q", err, &wantPing)
	}
	for _, tc := range []struct {
		name   string
		path   string
		args   []string
		code   int
		diag   string
		env    []string
		stdin  io.Reader
		stdout string
	}{
		{name: "argument", path: bin, args: []string{"extra"}, code: daemon.ExitUsage, diag: daemon.DiagArguments},
		{name: "missing URL", path: bin, code: daemon.ExitUsage, diag: daemon.DiagURL},
		{name: "session helper", path: link, args: []string{"ignored"}, code: daemon.ExitSuccess},
		{name: "stdio server", path: mcpLink, args: []string{"ignored"}, code: daemon.ExitSuccess,
			env: []string{boxmcp.EnvSocket + "=" + socket}, stdin: strings.NewReader(ping), stdout: wantPing.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", `exec "$0" "$@"`)
			cmd.Args = append(cmd.Args, tc.path)
			cmd.Args = append(cmd.Args, tc.args...)
			cmd.Env = append([]string{}, tc.env...)
			cmd.Dir = t.TempDir()
			cmd.Stdin = tc.stdin
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
			if code != tc.code || stdout.String() != tc.stdout || stderr.String() != want {
				t.Errorf("exit %d, stdout %q, stderr %q, want exit %d and stderr %q", code, &stdout, &stderr, tc.code, want)
			}
		})
	}
}
