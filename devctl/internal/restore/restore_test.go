package restore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const wantRestoreHelp = `Usage: devctl restore <space> <app> [--at <timestamp>]

Have opsctl on the space put <app> back from the space's own backups. The
app's etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s socket and service are stopped for the
restore and started again after it, unless <app> is disabled.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

type restoreSignature func(context.Context, []string, io.Writer, seam.Deps) error

var _ restoreSignature = Run

func TestRestorePublicContractAndGrammar(t *testing.T) {
	// R-ONR2-SC1Y R-FMOM-TP3F R-JW73-1HBN R-OQ6V-JVJC R-ORER-XNA1 R-FRK8-CS27
	errValue := &UsageError{Message: "bad", Help: "devctl restore --help"}
	if errValue.Error() != "bad" || errValue.ExitCode() != 2 || errValue.Detail() != "see 'devctl restore --help' for usage" || (&UsageError{}).Detail() != "" {
		t.Fatalf("UsageError = %#v", errValue)
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"sbx1", "crm", "--help"}} {
		var stdout bytes.Buffer
		if err := Run(context.Background(), args, &stdout, seam.Deps{Dir: t.TempDir(), Exec: restoreFailRunner(t), Cloud: restoreFailCloud(t)}); err != nil || stdout.String() != wantRestoreHelp {
			t.Fatalf("help %q: %q %v", args, stdout.String(), err)
		}
	}
	for _, test := range []struct {
		args          []string
		message, help string
	}{
		{nil, "restore needs <space> and <app>", "devctl restore --help"},
		{[]string{"sbx1"}, "restore needs <space> and <app>", "devctl restore --help"},
		{[]string{"sbx1", "crm", "extra"}, "restore takes only <space> and <app>", "devctl restore --help"},
		{[]string{"--bad", "sbx1", "crm"}, "unknown option '--bad'", "devctl restore --help"},
		{[]string{"sbx1", "crm", "--at"}, "option '--at' requires a value", "devctl restore --help"},
		{[]string{"sbx1", "crm", "--at="}, "option '--at' requires a value", "devctl restore --help"},
		{[]string{"sbx1", "crm", "--at", "yesterday"}, "--at takes an RFC 3339 timestamp", ""},
	} {
		err := Run(context.Background(), test.args, io.Discard, seam.Deps{Dir: t.TempDir(), Exec: restoreFailRunner(t), Cloud: restoreFailCloud(t)})
		var usage *UsageError
		if !errors.As(err, &usage) || usage.Message != test.message || usage.Help != test.help {
			t.Fatalf("Run(%q) = %#v", test.args, err)
		}
	}
	parsed, err := parse([]string{"--at", "2026-09-11T17:00:00Z", "sbx1", "crm", "--at=2026-09-11T18:00:00.25-05:00"})
	if err != nil || parsed.space != "sbx1" || parsed.app != "crm" || parsed.at != "2026-09-11T18:00:00.25-05:00" {
		t.Fatalf("parse = %#v, %v", parsed, err)
	}
}

func TestRestoreReturnsRootParseConnectAndLookupFailuresUnchanged(t *testing.T) {
	// R-OTUK-P6RF
	t.Run("root", func(t *testing.T) {
		h := newRestoreHarness(t)
		if err := os.WriteFile(filepath.Join(h.root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		err := Run(context.Background(), []string{"sbx1", "crm"}, &stdout, h.deps())
		var rootErr *checkout.RootFileError
		if !errors.As(err, &rootErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(rootErr).Pointer() || rootErr.Detail != "missing 'region'" || stdout.Len() != 0 || sshCommandCount(h.commands) != 0 || h.s3Calls != 0 {
			t.Fatalf("error %#v stdout %q commands %#v S3 %d", err, stdout.String(), h.commands, h.s3Calls)
		}
	})

	t.Run("parse", func(t *testing.T) {
		h := newRestoreHarness(t)
		err := Run(context.Background(), []string{"Bad", "crm"}, io.Discard, h.deps())
		var parseErr *spaceref.InvalidLabelError
		if !errors.As(err, &parseErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(parseErr).Pointer() || parseErr.Operand != "Bad" || h.cloudCalls != 0 || sshCommandCount(h.commands) != 0 || h.s3Calls != 0 {
			t.Fatalf("error %#v cloud %d commands %#v S3 %d", err, h.cloudCalls, h.commands, h.s3Calls)
		}
	})

	for _, test := range []struct {
		name string
		set  func(*restoreHarness, error)
	}{
		{name: "connect opener", set: func(h *restoreHarness, err error) { h.cloudErr = err }},
		{name: "connect identity", set: func(h *restoreHarness, err error) { h.stsErr = err }},
		{name: "lookup", set: func(h *restoreHarness, err error) { h.ec2Err = err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newRestoreHarness(t)
			want := errors.New(test.name + " failed")
			test.set(h, want)
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"sbx1", "crm"}, &stdout, h.deps())
			if reflect.ValueOf(err).Pointer() != reflect.ValueOf(want).Pointer() || stdout.Len() != 0 || sshCommandCount(h.commands) != 0 || h.s3Calls != 0 {
				t.Fatalf("error %#v want identity %#v stdout %q commands %#v S3 %d", err, want, stdout.String(), h.commands, h.s3Calls)
			}
		})
	}
}

func TestRestoreUsesOnlyRootSessionAndOneHostCommand(t *testing.T) {
	// R-OTUK-P6RF R-9R4L-UV1E R-FV7X-I3AA R-OWAD-GQ8T
	h := newRestoreHarness(t)
	var stdout bytes.Buffer
	stamp := "2026-09-11T18:00:00.25-05:00"
	if err := Run(context.Background(), []string{"--at", stamp, "sbx1.ikigenba.dev", "crm"}, &stdout, h.deps()); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "restore: ok (opsctl restore crm --at "+stamp+")\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if h.profile != "ikigenba.dev" || h.region != "us-east-2" || h.ec2Domain != "ikigenba.dev" || h.s3Calls != 0 {
		t.Fatalf("session = %q %q %q s3=%d", h.profile, h.region, h.ec2Domain, h.s3Calls)
	}
	if len(h.commands) != 2 || h.commands[0].Path != "git" || h.commands[1].Path != "ssh" {
		t.Fatalf("commands = %#v", h.commands)
	}
	wantRemote := "'sudo' 'opsctl' 'restore' 'crm' '--at' '" + stamp + "'"
	if got := h.commands[1].Args[len(h.commands[1].Args)-1]; got != wantRemote {
		t.Fatalf("remote = %q", got)
	}

	h = newRestoreHarness(t)
	h.state = cloud.StateStopped
	err := Run(context.Background(), []string{"sbx1", "crm"}, &stdout, h.deps())
	var stopped *space.NotRunningError
	if !errors.As(err, &stopped) || stopped.Domain != "sbx1.ikigenba.dev" || len(h.commands) != 1 || h.s3Calls != 0 {
		t.Fatalf("stopped = %#v commands %#v S3 calls %d", err, h.commands, h.s3Calls)
	}

	h = newRestoreHarness(t)
	h.instances = []cloud.Instance{}
	stdout.Reset()
	err = Run(context.Background(), []string{"gone", "crm"}, &stdout, h.deps())
	var missing *cloud.NoSpaceError
	if !errors.As(err, &missing) || missing.Domain != "gone.ikigenba.dev" || stdout.Len() != 0 || sshCommandCount(h.commands) != 0 || h.s3Calls != 0 {
		t.Fatalf("missing = %#v stdout %q commands %#v S3 calls %d", err, stdout.String(), h.commands, h.s3Calls)
	}

	h = newRestoreHarness(t)
	h.sshStatus, h.sshStderr = 1, "restore failed\nagain\n"
	stdout.Reset()
	err = Run(context.Background(), []string{"sbx1", "crm"}, &stdout, h.deps())
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != "restore" || commandErr.Status != 1 || commandErr.Detail() != "> restore failed\n> again" || stdout.Len() != 0 || h.s3Calls != 0 {
		t.Fatalf("host error = %#v stdout %q S3 calls %d", err, stdout.String(), h.s3Calls)
	}
}

func TestRestoreReturnsSudoErrorUnchanged(t *testing.T) {
	// R-FV7X-I3AA
	want := errors.New("sentinel")
	remote := &recordingSudoer{err: want}
	err := restoreOnHost(context.Background(), io.Discard, remote, []string{"opsctl", "restore", "crm"})
	if !errors.Is(err, want) || remote.step != "restore" || !reflect.DeepEqual(remote.args, []string{"opsctl", "restore", "crm"}) {
		t.Fatalf("result = %v %#v", err, remote)
	}
}

type recordingSudoer struct {
	step string
	args []string
	err  error
}

func (r *recordingSudoer) Sudo(_ context.Context, step string, args ...string) (host.Output, error) {
	r.step, r.args = step, append([]string(nil), args...)
	return host.Output{}, r.err
}

type restoreHarness struct {
	t                          *testing.T
	root                       string
	commands                   []seam.Cmd
	state                      cloud.InstanceState
	instances                  []cloud.Instance
	profile, region, ec2Domain string
	sshStatus, s3Calls         int
	sshStderr                  string
	cloudCalls                 int
	cloudErr, stsErr, ec2Err   error
}

func newRestoreHarness(t *testing.T) *restoreHarness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &restoreHarness{t: t, root: root, state: cloud.StateRunning}
}
func (h *restoreHarness) deps() seam.Deps {
	return seam.Deps{Dir: h.root, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		h.commands = append(h.commands, cmd)
		switch cmd.Path {
		case "git":
			return seam.Result{Stdout: []byte(h.root + "\n")}, nil
		case "ssh":
			return seam.Result{ExitCode: h.sshStatus, Stderr: []byte(h.sshStderr)}, nil
		default:
			h.t.Fatalf("unexpected command %#v", cmd)
			return seam.Result{}, nil
		}
	}, Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
		h.cloudCalls++
		h.profile, h.region = profile, region
		if h.cloudErr != nil {
			return cloud.Clients{}, h.cloudErr
		}
		return cloud.Clients{STS: restoreSTS{h: h}, EC2: &restoreEC2{h: h}, S3: &restoreS3{h: h}}, nil
	}}
}

type restoreSTS struct {
	cloud.STS
	h *restoreHarness
}

func (f restoreSTS) CallerAccountID(context.Context) (string, error) {
	if f.h.stsErr != nil {
		return "", f.h.stsErr
	}
	return "123456789012", nil
}

type restoreEC2 struct {
	cloud.EC2
	h *restoreHarness
}

func (f *restoreEC2) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	f.h.ec2Domain = domain
	if f.h.ec2Err != nil {
		return nil, f.h.ec2Err
	}
	if f.h.instances != nil {
		return f.h.instances, nil
	}
	return []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: f.h.state, Address: "18.118.7.42"}}, nil
}

type restoreS3 struct {
	h *restoreHarness
}

func (f *restoreS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	f.h.s3Calls++
	return nil, nil
}

func (f *restoreS3) PutObject(context.Context, string, string, io.Reader, int64) error {
	f.h.s3Calls++
	return nil
}

func (f *restoreS3) DeleteObjects(context.Context, string, []string) error {
	f.h.s3Calls++
	return nil
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
func restoreFailRunner(t *testing.T) seam.Runner {
	t.Helper()
	return func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected process call")
		return seam.Result{}, nil
	}
}
func restoreFailCloud(t *testing.T) cloud.Opener {
	t.Helper()
	return func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud call")
		return cloud.Clients{}, nil
	}
}
