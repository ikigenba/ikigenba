package seam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestSurface(t *testing.T) {
	// R-9ZWO-GWW7
	assertFields(t, Cmd{Dir: ""}, []field{
		{name: "Path", typ: reflect.TypeFor[string]()},
		{name: "Args", typ: reflect.TypeFor[[]string]()},
		{name: "Dir", typ: reflect.TypeFor[string]()},
		{name: "Env", typ: reflect.TypeFor[[]string]()},
	})
	assertFields(t, Result{}, []field{
		{name: "Stdout", typ: reflect.TypeFor[[]byte]()},
		{name: "Stderr", typ: reflect.TypeFor[[]byte]()},
		{name: "ExitCode", typ: reflect.TypeFor[int]()},
	})
	var _ Runner = Exec

	// R-BO0E-2JC6
	var _ StreamRunner = func(context.Context, Cmd, io.Writer) (Result, error) {
		return Result{}, nil
	}

	// R-BP8A-GB2V
	var _ StreamRunner = Stream
}

func TestExecContract(t *testing.T) {
	// R-A2CH-8GDL
	path, name := helperOnPath(t)
	t.Setenv("PATH", path+string(os.PathListSeparator)+environment("PATH"))
	t.Setenv("SEAM_INHERITED", "inherited")
	t.Setenv("SEAM_OVERRIDE", "old")
	dir := t.TempDir()
	result, err := Exec(context.Background(), Cmd{
		Path: name,
		Args: []string{"-test.run=TestHelperProcess", "--", "inspect"},
		Dir:  dir,
		Env: []string{
			"GO_WANT_SEAM_HELPER=1",
			"SEAM_OVERRIDE=new",
		},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	wantStdout := fmt.Sprintf("%s\ninherited\nnew\n", dir)
	if string(result.Stdout) != wantStdout {
		t.Fatalf("stdout = %q, want %q", result.Stdout, wantStdout)
	}
	if string(result.Stderr) != "separate stderr\n" {
		t.Fatalf("stderr = %q", result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestExecErrorsAndExitStatus(t *testing.T) {
	// R-A14K-UOMW
	dir := t.TempDir()
	path, name := helperOnPath(t)
	t.Setenv("PATH", path+string(os.PathListSeparator)+environment("PATH"))
	command := Cmd{
		Path: name,
		Args: []string{"-test.run=TestHelperProcess", "--", "exit"},
		Dir:  dir,
		Env:  []string{"GO_WANT_SEAM_HELPER=1"},
	}
	result, err := Exec(context.Background(), command)
	if err != nil {
		t.Fatalf("non-zero exit returned error: %v", err)
	}
	if result.ExitCode != 23 {
		t.Fatalf("exit code = %d, want 23", result.ExitCode)
	}

	emptyPath, emptyPathMarker := markerCommand(t)
	emptyPath.Path = ""
	if _, err := Exec(context.Background(), emptyPath); err == nil {
		t.Fatal("empty path returned nil error")
	}
	assertNotStarted(t, emptyPathMarker)

	emptyDir, emptyDirMarker := markerCommand(t)
	emptyDir.Dir = ""
	if _, err := Exec(context.Background(), emptyDir); err == nil {
		t.Fatal("empty directory returned nil error")
	}
	assertNotStarted(t, emptyDirMarker)

	if _, err := Exec(context.Background(), Cmd{Path: "not-a-real-seam-program", Dir: dir}); err == nil {
		t.Fatal("startup failure returned nil error")
	}

	preCancelled, preCancelledMarker := markerCommand(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Exec(ctx, preCancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v, want context.Canceled", err)
	}
	assertNotStarted(t, preCancelledMarker)

	ctx, cancel = context.WithCancel(context.Background())
	pidDir := t.TempDir()
	pidFile := filepath.Join(pidDir, "pid")
	blocking := helperCommand(t, "block")
	blocking.Env = append(blocking.Env, "SEAM_PID_FILE="+pidFile)
	done := make(chan streamOutcome, 1)
	go func() {
		got, execErr := Exec(ctx, blocking)
		done <- streamOutcome{result: got, err: execErr}
	}()
	pid := awaitHelperPID(t, pidDir)
	cancel()
	select {
	case outcome := <-done:
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("running cancellation error = %v, want context.Canceled", outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Exec did not finish after cancellation")
	}
	assertProcessGone(t, pid)
}

func TestDepsDefaults(t *testing.T) {
	// R-BMSH-ORLH
	assertFields(t, Deps{}, []field{
		{name: "Dir", typ: reflect.TypeFor[string]()},
		{name: "EUID", typ: reflect.TypeFor[int]()},
		{name: "Getenv", typ: reflect.TypeFor[func(string) string]()},
		{name: "Cloud", typ: reflect.TypeFor[cloud.Opener]()},
		{name: "Exec", typ: reflect.TypeFor[Runner]()},
		{name: "Stream", typ: reflect.TypeFor[StreamRunner]()},
		{name: "Now", typ: reflect.TypeFor[func() time.Time]()},
		{name: "After", typ: reflect.TypeFor[func(time.Duration) <-chan time.Time]()},
	})

	deps := (Deps{}).Defaults()
	if got := deps.Getenv("ANYTHING"); got != "" {
		t.Fatalf("nil Getenv default = %q, want empty", got)
	}
	if reflect.ValueOf(deps.Exec).Pointer() != reflect.ValueOf(Exec).Pointer() {
		t.Fatal("nil Exec did not select seam.Exec")
	}
	if reflect.ValueOf(deps.Stream).Pointer() != reflect.ValueOf(Stream).Pointer() {
		t.Fatal("nil Stream did not select seam.Stream")
	}
	before := time.Now()
	if got := deps.Now(); got.Before(before) || got.After(time.Now()) {
		t.Fatalf("nil Now did not select time.Now: %v", got)
	}
	select {
	case <-deps.After(time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("nil After did not select time.After")
	}
}

func TestStreamDeliversBeforeExitAndCapturesStderr(t *testing.T) {
	// R-BQG6-U2TK
	// R-BRO3-7UK9
	command := helperCommand(t, "stream")
	writer := &signalWriter{delivered: make(chan struct{})}
	done := make(chan streamOutcome, 1)
	go func() {
		result, err := Stream(context.Background(), command, writer)
		done <- streamOutcome{result: result, err: err}
	}()

	select {
	case <-writer.delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("stdout was not delivered before process exit")
	}
	select {
	case outcome := <-done:
		t.Fatalf("Stream returned before helper exit: %+v", outcome)
	default:
	}
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("Stream: %v", outcome.err)
	}
	if got := writer.String(); got != "ready\ndone\n" {
		t.Fatalf("streamed stdout = %q", got)
	}
	if len(outcome.result.Stdout) != 0 {
		t.Fatalf("result stdout = %q, want empty", outcome.result.Stdout)
	}
	if string(outcome.result.Stderr) != "stream stderr\n" {
		t.Fatalf("result stderr = %q", outcome.result.Stderr)
	}
}

func TestStreamCommandContract(t *testing.T) {
	// R-BQG6-U2TK
	path, name := helperOnPath(t)
	t.Setenv("PATH", path+string(os.PathListSeparator)+environment("PATH"))
	t.Setenv("SEAM_INHERITED", "inherited")
	t.Setenv("SEAM_OVERRIDE", "old")
	dir := t.TempDir()
	var stdout bytes.Buffer
	result, err := Stream(context.Background(), Cmd{
		Path: name,
		Args: []string{"-test.run=TestHelperProcess", "--", "inspect"},
		Dir:  dir,
		Env: []string{
			"GO_WANT_SEAM_HELPER=1",
			"SEAM_OVERRIDE=new",
		},
	}, &stdout)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	wantStdout := fmt.Sprintf("%s\ninherited\nnew\n", dir)
	if stdout.String() != wantStdout {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantStdout)
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("result stdout = %q, want empty", result.Stdout)
	}
	if string(result.Stderr) != "separate stderr\n" {
		t.Fatalf("stderr = %q", result.Stderr)
	}

	emptyPath, emptyPathMarker := markerCommand(t)
	emptyPath.Path = ""
	if _, err := Stream(context.Background(), emptyPath, &stdout); err == nil {
		t.Fatal("empty path returned nil error")
	}
	assertNotStarted(t, emptyPathMarker)

	emptyDir, emptyDirMarker := markerCommand(t)
	emptyDir.Dir = ""
	if _, err := Stream(context.Background(), emptyDir, &stdout); err == nil {
		t.Fatal("empty directory returned nil error")
	}
	assertNotStarted(t, emptyDirMarker)

	preCancelled, preCancelledMarker := markerCommand(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Stream(ctx, preCancelled, &stdout); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v, want context.Canceled", err)
	}
	assertNotStarted(t, preCancelledMarker)

	if _, err := Stream(context.Background(), Cmd{Path: "not-a-real-seam-program", Dir: dir}, &stdout); err == nil {
		t.Fatal("startup failure returned nil error")
	}
}

func TestStreamCancellationTerminatesAndReaps(t *testing.T) {
	// R-BRO3-7UK9
	ctx, cancel := context.WithCancel(context.Background())
	writer := &signalWriter{delivered: make(chan struct{})}
	done := make(chan error, 1)
	command := helperCommand(t, "block")
	pidDir := t.TempDir()
	pidFile := filepath.Join(pidDir, "pid")
	command.Env = append(command.Env, "SEAM_PID_FILE="+pidFile)
	go func() {
		_, err := Stream(ctx, command, writer)
		done <- err
	}()
	select {
	case <-writer.delivered:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("helper did not start")
	}
	pid := awaitHelperPID(t, pidDir)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not finish after cancellation")
	}
	assertProcessGone(t, pid)
}

func TestStreamWriterFailureStopsProcess(t *testing.T) {
	// R-BQG6-U2TK
	wantErr := errors.New("writer failed")
	done := make(chan error, 1)
	command := helperCommand(t, "block")
	pidDir := t.TempDir()
	pidFile := filepath.Join(pidDir, "pid")
	command.Env = append(command.Env, "SEAM_PID_FILE="+pidFile)
	go func() {
		_, err := Stream(context.Background(), command, errorWriter{err: wantErr})
		done <- err
	}()
	pid := awaitHelperPID(t, pidDir)
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("writer error = %v, want %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not stop after writer failure")
	}
	assertProcessGone(t, pid)
}

func TestHelperProcess(_ *testing.T) {
	if environment("GO_WANT_SEAM_HELPER") != "1" {
		return
	}
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(97)
	}
	switch os.Args[separator+1] {
	case "inspect":
		dir, err := filepath.Abs(".")
		if err != nil {
			os.Exit(96)
		}
		fmt.Printf("%s\n%s\n%s\n", dir, environment("SEAM_INHERITED"), environment("SEAM_OVERRIDE"))
		fmt.Fprintln(os.Stderr, "separate stderr")
	case "exit":
		os.Exit(23)
	case "stream":
		fmt.Println("ready")
		fmt.Fprintln(os.Stderr, "stream stderr")
		time.Sleep(300 * time.Millisecond)
		fmt.Println("done")
	case "block":
		if pidFile := environment("SEAM_PID_FILE"); pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
				os.Exit(94)
			}
		}
		fmt.Println("ready")
		time.Sleep(30 * time.Second)
	case "mark":
		if err := os.WriteFile(environment("SEAM_MARKER"), []byte("started\n"), 0o600); err != nil {
			os.Exit(93)
		}
	default:
		os.Exit(95)
	}
	os.Exit(0)
}

type streamOutcome struct {
	result Result
	err    error
}

type signalWriter struct {
	buffer    bytes.Buffer
	delivered chan struct{}
}

func (writer *signalWriter) Write(data []byte) (int, error) {
	count, err := writer.buffer.Write(data)
	select {
	case <-writer.delivered:
	default:
		close(writer.delivered)
	}
	return count, err
}

func (writer *signalWriter) String() string {
	return writer.buffer.String()
}

type errorWriter struct {
	err error
}

func (writer errorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type field struct {
	name string
	typ  reflect.Type
}

func assertFields(t *testing.T, value any, want []field) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typeOf, typeOf.NumField(), len(want))
	}
	for index, wantField := range want {
		got := typeOf.Field(index)
		if got.Name != wantField.name || got.Type != wantField.typ {
			t.Fatalf("%s field %d = %s %s, want %s %s", typeOf, index, got.Name, got.Type, wantField.name, wantField.typ)
		}
	}
}

