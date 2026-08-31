// Package options parses the command-line description of an OAuth login.
package options

import (
	"errors"
	"flag"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

// ErrHelp reports that either supported help flag was supplied.
var ErrHelp = flag.ErrHelp

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
	flags.StringVar(&parsed.authURL, "auth-url", "", "authorization endpoint")
	flags.StringVar(&parsed.tokenURL, "token-url", "", "token endpoint")
	flags.StringVar(&parsed.options.ClientID, "client-id", "", "OAuth client id")
	flags.StringVar(&parsed.options.Scope, "scope", "", "space-separated OAuth scopes")
	flags.StringVar(&parsed.options.ClientSecret, "client-secret", "", "OAuth client secret")
	flags.StringVar(&parsed.options.CallbackHost, "callback-host", "localhost", "callback host")
	flags.IntVar(&parsed.options.Port, "port", 0, "loopback callback port")
	flags.StringVar(&parsed.options.CallbackPath, "callback-path", "/callback", "callback path")
	flags.Var((*paramsValue)(&parsed.options.AuthParams), "auth-param", "extra authorization parameter")
	flags.Var((*paramsValue)(&parsed.options.TokenParams), "token-param", "extra token parameter")
	flags.Var((*paramsValue)(&parsed.options.TokenHeaders), "token-header", "extra token request header")
	flags.BoolVar(&parsed.options.NoBrowser, "no-browser", false, "do not open a browser")
	flags.DurationVar(&parsed.options.Timeout, "timeout", 5*time.Minute, "callback timeout")
	for _, name := range []string{"V", "version"} {
		flags.BoolVar(&parsed.options.Version, name, false, "print version")
	}
	flags.SetOutput(io.Discard)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Options{}, ErrHelp
		}

		return Options{}, err
	}

	var err error
	if parsed.authURL != "" {
		parsed.options.AuthURL, err = url.Parse(parsed.authURL)
		if err != nil {
			return Options{}, err
		}
	}
	if parsed.tokenURL != "" {
		parsed.options.TokenURL, err = url.Parse(parsed.tokenURL)
		if err != nil {
			return Options{}, err
		}
	}

	return parsed.options, nil
}

type paramsValue []oauth.Param

func (value *paramsValue) String() string {
	return ""
}

func (value *paramsValue) Set(raw string) error {
	key, parameterValue, _ := strings.Cut(raw, "=")
	*value = append(*value, oauth.Param{Key: key, Value: parameterValue})

	return nil
}
