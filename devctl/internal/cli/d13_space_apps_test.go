package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantSpaceRestartUsage = `Usage: devctl space restart <space> <app>

Have opsctl restart one app's service. Deploy the existing file to apply pushed
secrets; a restart uses the environment already installed on the host.
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
	// R-0QP0-DXBO R-0RWW-RP2D
	for _, tc := range []struct{ subcommand, option, want string }{
		{"restart", "--help", wantSpaceRestartUsage}, {"restart", "-h", wantSpaceRestartUsage},
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
	// R-0PH4-05KZ
	deps := seam.Deps{EUID: 1, Dir: "/outside"}
	assertResult(t, invokeWithDeps(deps, "space", "restart", "sbx1"), 2, "", "devctl: space restart needs <space> and <app>\n\nsee 'devctl space --help' for usage\n")
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
	// R-0T4T-5GT2 R-0UCP-J8JR
	for _, subcommand := range []string{"restart", "logs"} {
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
	root, work string
	instances  []cloud.Instance
	hostCalls  int
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
			return seam.Result{}, errors.New("unexpected host")
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			f.hostCalls++
			return seam.Result{}, errors.New("unexpected host stream")
		},
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
