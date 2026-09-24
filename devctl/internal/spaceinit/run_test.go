package spaceinit

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
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const expectedHelp = `Usage: devctl space init <space> [--opsctl <version>] [--acme-email <address>]

Set the host's five derived keys again and run opsctl init. Keep its installed
opsctl, its CA address, and its backup periods unless an option names a
replacement. Cloud resources, records, and secrets are unchanged.

Options:
  --opsctl <version>      move the host to this opsctl release first
  --acme-email <address>  change where the CA sends the space's expiry warnings
`

func TestRunSignature(_ *testing.T) {
	// R-OMDZ-2I7D
	acceptRunSignature(Run)
}

func acceptRunSignature(func(context.Context, []string, io.Writer, seam.Deps) error) {}

func TestParseInvocation(t *testing.T) {
	// R-ONLV-G9Y2
	tests := []struct {
		args []string
		want invocation
	}{
		{[]string{"sbx1"}, invocation{space: "sbx1"}},
		{[]string{"--opsctl", "v1", "sbx1", "--acme-email=first@example", "--opsctl=v2", "--acme-email", "last@example"}, invocation{space: "sbx1", opsctl: "v2", acmeEmail: "last@example"}},
		{[]string{"--help"}, invocation{help: true}},
		{[]string{"-h"}, invocation{help: true}},
	}
	for _, test := range tests {
		got, err := parseInvocation(test.args)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("parseInvocation(%q) = %#v, %v; want %#v", test.args, got, err, test.want)
		}
	}
}

func TestArgumentFailuresPrecedeEveryDependency(t *testing.T) {
	// R-OOTR-U1OR
	tests := []struct {
		args []string
		want string
	}{
		{nil, "space init needs <space>"},
		{[]string{"sbx1", "sbx2"}, "space init takes only <space>"},
		{[]string{"sbx1", "--force"}, "unknown option '--force'"},
		{[]string{"sbx1", "--opsctl"}, "option '--opsctl' requires a value"},
		{[]string{"sbx1", "--opsctl="}, "option '--opsctl' requires a value"},
		{[]string{"sbx1", "--acme-email"}, "option '--acme-email' requires a value"},
		{[]string{"sbx1", "--acme-email="}, "option '--acme-email' requires a value"},
	}
	for _, test := range tests {
		called := false
		deps := noCallDeps(t, &called)
		var stdout bytes.Buffer
		err := Run(context.Background(), test.args, &stdout, deps)
		want := &space.UsageError{Message: test.want, Help: "devctl space --help"}
		if !reflect.DeepEqual(err, want) || stdout.Len() != 0 || called {
			t.Fatalf("Run(%q) = %#v, stdout %q, dependency called %v", test.args, err, stdout.String(), called)
		}
	}
}

func TestSeparatedOptionsRejectDashPrefixedValuesBeforeEveryDependency(t *testing.T) {
	// R-ONLV-G9Y2 R-OOTR-U1OR
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"sbx1", "--opsctl", "--acme-email", "alerts@ikigenba.dev"}, "option '--opsctl' requires a value"},
		{[]string{"sbx1", "--acme-email", "--opsctl", "v1"}, "option '--acme-email' requires a value"},
	}
	for _, test := range tests {
		called := false
		deps := noCallDeps(t, &called)
		var stdout bytes.Buffer
		err := Run(context.Background(), test.args, &stdout, deps)
		want := &space.UsageError{Message: test.want, Help: "devctl space --help"}
		if !reflect.DeepEqual(err, want) || stdout.Len() != 0 || called {
			t.Fatalf("Run(%q) = %#v, stdout %q, dependency called %v", test.args, err, stdout.String(), called)
		}
	}
}

func TestHelpIsExactAndIndependent(t *testing.T) {
	// R-OQ1O-7TFG
	for _, option := range []string{"--help", "-h"} {
		called := false
		var stdout bytes.Buffer
		err := Run(context.Background(), []string{option}, &stdout, noCallDeps(t, &called))
		if err != nil || stdout.String() != expectedHelp || called {
			t.Fatalf("Run(%s) = %v, stdout %q, dependency called %v", option, err, stdout.String(), called)
		}
	}
}

