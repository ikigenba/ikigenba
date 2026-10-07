package runner_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/scripts/internal/runner"
)

func TestCgroupSetupAndCollection(t *testing.T) {
	// R-HGKZ-W41S R-HHSW-9VSH R-PB39-5WUV R-HLGL-F70K
	for _, kill := range []bool{false, true} {
		t.Run(strconv.FormatBool(kill), func(t *testing.T) {
			d, env := fixture(t, "import os\nprint(os.getpid(), flush=True)\nwhile not os.path.exists('release'): pass\n")
			group := filepath.Join(t.TempDir(), "run")
			out := newObserved()
			p, err := runner.Start(context.Background(), runner.Spec{Dir: d, Env: env, Stdout: out, Cgroup: group, MemoryMax: 268435456, PidsMax: 64})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { p.Kill(); wait(t, p) })
			placed, err := os.ReadFile(filepath.Clean(filepath.Join(group, "cgroup.procs")))
			if err != nil {
				t.Fatal(err)
			}
			pid := out.next(t)
			if string(placed) != pid {
				t.Fatalf("cgroup.procs at Start return = %q, process pid = %q", placed, pid)
			}
			for name, want := range map[string]string{"memory.max": "268435456", "memory.oom.group": "1", "pids.max": "64", "memory.swap.max": "0"} {
				b, err := os.ReadFile(filepath.Clean(filepath.Join(group, name)))
				if err != nil || string(b) != want {
					t.Fatalf("%s = %q, %v", name, b, err)
				}
			}
			pipe := filepath.Join(group, "cgroup.kill")
			if err = syscall.Mkfifo(pipe, 0600); err != nil {
				t.Fatal(err)
			}
			read := make(chan string, 1)
			go func() {
				b, err := os.ReadFile(filepath.Clean(pipe))
				if err != nil {
					read <- err.Error()
				} else {
					read <- string(b)
				}
			}()
			if kill {
				if !p.Kill() {
					t.Fatal("Kill false")
				}
			} else if err = os.WriteFile(filepath.Join(d, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			want := 0
			if kill {
				want = 137
			}
			if got := wait(t, p); got != want {
				t.Fatalf("Wait %d", got)
			}
			if got := <-read; got != "1" {
				t.Fatalf("kill write %q", got)
			}
			if _, err = os.Stat(group); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("group remains: %v", err)
			}
		})
	}
}

func TestCgroupStartRefusalsAndCleanup(t *testing.T) {
	// R-PCB5-JOLK R-PDJ1-XGC9 R-HNWE-6QHY R-HQC6-Y9ZC
	for _, bad := range []string{"memory.max", "memory.oom.group", "pids.max", "memory.swap.max", "cgroup.procs", "unreadable", "parent", "regular", "memory-zero", "pids-zero", "dir", "env", "context"} {
		t.Run(bad, func(t *testing.T) {
			d, env := fixture(t, "while True: pass\n")
			root := t.TempDir()
			group := filepath.Join(root, "group")
			spec := runner.Spec{Dir: d, Env: env, Cgroup: group, MemoryMax: 1, PidsMax: 1}
			ctx := context.Background()
			exists := false
			switch bad {
			case "parent":
				spec.Cgroup = filepath.Join(root, "absent", "group")
			case "regular":
				if err := os.WriteFile(group, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "memory-zero":
				spec.MemoryMax = 0
			case "pids-zero":
				spec.PidsMax = 0
			case "dir":
				spec.Dir = filepath.Join(root, "absent")
			case "env":
				spec.Env = nil
			case "context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "unreadable":
				if err := os.Mkdir(group, 0700); err != nil {
					t.Fatal(err)
				}
				// Restore permissions so the test can remove its own fixture.
				t.Cleanup(func() { _ = syscall.Chmod(group, 0700) })
				if err := syscall.Chmod(group, 0333); err != nil {
					t.Fatal(err)
				}
				exists = true
			default:
				if err := os.Mkdir(group, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(group, bad), 0700); err != nil {
					t.Fatal(err)
				}
				exists = true
			}
			p, err := runner.Start(ctx, spec)
			if err == nil || p != nil {
				t.Fatalf("Start = %v, %v", p, err)
			}
			st, err := os.Stat(spec.Cgroup)
			if exists {
				if err != nil || !st.IsDir() {
					t.Fatalf("preexisting group removed: %v", err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("new group remains: %v", err)
			}
			if bad == "unreadable" {
				for name, want := range map[string]string{"memory.max": "1", "memory.oom.group": "1", "pids.max": "1", "memory.swap.max": "0"} {
					b, err := os.ReadFile(filepath.Clean(filepath.Join(group, name)))
					if err != nil || string(b) != want {
						t.Fatalf("%s = %q, %v", name, b, err)
					}
				}
			}
			procs, err := os.ReadDir("/proc")
			if err != nil {
				t.Fatal(err)
			}
			for _, proc := range procs {
				pid, err := strconv.Atoi(proc.Name())
				if err != nil {
					continue
				}
				stat, err := os.ReadFile(filepath.Join("/proc", proc.Name(), "stat"))
				if err != nil {
					continue
				}
				fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
				if len(fields) < 2 || fields[1] != strconv.Itoa(os.Getpid()) {
					continue
				}
				args, err := os.ReadFile(filepath.Join("/proc", proc.Name(), "cmdline"))
				if err == nil && bytes.HasSuffix(args, []byte("main.py\x00")) && !gone(pid) {
					t.Fatal("refused process remains", pid)
				}
			}
		})
	}
}

func TestCgroupOptionalAndExternalKill(t *testing.T) {
	// R-HP4A-KI8N R-HRK3-C1Q1
	for _, withGroup := range []bool{false, true} {
		d, env := fixture(t, "import os, signal\nos.kill(os.getpid(), signal.SIGKILL)\n")
		spec := runner.Spec{Dir: d, Env: env}
		if withGroup {
			spec.Cgroup = filepath.Join(t.TempDir(), "group")
			spec.MemoryMax = 1
			spec.PidsMax = 1
		}
		p, err := runner.Start(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := wait(t, p); got != 137 {
			t.Fatalf("Wait %d", got)
		}
	}
	d, env := fixture(t, "pass\n")
	p, err := runner.Start(context.Background(), runner.Spec{Dir: d, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if got := wait(t, p); got != 0 {
		t.Fatalf("Wait %d", got)
	}
}

func TestCgroupRemovalFailureKeepsExitStatus(t *testing.T) {
	// R-HMOH-SYR9
	d, env := fixture(t, "import os\nprint('ready', flush=True)\nwhile not os.path.exists('release'): pass\n")
	parent := t.TempDir()
	group := filepath.Join(parent, "group")
	out := newObserved()
	p, err := runner.Start(context.Background(), runner.Spec{Dir: d, Env: env, Stdout: out, Cgroup: group, MemoryMax: 1, PidsMax: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Kill(); wait(t, p); _ = syscall.Chmod(parent, 0700) })
	if got := out.next(t); got != "ready" {
		t.Fatal(got)
	}
	if err = syscall.Chmod(parent, 0500); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(d, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, p); got != 0 {
		t.Fatalf("Wait %d", got)
	}
}
