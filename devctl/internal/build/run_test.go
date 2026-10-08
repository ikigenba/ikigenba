package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantUsage = `Usage: devctl build <sha|tag>

Build the suite at <sha|tag> for linux/amd64 and write dist/<sha>.tar.xz, one
release holding every app and opsctl. <sha> is the full commit sha the argument
resolves to; the working tree is not read.
`

func TestRunPublicSignature(_ *testing.T) {
	// R-F6V0-EZM8
	_ = []func(context.Context, []string, string, io.Writer, seam.Deps) error{build.Run}
}

func TestRunHelpAnywhereHasNoExternalOperation(t *testing.T) {
	// R-6DMZ-9OZX
	// R-R3A8-T6T1
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
			if err := build.Run(context.Background(), args, "test-version", &stdout, deps); err != nil {
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
	// R-QYEN-A3U9
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{name: "missing", message: "build needs <sha|tag>"},
		{name: "extra", args: []string{"crm", "api"}, message: "build takes one <sha|tag>"},
		{name: "long option", args: []string{"--force", "crm"}, message: "unknown option '--force'"},
		{name: "short option", args: []string{"crm", "-v"}, message: "unknown option '-v'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := build.Run(context.Background(), test.args, "test-version", &stdout, seam.Deps{})
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
}

func stringsForName(args []string) string {
	var name bytes.Buffer
	for _, arg := range args {
		name.WriteString(arg)
		name.WriteByte('_')
	}
	return name.String()
}