func noCallDeps(t *testing.T, called *bool) seam.Deps {
	t.Helper()
	mark := func() { *called = true }
	return seam.Deps{
		Dir:    t.TempDir(),
		Cloud:  func(context.Context, string, string) (cloud.Clients, error) { mark(); return cloud.Clients{}, nil },
		Exec:   func(context.Context, seam.Cmd) (seam.Result, error) { mark(); return seam.Result{}, nil },
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) { mark(); return seam.Result{}, nil },
	}
}

func TestResolutionAndOutputOrder(t *testing.T) {
	// R-OR9K-LL65 R-OSHG-ZCWU
	f := newFixture(t)
	f.sshResults = []seam.Result{{Stdout: []byte("arbitrary version output\n")}}
	stdout, err := f.run("sbx1")
	if err != nil {
		t.Fatal(err)
	}
	want := "account: ok (ikigenba.dev, us-east-2, 295229566359)\n" +
		"domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)\n" +
		"instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)\n" +
		"opsctl: ok (arbitrary version output kept, 5 keys set)\ninit: ok\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	wantOps := []string{"git", "open ikigenba.dev us-east-2", "sts", "spaces ikigenba.dev", "zone ikigenba.dev"}
	if !reflect.DeepEqual(f.ops[:5], wantOps) || !strings.HasPrefix(f.ops[5], "ssh ") {
		t.Fatalf("operations = %#v, want resolution before ssh", f.ops)
	}
}

func TestResolutionFailuresStopBeforeOutputAndSSH(t *testing.T) {
	// R-OR9K-LL65 R-ZIF9-2HC7
	t.Run("root file", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Remove(filepath.Join(f.root, checkout.RootFilePath)); err != nil {
			t.Fatal(err)
		}
		stdout, err := f.run("sbx1")
		var missing *checkout.NoRootFileError
		if !errors.As(err, &missing) || stdout != "" || containsPrefix(f.ops, "open ") || f.sshCalls != 0 {
			t.Fatalf("stdout=%q err=%#v ops=%v ssh=%d", stdout, err, f.ops, f.sshCalls)
		}
	})
	tests := []struct {
		name  string
		setup func(*fixture) error
		want  string
	}{
		{"connect", func(f *fixture) error { f.openErr = errors.New("open sentinel"); return f.openErr }, "open sentinel"},
		{"identity", func(f *fixture) error { f.stsErr = errors.New("identity sentinel"); return f.stsErr }, "identity sentinel"},
		{"lookup", func(f *fixture) error { f.instances = nil; return nil }, "no space at 'sbx1.ikigenba.dev'"},
		{"stopped", func(f *fixture) error { f.instances[0].State = cloud.StateStopped; return nil }, "'sbx1.ikigenba.dev' is stopped"},
		{"zone", func(f *fixture) error { f.zoneErr = errors.New("zone sentinel"); return f.zoneErr }, "zone sentinel"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			wantErr := test.setup(f)
			stdout, err := f.run("sbx1")
			if err == nil || err.Error() != test.want || stdout != "" || f.sshCalls != 0 || f.streamCalls != 0 {
				t.Fatalf("stdout=%q err=%v ops=%v ssh=%d stream=%d", stdout, err, f.ops, f.sshCalls, f.streamCalls)
			}
			if wantErr != nil && !errors.Is(err, wantErr) {
				t.Fatalf("error was not returned unchanged")
			}
		})
	}
	f := newFixture(t)
	stdout, err := f.run("crm.sbx1")
	if err == nil || err.Error() != "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'" || stdout != "" || containsPrefix(f.ops, "open ") {
		t.Fatalf("parse failure: stdout=%q err=%v ops=%v", stdout, err, f.ops)
	}
}

func TestUpgradeOrVersionThenFiveKeyConfiguration(t *testing.T) {
	// R-U320-UM5O R-OUX9-QWE8 R-GPPF-2CTN
	t.Run("keep arbitrary version and preserve other keys", func(t *testing.T) {
		f := newFixture(t)
		f.sshResults = []seam.Result{{Stdout: []byte(" release candidate +local \n")}}
		stdout, err := f.run("sbx1")
		if err != nil || !strings.Contains(stdout, "opsctl: ok ( release candidate +local  kept, 5 keys set)\n") {
			t.Fatalf("stdout=%q err=%v", stdout, err)
		}
		want := []string{
			"'sudo' 'opsctl' 'version'",
			"'sudo' 'opsctl' 'config' 'set' 'host.name=sbx1.ikigenba.dev'",
			"'sudo' 'opsctl' 'config' 'set' 'dns.provider=route53'",
			"'sudo' 'opsctl' 'config' 'set' 'dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78'",
			"'sudo' 'opsctl' 'config' 'set' 'aws.region=us-east-2'",
			"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/sbx1/'",
			"'sudo' 'opsctl' 'init'",
		}
		if !reflect.DeepEqual(f.remote, want) {
			t.Fatalf("remote commands = %#v, want %#v", f.remote, want)
		}
	})

	t.Run("upgrade and set email", func(t *testing.T) {
		f := newFixture(t)
		f.sshResults = make([]seam.Result, 9)
		f.sshResults[8].Stdout = []byte("successful init output is discarded\n")
		stdout, err := f.run("--acme-email", "alerts@ikigenba.dev", "sbx1", "--opsctl=v8.7.6")
		if err != nil || !strings.Contains(stdout, "opsctl: ok (v8.7.6 installed, 6 keys set)\ninit: ok\n") || strings.Contains(stdout, "successful init output") {
			t.Fatalf("stdout=%q err=%v", stdout, err)
		}
		if f.remote[0] != "'curl' '-fsSL' '-o' '"+hostsetup.InstallerPath+"' '"+hostsetup.DownloadURL+"/opsctl/v8.7.6/install.sh'" ||
			f.remote[1] != "'sudo' 'bash' '"+hostsetup.InstallerPath+"' 'v8.7.6'" ||
			f.remote[7] != "'sudo' 'opsctl' 'config' 'set' 'acme.email=alerts@ikigenba.dev'" {
			t.Fatalf("remote commands = %#v", f.remote)
		}
		joined := strings.Join(f.remote, "\n")
		for _, forbidden := range []string{"backup.host_files_seconds", "backup.service_files_seconds", "backup.service_db_seconds", "backup.service_wal_seconds", "host.apex", "config' 'del"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("forbidden configuration %q in %s", forbidden, joined)
			}
		}
	})
}

func TestRemoteFailureStopsAtFirstCommand(t *testing.T) {
	// R-U320-UM5O R-OUX9-QWE8 R-OW56-4O4X R-ZIF9-2HC7
	tests := []struct {
		name        string
		args        []string
		failAt      int
		wantStep    string
		wantOpsctl  bool
		wantCommand string
	}{
		{"version", []string{"sbx1"}, 0, "opsctl", false, "sudo opsctl version"},
		{"upgrade fetch", []string{"sbx1", "--opsctl=v9"}, 0, "opsctl", false, "curl -fsSL -o " + hostsetup.InstallerPath + " " + hostsetup.DownloadURL + "/opsctl/v9/install.sh"},
		{"upgrade install", []string{"sbx1", "--opsctl=v9"}, 1, "opsctl", false, "sudo bash " + hostsetup.InstallerPath + " v9"},
		{"configuration", []string{"sbx1", "--opsctl=v9"}, 4, "opsctl", false, "sudo opsctl config set dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78"},
		{"init", []string{"sbx1"}, 6, "init", true, "sudo opsctl init"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.sshResults = make([]seam.Result, test.failAt+1)
			f.sshResults[test.failAt] = seam.Result{ExitCode: 2, Stdout: []byte("line one\nline two\n")}
			stdout, err := f.run(test.args...)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) || commandErr.Step != test.wantStep || !strings.Contains(commandErr.Error(), test.wantCommand) || commandErr.Detail() != "> line one\n> line two" {
				t.Fatalf("error = %#v", err)
			}
			if f.sshCalls != test.failAt+1 || f.streamCalls != 0 || strings.Contains(strings.Join(f.remote, "\n"), "config' 'del") {
				t.Fatalf("ssh=%d stream=%d remote=%v", f.sshCalls, f.streamCalls, f.remote)
			}
			if got := strings.Contains(stdout, "opsctl: ok"); got != test.wantOpsctl || strings.Contains(stdout, "init: ok") {
				t.Fatalf("stdout=%q", stdout)
			}
		})
	}
}

func TestInitUsesOnlyAllowedOperationsAndPreservesWrittenKeys(t *testing.T) {
	// R-ZIF9-2HC7
	const version = "v4.5.6"
	allowed := []string{
		"'curl' '-fsSL' '-o' '" + hostsetup.InstallerPath + "' '" + hostsetup.DownloadURL + "/opsctl/" + version + "/install.sh'",
		"'sudo' 'bash' '" + hostsetup.InstallerPath + "' '" + version + "'",
		"'sudo' 'opsctl' 'config' 'set' 'host.name=sbx1.ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.provider=route53'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78'",
		"'sudo' 'opsctl' 'config' 'set' 'aws.region=us-east-2'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/sbx1/'",
		"'sudo' 'opsctl' 'init'",
	}
	for _, test := range []struct {
		name   string
		failAt int
		want   []string
	}{
		{name: "success", failAt: -1, want: allowed},
		{name: "failed config set", failAt: 4, want: allowed[:5]},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			if test.failAt >= 0 {
				f.sshResults = make([]seam.Result, test.failAt+1)
				f.sshResults[test.failAt].ExitCode = 3
			}
			_, err := f.run("sbx1", "--opsctl="+version)
			if (err != nil) != (test.failAt >= 0) {
				t.Fatalf("error = %v", err)
			}
			if !reflect.DeepEqual(f.remote, test.want) || f.sshCalls != len(test.want) || f.streamCalls != 0 {
				t.Fatalf("remote=%#v, ssh=%d, stream=%d; want %#v", f.remote, f.sshCalls, f.streamCalls, test.want)
			}
			wantOps := []string{"git", "open ikigenba.dev us-east-2", "sts", "spaces ikigenba.dev", "zone ikigenba.dev"}
			if !reflect.DeepEqual(f.ops[:5], wantOps) || len(f.ops) != len(wantOps)+len(test.want) {
				t.Fatalf("operations = %#v", f.ops)
			}
			for _, op := range f.ops[5:] {
				if !strings.HasPrefix(op, "ssh ") {
					t.Fatalf("unexpected operation %q", op)
				}
			}
		})
	}
}

type fixture struct {
	t           *testing.T
	root        string
	ops         []string
	remote      []string
	instances   []cloud.Instance
	sshResults  []seam.Result
	openErr     error
	stsErr      error
	spacesErr   error
	zoneErr     error
	sshCalls    int
	streamCalls int
	cloud.EC2
	cloud.Route53
	cloud.STS
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checkout.RootFilePath), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, root: root, instances: []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}}}
}

func (f *fixture) run(args ...string) (string, error) {
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, f.deps())
	return stdout.String(), err
}

func (f *fixture) deps() seam.Deps {
	return seam.Deps{
		Dir: f.root,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.ops = append(f.ops, "open "+profile+" "+region)
			if f.openErr != nil {
				return cloud.Clients{}, f.openErr
			}
			return cloud.Clients{EC2: f, Route53: f, STS: f}, nil
		},
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			switch cmd.Path {
			case "git":
				f.ops = append(f.ops, "git")
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			case "ssh":
				f.sshCalls++
				remote := cmd.Args[len(cmd.Args)-1]
				f.remote = append(f.remote, remote)
				f.ops = append(f.ops, "ssh "+remote)
				if index := f.sshCalls - 1; index < len(f.sshResults) {
					return f.sshResults[index], nil
				}
				return seam.Result{}, nil
			default:
				f.t.Fatalf("unexpected Exec command: %#v", cmd)
				return seam.Result{}, nil
			}
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			f.streamCalls++
			return seam.Result{}, errors.New("unexpected Stream")
		},
	}
}

func (f *fixture) CallerAccountID(context.Context) (string, error) {
	f.ops = append(f.ops, "sts")
	return "295229566359", f.stsErr
}

func (f *fixture) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	f.ops = append(f.ops, "spaces "+domain)
	return f.instances, f.spacesErr
}

func (f *fixture) Zone(_ context.Context, domain string) (cloud.Zone, error) {
	f.ops = append(f.ops, "zone "+domain)
	return cloud.Zone{ID: "Z09565073GHK8BYWQ1A78", Name: "ikigenba.dev"}, f.zoneErr
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
