package git_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/git"
)

func testGit(t *testing.T) (*git.Git, []string, string) {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := []string{"PATH=" + filepath.Dir(path), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	g, err := git.Find(filepath.Dir(path), func() []string { return env })
	if err != nil {
		t.Fatal(err)
	}
	return g, env, dir
}

func gitOutput(t *testing.T, g *git.Git, dir string, args ...string) string {
	t.Helper()
	data, err := g.Output(gitContext(t), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(data)
}

func gitContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestCommandUsesArgumentsDirectoryAndFreshEnvironment(t *testing.T) {
	// R-CMEP-VNSA R-COUI-N79O
	g, env, dir := testGit(t)
	first := filepath.Join(dir, "first-config")
	second := filepath.Join(dir, "second-config")
	for path, data := range map[string]string{first: "[test]\nvalue = first\n", second: "[test]\nvalue = second\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	current := append(append([]string(nil), env...), "GIT_CONFIG_GLOBAL="+first)
	g, err := git.Find(filepath.Dir(g.Command(gitContext(t), "", nil, "--version").Path), func() []string { return current })
	if err != nil {
		t.Fatal(err)
	}
	cmd := g.Command(gitContext(t), dir, nil, "config", "--get", "test.value")
	if cmd.Process != nil || cmd.Dir != dir {
		t.Fatalf("command already started or wrong directory: %+v", cmd)
	}
	current = append(append([]string(nil), env...), "GIT_CONFIG_GLOBAL="+second)
	secondCmd := g.Command(gitContext(t), dir, nil, "config", "--get", "test.value")
	if secondCmd.Process != nil || secondCmd.Dir != dir {
		t.Fatalf("second command already started or wrong directory: %+v", secondCmd)
	}
	// Neither command may consult this new environment when it is started.
	current = append([]string(nil), env...)
	data, err := cmd.Output()
	if err != nil || string(data) != "first\n" {
		t.Fatalf("first environment = %q, %v", data, err)
	}
	data, err = secondCmd.Output()
	if err != nil || string(data) != "second\n" {
		t.Fatalf("second construction-time environment = %q, %v", data, err)
	}
	current = append(append([]string(nil), env...), "GIT_CONFIG_GLOBAL="+second)
	data, err = g.Command(gitContext(t), dir, []string{"GIT_CONFIG_GLOBAL=" + first}, "config", "--get", "test.value").Output()
	if err != nil || string(data) != "first\n" {
		t.Fatalf("override = %q, %v", data, err)
	}
	gitOutput(t, g, dir, "init", "--template=", "--initial-branch=main", ".")
	if got := strings.TrimSpace(gitOutput(t, g, dir, "rev-parse", "--show-toplevel")); got != dir {
		t.Fatalf("working directory = %q, want %q", got, dir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, first)
	if err != nil {
		t.Fatal(err)
	}
	data, err = g.Command(gitContext(t), "", nil, "config", "--file", relative, "--get", "test.value").Output()
	if err != nil || string(data) != "first\n" {
		t.Fatalf("empty dir command in %s = %q, %v", cwd, data, err)
	}
}

func TestFindAndOutput(t *testing.T) {
	g, _, dir := testGit(t)
	if got := gitOutput(t, g, dir, "--version"); !strings.HasPrefix(got, "git version ") {
		t.Fatalf("version output = %q", got)
	}
	deadlineCtx := gitContext(t)
	if _, err := g.Output(deadlineCtx, dir, "not-a-git-command"); err == nil {
		t.Fatal("invalid command succeeded")
	}
	if err := deadlineCtx.Err(); err != nil {
		t.Fatalf("invalid-command git exceeded its deadline: %v", err)
	}
	ctx, cancel := context.WithCancel(gitContext(t))
	cancel()
	if _, err := g.Output(ctx, dir, "--version"); err == nil {
		t.Fatal("canceled Output succeeded")
	}
	for _, path := range []string{"", ".", ":.:" + dir, dir} {
		found, err := git.Find(path, func() []string { t.Error("Find read environment"); return nil })
		if found != nil || !errors.Is(err, git.ErrNotFound) {
			t.Fatalf("Find(%q) = %v, %v", path, found, err)
		}
	}
}

func TestCommandCancellationEndsReceivePack(t *testing.T) {
	// R-CQ2F-0Z0D
	g, _, dir := testGit(t)
	repo := filepath.Join(dir, "target.git")
	gitOutput(t, g, dir, "init", "--bare", "--template=", "--initial-branch=main", repo)
	// A socket receives git's own trace events, proving receive-pack has started
	// before cancellation; the pack remains deliberately incomplete throughout.
	traceDir, err := os.MkdirTemp("", "repos-git-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(traceDir); err != nil {
			t.Error(err)
		}
	})
	tracePath := filepath.Join(traceDir, "trace")
	listener, err := net.Listen("unix", tracePath)
	if err != nil {
		t.Fatal(err)
	}
	var traces sync.WaitGroup
	traces.Add(1)
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		traces.Wait()
	}()
	started := make(chan struct{}, 1)
	go func() {
		defer traces.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			traces.Add(1)
			go func() {
				defer traces.Done()
				defer func() {
					if err := conn.Close(); err != nil {
						t.Error(err)
					}
				}()
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					var event struct{ Event, Name string }
					if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Event == "cmd_name" && event.Name == "receive-pack" {
						select {
						case started <- struct{}{}:
						default:
						}
					}
				}
			}()
		}
	}()
	ctx, cancel := context.WithCancel(gitContext(t))
	defer cancel()
	cmd := g.Command(ctx, dir, []string{
		"GIT_PROJECT_ROOT=" + dir, "PATH_INFO=/target.git/git-receive-pack", "REQUEST_METHOD=POST", "QUERY_STRING=", "CONTENT_TYPE=application/x-git-receive-pack-request", "REMOTE_USER=test-user", "GIT_HTTP_EXPORT_ALL=1", "GIT_TRACE2_EVENT=af_unix:stream:" + tracePath,
	}, "-c", "http.receivepack=true", "http-backend")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	}()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	type outcome struct{ copyErr, waitErr error }
	finished := make(chan outcome, 1)
	go func() {
		_, err := io.Copy(io.Discard, stdout)
		finished <- outcome{copyErr: err, waitErr: cmd.Wait()}
	}()
	command := strings.Repeat("0", 40) + " " + strings.Repeat("1", 40) + " refs/heads/main\x00report-status side-band-64k\n"
	partial := fmt.Sprintf("%04x%s0000PACK", len(command)+4, command)
	if _, err := io.WriteString(stdin, partial); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("receive-pack did not start")
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("git reached its safety deadline before cancellation: %v", ctx.Err())
	}
	select {
	case result := <-finished:
		if result.copyErr != nil || result.waitErr == nil {
			t.Fatalf("canceled git: stdout error=%v wait error=%v", result.copyErr, result.waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation left git or its child holding stdout open")
	}
}
