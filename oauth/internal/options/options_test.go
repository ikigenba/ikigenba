package options_test

import (
	"errors"
	"flag"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
	"github.com/ikigenba/ikigenba/oauth/internal/options"
)

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

	got, err := options.Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) error = %v", err)
	}
	want := options.Options{
		CallbackHost: "localhost",
		CallbackPath: "/callback",
		Timeout:      5 * time.Minute,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(nil) = %#v, want %#v", got, want)
	}
}

// R-QMG2-ULGK
func TestParsePreservesEveryRepeatedParameterInOrder(t *testing.T) {
	t.Parallel()

	got, err := options.Parse([]string{
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
