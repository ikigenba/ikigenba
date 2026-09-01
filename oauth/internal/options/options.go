// Package options parses the command-line description of an OAuth login.
package options

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

const usage = "Usage: oauth [flags]\n"

// ErrHelp reports that either supported help flag was supplied.
var ErrHelp = flag.ErrHelp

// Usage returns the command's usage text for placement by the CLI.
func Usage() string {
	return usage
}

// Options describes an OAuth login and its command-line controls.
type Options struct {
	AuthURL      *url.URL
	TokenURL     *url.URL
	ClientID     string
	Scope        string
	ClientSecret string
	CallbackHost string
	Port         int
	CallbackPath string
	AuthParams   []oauth.Param
	TokenParams  []oauth.Param
	TokenHeaders []oauth.Param
	NoBrowser    bool
	Timeout      time.Duration
	Version      bool
}

// Parse parses args without writing diagnostics or usage text to a stream.
func Parse(args []string) (Options, error) {
	var parsed struct {
		authURL  string
		tokenURL string
		options  Options
	}

	flags := flag.NewFlagSet("oauth", flag.ContinueOnError)
	flags.StringVar(&parsed.authURL, "auth-url", "", "")
	flags.StringVar(&parsed.tokenURL, "token-url", "", "")
	flags.StringVar(&parsed.options.ClientID, "client-id", "", "")
	flags.StringVar(&parsed.options.Scope, "scope", "", "")
	flags.StringVar(&parsed.options.ClientSecret, "client-secret", "", "")
	flags.StringVar(&parsed.options.CallbackHost, "callback-host", "localhost", "")
	flags.IntVar(&parsed.options.Port, "port", 0, "")
	flags.StringVar(&parsed.options.CallbackPath, "callback-path", "/callback", "")
	flags.Var(newParamsValue("--auth-param", &parsed.options.AuthParams), "auth-param", "")
	flags.Var(newParamsValue("--token-param", &parsed.options.TokenParams), "token-param", "")
	flags.Var(newParamsValue("--token-header", &parsed.options.TokenHeaders), "token-header", "")
	flags.BoolVar(&parsed.options.NoBrowser, "no-browser", false, "")
	flags.DurationVar(&parsed.options.Timeout, "timeout", 5*time.Minute, "")
	for _, name := range []string{"V", "version"} {
		flags.BoolVar(&parsed.options.Version, name, false, "")
	}
	flags.SetOutput(io.Discard)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Options{}, ErrHelp
		}

		return Options{}, err
	}
	if parsed.options.Version {
		var err error
		if parsed.authURL != "" {
			parsed.options.AuthURL, err = parseURL("--auth-url", parsed.authURL)
			if err != nil {
				return Options{}, err
			}
		}
		if parsed.tokenURL != "" {
			parsed.options.TokenURL, err = parseURL("--token-url", parsed.tokenURL)
			if err != nil {
				return Options{}, err
			}
		}

		return parsed.options, nil
	}

	for _, required := range []struct {
		name  string
		value string
	}{
		{"--auth-url", parsed.authURL},
		{"--token-url", parsed.tokenURL},
		{"--client-id", parsed.options.ClientID},
	} {
		if required.value == "" {
			return Options{}, fmt.Errorf("missing required flag %s", required.name)
		}
	}

	var err error
	parsed.options.AuthURL, err = parseURL("--auth-url", parsed.authURL)
	if err != nil {
		return Options{}, err
	}
	parsed.options.TokenURL, err = parseURL("--token-url", parsed.tokenURL)
	if err != nil {
		return Options{}, err
	}
	if err := validateExtraParams(parsed.options.AuthParams, parsed.options.TokenParams); err != nil {
		return Options{}, err
	}
	if err := validateOptions(parsed.options); err != nil {
		return Options{}, err
	}

	return parsed.options, nil
}

func validateOptions(parsed Options) error {
	if parsed.ClientSecret != "" {
		for _, header := range parsed.TokenHeaders {
			if strings.EqualFold(header.Key, "Authorization") {
				return errors.New("--client-secret cannot be used with an Authorization --token-header")
			}
		}
	}
	if parsed.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if !strings.HasPrefix(parsed.CallbackPath, "/") {
		return errors.New("--callback-path must begin with /")
	}

	return nil
}

func validateExtraParams(authParams, tokenParams []oauth.Param) error {
	for _, param := range authParams {
		if !oauth.ReservedAuthorizeParam(param.Key) {
			continue
		}
		if param.Key == "redirect_uri" {
			return fmt.Errorf(
				"--auth-param key %q is reserved; configure the redirect URI with --callback-host, --port, and --callback-path",
				param.Key,
			)
		}

		return fmt.Errorf("--auth-param key %q is reserved", param.Key)
	}
	for _, param := range tokenParams {
		if oauth.ReservedTokenParam(param.Key) {
			return fmt.Errorf("--token-param key %q is reserved", param.Key)
		}
	}

	return nil
}

func parseURL(flagName, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", flagName, raw, err)
	}

	return parsed, nil
}

type paramsValue struct {
	flagName string
	params   *[]oauth.Param
}

func newParamsValue(flagName string, params *[]oauth.Param) *paramsValue {
	return &paramsValue{flagName: flagName, params: params}
}

func (value *paramsValue) String() string {
	return ""
}

func (value *paramsValue) Set(raw string) error {
	key, parameterValue, found := strings.Cut(raw, "=")
	if !found || key == "" {
		return fmt.Errorf("%s value %q must be key=value with a non-empty key", value.flagName, raw)
	}
	*value.params = append(*value.params, oauth.Param{Key: key, Value: parameterValue})

	return nil
}
