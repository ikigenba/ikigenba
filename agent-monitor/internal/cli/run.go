package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
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
		if sys.Terminal && sys.StdinTerminal {
			return runBrowse(sys, stdout, stderr)
		}
		return writeProduct(stdout, stderr, Usage)
	}
	switch args[0] {
	case "--help", "-h":
		return writeProduct(stdout, stderr, Usage)
	case "--version", "-V":
		return writeProduct(stdout, stderr, Version+"\n")
	case "list":
		return runCommand("list", args[1:], sys, stdout, stderr)
	case "tree":
		return runCommand("tree", args[1:], sys, stdout, stderr)
	case "chat":
		return runCommand("chat", args[1:], sys, stdout, stderr)
	default:
		kind := "unknown command"
		if strings.HasPrefix(args[0], "-") {
			kind = "unknown option"
		}
		return usageError(stderr, kind, args[0])
	}
}

type parsedCommand struct {
	kind, harness, id, agent string
	noColor, follow          bool
}

type renderedCommand struct {
	view       string
	transcript *chat.Transcript
	entries    []chat.Entry
}

func runCommand(kind string, args []string, sys System, stdout, stderr io.Writer) ExitCode {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			usage := ListUsage
			if kind == "tree" {
				usage = TreeUsage
			}
			if kind == "chat" {
				usage = ChatUsage
			}
			return writeProduct(stdout, stderr, usage)
		}
	}
	command := parsedCommand{kind: kind}
	place := 0
	for _, arg := range args {
		if arg == "-f" || arg == "--follow" {
			command.follow = true
			continue
		}
		if kind == "tree" && arg == "--no-color" {
			command.noColor = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return usageError(stderr, "unknown option", arg)
		}
		switch place {
		case 0:
			switch arg {
			case "claude", "codex", "grok":
				command.harness = arg
			default:
				return usageError(stderr, "unknown harness", arg)
			}
		case 1:
			if kind == "list" {
				return usageError(stderr, "unexpected argument", arg)
			}
			command.id = arg
		case 2:
			if kind != "chat" {
				return usageError(stderr, "unexpected argument", arg)
			}
			command.agent = arg
		default:
			return usageError(stderr, "unexpected argument", arg)
		}
		place++
	}
	if place == 0 {
		writeDiagnostic(stderr, "agent-monitor: missing harness"+usageHint)
		return ExitUsage
	}
	if kind != "list" && place == 1 {
		writeDiagnostic(stderr, "agent-monitor: missing session id"+usageHint)
		return ExitUsage
	}
	if kind == "chat" && place == 2 {
		command.agent = command.id
	}
	if sys.Home == "" {
		writeDiagnostic(stderr, "agent-monitor: cannot find the home directory: HOME is not set\n")
		return ExitDataUnreadable
	}
	if command.follow {
		return followCommand(command, sys, stdout, stderr)
	}
	result, err := renderCommand(command, sys)
	if err != nil {
		return reportHarnessError(command, stderr, err)
	}
	if kind == "chat" {
		var product strings.Builder
		for _, entry := range result.entries {
			product.WriteString(chat.Format(entry))
		}
		product.WriteString(chat.TotalsLine(result.transcript.Usage(), result.transcript.Recorded()))
		result.view = product.String()
	}
	return writeProduct(stdout, stderr, result.view)
}

func renderCommand(command parsedCommand, sys System) (renderedCommand, error) {
	var result renderedCommand
	switch command.kind {
	case "list":
		var sessions []session.Session
		var err error
		switch command.harness {
		case "claude":
			sessions, err = claude.List(sys.Root, sys.Home)
		case "codex":
			sessions, err = codex.List(sys.Root, sys.Home)
		case "grok":
			sessions, err = grok.List(sys.Root, sys.Home)
		}
		if err != nil {
			return result, err
		}
		result.view = session.Table(sessions)
	case "tree":
		var value tree.Tree
		var err error
		switch command.harness {
		case "claude":
			value, err = claude.Tree(sys.Root, sys.Home, command.id)
		case "codex":
			value, err = codex.Tree(sys.Root, sys.Home, command.id)
		case "grok":
			value, err = grok.Tree(sys.Root, sys.Home, command.id)
		}
		if err != nil {
			return result, err
		}
		color := sys.Terminal && sys.NoColor == "" && sys.Term != "dumb" && !command.noColor
		result.view = tree.Draw(value, color)
	case "chat":
		var err error
		switch command.harness {
		case "claude":
			result.transcript, result.entries, err = claude.Chat(sys.Root, sys.Home, command.id, command.agent)
		case "codex":
			result.transcript, result.entries, err = codex.Chat(sys.Root, sys.Home, command.id, command.agent)
		case "grok":
			result.transcript, result.entries, err = grok.Chat(sys.Root, sys.Home, command.id, command.agent)
		}
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func reportHarnessError(command parsedCommand, stderr io.Writer, err error) ExitCode {
	if errors.Is(err, tree.ErrNotFound) {
		writeDiagnostic(stderr, "agent-monitor: no "+command.harness+" session '"+quote.Arg(command.id)+"'\n")
		return ExitNotFound
	}
	if errors.Is(err, chat.ErrAgentNotFound) {
		writeDiagnostic(stderr, "agent-monitor: no "+command.harness+" agent '"+quote.Arg(command.agent)+"' in session '"+quote.Arg(command.id)+"'\n")
		return ExitNotFound
	}
	var readErr *session.ReadError
	if errors.As(err, &readErr) {
		writeDiagnostic(stderr, "agent-monitor: cannot read "+quote.Field(readErr.Path)+": "+readErr.Err.Error()+"\n")
	} else {
		writeDiagnostic(stderr, "agent-monitor: cannot read session data: "+err.Error()+"\n")
	}
	return ExitDataUnreadable
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
