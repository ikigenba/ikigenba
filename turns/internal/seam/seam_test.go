package seam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func TestUnits(t *testing.T) {
	// R-JSP9-R2Q3
	type unitsSignature func() fs.FS
	var unitsFunc unitsSignature = turns.Units
	units := unitsFunc()
	// R-JWCY-WDY6 R-JXKV-A5OV R-JYSR-NXFK
	type unitName string
	const service unitName = turns.ServiceUnit
	const timer unitName = turns.TimerUnit
	const binary unitName = turns.HostBinary
	if service != "turns.service" || timer != "turns.timer" || binary != "/usr/local/bin/turns" {
		t.Fatalf("unit constants: %q %q %q", service, timer, binary)
	}
	// R-JTX6-4UGS
	entries, err := fs.ReadDir(units, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("root entries: %v", entries)
	}
	for i, want := range []string{turns.ServiceUnit, turns.TimerUnit} {
		info, statErr := entries[i].Info()
		if statErr != nil {
			t.Fatal(statErr)
		}
		if entries[i].Name() != want || !info.Mode().IsRegular() {
			t.Fatalf("entry %d: %v", i, info)
		}
	}
	// R-JV52-IM7H
	data, err := fs.ReadFile(units, turns.ServiceUnit)
	if err != nil {
		t.Fatal(err)
	}
	var execLines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			execLines = append(execLines, line)
		}
	}
	if len(execLines) != 1 || execLines[0] != "ExecStart=/usr/local/bin/turns push" {
		t.Fatalf("ExecStart lines: %q", execLines)
	}
}

func TestPublicShapes(t *testing.T) {
	// Conversions use the exact public shapes without inspecting implementation.
	// R-K00O-1P69
	cmd := seam.Cmd{Path: "tool", Args: []string{"arg"}, Dir: "dir", Env: []string{"NAME=value"}}
	shape := struct {
		Path string
		Args []string
		Dir  string
		Env  []string
	}(cmd)
	if shape.Path != "tool" || shape.Args[0] != "arg" || shape.Dir != "dir" || shape.Env[0] != "NAME=value" {
		t.Fatalf("command: %+v", shape)
	}
	// R-K18K-FGWY
	result := struct {
		Stdout   []byte
		Stderr   []byte
		ExitCode int
	}(seam.Result{Stdout: []byte("out"), Stderr: []byte("err"), ExitCode: 7})
	if string(result.Stdout) != "out" || string(result.Stderr) != "err" || result.ExitCode != 7 {
		t.Fatalf("result: %+v", result)
	}
	// R-K2GG-T8NN R-K3OD-70EC
	var runner seam.Runner = seam.Exec
	var function func(context.Context, seam.Cmd) (seam.Result, error) = runner
	if _, err := function(context.Background(), seam.Cmd{}); err == nil {
		t.Fatal("empty command accepted")
	}
	// R-KDFK-96BW
	now := time.Date(2030, 2, 3, 4, 5, 6, 0, time.UTC)
	httpClient := &http.Client{}
	deps := struct {
		EUID     int
		Getenv   func(string) string
		Dir      string
		Arg0     string
		Etc      string
		Now      func() time.Time
		Hostname func() (string, error)
		HTTP     *http.Client
		Exec     seam.Runner
	}(seam.Deps{
		EUID: 123, Getenv: func(key string) string { return "env-" + key }, Dir: "directory", Arg0: "invoked", Etc: "system",
		Now: func() time.Time { return now }, Hostname: func() (string, error) { return "host", nil }, HTTP: httpClient, Exec: runner,
	})
	host, err := deps.Hostname()
	if err != nil || host != "host" || deps.EUID != 123 || deps.Getenv("key") != "env-key" || deps.Dir != "directory" || deps.Arg0 != "invoked" || deps.Etc != "system" || deps.Now() != now || deps.HTTP != httpClient {
		t.Fatalf("dependencies: %+v, host %q, error %v", deps, host, err)
	}
	if _, err := deps.Exec(context.Background(), seam.Cmd{}); err == nil {
		t.Fatal("Deps.Exec did not invoke runner")
	}
}

