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
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const deployHelp = `Usage: devctl deploy <space> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
space's deploy/ prefix in the bucket and have opsctl on the space install it
from there. The app and tag (v<semver>) are read from the file name.
`

type runSignature func(context.Context, []string, io.Writer, seam.Deps) error

var _ runSignature = deploy.Run

func TestDeployPublicContract(t *testing.T) {
	// R-O0KZ-IOYR R-YR44-RQHN R-YSC1-5I8C R-O1SV-WGPG R-F81U-8G73 R-FWFT-VV0Z R-OIVH-9936
	assertFields(t, deploy.UsageError{}, "Message", "Help")
	assertFields(t, deploy.NoFileError{}, "Path")
	assertFields(t, deploy.MissingSecretsError{}, "App", "Space", "Names")
	assertFields(t, deploy.ProcessError{}, "Label", "Status", "Stderr")
	assertFields(t, deploy.FileError{}, "Path", "Reason")
	assertFieldTypes(t, deploy.ProcessError{}, reflect.TypeFor[string](), reflect.TypeFor[int](), reflect.TypeFor[string]())
	assertFieldTypes(t, deploy.FileError{}, reflect.TypeFor[string](), reflect.TypeFor[string]())
	usage := &deploy.UsageError{Message: "bad", Help: "devctl deploy --help"}
	if usage.Error() != "bad" || usage.Detail() != "see 'devctl deploy --help' for usage" || usage.ExitCode() != 2 || (&deploy.UsageError{}).Detail() != "" {
		t.Fatalf("UsageError contract failed: %#v", usage)
	}
	missing := &deploy.MissingSecretsError{App: "crm", Space: "sbx1", Names: []string{"A", "B"}}
	if missing.Error() != "crm: secrets missing A,B" || missing.Detail() != "run 'devctl secrets push sbx1 crm'" || missing.ExitCode() != 2 {
		t.Fatalf("MissingSecretsError contract failed: %#v", missing)
	}
	process := &deploy.ProcessError{Label: "tar -t", Status: 9, Stderr: "first\nsecond\n"}
	if process.Error() != "tar -t: exit status 9" || process.Detail() != "> first\n> second" || process.ExitCode() != 1 {
		t.Fatalf("ProcessError contract failed: %#v", process)
	}
	file := &deploy.FileError{Path: "notes.tar.xz", Reason: "name is not <app>-v<semver>.tar.xz"}
	if file.Error() != "'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz" || file.ExitCode() != 2 {
		t.Fatalf("FileError contract failed: %#v", file)
	}
	if got := deploy.ObjectKey("sbx1", "crm-v0.1.0.tar.xz"); got != "sbx1/deploy/crm-v0.1.0.tar.xz" {
		t.Fatalf("ObjectKey = %q", got)
	}
}

