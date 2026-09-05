// Package options parses and validates the agent-repl command-line options.
package options

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// ErrHelp indicates that command-line help was requested.
var ErrHelp = flag.ErrHelp

// Pair is an unvalidated command-line configuration key and value.
type Pair struct {
	Key   string
	Value string
}

// Flags contains the syntax-only result of parsing command-line flags.
type Flags struct {
	Config  []Pair
	Raw     bool
	Version bool
}

// ParseFlags parses args without applying semantic option validation.
func ParseFlags(args []string) (Flags, error) {
	var parsed Flags
	set := flag.NewFlagSet("agent-repl", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.Var(configValue{pairs: &parsed.Config}, "c", "set a config value")
	set.BoolVar(&parsed.Raw, "raw", false, "emit the raw message stream")
	for _, name := range []string{"V", "version"} {
		set.BoolVar(&parsed.Version, name, false, "print the version")
	}

	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return parsed, ErrHelp
		}
		return parsed, err
	}
	if set.NArg() != 0 {
		return parsed, fmt.Errorf("unexpected argument %q", set.Arg(0))
	}
	return parsed, nil
}

type configValue struct {
	pairs *[]Pair
}

func (value configValue) String() string {
	return ""
}

func (value configValue) Set(argument string) error {
	key, configValue, found := strings.Cut(argument, "=")
	if !found || key == "" {
		return fmt.Errorf("malformed -c argument %q", argument)
	}
	*value.pairs = append(*value.pairs, Pair{Key: key, Value: configValue})
	return nil
}
