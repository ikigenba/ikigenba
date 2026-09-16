package host_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestBoundaryFields(t *testing.T) {
	// R-5GJV-YR90 R-5HRS-CIZP R-5IZO-QAQE
	cases := []struct {
		typ    reflect.Type
		fields map[string]reflect.Type
	}{
		{reflect.TypeFor[host.Env](), map[string]reflect.Type{"Root": reflect.TypeFor[string](), "Getenv": reflect.TypeFor[func(string) string](), "Execute": reflect.TypeFor[func(context.Context, host.Command) (host.Result, error)](), "Now": reflect.TypeFor[func() time.Time]()}},
		{reflect.TypeFor[host.Command](), map[string]reflect.Type{"Name": reflect.TypeFor[string](), "Args": reflect.TypeFor[[]string](), "Dir": reflect.TypeFor[string](), "Env": reflect.TypeFor[[]string](), "Stdin": reflect.TypeFor[io.Reader]()}},
		{reflect.TypeFor[host.Result](), map[string]reflect.Type{"Stdout": reflect.TypeFor[[]byte](), "Stderr": reflect.TypeFor[[]byte](), "ExitCode": reflect.TypeFor[int]()}},
		{reflect.TypeFor[host.CommandError](), map[string]reflect.Type{"Label": reflect.TypeFor[string](), "Result": reflect.TypeFor[host.Result](), "Err": reflect.TypeFor[error]()}},
	}
	for _, c := range cases {
		if c.typ.NumField() != len(c.fields) {
			t.Errorf("%s field count = %d, want %d", c.typ, c.typ.NumField(), len(c.fields))
		}
		for name, typ := range c.fields {
			f, ok := c.typ.FieldByName(name)
			if !ok || !f.IsExported() || f.Type != typ {
				t.Errorf("%s.%s = %v, want exported %v", c.typ, name, f.Type, typ)
			}
		}
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