func TestDeployResolvesAndValidatesFileBeforeParsing(t *testing.T) {
	// R-08GB-YDTQ R-FBPJ-DRF6 R-BU3V-ZE1N
	dir := t.TempDir()
	absoluteDir := t.TempDir()
	absolute := filepath.Join(absoluteDir, "crm-v0.1.0.tar.xz")
	for _, path := range []string{filepath.Join(dir, "crm-v0.1.0.tar.xz"), absolute} {
		if err := os.WriteFile(path, []byte("artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, operand := range []string{"crm-v0.1.0.tar.xz", absolute} {
		t.Run(operand, func(t *testing.T) {
			var commands []seam.Cmd
			err := deploy.Run(context.Background(), []string{"sbx1", operand}, io.Discard, seam.Deps{
				Dir: dir,
				Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
					commands = append(commands, command)
					return seam.Result{}, nil
				},
				Cloud: failCloud(t),
			})
			var fileErr *deploy.FileError
			want := seam.Cmd{Path: "tar", Args: []string{"-t", "-J", "-f", operand}, Dir: dir}
			if !errors.As(err, &fileErr) || fileErr.Path != operand || fileErr.Reason != "no "+checkout.ManifestFile+" in the archive" || !reflect.DeepEqual(commands, []seam.Cmd{want}) {
				t.Fatalf("Run(%q) = error %#v commands %#v", operand, err, commands)
			}
		})
	}

	if err := os.Mkdir(filepath.Join(dir, "directory-v0.1.0.tar.xz"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, operand := range []string{"notes.tar.xz", "directory-v0.1.0.tar.xz"} {
		t.Run("not-regular-"+operand, func(t *testing.T) {
			var stdout bytes.Buffer
			err := deploy.Run(context.Background(), []string{"sbx1", operand}, &stdout, seam.Deps{Dir: dir, Exec: failRunner(t), Cloud: failCloud(t)})
			var noFile *deploy.NoFileError
			if !errors.As(err, &noFile) || noFile.Path != operand || err.Error() != "no such file '"+operand+"'" || noFile.ExitCode() != 2 || stdout.Len() != 0 {
				t.Fatalf("Run(%q) = %#v stdout %q", operand, err, stdout.String())
			}
		})
	}

	invalid := "notes.tar.xz"
	if err := os.WriteFile(filepath.Join(dir, invalid), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := deploy.Run(context.Background(), []string{"sbx1", invalid}, &stdout, seam.Deps{Dir: dir, Exec: failRunner(t), Cloud: failCloud(t)})
	var fileErr *deploy.FileError
	if !errors.As(err, &fileErr) || fileErr.Path != invalid || fileErr.Reason != "name is not <app>-v<semver>.tar.xz" || fileErr.ExitCode() != 2 || stdout.Len() != 0 {
		t.Fatalf("invalid name = %#v stdout %q", err, stdout.String())
	}
}

func TestDeployArchiveInspectionFailures(t *testing.T) {
	// R-Z9EM-IAM2 R-ZAMI-W2CR R-ZEA8-1DKU
	tests := []struct {
		name, members, manifest, reason string
		calls                           int
	}{
		{name: "missing manifest", members: "bin/crm\n", reason: "no etc/manifest.toml in the archive", calls: 1},
		{name: "manifest decode", members: "etc/manifest.toml\nbin/crm\n", manifest: "not toml =", calls: 2},
		{name: "manifest mismatch", members: "etc/manifest.toml\nbin/gmail\n", manifest: "app = \"gmail\"\n", reason: "manifest app does not match file name", calls: 2},
		{name: "missing binary", members: "etc/manifest.toml\nbin/other\n", manifest: "app = \"crm\"\n", reason: "no bin/crm in the archive", calls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			operand := "crm-v1.2.3.tar.xz"
			if err := os.WriteFile(filepath.Join(dir, operand), []byte("artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.name == "manifest decode" {
				_, decodeErr := checkout.DecodeManifest(strings.NewReader(test.manifest))
				test.reason = checkout.ManifestFile + ": " + decodeErr.Error()
			}
			var commands []seam.Cmd
			err := deploy.Run(context.Background(), []string{"sbx1", operand}, io.Discard, seam.Deps{Dir: dir, Cloud: failCloud(t), Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, command)
				if len(commands) == 1 {
					return seam.Result{Stdout: []byte(test.members)}, nil
				}
				return seam.Result{Stdout: []byte(test.manifest)}, nil
			}})
			var fileErr *deploy.FileError
			if !errors.As(err, &fileErr) || fileErr.Path != operand || fileErr.Reason != test.reason || len(commands) != test.calls {
				t.Fatalf("error %#v commands %#v", err, commands)
			}
			wantList := seam.Cmd{Path: "tar", Args: []string{"-t", "-J", "-f", operand}, Dir: dir}
			if !reflect.DeepEqual(commands[0], wantList) {
				t.Fatalf("list command = %#v", commands[0])
			}
			if test.calls == 2 {
				wantExtract := seam.Cmd{Path: "tar", Args: []string{"-x", "-J", "-O", "-f", operand, checkout.ManifestFile}, Dir: dir}
				if !reflect.DeepEqual(commands[1], wantExtract) {
					t.Fatalf("extract command = %#v", commands[1])
				}
			}
		})
	}

	startErr := errors.New("process start failed")
	for _, test := range []struct {
		name      string
		failCall  int
		result    seam.Result
		runnerErr error
		label     string
	}{
		{name: "list exit", failCall: 1, result: seam.Result{ExitCode: 7, Stderr: []byte("bad list\n")}, label: "tar -t -J -f crm-v1.2.3.tar.xz"},
		{name: "extract exit", failCall: 2, result: seam.Result{ExitCode: 8, Stderr: []byte("bad extract\n")}, label: "tar -x -J -O -f crm-v1.2.3.tar.xz etc/manifest.toml"},
		{name: "list runner", failCall: 1, runnerErr: startErr},
		{name: "extract runner", failCall: 2, runnerErr: startErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			operand := "crm-v1.2.3.tar.xz"
			if err := os.WriteFile(filepath.Join(dir, operand), []byte("artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			err := deploy.Run(context.Background(), []string{"sbx1", operand}, io.Discard, seam.Deps{Dir: dir, Cloud: failCloud(t), Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				calls++
				if calls == test.failCall {
					return test.result, test.runnerErr
				}
				return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/crm\n")}, nil
			}})
			var processErr *deploy.ProcessError
			if test.runnerErr == nil {
				if !errors.As(err, &processErr) || processErr.Label != test.label || processErr.Status != test.result.ExitCode || processErr.Stderr != string(test.result.Stderr) {
					t.Fatalf("process error = %#v", err)
				}
			} else if errors.As(err, &processErr) || !errors.Is(err, startErr) || !strings.Contains(err.Error(), "tar") {
				t.Fatalf("runner error = %#v", err)
			}
			if calls != test.failCall {
				t.Fatalf("calls = %d", calls)
			}
		})
	}
}

