package deploy_test

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

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
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

type usageErrorContract interface {
	Error() string
	Detail() string
	ExitCode() int
}

type exitErrorContract interface {
	Error() string
	ExitCode() int
}

type detailedExitErrorContract interface {
	Error() string
	Detail() string
	ExitCode() int
}

var (
	_ usageErrorContract        = (*deploy.UsageError)(nil)
	_ exitErrorContract         = (*deploy.NoFileError)(nil)
	_ detailedExitErrorContract = (*deploy.MissingSecretsError)(nil)
	_ detailedExitErrorContract = (*deploy.ProcessError)(nil)
	_ exitErrorContract         = (*deploy.FileError)(nil)
)

type fieldSpec struct {
	name   string
	typeOf reflect.Type
}

func TestRunSignatureAndErrorContracts(t *testing.T) {
	// R-YOOC-0709 R-YR44-RQHN R-YSC1-5I8C R-YVZQ-ATGF R-F81U-8G73 R-FWFT-VV0Z
	stringType := reflect.TypeFor[string]()
	intType := reflect.TypeFor[int]()
	stringsType := reflect.TypeFor[[]string]()
	assertFields(t, deploy.UsageError{}, []fieldSpec{{"Message", stringType}, {"Help", stringType}})
	assertFields(t, deploy.NoFileError{}, []fieldSpec{{"Path", stringType}})
	assertFields(t, deploy.MissingSecretsError{}, []fieldSpec{
		{"App", stringType}, {"Domain", stringType}, {"Profile", stringType}, {"Names", stringsType},
	})
	assertFields(t, deploy.ProcessError{}, []fieldSpec{
		{"Label", stringType}, {"Status", intType}, {"Stderr", stringType},
	})
	assertFields(t, deploy.FileError{}, []fieldSpec{{"Path", stringType}, {"Reason", stringType}})

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
	var stdout bytes.Buffer
	err = deploy.Run(context.Background(), []string{"foo.example", "notes.tar.xz"}, &stdout, deps, "account")
	var fileErr *deploy.FileError
	if !errors.As(err, &fileErr) || fileErr.Path != "notes.tar.xz" || fileErr.Reason != "name is not <app>-v<semver>.tar.xz" {
		t.Fatalf("existing invalid-name error = %T %#v, want exact FileError", err, err)
	}
	if stdout.Len() != 0 || cloudCalls != 0 || execCalls != 0 {
		t.Fatalf("stdout/external = %q, cloud %d, exec %d", stdout.String(), cloudCalls, execCalls)
	}
}

func TestRelativeAndAbsoluteMissingFilesPreserveOperand(t *testing.T) {
	// R-YSC1-5I8C R-08GB-YDTQ
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

func TestFileResolutionArchiveCommandsAndFileStep(t *testing.T) {
	// R-08GB-YDTQ R-Z9EM-IAM2 R-ZFI4-F5BJ R-FE5C-5AWK
	dir := t.TempDir()
	relative := "crm-v1.2.3.tar.xz"
	absolute := filepath.Join(t.TempDir(), "crm-v2.3.4.tar.xz")
	for _, test := range []struct {
		name    string
		operand string
		version string
	}{
		{name: "relative", operand: relative, version: "v1.2.3"},
		{name: "absolute", operand: absolute, version: "v2.3.4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := test.operand
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
				t.Fatal(err)
			}

			var commands []seam.Cmd
			deps := seam.Deps{Dir: dir, Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, command)
				if len(commands) == 1 {
					return seam.Result{Stdout: []byte("\netc/manifest.toml\n\nbin/crm\n")}, nil
				}
				return seam.Result{Stdout: []byte("app = \"crm\"\n")}, nil
			}}
			var stdout bytes.Buffer
			if err := deploy.Run(context.Background(), []string{"foo.example", test.operand}, &stdout, deps, "account"); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			wantCommands := []seam.Cmd{
				{Path: "tar", Args: []string{"-t", "-J", "-f", test.operand}, Dir: dir},
				{Path: "tar", Args: []string{"-x", "-J", "-O", "-f", test.operand, "etc/manifest.toml"}, Dir: dir},
			}
			if !reflect.DeepEqual(commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
			}
			if want := "file: ok (crm " + test.version + ")\n"; stdout.String() != want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
		})
	}
}

func TestNonRegularFileStopsBeforeExternalAccess(t *testing.T) {
	// R-08GB-YDTQ
	dir := t.TempDir()
	operand := "crm-v1.2.3.tar.xz"
	if err := os.Mkdir(filepath.Join(dir, operand), 0o700); err != nil {
		t.Fatal(err)
	}
	cloudCalls, execCalls := 0, 0
	deps := recordingDeps(t, &cloudCalls, &execCalls)
	deps.Dir = dir

	err := deploy.Run(context.Background(), []string{"foo.example", operand}, io.Discard, deps, "account")
	var noFile *deploy.NoFileError
	if !errors.As(err, &noFile) || noFile.Path != operand || cloudCalls != 0 || execCalls != 0 {
		t.Fatalf("Run() = %T %#v, cloud %d, exec %d", err, err, cloudCalls, execCalls)
	}
}

