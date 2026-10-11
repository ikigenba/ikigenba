// Package files serves artifact bytes and their metadata over HTTP.
package files

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

// ExpiresHeader names the expiry header; the other constants are refusal copy.
const (
	ExpiresHeader  = "X-Artifact-Expires"
	InvalidPath    = "Invalid artifact path."
	InvalidExpires = "Invalid artifact expiry."
	PastExpiry     = "Artifact expiry must be in the future."
	NotFound       = "Artifact not found."
	Forbidden      = "Only the owner may change this artifact."
	TooLarge       = "Artifact exceeds the byte limit."
	BodyFailed     = "Artifact body could not be read."
)

// Metadata is the public description of an artifact.
type Metadata struct {
	ID          string  `json:"id"`
	Path        string  `json:"path"`
	Bytes       int64   `json:"bytes"`
	ContentType string  `json:"content_type"`
	SHA256      string  `json:"sha256"`
	Owner       string  `json:"owner"`
	Created     string  `json:"created"`
	Modified    string  `json:"modified"`
	Expires     *string `json:"expires,omitempty"`
	URL         string  `json:"url"`
}

// Config supplies the store, byte bound, public addresses and event writers.
type Config struct {
	Store        *store.Store
	Max          int64
	ServicesPath string
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Describe forms the public metadata of a stored artifact.
func Describe(a store.Artifact, base string) Metadata {
	m := Metadata{ID: a.ID, Path: a.Path, Bytes: a.Bytes, ContentType: a.ContentType, SHA256: a.SHA256, Owner: a.OwnerEmail, Created: a.Created.UTC().Format(time.RFC3339), Modified: a.Modified.UTC().Format(time.RFC3339), URL: urls.File(base, a.Path)}
	if !a.Expires.IsZero() {
		exp := a.Expires.UTC().Format(time.RFC3339)
		m.Expires = &exp
	}
	return m
}

func refusal(w http.ResponseWriter, r *http.Request, status int, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(line + "\n"))
	}
}

// uploadBody keeps the store's reads and a refusal's drain under one bound.
type uploadBody struct {
	reader    io.Reader
	remaining int64
	ended     bool
	tooLarge  bool
}

func (b *uploadBody) Read(p []byte) (int, error) {
	if b.ended || b.tooLarge {
		return 0, io.EOF
	}
	if b.remaining < math.MaxInt64 && int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.reader.Read(p)
	if int64(n) > b.remaining {
		b.tooLarge = true
	} else {
		b.remaining -= int64(n)
	}
	b.ended = err != nil
	return n, err
}

func (b *uploadBody) drain() bool {
	_, _ = io.Copy(io.Discard, b)
	return b.tooLarge
}

func failure(w http.ResponseWriter, r *http.Request, err error) {
	status, line := http.StatusServiceUnavailable, store.Unreachable
	switch {
	case errors.Is(err, store.ErrPastExpiry):
		status, line = http.StatusBadRequest, PastExpiry
	case errors.Is(err, store.ErrNotFound):
		status, line = http.StatusNotFound, NotFound
	case errors.Is(err, store.ErrForbidden):
		status, line = http.StatusForbidden, Forbidden
	case errors.Is(err, store.ErrTooLarge):
		status, line = http.StatusRequestEntityTooLarge, TooLarge
		w.Header().Set("Connection", "close")
	case errors.Is(err, store.ErrBody):
		status, line = http.StatusBadRequest, BodyFailed
	case errors.Is(err, store.ErrInvalid):
		status, line = http.StatusBadRequest, InvalidPath
	}
	refusal(w, r, status, line)
}

// Handler serves artifact bytes and metadata.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body *uploadBody
		if r.Method == http.MethodPut {
			body = &uploadBody{reader: r.Body, remaining: max(cfg.Max, 0)}
		}
		reject := func(status int, line string) {
			if body != nil && body.drain() {
				status, line = http.StatusRequestEntityTooLarge, TooLarge
				w.Header().Set("Connection", "close")
			}
			refusal(w, r, status, line)
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(r.URL.EscapedPath(), urls.FilesPrefix)
		if !store.ValidPath(p) {
			reject(http.StatusBadRequest, InvalidPath)
			return
		}
		switch r.Method {
		case http.MethodPut:
			var exp time.Time
			values := r.Header.Values(ExpiresHeader)
			if len(values) > 0 {
				var err error
				if len(values) != 1 {
					reject(http.StatusBadRequest, InvalidExpires)
					return
				}
				exp, err = time.Parse(time.RFC3339, values[0])
				if err != nil {
					reject(http.StatusBadRequest, InvalidExpires)
					return
				}
			}
			caller, _ := identity.FromContext(r.Context())
			a, created, err := cfg.Store.Put(r.Context(), store.Draft{Path: p, ContentType: r.Header.Get("Content-Type"), OwnerID: caller.UserID, OwnerEmail: caller.Email, Expires: exp, Max: cfg.Max}, body)
			if err != nil {
				if body.drain() {
					err = store.ErrTooLarge
				}
				failure(w, r, err)
				return
			}
			name, status := trail.Replaced, http.StatusOK
			if created {
				name, status = trail.Created, http.StatusCreated
			}
			trail.Record(r.Context(), cfg.Telemetry, cfg.Events, name, a)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(Describe(a, urls.Base(r, cfg.ServicesPath)))
		case http.MethodDelete:
			caller, _ := identity.FromContext(r.Context())
			a, err := cfg.Store.Delete(r.Context(), p, caller.UserID)
			if err != nil {
				failure(w, r, err)
				return
			}
			trail.Record(r.Context(), cfg.Telemetry, cfg.Events, trail.Deleted, a)
			w.WriteHeader(http.StatusNoContent)
		default:
			a, f, err := cfg.Store.Open(r.Context(), p)
			if err != nil {
				failure(w, r, err)
				return
			}
			defer func() { _ = f.Close() }()
			name := path.Base(p)
			w.Header().Set("Content-Type", a.ContentType)
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("ETag", `"`+a.SHA256+`"`)
			http.ServeContent(w, r, name, a.Modified, f)
		}
	})
}
