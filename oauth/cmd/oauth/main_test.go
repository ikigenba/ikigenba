package main_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const binaryTestTimeout = 10 * time.Second

type observedAuthorize struct {
	state         string
	codeChallenge string
}

type testProvider struct {
	server    *httptest.Server
	tokenBody []byte

	mu       sync.Mutex
	observed []observedAuthorize
}

func newTestProvider(t *testing.T, tokenBody []byte) *testProvider {
	t.Helper()

	provider := &testProvider{tokenBody: tokenBody}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.server.Close)

	return provider
}

func (provider *testProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/authorize":
		query := request.URL.Query()
		provider.mu.Lock()
		provider.observed = append(provider.observed, observedAuthorize{
			state:         query.Get("state"),
			codeChallenge: query.Get("code_challenge"),
		})
		provider.mu.Unlock()

		redirect, err := url.Parse(query.Get("redirect_uri"))
		if err != nil || redirect.Scheme == "" || redirect.Host == "" {
			http.Error(response, "invalid redirect_uri", http.StatusBadRequest)
			return
		}
		redirectQuery := redirect.Query()
		redirectQuery.Set("code", "known-authorization-code")
		redirectQuery.Set("state", query.Get("state"))
		redirect.RawQuery = redirectQuery.Encode()
		// #nosec G710 -- redirect_uri is intentionally supplied by the binary under test.
		http.Redirect(response, request, redirect.String(), http.StatusFound)
	case "/token":
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(provider.tokenBody)
	default:
		http.NotFound(response, request)
	}
}

func (provider *testProvider) authorizationEndpoint() string {
	return provider.server.URL + "/authorize"
}

func (provider *testProvider) tokenURL() string {
	return provider.server.URL + "/token"
}

func (provider *testProvider) observations() []observedAuthorize {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return append([]observedAuthorize(nil), provider.observed...)
}

func buildBinary(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "oauth")
	ctx, cancel := context.WithTimeout(context.Background(), binaryTestTimeout)
	defer cancel()
	// #nosec G204 -- all arguments are fixed except the test-owned temporary output path.
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/oauth")
	command.Dir = filepath.Join("..", "..")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build ./cmd/oauth: %v\n%s", err, output)
	}

	return binary
}

type binaryResult struct {
	stdout              []byte
	stderr              []byte
	printedAuthorizeURL string
	err                 error
}

type binaryProcess struct {
	command  *exec.Cmd
	stdout   bytes.Buffer
	stderr   bytes.Buffer
	urlReady chan string
	scanDone chan struct{}
	scanErr  error
	waited   bool
}

func startBinaryProcess(ctx context.Context, binary string, provider *testProvider) (*binaryProcess, error) {
	// #nosec G204 -- binary and endpoint arguments are created by this test harness.
	command := exec.CommandContext(ctx, binary,
		"--no-browser",
		"--auth-url", provider.authorizationEndpoint(),
		"--token-url", provider.tokenURL(),
		"--client-id", "binary-test-client",
		"--callback-host", "127.0.0.1",
		"--timeout", "5s",
	)

	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}
	process := &binaryProcess{
		command:  command,
		urlReady: make(chan string, 1),
		scanDone: make(chan struct{}),
	}
	command.Stdout = &process.stdout
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start binary: %w", err)
	}
	go process.scanStderr(stderrPipe)

	return process, nil
}

func (process *binaryProcess) scanStderr(stderrPipe io.Reader) {
	defer close(process.scanDone)

	scanner := bufio.NewScanner(stderrPipe)
	for scanner.Scan() {
		line := scanner.Text()
		process.stderr.WriteString(line)
		process.stderr.WriteByte('\n')
		parsed, err := url.Parse(line)
		if err == nil && parsed.Scheme == "http" && parsed.Host != "" {
			select {
			case process.urlReady <- line:
			default:
			}
		}
	}
	process.scanErr = scanner.Err()
}

func (process *binaryProcess) waitForPrintedURL(ctx context.Context) (string, error) {
	select {
	case printedURL := <-process.urlReady:
		return printedURL, nil
	case <-process.scanDone:
		return "", errors.New("binary exited before printing authorize URL")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func driveAuthorizeRedirect(ctx context.Context, printedURL string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, printedURL, nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("drive authorize redirect: %w", err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()

	return errors.Join(readErr, closeErr)
}

func (process *binaryProcess) finish(kill bool) error {
	if process.waited {
		return process.scanErr
	}
	if kill {
		_ = process.command.Process.Kill()
	}
	waitErr := process.command.Wait()
	process.waited = true
	<-process.scanDone

	return errors.Join(waitErr, process.scanErr)
}

func (process *binaryProcess) result(printedURL string, err error) binaryResult {
	return binaryResult{
		stdout:              process.stdout.Bytes(),
		stderr:              process.stderr.Bytes(),
		printedAuthorizeURL: printedURL,
		err:                 err,
	}
}

func runBinaryLogin(t *testing.T, binary string, provider *testProvider) binaryResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), binaryTestTimeout)
	defer cancel()
	process, err := startBinaryProcess(ctx, binary, provider)
	if err != nil {
		return binaryResult{err: err}
	}
	defer func() { _ = process.finish(true) }()

	printedURL, err := process.waitForPrintedURL(ctx)
	if err != nil {
		return process.result(printedURL, errors.Join(err, process.finish(true)))
	}
	if err := driveAuthorizeRedirect(ctx, printedURL); err != nil {
		return process.result(printedURL, errors.Join(err, process.finish(true)))
	}

	return process.result(printedURL, process.finish(false))
}

// R-EAGT-7VN4: the real binary completes a loopback login and preserves token bytes.
func TestBinaryCompletesLoopbackLogin(t *testing.T) {
	tokenBody := []byte("distinctive-token-response\x00with trailing newline\n")
	provider := newTestProvider(t, tokenBody)
	result := runBinaryLogin(t, buildBinary(t), provider)

	if result.err != nil {
		t.Fatalf("binary login did not exit successfully: %v\nauthorize URL: %q\nstdout: %q\nstderr: %q", result.err, result.printedAuthorizeURL, result.stdout, result.stderr)
	}
	if !bytes.Equal(result.stdout, tokenBody) {
		t.Errorf("stdout = %q, want exact token bytes %q", result.stdout, tokenBody)
	}
}

// R-EBOP-LNDT: separate real-binary logins use independently fresh entropy.
func TestBinaryUsesFreshEntropyAcrossRuns(t *testing.T) {
	provider := newTestProvider(t, []byte("ok"))
	binary := buildBinary(t)
	for run := 1; run <= 2; run++ {
		result := runBinaryLogin(t, binary, provider)
		if result.err != nil {
			t.Fatalf("binary login %d did not exit successfully: %v\nauthorize URL: %q\nstdout: %q\nstderr: %q", run, result.err, result.printedAuthorizeURL, result.stdout, result.stderr)
		}
	}

	observed := provider.observations()
	if len(observed) != 2 {
		t.Fatalf("authorize observations = %d, want exactly 2", len(observed))
	}
	for index, observation := range observed {
		if strings.TrimSpace(observation.state) == "" {
			t.Errorf("run %d state is empty", index+1)
		}
		if strings.TrimSpace(observation.codeChallenge) == "" {
			t.Errorf("run %d code_challenge is empty", index+1)
		}
	}
	if observed[0].state == observed[1].state {
		t.Errorf("successive state values are equal: %q", observed[0].state)
	}
	if observed[0].codeChallenge == observed[1].codeChallenge {
		t.Errorf("successive code_challenge values are equal: %q", observed[0].codeChallenge)
	}
}
