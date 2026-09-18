package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantRemoveUsage = `Usage: devctl --account <name> remove <domain> <app>

Have opsctl on <domain> take <app> off the space: stop and remove its service,
remove its binary and configuration, and stop routing its name. Its state/ is
kept on the host and its secrets are kept in the account, so a later deploy of
<app> lands over its data. What remove does on the host is opsctl's.
`

func TestRemoveHelpBeforeAccountAndExternalAccess(t *testing.T) {
	// R-GUL0-LFSF
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			external := 0
			deps := seam.Deps{
				EUID: 1,
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					external++
					return cloud.Clients{}, errors.New("unexpected cloud access")
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					external++
					return seam.Result{}, errors.New("unexpected process access")
				},
			}

			assertResult(t, invokeWithDeps(deps, "remove", option), 0, wantRemoveUsage, "")
			if external != 0 {
				t.Fatalf("external calls = %d, want none", external)
			}
		})
	}
}

func TestCLIDispatchesRemoveArgumentsStdoutDepsAndProfile(t *testing.T) {
	// R-GX0T-CZ9T
	const profile = "Selected Profile"
	const domain = "remove.example.test"
	const app = "crm"
	const dir = "/injected/work"
	var profiles []string
	var commands []seam.Cmd
	deps := seam.Deps{
		EUID: 1,
		Dir:  dir,
		Cloud: func(_ context.Context, gotProfile, region string) (cloud.Clients, error) {
			profiles = append(profiles, gotProfile)
			switch region {
			case "":
				return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
			case "us-test-1":
				return cloud.Clients{EC2: &d12EC2{instances: []cloud.Instance{{
					ID:      "i-remove",
					Space:   domain,
					State:   cloud.StateRunning,
					Address: "192.0.2.48",
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

	result := invokeWithDeps(deps, "--account", profile, "remove", domain, app)
	assertResult(t, result, 0, "remove: ok (opsctl uninstalled crm)\n", "")
	if !reflect.DeepEqual(profiles, []string{profile, profile}) {
		t.Fatalf("Cloud profiles = %#v, want selected profile twice", profiles)
	}
	wantCommands := []seam.Cmd{{
		Path: "ssh",
		Args: []string{
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=accept-new",
			"ec2-user@192.0.2.48",
			"'sudo' 'opsctl' 'uninstall' 'crm'",
		},
		Dir: dir,
	}}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
}

type d12EC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (fake *d12EC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return fake.instances, nil
}