func TestDeployHelpAndGrammarPrecedeExternalAccess(t *testing.T) {
	// R-O48O-O06U R-O5GL-1RXJ R-O6OH-FJO8 R-O7WD-TBEX R-OAC6-KUWB
	for _, args := range [][]string{{"--help"}, {"-h"}, {"sbx1", "crm-v0.1.0.tar.xz", "--help"}} {
		var stdout bytes.Buffer
		calls := 0
		deps := seam.Deps{Dir: t.TempDir(), Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			calls++
			return cloud.Clients{}, errors.New("called")
		}, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			calls++
			return seam.Result{}, errors.New("called")
		}}
		if err := deploy.Run(context.Background(), args, &stdout, deps); err != nil || stdout.String() != deployHelp || calls != 0 {
			t.Fatalf("help %q = stdout %q err %v calls %d", args, stdout.String(), err, calls)
		}
	}
	for _, test := range []struct {
		args    []string
		message string
	}{
		{nil, "deploy needs <space> and <file>"},
		{[]string{"sbx1"}, "deploy needs <space> and <file>"},
		{[]string{"sbx1", "a", "extra"}, "deploy takes only <space> and <file>"},
		{[]string{"sbx1", "--bad"}, "unknown option '--bad'"},
	} {
		err := deploy.Run(context.Background(), test.args, io.Discard, seam.Deps{Dir: t.TempDir(), Exec: failRunner(t), Cloud: failCloud(t)})
		var usage *deploy.UsageError
		if !errors.As(err, &usage) || usage.Message != test.message || usage.Help != "devctl deploy --help" {
			t.Fatalf("Run(%q) = %#v", test.args, err)
		}
	}
}

func TestDeployFileValidationAndArchiveContract(t *testing.T) {
	// R-08GB-YDTQ R-FBPJ-DRF6 R-Z9EM-IAM2 R-ZAMI-W2CR R-ZEA8-1DKU R-ZFI4-F5BJ R-OBK2-YMN0
	dir := t.TempDir()
	for _, test := range []struct{ operand, want string }{{"missing-v0.1.0.tar.xz", "no such file"}, {"notes.tar.xz", "name is not <app>-v<semver>.tar.xz"}} {
		if test.operand == "notes.tar.xz" {
			if err := os.WriteFile(filepath.Join(dir, test.operand), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		err := deploy.Run(context.Background(), []string{"sbx1", test.operand}, io.Discard, seam.Deps{Dir: dir, Exec: failRunner(t), Cloud: failCloud(t)})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: %v", test.operand, err)
		}
	}
	h := newHarness(t, "crm-v0.2.0.tar.xz", "app = \"crm\"\n")
	var stdout bytes.Buffer
	if err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps()); err != nil {
		t.Fatal(err)
	}
	if len(h.commands) != 4 || h.commands[0].Path != "tar" || h.commands[1].Path != "tar" || h.commands[2].Path != "git" || h.commands[3].Path != "ssh" {
		t.Fatalf("commands = %#v", h.commands)
	}
	if got := stdout.String(); !strings.HasPrefix(got, "file: ok (crm v0.2.0)\n") {
		t.Fatalf("stdout = %q", got)
	}

	h = newHarness(t, "crm-v0.2.0.tar.xz", "app = \"wrong\"\n")
	err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, io.Discard, h.deps())
	var fileErr *deploy.FileError
	if !errors.As(err, &fileErr) || fileErr.Reason != "manifest app does not match file name" || len(h.commands) != 2 {
		t.Fatalf("mismatch = %#v commands %d", err, len(h.commands))
	}
}

