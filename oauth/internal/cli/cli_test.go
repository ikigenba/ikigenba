package cli_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/callback"
	"github.com/ikigenba/ikigenba/oauth/internal/cli"
	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

type failLauncher struct {
	t *testing.T
}

func (launcher failLauncher) Open(url string) error {
	launcher.t.Fatalf("Launcher.Open(%q) called during control handling", url)

	return nil
}

func failDeps(t *testing.T) cli.Deps {
	t.Helper()

	return cli.Deps{
		Launcher: failLauncher{t: t},
		Listen: func(network, address string) (net.Listener, error) {
			t.Fatalf("Listen(%q, %q) called during control handling", network, address)

			return nil, nil
		},
	}
}

// R-E5L7-OSOC
func TestRunReturnsInProcess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), validArgs(), &stdout, &stderr, cli.Deps{})

	if code != 0 {
		t.Fatalf("Run returned exit code %d, want 0 for the phase scaffold", code)
	}
}

func validArgs() []string {
	return []string{
		"--auth-url", "https://identity.example/authorize",
		"--token-url", "https://identity.example/token",
		"--client-id", "client-123",
	}
}

// R-R4QK-L5KZ
func TestRunValidatesBeforeObservableSideEffects(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "missing auth URL",
			args: []string{"--token-url", "https://identity.example/token", "--client-id", "client-123"},
		},
		{
			name: "missing token URL",
			args: []string{"--auth-url", "https://identity.example/authorize", "--client-id", "client-123"},
		},
		{
			name: "missing client ID",
			args: []string{"--auth-url", "https://identity.example/authorize", "--token-url", "https://identity.example/token"},
		},
		{
			name: "unparsable auth URL",
			args: []string{"--auth-url", ":", "--token-url", "https://identity.example/token", "--client-id", "client-123"},
		},
		{
			name: "unparsable token URL",
			args: []string{"--auth-url", "https://identity.example/authorize", "--token-url", ":", "--client-id", "client-123"},
		},
		{name: "auth param without equals", args: append(validArgs(), "--auth-param", "prompt")},
		{name: "auth param with empty key", args: append(validArgs(), "--auth-param", "=login")},
		{name: "token param without equals", args: append(validArgs(), "--token-param", "resource")},
		{name: "token param with empty key", args: append(validArgs(), "--token-param", "=api")},
		{name: "token header without equals", args: append(validArgs(), "--token-header", "X-Trace-ID")},
		{name: "token header with empty key", args: append(validArgs(), "--token-header", "=trace-123")},
		{name: "reserved authorize param", args: append(validArgs(), "--auth-param", "state=caller-state")},
		{name: "reserved redirect URI authorize param", args: append(validArgs(), "--auth-param", "redirect_uri=https://client.example/callback")},
		{name: "reserved token param", args: append(validArgs(), "--token-param", "code=caller-code")},
		{name: "multiple client authentication methods", args: append(validArgs(), "--client-secret", "secret", "--token-header", "aUtHoRiZaTiOn=Basic abc")},
		{name: "zero timeout", args: append(validArgs(), "--timeout", "0s")},
		{name: "negative timeout", args: append(validArgs(), "--timeout", "-1s")},
		{name: "callback path without leading slash", args: append(validArgs(), "--callback-path", "callback")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), test.args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2; stderr = %q", code, stderr.String())
			}
		})
	}
}

// R-QZUZ-22M7
func TestRunRejectsMultipleClientAuthenticationMethods(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
	}{
		{name: "canonical", headers: []string{"Authorization=Basic abc"}},
		{name: "lowercase", headers: []string{"authorization=Basic abc"}},
		{name: "uppercase", headers: []string{"AUTHORIZATION=Basic abc"}},
		{name: "mixed case", headers: []string{"aUtHoRiZaTiOn=Basic abc"}},
		{name: "later repeated header", headers: []string{"X-Trace-ID=trace-123", "Authorization=Basic abc"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append(validArgs(), "--client-secret", "secret")
			for _, header := range test.headers {
				args = append(args, "--token-header", header)
			}
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2", code)
			}
			for _, flag := range []string{"--client-secret", "--token-header"} {
				if !strings.Contains(stderr.String(), flag) {
					t.Errorf("stderr = %q, want offending flag %q", stderr.String(), flag)
				}
			}
		})
	}
}

