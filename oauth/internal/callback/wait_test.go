package callback_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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

// R-GIPW-P83Y
func TestWaitRejectsInvalidState(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
	}{
		{name: "absent", query: url.Values{"code": {"code-without-state"}}},
		{name: "empty", query: url.Values{"state": {""}, "code": {"code-with-empty-state"}}},
		{name: "unequal", query: url.Values{"state": {"other-state"}, "code": {"code-with-other-state"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := listenForWait(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcomes := startWait(ctx, server, "/callback", "expected-state")

			response := getCallback(ctx, t, server.Port(), "/callback", test.query)
			if response.StatusCode != http.StatusBadRequest {
				t.Errorf("invalid-state callback status = %d, want %d", response.StatusCode, http.StatusBadRequest)
			}
			closeResponse(t, response)

			outcome := requireWaitOutcome(ctx, t, outcomes)
			if !errors.Is(outcome.err, callback.ErrStateMismatch) {
				t.Errorf("Wait() error = %v, want errors.Is(..., ErrStateMismatch)", outcome.err)
			}
		})
	}
}

// R-GJXT-2ZUN
func TestWaitReturnsProviderAuthorizeError(t *testing.T) {
	server := listenForWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes := startWait(ctx, server, "/authorize-return", "expected-state")

	response := getCallback(ctx, t, server.Port(), "/authorize-return", url.Values{
		"state":             {"expected-state"},
		"error":             {"access_denied_distinguishing_code"},
		"error_description": {"provider description with distinguishing text"},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("provider-error callback status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	closeResponse(t, response)

	outcome := requireWaitOutcome(ctx, t, outcomes)
	var authorizeErr *callback.AuthorizeError
	if !errors.As(outcome.err, &authorizeErr) {
		t.Fatalf("Wait() error = %v, want *callback.AuthorizeError", outcome.err)
	}
	if authorizeErr.Code != "access_denied_distinguishing_code" {
		t.Errorf("AuthorizeError.Code = %q, want exact provider error", authorizeErr.Code)
	}
	if authorizeErr.Description != "provider description with distinguishing text" {
		t.Errorf("AuthorizeError.Description = %q, want exact provider description", authorizeErr.Description)
	}
}

// R-GL5P-GRLC
func TestWaitRejectsCallbackWithoutCodeOrError(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
	}{
		{name: "absent", query: url.Values{"state": {"expected-state"}}},
		{name: "empty", query: url.Values{"state": {"expected-state"}, "code": {""}, "error": {""}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := listenForWait(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcomes := startWait(ctx, server, "/callback", "expected-state")

			response := getCallback(ctx, t, server.Port(), "/callback", test.query)
			if response.StatusCode != http.StatusBadRequest {
				t.Errorf("code-less callback status = %d, want %d", response.StatusCode, http.StatusBadRequest)
			}
			closeResponse(t, response)

			outcome := requireWaitOutcome(ctx, t, outcomes)
			if !errors.Is(outcome.err, callback.ErrNoCode) {
				t.Errorf("Wait() error = %v, want errors.Is(..., ErrNoCode)", outcome.err)
			}
		})
	}
}

// R-GMDL-UJC1
func TestWaitChecksStateBeforeProviderError(t *testing.T) {
	server := listenForWait(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes := startWait(ctx, server, "/callback", "expected-state")

	response := getCallback(ctx, t, server.Port(), "/callback", url.Values{
		"state":             {"attacker-state"},
		"error":             {"attacker-chosen-error"},
		"error_description": {"attacker-chosen-description"},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("mismatched-state provider-error callback status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	closeResponse(t, response)

	outcome := requireWaitOutcome(ctx, t, outcomes)
	if !errors.Is(outcome.err, callback.ErrStateMismatch) {
		t.Errorf("Wait() error = %v, want errors.Is(..., ErrStateMismatch)", outcome.err)
	}
	var authorizeErr *callback.AuthorizeError
	if errors.As(outcome.err, &authorizeErr) {
		t.Errorf("Wait() error = %v, unexpectedly unwraps to *callback.AuthorizeError %+v", outcome.err, authorizeErr)
	}
}

// R-GNLI-8B2Q
func TestWaitServesTypedHTMLPagesWithEscapedProviderDescription(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := listenForWait(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		outcomes := startWait(ctx, server, "/callback", "expected-state")

		page := getCallbackPage(ctx, t, server.Port(), "/callback", url.Values{
			"state": {"expected-state"},
			"code":  {"successful-code"},
		})
		if page.statusCode != http.StatusOK {
			t.Errorf("success status = %d, want %d", page.statusCode, http.StatusOK)
		}
		if page.contentType != "text/html; charset=utf-8" {
			t.Errorf("success Content-Type = %q, want exact HTML content type", page.contentType)
		}
		if !strings.Contains(page.body, "Login complete") {
			t.Errorf("success body = %q, want Login complete", page.body)
		}

		outcome := requireWaitOutcome(ctx, t, outcomes)
		if outcome.err != nil || outcome.result.Code != "successful-code" {
			t.Errorf("Wait() = (%+v, %v), want successful-code and nil error", outcome.result, outcome.err)
		}
	})

	t.Run("provider failure", func(t *testing.T) {
		server := listenForWait(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		outcomes := startWait(ctx, server, "/callback", "expected-state")
		providerDescription := `denied <img src=x onerror="alert('unsafe')"> & "quoted"`
		escapedDescription := `denied &lt;img src=x onerror=&#34;alert(&#39;unsafe&#39;)&#34;&gt; &amp; &#34;quoted&#34;`

		page := getCallbackPage(ctx, t, server.Port(), "/callback", url.Values{
			"state":             {"expected-state"},
			"error":             {"access_denied"},
			"error_description": {providerDescription},
		})
		if page.statusCode != http.StatusBadRequest {
			t.Errorf("failure status = %d, want %d", page.statusCode, http.StatusBadRequest)
		}
		if page.contentType != "text/html; charset=utf-8" {
			t.Errorf("failure Content-Type = %q, want exact HTML content type", page.contentType)
		}
		if !strings.Contains(page.body, escapedDescription) {
			t.Errorf("failure body = %q, want escaped provider description %q", page.body, escapedDescription)
		}
		if strings.Contains(page.body, providerDescription) || strings.Contains(strings.ToLower(page.body), "<img") {
			t.Errorf("failure body contains raw provider markup: %q", page.body)
		}

		outcome := requireWaitOutcome(ctx, t, outcomes)
		var authorizeErr *callback.AuthorizeError
		if !errors.As(outcome.err, &authorizeErr) {
			t.Fatalf("Wait() error = %v, want *callback.AuthorizeError", outcome.err)
		}
		if authorizeErr.Code != "access_denied" || authorizeErr.Description != providerDescription {
			t.Errorf("AuthorizeError = %+v, want exact provider fields", authorizeErr)
		}
	})
}

// R-GOTE-M2TF
func TestWaitPagesContainNoExternalResourceReferences(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
	}{
		{name: "success", query: url.Values{"state": {"expected-state"}, "code": {"successful-code"}}},
		{name: "failure", query: url.Values{"state": {"expected-state"}, "error": {"access_denied"}, "error_description": {"provider refused login"}}},
	}
	resourceReference := regexp.MustCompile(`(?i)(src\s*=|href\s*=|srcset\s*=|url\s*\(|@import|<\s*(link|script|img|iframe|object|embed|audio|video|source)(\s|>))`)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := listenForWait(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcomes := startWait(ctx, server, "/callback", "expected-state")

			page := getCallbackPage(ctx, t, server.Port(), "/callback", test.query)
			if match := resourceReference.FindString(page.body); match != "" {
				t.Errorf("callback page contains resource-bearing construct %q: %q", match, page.body)
			}
			requireWaitOutcome(ctx, t, outcomes)
		})
	}
}

// R-GR97-DMAT
func TestWaitReturnsContextErrorAndShutsDownListener(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		server := listenForWait(t)
		waitCtx, cancelWait := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancelWait()
		outcomes := startWait(waitCtx, server, "/callback", "expected-state")
		outerCtx, cancelOuter := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelOuter()

		outcome := requireWaitOutcome(outerCtx, t, outcomes)
		if !errors.Is(outcome.err, context.DeadlineExceeded) {
			t.Errorf("Wait() error = %v, want context.DeadlineExceeded", outcome.err)
		}
		if outcome.result != (callback.Result{}) {
			t.Errorf("Wait() result = %+v, want zero result", outcome.result)
		}
		requireListenerClosed(t, server.Port())
	})

	t.Run("canceled", func(t *testing.T) {
		server := listenForWait(t)
		waitCtx, cancelWait := context.WithCancel(context.Background())
		outcomes := startWait(waitCtx, server, "/callback", "expected-state")
		outerCtx, cancelOuter := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelOuter()

		strayPage := getCallbackPage(outerCtx, t, server.Port(), "/wait-started", nil)
		if strayPage.statusCode != http.StatusNotFound {
			t.Fatalf("startup probe status = %d, want %d", strayPage.statusCode, http.StatusNotFound)
		}
		cancelWait()
		outcome := requireWaitOutcome(outerCtx, t, outcomes)
		if !errors.Is(outcome.err, context.Canceled) {
			t.Errorf("Wait() error = %v, want context.Canceled", outcome.err)
		}
		if errors.Is(outcome.err, context.DeadlineExceeded) {
			t.Errorf("Wait() error = %v, unexpectedly satisfies context.DeadlineExceeded", outcome.err)
		}
		if outcome.result != (callback.Result{}) {
			t.Errorf("Wait() result = %+v, want zero result", outcome.result)
		}
		requireListenerClosed(t, server.Port())
	})
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

type callbackPage struct {
	statusCode  int
	contentType string
	body        string
}

func getCallbackPage(ctx context.Context, t *testing.T, port int, path string, query url.Values) callbackPage {
	t.Helper()
	response := getCallback(ctx, t, port, path, query)
	defer closeResponse(t, response)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Errorf("response body ReadAll() error = %v", err)
	}
	return callbackPage{
		statusCode:  response.StatusCode,
		contentType: response.Header.Get("Content-Type"),
		body:        string(body),
	}
}

func requireListenerClosed(t *testing.T, port int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = connection.Close()
		t.Errorf("listener on port %d still accepts connections after Wait() returned", port)
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
