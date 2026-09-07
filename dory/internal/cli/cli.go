// Package cli provides the dory command-line composition root.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/dory/internal/agent"
	"github.com/ikigenba/ikigenba/dory/internal/model"
	"github.com/ikigenba/ikigenba/dory/internal/options"
	"github.com/ikigenba/ikigenba/dory/internal/render"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

type exitCode int

const (
	exitSuccess exitCode = 0
	exitFailure exitCode = 1
	exitUsage   exitCode = 2
)

var version = "v0.1.0"

// Deps carries the environmental dependencies of a CLI run.
type Deps struct {
	Home       string
	Getenv     func(string) string
	Now        func() time.Time
	SessionID  string
	Root       string
	Interrupts <-chan struct{}
}

// Run executes the CLI and returns its process exit code without terminating
// the calling process.
func Run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	deps Deps,
) int {
	return int(run(ctx, args, stdin, stdout, stderr, deps))
}

func run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	deps Deps,
) exitCode {
	result, failure := preflight(args, stdin)
	if failure != nil {
		return reportPreflightFailure(stderr, failure)
	}

	switch result.action {
	case preflightHelp:
		if err := writeOutput(stdout, options.Usage()); err != nil {
			return exitFailure
		}
		return exitSuccess
	case preflightVersion:
		if err := writeOutput(stdout, version+"\n"); err != nil {
			return exitFailure
		}
		return exitSuccess
	case preflightExecute:
		return execute(ctx, result, stdout, stderr, deps)
	default:
		panic("unknown preflight action")
	}
}

// reportPreflightFailure owns preflight diagnostics. The options parsing and
// validation contract is silent: its FlagSet output is discarded, so no
// diagnostic or usage text has been written before this point.
func reportPreflightFailure(stderr io.Writer, failure *preflightFailure) exitCode {
	if err := writeError(stderr, failure.err); err != nil {
		return exitFailure
	}
	if !failure.usage {
		return exitFailure
	}
	if err := writeOutput(stderr, options.Usage()); err != nil {
		return exitFailure
	}
	return exitUsage
}

type preflightAction uint8

const (
	preflightExecute preflightAction = iota
	preflightHelp
	preflightVersion
)

type preflightResult struct {
	options options.Options
	prompt  string
	action  preflightAction
}

type preflightFailure struct {
	err   error
	usage bool
}

func preflight(args []string, stdin io.Reader) (preflightResult, *preflightFailure) {
	validated, action, failure := parseAndValidateFlags(args)
	if failure != nil {
		return preflightResult{}, failure
	}
	if action != preflightExecute {
		return preflightResult{action: action}, nil
	}

	prompt, failure := readPrompt(stdin)
	if failure != nil {
		return preflightResult{}, failure
	}

	return preflightResult{
		options: validated,
		prompt:  prompt,
		action:  preflightExecute,
	}, nil
}

func parseAndValidateFlags(
	args []string,
) (options.Options, preflightAction, *preflightFailure) {
	flags, err := options.ParseFlags(args)
	if errors.Is(err, options.ErrHelp) {
		return options.Options{}, preflightHelp, nil
	}
	if err != nil {
		return options.Options{}, preflightExecute, &preflightFailure{err: err, usage: true}
	}
	if flags.Version {
		return options.Options{}, preflightVersion, nil
	}

	validated, err := flags.Validate()
	if err != nil {
		return options.Options{}, preflightExecute, &preflightFailure{err: err, usage: true}
	}
	return validated, preflightExecute, nil
}

func readPrompt(stdin io.Reader) (string, *preflightFailure) {
	promptBytes, err := io.ReadAll(stdin)
	if err != nil {
		return "", &preflightFailure{err: fmt.Errorf("read prompt: %w", err)}
	}
	prompt := strings.TrimRightFunc(string(promptBytes), unicode.IsSpace)
	if prompt == "" {
		return "", &preflightFailure{
			err:   errors.New("prompt is empty"),
			usage: true,
		}
	}
	return prompt, nil
}

func execute(
	ctx context.Context,
	preflight preflightResult,
	stdout io.Writer,
	stderr io.Writer,
	deps Deps,
) exitCode {
	supervisor, err := openRole(preflight.options.Supervisor, deps)
	if err != nil {
		return reportOperationalFailure(stderr, fmt.Errorf("open supervisor model: %w", err))
	}
	worker, err := openRole(preflight.options.Worker, deps)
	if err != nil {
		return reportOperationalFailure(stderr, fmt.Errorf("open worker model: %w", err))
	}

	session, sessionID, err := openSession(preflight.options, deps)
	if err != nil {
		return reportOperationalFailure(stderr, err)
	}
	if session.Root() != deps.Root {
		err = fmt.Errorf("session root %q differs from working root %q", session.Root(), deps.Root)
		if closeErr := session.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close session: %w", closeErr))
		}
		return reportOperationalFailure(stderr, err)
	}

	trace := render.NewTrace(stdout, stderr)
	passCtx, cancel, passDone := interruptContext(ctx, deps.Interrupts)
	result, passErr := agent.RunPass(passCtx, agent.Config{
		Store:      session,
		Supervisor: supervisor,
		Worker:     worker,
		Root:       deps.Root,
		Now:        deps.Now,
		Trace:      trace,
	}, preflight.prompt)
	close(passDone)
	cancel()

	trace.Summary(result.Usage, result.Cost, sessionID)
	closeErr := session.Close()
	if closeErr != nil {
		_ = writeError(stderr, fmt.Errorf("close session: %w", closeErr))
		return exitFailure
	}
	if passErr != nil {
		return exitFailure
	}
	return exitSuccess
}

func openRole(role options.Role, deps Deps) (*model.Factory, error) {
	return model.Open(model.Config{
		Provider:   role.Provider,
		Model:      role.Model,
		Wire:       role.Wire,
		Auth:       role.Auth,
		AuthFile:   role.AuthFile,
		BaseURL:    role.BaseURL,
		MaxContext: role.MaxContext,
		Settings:   role.Settings,
		Home:       deps.Home,
		Getenv:     deps.Getenv,
	})
}

func openSession(validated options.Options, deps Deps) (*store.Store, string, error) {
	directory := filepath.Join(deps.Home, ".dory", "sessions")
	if validated.Resume != "" {
		path := filepath.Join(directory, validated.Resume+".db")
		session, err := store.Open(path, deps.Now)
		if err != nil {
			return nil, "", fmt.Errorf("open session %q: %w", path, err)
		}
		return session, session.ID(), nil
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, "", fmt.Errorf("create session directory %q: %w", directory, err)
	}
	path := filepath.Join(directory, deps.SessionID+".db")
	session, err := store.Create(path, deps.SessionID, deps.Root, deps.Now)
	if err != nil {
		return nil, "", fmt.Errorf("create session %q: %w", path, err)
	}
	return session, deps.SessionID, nil
}

func interruptContext(
	parent context.Context,
	interrupts <-chan struct{},
) (context.Context, context.CancelFunc, chan struct{}) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		select {
		case <-interrupts:
			cancel()
		case <-done:
		}
	}()
	return ctx, cancel, done
}

func reportOperationalFailure(stderr io.Writer, err error) exitCode {
	_ = writeError(stderr, err)
	return exitFailure
}

func writeError(destination io.Writer, err error) error {
	_, writeErr := fmt.Fprintln(destination, err)
	return writeErr
}

func writeOutput(destination io.Writer, output string) error {
	_, err := io.WriteString(destination, output)
	return err
}