func TestDeployUsesRootSessionSpaceSecretsUploadAndHost(t *testing.T) {
	// R-OCRZ-CEDP R-9PWP-H3AP R-OF7S-3XV3 R-0HFK-QF61 R-OHNK-VHCH R-OK3D-N0TV R-FAHM-ZZOH R-FHT1-AM4N
	h := newHarness(t, "crm-v0.2.0-rc.1.tar.xz", "app = \"crm\"\nsecrets = [\"B\", \"A\", \"C\"]\n")
	h.secretNames = []string{"A", "B", "C", "EXTRA"}
	var stdout bytes.Buffer
	if err := deploy.Run(context.Background(), []string{"sbx1.ikigenba.dev", h.operand}, &stdout, h.deps()); err != nil {
		t.Fatal(err)
	}
	want := "file: ok (crm v0.2.0-rc.1)\nsecrets: ok (3 keys)\nupload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz)\ninstall: ok (opsctl installed crm)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if h.cloudProfile != "ikigenba.dev" || h.cloudRegion != "us-east-2" || h.ec2Domain != "ikigenba.dev" || h.ssmCalls != 1 || h.ssmParameter != "/sbx1.ikigenba.dev/crm" {
		t.Fatalf("root session evidence = profile %q region %q EC2 domain %q SSM calls %d parameter %q", h.cloudProfile, h.cloudRegion, h.ec2Domain, h.ssmCalls, h.ssmParameter)
	}
	if h.bucket != "ikigenba.dev" || h.key != "sbx1/deploy/crm-v0.2.0-rc.1.tar.xz" || !bytes.Equal(h.body, h.artifact) || h.size != int64(len(h.artifact)) {
		t.Fatalf("upload = %q %q %d %q", h.bucket, h.key, h.size, h.body)
	}
	ssh := h.commands[len(h.commands)-1]
	if ssh.Path != "ssh" || !strings.Contains(ssh.Args[len(ssh.Args)-1], "s3://ikigenba.dev/sbx1/deploy/crm-v0.2.0-rc.1.tar.xz") {
		t.Fatalf("ssh = %#v", ssh)
	}

	h = newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\nsecrets = [\"Z\", \"A\"]\n")
	err := deploy.Run(context.Background(), []string{"sbx1.ikigenba.dev", h.operand}, io.Discard, h.deps())
	var missing *deploy.MissingSecretsError
	if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Names, []string{"A", "Z"}) || missing.Space != "sbx1" || h.putCalls != 0 {
		t.Fatalf("missing = %#v uploads %d", err, h.putCalls)
	}

	h = newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
	h.state = cloud.StateStopped
	err = deploy.Run(context.Background(), []string{"sbx1", h.operand}, io.Discard, h.deps())
	var stopped *space.NotRunningError
	if !errors.As(err, &stopped) || stopped.Domain != "sbx1.ikigenba.dev" || h.putCalls != 0 {
		t.Fatalf("stopped = %#v", err)
	}

	h = newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
	h.sshStatus, h.sshStderr = 7, "first\nsecond\n"
	err = deploy.Run(context.Background(), []string{"sbx1", h.operand}, io.Discard, h.deps())
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != "install" || commandErr.Status != 7 || commandErr.Detail() != "> first\n> second" {
		t.Fatalf("host error = %#v", err)
	}
}

