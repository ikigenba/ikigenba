package host_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestBoundaryFields(t *testing.T) {
	// R-Y5EK-K08K R-Y6MG-XRZ9 R-Y7UD-BJPY R-YBI2-GUY1
	stdin := strings.NewReader("input")
	command := host.Command{Name: "tool", Args: []string{"a"}, Dir: "/dir", Env: []string{"K=V"}, Stdin: stdin}
	name := typed[string](command.Name)
	args := typed[[]string](command.Args)
	dir := typed[string](command.Dir)
	cmdEnv := typed[[]string](command.Env)
	cmdStdin := *typed[*io.Reader](&command.Stdin)
	if name != "tool" || len(args) != 1 || args[0] != "a" || dir != "/dir" || len(cmdEnv) != 1 || cmdEnv[0] != "K=V" || cmdStdin != stdin {
		t.Errorf("Command = %+v", command)
	}

	result := host.Result{Stdout: []byte("out"), Stderr: []byte("err"), ExitCode: 2}
	stdout := typed[[]byte](result.Stdout)
	stderr := typed[[]byte](result.Stderr)
	exitCode := typed[int](result.ExitCode)
	if string(stdout) != "out" || string(stderr) != "err" || exitCode != 2 {
		t.Errorf("Result = %+v", result)
	}

	env := host.Env{
		Root:    "/root",
		Getenv:  func(key string) string { return key },
		Execute: func(context.Context, host.Command) (host.Result, error) { return result, nil },
		Now:     func() time.Time { return time.Unix(5, 0) },
	}
	root := typed[string](env.Root)
	getenv := typed[func(string) string](env.Getenv)
	execute := typed[func(context.Context, host.Command) (host.Result, error)](env.Execute)
	now := typed[func() time.Time](env.Now)
	if got, err := execute(context.Background(), command); root != "/root" || getenv("K") != "K" || err != nil || got.ExitCode != 2 || !now().Equal(time.Unix(5, 0)) {
		t.Errorf("Env = %+v, Execute = (%+v, %v)", env, got, err)
	}

	cause := errors.New("cause")
	commandErr := host.CommandError{Label: "label", Result: result, Err: cause}
	label := typed[string](commandErr.Label)
	errResult := typed[host.Result](commandErr.Result)
	errErr := typed[error](commandErr.Err)
	if label != "label" || errResult.ExitCode != 2 || !errors.Is(errErr, cause) {
		t.Errorf("CommandError = %+v", commandErr)
	}
}

func TestCommandError(t *testing.T) {
	// R-GXUB-4BTA R-GZ27-I3JZ
	cause := errors.New("cannot start")
	for _, c := range []struct {
		err  *host.CommandError
		want string
	}{{&host.CommandError{Label: "apply", Err: cause}, "apply: cannot start"}, {&host.CommandError{Label: "apply", Result: host.Result{ExitCode: 23}}, "apply: exit status 23"}} {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error = %q, want %q", got, c.want)
		}
		if got := c.err.Unwrap(); !errors.Is(got, c.err.Err) {
			t.Errorf("Unwrap = %v, want %v", got, c.err.Err)
		}
	}
	if !errors.Is(&host.CommandError{Err: cause}, cause) {
		t.Fatal("CommandError does not preserve cause")
	}
}

func TestApex(t *testing.T) {
	// R-DGJQ-6ZMX R-DHRM-KRDM R-O0MO-ORKU
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "sbx.ikigenba.dev", want: "ikigenba.dev"},
		{name: "a.b.c.d", want: "b.c.d"},
		{name: "SBX.Ikigenba.dev", want: "Ikigenba.dev"},
	} {
		got, err := host.Apex(test.name)
		if err != nil || got != test.want {
			t.Errorf("Apex(%q) = %q, %v; want %q, nil", test.name, got, err, test.want)
		}
	}

	for _, name := range []string{"", "localhost", "ikigenba.dev", ".a.b", "a..b", "a.b."} {
		got, err := host.Apex(name)
		wantErr := "host.apex is set but host.name '" + name + "' has no parent domain"
		if got != "" || err == nil || err.Error() != wantErr {
			t.Errorf("Apex(%q) = %q, %v; want empty result and %q", name, got, err, wantErr)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	// R-NWYZ-JGCR R-NY6V-X83G
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "SBX.Ikigenba.dev.", want: "sbx.ikigenba.dev"},
		{name: "a.b..", want: "a.b."},
		{name: "", want: ""},
		{name: " A.B. ", want: " a.b. "},
		{name: ".A..B", want: ".a..b"},
		{name: "\u00c4.\u0130.\uff21.", want: "\u00c4.\u0130.\uff21"},
	} {
		if got := host.NormalizeName(test.name); got != test.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestExecDirectProcess(t *testing.T) {
	// R-5LFH-HU7S
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("OPSCTL_HOST_OVERLAY", "inherited")
	t.Setenv("OPSCTL_HOST_INHERITED", "kept")
	literal := "$(touch forbidden); * ' \" $HOME"
	result, err := host.Exec(context.Background(), host.Command{Name: executable, Args: []string{"-test.run=^TestHostChild$", "--", literal}, Dir: dir, Env: []string{"OPSCTL_HOST_CHILD=1", "OPSCTL_HOST_OVERLAY=first", "OPSCTL_HOST_OVERLAY=last"}, Stdin: strings.NewReader("input\x00\r\n")})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(literal + "\x00last\x00kept\x00" + dir + "\x00input\x00\r\n")
	if result.ExitCode != 23 || !bytes.Equal(result.Stdout, want) || !bytes.Equal(result.Stderr, []byte("diagnostic\x00\r\n")) {
		t.Fatalf("result = %#v, want status 23, stdout %q and separate diagnostic", result, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(err) {
		t.Fatalf("shell expression executed: %v", err)
	}
}

func TestExecCompletionAndStartErrors(t *testing.T) {
	// R-5LFH-HU7S
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.Exec(context.Background(), host.Command{Name: executable, Args: []string{"-test.run=^TestHostChild$"}, Env: []string{"OPSCTL_HOST_CHILD=success"}})
	if err != nil || result.ExitCode != 0 || string(result.Stdout) != "ok" || len(result.Stderr) != 0 {
		t.Fatalf("successful result = %#v, error %v", result, err)
	}
	for _, command := range []host.Command{{Name: filepath.Join(t.TempDir(), "missing")}, {Name: executable, Dir: filepath.Join(t.TempDir(), "missing")}} {
		if _, err := host.Exec(context.Background(), command); err == nil {
			t.Fatal("unstartable command returned nil error")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := host.Exec(ctx, host.Command{Name: executable}); err == nil {
		t.Fatal("cancelled command returned nil error")
	}
}

func TestHostChild(_ *testing.T) {
	switch os.Getenv("OPSCTL_HOST_CHILD") {
	case "success":
		if _, err := os.Stdout.WriteString("ok"); err != nil {
			os.Exit(99)
		}
		os.Exit(0)
	case "1":
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(99)
		}
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(99)
		}
		output := os.Args[len(os.Args)-1] + "\x00" + os.Getenv("OPSCTL_HOST_OVERLAY") + "\x00" + os.Getenv("OPSCTL_HOST_INHERITED") + "\x00" + dir + "\x00" + string(input)
		if _, err := os.Stdout.WriteString(output); err != nil {
			os.Exit(99)
		}
		if _, err := os.Stderr.WriteString("diagnostic\x00\r\n"); err != nil {
			os.Exit(99)
		}
		os.Exit(23)
	}
}

// typed returns v as a T; the call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }
