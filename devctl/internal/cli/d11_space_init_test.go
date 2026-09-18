package cli

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantSpaceInitUsage = `Usage: devctl --account <name> space init <domain> [--opsctl <version>] [--acme-email <address>]

Set the host's nine derived keys again and run opsctl init. Keep its installed
opsctl and email unless an option names a replacement. Cloud resources and
secrets are unchanged.

Options:
  --opsctl <version>      install this release using the host's saved installer
  --acme-email <address>  replace the CA contact address
`

func TestSpaceInitHelpThroughCLIWithoutAccountOrExternalOperation(t *testing.T) {
	// R-GM1P-X1LK
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			externalCalls := 0
			deps := spaceInitSentinelDeps(&externalCalls)

			assertResult(t, invokeWithDeps(deps, "space", "init", option), 0, wantSpaceInitUsage, "")
			if externalCalls != 0 {
				t.Fatalf("external operations = %d, want none", externalCalls)
			}
		})
	}
}

func TestSpaceInitHelpObeysRootRefusalBeforeExternalOperation(t *testing.T) {
	// R-GM1P-X1LK
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			externalCalls := 0
			deps := spaceInitSentinelDeps(&externalCalls)
			deps.EUID = 0

			assertResult(t, invokeWithDeps(deps, "space", "init", option), 3, "", "devctl: must not run as root\n")
			if externalCalls != 0 {
				t.Fatalf("external operations = %d, want none", externalCalls)
			}
		})
	}
}

func spaceInitSentinelDeps(calls *int) seam.Deps {
	unexpected := errors.New("unexpected external operation")
	return seam.Deps{
		EUID: 1,
		Getenv: func(string) string {
			(*calls)++
			return ""
		},
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