func TestDeployInstallHostUsesFoundAddressAndDeps(t *testing.T) {
	// R-ELEZ-QEF2
	h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
	h.instances = []cloud.Instance{{ID: "i-target", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "203.0.113.77"}}
	deps := h.deps()
	baseExec := deps.Exec
	installErr := errors.New("install reached supplied runner")
	var installCmd seam.Cmd
	installCalls := 0
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "ssh" {
			installCalls++
			installCmd = cmd
			return seam.Result{}, installErr
		}
		return baseExec(ctx, cmd)
	}
	err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, io.Discard, deps)
	if !errors.Is(err, installErr) || installCalls != 1 {
		t.Fatalf("install error = %v, calls = %d", err, installCalls)
	}
	if installCmd.Dir != deps.Dir || len(installCmd.Args) != 8 || installCmd.Args[6] != "ec2-user@203.0.113.77" {
		t.Fatalf("install command = %#v, deps dir = %q", installCmd, deps.Dir)
	}
}

func TestDeployCountsDistinctRequiredSecrets(t *testing.T) {
	// R-0HFK-QF61
	for _, test := range []struct {
		name, manifest, wantLine string
	}{
		{name: "duplicate names", manifest: "app = \"crm\"\nsecrets = [\"A\", \"B\", \"A\", \"C\"]\n", wantLine: "secrets: ok (3 keys)\n"},
		{name: "two keys", manifest: "app = \"crm\"\nsecrets = [\"A\", \"B\"]\n", wantLine: "secrets: ok (2 keys)\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, "crm-v0.1.0.tar.xz", test.manifest)
			h.secretNames = []string{"A", "B", "C", "EXTRA"}
			var stdout bytes.Buffer
			if err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps()); err != nil {
				t.Fatal(err)
			}
			if h.ssmCalls != 1 || !strings.Contains(stdout.String(), test.wantLine) {
				t.Fatalf("SSM calls %d stdout %q", h.ssmCalls, stdout.String())
			}
		})
	}
}

func TestDeployLifecycleFailuresStopInOrderAndKeepUpload(t *testing.T) {
	// R-FAHM-ZZOH
	t.Run("secrets", func(t *testing.T) {
		h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
		h.secretErr = errors.New("secrets failed")
		var stdout bytes.Buffer
		err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps())
		if !errors.Is(err, h.secretErr) || stdout.String() != "file: ok (crm v0.1.0)\n" || h.putCalls != 0 || sshCommandCount(h.commands) != 0 {
			t.Fatalf("error %#v stdout %q put %d commands %#v", err, stdout.String(), h.putCalls, h.commands)
		}
	})

	t.Run("upload", func(t *testing.T) {
		h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
		h.putErr = errors.New("upload failed")
		var stdout bytes.Buffer
		err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps())
		if !errors.Is(err, h.putErr) || stdout.String() != "file: ok (crm v0.1.0)\nsecrets: ok (0 keys)\n" || h.putCalls != 1 || sshCommandCount(h.commands) != 0 {
			t.Fatalf("error %#v stdout %q put %d commands %#v", err, stdout.String(), h.putCalls, h.commands)
		}
	})

	for _, test := range []struct {
		name      string
		status    int
		runnerErr error
	}{
		{name: "install exit", status: 19},
		{name: "install runner", runnerErr: errors.New("install transport failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
			h.sshStatus, h.sshErr = test.status, test.runnerErr
			var stdout bytes.Buffer
			err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps())
			if err == nil || h.putCalls != 1 || h.deleteCalls != 0 || sshCommandCount(h.commands) != 1 || !strings.HasSuffix(stdout.String(), "upload: ok (-> ikigenba.dev/sbx1/deploy/crm-v0.1.0.tar.xz)\n") || strings.Contains(stdout.String(), "install: ok") {
				t.Fatalf("error %#v stdout %q put/delete %d/%d commands %#v", err, stdout.String(), h.putCalls, h.deleteCalls, h.commands)
			}
		})
	}

	h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
	if err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, io.Discard, h.deps()); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"tar-list", "tar-manifest", "root", "connect", "identity", "lookup", "secrets", "upload", "install"}
	if !reflect.DeepEqual(h.operations, wantOrder) {
		t.Fatalf("operations = %#v, want %#v", h.operations, wantOrder)
	}
}