func TestProcess(t *testing.T) {
	t.Chdir(t.TempDir())
	// R-ULWI-KYB6
	type processSignature func() seam.Deps
	var process processSignature = seam.Process
	deps := process()
	// R-UN4E-YQ1V
	if deps.EUID != os.Geteuid() {
		t.Fatalf("uid: %d", deps.EUID)
	}
	// R-UOCB-CHSK
	for _, value := range []string{"before", "after", ""} {
		t.Setenv("TURNS_SEAM_ENV", value)
		if deps.Getenv("TURNS_SEAM_ENV") != os.Getenv("TURNS_SEAM_ENV") {
			t.Fatalf("environment did not track %q", value)
		}
	}
	// R-UPK7-Q9J9
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if deps.Dir != dir {
		t.Fatalf("working directory: %q, want %q", deps.Dir, dir)
	}
	// R-UQS4-419Y
	if deps.Arg0 != os.Args[0] {
		t.Fatalf("argument zero: %q", deps.Arg0)
	}
	// R-UT7W-VKRC
	if deps.Etc != "/etc" {
		t.Fatalf("system root: %q", deps.Etc)
	}
	// R-UUFT-9CI1
	if deps.Now != nil || deps.Hostname != nil || deps.HTTP != nil || deps.Exec != nil {
		t.Fatal("optional dependency is non-nil")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Fatal("removed working directory unexpectedly resolves")
	}
	if got := seam.Process().Dir; got != "" {
		t.Fatalf("unreadable working directory: %q", got)
	}
}

func TestQuoteOutput(t *testing.T) {
	// R-KLYU-XKIR R-UKOM-76KH
	type quoteSignature func(string) string
	var quote quoteSignature = seam.QuoteOutput
	for _, test := range []struct{ input, want string }{
		{"", ""}, {"\n", ""}, {"\n\n", ""}, {" a \r", ">  a \r"}, {"a\r\n", "> a\r"},
		{"a\n\nb\n\n", "> a\n> \n> b"}, {"\n leading \x00\xff\n", "> \n>  leading \x00\xff"},
	} {
		if got := quote(test.input); got != test.want {
			t.Errorf("quote %q: got %q, want %q", test.input, got, test.want)
		}
	}
}

type helperReport struct {
	Args  []string
	Dir   string
	Value string
	Input string
}

