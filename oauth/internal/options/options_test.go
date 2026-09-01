package options_test

import (
	"errors"
	"flag"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
	"github.com/ikigenba/ikigenba/oauth/internal/options"
)

const expectedFlagsBlock = `Usage: oauth [flags]

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
`

// R-R5YG-YXBO
func TestUsageBeginsWithExactFlagsBlock(t *testing.T) {
	t.Parallel()

	got := options.Usage()
	if len(got) < len(expectedFlagsBlock) {
		t.Fatalf("len(Usage()) = %d, want at least %d", len(got), len(expectedFlagsBlock))
	}
	if got[:len(expectedFlagsBlock)] != expectedFlagsBlock {
		t.Errorf("Usage() prefix = %q, want %q", got[:len(expectedFlagsBlock)], expectedFlagsBlock)
	}
}

// R-R8E9-QGT2
func TestUsageNamesEveryFlagSpelling(t *testing.T) {
	t.Parallel()

	lines := strings.Split(options.Usage(), "\n")
	flagRows := []string{
		"  --auth-url string",
		"  --token-url string",
		"  --client-id string",
		"  --scope string",
		"  --client-secret string",
		"  --callback-host string",
		"  --port int",
		"  --callback-path string",
		"  --auth-param key=value",
		"  --token-param key=value",
		"  --token-header key=value",
		"  --no-browser",
		"  --timeout duration",
		"  -h, --help",
		"  -V, --version",
	}
	lastFlagLine := 3 + 2*(len(flagRows)-1)
	if len(lines) <= lastFlagLine {
		t.Fatalf("Usage() has %d lines, need flag row through line %d", len(lines), lastFlagLine+1)
	}
	for index, want := range flagRows {
		line := 3 + 2*index
		if lines[line] != want {
			t.Errorf("Usage() line %d = %q, want flag row %q", line+1, lines[line], want)
		}
	}
}

// R-QK0A-31Z6
func TestParseAcceptsEveryFlagAndPopulatesOptions(t *testing.T) {
	t.Parallel()

	got, err := options.Parse([]string{
		"--auth-url", "https://identity.example/authorize?audience=api",
		"--token-url", "https://identity.example/oauth/token",
		"--client-id", "client-123",
		"--scope", "openid profile",
		"--client-secret", "secret-456",
		"--callback-host", "login.example",
		"--port", "48123",
		"--callback-path", "/oauth/return",
		"--auth-param", "prompt=consent",
		"--token-param", "resource=calendar",
		"--token-header", "X-Tenant=west",
		"--no-browser",
		"--timeout", "37s",
		"--version",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	wantAuthURL, err := url.Parse("https://identity.example/authorize?audience=api")
	if err != nil {
		t.Fatalf("url.Parse(auth URL) error = %v", err)
	}
	wantTokenURL, err := url.Parse("https://identity.example/oauth/token")
	if err != nil {
		t.Fatalf("url.Parse(token URL) error = %v", err)
	}
	want := options.Options{
		AuthURL:      wantAuthURL,
		TokenURL:     wantTokenURL,
		ClientID:     "client-123",
		Scope:        "openid profile",
		ClientSecret: "secret-456",
		CallbackHost: "login.example",
		Port:         48123,
		CallbackPath: "/oauth/return",
		AuthParams:   []oauth.Param{{Key: "prompt", Value: "consent"}},
		TokenParams:  []oauth.Param{{Key: "resource", Value: "calendar"}},
		TokenHeaders: []oauth.Param{{Key: "X-Tenant", Value: "west"}},
		NoBrowser:    true,
		Timeout:      37 * time.Second,
		Version:      true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() = %#v, want %#v", got, want)
	}

	for _, args := range [][]string{{"-V"}, {"--version"}} {
		got, err := options.Parse(args)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", args, err)
			continue
		}
		if !got.Version {
			t.Errorf("Parse(%q).Version = false, want true", args)
		}
	}
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		_, err := options.Parse(args)
		if !errors.Is(err, options.ErrHelp) || !errors.Is(err, flag.ErrHelp) {
			t.Errorf("Parse(%q) error = %v, want ErrHelp", args, err)
		}
	}
}

// R-QL86-GTPV
func TestParseSuppliesDocumentedDefaults(t *testing.T) {
	t.Parallel()

	got, err := options.Parse([]string{
		"--auth-url", "https://identity.example/authorize",
		"--token-url", "https://identity.example/token",
		"--client-id", "client-123",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	wantAuthURL, err := url.Parse("https://identity.example/authorize")
	if err != nil {
		t.Fatalf("url.Parse(auth URL) error = %v", err)
	}
	wantTokenURL, err := url.Parse("https://identity.example/token")
	if err != nil {
		t.Fatalf("url.Parse(token URL) error = %v", err)
	}
	want := options.Options{
		AuthURL:      wantAuthURL,
		TokenURL:     wantTokenURL,
		ClientID:     "client-123",
		CallbackHost: "localhost",
		CallbackPath: "/callback",
		Timeout:      5 * time.Minute,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() = %#v, want %#v", got, want)
	}
}

// R-QMG2-ULGK
func TestParsePreservesEveryRepeatedParameterInOrder(t *testing.T) {
	t.Parallel()

	got, err := options.Parse([]string{
		"--auth-url", "https://identity.example/authorize",
		"--token-url", "https://identity.example/token",
		"--client-id", "client-123",
		"--auth-param", "first=one",
		"--token-param", "token-first=alpha",
		"--token-header", "X-First=1",
		"--auth-param", "same=",
		"--token-header", "X-First=2",
		"--token-param", "same=",
		"--auth-param", "first=three",
		"--token-param", "token-last=omega",
		"--token-header", "X-Last=3",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	wantAuthParams := []oauth.Param{
		{Key: "first", Value: "one"},
		{Key: "same", Value: ""},
		{Key: "first", Value: "three"},
	}
	wantTokenParams := []oauth.Param{
		{Key: "token-first", Value: "alpha"},
		{Key: "same", Value: ""},
		{Key: "token-last", Value: "omega"},
	}
	wantTokenHeaders := []oauth.Param{
		{Key: "X-First", Value: "1"},
		{Key: "X-First", Value: "2"},
		{Key: "X-Last", Value: "3"},
	}
	if !reflect.DeepEqual(got.AuthParams, wantAuthParams) {
		t.Errorf("AuthParams = %#v, want %#v", got.AuthParams, wantAuthParams)
	}
	if !reflect.DeepEqual(got.TokenParams, wantTokenParams) {
		t.Errorf("TokenParams = %#v, want %#v", got.TokenParams, wantTokenParams)
	}
	if !reflect.DeepEqual(got.TokenHeaders, wantTokenHeaders) {
		t.Errorf("TokenHeaders = %#v, want %#v", got.TokenHeaders, wantTokenHeaders)
	}
}
