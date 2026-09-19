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

const wantSpaceInitUsage = `Usage: devctl space init <space> [--opsctl <version>] [--acme-email <address>]

Set the host's five derived keys again and run opsctl init. Keep its installed
opsctl, its CA address, and its backup periods unless an option names a
replacement. Cloud resources, records, and secrets are unchanged.

Options:
  --opsctl <version>      move the host to this opsctl release first
  --acme-email <address>  change where the CA sends the space's expiry warnings
`

func TestSpaceInitArgumentDiagnosticsThroughCLI(t *testing.T) {
	// R-OOTR-U1OR
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"space", "init"}, "devctl: space init needs <space>\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "init", "sbx1", "--opsctl"}, "devctl: option '--opsctl' requires a value\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "init", "sbx1", "--acme-email"}, "devctl: option '--acme-email' requires a value\n\nsee 'devctl space --help' for usage\n"},
	}
	for _, test := range tests {
		calls := 0
		result := invokeWithDeps(spaceInitSentinelDeps(&calls), test.args...)
		assertResult(t, result, 2, "", test.want)
		if calls != 0 {
			t.Fatalf("%q made %d external calls", test.args, calls)
		}
	}
}

func TestSpaceInitHelpThroughCLIOutsideCheckout(t *testing.T) {
	// R-OQ1O-7TFG
	for _, option := range []string{"--help", "-h"} {
		calls := 0
		deps := spaceInitSentinelDeps(&calls)
		deps.Dir = t.TempDir()
		assertResult(t, invokeWithDeps(deps, "space", "init", option), 0, wantSpaceInitUsage, "")
		if calls != 0 {
			t.Fatalf("%s made %d external calls", option, calls)
		}

		deps.EUID = 0
		assertResult(t, invokeWithDeps(deps, "space", "init", option), 3, "", "devctl: must not run as root\n")
	}
}

func TestSpaceInitLookupRefusalsThroughCLI(t *testing.T) {
	// R-OR9K-LL65
	t.Run("absent", func(t *testing.T) {
		h := newD11Harness(t)
		h.instances = nil
		assertResult(t, invokeWithDeps(h.deps(), "space", "init", "gone"), 1, "", "devctl: no space at 'gone.ikigenba.dev'\n")
		if h.sshCalls != 0 {
			t.Fatalf("ssh calls = %d", h.sshCalls)
		}
	})
	t.Run("stopped", func(t *testing.T) {
		h := newD11Harness(t)
		h.instances[0].State = cloud.StateStopped
		assertResult(t, invokeWithDeps(h.deps(), "space", "init", "sbx2"), 1, "", "devctl: 'sbx2.ikigenba.dev' is stopped\n")
		if h.sshCalls != 0 {
			t.Fatalf("ssh calls = %d", h.sshCalls)
		}
	})
}

func TestSpaceInitFailureRelaysHostDiagnosticThroughCLI(t *testing.T) {
	// R-OSHG-ZCWU R-OW56-4O4X
	h := newD11Harness(t)
	h.sshResults = make([]seam.Result, 7)
	h.sshResults[0].Stdout = []byte("v3.2.1\n")
	h.sshResults[6] = seam.Result{ExitCode: 2, Stdout: []byte("arbitrary output\nsecond line\n")}
	wantStdout := "account: ok (ikigenba.dev, us-east-2, 295229566359)\n" +
		"domain: ok (zone ikigenba.dev Z09565073GHK8BYWQ1A78)\n" +
		"instance: ok (i-0c9e94542d98846a8 running, 18.118.7.42)\n" +
		"opsctl: ok (v3.2.1 kept, 5 keys set)\n"
	wantStderr := "devctl: init: ssh ec2-user@18.118.7.42 sudo opsctl init: exit status 2\n\n> arbitrary output\n> second line\n"
	assertResult(t, invokeWithDeps(h.deps(), "space", "init", "sbx1"), 1, wantStdout, wantStderr)
	if h.sshCalls != 7 || h.streamCalls != 0 || h.remote[6] != "'sudo' 'opsctl' 'init'" {
		t.Fatalf("ssh=%d stream=%d remote=%v", h.sshCalls, h.streamCalls, h.remote)
	}
}

func spaceInitSentinelDeps(calls *int) seam.Deps {
	unexpected := errors.New("unexpected external operation")
	return seam.Deps{
		EUID:   1,
		Getenv: func(string) string { (*calls)++; return "" },
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			(*calls)++
			return cloud.Clients{}, unexpected
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, unexpected
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, unexpected
		},
	}
}

type d11Harness struct {
	t           *testing.T
	root        string
	instances   []cloud.Instance
	sshResults  []seam.Result
	remote      []string
	sshCalls    int
	streamCalls int
	cloud.EC2
	cloud.Route53
	cloud.STS
}

func newD11Harness(t *testing.T) *d11Harness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checkout.RootFilePath), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &d11Harness{t: t, root: root, instances: []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: "sbx2.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}, {ID: "i-0c9e94542d98846a8", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}}}
}

func (h *d11Harness) deps() seam.Deps {
	return seam.Deps{
		EUID: 1,
		Dir:  h.root,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			return cloud.Clients{EC2: h, Route53: h, STS: h}, nil
		},
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			if cmd.Path == "git" {
				return seam.Result{Stdout: []byte(h.root + "\n")}, nil
			}
			if cmd.Path != "ssh" {
				h.t.Fatalf("unexpected command: %#v", cmd)
			}
			h.sshCalls++
			h.remote = append(h.remote, cmd.Args[len(cmd.Args)-1])
			if index := h.sshCalls - 1; index < len(h.sshResults) {
				return h.sshResults[index], nil
			}
			return seam.Result{}, nil
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			h.streamCalls++
			return seam.Result{}, errors.New("unexpected Stream")
		},
	}
}

func (*d11Harness) CallerAccountID(context.Context) (string, error) {
	return "295229566359", nil
}

func (h *d11Harness) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return h.instances, nil
}

func (*d11Harness) Zone(context.Context, string) (cloud.Zone, error) {
	return cloud.Zone{ID: "Z09565073GHK8BYWQ1A78", Name: "ikigenba.dev"}, nil
}
