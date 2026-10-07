// Package cli implements the in-process sandbox command entry point.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// Version is the source-declared version reported by sandbox.
var Version = "v0.2.0"

type invocation struct {
	ctx            context.Context
	stdin          io.Reader
	stdout, stderr io.Writer
	deps           seam.Deps
}

// Run executes args, excluding the program name, and returns the command's exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	command, text, usage := parse(args, Version)
	if usage != nil {
		return writeUsage(stderr, usage)
	}
	if text != "" {
		_, _ = fmt.Fprint(stdout, text)
		return 0
	}
	if deps.EUID == 0 {
		_, _ = fmt.Fprintln(stderr, "sandbox: refusing to run as root")
		return 3
	}
	if deps.Getenv == nil {
		deps.Getenv = func(string) string { return "" }
	}
	if deps.Exec == nil {
		deps.Exec = seam.Exec
	}
	if deps.Stream == nil {
		deps.Stream = seam.Stream
	}
	call := invocation{ctx: ctx, stdin: stdin, stdout: stdout, stderr: stderr, deps: deps}
	switch command.name {
	case "up":
		return call.runUp()
	case "url":
		return call.runURL()
	case "down":
		return call.runDown(command.operand, command.operandGiven)
	case "wipe":
		return call.runWipe(command.operand, command.operandGiven)
	case "ls":
		return call.runLS()
	case "status":
		return call.runStatus()
	case "logs":
		return call.runLogs(command.operand, command.lines, command.follow, command.operandGiven)
	case "token":
		return call.runToken(command.tokenSet)
	}
	panic("parser returned an unknown command")
}
