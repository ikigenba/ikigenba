package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/deploy"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantHelp = `Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
`

type runSignature func(context.Context, []string, io.Writer, seam.Deps, string) error

var _ runSignature = deploy.Run

func TestRunSignatureAndErrorContracts(t *testing.T) {
	// R-YOOC-0709 R-YR44-RQHN R-YSC1-5I8C R-YVZQ-ATGF R-F81U-8G73 R-FWFT-VV0Z
	assertFields(t, deploy.UsageError{}, []string{"Message", "Help"})
	assertFields(t, deploy.NoFileError{}, []string{"Path"})
	assertFields(t, deploy.MissingSecretsError{}, []string{"App", "Domain", "Profile", "Names"})
	assertFields(t, deploy.ProcessError{}, []string{"Label", "Status", "Stderr"})
	assertFields(t, deploy.FileError{}, []string{"Path", "Reason"})

	usage := &deploy.UsageError{Message: "bad", Help: "devctl deploy --help"}
	if usage.Error() != "bad" || usage.Detail() != "see 'devctl deploy --help' for usage" || usage.ExitCode() != 2 {
		t.Fatalf("UsageError methods = %q, %q, %d", usage.Error(), usage.Detail(), usage.ExitCode())
	}
	if got := (&deploy.UsageError{Message: "bad"}).Detail(); got != "" {
		t.Fatalf("empty-help Detail = %q", got)
	}
	noFile := &deploy.NoFileError{Path: "missing.tar.xz"}
	if noFile.Error() != "no such file 'missing.tar.xz'" || noFile.ExitCode() != 2 {
		t.Fatalf("NoFileError methods = %q, %d", noFile.Error(), noFile.ExitCode())
	}
	missing := &deploy.MissingSecretsError{App: "crm", Domain: "foo.example", Profile: "Prod", Names: []string{"A", "B"}}
	if missing.Error() != "crm: secrets missing A,B" || missing.Detail() != "run 'devctl --account Prod secrets push foo.example crm'" || missing.ExitCode() != 2 {
		t.Fatalf("MissingSecretsError methods = %q, %q, %d", missing.Error(), missing.Detail(), missing.ExitCode())
	}
	process := &deploy.ProcessError{Label: "tar -t", Status: 9, Stderr: "first\nsecond\n"}
	if process.Error() != "tar -t: exit status 9" || process.Detail() != "> first\n> second" || process.ExitCode() != 1 {
		t.Fatalf("ProcessError methods = %q, %q, %d", process.Error(), process.Detail(), process.ExitCode())
	}
	file := &deploy.FileError{Path: "notes.tar.xz", Reason: "name is not <app>-v<semver>.tar.xz"}
	if file.Error() != "'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz" || file.ExitCode() != 2 {
		t.Fatalf("FileError methods = %q, %d", file.Error(), file.ExitCode())
	}
}

func TestHelpAnywhereStopsBeforeExternalAccess(t *testing.T) {
	// R-078F-KM31 R-F99Q-M7XS
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"foo.sbx.ikigenba.dev", "crm/dist/crm-v0.1.0.tar.xz", "--help"},
		{"--unknown", "-h"},
	} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			var stdout bytes.Buffer
			cloudCalls, execCalls := 0, 0
			deps := seam.Deps{
				Dir: t.TempDir(),
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, errors.New("unexpected cloud call")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					execCalls++
					return seam.Result{}, errors.New("unexpected exec call")
				},
			}
			if err := deploy.Run(context.Background(), args, &stdout, deps, "MixedCase"); err != nil {
				t.Fatalf("Run(%q): %v", args, err)
			}
			if stdout.String() != wantHelp {
				t.Fatalf("stdout = %q, want %q", stdout.String(), wantHelp)
			}
			if cloudCalls != 0 || execCalls != 0 {
				t.Fatalf("external calls = cloud %d, exec %d", cloudCalls, execCalls)
			}
		})
	}
}

func TestOperandAndOptionRefusals(t *testing.T) {
	// R-Z0VB-TWF7 R-V2NL-65SU R-Z3B4-LFWL
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{name: "none", message: "deploy needs <domain> and <file>"},
		{name: "one", args: []string{"foo.example"}, message: "deploy needs <domain> and <file>"},
		{name: "extra", args: []string{"foo.example", "app-v1.0.0.tar.xz", "extra"}, message: "deploy takes only <domain> and <file>"},
		{name: "unknown first", args: []string{"--wat", "foo.example", "app-v1.0.0.tar.xz"}, message: "unknown option '--wat'"},
		{name: "unknown later", args: []string{"foo.example", "-x"}, message: "unknown option '-x'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			cloudCalls, execCalls := 0, 0
			deps := recordingDeps(t, &cloudCalls, &execCalls)
			err := deploy.Run(context.Background(), test.args, &stdout, deps, "account-name")
			var usage *deploy.UsageError
			if !errors.As(err, &usage) {
				t.Fatalf("error = %T %v, want *UsageError", err, err)
			}
			if usage.Message != test.message || usage.Help != "devctl deploy --help" || usage.Detail() != "see 'devctl deploy --help' for usage" || usage.ExitCode() != 2 {
				t.Fatalf("UsageError = %#v, detail %q, code %d", usage, usage.Detail(), usage.ExitCode())
			}
			if stdout.Len() != 0 || cloudCalls != 0 || execCalls != 0 {
				t.Fatalf("stdout/external = %q, cloud %d, exec %d", stdout.String(), cloudCalls, execCalls)
			}
		})
	}
}

func TestFileExistencePrecedesNameParsingAndExternalAccess(t *testing.T) {
	// R-FBPJ-DRF6
	dir := t.TempDir()
	cloudCalls, execCalls := 0, 0
	deps := recordingDeps(t, &cloudCalls, &execCalls)
	deps.Dir = dir

	err := deploy.Run(context.Background(), []string{"foo.example", "notes.tar.xz"}, io.Discard, deps, "account")
	var noFile *deploy.NoFileError
	if !errors.As(err, &noFile) || noFile.Path != "notes.tar.xz" {
		t.Fatalf("missing invalid-name error = %T %#v, want NoFileError with operand", err, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "notes.tar.xz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = deploy.Run(context.Background(), []string{"foo.example", "notes.tar.xz"}, io.Discard, deps, "account")
	var fileErr *deploy.FileError
	if !errors.As(err, &fileErr) || fileErr.Path != "notes.tar.xz" || fileErr.Reason != "name is not <app>-v<semver>.tar.xz" {
		t.Fatalf("existing invalid-name error = %T %#v, want exact FileError", err, err)
	}
	if cloudCalls != 0 || execCalls != 0 {
		t.Fatalf("external calls = cloud %d, exec %d", cloudCalls, execCalls)
	}
}

func TestRelativeAndAbsoluteMissingFilesPreserveOperand(t *testing.T) {
	// R-YSC1-5I8C
	dir := t.TempDir()
	for _, operand := range []string{"crm-v1.0.0.tar.xz", filepath.Join(dir, "abs-v1.0.0.tar.xz")} {
		cloudCalls, execCalls := 0, 0
		deps := recordingDeps(t, &cloudCalls, &execCalls)
		deps.Dir = dir
		err := deploy.Run(context.Background(), []string{"foo.example", operand}, io.Discard, deps, "account")
		var noFile *deploy.NoFileError
		if !errors.As(err, &noFile) || noFile.Path != operand || noFile.Error() != "no such file '"+operand+"'" {
			t.Fatalf("Run(%q) error = %T %#v", operand, err, err)
		}
		if cloudCalls != 0 || execCalls != 0 {
			t.Fatalf("external calls = cloud %d, exec %d", cloudCalls, execCalls)
		}
	}
}

func assertFields(t *testing.T, value any, want []string) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	got := make([]string, typeOf.NumField())
	for i := range typeOf.NumField() {
		got[i] = typeOf.Field(i).Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s fields = %v, want %v", typeOf.Name(), got, want)
	}
}

func recordingDeps(t *testing.T, cloudCalls, execCalls *int) seam.Deps {
	t.Helper()
	return seam.Deps{
		Dir: t.TempDir(),
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			*cloudCalls++
			return cloud.Clients{}, errors.New("unexpected cloud call")
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			*execCalls++
			return seam.Result{}, errors.New("unexpected exec call")
		},
	}
}
