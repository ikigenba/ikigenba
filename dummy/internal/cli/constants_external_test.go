package cli_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/cli"
)

// R-092E-Q4T0
// These declarations must compile from a package importing cli.
const (
	externalExitSuccess      = cli.ExitSuccess
	externalExitServerFailed = cli.ExitServerFailed
	externalExitUsage        = cli.ExitUsage
)

func TestExportedExitConstants(t *testing.T) {
	t.Parallel()

	if externalExitSuccess != 0 || externalExitServerFailed != 1 || externalExitUsage != 2 {
		t.Errorf("exit codes = (%d, %d, %d), want (0, 1, 2)",
			externalExitSuccess, externalExitServerFailed, externalExitUsage)
	}
}
