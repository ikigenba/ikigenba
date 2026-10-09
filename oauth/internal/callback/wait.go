package callback

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/oauth"
)

// Result is the successful information carried by a callback.
type Result struct{ Code string }

var (
	// ErrStateMismatch reports an absent, empty, or unequal callback state.
	ErrStateMismatch = errors.New("callback state did not match")
	// ErrNoCode reports a callback carrying neither a code nor provider error.
	ErrNoCode = errors.New("callback carried neither code nor error")
)

// AuthorizeError reports a provider-sent error=/error_description= redirect.
type AuthorizeError struct{ Code, Description string }

func (e *AuthorizeError) Error() string {
	return fmt.Sprintf("authorization failed: code %q, description %q", e.Code, e.Description)
}

type waitResult struct {
	result Result
	err    error
}

// Wait serves the bound listeners until one callback completes the flow or ctx
// expires. Precondition: called at most once.
func (s *Server) Wait(ctx context.Context, path, state string) (Result, error) {
	completed := make(chan waitResult, 1)
	httpServers, serving := s.startHTTPServers(callbackHandler(path, state, completed))
	var outcome waitResult
	select {
	case outcome = <-completed:
	case <-ctx.Done():
		outcome.err = ctx.Err()
	}
	shutdownHTTPServers(httpServers)
	serving.Wait()

	return outcome.result, outcome.err
}

func callbackHandler(path, state string, completed chan<- waitResult) http.Handler {
	var completeOnce sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			http.NotFound(w, request)
			return
		}

		query := request.URL.Query()
		if query.Get("state") != state || query.Get("state") == "" {
			completeCallback(w, http.StatusBadRequest, "failure", failureData{}, &completeOnce, completed, waitResult{err: ErrStateMismatch})
			return
		}

		if errorCode := query.Get("error"); errorCode != "" {
			description := query.Get("error_description")
			completeCallback(w, http.StatusBadRequest, "failure", failureData{Description: description}, &completeOnce, completed, waitResult{err: &AuthorizeError{
				Code:        errorCode,
				Description: description,
			}})
			return
		}

		code := query.Get("code")
		if code == "" {
			completeCallback(w, http.StatusBadRequest, "failure", failureData{}, &completeOnce, completed, waitResult{err: ErrNoCode})
			return
		}

		completeCallback(w, http.StatusOK, "success", nil, &completeOnce, completed, waitResult{result: Result{Code: code}})
	})
}

type failureData struct {
	Description string
}

var pages = template.Must(template.ParseFS(oauth.Assets(), "*.html"))

func completeCallback(
	w http.ResponseWriter,
	status int,
	page string,
	pageData any,
	completeOnce *sync.Once,
	completed chan<- waitResult,
	outcome waitResult,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pages.ExecuteTemplate(w, page, pageData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	completeOnce.Do(func() {
		completed <- outcome
	})
}

func (s *Server) startHTTPServers(handler http.Handler) ([]*http.Server, *sync.WaitGroup) {
	listeners := []net.Listener{s.ipv4}
	if s.ipv6 != nil {
		listeners = append(listeners, s.ipv6)
	}

	httpServers := make([]*http.Server, 0, len(listeners))
	var serving sync.WaitGroup
	for _, listener := range listeners {
		httpServer := &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		}
		httpServers = append(httpServers, httpServer)
		serving.Add(1)
		go func() {
			defer serving.Done()
			_ = httpServer.Serve(listener)
		}()
	}

	return httpServers, &serving
}

func shutdownHTTPServers(httpServers []*http.Server) {
	for _, httpServer := range httpServers {
		_ = httpServer.Shutdown(context.Background())
	}
}
