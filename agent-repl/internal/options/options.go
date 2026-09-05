package options

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultModel = "gpt-5.6-sol"

// Options contains the semantically validated command-line options.
type Options struct {
	Provider string
	Model    string
	Wire     string
	Auth     string
	AuthFile string
	BaseURL  string
	Settings map[string]string
	Raw      bool
	Version  bool
}

// Validate folds and semantically validates the parsed configuration flags.
func (flags Flags) Validate() (Options, error) {
	options := Options{
		Settings: make(map[string]string),
		Raw:      flags.Raw,
		Version:  flags.Version,
	}
	given := make(map[string]bool)

	for _, pair := range flags.Config {
		switch pair.Key {
		case "provider":
			options.Provider = pair.Value
			given[pair.Key] = true
		case "model":
			options.Model = pair.Value
			given[pair.Key] = true
		case "wire":
			options.Wire = pair.Value
			given[pair.Key] = true
		case "auth":
			options.Auth = pair.Value
			given[pair.Key] = true
		case "auth_file":
			options.AuthFile = pair.Value
		case "base_url":
			options.BaseURL = pair.Value
		default:
			options.Settings[pair.Key] = pair.Value
		}
	}

	if !given["model"] {
		options.Model = defaultModel
	}
	if options.Model == "" {
		return Options{}, fmt.Errorf("invalid model %q: must not be empty", options.Model)
	}
	if err := validateChoice("provider", options.Provider, given["provider"], validProviders); err != nil {
		return Options{}, err
	}
	if err := validateChoice("auth", options.Auth, given["auth"], validAuthModes); err != nil {
		return Options{}, err
	}
	if err := validateChoice("wire", options.Wire, given["wire"], validWires); err != nil {
		return Options{}, err
	}

	if options.Provider == "" {
		offering, err := agentkit.Lookup(options.Model, "", "")
		if err != nil {
			return Options{}, fmt.Errorf("invalid model %q: %w", options.Model, err)
		}
		options.Provider = string(offering.Host)
	}

	return options, nil
}

var validProviders = map[string]struct{}{
	"anthropic":  {},
	"gemini":     {},
	"openai":     {},
	"openrouter": {},
	"xai":        {},
}

var validAuthModes = map[string]struct{}{
	"api_key": {},
	"oauth":   {},
}

var validWires = map[string]struct{}{
	"messages":         {},
	"generate-content": {},
	"chat":             {},
	"responses":        {},
}

func validateChoice(key, value string, given bool, choices map[string]struct{}) error {
	if !given {
		return nil
	}
	if _, valid := choices[value]; !valid {
		return fmt.Errorf("invalid %s %q", key, value)
	}
	return nil
}