func TestDeployReturnsRootParseConnectAndLookupFailuresUnchanged(t *testing.T) {
	// R-OCRZ-CEDP
	t.Run("root", func(t *testing.T) {
		h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
		if err := os.WriteFile(filepath.Join(h.root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps())
		var rootErr *checkout.RootFileError
		if !errors.As(err, &rootErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(rootErr).Pointer() || rootErr.Detail != "missing 'region'" || stdout.String() != "file: ok (crm v0.1.0)\n" || h.s3Calls != 0 || sshCommandCount(h.commands) != 0 {
			t.Fatalf("error %#v stdout %q calls %#v", err, stdout.String(), h.commands)
		}
	})

	t.Run("parse", func(t *testing.T) {
		h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
		err := deploy.Run(context.Background(), []string{"Bad", h.operand}, io.Discard, h.deps())
		var parseErr *spaceref.InvalidLabelError
		if !errors.As(err, &parseErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(parseErr).Pointer() || parseErr.Operand != "Bad" || h.cloudCalls != 0 || h.s3Calls != 0 || sshCommandCount(h.commands) != 0 {
			t.Fatalf("error %#v cloud %d S3 %d commands %#v", err, h.cloudCalls, h.s3Calls, h.commands)
		}
	})

	for _, test := range []struct {
		name string
		set  func(*harness, error)
	}{
		{name: "connect opener", set: func(h *harness, err error) { h.cloudErr = err }},
		{name: "connect identity", set: func(h *harness, err error) { h.stsErr = err }},
		{name: "lookup", set: func(h *harness, err error) { h.ec2Err = err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
			want := errors.New(test.name + " failed")
			test.set(h, want)
			var stdout bytes.Buffer
			err := deploy.Run(context.Background(), []string{"sbx1", h.operand}, &stdout, h.deps())
			if reflect.ValueOf(err).Pointer() != reflect.ValueOf(want).Pointer() || stdout.String() != "file: ok (crm v0.1.0)\n" || h.s3Calls != 0 || sshCommandCount(h.commands) != 0 {
				t.Fatalf("error %#v want identity %#v stdout %q S3 %d commands %#v", err, want, stdout.String(), h.s3Calls, h.commands)
			}
		})
	}
}

func TestDeployPromotesSameFileAcrossSpacesWithoutIdentityRestriction(t *testing.T) {
	// R-OF7S-3XV3
	h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
	h.accountID = "account-identity-is-not-a-deploy-policy"
	h.instances = []cloud.Instance{
		{ID: "i-sbx1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"},
		{ID: "i-staging", Space: "staging.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.43"},
	}

	for _, test := range []struct {
		space, label, address string
	}{
		{space: "sbx1", label: "sbx1", address: "18.118.7.42"},
		{space: "staging", label: "staging", address: "18.118.7.43"},
	} {
		var stdout bytes.Buffer
		if err := deploy.Run(context.Background(), []string{test.space, h.operand}, &stdout, h.deps()); err != nil {
			t.Fatalf("deploy to %s: %v", test.space, err)
		}
		want := "file: ok (crm v0.1.0)\nsecrets: ok (0 keys)\nupload: ok (-> ikigenba.dev/" + test.label + "/deploy/crm-v0.1.0.tar.xz)\ninstall: ok (opsctl installed crm)\n"
		if stdout.String() != want {
			t.Fatalf("deploy to %s stdout = %q", test.space, stdout.String())
		}
		ssh := h.commands[len(h.commands)-1]
		if ssh.Path != "ssh" || !strings.Contains(strings.Join(ssh.Args, " "), "ec2-user@"+test.address) || !strings.Contains(ssh.Args[len(ssh.Args)-1], "s3://ikigenba.dev/"+test.label+"/deploy/crm-v0.1.0.tar.xz") {
			t.Fatalf("deploy to %s ssh = %#v", test.space, ssh)
		}
	}
	if h.stsCalls != 2 || h.putCalls != 2 {
		t.Fatalf("same-file promotion calls = STS %d PutObject %d", h.stsCalls, h.putCalls)
	}
}

func TestDeploySpaceFailuresStopBeforeSecretsUploadAndHost(t *testing.T) {
	// R-OCRZ-CEDP
	tests := []struct {
		name, operand string
		instances     []cloud.Instance
		check         func(*testing.T, error)
	}{
		{
			name: "missing", operand: "gone", instances: []cloud.Instance{},
			check: func(t *testing.T, err error) {
				var missing *cloud.NoSpaceError
				if !errors.As(err, &missing) || missing.Domain != "gone.ikigenba.dev" {
					t.Fatalf("error = %#v", err)
				}
			},
		},
		{
			name: "stopped", operand: "sbx2",
			instances: []cloud.Instance{{ID: "i-sbx2", Space: "sbx2.ikigenba.dev", State: cloud.StateStopped, Address: "18.118.7.44"}},
			check: func(t *testing.T, err error) {
				var stopped *space.NotRunningError
				if !errors.As(err, &stopped) || stopped.Domain != "sbx2.ikigenba.dev" || stopped.State != cloud.StateStopped {
					t.Fatalf("error = %#v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\n")
			h.instances = test.instances
			var stdout bytes.Buffer
			err := deploy.Run(context.Background(), []string{test.operand, h.operand}, &stdout, h.deps())
			test.check(t, err)
			wantCommands := []string{"tar", "tar", "git"}
			if stdout.String() != "file: ok (crm v0.1.0)\n" || h.ssmCalls != 0 || h.s3Calls != 0 || !reflect.DeepEqual(commandPaths(h.commands), wantCommands) {
				t.Fatalf("after failure: stdout %q SSM %d S3 %d commands %#v", stdout.String(), h.ssmCalls, h.s3Calls, h.commands)
			}
		})
	}
}

func TestDeployMissingSecretsForAbbreviatedAndFQDNSpace(t *testing.T) {
	// R-OHNK-VHCH
	for _, operand := range []string{"sbx1", "sbx1.ikigenba.dev"} {
		t.Run(operand, func(t *testing.T) {
			h := newHarness(t, "crm-v0.1.0.tar.xz", "app = \"crm\"\nsecrets = [\"CRM_WEBHOOK_SECRET\", \"CRM_ORG\"]\n")
			var stdout bytes.Buffer
			err := deploy.Run(context.Background(), []string{operand, h.operand}, &stdout, h.deps())
			var missing *deploy.MissingSecretsError
			if !errors.As(err, &missing) || missing.App != "crm" || missing.Space != "sbx1" || !reflect.DeepEqual(missing.Names, []string{"CRM_ORG", "CRM_WEBHOOK_SECRET"}) {
				t.Fatalf("error = %#v", err)
			}
			if stdout.String() != "file: ok (crm v0.1.0)\n" || h.ssmCalls != 1 || h.s3Calls != 0 || sshCommandCount(h.commands) != 0 {
				t.Fatalf("after missing secrets: stdout %q SSM %d S3 %d commands %#v", stdout.String(), h.ssmCalls, h.s3Calls, h.commands)
			}
		})
	}
}

func sshCommandCount(commands []seam.Cmd) int {
	count := 0
	for _, command := range commands {
		if command.Path == "ssh" {
			count++
		}
	}
	return count
}

func commandPaths(commands []seam.Cmd) []string {
	paths := make([]string, len(commands))
	for i, command := range commands {
		paths[i] = command.Path
	}
	return paths
}

type harness struct {
	t                                                   *testing.T
	root, operand                                       string
	artifact                                            []byte
	manifest                                            string
	commands                                            []seam.Cmd
	state                                               cloud.InstanceState
	instances                                           []cloud.Instance
	secretNames                                         []string
	cloudProfile, cloudRegion, ec2Domain, ssmParameter  string
	accountID                                           string
	bucket, key                                         string
	body                                                []byte
	size                                                int64
	stsCalls, ssmCalls, s3Calls, putCalls, sshStatus    int
	sshStderr                                           string
	cloudCalls, deleteCalls                             int
	cloudErr, stsErr, ec2Err, secretErr, putErr, sshErr error
	operations                                          []string
	ssm                                                 *fakeSSM
}

func newHarness(t *testing.T, name, manifest string) *harness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := []byte("complete artifact bytes")
	if err := os.WriteFile(filepath.Join(root, name), artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, root: root, operand: name, artifact: artifact, manifest: manifest, state: cloud.StateRunning, accountID: "123456789012"}
	h.ssm = &fakeSSM{h: h}
	return h
}

func (h *harness) deps() seam.Deps {
	return seam.Deps{Dir: h.root, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		h.commands = append(h.commands, cmd)
		switch cmd.Path {
		case "tar":
			if cmd.Args[0] == "-t" {
				h.operations = append(h.operations, "tar-list")
				return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/crm\n")}, nil
			}
			h.operations = append(h.operations, "tar-manifest")
			return seam.Result{Stdout: []byte(h.manifest)}, nil
		case "git":
			h.operations = append(h.operations, "root")
			return seam.Result{Stdout: []byte(h.root + "\n")}, nil
		case "ssh":
			h.operations = append(h.operations, "install")
			if h.sshErr != nil {
				return seam.Result{}, h.sshErr
			}
			return seam.Result{ExitCode: h.sshStatus, Stderr: []byte(h.sshStderr)}, nil
		default:
			h.t.Fatalf("unexpected command %#v", cmd)
			return seam.Result{}, nil
		}
	}, Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
		h.cloudCalls++
		h.operations = append(h.operations, "connect")
		h.cloudProfile, h.cloudRegion = profile, region
		if h.cloudErr != nil {
			return cloud.Clients{}, h.cloudErr
		}
		return cloud.Clients{STS: &fakeSTS{h: h}, EC2: &fakeEC2{h: h}, SSM: h.ssm, S3: &fakeS3{h: h}}, nil
	}}
}

type fakeSTS struct {
	cloud.STS
	h *harness
}

func (f *fakeSTS) CallerAccountID(context.Context) (string, error) {
	f.h.stsCalls++
	f.h.operations = append(f.h.operations, "identity")
	if f.h.stsErr != nil {
		return "", f.h.stsErr
	}
	return f.h.accountID, nil
}

type fakeEC2 struct {
	cloud.EC2
	h *harness
}

func (f *fakeEC2) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	f.h.operations = append(f.h.operations, "lookup")
	f.h.ec2Domain = domain
	if f.h.ec2Err != nil {
		return nil, f.h.ec2Err
	}
	if f.h.instances != nil {
		return f.h.instances, nil
	}
	return []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: f.h.state, Address: "18.118.7.42"}}, nil
}

