package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const expectedBuildUsage = `Usage: devctl build <sha|tag>

Build the suite at <sha|tag> for linux/amd64 and write dist/<sha>.tar.xz, one
release holding every app and opsctl. <sha> is the full commit sha the argument
resolves to; the working tree is not read.
`

func TestBuildUsageDiagnosticsThroughCLI(t *testing.T) {
	// R-QZMJ-NVKY
	// R-R0UG-1NBN
	// R-6IIK-SRYP
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", args: []string{"build"}, want: "devctl: build needs <sha|tag>\n\nsee 'devctl build --help' for usage\n"},
		{name: "extra", args: []string{"build", "crm", "api"}, want: "devctl: build takes one <sha|tag>\n\nsee 'devctl build --help' for usage\n"},
		{name: "unknown before app", args: []string{"build", "--force", "crm"}, want: "devctl: unknown option '--force'\n\nsee 'devctl build --help' for usage\n"},
		{name: "unknown after app", args: []string{"build", "crm", "-v"}, want: "devctl: unknown option '-v'\n\nsee 'devctl build --help' for usage\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			deps := seam.Deps{EUID: 1, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				calls++
				return seam.Result{}, errors.New("unexpected process")
			}}
			assertResult(t, invokeWithDeps(deps, test.args...), 2, "", test.want)
			if calls != 0 {
				t.Fatalf("Exec calls = %d, want zero", calls)
			}
		})
	}
}

func TestBuildHelpThroughCLIHasNoExternalOperation(t *testing.T) {
	// R-6DMZ-9OZX
	for _, args := range [][]string{
		{"build", "--help"},
		{"build", "-h"},
		{"build", "crm", "--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			execCalls := 0
			cloudCalls := 0
			result := invokeWithDeps(seam.Deps{
				EUID: 1,
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					execCalls++
					return seam.Result{}, errors.New("unexpected Exec call")
				},
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, errors.New("unexpected Cloud call")
				},
			}, args...)
			assertResult(t, result, 0, expectedBuildUsage, "")
			if execCalls != 0 || cloudCalls != 0 {
				t.Fatalf("external calls = Exec %d, Cloud %d; want zero", execCalls, cloudCalls)
			}
		})
	}
}
