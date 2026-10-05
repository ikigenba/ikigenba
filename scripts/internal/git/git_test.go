package git_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/git"
)

func executable(t *testing.T) string {
	t.Helper()
	p, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func environment(d string) []string {
	return []string{"HOME=" + d, "XDG_CONFIG_HOME=" + d, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0="}
}
func selected(t *testing.T, env func() []string) *git.Git {
	t.Helper()
	g, e := git.Find(filepath.Dir(executable(t)), env)
	if e != nil {
		t.Fatal(e)
	}
	return g
}

// R-H8XZ-127W R-HCLO-6DFZ R-HDTK-K56O R-HIP6-385G
func TestFind(t *testing.T) {
	d := t.TempDir()
	env := environment(d)
	gitPath := executable(t)
	for _, path := range []string{"", ":relative", d} {
		g, e := git.Find(path, func() []string { return env })
		if g != nil || !errors.Is(e, git.ErrNotFound) {
			t.Fatalf("Find(%q) = %v %v", path, g, e)
		}
	}
	bad := filepath.Join(d, "bad")
	first := filepath.Join(d, "first")
	second := filepath.Join(d, "second")
	for _, p := range []string{bad, first, second} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(bad, "git"), []byte("not executable"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{first, second} {
		if e := os.Symlink(gitPath, filepath.Join(p, "git")); e != nil {
			t.Fatal(e)
		}
	}
	g, e := git.Find(":relative:"+bad+":"+first+":"+second, func() []string { return env })
	if e != nil || g == nil {
		t.Fatal(e)
	}
	if _, e = g.Output(context.Background(), d, "--version"); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(first, "git")); e != nil {
		t.Fatal(e)
	}
	_, e = g.Output(context.Background(), d, "--version")
	var ge *git.Error
	if !errors.Is(e, git.ErrNotFound) || errors.As(e, &ge) {
		t.Fatalf("selected path lost: %v", e)
	}
	if e = os.Remove(filepath.Join(second, "git")); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(second, "git"), 0700); e != nil {
		t.Fatal(e)
	}
	if g, e = git.Find(second, func() []string { return env }); g != nil || !errors.Is(e, git.ErrNotFound) {
		t.Fatal("directory accepted")
	}
}

// R-HA5V-ETYL R-HF1G-XWXD R-HBDR-SLPA R-HG9D-BOO2 R-HMCV-8JDJ
func TestOutput(t *testing.T) {
	d := t.TempDir()
	env := environment(d)
	cfg := filepath.Join(d, "global")
	if e := os.WriteFile(cfg, []byte("[example]\nvalue = first\n"), 0600); e != nil {
		t.Fatal(e)
	}
	env = append(env, "GIT_CONFIG_GLOBAL="+cfg)
	calls := 0
	g := selected(t, func() []string { calls++; return env })
	out, e := g.Output(context.Background(), d, "config", "--get", "example.value")
	if e != nil || string(out) != "first\n" {
		t.Fatalf("%q %v", out, e)
	}
	other := filepath.Join(d, "other")
	if e = os.WriteFile(other, []byte("[example]\nvalue = second\n"), 0600); e != nil {
		t.Fatal(e)
	}
	env = append(environment(d), "GIT_CONFIG_GLOBAL="+other)
	out, e = g.Output(context.Background(), d, "config", "--get", "example.value")
	if e != nil || string(out) != "second\n" || calls != 2 {
		t.Fatalf("%q %v calls %d", out, e, calls)
	}
	if e = os.WriteFile(filepath.Join(d, "local"), []byte("[example]\nvalue = local\n"), 0600); e != nil {
		t.Fatal(e)
	}
	out, e = g.Output(context.Background(), d, "config", "--file", "local", "--get", "example.value")
	if e != nil || string(out) != "local\n" {
		t.Fatalf("working dir: %q %v", out, e)
	}
	args := []string{"--git-dir=" + filepath.Join(d, "missing"), "rev-parse", "--verify", "main"}
	cmd := exec.Command(executable(t), args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	_, e = g.Output(context.Background(), "", args...)
	var ge *git.Error
	if !errors.As(e, &ge) || ge.Status != 128 || ge.Stderr != stderr.String() || !strings.HasPrefix(ge.Stderr, "fatal: not a git repository") || ge.Error() != "git exited with status 128" {
		t.Fatalf("%#v", e)
	}
}

func processes(t *testing.T, marker string) []int {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	if e != nil {
		t.Fatal(e)
	}
	var found []int
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if e == nil && bytes.Contains(b, []byte(marker+"\x00")) {
			found = append(found, pid)
		}
	}
	return found
}
func awaitProcess(t *testing.T, marker string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if len(processes(t, marker)) > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("git did not start")
		default:
			runtime.Gosched()
		}
	}
}

