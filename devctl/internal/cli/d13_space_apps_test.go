package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantSpaceRestartUsage = `Usage: devctl space restart <space> <app>

Have opsctl restart one app's service. Deploy the existing file to apply pushed
secrets; a restart uses the environment already installed on the host.
`

const wantSpaceDisableUsage = `Usage: devctl space disable <space> <app>

Have opsctl take one app offline: stop its socket and service and keep both
from starting until 'devctl space enable'. Its release, data and units stay
on the host, and deploy, restore, init and restart leave it disabled.
`

const wantSpaceEnableUsage = `Usage: devctl space enable <space> <app>

Have opsctl bring a disabled app back: enable and start its socket and
service. Enabling an app that is already enabled changes nothing.
`

const wantSpaceLogsUsage = `Usage: devctl space logs <space> <app> [--follow] [--since <when>]

Print the last 100 journal lines of one app's service on the space's host. With
--since, print every line from that moment instead; with --follow, keep printing
until interrupted. The two combine.

Options:
  --follow         keep printing as the app writes, until interrupted
  --since <when>   start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00
`

func TestSpaceAppHelpBeforeExternalAccess(t *testing.T) {
	// R-0QP0-DXBO R-0RWW-RP2D R-JRBH-IECV R-JSJD-W63K
	for _, tc := range []struct{ subcommand, option, want string }{
		{"restart", "--help", wantSpaceRestartUsage}, {"restart", "-h", wantSpaceRestartUsage},
		{"disable", "--help", wantSpaceDisableUsage}, {"disable", "-h", wantSpaceDisableUsage},
		{"enable", "--help", wantSpaceEnableUsage}, {"enable", "-h", wantSpaceEnableUsage},
		{"logs", "--help", wantSpaceLogsUsage}, {"logs", "-h", wantSpaceLogsUsage},
	} {
		calls := 0
		deps := seam.Deps{EUID: 1, Dir: "/outside",
			Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				calls++
				return cloud.Clients{}, errors.New("unexpected")
			},
			Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				calls++
				return seam.Result{}, errors.New("unexpected")
			},
			Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
				calls++
				return seam.Result{}, errors.New("unexpected")
			},
		}
		assertResult(t, invokeWithDeps(deps, "space", tc.subcommand, tc.option), 0, tc.want, "")
		if calls != 0 {
			t.Fatalf("%s %s calls = %d", tc.subcommand, tc.option, calls)
		}
	}
}

func TestSpaceAppSyntaxThroughCLI(t *testing.T) {
	// R-JL7Z-LJNE R-JK03-7RWP
	deps := seam.Deps{EUID: 1, Dir: "/outside"}
	assertResult(t, invokeWithDeps(deps, "space", "restart", "sbx1"), 2, "", "devctl: space restart needs <space> and <app>\n\nsee 'devctl space --help' for usage\n")
	for _, tc := range []struct {
		args  []string
		first string
	}{
		{[]string{"disable", "sbx1"}, "space disable needs <space> and <app>"},
		{[]string{"enable", "sbx1"}, "space enable needs <space> and <app>"},
		{[]string{"disable", "sbx1", "crm", "extra"}, "space disable takes only <space> and <app>"},
		{[]string{"enable", "sbx1", "crm", "--now"}, "unknown option '--now'"},
	} {
		args := append([]string{"space"}, tc.args...)
		assertResult(t, invokeWithDeps(deps, args...), 2, "", "devctl: "+tc.first+"\n\nsee 'devctl space --help' for usage\n")
	}
	result := invokeWithDeps(deps, "space", "logs", "sbx1")
	if result.code != 2 || result.stdout != "" || result.stderr != "devctl: space logs needs <space> and <app>\n\nsee 'devctl space --help' for usage\n" {
		t.Fatalf("logs syntax = %#v", result)
	}
	result = invokeWithDeps(deps, "space", "logs", "sbx1", "crm", "--since")
	if result.code != 2 || result.stdout != "" || result.stderr != "devctl: option '--since' requires a value\n\nsee 'devctl space --help' for usage\n" {
		t.Fatalf("since syntax = %#v", result)
	}
}