// R-R12V-FUCW
func TestRunAcceptsOneClientAuthenticationMethod(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "client secret alone", args: []string{"--client-secret", "secret"}},
		{name: "Authorization header alone", args: []string{"--token-header", "Authorization=Basic abc"}},
		{name: "client secret with unrelated header", args: []string{"--client-secret", "secret", "--token-header", "X-Trace-ID=trace-123"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append(validArgs(), test.args...)
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if code != 0 {
				t.Errorf("Run() exit code = %d, want 0; stderr = %q", code, stderr.String())
			}
			if stderr.String() != "" {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

// R-R2AR-TM3L
func TestRunRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []string{"0s", "-1s"} {
		t.Run(timeout, func(t *testing.T) {
			args := append(validArgs(), "--timeout", timeout)
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), "--timeout") {
				t.Errorf("stderr = %q, want offending flag --timeout", stderr.String())
			}
		})
	}
}

// R-R3IO-7DUA
func TestRunRejectsCallbackPathWithoutLeadingSlash(t *testing.T) {
	for _, path := range []string{"callback", ""} {
		name := path
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			args := append(validArgs(), "--callback-path", path)
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), "--callback-path") {
				t.Errorf("stderr = %q, want offending flag --callback-path", stderr.String())
			}
		})
	}
}

// R-QTRH-57WQ
func TestRunRejectsMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name    string
		missing string
		args    []string
	}{
		{
			name:    "auth URL",
			missing: "--auth-url",
			args:    []string{"--token-url", "https://identity.example/token", "--client-id", "client-123"},
		},
		{
			name:    "token URL",
			missing: "--token-url",
			args:    []string{"--auth-url", "https://identity.example/authorize", "--client-id", "client-123"},
		},
		{
			name:    "client ID",
			missing: "--client-id",
			args:    []string{"--auth-url", "https://identity.example/authorize", "--token-url", "https://identity.example/token"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), test.args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2", code)
			}
			if stdout.String() != "" {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.missing) {
				t.Errorf("stderr = %q, want missing flag %q", stderr.String(), test.missing)
			}
		})
	}
}

// R-QUZD-IZNF
func TestRunRejectsUnparsableEndpointURLs(t *testing.T) {
	tests := []struct {
		name string
		flag string
		args []string
	}{
		{
			name: "auth URL",
			flag: "--auth-url",
			args: []string{"--auth-url", ":", "--token-url", "https://identity.example/token", "--client-id", "client-123"},
		},
		{
			name: "token URL",
			flag: "--token-url",
			args: []string{"--auth-url", "https://identity.example/authorize", "--token-url", ":", "--client-id", "client-123"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), test.args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), test.flag) {
				t.Errorf("stderr = %q, want offending flag %q", stderr.String(), test.flag)
			}
		})
	}
}

// R-QW79-WRE4
func TestRunRejectsMalformedRepeatedParameters(t *testing.T) {
	tests := []struct {
		name string
		flag string
		raw  string
	}{
		{name: "auth param without equals", flag: "--auth-param", raw: "auth-missing-equals"},
		{name: "auth param empty key", flag: "--auth-param", raw: "=auth-empty-key"},
		{name: "token param without equals", flag: "--token-param", raw: "token-missing-equals"},
		{name: "token param empty key", flag: "--token-param", raw: "=token-empty-key"},
		{name: "token header without equals", flag: "--token-header", raw: "header-missing-equals"},
		{name: "token header empty key", flag: "--token-header", raw: "=header-empty-key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append(validArgs(), test.flag, test.raw)
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), test.flag) {
				t.Errorf("stderr = %q, want offending flag %q", stderr.String(), test.flag)
			}
			if !strings.Contains(stderr.String(), test.raw) {
				t.Errorf("stderr = %q, want offending value %q", stderr.String(), test.raw)
			}
		})
	}
}