// TestSeamProcessHelper is the controlled external program used only by Exec tests.
func TestSeamProcessHelper(t *testing.T) {
	mode := os.Getenv("TURNS_SEAM_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "report":
		index := 0
		for i, arg := range os.Args {
			if arg == "--" {
				index = i + 1
				break
			}
		}
		dir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(helperReport{os.Args[index:], dir, os.Getenv("TURNS_SEAM_VALUE"), string(input)}); err != nil {
			t.Fatal(err)
		}
	case "streams":
		if _, err := os.Stdout.Write(bytes.Repeat([]byte{0, 'a', '\n', 255}, 300_000)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stderr.Write(bytes.Repeat([]byte{255, '\r', 'b', 0}, 300_000)); err != nil {
			t.Fatal(err)
		}
	case "failure":
		os.Exit(27)
	case "signal":
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
	case "long":
		if _, err := io.WriteString(os.Stdout, "running\n"); err != nil {
			t.Fatal(err)
		}
		<-time.NewTimer(time.Hour).C
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
	os.Exit(0)
}

func helperCmd(t *testing.T, mode string) seam.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return seam.Cmd{Path: executable, Args: []string{"-test.run=^TestSeamProcessHelper$", "--"}, Env: []string{"TURNS_SEAM_HELPER=" + mode}}
}

func report(t *testing.T, cmd seam.Cmd) helperReport {
	t.Helper()
	result, err := seam.Exec(context.Background(), cmd)
	if err != nil || result.ExitCode != 0 || len(result.Stderr) != 0 {
		t.Fatalf("run: result %+v, error %v", result, err)
	}
	var got helperReport
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestExecInvocation(t *testing.T) {
	// R-UEL4-ABV0
	cmd := helperCmd(t, "report")
	dir := t.TempDir()
	name := filepath.Join(dir, "seam-helper")
	if err := os.Symlink(cmd.Path, name); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	cmd.Path = "seam-helper"
	cmd.Env = append(cmd.Env, "PATH=/not/the/lookup/path")
	if got := report(t, cmd); got.Input != "" {
		t.Fatalf("input: %q", got.Input)
	}
	cmd.Path = name
	t.Setenv("PATH", "")
	// R-UFT0-O3LP
	wantArgs := []string{"one", "two words", "", "--flag", "'\"$\\"}
	cmd.Args = append(cmd.Args, wantArgs...)
	// R-UH0X-1VCE
	cmd.Dir = t.TempDir()
	// R-KAZR-HMUI
	t.Setenv("TURNS_SEAM_VALUE", "inherited")
	got := report(t, cmd)
	if got.Value != "inherited" {
		t.Fatalf("inherited environment: %q", got.Value)
	}
	cmd.Env = append(cmd.Env, "TURNS_SEAM_VALUE=first", "TURNS_SEAM_VALUE=last")
	got = report(t, cmd)
	if got.Value != "last" || got.Dir != cmd.Dir || len(got.Args) != len(wantArgs) {
		t.Fatalf("invocation: %+v", got)
	}
	for i, want := range wantArgs {
		if got.Args[i] != want {
			t.Fatalf("argument %d: got %q, want %q", i, got.Args[i], want)
		}
	}
	// R-K9RV-3V3T
	t.Chdir(t.TempDir())
	wantDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = ""
	if got := report(t, cmd); got.Dir != wantDir {
		t.Fatalf("inherited working directory: %q, want %q", got.Dir, wantDir)
	}
}

func TestExecInputAndStreams(t *testing.T) {
	// R-3RRX-D2Z9
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := read.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := write.Write([]byte("parent input must remain unread")); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = previous })
	if got := report(t, helperCmd(t, "report")); got.Input != "" {
		t.Fatalf("child read parent input: %q", got.Input)
	}
	remaining, err := io.ReadAll(read)
	if err != nil || string(remaining) != "parent input must remain unread" {
		t.Fatalf("parent input remaining %q, error %v", remaining, err)
	}
	// R-SHPU-QWD6
	result, err := seam.Exec(context.Background(), helperCmd(t, "streams"))
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("streams exit %d, error %v", result.ExitCode, err)
	}
	if !bytes.Equal(result.Stdout, bytes.Repeat([]byte{0, 'a', '\n', 255}, 300_000)) || !bytes.Equal(result.Stderr, bytes.Repeat([]byte{255, '\r', 'b', 0}, 300_000)) {
		t.Fatalf("captured streams differ: stdout %d, stderr %d", len(result.Stdout), len(result.Stderr))
	}
}

func TestExecExitAndFailures(t *testing.T) {
	// R-3O48-7RR6 R-K645-YJVQ
	for _, cmd := range []seam.Cmd{{}, {Path: filepath.Join(t.TempDir(), "missing")}, {Path: t.TempDir()}} {
		if _, err := seam.Exec(context.Background(), cmd); err == nil {
			t.Fatalf("unstartable command succeeded: %+v", cmd)
		}
	}
	if result, err := seam.Exec(context.Background(), helperCmd(t, "report")); err != nil || result.ExitCode != 0 {
		t.Fatalf("successful process: %+v, %v", result, err)
	}
	// R-3PC4-LJHV
	if result, err := seam.Exec(context.Background(), helperCmd(t, "failure")); err != nil || result.ExitCode != 27 {
		t.Fatalf("nonzero exit: %+v, %v", result, err)
	}
	// R-3QK0-ZB8K
	if result, err := seam.Exec(context.Background(), helperCmd(t, "signal")); err != nil || result.ExitCode != -1 {
		t.Fatalf("signal exit: %+v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := seam.Exec(ctx, helperCmd(t, "report")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
	// R-SGHY-D4MH
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	result, err := seam.Exec(ctx, helperCmd(t, "long"))
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) || elapsed > 5*time.Second || string(result.Stdout) != "running\n" {
		t.Fatalf("deadline: error %v, duration %v, stdout %q (helper runs an hour)", err, elapsed, result.Stdout)
	}
}
