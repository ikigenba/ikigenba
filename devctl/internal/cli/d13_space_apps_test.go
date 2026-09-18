package cli

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantSpaceRestartUsage = `Usage: devctl --account <name> space restart <domain> <app>

Have opsctl restart one app's service. Deploy the existing file to apply pushed
secrets; a restart uses the environment already installed on the host.
`

const wantSpaceLogsUsage = `Usage: devctl --account <name> space logs <domain> <app> [--since <when>] [--follow]

Print the last 100 journal lines for an installed app. With --since, print all
lines from that moment using journalctl's time syntax.

Options:
  --since <when>   read from this moment; passed unchanged to journalctl
  --follow         stream new lines until interrupted
`

func TestSpaceRestartHelpBeforeAccountAndExternalAccess(t *testing.T) {
	// R-H5K4-1DGO
	assertSpaceAppHelp(t, "restart", wantSpaceRestartUsage)
}

func TestSpaceLogsHelpBeforeAccountAndExternalAccess(t *testing.T) {
	// R-H7ZW-SWY2
	assertSpaceAppHelp(t, "logs", wantSpaceLogsUsage)
}

func assertSpaceAppHelp(t *testing.T, command, want string) {
	t.Helper()
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			external := 0
			unexpected := errors.New("unexpected external access")
			deps := seam.Deps{
				EUID: 1,
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					external++
					return cloud.Clients{}, unexpected
				},
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					external++
					return seam.Result{}, unexpected
				},
				Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
					external++
					return seam.Result{}, unexpected
				},
			}

			assertResult(t, invokeWithDeps(deps, "space", command, option), 0, want, "")
			if external != 0 {
				t.Fatalf("external calls = %d, want none", external)
			}

			assertResult(t, invokeWithDeps(seam.Deps{EUID: 0}, "space", command, option),
				3, "", "devctl: must not run as root\n")
		})
	}
}
