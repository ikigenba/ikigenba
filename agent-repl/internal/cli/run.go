// Package cli sequences the agent-repl command-line interface.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
	"github.com/ikigenba/ikigenba/agent-repl/internal/render"
	"github.com/ikigenba/ikigenba/agent-repl/internal/session"
	"github.com/ikigenba/ikigenba/agentkit"
)

type exitCode int

const (
	exitSuccess exitCode = 0
	exitFailure exitCode = 1
	exitUsage   exitCode = 2
)

// Deps carries the environmental dependencies of a session.
type Deps struct {
	Home       string
	Getenv     func(string) string
	Now        func() time.Time
	Root       string
	Interrupts <-chan struct{}
}

// Run executes the CLI and returns its process exit code without terminating
// the calling process. args excludes the program name.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int {
	return int(run(ctx, args, stdin, stdout, stderr, deps))
}

// run uses a distinct type for exit codes internally. Run converts at the
// public boundary because its fixed API contract returns int.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) exitCode {
	flags, err := options.ParseFlags(args)
	if errors.Is(err, options.ErrHelp) {
		return writeText(stdout, options.Usage(), exitSuccess)
	}
	if err != nil {
		return writeUsageError(stderr, err)
	}

	if flags.Version {
		return writeText(stdout, version+"\n", exitSuccess)
	}

	opts, err := flags.Validate()
	if err != nil {
		return writeUsageError(stderr, err)
	}

	active, err := openCLISession(opts, stdout, stderr, deps)
	if err != nil {
		return writeFailure(stderr, err)
	}
	return active.execute(ctx, stdin, deps.Interrupts)
}

type cliSession struct {
	file      *os.File
	log       *agentkit.Log
	sink      *render.LogSink
	opened    *session.Session
	decorated *render.Decorated
	raw       bool
}

func openCLISession(opts options.Options, stdout, stderr io.Writer, deps Deps) (*cliSession, error) {
	file, err := createLogFile(deps)
	if err != nil {
		return nil, err
	}

	destinations := []io.Writer{file}
	if opts.Raw {
		destinations = append(destinations, stdout)
	}
	sink := render.NewLogSink(destinations...)
	log := agentkit.NewLog(sink, deps.Now)
	opened, err := session.Open(session.Config{
		Provider: opts.Provider,
		Model:    opts.Model,
		Wire:     opts.Wire,
		Auth:     opts.Auth,
		AuthFile: opts.AuthFile,
		BaseURL:  opts.BaseURL,
		Settings: opts.Settings,
		Home:     deps.Home,
		Getenv:   deps.Getenv,
		Root:     deps.Root,
		Log:      log,
	})
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &cliSession{
		file:      file,
		log:       log,
		sink:      sink,
		opened:    opened,
		decorated: render.NewDecorated(stdout, stderr),
		raw:       opts.Raw,
	}, nil
}

func (active *cliSession) execute(ctx context.Context, stdin io.Reader, interrupts <-chan struct{}) exitCode {
	runSession(ctx, stdin, active.opened, active.decorated, active.raw, interrupts)
	_ = active.log.Close()
	_ = active.file.Close()
	if !active.raw {
		active.decorated.End()
		if usage, cost, ok := active.sink.Summary(); ok {
			active.decorated.Summary(usage, cost)
		}
	}
	return exitSuccess
}

func createLogFile(deps Deps) (*os.File, error) {
	directory := filepath.Join(deps.Home, ".agent-repl", "logs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	name := logStamp(deps.Now()) + ".jsonl"
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	file, openErr := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	closeErr := root.Close()
	if openErr != nil {
		return nil, openErr
	}
	if closeErr != nil {
		_ = file.Close()
		return nil, closeErr
	}
	return file, nil
}

func logStamp(now time.Time) string {
	utc := now.UTC()
	return utc.Format("20060102T150405Z")
}

type lineResult struct {
	line string
	err  error
}

func runSession(
	ctx context.Context,
	stdin io.Reader,
	opened *session.Session,
	decorated *render.Decorated,
	raw bool,
	interrupts <-chan struct{},
) {
	reader := bufio.NewReader(stdin)
	for {
		if ctx.Err() != nil {
			return
		}
		if !raw {
			decorated.Prompt()
		}

		line, ok, err := awaitLine(ctx, reader, interrupts)
		if !ok {
			if err != nil {
				decorated.Error(fmt.Errorf("input ended early: %w", err))
			}
			return
		}
		line = stripTerminator(line)
		if line == "" {
			continue
		}
		if !raw {
			decorated.Begin()
		}
		runTurn(ctx, opened, decorated, raw, interrupts, line)
	}
}

func stripTerminator(line string) string {
	if strings.HasSuffix(line, "\r\n") {
		return strings.TrimSuffix(line, "\r\n")
	}
	return strings.TrimSuffix(line, "\n")
}

func awaitLine(ctx context.Context, reader *bufio.Reader, interrupts <-chan struct{}) (string, bool, error) {
	result := make(chan lineResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		result <- lineResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", false, nil
	case <-interrupts:
		return "", false, nil
	case read := <-result:
		if read.err == nil {
			return read.line, true, nil
		}
		if errors.Is(read.err, io.EOF) {
			return read.line, read.line != "", nil
		}
		return "", false, fmt.Errorf("read input: %w", read.err)
	}
}

func runTurn(
	ctx context.Context,
	opened *session.Session,
	decorated *render.Decorated,
	raw bool,
	interrupts <-chan struct{},
	prompt string,
) {
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := opened.Send(turnCtx, prompt)
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for event := range stream.Events() {
			if !raw {
				decorated.Event(event)
			}
		}
	}()

	select {
	case <-eventsDone:
	case <-ctx.Done():
		cancel()
		<-eventsDone
	case <-interrupts:
		cancel()
		<-eventsDone
	}
	if err := stream.Err(); err != nil {
		decorated.Error(fmt.Errorf("turn ended early: %w", err))
	}
}

func writeFailure(stderr io.Writer, cause error) exitCode {
	if _, err := fmt.Fprintf(stderr, "error: %s\n", sanitizeTerminalDiagnostic(cause.Error())); err != nil {
		return exitFailure
	}
	return exitFailure
}

func writeUsageError(stderr io.Writer, cause error) exitCode {
	return writeText(stderr, sanitizeTerminalDiagnostic(cause.Error())+"\n"+options.Usage(), exitUsage)
}

func sanitizeTerminalDiagnostic(message string) string {
	return strings.Map(func(character rune) rune {
		if character < ' ' || character == '\x7f' {
			return ' '
		}
		return character
	}, message)
}

func writeText(destination io.Writer, value string, successCode exitCode) exitCode {
	if _, err := io.WriteString(destination, value); err != nil {
		return exitFailure
	}
	return successCode
}
