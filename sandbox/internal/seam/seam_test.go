package seam_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-Y1NR-G0BX R-Y2VN-TS2M
func TestPublicTypes(t *testing.T) {
	cmd := seam.Cmd{Path: "/bin/true", Args: []string{}, Dir: t.TempDir()}
	result := seam.Result{Stdout: []byte("a"), Output: []byte("b"), ExitCode: 0}
	var exec seam.Runner = seam.Exec
	var stream seam.StreamRunner = seam.Stream
	deps := seam.Deps{Dir: cmd.Dir, EUID: 1000, Getenv: func(string) string { return "" }, Exec: exec, Stream: stream}
	got, err := deps.Exec(context.Background(), cmd)
	if err != nil || got.ExitCode != result.ExitCode {
		t.Fatalf("%+v %v", got, err)
	}
}

func script(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "helper")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	file, err := root.OpenFile("helper", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("#!/bin/sh\n" + content); err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0700); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// R-Y8Z5-QMS3 R-0EC6-P58T
func TestArgumentsDirectory(t *testing.T) {
	p := script(t, "printf '%s\\n' \"$@\"; pwd\n")
	t.Setenv("PATH", filepath.Dir(p))
	dir := t.TempDir()
	cmd := seam.Cmd{Path: "helper", Args: []string{"a b", "x"}, Dir: dir}
	r, err := seam.Exec(context.Background(), cmd)
	want := "a b\nx\n" + dir + "\n"
	if err != nil || string(r.Stdout) != want {
		t.Fatalf("%q %v", r.Stdout, err)
	}
	var b bytes.Buffer
	r, err = seam.Stream(context.Background(), cmd, &b)
	if err != nil || b.String() != want || r.ExitCode != 0 {
		t.Fatalf("%q %+v %v", b.String(), r, err)
	}
}