type fakeSSM struct {
	cloud.SSM
	h *harness
}

func (f *fakeSSM) GetParameter(_ context.Context, name string) (string, error) {
	f.h.ssmCalls++
	f.h.operations = append(f.h.operations, "secrets")
	f.h.ssmParameter = name
	if f.h.secretErr != nil {
		return "", f.h.secretErr
	}
	object := make([]string, len(f.h.secretNames))
	for i, n := range f.h.secretNames {
		object[i] = `"` + n + `":"value"`
	}
	return "{" + strings.Join(object, ",") + "}", nil
}

type fakeS3 struct {
	h *harness
}

func (f *fakeS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	f.h.s3Calls++
	return nil, nil
}

func (f *fakeS3) PutObject(_ context.Context, bucket, key string, body io.Reader, size int64) error {
	f.h.s3Calls++
	f.h.putCalls++
	f.h.operations = append(f.h.operations, "upload")
	f.h.bucket, f.h.key, f.h.size = bucket, key, size
	f.h.body, _ = io.ReadAll(body)
	return f.h.putErr
}

func (f *fakeS3) DeleteObjects(context.Context, string, []string) error {
	f.h.s3Calls++
	f.h.deleteCalls++
	return nil
}

func assertFields(t *testing.T, value any, names ...string) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(names) {
		t.Fatalf("%T has %d fields, want %d", value, typ.NumField(), len(names))
	}
	for i, name := range names {
		if typ.Field(i).Name != name {
			t.Fatalf("%T field %d = %s", value, i, typ.Field(i).Name)
		}
	}
}

func assertFieldTypes(t *testing.T, value any, types ...reflect.Type) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(types) {
		t.Fatalf("%T has %d fields, want %d", value, typ.NumField(), len(types))
	}
	for i, want := range types {
		if got := typ.Field(i).Type; got != want {
			t.Fatalf("%T field %d type = %v, want %v", value, i, got, want)
		}
	}
}

func failRunner(t *testing.T) seam.Runner {
	t.Helper()
	return func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected process call")
		return seam.Result{}, nil
	}
}
func failCloud(t *testing.T) cloud.Opener {
	t.Helper()
	return func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud call")
		return cloud.Clients{}, nil
	}
}
