// Package cli implements the testable command-line interface for idgen.
package cli

import (
	"bufio"
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
	number := flags.Int("n", 1, "number of identifiers to mint")
	flags.IntVar(number, "number", 1, "number of identifiers to mint")
	prefix := flags.String("p", "R", "identifier prefix")
	flags.StringVar(prefix, "prefix", "R", "identifier prefix")
	decode := flags.Bool("decode", false, "decode identifiers")
	help := flags.Bool("h", false, "print help")
	flags.BoolVar(help, "help", false, "print help")
	showVersion := flags.Bool("V", false, "print version")
	flags.BoolVar(showVersion, "version", false, "print version")
	if err := flags.Parse(args); err != nil {
		flags.Usage()
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
	if flags.NArg() > 0 {
		_, _ = io.WriteString(stderr, "idgen: unexpected argument "+strconv.Quote(flags.Arg(0))+"\n")
		flags.Usage()
		return exitUsage
	}
	if !validPrefix.MatchString(*prefix) {
		_, _ = io.WriteString(stderr, "idgen: invalid prefix\n")
		flags.Usage()
		return exitUsage
	}
	if *number <= 0 {
		_, _ = io.WriteString(stderr, "idgen: --number must be > 0\n")
		flags.Usage()
		return exitUsage
	}

	var previousMillisecond int64
	for minted := 0; minted < *number; minted++ {
		instant := clock.Now()
		for minted > 0 && instant.UnixMilli() <= previousMillisecond {
			clock.Sleep(time.Millisecond)
			instant = clock.Now()
		}

		_, _ = io.WriteString(stdout, idgen.MintAt(*prefix, instant)+"\n")
		previousMillisecond = instant.UnixMilli()
	}
	return exitSuccess
}

func runDecode(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	failed := false
	decode := func(token string) {
		instant, err := idgen.TimeOf(token)
		if err != nil {
			_, _ = io.WriteString(stderr, "idgen: invalid id "+token+": "+err.Error()+"\n")
			failed = true
			return
		}
		_, _ = io.WriteString(stdout, instant.UTC().Format("2006-01-02T15:04:05.000Z")+"\n")
	}

	if len(args) > 0 {
		for _, token := range args {
			decode(token)
		}
	} else {
		scanner := bufio.NewScanner(stdin)
		scanner.Split(bufio.ScanWords)
		for scanner.Scan() {
			decode(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			_, _ = io.WriteString(stderr, "idgen: "+err.Error()+"\n")
			failed = true
		}
	}

	if failed {
		return exitFailure
	}
	return exitSuccess
}