func helperCommand(t *testing.T, operation string) Cmd {
	t.Helper()
	path, name := helperOnPath(t)
	t.Setenv("PATH", path+string(os.PathListSeparator)+environment("PATH"))
	return Cmd{
		Path: name,
		Args: []string{"-test.run=TestHelperProcess", "--", operation},
		Dir:  t.TempDir(),
		Env:  []string{"GO_WANT_SEAM_HELPER=1"},
	}
}

func markerCommand(t *testing.T) (Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	command := helperCommand(t, "mark")
	command.Env = append(command.Env, "SEAM_MARKER="+marker)
	return command, marker
}

func assertNotStarted(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("process start marker: %v, want not exist", err)
	}
}

func awaitHelperPID(t *testing.T, dir string) int {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("open helper pid directory: %v", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("close helper pid directory: %v", err)
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := root.ReadFile("pid")
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
				t.Fatalf("parse helper pid %q: %v", data, err)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read helper pid: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper did not record its pid")
	return 0
}

func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("helper process %d still exists after return: %v", pid, err)
	}
}

func helperOnPath(t *testing.T) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dir := t.TempDir()
	name := "seam-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	if err := os.Symlink(executable, target); err != nil {
		t.Fatalf("link helper: %v", err)
	}
	return dir, name
}

func environment(name string) string {
	value, _ := syscall.Getenv(name)
	return value
}
