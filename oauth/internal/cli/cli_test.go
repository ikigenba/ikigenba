package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/callback"
	"github.com/ikigenba/ikigenba/oauth/internal/cli"
	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

const completeUsage = `Usage: oauth [flags]

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

type launcherFunc func(string) error

func (open launcherFunc) Open(authorizeURL string) error {
	return open(authorizeURL)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type successfulRun struct {
	authorizeURL string
	boundPort    int
	callLog      []string
	tokenForm    url.Values
	tokenCalls   int
	exitCode     int
	stdout       []byte
	stderr       []byte
}

type runConfig struct {
	args                 []string
	callbackQuery        func(state string) url.Values
	disableCallback      bool
	callbackStderrMarker string
	launcherError        error
	tokenStatus          int
	tokenBody            []byte
	stdout               io.Writer
	stderr               io.Writer
	outerTimeout         time.Duration
}

type synchronizedBuffer struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	changed chan struct{}
}

func newSynchronizedBuffer() *synchronizedBuffer {
	return &synchronizedBuffer{changed: make(chan struct{}, 1)}
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	written, err := buffer.buffer.Write(data)
	buffer.mu.Unlock()
	select {
	case buffer.changed <- struct{}{}:
	default:
	}

	return written, err
}

func (buffer *synchronizedBuffer) Bytes() []byte {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func (buffer *synchronizedBuffer) waitForAuthorizeURL(ctx context.Context, marker string) (string, error) {
	for {
		text := string(buffer.Bytes())
		if strings.Contains(text, marker) {
			for _, line := range strings.Split(text, "\n") {
				if isCompleteAuthorizeURL(line) {
					return line, nil
				}
			}
		}

		select {
		case <-buffer.changed:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func runLogin(t *testing.T, config runConfig) successfulRun {
	t.Helper()

	outerTimeout := config.outerTimeout
	if outerTimeout == 0 {
		outerTimeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), outerTimeout)
	defer cancel()
	var capture successfulRun
	callbackDone := make(chan error, 1)
	var listenConfig net.ListenConfig
	listen := func(network, address string) (net.Listener, error) {
		capture.callLog = append(capture.callLog, "listen:"+network)
		listener, err := listenConfig.Listen(ctx, network, address)
		if err == nil && network == "tcp4" {
			_, portText, splitErr := net.SplitHostPort(listener.Addr().String())
			if splitErr != nil {
				_ = listener.Close()

				return nil, splitErr
			}
			capture.boundPort, err = strconv.Atoi(portText)
			if err != nil {
				_ = listener.Close()

				return nil, err
			}
		}

		return listener, err
	}
	launcher := launcherFunc(func(authorizeURL string) error {
		capture.callLog = append(capture.callLog, "launch")
		capture.authorizeURL = authorizeURL
		if config.disableCallback || config.callbackStderrMarker != "" {
			return config.launcherError
		}
		go func() {
			callbackDone <- sendCallback(ctx, authorizeURL, config.callbackQuery)
		}()

		return config.launcherError
	})
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		capture.tokenCalls++
		if err := request.ParseForm(); err != nil {
			return nil, err
		}
		capture.tokenForm = request.PostForm

		status := config.tokenStatus
		if status == 0 {
			status = http.StatusOK
		}
		body := config.tokenBody
		if body == nil {
			body = []byte(`{"access_token":"synthetic"}`)
		}

		return &http.Response{
			StatusCode: status,
			Status:     strconv.Itoa(status) + " " + http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	})

	var stdoutBuffer bytes.Buffer
	stderrBuffer := newSynchronizedBuffer()
	stdout := config.stdout
	if stdout == nil {
		stdout = &stdoutBuffer
	}
	stderr := config.stderr
	if stderr == nil {
		stderr = stderrBuffer
	}
	if config.callbackStderrMarker != "" {
		if config.stderr != nil {
			t.Fatal("callbackStderrMarker requires the harness's synchronized stderr")
		}
		go func() {
			authorizeURL, err := stderrBuffer.waitForAuthorizeURL(ctx, config.callbackStderrMarker)
			if err == nil {
				err = sendCallback(ctx, authorizeURL, config.callbackQuery)
			}
			callbackDone <- err
		}()
	}
	capture.exitCode = cli.Run(ctx, config.args, stdout, stderr, cli.Deps{
		Launcher:   launcher,
		Entropy:    bytes.NewReader(make([]byte, 96)),
		HTTPClient: &http.Client{Transport: transport},
		Listen:     listen,
	})
	capture.stdout = append([]byte(nil), stdoutBuffer.Bytes()...)
	capture.stderr = stderrBuffer.Bytes()
	if capture.authorizeURL == "" && config.stderr == nil {
		capture.authorizeURL = authorizeURLFromOutput(string(capture.stderr))
	}
	if !config.disableCallback {
		select {
		case err := <-callbackDone:
			if err != nil {
				t.Fatalf("callback request error = %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("callback request did not finish: %v", ctx.Err())
		}
	}

	return capture
}

func sendCallback(ctx context.Context, authorizeURL string, callbackQuery func(string) url.Values) error {
	parsedAuthorize, err := url.Parse(authorizeURL)
	if err != nil {
		return err
	}
	state := parsedAuthorize.Query().Get("state")
	callbackURL, err := url.Parse(parsedAuthorize.Query().Get("redirect_uri"))
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(callbackURL.Host)
	if err != nil {
		return err
	}
	callbackURL.Host = net.JoinHostPort("127.0.0.1", port)
	query := url.Values{"state": {state}, "code": {"known-code"}}
	if callbackQuery != nil {
		query = callbackQuery(state)
	}
	callbackURL.RawQuery = query.Encode()

	client := http.Client{Timeout: 2 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL.String(), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}

	return response.Body.Close()
}

func authorizeURLFromOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if isCompleteAuthorizeURL(line) {
			return line
		}
	}

	return ""
}

func isCompleteAuthorizeURL(candidate string) bool {
	parsed, err := url.Parse(candidate)
	if err != nil || !parsed.IsAbs() {
		return false
	}
	query := parsed.Query()

	return query.Get("state") != "" && query.Get("redirect_uri") != ""
}

func requireSuccessfulLogin(t *testing.T, args []string) successfulRun {
	t.Helper()
	capture := runLogin(t, runConfig{args: args})
	if capture.exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", capture.exitCode, capture.stderr)
	}

	return capture
}

// R-EMNT-1L22
func TestRunNoBrowserSkipsLauncherAndPrintsAuthorizeURL(t *testing.T) {
	tokenBody := []byte("phase-seven-no-browser-token\x00")
	capture := runLogin(t, runConfig{
		args:                 append(validArgs(), "--no-browser"),
		callbackStderrMarker: "https://identity.example/authorize?",
		tokenBody:            tokenBody,
	})

	if capture.exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", capture.exitCode, capture.stderr)
	}
	if calls := countCalls(capture.callLog, "launch"); calls != 0 {
		t.Errorf("Launcher.Open calls = %d, want 0; call log = %v", calls, capture.callLog)
	}
	if capture.authorizeURL == "" {
		t.Fatal("stderr did not contain a complete authorize URL")
	}
	if !bytes.Contains(capture.stderr, []byte(capture.authorizeURL+"\n")) {
		t.Errorf("stderr = %q, want complete authorize URL %q", capture.stderr, capture.authorizeURL)
	}
	if capture.tokenCalls != 1 {
		t.Errorf("token exchange calls = %d, want 1", capture.tokenCalls)
	}
	if !bytes.Equal(capture.stdout, tokenBody) {
		t.Errorf("stdout = %q, want exact token bytes %q", capture.stdout, tokenBody)
	}
}

// R-ENVP-FCSR
func TestRunContinuesAfterBrowserLaunchError(t *testing.T) {
	launchErr := errors.New("distinctive phase-seven browser start failure")
	tokenBody := []byte("phase-seven-launch-error-token\r\n")
	capture := runLogin(t, runConfig{
		args:                 validArgs(),
		callbackStderrMarker: launchErr.Error(),
		launcherError:        launchErr,
		tokenBody:            tokenBody,
	})

	if capture.exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", capture.exitCode, capture.stderr)
	}
	if calls := countCalls(capture.callLog, "launch"); calls != 1 {
		t.Errorf("Launcher.Open calls = %d, want 1; call log = %v", calls, capture.callLog)
	}
	if capture.authorizeURL == "" || !bytes.Contains(capture.stderr, []byte(capture.authorizeURL+"\n")) {
		t.Errorf("stderr = %q, want launched authorize URL %q", capture.stderr, capture.authorizeURL)
	}
	if !bytes.Contains(capture.stderr, []byte(launchErr.Error())) {
		t.Errorf("stderr = %q, want launch error %q", capture.stderr, launchErr)
	}
	if bytes.Contains(capture.stdout, []byte(launchErr.Error())) {
		t.Errorf("stdout = %q, must not contain launch error %q", capture.stdout, launchErr)
	}
	if capture.tokenCalls != 1 || capture.tokenForm.Get("code") != "known-code" {
		t.Errorf("token exchange calls = %d, code = %q; want one exchange after callback with known-code",
			capture.tokenCalls, capture.tokenForm.Get("code"))
	}
	if !bytes.Equal(capture.stdout, tokenBody) {
		t.Errorf("stdout = %q, want exact token bytes %q", capture.stdout, tokenBody)
	}
}

func countCalls(callLog []string, want string) int {
	count := 0
	for _, call := range callLog {
		if call == want {
			count++
		}
	}

	return count
}

// R-EJ03-W9TZ
func TestRunWritesTokenResponseBytesVerbatimToStdout(t *testing.T) {
	responseBody := []byte(" \tplain-text token response\r\n{not-json}\x00\xff ")
	capture := runLogin(t, runConfig{
		args:      validArgs(),
		tokenBody: responseBody,
	})

	if capture.exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", capture.exitCode, capture.stderr)
	}
	if len(capture.stdout) != len(responseBody) {
		t.Errorf("stdout length = %d, want exactly %d; stdout = %q", len(capture.stdout), len(responseBody), capture.stdout)
	}
	if !bytes.Equal(capture.stdout, responseBody) {
		t.Errorf("stdout bytes = %q, want exact token response bytes %q", capture.stdout, responseBody)
	}
}

// R-EK80-A1KO
// llm-lint:ignore overclaiming-exhaustive-test-name
func TestRunKeepsStdoutEmptyForEveryFailureMode(t *testing.T) {
	tests := []struct {
		name     string
		wantCode int
		run      func(t *testing.T) (int, []byte)
	}{
		{
			name:     "usage error",
			wantCode: 2,
			run: func(t *testing.T) (int, []byte) {
				var stdout, stderr bytes.Buffer
				code := cli.Run(context.Background(), []string{"--phase-six-unknown"}, &stdout, &stderr, failDeps(t))

				return code, stdout.Bytes()
			},
		},
		{
			name:     "callback state mismatch",
			wantCode: 1,
			run: func(t *testing.T) (int, []byte) {
				capture := runLogin(t, runConfig{
					args: validArgs(),
					callbackQuery: func(string) url.Values {
						return url.Values{"state": {"wrong-state"}, "code": {"plausible-code"}}
					},
				})

				return capture.exitCode, capture.stdout
			},
		},
		{
			name:     "provider error redirect",
			wantCode: 1,
			run: func(t *testing.T) (int, []byte) {
				capture := runLogin(t, runConfig{
					args: validArgs(),
					callbackQuery: func(state string) url.Values {
						return url.Values{
							"state":             {state},
							"error":             {"access_denied_phase_six"},
							"error_description": {"provider declined phase six request"},
						}
					},
				})

				return capture.exitCode, capture.stdout
			},
		},
		{
			name:     "non-2xx token response",
			wantCode: 1,
			run: func(t *testing.T) (int, []byte) {
				capture := runLogin(t, runConfig{
					args:        validArgs(),
					tokenStatus: http.StatusBadGateway,
					tokenBody:   []byte("distinctive phase six upstream failure"),
				})

				return capture.exitCode, capture.stdout
			},
		},
		{
			name:     "expired callback deadline",
			wantCode: 1,
			run: func(t *testing.T) (int, []byte) {
				capture := runLogin(t, runConfig{
					args:            append(validArgs(), "--timeout", "20ms"),
					disableCallback: true,
					outerTimeout:    2 * time.Second,
				})

				return capture.exitCode, capture.stdout
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout := test.run(t)
			if code != test.wantCode {
				t.Errorf("Run() exit code = %d, want %d", code, test.wantCode)
			}
			if len(stdout) != 0 {
				t.Errorf("stdout length = %d, want exactly 0; stdout = %q", len(stdout), stdout)
			}
		})
	}
}

// R-ELFW-NTBD
func TestRunRoutesHumanOutputOnlyToInjectedStderr(t *testing.T) {
	stdoutFile, err := os.CreateTemp(t.TempDir(), "redirected-stdout-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	var stderr bytes.Buffer
	const distinctiveError = "provider token failure intended for injected stderr"
	capture := runLogin(t, runConfig{
		args: []string{
			"--auth-url", "https://human-output.example/authorize",
			"--token-url", "https://human-output.example/token",
			"--client-id", "human-output-client",
		},
		tokenStatus: http.StatusUnauthorized,
		tokenBody:   []byte(distinctiveError),
		stdout:      stdoutFile,
		stderr:      &stderr,
	})
	if capture.exitCode != 1 {
		t.Errorf("Run() exit code = %d, want 1; stderr = %q", capture.exitCode, stderr.String())
	}
	if err := stdoutFile.Close(); err != nil {
		t.Fatalf("closing redirected stdout: %v", err)
	}
	redirected, err := os.ReadFile(stdoutFile.Name())
	if err != nil {
		t.Fatalf("reading redirected stdout: %v", err)
	}

	for label, text := range map[string]string{
		"complete authorize URL": capture.authorizeURL,
		"runtime error":          distinctiveError,
	} {
		if !strings.Contains(stderr.String(), text) {
			t.Errorf("injected stderr = %q, want %s %q", stderr.String(), label, text)
		}
		if bytes.Contains(redirected, []byte(text)) {
			t.Errorf("redirected stdout = %q, must not contain %s %q", redirected, label, text)
		}
	}
	if len(redirected) != 0 {
		t.Errorf("redirected stdout length = %d, want exactly 0; contents = %q", len(redirected), redirected)
	}
}

// R-E5L7-OSOC
func TestRunReturnsInProcess(t *testing.T) {
	requireSuccessfulLogin(t, validArgs())
}

// R-ECWL-ZF4I
func TestRunBindsBeforeLaunchingBrowser(t *testing.T) {
	capture := requireSuccessfulLogin(t, validArgs())
	launchIndex := -1
	for index, call := range capture.callLog {
		if call == "launch" {
			launchIndex = index
		}
	}
	if launchIndex < 0 {
		t.Fatalf("call log = %v, want launch entry", capture.callLog)
	}
	for index, call := range capture.callLog {
		if strings.HasPrefix(call, "listen:") && index >= launchIndex {
			t.Errorf("call log = %v, bind %q at index %d is not before launch at index %d", capture.callLog, call, index, launchIndex)
		}
	}
}

// R-EFCE-QYLW
func TestRunAuthorizeURLUsesActuallyBoundPort(t *testing.T) {
	capture := requireSuccessfulLogin(t, append(validArgs(), "--port", "0"))
	redirectURI := authorizeRedirectURI(t, capture.authorizeURL)
	parsedRedirect, err := url.Parse(redirectURI)
	if err != nil {
		t.Fatalf("url.Parse(redirect_uri) error = %v", err)
	}
	_, portText, err := net.SplitHostPort(parsedRedirect.Host)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", parsedRedirect.Host, err)
	}
	if capture.boundPort == 0 {
		t.Fatal("independently observed bound port = 0, want nonzero OS-assigned port")
	}
	if portText != strconv.Itoa(capture.boundPort) {
		t.Errorf("redirect_uri port = %q, want independently observed port %d; URI = %q", portText, capture.boundPort, redirectURI)
	}
	if portText == "0" {
		t.Errorf("redirect_uri = %q, must never carry requested port 0", redirectURI)
	}
}

// R-EGKB-4QCL
func TestRunBracketsIPv6CallbackHostInRedirectURI(t *testing.T) {
	const callbackPath = "/oauth/return"
	capture := requireSuccessfulLogin(t, append(validArgs(), "--callback-host", "::1", "--callback-path", callbackPath))
	want := "http://[::1]:" + strconv.Itoa(capture.boundPort) + callbackPath
	if got := authorizeRedirectURI(t, capture.authorizeURL); got != want {
		t.Errorf("redirect_uri = %q, want %q", got, want)
	}
}

// R-EHS7-II3A
func TestRunReusesRedirectURIForTokenExchange(t *testing.T) {
	capture := requireSuccessfulLogin(t, append(validArgs(), "--port", "0"))
	parsedAuthorize, err := url.Parse(capture.authorizeURL)
	if err != nil {
		t.Fatalf("url.Parse(authorize URL) error = %v", err)
	}
	authorizeValues := parsedAuthorize.Query()["redirect_uri"]
	tokenValues := capture.tokenForm["redirect_uri"]
	if len(authorizeValues) != 1 {
		t.Fatalf("authorize redirect_uri values = %q, want exactly one", authorizeValues)
	}
	if len(tokenValues) != 1 {
		t.Fatalf("token redirect_uri values = %q, want exactly one", tokenValues)
	}
	want := "http://localhost:" + strconv.Itoa(capture.boundPort) + "/callback"
	if authorizeValues[0] != want {
		t.Errorf("authorize redirect_uri = %q, want independently composed %q", authorizeValues[0], want)
	}
	if tokenValues[0] != want {
		t.Errorf("token redirect_uri = %q, want independently composed %q", tokenValues[0], want)
	}
	if authorizeValues[0] != tokenValues[0] {
		t.Errorf("redirect_uri differs byte-for-byte: authorize = %q, token = %q", authorizeValues[0], tokenValues[0])
	}
	if capture.tokenCalls != 1 {
		t.Errorf("token request calls = %d, want exactly one", capture.tokenCalls)
	}
}

func authorizeRedirectURI(t *testing.T, authorizeURL string) string {
	t.Helper()
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("url.Parse(authorize URL) error = %v", err)
	}
	values := parsed.Query()["redirect_uri"]
	if len(values) != 1 {
		t.Fatalf("authorize redirect_uri values = %q, want exactly one", values)
	}

	return values[0]
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
			diagnostic := firstLine(stderr.String())
			for _, flag := range []string{"--client-secret", "--token-header"} {
				if !strings.Contains(diagnostic, flag) {
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
			requireSuccessfulLogin(t, args)
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
			if !strings.Contains(firstLine(stderr.String()), "--timeout") {
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
			if !strings.Contains(firstLine(stderr.String()), "--callback-path") {
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
			if !strings.Contains(firstLine(stderr.String()), test.missing) {
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
			if !strings.Contains(firstLine(stderr.String()), test.flag) {
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
			diagnostic := firstLine(stderr.String())
			if !strings.Contains(diagnostic, test.flag) {
				t.Errorf("stderr = %q, want offending flag %q", stderr.String(), test.flag)
			}
			if !strings.Contains(diagnostic, test.raw) {
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
			if !test.reserved {
				capture := requireSuccessfulLogin(t, args)
				parsed, err := url.Parse(capture.authorizeURL)
				if err != nil {
					t.Fatalf("url.Parse(authorize URL) error = %v", err)
				}
				if got := parsed.Query().Get(test.key); got != "value" {
					t.Errorf("authorize parameter %q = %q, want %q", test.key, got, "value")
				}

				return
			}

			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))
			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2 for reserved key %q", code, test.key)
			}
			diagnostic := firstLine(stderr.String())
			if !strings.Contains(diagnostic, "--auth-param") {
				t.Errorf("stderr = %q, want flag --auth-param", stderr.String())
			}
			if !strings.Contains(diagnostic, test.key) {
				t.Errorf("stderr = %q, want key %q", stderr.String(), test.key)
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
	diagnostic := firstLine(stderr.String())
	for _, flag := range []string{"--callback-host", "--port", "--callback-path"} {
		if !strings.Contains(diagnostic, flag) {
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
			if !test.reserved {
				capture := requireSuccessfulLogin(t, args)
				if got := capture.tokenForm.Get(test.key); got != "value" {
					t.Errorf("token parameter %q = %q, want %q", test.key, got, "value")
				}

				return
			}

			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), args, &stdout, &stderr, failDeps(t))
			if code != 2 {
				t.Errorf("Run() exit code = %d, want 2 for reserved key %q", code, test.key)
			}
			diagnostic := firstLine(stderr.String())
			if !strings.Contains(diagnostic, "--token-param") {
				t.Errorf("stderr = %q, want flag --token-param", stderr.String())
			}
			if !strings.Contains(diagnostic, test.key) {
				t.Errorf("stderr = %q, want key %q", stderr.String(), test.key)
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

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")

	return line
}

// R-REHR-NBIJ
func TestRunUsageErrorWritesCompleteUsageToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(
		context.Background(),
		[]string{"--phase-three-unknown"},
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
	if !strings.HasSuffix(stderr.String(), completeUsage) {
		t.Errorf("stderr = %q, want it to end with complete usage %q", stderr.String(), completeUsage)
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

// R-RD9V-9JRU
func TestRunHelpWritesExactUsageToStdout(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{flag}, &stdout, &stderr, failDeps(t))

			if code != 0 {
				t.Errorf("Run(%q) exit code = %d, want 0", flag, code)
			}
			if stdout.String() != completeUsage {
				t.Errorf("stdout = %q, want exact usage %q", stdout.String(), completeUsage)
			}
			if stderr.String() != "" {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
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
