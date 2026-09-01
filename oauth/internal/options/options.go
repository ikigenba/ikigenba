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

const usage = `Usage: oauth [flags]

Flags:
  --auth-url string
        authorization endpoint (required)
  --token-url string
        token endpoint (required)
  --client-id string
        OAuth client id (required)
  --scope string
        space-separated OAuth scopes
  --client-secret string
        client secret sent in the token request body
  --callback-host string
        host used in the redirect URI (default "localhost")
  --port int
        loopback callback port; 0 chooses an available port (default 0)
  --callback-path string
        callback route and redirect URI path (default "/callback")
  --auth-param key=value
        extra authorize parameter (repeatable)
  --token-param key=value
        extra token parameter (repeatable)
  --token-header key=value
        extra token request header (repeatable)
  --no-browser
        print the authorize URL without opening a browser
  --timeout duration
        maximum time to wait for the callback (default 5m)
  -h, --help
        print help and exit
  -V, --version
        print version and exit

OpenAI example:
  oauth \
    --auth-url  https://auth.openai.com/oauth/authorize \
    --token-url https://auth.openai.com/oauth/token \
    --client-id app_EMoamEEZ73f0CkXaXp7hrann \
    --scope "openid profile email offline_access" \
    --port 1455 --callback-path /auth/callback \
    > auth.json

xAI example:
  oauth \
    --auth-url  https://auth.x.ai/oauth2/authorize \
    --token-url https://auth.x.ai/oauth2/token \
    --client-id b1a00492-073a-47ea-816f-4c329264a828 \
    --scope "openid profile email offline_access grok-cli:access api:access" \
    --callback-host 127.0.0.1 \
    --port 56121 \
    --callback-path /callback \
    > x-ai-auth.json

Basic authentication:
  --token-header "Authorization=Basic $(printf '%s:%s' "$ID" "$SECRET" | base64 -w0)"
`

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

// Flags holds the raw values parsed from the command line.
type Flags struct {
	AuthURL      string
	TokenURL     string
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

// ParseFlags parses flag syntax without applying login validation.
func ParseFlags(args []string) (Flags, error) {
	var parsed Flags

	flags := flag.NewFlagSet("oauth", flag.ContinueOnError)
	flags.StringVar(&parsed.AuthURL, "auth-url", "", "")
	flags.StringVar(&parsed.TokenURL, "token-url", "", "")
	flags.StringVar(&parsed.ClientID, "client-id", "", "")
	flags.StringVar(&parsed.Scope, "scope", "", "")
	flags.StringVar(&parsed.ClientSecret, "client-secret", "", "")
	flags.StringVar(&parsed.CallbackHost, "callback-host", "localhost", "")
	flags.IntVar(&parsed.Port, "port", 0, "")
	flags.StringVar(&parsed.CallbackPath, "callback-path", "/callback", "")
	flags.Var(newParamsValue(&parsed.AuthParams), "auth-param", "")
	flags.Var(newParamsValue(&parsed.TokenParams), "token-param", "")
	flags.Var(newParamsValue(&parsed.TokenHeaders), "token-header", "")
	flags.BoolVar(&parsed.NoBrowser, "no-browser", false, "")
	flags.DurationVar(&parsed.Timeout, "timeout", 5*time.Minute, "")
	for _, name := range []string{"V", "version"} {
		flags.BoolVar(&parsed.Version, name, false, "")
	}
	flags.SetOutput(io.Discard)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Flags{}, ErrHelp
		}

		return Flags{}, err
	}

	return parsed, nil
}

// Validate applies semantic login validation and returns parsed endpoint URLs.
func (parsed Flags) Validate() (Options, error) {

	for _, required := range []struct {
		name  string
		value string
	}{
		{"--auth-url", parsed.AuthURL},
		{"--token-url", parsed.TokenURL},
		{"--client-id", parsed.ClientID},
	} {
		if required.value == "" {
			return Options{}, fmt.Errorf("missing required flag %s", required.name)
		}
	}

	validated := Options{
		ClientID:     parsed.ClientID,
		Scope:        parsed.Scope,
		ClientSecret: parsed.ClientSecret,
		CallbackHost: parsed.CallbackHost,
		Port:         parsed.Port,
		CallbackPath: parsed.CallbackPath,
		AuthParams:   parsed.AuthParams,
		TokenParams:  parsed.TokenParams,
		TokenHeaders: parsed.TokenHeaders,
		NoBrowser:    parsed.NoBrowser,
		Timeout:      parsed.Timeout,
		Version:      parsed.Version,
	}

	var err error
	validated.AuthURL, err = parseURL("--auth-url", parsed.AuthURL)
	if err != nil {
		return Options{}, err
	}
	validated.TokenURL, err = parseURL("--token-url", parsed.TokenURL)
	if err != nil {
		return Options{}, err
	}
	if err := validateParamForms("--auth-param", parsed.AuthParams); err != nil {
		return Options{}, err
	}
	if err := validateParamForms("--token-param", parsed.TokenParams); err != nil {
		return Options{}, err
	}
	if err := validateParamForms("--token-header", parsed.TokenHeaders); err != nil {
		return Options{}, err
	}
	if err := validateExtraParams(parsed.AuthParams, parsed.TokenParams); err != nil {
		return Options{}, err
	}
	if err := validateOptions(validated); err != nil {
		return Options{}, err
	}

	return validated, nil
}

func validateParamForms(flagName string, params []oauth.Param) error {
	for _, param := range params {
		if param.Key == "" {
			return fmt.Errorf("%s value %q must be key=value with a non-empty key", flagName, param.Value)
		}
	}

	return nil
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
	params *[]oauth.Param
}

func newParamsValue(params *[]oauth.Param) *paramsValue {
	return &paramsValue{params: params}
}

func (value *paramsValue) String() string {
	return ""
}

func (value *paramsValue) Set(raw string) error {
	key, parameterValue, found := strings.Cut(raw, "=")
	if !found || key == "" {
		key, parameterValue = "", raw
	}
	*value.params = append(*value.params, oauth.Param{Key: key, Value: parameterValue})

	return nil
}
