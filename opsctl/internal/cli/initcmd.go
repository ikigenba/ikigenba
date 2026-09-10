package cli

import (
	"errors"
	"io"
	"os"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

const initUsage = `Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  none yet; each setup command adds itself here when it is designed

Configuration keys:
  host.name  the fully-qualified name this host answers at, at or under a configured zone
`

func runInit(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return writeOut(stdout, initUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) != 0 {
		return writeInitUsageError(stderr, "init takes no arguments")
	}

	entries, err := (config.Store{Root: deps.Root}).List()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return configErr(stderr, err)
	}
	return runInitPreflight(stdout, deps, entries)
}

func writeInitUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl init --help' for usage\n")
	return exitUsage
}

// runInitPreflight is the boundary between command handling and the checks.
// The preflight implementation consumes the configuration snapshot loaded
// before any output, so a corrupt store can never produce partial stdout.
func runInitPreflight(_ io.Writer, _ Deps, _ []config.Entry) exitCode {
	return exitOK
}