func TestArchiveValidationErrorsStopBeforeCloud(t *testing.T) {
	// R-ZAMI-W2CR R-FE5C-5AWK
	tests := []struct {
		name          string
		members       string
		manifest      string
		wantReason    string
		wantExecCalls int
	}{
		{name: "missing manifest", members: "bin/crm\n", wantReason: "no etc/manifest.toml in the archive", wantExecCalls: 1},
		{name: "invalid manifest", members: "etc/manifest.toml\nbin/crm\n", manifest: "app = [", wantReason: "etc/manifest.toml: toml: line 1", wantExecCalls: 2},
		{name: "app mismatch", members: "etc/manifest.toml\nbin/gmail\n", manifest: "app = \"gmail\"\n", wantReason: "manifest app does not match file name", wantExecCalls: 2},
		{name: "missing binary", members: "etc/manifest.toml\nbin/other\n", manifest: "app = \"crm\"\n", wantReason: "no bin/crm in the archive", wantExecCalls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "invalid manifest" {
				_, decodeErr := checkout.DecodeManifest(strings.NewReader(test.manifest))
				test.wantReason = checkout.ManifestFile + ": " + decodeErr.Error()
			}
			dir := t.TempDir()
			operand := "crm-v1.2.3.tar.xz"
			if err := os.WriteFile(filepath.Join(dir, operand), []byte("artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			cloudCalls, execCalls := 0, 0
			deps := seam.Deps{
				Dir: dir,
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, errors.New("unexpected cloud call")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					execCalls++
					if execCalls == 1 {
						return seam.Result{Stdout: []byte(test.members)}, nil
					}
					return seam.Result{Stdout: []byte(test.manifest)}, nil
				},
			}

			err := deploy.Run(context.Background(), []string{"foo.example", operand}, io.Discard, deps, "account")
			var fileErr *deploy.FileError
			if !errors.As(err, &fileErr) || fileErr.Path != operand || fileErr.Reason != test.wantReason {
				t.Fatalf("Run() error = %T %#v", err, err)
			}
			if cloudCalls != 0 || execCalls != test.wantExecCalls {
				t.Fatalf("external calls = cloud %d, exec %d", cloudCalls, execCalls)
			}
		})
	}
}

func TestArchiveProcessFailures(t *testing.T) {
	// R-ZEA8-1DKU
	startErr := errors.New("process start failed")
	tests := []struct {
		name        string
		failCall    int
		result      seam.Result
		err         error
		wantLabel   string
		wantProcess bool
	}{
		{name: "list exit", failCall: 1, result: seam.Result{ExitCode: 7, Stderr: []byte("bad list\n")}, wantLabel: "tar -t -J -f crm-v1.2.3.tar.xz", wantProcess: true},
		{name: "extract exit", failCall: 2, result: seam.Result{ExitCode: 8, Stderr: []byte("bad extract\n")}, wantLabel: "tar -x -J -O -f crm-v1.2.3.tar.xz etc/manifest.toml", wantProcess: true},
		{name: "list start", failCall: 1, err: startErr},
		{name: "extract start", failCall: 2, err: startErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			operand := "crm-v1.2.3.tar.xz"
			if err := os.WriteFile(filepath.Join(dir, operand), []byte("artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			calls, cloudCalls := 0, 0
			deps := seam.Deps{
				Dir: dir,
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, errors.New("unexpected cloud call")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					calls++
					if calls == test.failCall {
						return test.result, test.err
					}
					return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/crm\n")}, nil
				},
			}

			err := deploy.Run(context.Background(), []string{"foo.example", operand}, io.Discard, deps, "account")
			var processErr *deploy.ProcessError
			if test.wantProcess {
				if !errors.As(err, &processErr) || processErr.Label != test.wantLabel || processErr.Status != test.result.ExitCode || processErr.Stderr != string(test.result.Stderr) {
					t.Fatalf("Run() error = %T %#v", err, err)
				}
			} else if errors.As(err, &processErr) || !errors.Is(err, startErr) || !strings.Contains(err.Error(), "tar") {
				t.Fatalf("Run() start error = %T %#v", err, err)
			}
			if calls != test.failCall || cloudCalls != 0 {
				t.Fatalf("external calls = exec %d, cloud %d", calls, cloudCalls)
			}
		})
	}
}

func TestObjectKey(t *testing.T) {
	// R-FFD8-J2N9
	if got, want := deploy.ObjectKey("foo.example", "crm-v1.2.3.tar.xz"), "foo.example/deploy/crm-v1.2.3.tar.xz"; got != want {
		t.Fatalf("ObjectKey() = %q, want %q", got, want)
	}
}

func assertFields(t *testing.T, value any, want []fieldSpec) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	got := make([]fieldSpec, typeOf.NumField())
	for index := range typeOf.NumField() {
		field := typeOf.Field(index)
		got[index] = fieldSpec{name: field.Name, typeOf: field.Type}
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
