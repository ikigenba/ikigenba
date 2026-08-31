// Package cli implements the testable command-line interface for idgen.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/idgen"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

type commandMode uint8

const (
	modeMint commandMode = iota
	modeDecode
	modeVersion
	modeHelp
)

type commandModes uint8

func (modes commandModes) selected() commandMode {
	for mode := modeHelp; mode > modeMint; mode-- {
		if modes&(1<<mode) != 0 {
			return mode
		}
	}
	return modeMint
}

type commandModeFlag struct {
	modes *commandModes
	mode  commandMode
}

func (f commandModeFlag) String() string {
	return strconv.FormatBool(*f.modes&(1<<f.mode) != 0)
}

func (f commandModeFlag) Set(value string) error {
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	if enabled {
		*f.modes |= 1 << f.mode
	} else {
		*f.modes &^= 1 << f.mode
	}
	return nil
}

func (commandModeFlag) IsBoolFlag() bool {
	return true
}

func registerModeFlag(flags *flag.FlagSet, name string, modes *commandModes, mode commandMode) {
	flags.Var(commandModeFlag{modes: modes, mode: mode}, name, "")
}

// intFlag registers an int option under both its short and long spellings,
// binding both to a single pointer so the default and usage appear once.
func intFlag(flags *flag.FlagSet, short, long string, def int, usage string) *int {
	value := flags.Int(short, def, usage)
	flags.IntVar(value, long, def, usage)
	return value
}

// stringFlag registers a string option under both its short and long
// spellings, binding both to a single pointer so the default and usage appear
// once.
func stringFlag(flags *flag.FlagSet, short, long string, def, usage string) *string {
	value := flags.String(short, def, usage)
	flags.StringVar(value, long, def, usage)
	return value
}

// registerModeFlagPair registers a mode option under both its short and long
// spellings in one call, so the pair's shared default and description appear
// once.
func registerModeFlagPair(flags *flag.FlagSet, short, long string, modes *commandModes, mode commandMode) {
	registerModeFlag(flags, short, modes, mode)
	registerModeFlag(flags, long, modes, mode)
}

const usageText = `Usage: idgen [options] [ID ...]

Mint an identifier using the current time by default.

Options:
  -n, --number N       mint N identifiers (default 1)
  -p, --prefix PREFIX  use PREFIX (default "R")
      --decode         decode ID arguments, or whitespace-delimited IDs from stdin
  -h, --help           print this help
  -V, --version        print version
`

// Clock supplies time to the CLI.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// Run executes the CLI and returns its process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) int {
	flags := flag.NewFlagSet("idgen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = io.WriteString(flags.Output(), usageText)
	}
	number := intFlag(flags, "n", "number", 1, "")
	prefix := stringFlag(flags, "p", "prefix", "R", "")
	var modes commandModes
	registerModeFlag(flags, "decode", &modes, modeDecode)
	registerModeFlagPair(flags, "h", "help", &modes, modeHelp)
	registerModeFlagPair(flags, "V", "version", &modes, modeVersion)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	switch modes.selected() {
	case modeHelp:
		writeInformationalOutput(stdout, usageText)
		return exitSuccess
	case modeVersion:
		writeInformationalOutput(stdout, version+"\n")
		return exitSuccess
	case modeDecode:
		return runDecode(flags.Args(), stdin, stdout, stderr)
	default:
		return runMintMode(flags.Args(), *number, *prefix, stdout, stderr, flags.Usage, clock)
	}
}

func writeInformationalOutput(output io.Writer, text string) {
	if _, err := io.WriteString(output, text); err != nil {
		// Help and version output is best effort: their established exit status
		// reports that the requested mode was handled, independent of stream health.
		return
	}
}

func runMintMode(
	args []string,
	number int,
	prefix string,
	stdout, stderr io.Writer,
	usage func(),
	clock Clock,
) int {
	if len(args) > 0 {
		_, _ = io.WriteString(stderr, "idgen: unexpected argument "+strconv.Quote(args[0])+"\n")
		usage()
		return exitUsage
	}
	if !idgen.ValidPrefix(prefix) {
		_, _ = io.WriteString(stderr, "idgen: invalid prefix\n")
		usage()
		return exitUsage
	}
	if number <= 0 {
		_, _ = io.WriteString(stderr, "idgen: --number must be > 0\n")
		usage()
		return exitUsage
	}

	var previousMillisecond int64
	out := bufio.NewWriter(stdout)
	for minted := 0; minted < number; minted++ {
		instant := clock.Now()
		for minted > 0 && instant.UnixMilli() <= previousMillisecond {
			clock.Sleep(time.Millisecond)
			instant = clock.Now()
		}

		if _, err := io.WriteString(out, idgen.MintAt(prefix, instant)+"\n"); err != nil {
			return exitFailure
		}
		previousMillisecond = instant.UnixMilli()
	}
	if err := out.Flush(); err != nil {
		return exitFailure
	}
	return exitSuccess
}

func formatUTC(instant time.Time) string {
	return instant.UTC().Format("2006-01-02T15:04:05.000Z")
}

func runDecode(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	failed := false
	var inputErr error
	out := bufio.NewWriter(stdout)
	decode := func(token string) (bool, error) {
		instant, err := idgen.TimeOf(token)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "idgen: %q: %v\n", token, err)
			return false, nil
		}
		if _, err := io.WriteString(out, formatUTC(instant)+"\n"); err != nil {
			return false, err
		}
		return true, nil
	}

	if len(args) > 0 {
		for _, token := range args {
			valid, err := decode(token)
			if err != nil {
				return exitFailure
			}
			if !valid {
				failed = true
			}
		}
	} else {
		scanner := bufio.NewScanner(stdin)
		scanner.Split(bufio.ScanWords)
		for scanner.Scan() {
			valid, err := decode(scanner.Text())
			if err != nil {
				return exitFailure
			}
			if !valid {
				failed = true
			}
		}
		if err := scanner.Err(); err != nil {
			_, _ = fmt.Fprintf(stderr, "idgen: reading stdin stopped early: %v\n", err)
			inputErr = err
		}
	}
	if err := out.Flush(); err != nil {
		return exitFailure
	}

	if inputErr != nil {
		return exitFailure
	}
	if failed {
		return exitFailure
	}
	return exitSuccess
}
