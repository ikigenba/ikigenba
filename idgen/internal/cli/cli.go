// Package cli implements the testable command-line interface for idgen.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/idgen"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

var validPrefix = regexp.MustCompile(`^[A-Za-z0-9]+$`)

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
	number := flags.Int("n", 1, "")
	flags.IntVar(number, "number", 1, "")
	prefix := flags.String("p", "R", "")
	flags.StringVar(prefix, "prefix", "R", "")
	decode := flags.Bool("decode", false, "")
	help := flags.Bool("h", false, "")
	flags.BoolVar(help, "help", false, "")
	showVersion := flags.Bool("V", false, "")
	flags.BoolVar(showVersion, "version", false, "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if *help {
		_, _ = io.WriteString(stdout, usageText)
		return exitSuccess
	}
	if *showVersion {
		_, _ = io.WriteString(stdout, version+"\n")
		return exitSuccess
	}
	if *decode {
		return runDecode(flags.Args(), stdin, stdout, stderr)
	}
	return runMintMode(flags.Args(), *number, *prefix, stdout, stderr, flags.Usage, clock)
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
	if !validPrefix.MatchString(prefix) {
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

func runDecode(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	failed := false
	out := bufio.NewWriter(stdout)
	decode := func(token string) (bool, error) {
		instant, err := idgen.TimeOf(token)
		if err != nil {
			_, _ = io.WriteString(stderr, "idgen: invalid id "+token+": "+err.Error()+"\n")
			return false, nil
		}
		if _, err := io.WriteString(out, instant.UTC().Format("2006-01-02T15:04:05.000Z")+"\n"); err != nil {
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
			_, _ = io.WriteString(stderr, "idgen: "+err.Error()+"\n")
			failed = true
		}
	}
	if err := out.Flush(); err != nil {
		return exitFailure
	}

	if failed {
		return exitFailure
	}
	return exitSuccess
}