// R-QXF6-AJ4T
func TestRunAuthParamDecisionAgreesWithOAuthReservedPredicate(t *testing.T) {
	tests := []struct {
		key      string
		reserved bool
	}{
		{key: "response_type", reserved: true},
		{key: "client_id", reserved: true},
		{key: "redirect_uri", reserved: true},
		{key: "state", reserved: true},
		{key: "code_challenge", reserved: true},
		{key: "code_challenge_method", reserved: true},
		{key: "scope", reserved: true},
		{key: "prompt", reserved: false},
		{key: "audience", reserved: false},
		{key: "Response_type", reserved: false},
		{key: "client-id", reserved: false},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if got := oauth.ReservedAuthorizeParam(test.key); got != test.reserved {
				t.Fatalf("ReservedAuthorizeParam(%q) = %t, want %t", test.key, got, test.reserved)
			}

			args := append(validArgs(), "--auth-param", test.key+"=value")
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if test.reserved {
				if code != 2 {
					t.Errorf("Run() exit code = %d, want 2 for reserved key %q", code, test.key)
				}
				if !strings.Contains(stderr.String(), "--auth-param") {
					t.Errorf("stderr = %q, want flag --auth-param", stderr.String())
				}
				if !strings.Contains(stderr.String(), test.key) {
					t.Errorf("stderr = %q, want key %q", stderr.String(), test.key)
				}

				return
			}

			if code != 0 {
				t.Errorf("Run() exit code = %d, want 0 for non-reserved key %q; stderr = %q", code, test.key, stderr.String())
			}
			if stderr.String() != "" {
				t.Errorf("stderr = %q, want empty for non-reserved key %q", stderr.String(), test.key)
			}
		})
	}
}

// R-LCAT-LU5C
func TestRunRedirectURIAuthParamNamesConfigurationFlags(t *testing.T) {
	args := append(validArgs(), "--auth-param", "redirect_uri=https://client.example/callback")
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

	if code != 2 {
		t.Errorf("Run() exit code = %d, want 2", code)
	}
	for _, flag := range []string{"--callback-host", "--port", "--callback-path"} {
		if !strings.Contains(stderr.String(), flag) {
			t.Errorf("stderr = %q, want configuration flag %q", stderr.String(), flag)
		}
	}
}

// R-QYN2-OAVI
func TestRunTokenParamDecisionAgreesWithOAuthReservedPredicate(t *testing.T) {
	tests := []struct {
		key      string
		reserved bool
	}{
		{key: "grant_type", reserved: true},
		{key: "code", reserved: true},
		{key: "code_verifier", reserved: true},
		{key: "redirect_uri", reserved: true},
		{key: "client_id", reserved: true},
		{key: "client_secret", reserved: true},
		{key: "resource", reserved: false},
		{key: "audience", reserved: false},
		{key: "Grant_type", reserved: false},
		{key: "client-id", reserved: false},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if got := oauth.ReservedTokenParam(test.key); got != test.reserved {
				t.Fatalf("ReservedTokenParam(%q) = %t, want %t", test.key, got, test.reserved)
			}

			args := append(validArgs(), "--token-param", test.key+"=value")
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))

			if test.reserved {
				if code != 2 {
					t.Errorf("Run() exit code = %d, want 2 for reserved key %q", code, test.key)
				}
				if !strings.Contains(stderr.String(), "--token-param") {
					t.Errorf("stderr = %q, want flag --token-param", stderr.String())
				}
				if !strings.Contains(stderr.String(), test.key) {
					t.Errorf("stderr = %q, want key %q", stderr.String(), test.key)
				}

				return
			}

			if code != 0 {
				t.Errorf("Run() exit code = %d, want 0 for non-reserved key %q; stderr = %q", code, test.key, stderr.String())
			}
			if stderr.String() != "" {
				t.Errorf("stderr = %q, want empty for non-reserved key %q", stderr.String(), test.key)
			}
		})
	}
}

