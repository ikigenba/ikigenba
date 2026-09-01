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

// R-M68L-ADTD
func TestOptionsHasExactExportedStructShape(t *testing.T) {
	t.Parallel()

	got := reflect.TypeOf(options.Options{})
	if got.Kind() != reflect.Struct {
		t.Fatalf("options.Options kind = %v, want %v", got.Kind(), reflect.Struct)
	}
	if got.Name() != "Options" {
		t.Errorf("options.Options type name = %q, want %q", got.Name(), "Options")
	}
	const wantPackagePath = "github.com/ikigenba/ikigenba/oauth/internal/options"
	if got.PkgPath() != wantPackagePath {
		t.Errorf("options.Options package path = %q, want %q", got.PkgPath(), wantPackagePath)
	}

	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{"AuthURL", reflect.TypeOf((*url.URL)(nil))},
		{"TokenURL", reflect.TypeOf((*url.URL)(nil))},
		{"ClientID", reflect.TypeOf(string(""))},
		{"Scope", reflect.TypeOf(string(""))},
		{"ClientSecret", reflect.TypeOf(string(""))},
		{"CallbackHost", reflect.TypeOf(string(""))},
		{"Port", reflect.TypeOf(int(0))},
		{"CallbackPath", reflect.TypeOf(string(""))},
		{"AuthParams", reflect.TypeOf([]oauth.Param(nil))},
		{"TokenParams", reflect.TypeOf([]oauth.Param(nil))},
		{"TokenHeaders", reflect.TypeOf([]oauth.Param(nil))},
		{"NoBrowser", reflect.TypeOf(bool(false))},
		{"Timeout", reflect.TypeOf(time.Duration(0))},
		{"Version", reflect.TypeOf(bool(false))},
	}
	if got.NumField() != len(wantFields) {
		t.Fatalf("options.Options field count = %d, want exactly %d", got.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := got.Field(index)
		if field.Name != want.name {
			t.Errorf("options.Options field %d name = %q, want %q", index, field.Name, want.name)
		}
		if field.Type != want.typ {
			t.Errorf("options.Options field %d (%s) type = %v, want %v", index, field.Name, field.Type, want.typ)
		}
		if !field.IsExported() {
			t.Errorf("options.Options field %d (%s) is unexported (package path %q), want exported", index, field.Name, field.PkgPath)
		}
		if field.Anonymous {
			t.Errorf("options.Options field %d (%s) is anonymous, want named", index, field.Name)
		}
		if field.Tag != "" {
			t.Errorf("options.Options field %d (%s) tag = %q, want empty", index, field.Name, field.Tag)
		}
	}
}

// R-M7GH-O5K2
func TestUsageExportedFunctionSignature(t *testing.T) {
	t.Parallel()

	requireSignature := func(usage func() string) func() string { return usage }
	usage := requireSignature(options.Usage)
	if got := usage(); got == "" {
		t.Error("options.Usage() returned an empty string, want non-empty usage text")
	}
}

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

// R-M50O-WM2O
func TestParseFlagsReturnsExportedHelpSentinel(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"-h"}, {"--help"}} {
		_, err := options.ParseFlags(args)
		if !errors.Is(err, options.ErrHelp) || !errors.Is(err, flag.ErrHelp) {
			t.Errorf("ParseFlags(%q) error = %v, want options.ErrHelp", args, err)
		}
	}
}

// R-M1CZ-RAUL
func TestFlagsHasExactExportedStructShape(t *testing.T) {
	t.Parallel()

	got := reflect.TypeOf(options.Flags{})
	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{"AuthURL", reflect.TypeOf(string(""))},
		{"TokenURL", reflect.TypeOf(string(""))},
		{"ClientID", reflect.TypeOf(string(""))},
		{"Scope", reflect.TypeOf(string(""))},
		{"ClientSecret", reflect.TypeOf(string(""))},
		{"CallbackHost", reflect.TypeOf(string(""))},
		{"Port", reflect.TypeOf(int(0))},
		{"CallbackPath", reflect.TypeOf(string(""))},
		{"AuthParams", reflect.TypeOf([]oauth.Param(nil))},
		{"TokenParams", reflect.TypeOf([]oauth.Param(nil))},
		{"TokenHeaders", reflect.TypeOf([]oauth.Param(nil))},
		{"NoBrowser", reflect.TypeOf(bool(false))},
		{"Timeout", reflect.TypeOf(time.Duration(0))},
		{"Version", reflect.TypeOf(bool(false))},
	}
	if got.NumField() != len(wantFields) {
		t.Fatalf("options.Flags field count = %d, want exactly %d", got.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := got.Field(index)
		if field.Name != want.name || field.Type != want.typ || !field.IsExported() || field.Anonymous || field.Tag != "" {
			t.Errorf("options.Flags field %d = %#v, want exported named field %s %v without tag", index, field, want.name, want.typ)
		}
	}
}

