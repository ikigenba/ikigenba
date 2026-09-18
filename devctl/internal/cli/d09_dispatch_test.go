package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const expectedDeployUsage = `Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
`

func TestCLIDispatchesDeployArgumentsAndStdout(t *testing.T) {
	// R-Z4J0-Z7NA
	deps := seam.Deps{
		EUID: 1,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			t.Fatal("deploy help called Exec")
			return seam.Result{}, errors.New("unreachable")
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("deploy help called Cloud")
			return cloud.Clients{}, errors.New("unreachable")
		},
	}
	assertResult(t, invokeWithDeps(deps, "deploy", "crm.example", "artifact.tar.xz", "--help"), 0, expectedDeployUsage, "")
	assertResult(
		t,
		invokeWithDeps(deps, "--account", "SelectedProfile", "deploy", "crm.example"),
		2,
		"",
		"devctl: deploy needs <domain> and <file>\n\nsee 'devctl deploy --help' for usage\n",
	)
}

type d09EC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (fake *d09EC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return fake.instances, nil
}

func TestCLIDispatchesRestoreWithProfileDepsAndStdout(t *testing.T) {
	// R-FSS4-QJSW
	const profile = "SelectedProfile"
	const domain = "restore.example.test"
	var profiles []string
	var commands []seam.Cmd
	deps := seam.Deps{
		EUID: 1,
		Dir:  t.TempDir(),
		Cloud: func(_ context.Context, gotProfile, region string) (cloud.Clients, error) {
			profiles = append(profiles, gotProfile)
			switch region {
			case "":
				return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
			case "us-test-1":
				return cloud.Clients{EC2: &d09EC2{instances: []cloud.Instance{{
					ID:      "i-restore",
					Space:   domain,
					State:   cloud.StateRunning,
					Address: "192.0.2.44",
				}}}}, nil
			default:
				t.Fatalf("unexpected region %q", region)
				return cloud.Clients{}, errors.New("unreachable")
			}
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command)
			return seam.Result{}, nil
		},
	}

	result := invokeWithDeps(deps, "--account", profile, "restore", domain, "crm")
	assertResult(t, result, 0, "restore: ok (opsctl restore crm)\n", "")
	if !reflect.DeepEqual(profiles, []string{profile, profile}) {
		t.Fatalf("Cloud profiles = %#v, want selected profile twice", profiles)
	}
	if len(commands) != 1 || commands[0].Path != "ssh" {
		t.Fatalf("commands = %#v, want one ssh command", commands)
	}
}
