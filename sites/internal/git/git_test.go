package git_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/git"
)

type fixture struct {
	root, executable string
	env              []string
	g                *git.Git
}

func setup(t *testing.T) fixture {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "git")
	if err := os.Symlink(gitPath, executable); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + root, "XDG_CONFIG_HOME=" + root, "PATH=" + filepath.Dir(gitPath), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0="}
	g, err := git.Find(bin, func() []string { return env })
	if err != nil {
		t.Fatal(err)
	}
	return fixture{root, executable, env, g}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

// R-A91M-9NRL R-CBY7-DQF0 R-CD63-RI5P
func TestFind(t *testing.T) {
	f := setup(t)
	calls := 0
	environ := func() []string { calls++; return f.env }
	bad := filepath.Join(f.root, "bad")
	if err := os.Mkdir(bad, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bad, "git"), "not executable")
	for _, path := range []string{"", ".", ":relative:", bad, f.executable} {
		got, err := git.Find(path, environ)
		if got != nil || !errors.Is(err, git.ErrNotFound) {
			t.Fatalf("Find(%q) = %v, %v", path, got, err)
		}
	}
	later := filepath.Join(f.root, "later")
	if err := os.Mkdir(later, 0700); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(f.executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(later, "git")); err != nil {
		t.Fatal(err)
	}
	g, err := git.Find(":"+bad+":relative:"+filepath.Dir(f.executable)+":"+later, environ)
	if err != nil || g == nil {
		t.Fatalf("Find = %v, %v", g, err)
	}
	if calls != 0 {
		t.Fatal("Find evaluated process environment")
	}
	if err := os.Remove(f.executable); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Output(t.Context(), f.root, "--version"); !errors.Is(err, git.ErrNotFound) {
		t.Fatal(err)
	}
}

// R-AA9I-NFIA R-AMGI-H4X8
func TestOutputEnvironmentAndDirectory(t *testing.T) {
	f := setup(t)
	one, two := filepath.Join(f.root, "one"), filepath.Join(f.root, "two")
	write(t, one, "[fixture]\n value = first\n")
	write(t, two, "[fixture]\n value = second\n")
	env := slices.Clone(f.env)
	calls := 0
	g, err := git.Find(filepath.Dir(f.executable), func() []string { calls++; return env })
	if err != nil {
		t.Fatal(err)
	}
	for i, test := range []struct{ config, want string }{{one, "first\n"}, {two, "second\n"}} {
		env[4] = "GIT_CONFIG_GLOBAL=" + test.config
		out, err := g.Output(t.Context(), f.root, "config", "--get", "fixture.value")
		if err != nil || string(out) != test.want {
			t.Fatalf("output %q: %v", out, err)
		}
		if calls != i+1 {
			t.Fatalf("environment calls %d", calls)
		}
	}
	write(t, filepath.Join(f.root, "local"), "[fixture]\n value = local\n")
	out, err := g.Output(t.Context(), f.root, "config", "--file", "local", "--get", "fixture.value")
	if err != nil || string(out) != "local\n" {
		t.Fatalf("directory output %q: %v", out, err)
	}
	cmd := g.Command(t.Context(), "", "config", "--get", "fixture.value")
	if cmd.Dir != "" || !slices.Equal(cmd.Env, env) {
		t.Fatalf("command directory/environment: %q %q", cmd.Dir, cmd.Env)
	}
	out, err = g.Output(t.Context(), "", "config", "--get", "fixture.value")
	if err != nil || string(out) != "second\n" {
		t.Fatalf("empty directory output %q: %v", out, err)
	}
}

// R-ABHF-178Z R-ANOE-UWNX R-ASK0-DZMP
func TestExitError(t *testing.T) {
	f := setup(t)
	args := []string{"--git-dir=" + filepath.Join(f.root, "absent"), "rev-parse", "--verify", "main"}
	_, err := f.g.Output(t.Context(), "", args...)
	var failure *git.Error
	if !errors.As(err, &failure) || failure.Status != 128 || !strings.HasPrefix(failure.Stderr, "fatal: not a git repository") {
		t.Fatalf("failure = %#v: %v", failure, err)
	}
	direct := exec.CommandContext(t.Context(), f.executable, args...)
	direct.Env = f.env
	var stderr bytes.Buffer
	direct.Stderr = &stderr
	if direct.Run() == nil {
		t.Fatal("expected direct git failure")
	}
	if failure.Stderr != stderr.String() {
		t.Fatalf("stderr %q != %q", failure.Stderr, stderr.String())
	}
	for _, status := range []int{1, 128, -1} {
		e := &git.Error{Status: status, Stderr: "arbitrary"}
		want := map[int]string{1: "git exited with status 1", 128: "git exited with status 128", -1: "git exited with status -1"}[status]
		if e.Error() != want {
			t.Fatal(e.Error())
		}
	}
}

// R-AOWB-8OEM
func TestMissingExecutable(t *testing.T) {
	f := setup(t)
	if err := os.Remove(f.executable); err != nil {
		t.Fatal(err)
	}
	_, err := f.g.Output(t.Context(), f.root, "--version")
	var failure *git.Error
	if !errors.Is(err, git.ErrNotFound) || errors.As(err, &failure) {
		t.Fatal(err)
	}
}