// R-M2KW-52LA
func TestParseFlagsExportedFunctionSignature(t *testing.T) {
	t.Parallel()

	requireSignature := func(parse func([]string) (options.Flags, error)) func([]string) (options.Flags, error) {
		return parse
	}
	if _, err := requireSignature(options.ParseFlags)(nil); err != nil {
		t.Fatalf("ParseFlags(nil) error = %v", err)
	}
}

// R-M3SS-IUBZ
func TestFlagsValidateExportedMethodSignature(t *testing.T) {
	t.Parallel()

	requireSignature := func(validate func(options.Flags) (options.Options, error)) func(options.Flags) (options.Options, error) {
		return validate
	}
	_, err := requireSignature(options.Flags.Validate)(options.Flags{})
	if err == nil {
		t.Fatal("Flags{}.Validate() error = nil, want semantic validation error")
	}
}

// R-JL3B-GSOS
func TestParseFlagsPopulatesSuppliedFlagValues(t *testing.T) {
	t.Parallel()

	got, err := options.ParseFlags([]string{
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
		t.Fatalf("ParseFlags() error = %v", err)
	}
	want := options.Flags{ //nolint:gosec // These are inert parser test values, not credentials.
		AuthURL:      "https://identity.example/authorize?audience=api",
		TokenURL:     "https://identity.example/oauth/token",
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
		t.Errorf("ParseFlags() = %#v, want %#v", got, want)
	}

	for _, args := range [][]string{{"-V"}, {"--version"}} {
		got, err := options.ParseFlags(args)
		if err != nil {
			t.Errorf("ParseFlags(%q) error = %v", args, err)
			continue
		}
		if !got.Version {
			t.Errorf("ParseFlags(%q).Version = false, want true", args)
		}
	}
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		if _, err := options.ParseFlags(args); !errors.Is(err, options.ErrHelp) {
			t.Errorf("ParseFlags(%q) error = %v, want ErrHelp", args, err)
		}
	}
}

// R-JMB7-UKFH
func TestParseFlagsSuppliesDocumentedDefaults(t *testing.T) {
	t.Parallel()

	got, err := options.ParseFlags(nil)
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	want := options.Flags{
		CallbackHost: "localhost",
		CallbackPath: "/callback",
		Timeout:      5 * time.Minute,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFlags() = %#v, want %#v", got, want)
	}
}

// R-JNJ4-8C66
func TestParseFlagsPreservesEveryRepeatedParameterInOrder(t *testing.T) {
	t.Parallel()

	got, err := options.ParseFlags([]string{
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
		t.Fatalf("ParseFlags() error = %v", err)
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

// R-JOR0-M3WV
func TestParseFlagsDefersSemanticValidation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing required flags"},
		{
			name: "reserved authorize parameter",
			args: []string{
				"--auth-url", "https://identity.example/authorize",
				"--token-url", "https://identity.example/token",
				"--client-id", "client-123",
				"--auth-param", "client_id=override",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			flags, err := options.ParseFlags(test.args)
			if err != nil {
				t.Fatalf("ParseFlags() error = %v, want nil", err)
			}
			if _, err := flags.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want semantic validation error")
			}
		})
	}
}

// R-JPYW-ZVNK
func TestFlagsValidateReturnsParsedURLsWithoutIOParameters(t *testing.T) {
	t.Parallel()

	flags := options.Flags{ //nolint:gosec // These are inert validation test values, not credentials.
		AuthURL:      "https://identity.example/authorize?audience=api",
		TokenURL:     "https://identity.example/oauth/token",
		ClientID:     "client-123",
		Scope:        "openid profile",
		CallbackHost: "localhost",
		CallbackPath: "/callback",
		Timeout:      5 * time.Minute,
	}
	got, err := flags.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	wantAuthURL := &url.URL{
		Scheme:   "https",
		Host:     "identity.example",
		Path:     "/authorize",
		RawQuery: "audience=api",
	}
	wantTokenURL := &url.URL{
		Scheme: "https",
		Host:   "identity.example",
		Path:   "/oauth/token",
	}
	if !reflect.DeepEqual(got.AuthURL, wantAuthURL) {
		t.Errorf("Options.AuthURL = %#v, want %#v", got.AuthURL, wantAuthURL)
	}
	if !reflect.DeepEqual(got.TokenURL, wantTokenURL) {
		t.Errorf("Options.TokenURL = %#v, want %#v", got.TokenURL, wantTokenURL)
	}

	validateType := reflect.TypeOf(options.Flags.Validate)
	if validateType.NumIn() != 1 || validateType.NumOut() != 2 {
		t.Errorf("Flags.Validate signature = %v, want func(Flags) (Options, error) with no I/O parameter", validateType)
	}
}
