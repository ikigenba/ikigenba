// Package fetch reads bodies from services on this space.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config describes the artifact service contract.
type Config struct {
	ServicesPath string
	Telemetry    *telemetry.Writer
	Limit        time.Duration
	After        func(time.Duration) <-chan time.Time
}

// Response describes the artifact service contract.
type Response struct {
	ContentType string
	Body        io.ReadCloser
}

var (
	// ErrNotOnSpace refuses targets outside the services file.
	ErrNotOnSpace = errors.New("URL is not on this space")
	// ErrNoAnswer identifies a sibling that supplied no headers.
	ErrNoAnswer = errors.New("sibling did not answer")
	// ErrTimedOut identifies the expired import bound.
	ErrTimedOut = errors.New("fetch timed out")
)

// StatusError describes the artifact service contract.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("sibling answered %d", e.Code) }

// Target implements the artifact service contract.
func Target(path, raw string) (services.Entry, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return services.Entry{}, ErrNotOnSpace
	}
	entries, err := services.Read(path)
	if err != nil {
		return services.Entry{}, ErrNotOnSpace
	}
	for _, e := range entries {
		v, err := url.Parse(e.URL)
		if err == nil && v.Host != "" && e.Socket != "" && v.Scheme == u.Scheme && v.Host == u.Host {
			return e, nil
		}
	}
	return services.Entry{}, ErrNotOnSpace
}

type bounded struct {
	mu           sync.Mutex
	body         io.ReadCloser
	timer        <-chan time.Time
	cancel       context.CancelFunc
	done         chan struct{}
	ended, timed bool
}

func (b *bounded) timeout() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.ended && !b.timed {
		select {
		case <-b.timer:
			b.timed = true
			b.cancel()
		default:
		}
	}
	return b.timed
}
func (b *bounded) finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.ended {
		b.ended = true
		close(b.done)
		b.cancel()
	}
}
func (b *bounded) Read(p []byte) (int, error) {
	if b.timeout() {
		return 0, ErrTimedOut
	}
	n, err := b.body.Read(p)
	if b.timeout() {
		return n, ErrTimedOut
	}
	if err == io.EOF {
		b.finish()
	}
	return n, err
}
func (b *bounded) Close() error { b.finish(); return b.body.Close() }

// Get implements the artifact service contract.
func Get(ctx context.Context, cfg Config, raw string) (*Response, error) {
	e, err := Target(cfg.ServicesPath, raw)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(raw)
	u.Scheme = "http"
	u.Host = e.Name
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	ctx, cancel := context.WithCancel(ctx)
	after := cfg.After
	if after == nil {
		after = time.After
	}
	b := &bounded{timer: after(cfg.Limit), cancel: cancel, done: make(chan struct{})}
	go func() {
		select {
		case <-b.timer:
			b.mu.Lock()
			if !b.ended {
				b.timed = true
				cancel()
			}
			b.mu.Unlock()
		case <-b.done:
		}
	}()
	transport := telemetry.SocketTransport(e.Socket)
	transport.DisableCompression = true
	client := telemetry.SiblingClient(cfg.Telemetry, e.Name, transport)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		b.finish()
		return nil, ErrNoAnswer
	}
	res, err := client.Do(req)
	if err != nil {
		timed := b.timeout()
		b.finish()
		transport.CloseIdleConnections()
		if timed {
			return nil, ErrTimedOut
		}
		return nil, ErrNoAnswer
	}
	if b.timeout() {
		_ = res.Body.Close()
		b.finish()
		return nil, ErrTimedOut
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		b.finish()
		return nil, &StatusError{Code: res.StatusCode}
	}
	b.body = res.Body
	return &Response{ContentType: res.Header.Get("Content-Type"), Body: b}, nil
}