// R-QOVV-M4XY
func TestRunRoutesUnknownFlagToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(
		context.Background(),
		[]string{"--distinctively-unknown"},
		&stdout,
		&stderr,
		failDeps(t),
	)

	if code != 2 {
		t.Errorf("Run() exit code = %d, want 2", code)
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	const usage = "Usage: oauth [flags]\n"
	if !strings.Contains(stderr.String(), usage) {
		t.Errorf("stderr = %q, want it to contain usage %q", stderr.String(), usage)
	}
}

func TestRunQuotesControlCharactersInParseErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	const rawFlag = "--unknown\ninjected"
	_ = cli.Run(
		context.Background(),
		[]string{rawFlag},
		&stdout,
		&stderr,
		failDeps(t),
	)

	if strings.Contains(stderr.String(), "unknown\ninjected") {
		t.Errorf("stderr contains raw newline from argv: %q", stderr.String())
	}
}

// R-QQ3R-ZWON
func TestRunReportsUnknownFlagCauseExactlyOnce(t *testing.T) {
	var stdout, stderr bytes.Buffer
	const cause = "flag provided but not defined: -one-off-unknown"
	_ = cli.Run(
		context.Background(),
		[]string{"--one-off-unknown"},
		&stdout,
		&stderr,
		failDeps(t),
	)

	combined := stdout.String() + stderr.String()
	if got := strings.Count(combined, cause); got != 1 {
		t.Errorf("cause occurs %d times across output, want exactly 1; output = %q", got, combined)
	}
	if strings.Contains(stdout.String(), cause) {
		t.Errorf("stdout = %q, want no usage-error cause", stdout.String())
	}
}

// R-QRBO-DOFC
func TestRunHelpShortCircuitsBeforeLoginSideEffects(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{flag}, &stdout, &stderr, failDeps(t))

			if code != 0 {
				t.Errorf("Run(%q) exit code = %d, want 0", flag, code)
			}
		})
	}
}

// R-QSJK-RG61
func TestRunVersionShortCircuitsBeforeLoginSideEffects(t *testing.T) {
	for _, flag := range []string{"-V", "--version"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{flag}, &stdout, &stderr, failDeps(t))

			if code != 0 {
				t.Errorf("Run(%q) exit code = %d, want 0", flag, code)
			}
		})
	}
}

// R-E6T4-2KF1
func TestDepsExactFields(t *testing.T) {
	var launcher browser.Launcher
	var entropy io.Reader
	var client *http.Client
	var listen callback.ListenFunc
	_ = cli.Deps{
		Launcher:   launcher,
		Entropy:    entropy,
		HTTPClient: client,
		Listen:     listen,
	}

	type expectedField struct {
		name string
		typ  reflect.Type
	}
	want := []expectedField{
		{"Launcher", reflect.TypeOf((*browser.Launcher)(nil)).Elem()},
		{"Entropy", reflect.TypeOf((*io.Reader)(nil)).Elem()},
		{"HTTPClient", reflect.TypeOf((*http.Client)(nil))},
		{"Listen", reflect.TypeOf((*callback.ListenFunc)(nil)).Elem()},
	}

	typ := reflect.TypeOf(cli.Deps{})
	if typ.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want exactly %d", typ.NumField(), len(want))
	}
	for i, expected := range want {
		field := typ.Field(i)
		if field.Name != expected.name {
			t.Errorf("Deps field %d is named %q, want %q", i, field.Name, expected.name)
		}
		if field.Type != expected.typ {
			t.Errorf("Deps.%s has type %v, want %v", field.Name, field.Type, expected.typ)
		}
	}
}
