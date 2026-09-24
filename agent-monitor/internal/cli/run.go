package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/codex"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

const usageHint = "\n\nsee 'agent-monitor --help' for usage\n"

// Run handles one invocation and returns its exit code to the caller.
func Run(args []string, sys System, stdout, stderr io.Writer) ExitCode {
	if len(args) == 0 {
		return writeProduct(stdout, stderr, Usage)
	}
	switch args[0] {
	case "--help", "-h":
		return writeProduct(stdout, stderr, Usage)
	case "--version", "-V":
		return writeProduct(stdout, stderr, Version+"\n")
	case "list":
		return runList(args[1:], sys, stdout, stderr)
	case "tree":
		return runTree(args[1:], sys, stdout, stderr)
	default:
		kind := "unknown command"
		if strings.HasPrefix(args[0], "-") {
			kind = "unknown option"
		}
		return usageError(stderr, kind, args[0])
	}
}

func runTree(args []string, sys System, stdout, stderr io.Writer) ExitCode {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return writeProduct(stdout, stderr, TreeUsage)
		}
	}
	if len(args) == 0 {
		writeDiagnostic(stderr, "agent-monitor: missing harness"+usageHint)
		return ExitUsage
	}
	var harness, id string
	place := 0
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageError(stderr, "unknown option", arg)
		}
		switch place {
		case 0:
			switch arg {
			case "claude", "codex", "grok":
				harness = arg
			default:
				return usageError(stderr, "unknown harness", arg)
			}
		case 1:
			id = arg
		default:
			return usageError(stderr, "unexpected argument", arg)
		}
		place++
	}
	if place == 1 {
		writeDiagnostic(stderr, "agent-monitor: missing session id"+usageHint)
		return ExitUsage
	}
	if sys.Home == "" {
		writeDiagnostic(stderr, "agent-monitor: cannot find the home directory: HOME is not set\n")
		return ExitDataUnreadable
	}
	var result tree.Tree
	var err error
	switch harness {
	case "claude":
		result, err = claude.Tree(sys.Root, sys.Home, id)
	case "codex":
		result, err = codex.Tree(sys.Root, sys.Home, id)
	case "grok":
		result, err = grok.Tree(sys.Root, sys.Home, id)
	}
	if err != nil {
		if errors.Is(err, tree.ErrNotFound) {
			writeDiagnostic(stderr, "agent-monitor: no "+harness+" session '"+quote.Arg(id)+"'\n")
			return ExitSessionNotFound
		}
		var readErr *session.ReadError
		if errors.As(err, &readErr) {
			writeDiagnostic(stderr, "agent-monitor: cannot read "+quote.Field(readErr.Path)+": "+readErr.Err.Error()+"\n")
			return ExitDataUnreadable
		}
		writeDiagnostic(stderr, "agent-monitor: cannot read session data: "+err.Error()+"\n")
		return ExitDataUnreadable
	}
	return writeProduct(stdout, stderr, tree.Draw(result))
}

func runList(args []string, sys System, stdout, stderr io.Writer) ExitCode {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return writeProduct(stdout, stderr, ListUsage)
		}
	}
	if len(args) == 0 {
		writeDiagnostic(stderr, "agent-monitor: missing harness"+usageHint)
		return ExitUsage
	}
	var harness string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usageError(stderr, "unknown option", arg)
		}
		if harness == "" {
			switch arg {
			case "claude", "codex", "grok":
				harness = arg
			default:
				return usageError(stderr, "unknown harness", arg)
			}
		} else {
			return usageError(stderr, "unexpected argument", arg)
		}
	}
	if sys.Home == "" {
		writeDiagnostic(stderr, "agent-monitor: cannot find the home directory: HOME is not set\n")
		return ExitDataUnreadable
	}
	var sessions []session.Session
	var err error
	switch harness {
	case "claude":
		sessions, err = claude.List(sys.Root, sys.Home)
	case "codex":
		sessions, err = codex.List(sys.Root, sys.Home)
	case "grok":
		sessions, err = grok.List(sys.Root, sys.Home)
	}
	if err != nil {
		var readErr *session.ReadError
		if errors.As(err, &readErr) {
			writeDiagnostic(stderr, "agent-monitor: cannot read "+quote.Field(readErr.Path)+": "+readErr.Err.Error()+"\n")
		} else {
			writeDiagnostic(stderr, "agent-monitor: cannot read session data: "+err.Error()+"\n")
		}
		return ExitDataUnreadable
	}
	return writeProduct(stdout, stderr, session.Table(sessions))
}

func usageError(stderr io.Writer, kind, arg string) ExitCode {
	writeDiagnostic(stderr, "agent-monitor: "+kind+" '"+quote.Arg(arg)+"'"+usageHint)
	return ExitUsage
}

func writeDiagnostic(stderr io.Writer, message string) {
	_, _ = stderr.Write([]byte(message))
}

func writeProduct(stdout, stderr io.Writer, product string) ExitCode {
	_, err := stdout.Write([]byte(product))
	if err != nil {
		writeDiagnostic(stderr, "agent-monitor: write error: "+err.Error()+"\n")
		return ExitWriteFailed
	}
	return ExitSuccess
}