func TestSpaceAppResolutionThroughCLI(t *testing.T) {
	// R-JMFV-ZBE3
	for _, subcommand := range []string{"restart", "disable", "enable", "logs"} {
		missing := newD13Fixture(t, nil)
		assertResult(t, invokeWithDeps(missing.deps(), "space", subcommand, "gone", "crm"), 1, "", "devctl: no space at 'gone.ikigenba.dev'\n")
		if missing.hostCalls != 0 {
			t.Fatalf("%s missing host calls = %d", subcommand, missing.hostCalls)
		}
		stopped := newD13Fixture(t, []cloud.Instance{{Space: "sbx2.ikigenba.dev", State: cloud.StateStopped}})
		assertResult(t, invokeWithDeps(stopped.deps(), "space", subcommand, "sbx2", "crm"), 1, "", "devctl: 'sbx2.ikigenba.dev' is stopped\n")
		if stopped.hostCalls != 0 {
			t.Fatalf("%s stopped host calls = %d", subcommand, stopped.hostCalls)
		}
	}
}

type d13Fixture struct {
	root, work  string
	instances   []cloud.Instance
	hostCalls   int
	hostEnabled bool
	hostResult  seam.Result
	hostCmds    []seam.Cmd
}

func newD13Fixture(t *testing.T, instances []cloud.Instance) *d13Fixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checkout.RootFilePath), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	return &d13Fixture{root: root, work: work, instances: instances}
}

func (f *d13Fixture) deps() seam.Deps {
	return seam.Deps{EUID: 1, Dir: f.work,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			return cloud.Clients{STS: d13STS{}, EC2: d13EC2{instances: f.instances}}, nil
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path == "git" {
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			f.hostCalls++
			f.hostCmds = append(f.hostCmds, command)
			if !f.hostEnabled {
				return seam.Result{}, errors.New("unexpected host")
			}
			return f.hostResult, nil
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			f.hostCalls++
			return seam.Result{}, errors.New("unexpected host stream")
		},
	}
}

func TestSpaceOpsctlResultsThroughCLI(t *testing.T) {
	// R-SBQG-TBGD R-SGM2-CEF5 R-SK9R-HPN8 R-JNNS-D34S
	for _, tc := range []struct {
		command string
		output  string
	}{
		{"restart", "a\n"},
		{"disable", "a\nb\n"},
		{"enable", "a\nb\nc\n"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			f := newD13Fixture(t, []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}})
			f.hostEnabled = true
			f.hostResult = seam.Result{Stdout: []byte(tc.output), Stderr: []byte("ignored\n")}
			assertResult(t, invokeWithDeps(f.deps(), "space", tc.command, "sbx1", "crm"), 0, tc.output, "")
			want := []seam.Cmd{{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@18.118.7.42", "'sudo' 'opsctl' '" + tc.command + "' 'crm'"}, Dir: f.work}}
			if !reflect.DeepEqual(f.hostCmds, want) {
				t.Fatalf("host commands = %#v", f.hostCmds)
			}
			f.hostResult = seam.Result{Stdout: []byte("a")}
			assertResult(t, invokeWithDeps(f.deps(), "space", tc.command, "sbx1", "crm"), 0, "a", "")
			f.hostResult = seam.Result{ExitCode: 1, Stdout: []byte("bad\nmore\n"), Stderr: []byte("error\nmore\n")}
			assertResult(t, invokeWithDeps(f.deps(), "space", tc.command, "sbx1", "crm"), 1, "", "devctl: "+tc.command+": ssh ec2-user@18.118.7.42 sudo opsctl "+tc.command+" crm: exit status 1\n\n> bad\n> more\n> error\n> more\n")
		})
	}
}

type d13STS struct{ cloud.STS }

func (d13STS) CallerAccountID(context.Context) (string, error) { return "123", nil }

type d13EC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (f d13EC2) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return append([]cloud.Instance(nil), f.instances...), nil
}