// R-CEE0-59WE R-ARC4-07W0
func TestCancellationKillsDescendants(t *testing.T) {
	for _, output := range []bool{true, false} {
		t.Run(strconv.FormatBool(output), func(t *testing.T) {
			f := setup(t)
			cause := errors.New("caller ended")
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(cause)
			tracePath := filepath.Join(f.root, "already-done")
			g, err := git.Find(filepath.Dir(f.executable), func() []string { return append(slices.Clone(f.env), "GIT_TRACE="+tracePath) })
			if err != nil {
				t.Fatal(err)
			}
			done, stop := context.WithCancelCause(t.Context())
			stop(cause)
			_, err = g.Output(done, f.root, "--version")
			var failure *git.Error
			if !errors.Is(err, cause) || errors.As(err, &failure) {
				t.Fatalf("done context: %v", err)
			}
			if _, err := os.Stat(tracePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("done context ran git")
			}
			fifo := filepath.Join(f.root, "trace")
			config := filepath.Join(f.root, "config")
			for _, path := range []string{fifo, config} {
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			trace, err := os.OpenFile(filepath.Clean(fifo), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := trace.Close(); err != nil {
					t.Error(err)
				}
			}()
			env := append(slices.Clone(f.env), "GIT_TRACE2_EVENT="+fifo)
			g, err = git.Find(filepath.Dir(f.executable), func() []string { return env })
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-c", "alias.hold=!git config --file " + strconv.Quote(config) + " --get fixture.value", "hold"}
			result := make(chan error, 1)
			var cmd *exec.Cmd
			if output {
				go func() { _, err := g.Output(ctx, f.root, args...); result <- err }()
			} else {
				cmd = g.Command(ctx, f.root, args...)
				if cmd.Process != nil || cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil || cmd.Dir != f.root || cmd.Path != f.executable || !slices.Equal(cmd.Env, env) || !slices.Equal(cmd.Args, append([]string{f.executable}, args...)) {
					t.Fatal("wrong unstarted command shape")
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				go func() { result <- cmd.Wait() }()
			}
			if err := trace.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(trace)
			var pids []int
			for len(pids) < 2 {
				var event struct {
					Event string
					SID   string `json:"sid"`
					Argv  []string
				}
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if event.Event != "start" {
					continue
				}
				marker := strings.LastIndex(event.SID, "-P")
				if marker < 0 {
					t.Fatalf("no pid in trace SID %q", event.SID)
				}
				pid, err := strconv.ParseInt(event.SID[marker+2:], 16, 32)
				if err != nil {
					t.Fatal(err)
				}
				pids = append(pids, int(pid))
			}
			defer func() {
				for _, pid := range pids {
					if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
						t.Error(err)
					}
				}
			}()
			// pidfds observe precisely these test-owned git processes even after reaping.
			fds := make([]int, 0, len(pids))
			for _, pid := range pids {
				fd, _, errno := syscall.Syscall(434, uintptr(pid), 0, 0) // Linux pidfd_open.
				if errno != 0 {
					t.Fatal(errno)
				}
				fds = append(fds, int(fd))
			}
			defer func() {
				for _, fd := range fds {
					if err := syscall.Close(fd); err != nil {
						t.Error(err)
					}
				}
			}()
			cancel(cause)
			select {
			case err := <-result:
				if output && (!errors.Is(err, cause) || errors.As(err, &failure)) {
					t.Fatal(err)
				}
				if !output && (err == nil || cmd.ProcessState == nil || cmd.ProcessState.Success()) {
					t.Fatalf("command state %v: %v", cmd.ProcessState, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("git did not return after cancellation")
			}
			for index, fd := range fds {
				var ready syscall.FdSet
				if fd >= len(ready.Bits)*64 {
					t.Fatal("pidfd exceeds select range")
				}
				ready.Bits[fd/64] |= int64(1) << uint(fd%64)
				timeout := syscall.Timeval{Sec: 5}
				if index == 0 {
					timeout = syscall.Timeval{}
				}
				n, err := syscall.Select(fd+1, &ready, nil, nil, &timeout)
				if err != nil || n != 1 {
					t.Fatalf("git process survived cancellation: %v, ready %d", err, n)
				}
			}
		})
	}
}

// TestSilentStreams supplies the git half of the cache owner's silence evidence.
func TestSilentStreams(t *testing.T) {
	f := setup(t)
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outWrite, errWrite
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		for _, file := range []*os.File{outWrite, errWrite, outRead, errRead} {
			if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
		}
	}()
	if _, err := f.g.Output(t.Context(), f.root, "config", "--get", "absent.key"); err == nil {
		t.Fatal("expected absent key")
	}
	if _, err := f.g.Output(t.Context(), f.root, "--git-dir="+filepath.Join(f.root, "absent"), "rev-parse", "HEAD"); err == nil {
		t.Fatal("expected invalid repository")
	}
	if _, err := f.g.Output(t.Context(), f.root, "--version"); err != nil {
		t.Fatal(err)
	}
	cmd := f.g.Command(t.Context(), f.root, "--git-dir="+filepath.Join(f.root, "absent"), "rev-parse", "HEAD")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected command failure")
	}
	cmd = f.g.Command(t.Context(), f.root, "--version")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = oldOut, oldErr
	for _, writer := range []*os.File{outWrite, errWrite} {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, read := range []*os.File{outRead, errRead} {
		data, err := io.ReadAll(read)
		if err != nil || len(data) != 0 {
			t.Fatalf("process stream wrote %q: %v", data, err)
		}
	}
}
