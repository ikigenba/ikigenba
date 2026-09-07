// Package options parses and validates dory command-line options.
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

// Pair is one configuration key and value supplied on the command line.
type Pair struct {
	Key   string
	Value string
}

// Flags contains the syntactically parsed command-line flags.
type Flags struct {
	Config  []Pair
	Resume  string
	Version bool
}

// Options is the validated command-line configuration.
//
// Its complete shape is introduced with semantic validation.
type Options struct{}

// ParseFlags parses dory's command-line flag grammar.
func ParseFlags(args []string) (Flags, error) {
	var parsed Flags

	flags := flag.NewFlagSet("dory", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Var(configValue{pairs: &parsed.Config}, "c", "set a configuration value")
	flags.StringVar(&parsed.Resume, "resume", "", "resume an existing session")
	boolVarAliases(flags, &parsed.Version, false, "print the version", "V", "version")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Flags{}, ErrHelp
		}
		return Flags{}, err
	}
	if flags.NArg() != 0 {
		return Flags{}, fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
	}

	return parsed, nil
}

func boolVarAliases(flags *flag.FlagSet, target *bool, defaultValue bool, usage string, names ...string) {
	for _, name := range names {
		flags.BoolVar(target, name, defaultValue, usage)
	}
}

// Validate converts syntactically parsed flags into validated options.
// Semantic validation is introduced in the next phase.
func (Flags) Validate() (Options, error) {
	return Options{}, nil
}

// Usage returns the command's usage synopsis.
func Usage() string {
	return "dory [-c key=value ...] [-resume UUID] [-V] [-h] < prompt\n"
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
		return fmt.Errorf("malformed configuration %q", argument)
	}

	*value.pairs = append(*value.pairs, Pair{Key: key, Value: configValue})
	return nil
}