// R-HJX2-GZW5 R-HL4Y-URMU
func TestCancellationAndCommand(t *testing.T) {
	d := t.TempDir()
	fifo := filepath.Join(d, "trace")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	marker := "SCRIPTS_GIT_TEST=" + d
	env := append(environment(d), "GIT_TRACE="+fifo, marker)
	calls := 0
	g := selected(t, func() []string { calls++; return env })
	cause := errors.New("caller stopped")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	_, e := g.Output(ctx, d, "--version")
	var ge *git.Error
	if !errors.Is(e, cause) || errors.As(e, &ge) || calls != 0 {
		t.Fatalf("already canceled: %v calls %d", e, calls)
	}
	ctx, cancel = context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() { _, err := g.Output(ctx, d, "--version"); done <- err }()
	awaitProcess(t, marker)
	cancel(cause)
	select {
	case e = <-done:
		if !errors.Is(e, cause) || errors.As(e, &ge) {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("git did not exit")
	}
	if p := processes(t, marker); len(p) > 0 {
		t.Fatalf("processes remain %v", p)
	}
	ctx, cancel = context.WithCancelCause(context.Background())
	cmd := g.Command(ctx, d, "--version")
	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil || cmd.Process != nil || cmd.Dir != d || len(cmd.Env) != len(env) {
		t.Fatal("prepared command differs")
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	awaitProcess(t, marker)
	cancel(cause)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not exit")
	}
	if p := processes(t, marker); len(p) > 0 {
		t.Fatalf("processes remain %v", p)
	}
}

// R-HJX2-GZW5 R-HL4Y-URMU
func TestCancellationKillsGitDescendant(t *testing.T) {
	for _, operation := range []string{"output", "command"} {
		t.Run(operation, func(t *testing.T) {
			d := t.TempDir()
			fifo := filepath.Join(d, "child-trace")
			if e := syscall.Mkfifo(fifo, 0600); e != nil {
				t.Fatal(e)
			}
			parentMarker := "SCRIPTS_GIT_PARENT=" + d
			childMarker := "SCRIPTS_GIT_CHILD=" + d
			env := append(environment(d), parentMarker)
			g := selected(t, func() []string { return env })
			// Git's shell alias execs another real git, which blocks opening its trace FIFO.
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			alias := "!GIT_TRACE=" + quote(fifo) + " " + childMarker + " exec " + quote(executable(t)) + " --version"
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			cause := errors.New("stop group")
			done := make(chan error, 1)
			if operation == "output" {
				go func() { _, e := g.Output(ctx, d, "-c", "alias.child="+alias, "child"); done <- e }()
			} else {
				cmd := g.Command(ctx, d, "-c", "alias.child="+alias, "child")
				if e := cmd.Start(); e != nil {
					t.Fatal(e)
				}
				go func() { done <- cmd.Wait() }()
			}
			awaitProcess(t, childMarker)
			if p := processes(t, parentMarker); len(p) < 2 {
				t.Fatalf("expected parent and descendant, got %v", p)
			}
			cancel(cause)
			select {
			case e := <-done:
				if operation == "output" && !errors.Is(e, cause) {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("process group did not exit")
			}
			for _, marker := range []string{parentMarker, childMarker} {
				if p := processes(t, marker); len(p) > 0 {
					t.Fatalf("remaining processes %v", p)
				}
			}
		})
	}
}
