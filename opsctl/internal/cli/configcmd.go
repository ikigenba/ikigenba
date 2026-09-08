package cli

import (
	"errors"
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
		return writeConfigUsage(stderr)
	}
	switch args[0] {
	case "get":
		return configGet(args[1:], stdout, stderr, deps)
	case "set":
		return configSet(args[1:], stderr, deps)
	case "del":
		return configDel(args[1:], stderr, deps)
	case "list":
		return configList(args[1:], stdout, stderr, deps)
	default:
		return writeConfigUsage(stderr)
	}
}

func writeConfigUsage(stderr io.Writer) exitCode {
	writePrefixed(stderr, "opsctl config: ", configUsage)
	return exitUsage
}

func configErr(stderr io.Writer, err error) exitCode {
	_, _ = fmt.Fprintf(stderr, "opsctl config: %v\n", err)
	return exitFail
}

func configSet(args []string, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 1 {
		return writeConfigUsage(stderr)
	}
	key, value, found := strings.Cut(args[0], "=")
	if !found {
		_, _ = io.WriteString(stderr, "opsctl config: set requires KEY=VALUE\n")
		return exitUsage
	}
	if err := (config.Store{Root: deps.Root}).Set(key, value); err != nil {
		if errors.Is(err, config.ErrInvalidKey) {
			_, _ = io.WriteString(stderr, "opsctl config: invalid key: "+diagnosticArg(key)+"\n")
			return exitUsage
		}
		if errors.Is(err, config.ErrInvalidValue) {
			_, _ = io.WriteString(stderr, "opsctl config: invalid value\n")
			return exitUsage
		}
		return configErr(stderr, err)
	}
	return exitOK
}

func configGet(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 1 {
		return writeConfigUsage(stderr)
	}
	value, err := (config.Store{Root: deps.Root}).Get(args[0])
	if err != nil {
		if errors.Is(err, config.ErrNotSet) {
			_, _ = io.WriteString(stderr, "opsctl config: key not set: "+diagnosticArg(args[0])+"\n")
			return exitFail
		}
		return configErr(stderr, err)
	}
	if _, err := fmt.Fprintln(stdout, value); err != nil {
		return exitFail
	}
	return exitOK
}

func configDel(args []string, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 1 {
		return writeConfigUsage(stderr)
	}
	if err := (config.Store{Root: deps.Root}).Del(args[0]); err != nil {
		return configErr(stderr, err)
	}
	return exitOK
}

func configList(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 0 {
		return writeConfigUsage(stderr)
	}
	entries, err := (config.Store{Root: deps.Root}).List()
	if err != nil {
		return configErr(stderr, err)
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Key)
		b.WriteByte('=')
		b.WriteString(e.Value)
		b.WriteByte('\n')
	}
	return writeOut(stdout, b.String())
}
