package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantUsage = `Usage: devctl build <app>

Build <app> for linux/amd64 and write <app>/dist/<app>-<version>.tar.xz, the
file deploy copies to a host and opsctl installs. HEAD must be a commit that
the app's version tag (<app>/v<semver>) points at, with no uncommitted
changes.
`

func TestRunPublicSignature(t *testing.T) {
	// R-63VS-7J2D
	want := reflect.TypeFor[func(context.Context, []string, io.Writer, seam.Deps) error]()
	if got := reflect.TypeOf(build.Run); got != want {
		t.Fatalf("Run type = %s, want %s", got, want)
	}
}

func TestRunHelpAnywhereHasNoExternalOperation(t *testing.T) {
	// R-6DMZ-9OZX
	// R-GSOJ-R4YD
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"crm", "--help"},
		{"--unknown", "crm", "-h"},
	} {
		t.Run(stringsForName(args), func(t *testing.T) {
			execCalls := 0
			deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				execCalls++
				return seam.Result{}, errors.New("unexpected execution")
			}}
			var stdout bytes.Buffer
			if err := build.Run(context.Background(), args, &stdout, deps); err != nil {
				t.Fatalf("Run(%q) error = %v", args, err)
			}
			if got := stdout.String(); got != wantUsage {
				t.Fatalf("stdout = %q, want %q", got, wantUsage)
			}
			if execCalls != 0 {
				t.Fatalf("Exec calls = %d, want 0", execCalls)
			}
		})
	}
}

func TestRunArgumentParsing(t *testing.T) {
	// R-6EUV-NGQM
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{name: "missing", message: "build needs <app>"},
		{name: "extra", args: []string{"crm", "api"}, message: "build takes one <app>"},
		{name: "long option", args: []string{"--force", "crm"}, message: "unknown option '--force'"},
		{name: "short option", args: []string{"crm", "-v"}, message: "unknown option '-v'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := build.Run(context.Background(), test.args, &stdout, seam.Deps{})
			var usageError *build.UsageError
			if !errors.As(err, &usageError) {
				t.Fatalf("Run(%q) error = %T %v, want *UsageError", test.args, err, err)
			}
			if usageError.Message != test.message || usageError.Help != "devctl build --help" {
				t.Fatalf("Run(%q) error = %#v", test.args, usageError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("Run(%q) stdout = %q, want empty", test.args, stdout.String())
			}
		})
	}

	root := t.TempDir()
	err := build.Run(context.Background(), []string{"crm"}, io.Discard, seam.Deps{
		Dir: root,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
	})
	var noAppError *checkout.NoAppError
	if !errors.As(err, &noAppError) {
		t.Fatalf("Run with one operand error = %T %v, want *checkout.NoAppError", err, err)
	}
}

func stringsForName(args []string) string {
	var name bytes.Buffer
	for _, arg := range args {
		name.WriteString(arg)
		name.WriteByte('_')
	}
	return name.String()
}
