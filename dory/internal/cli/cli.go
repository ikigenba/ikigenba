// Package cli provides the dory command-line composition root.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/dory/internal/options"
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
		return execute(ctx, result, deps)
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

func execute(context.Context, preflightResult, Deps) exitCode {
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
