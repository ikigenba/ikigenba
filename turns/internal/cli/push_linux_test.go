//go:build linux

package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/paths"
)

func pushHeldLock(t *testing.T, path string) *os.File {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// R-PF47-9M1L
func TestPushHoldsExclusiveLockDuringEveryRequest(t *testing.T) {
	deps, p, f := pushSetup(t)
	pushClaude(t, p, "a", 1001)
	if _, err := os.Stat(p.State); !os.IsNotExist(err) {
		t.Fatal("state already exists")
	}
	f.hook = func(*http.Request) {
		lock, err := os.OpenFile(filepath.Join(p.State, paths.LockFile), os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = lock.Close() }()
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			t.Fatalf("exclusive lock not held: %v", err)
		}
	}
	code, _, errText := pushRun(t, deps)
	if code != ExitOK || errText != "" || len(f.requests) != 3 {
		t.Fatalf("%d %q %d", code, errText, len(f.requests))
	}
}

// R-M7NY-K4WY R-9HY2-JLBZ
func TestPushWaitsForActualFlockWaiter(t *testing.T) {
	deps, p, f := pushSetup(t)
	pushClaude(t, p, "a", 1)
	lock := pushHeldLock(t, filepath.Join(p.State, paths.LockFile))
	info, err := lock.Stat()
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("missing inode")
	}
	var requests atomic.Int64
	f.hook = func(*http.Request) { requests.Add(1) }
	type result struct {
		code     int
		out, err string
	}
	done := make(chan result, 1)
	go func() { code, out, errText := pushRun(t, deps); done <- result{code, out, errText} }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case got := <-done:
			t.Fatalf("push returned before lock release: %#v", got)
		default:
		}
		data, readErr := os.ReadFile("/proc/locks")
		if readErr != nil {
			t.Fatal(readErr)
		}
		waiter := false
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 6 || !strings.Contains(line, "->") || !strings.Contains(line, "FLOCK") {
				continue
			}
			for _, field := range fields {
				parts := strings.Split(field, ":")
				if len(parts) == 3 && parts[2] == strconv.FormatUint(stat.Ino, 10) {
					waiter = true
				}
			}
		}
		if waiter {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no blocked flock waiter in /proc/locks")
		}
		runtime.Gosched()
	}
	if requests.Load() != 0 {
		t.Fatal("request while waiting")
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.code != ExitOK || got.err != "" || got.out != "claude: 1 session, 1 turn pushed\ncodex: 0 sessions, 0 turns pushed\ngrok: 0 sessions, 0 turns pushed\n" {
			t.Fatalf("after unlock: %#v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("push did not complete after unlock")
	}
}

// R-9FI9-S1UL R-PHK0-15IZ
func TestPushRefusalAndDryRunDoNotWaitForLock(t *testing.T) {
	for _, kind := range []string{"token", "space", "none", "host", "dry"} {
		t.Run(kind, func(t *testing.T) {
			deps, p, f := pushSetup(t)
			lock := pushHeldLock(t, filepath.Join(p.State, paths.LockFile))
			switch kind {
			case "token":
				pushWrite(t, filepath.Join(p.Config, paths.ConfigFile), "")
			case "space":
				pushWrite(t, filepath.Join(p.Config, paths.TokenFile), "")
			case "none":
				pushWrite(t, filepath.Join(p.Config, paths.ConfigFile), "")
				pushWrite(t, filepath.Join(p.Config, paths.TokenFile), "")
			case "host":
				pushWrite(t, p.HostConfig, "space=test.example\n")
				pushWrite(t, filepath.Join(p.Config, paths.TokenFile), "")
			case "dry":
				pushClaude(t, p, "a", 1)
			}
			done := make(chan int, 1)
			go func() {
				args := []string{}
				if kind == "dry" {
					args = []string{"--dry-run"}
				}
				code, _, _ := pushRun(t, deps, args...)
				done <- code
			}()
			select {
			case code := <-done:
				want := ExitNotConfigured
				if kind == "dry" {
					want = ExitOK
				}
				if code != want {
					t.Fatal(code)
				}
			case <-time.After(10 * time.Second):
				_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
				<-done
				t.Fatal("waited on irrelevant lock")
			}
			if kind == "dry" && len(f.requests) != 1 {
				t.Fatal("dry run did not query")
			}
		})
	}
	t.Run("absent-lock", func(t *testing.T) {
		deps, p, _ := pushSetup(t)
		pushClaude(t, p, "a", 1)
		code, _, _ := pushRun(t, deps, "--dry-run")
		if code != ExitOK {
			t.Fatal(code)
		}
		if _, err := os.Stat(filepath.Join(p.State, paths.LockFile)); !os.IsNotExist(err) {
			t.Fatal(fmt.Sprint("created lock: ", err))
		}
	})
}