// R-OJVX-QLEL R-0FK3-2WZI
func TestEnvironment(t *testing.T) {
	t.Setenv("SANDBOX_TEST_ENV", "unaltered value")
	want := environment(os.Environ())
	cmd := seam.Cmd{Path: "/usr/bin/env", Args: []string{"-0"}, Dir: t.TempDir()}
	r, err := seam.Exec(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(environment(strings.Split(strings.TrimSuffix(string(r.Stdout), "\x00"), "\x00")), want) {
		t.Fatal("environment changed")
	}
	var b bytes.Buffer
	_, err = seam.Stream(context.Background(), cmd, &b)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(environment(strings.Split(strings.TrimSuffix(b.String(), "\x00"), "\x00")), want) {
		t.Fatal("stream environment changed")
	}
}
func environment(values []string) []string {
	var out []string
	for _, v := range values {
		if !strings.HasPrefix(v, "PWD=") {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// R-YCMU-VY06 R-0GRZ-GOQ7
func TestEmptyInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	defer func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}()
	_, err = writer.WriteString("must not inherit")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = old }()
	cmd := seam.Cmd{Path: "/bin/sh", Args: []string{"-c", "cat"}, Dir: t.TempDir()}
	r, err := seam.Exec(context.Background(), cmd)
	if err != nil || len(r.Stdout) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	var b bytes.Buffer
	_, err = seam.Stream(context.Background(), cmd, &b)
	if err != nil || b.Len() != 0 {
		t.Fatalf("%q %v", b.String(), err)
	}
}

// R-YDUR-9PQV R-YF2N-NHHK R-YNLY-BVOF
func TestCapture(t *testing.T) {
	for _, tc := range []struct{ source, out, diagnostic string }{{"printf 'abc'; printf 'def' >&2", "abc", "def"}, {"printf 'abc'", "abc", ""}, {"printf 'def' >&2", "", "def"}} {
		cmd := seam.Cmd{Path: "/bin/sh", Args: []string{"-c", tc.source}, Dir: t.TempDir()}
		r, err := seam.Exec(context.Background(), cmd)
		if err != nil || string(r.Stdout) != tc.out {
			t.Fatalf("%+v %v", r, err)
		}
		// Each stream's bytes must retain its own order. Distinct bytes make filtering unambiguous.
		var out, diagnostic strings.Builder
		for _, c := range string(r.Output) {
			if strings.ContainsRune("abc", c) {
				out.WriteRune(c)
			} else {
				diagnostic.WriteRune(c)
			}
		}
		if out.String() != tc.out || diagnostic.String() != tc.diagnostic {
			t.Fatalf("merged %q", r.Output)
		}
		var b bytes.Buffer
		r, err = seam.Stream(context.Background(), cmd, &b)
		if err != nil || b.String() != tc.out || string(r.Output) != tc.diagnostic || len(r.Stdout) != 0 {
			t.Fatalf("stream %q %+v %v", b.String(), r, err)
		}
	}
}

// R-YGAK-1989 R-YHIG-F0YY R-0HZV-UGGW R-0J7S-887L
func TestExit(t *testing.T) {
	for _, tc := range []struct {
		source string
		code   int
	}{{"exit 0", 0}, {"exit 17", 17}, {"kill -TERM $$", 143}} {
		cmd := seam.Cmd{Path: "/bin/sh", Args: []string{"-c", tc.source}, Dir: t.TempDir()}
		r, err := seam.Exec(context.Background(), cmd)
		if err != nil || r.ExitCode != tc.code {
			t.Fatalf("%+v %v", r, err)
		}
		r, err = seam.Stream(context.Background(), cmd, io.Discard)
		if err != nil || r.ExitCode != tc.code {
			t.Fatalf("%+v %v", r, err)
		}
	}
}

// R-YIQC-SSPN R-YJY9-6KGC R-0KFO-LZYA R-0LNK-ZROZ
func TestStartFailures(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	p := script(t, "touch '"+marker+"'\n")
	for _, cmd := range []seam.Cmd{{Path: "missing-sandbox-test-program", Dir: t.TempDir()}, {Path: p, Dir: filepath.Join(t.TempDir(), "missing")}, {Path: p, Dir: ""}} {
		if _, err := seam.Exec(context.Background(), cmd); err == nil {
			t.Fatal("exec succeeded")
		}
		if _, err := seam.Stream(context.Background(), cmd, io.Discard); err == nil {
			t.Fatal("stream succeeded")
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("program started %v", err)
	}
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

// R-YME1-Y3XQ
func TestStreamingBeforeExit(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "release")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var b bytes.Buffer
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	writer := writerFunc(func(p []byte) (int, error) {
		b.Write(p)
		f, err := root.OpenFile("release", os.O_WRONLY, 0)
		if err != nil {
			return 0, err
		}
		_, err = f.WriteString("go\n")
		closeErr := f.Close()
		if err != nil {
			return 0, err
		}
		if closeErr != nil {
			return 0, closeErr
		}
		return len(p), nil
	})
	_, err = seam.Stream(ctx, seam.Cmd{Path: "/bin/sh", Args: []string{"-c", "echo ready; read value < \"$1\"", "helper", fifo}, Dir: dir}, writer)
	if err != nil || b.String() != "ready\n" {
		t.Fatalf("%q %v", b.String(), err)
	}
}

// R-YR9N-H6WI
func TestWriterFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	want := errors.New("writer failure")
	pid := 0
	var pidErr error
	_, err := seam.Stream(ctx, seam.Cmd{Path: "/bin/sh", Args: []string{"-c", "echo $$; exec sleep 600"}, Dir: t.TempDir()}, writerFunc(func(p []byte) (int, error) {
		pid, pidErr = strconv.Atoi(strings.TrimSpace(string(p)))
		return 0, want
	}))
	if !errors.Is(err, want) || ctx.Err() != nil {
		t.Fatalf("%v ctx %v", err, ctx.Err())
	}
	if pidErr != nil || pid <= 0 {
		t.Fatalf("child did not report its pid: %d %v", pid, pidErr)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child %d still exists after Stream returned: %v", pid, err)
	}
}

// R-OMBQ-I4VZ R-ONJM-VWMO
func TestCancellationKillsGroup(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			dir := t.TempDir()
			pid := 0
			done := make(chan error, 1)
			if stream {
				go func() {
					_, err := seam.Stream(ctx, seam.Cmd{Path: "/bin/sh", Args: []string{"-c", "sleep 600 & echo $$; sleep 600"}, Dir: dir}, writerFunc(func(p []byte) (int, error) {
						var err error
						pid, err = strconv.Atoi(strings.TrimSpace(string(p)))
						if err != nil {
							return 0, err
						}
						cancel()
						return len(p), nil
					}))
					done <- err
				}()
			} else {
				fifo := filepath.Join(dir, "pid")
				if err := syscall.Mkfifo(fifo, 0600); err != nil {
					t.Fatal(err)
				}
				go func() {
					_, err := seam.Exec(ctx, seam.Cmd{Path: "/bin/sh", Args: []string{"-c", "sleep 600 & echo $$ > \"$1\"; sleep 600", "helper", fifo}, Dir: dir})
					done <- err
				}()
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := root.Close(); err != nil {
						t.Error(err)
					}
				}()
				f, err := root.Open("pid")
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(f)
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("%v %v", err, closeErr)
				}
				pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil {
					t.Fatal(err)
				}
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, ctx.Err()) {
					t.Fatalf("%v ctx %v", err, ctx.Err())
				}
			case <-time.After(12 * time.Second):
				t.Fatal("runner did not return")
			}
			if pid <= 0 {
				t.Fatal("no group pid")
			}
			assertNoLiveGroup(t, pid)
		})
	}
}
func assertNoLiveGroup(t *testing.T, pid int) {
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
		if err != nil {
			continue
		}
		end := strings.LastIndex(string(data), ")")
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data)[end+1:])
		if len(fields) > 2 && fields[2] == strconv.Itoa(pid) && fields[0] != "Z" {
			t.Fatalf("live group member %s: %s", entry.Name(), data)
		}
	}
}
