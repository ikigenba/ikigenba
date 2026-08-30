// Package cli implements the testable command-line interface for idgen.
package cli

import (
	"flag"
	"io"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/idgen"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

// Clock supplies time to the CLI.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// Run executes the CLI and returns its process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) int {
	_ = stdin

	flags := flag.NewFlagSet("idgen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	number := flags.Int("n", 1, "number of identifiers to mint")
	flags.IntVar(number, "number", 1, "number of identifiers to mint")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	var previousMillisecond int64
	for minted := 0; minted < *number; minted++ {
		instant := clock.Now()
		for minted > 0 && instant.UnixMilli() <= previousMillisecond {
			clock.Sleep(time.Millisecond)
			instant = clock.Now()
		}

		_, _ = io.WriteString(stdout, idgen.MintAt("R", instant)+"\n")
		previousMillisecond = instant.UnixMilli()
	}
	return exitSuccess
}
