package callback_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/oauth/internal/callback"
)

type waitOutcome struct {
	result callback.Result
	err    error
}

// R-GGA3-XOMK
func TestWaitReturnsCodeFromValidCallback(t *testing.T) {
	server := listenForWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes := startWait(ctx, server, "/callback", "state with spaces")

	response := getCallback(ctx, t, server.Port(), "/callback", url.Values{
		"state": {"state with spaces"},
		"code":  {"code/with+a-distinguishing-value"},
	})
	if response.StatusCode != http.StatusOK {
		t.Errorf("valid callback status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	closeResponse(t, response)

	outcome := requireWaitOutcome(ctx, t, outcomes)
	if outcome.err != nil {
		t.Fatalf("Wait() error = %v, want nil", outcome.err)
	}
	if outcome.result.Code != "code/with+a-distinguishing-value" {
		t.Errorf("Wait() result code = %q, want exact callback code", outcome.result.Code)
	}
}

// R-GHI0-BGD9
func TestWaitIgnoresStrayPathBeforeValidCallback(t *testing.T) {
	server := listenForWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes := startWait(ctx, server, "/callback", "expected-state")

	strayResponse := getCallback(ctx, t, server.Port(), "/favicon.ico", nil)
	if strayResponse.StatusCode != http.StatusNotFound {
		t.Errorf("stray request status = %d, want %d", strayResponse.StatusCode, http.StatusNotFound)
	}
	closeResponse(t, strayResponse)
	select {
	case outcome := <-outcomes:
		t.Fatalf("stray request ended Wait() with result %+v and error %v", outcome.result, outcome.err)
	default:
	}

	validResponse := getCallback(ctx, t, server.Port(), "/callback", url.Values{
		"state": {"expected-state"},
		"code":  {"code-after-stray-request"},
	})
	if validResponse.StatusCode != http.StatusOK {
		t.Errorf("valid callback status = %d, want %d", validResponse.StatusCode, http.StatusOK)
	}
	closeResponse(t, validResponse)

	outcome := requireWaitOutcome(ctx, t, outcomes)
	if outcome.err != nil {
		t.Fatalf("Wait() error = %v, want nil", outcome.err)
	}
	if outcome.result.Code != "code-after-stray-request" {
		t.Errorf("Wait() result code = %q, want exact subsequent callback code", outcome.result.Code)
	}
}

func listenForWait(t *testing.T) *callback.Server {
	t.Helper()
	server, err := callback.Listen(net.Listen, 0)
	if err != nil {
		t.Fatalf("Listen(..., 0) error = %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("server.Close() error = %v", err)
		}
	})
	return server
}

func startWait(ctx context.Context, server *callback.Server, path, state string) <-chan waitOutcome {
	outcomes := make(chan waitOutcome, 1)
	go func() {
		result, err := server.Wait(ctx, path, state)
		outcomes <- waitOutcome{result: result, err: err}
	}()
	return outcomes
}

func getCallback(ctx context.Context, t *testing.T, port int, path string, query url.Values) *http.Response {
	t.Helper()
	requestURL := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	if len(query) != 0 {
		requestURL += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s error = %v", requestURL, err)
	}
	return response
}

func closeResponse(t *testing.T, response *http.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Errorf("response body Close() error = %v", err)
	}
}

func requireWaitOutcome(ctx context.Context, t *testing.T, outcomes <-chan waitOutcome) waitOutcome {
	t.Helper()
	select {
	case outcome := <-outcomes:
		return outcome
	case <-ctx.Done():
		t.Fatalf("Wait() did not return before test context ended: %v", ctx.Err())
		return waitOutcome{}
	}
}
