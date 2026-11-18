// Package daemonapi serves the host daemon's binary, desired state, and report intake.
package daemonapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Poll and download limits, and the protocol's plain-text responses.
const (
	MaxWaiters = 1000
	CopyBuffer = 32 << 10

	NotFound    = "Daemon resource not found."
	NotAllowed  = "Method not allowed."
	BadRequest  = "Malformed daemon request."
	Refused     = "Daemon identity refused."
	UnknownHost = "No live host matches this daemon."
	TooLarge    = "Daemon report exceeds its size limit."
	Busy        = "Daemon service temporarily unavailable."
)

// Config supplies the handler's dependencies and its deployment settings.
type Config struct {
	Store      *store.Store
	Verifier   *hostauth.Verifier
	Binary     string
	Telemetry  *telemetry.Writer
	Now        func() time.Time
	Configured bool
	After      func(time.Duration) <-chan time.Time
}

// Server handles the daemon endpoints for one accounts process.
type Server struct {
	cfg    Config
	binary *os.File
	size   int64
	digest string

	stopOnce sync.Once
	stopped  chan struct{}
	waiters  chan struct{}
}

// New opens and digests the daemon binary when it is available.
func New(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	s := &Server{cfg: cfg, stopped: make(chan struct{}), waiters: make(chan struct{}, MaxWaiters)}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Check before opening so a named pipe cannot block startup in Open.
	info, err := os.Stat(cfg.Binary)
	if err != nil || !info.Mode().IsRegular() {
		return s, nil
	}
	file, err := os.Open(cfg.Binary)
	if err != nil {
		return s, nil
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return s, nil
	}
	input := &contextReader{ctx: ctx, r: file}
	digest, err := protocol.Digest(input)
	if ctx.Err() != nil {
		_ = file.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		_ = file.Close()
		return s, nil
	}
	s.binary = file
	s.size = input.read
	s.digest = digest
	return s, nil
}

type contextReader struct {
	ctx  context.Context
	r    io.Reader
	read int64
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	r.read += int64(n)
	return n, err
}

// Digest returns the digest of the binary this process serves.
func (s *Server) Digest() string { return s.digest }

// Stop wakes all current waiters and closes admission for subsequent waiters.
func (s *Server) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Configured {
		http.Error(w, NotFound, http.StatusNotFound)
		return
	}
	switch r.URL.Path {
	case protocol.BinaryPath:
		s.download(w, r)
	case protocol.StatePath:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		after, digest, err := parsePoll(r.URL.RawQuery)
		if err != nil {
			http.Error(w, BadRequest, http.StatusBadRequest)
			return
		}
		host, ok := s.identify(w, r)
		if ok {
			s.poll(w, r, host, after, digest)
		}
	case protocol.ReportPath:
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		host, ok := s.identify(w, r)
		if ok {
			s.report(w, r, host.Name)
		}
	default:
		http.Error(w, NotFound, http.StatusNotFound)
	}
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, NotAllowed, http.StatusMethodNotAllowed)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	if s.binary == nil {
		http.Error(w, NotFound, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", protocol.ContentBinary)
	w.Header().Set("Content-Length", strconv.FormatInt(s.size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		input := io.NewSectionReader(s.binary, 0, s.size)
		// Neither endpoint can bypass the fixed buffer via ReaderFrom or WriterTo.
		_, _ = io.CopyBuffer(struct{ io.Writer }{w}, input, make([]byte, CopyBuffer))
	}
}

func parsePoll(raw string) (after int64, digest string, err error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return 0, "", err
	}
	if values, ok := query[protocol.QueryAfter]; ok {
		if len(values) != 1 || values[0] == "" || strings.ContainsFunc(values[0], func(r rune) bool {
			return r < '0' || r > '9'
		}) {
			return 0, "", errors.New("invalid after value")
		}
		after, err = strconv.ParseInt(values[0], 10, 64)
		if err != nil {
			return 0, "", err
		}
	}
	return after, query.Get(protocol.QueryDigest), nil
}

func (s *Server) identify(w http.ResponseWriter, r *http.Request) (store.Host, bool) {
	ctx := r.Context()
	id, err := s.cfg.Verifier.Verify(ctx, r.Header.Get(hostauth.HeaderName))
	if err != nil {
		switch {
		case errors.Is(err, hostauth.ErrMalformed), errors.Is(err, hostauth.ErrStale),
			errors.Is(err, hostauth.ErrReplayed), errors.Is(err, hostauth.ErrRejected):
			w.Header().Set("WWW-Authenticate", "Accountsd-Identity")
			http.Error(w, Refused, http.StatusUnauthorized)
		default:
			http.Error(w, Busy, http.StatusServiceUnavailable)
		}
		return store.Host{}, false
	}
	host, err := s.cfg.Store.HostByInstance(ctx, id)
	if err != nil {
		storeFailure(w, err)
		return store.Host{}, false
	}
	return host, true
}

func storeFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, UnknownHost, http.StatusForbidden)
		return
	}
	http.Error(w, store.Unreachable, http.StatusServiceUnavailable)
}

func (s *Server) poll(w http.ResponseWriter, r *http.Request, host store.Host, after int64, digest string) {
	ctx := r.Context()
	if host.Version != after || (s.digest != "" && s.digest != digest) {
		s.state(ctx, w, host.Name)
		return
	}

	changed, release := s.cfg.Store.Watch(host.Name)
	defer release()
	// Recheck after registering the watch to catch commits since HostByInstance.
	current, err := s.cfg.Store.Host(ctx, host.Name)
	if err != nil {
		storeFailure(w, err)
		return
	}
	if current.Version == after && !s.wait(ctx, w, changed) {
		return
	}
	s.state(ctx, w, host.Name)
}

func (s *Server) wait(ctx context.Context, w http.ResponseWriter, changed <-chan struct{}) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-changed:
		return true
	default:
	}
	select {
	case <-s.stopped:
		http.Error(w, Busy, http.StatusServiceUnavailable)
		return false
	default:
	}
	select {
	case s.waiters <- struct{}{}:
		defer func() { <-s.waiters }()
	default:
		http.Error(w, Busy, http.StatusServiceUnavailable)
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-changed:
	case <-s.cfg.After(protocol.LongPoll):
	case <-s.stopped:
	}
	return ctx.Err() == nil
}

func (s *Server) state(ctx context.Context, w http.ResponseWriter, name string) {
	desired, err := s.cfg.Store.Desired(ctx, name)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		storeFailure(w, err)
		return
	}
	desired.Daemon = s.digest
	w.Header().Set("Content-Type", protocol.ContentJSON)
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(desired)
}

func (s *Server) report(w http.ResponseWriter, r *http.Request, name string) {
	body := http.MaxBytesReader(w, r.Body, protocol.MaxReportBytes)
	report, err := protocol.DecodeReport(body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.Is(err, protocol.ErrTooLarge) || errors.As(err, &limit) {
			http.Error(w, TooLarge, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, BadRequest, http.StatusBadRequest)
		return
	}
	change, err := s.cfg.Store.RecordReport(r.Context(), name, report, s.cfg.Now())
	if err != nil {
		storeFailure(w, err)
		return
	}
	// Daemon state changes carry no user or request identity.
	if change.Synced {
		trail.HostSynced(context.Background(), s.cfg.Telemetry, name, change.Applied)
	}
	if change.ApplyFailed {
		trail.HostApplyFailed(context.Background(), s.cfg.Telemetry, name, change.Attempted)
	}
	w.WriteHeader(http.StatusNoContent)
}
