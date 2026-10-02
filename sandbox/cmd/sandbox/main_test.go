package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sandbox/internal/cli"
	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "sandbox-child" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		main()
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "sandbox-deleted-cwd" {
		dir := os.Args[2]
		if err := os.Chdir(dir); err != nil {
			panic(err)
		}
		root, err := os.OpenRoot(filepath.Dir(dir))
		if err != nil {
			panic(err)
		}
		if err := root.Remove(filepath.Base(dir)); err != nil {
			panic(err)
		}
		if err := root.Close(); err != nil {
			panic(err)
		}
		os.Args = append(os.Args[:1], os.Args[3:]...)
		main()
		return
	}
	os.Exit(m.Run())
}

func child(t *testing.T, dir string, env, args []string) *exec.Cmd {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("main contract tests must run as a non-root user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "/proc/self/exe", "sandbox-child")
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir, cmd.Env = dir, env
	return cmd
}

func expectChild(t *testing.T, cmd *exec.Cmd, code int, out, diagnostic string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	assertExit(t, cmd, err, code)
	if stdout.String() != out || stderr.String() != diagnostic {
		t.Fatalf("stdout=%q stderr=%q; want stdout=%q stderr=%q", stdout.String(), stderr.String(), out, diagnostic)
	}
}

func assertExit(t *testing.T, cmd *exec.Cmd, err error, code int) {
	t.Helper()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("child failed to run: %v", err)
		}
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != code {
		t.Fatalf("child exit=%v error=%v; want %d", cmd.ProcessState, err, code)
	}
}

func script(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := root.Chmod(name, 0700); err != nil {
		t.Fatal(err)
	}
}

// R-0PBA-52X2
func TestMainHelp(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := cli.Run(context.Background(), []string{"--help"}, strings.NewReader(""), &out, &diagnostic, seam.Deps{EUID: os.Geteuid()})
	if code < 0 || code > 3 || code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("cli help returned %d, %q", code, diagnostic.String())
	}
	dir := t.TempDir()
	expectChild(t, child(t, dir, []string{"HOME=" + dir, "PATH=" + dir}, []string{"--help"}), 0, out.String(), "")
}

// R-0QJ6-IUNR
func TestMainVersion(t *testing.T) {
	dir := t.TempDir()
	expectChild(t, child(t, dir, []string{"HOME=" + dir, "PATH=" + dir}, []string{"version"}), 0, cli.Version+"\n", "")
}

// R-0RR2-WMEG
func TestMainNotCheckout(t *testing.T) {
	dir, bin := t.TempDir(), t.TempDir()
	script(t, bin, "git", "exit 128\n")
	expectChild(t, child(t, dir, []string{"HOME=" + dir, "PATH=" + bin}, []string{"url"}), 2, "", fmt.Sprintf("sandbox: '%s' is not inside a git checkout\n", dir))
}

// R-0SYZ-AE55
func TestMainEmptyList(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	expectChild(t, child(t, dir, []string{"HOME=" + home, "PATH=" + dir}, []string{"ls"}), 0, "", "")
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("HOME entries=%v error=%v", entries, err)
	}
}

// R-56BL-8JQZ
func TestMainDeletedDirectory(t *testing.T) {
	var help, diagnostic bytes.Buffer
	code := cli.Run(context.Background(), []string{"--help"}, strings.NewReader(""), &help, &diagnostic, seam.Deps{EUID: os.Geteuid()})
	if code < 0 || code > 3 || code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("cli help returned %d, %q", code, diagnostic.String())
	}
	for _, tc := range []struct {
		arg, out, diagnostic string
		code                 int
	}{{"--help", help.String(), "", 0}, {"url", "", "sandbox: the current directory no longer exists\n", 2}} {
		t.Run(tc.arg, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "removed")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			cmd := child(t, root, []string{"HOME=" + root, "PATH=" + root}, nil)
			cmd.Args = []string{cmd.Path, "sandbox-deleted-cwd", dir, tc.arg}
			expectChild(t, cmd, tc.code, tc.out, tc.diagnostic)
		})
	}
}

// R-OORJ-9ODD R-OPZF-NG42
func TestMainCancelsGitGroup(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			dir, bin := t.TempDir(), t.TempDir()
			fifo := filepath.Join(dir, "ready")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			// RDWR opens the FIFO without waiting for its writer; the read still
			// waits for the child's explicit readiness notification.
			fifoRoot, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fifoRoot.Close() })
			ready, err := fifoRoot.OpenFile("ready", os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ready.Close() })
			script(t, bin, "git", "sleep 600 &\nprintf '%s\\n' \"$$\" > \"$READY_FIFO\"\nsleep 600\n")
			if err := os.Symlink("/bin/sleep", filepath.Join(bin, "sleep")); err != nil {
				t.Fatal(err)
			}
			cmd := child(t, dir, []string{"HOME=" + dir, "PATH=" + bin, "READY_FIFO=" + fifo}, []string{"url"})
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			line := make(chan string, 1)
			go func() { text, _ := bufio.NewReader(ready).ReadString('\n'); line <- text }()
			var group int
			select {
			case text := <-line:
				group, err = strconv.Atoi(strings.TrimSpace(text))
				if err != nil || group <= 0 {
					t.Fatalf("invalid group notification %q: %v", text, err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("git did not report readiness before deadline")
			}
			t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			err = <-wait
			assertExit(t, cmd, err, 1)
			assertNoLiveGroup(t, group)
		})
	}
}

func assertNoLiveGroup(t *testing.T, group int) {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(data)[strings.LastIndexByte(string(data), ')')+1:])
		if len(fields) >= 3 && fields[2] == strconv.Itoa(group) && fields[0] != "Z" && fields[0] != "X" {
			t.Fatalf("process %s remains live in group %d: %s", entry.Name(), group, data)
		}
	}
}

// R-WQYZ-WEDV
func TestMainFollowInterrupt(t *testing.T) {
	root, home, state, bin := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	worktree := filepath.Join(root, "wip")
	registryDir := filepath.Join(state, "ikigenba", "sandbox")
	for _, dir := range []string{worktree, registryDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := json.Marshal(map[string]any{"sandboxes": []any{map[string]any{"name": "wip", "port": 7400, "worktree": worktree, "apps": []any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registryDir, "registry.json"), registry, 0600); err != nil {
		t.Fatal(err)
	}
	script(t, bin, "git", "printf '%s\\n' \"$TEST_WORKTREE\"\n")
	script(t, bin, "journalctl", "printf 'a log line\\n'\nexec /bin/sleep 600\n")
	cmd := child(t, worktree, []string{"HOME=" + home, "XDG_STATE_HOME=" + state, "PATH=" + bin, "TEST_WORKTREE=" + worktree}, []string{"logs", "-f"})
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil || line != "a log line\n" {
		t.Fatalf("first line=%q error=%v", line, err)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	var rest bytes.Buffer
	if _, err := rest.ReadFrom(reader); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	assertExit(t, cmd, err, 0)
	if rest.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("additional stdout=%q stderr=%q", rest.String(), stderr.String())
	}
}
