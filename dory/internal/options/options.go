// Package options parses and validates dory command-line options.
package options

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/ikigenba/ikigenba/agentkit"
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

// Role is the validated configuration for one kind of agent.
type Role struct {
	Provider   string
	Model      string
	Wire       string
	Auth       string
	AuthFile   string
	BaseURL    string
	MaxContext int64
	Settings   map[string]string
}

// Options is the validated command-line configuration.
type Options struct {
	Supervisor Role
	Worker     Role
	Resume     string
	Version    bool
}

// ParseFlags parses dory's command-line flag grammar.
func ParseFlags(args []string) (Flags, error) {
	var parsed Flags

	flags := flag.NewFlagSet("dory", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Var(configValue{pairs: &parsed.Config}, "c", "")
	flags.StringVar(&parsed.Resume, "resume", "", "")
	boolVarAliases(flags, &parsed.Version, false, "V", "version")

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

func boolVarAliases(flags *flag.FlagSet, target *bool, defaultValue bool, names ...string) {
	for _, name := range names {
		flags.BoolVar(target, name, defaultValue, "")
	}
}

// Validate converts syntactically parsed flags into validated options.
func (flags Flags) Validate() (Options, error) {
	values := map[string]map[string]string{
		"supervisor": {},
		"worker":     {},
	}
	for _, pair := range flags.Config {
		role, name, found := strings.Cut(pair.Key, ".")
		roleValues, validRole := values[role]
		if !found || !validRole {
			return Options{}, fmt.Errorf("invalid configuration key %q", pair.Key)
		}
		roleValues[name] = pair.Value
	}

	supervisor, err := validateRole("supervisor", values["supervisor"])
	if err != nil {
		return Options{}, err
	}
	worker, err := validateRole("worker", values["worker"])
	if err != nil {
		return Options{}, err
	}
	if flags.Resume != "" {
		parsed, parseErr := uuid.Parse(flags.Resume)
		if parseErr != nil || parsed.String() != flags.Resume {
			return Options{}, fmt.Errorf("invalid resume %q", flags.Resume)
		}
	}

	return Options{
		Supervisor: supervisor,
		Worker:     worker,
		Resume:     flags.Resume,
		Version:    flags.Version,
	}, nil
}

func validateRole(roleName string, values map[string]string) (Role, error) {
	role := Role{
		Model:      "gpt-5.6-sol",
		MaxContext: -1,
		Settings:   make(map[string]string),
	}
	for name, value := range values {
		switch name {
		case "provider":
			role.Provider = value
		case "model":
			role.Model = value
		case "wire":
			role.Wire = value
		case "auth":
			role.Auth = value
		case "auth_file":
			role.AuthFile = value
		case "base_url":
			role.BaseURL = value
		case "max_context":
			maxContext, err := parseMaxContext(value)
			if err != nil {
				return Role{}, fmt.Errorf("invalid %s.max_context %q: %w", roleName, value, err)
			}
			role.MaxContext = maxContext
		default:
			role.Settings[name] = value
		}
	}

	if _, present := values["model"]; present && role.Model == "" {
		return Role{}, fmt.Errorf("invalid %s.model: model is empty", roleName)
	}
	if _, present := values["provider"]; present {
		if !oneOf(role.Provider, "anthropic", "gemini", "openai", "openrouter", "xai") {
			return Role{}, fmt.Errorf("invalid %s.provider %q", roleName, role.Provider)
		}
	} else {
		offering, err := agentkit.Lookup(role.Model, "", "")
		if err != nil {
			return Role{}, fmt.Errorf("invalid %s.model %q: %w", roleName, role.Model, err)
		}
		role.Provider = string(offering.Host)
	}
	if _, present := values["auth"]; present && !oneOf(role.Auth, "api_key", "oauth") {
		return Role{}, fmt.Errorf("invalid %s.auth %q", roleName, role.Auth)
	}
	if _, present := values["wire"]; present && !oneOf(role.Wire, "messages", "generate-content", "chat", "responses") {
		return Role{}, fmt.Errorf("invalid %s.wire %q", roleName, role.Wire)
	}

	return role, nil
}

func parseMaxContext(value string) (int64, error) {
	parsed, err := strconv.ParseUint(value, 10, 63)
	if strings.HasPrefix(value, "+") {
		err = errors.New("signed value")
	}
	if err != nil {
		return 0, errors.New("must be a non-negative decimal integer")
	}
	return int64(parsed), nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// Usage returns the command's usage text.
func Usage() string {
	return `usage: dory [-c key=value ...] [-resume UUID] [-V] [-h] < prompt

Role configuration (-c <role>.<key>=<value>):
  supervisor.provider, supervisor.model, supervisor.wire
  supervisor.auth, supervisor.auth_file, supervisor.base_url, supervisor.max_context
  worker.provider, worker.model, worker.wire
  worker.auth, worker.auth_file, worker.base_url, worker.max_context
Other role-scoped names are passed through as agent settings.
`
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
