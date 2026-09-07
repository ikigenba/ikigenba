package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

const configUsage = `Usage: opsctl config <subcommand> [arguments]

Read and write the host configuration store (/etc/ikigenba/config.json).

Subcommands:
  get KEY        print the value of KEY; exit 1 if KEY is not set
  set KEY=VALUE  set KEY to VALUE, creating or replacing it
  del KEY        remove KEY; succeeds whether or not KEY is set
  list           print every KEY=VALUE, one per line, sorted by key

Keys match ^[a-z0-9_.-]+$. Values may not contain newlines.
`

func runConfig(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, configUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) == 0 {
		return exitUsage
	}
	switch args[0] {
	case "set":
		return configSet(args[1:], stderr, deps)
	case "get":
		return configGet(args[1:], stdout, stderr, deps)
	default:
		return exitUsage
	}
}

func configSet(args []string, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 1 {
		return exitUsage
	}
	key, value, found := strings.Cut(args[0], "=")
	if !found {
		return exitUsage
	}
	if err := (config.Store{Root: deps.Root}).Set(key, value); err != nil {
		_, _ = fmt.Fprintf(stderr, "opsctl config: %v\n", err)
		return exitFail
	}
	return exitOK
}

func configGet(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 1 {
		return exitUsage
	}
	value, err := (config.Store{Root: deps.Root}).Get(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "opsctl config: %v\n", err)
		return exitFail
	}
	if _, err := fmt.Fprintln(stdout, value); err != nil {
		return exitFail
	}
	return exitOK
}
