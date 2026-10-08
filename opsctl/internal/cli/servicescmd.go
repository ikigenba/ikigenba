package cli

import (
	"context"
	"errors"
	"io"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/services"
)

const servicesUsage = `Usage: opsctl services <subcommand>

Generate /run/ikigenba/services.json, the services file every app reads
through IKIGENBA_SERVICES, from the release /opt/ikigenba/current names,
host.name, and which apps systemd reports disabled. The file is generated,
never edited.

Subcommands:
  apply  write the file; with no current release, write an empty list

Configuration keys:
  host.name  the fully-qualified name this host answers at

ikigenba-services.service runs 'opsctl services apply' at boot, before any
app starts, so the file is there again after the host restarts.
`

func runServices(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, servicesUsage)
	}
	message := ""
	switch {
	case len(args) == 0:
		message = "no services subcommand given"
	case args[0] != "apply":
		message = "unknown services subcommand '" + diagnosticArg(args[0]) + "'"
	case len(args) != 1:
		message = "services apply takes no arguments"
	}
	if message != "" {
		_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl services --help' for usage\n")
		return exitUsage
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	name, err := (config.Store{Root: deps.Root}).Get("host.name")
	if err != nil && !errors.Is(err, config.ErrNotSet) {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	if err := services.Apply(context.Background(), env, host.NormalizeName(name)); err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	return exitOK
}
