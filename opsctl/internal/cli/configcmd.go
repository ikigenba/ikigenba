package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

func runConfig(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 0 {
		return exitUsage
	}
	switch args[0] {
	case "set":
		if code := requireRoot(deps, stderr); code != 0 {
			return code
		}
		return configSet(args[1:], stderr, deps)
	case "get":
		if code := requireRoot(deps, stderr); code != 0 {
			return code
		}
		return configGet(args[1:], stdout, stderr, deps)
	default:
		return exitUsage
	}
}

func requireRoot(deps Deps, stderr io.Writer) exitCode {
	if deps.EUID == 0 {
		return exitOK
	}
	_, _ = io.WriteString(stderr, "opsctl: must run as root\n")
	return exitRefused
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
