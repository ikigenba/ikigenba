package callback

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

// Result is the successful information carried by a callback.
type Result struct{ Code string }

// Wait serves the bound listeners until one callback completes the flow or ctx
// expires. Precondition: called at most once.
func (s *Server) Wait(ctx context.Context, path, state string) (Result, error) {
	_ = ctx

	completed := make(chan Result, 1)
	httpServers, serving := s.startHTTPServers(callbackHandler(path, state, completed))
	result := <-completed
	shutdownHTTPServers(httpServers)
	serving.Wait()

	return result, nil
}

func callbackHandler(path, state string, completed chan<- Result) http.Handler {
	var completeOnce sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			http.NotFound(w, request)
			return
		}

		query := request.URL.Query()
		code := query.Get("code")
		if query.Get("state") != state || code == "" {
			return
		}

		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		completeOnce.Do(func() {
			completed <- Result{Code: code}
		})
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
