// Package ingress admits deliveries from senders outside the suite at /in/<slug>.
package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
	"github.com/ikigenba/ikigenba/webhooks/internal/urls"
)

// The headers a sender authenticates with and the ones a delivery keeps.
const (
	AuthHeader           = "X-Webhook-Secret"
	SignatureHeader      = "X-Hub-Signature-256"
	SignaturePrefix      = "sha256="
	GitHubEventHeader    = "X-GitHub-Event"
	GitHubDeliveryHeader = "X-GitHub-Delivery"
)

// Config supplies the records and the two event sinks.
type Config struct {
	Store     *store.Store
	Telemetry *telemetry.Writer
	Events    *events.Emitter
}

// Admits reports whether a delivery's headers and raw body carry the webhook's secret.
func Admits(h store.Webhook, header http.Header, body []byte) bool {
	switch h.Scheme {
	case store.Bearer:
		values := header.Values(AuthHeader)
		if len(values) != 1 || h.SecretSHA256 == "" {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(store.HashSecret(values[0])), []byte(h.SecretSHA256)) == 1
	case store.GitHubHMAC:
		values := header.Values(SignatureHeader)
		if len(values) != 1 || h.SecretPlain == "" || !strings.HasPrefix(values[0], SignaturePrefix) {
			return false
		}
		got, err := hex.DecodeString(values[0][len(SignaturePrefix):])
		if err != nil || len(got) != sha256.Size {
			return false
		}
		mac := hmac.New(sha256.New, []byte(h.SecretPlain))
		_, _ = mac.Write(body)
		return hmac.Equal(got, mac.Sum(nil))
	}
	return false
}

func answer(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

func refuse(w http.ResponseWriter) {
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusNotFound)
}

func unreachable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, store.Unreachable+"\n")
}

// Handler answers every request whose path begins with urls.IngressPrefix.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			answer(w, http.StatusMethodNotAllowed)
			return
		}
		slug := strings.TrimPrefix(r.URL.Path, urls.IngressPrefix)
		if !store.ValidSlug(slug) {
			refuse(w)
			return
		}
		h, err := cfg.Store.Get(r.Context(), slug)
		if errors.Is(err, store.ErrNotFound) {
			refuse(w)
			return
		}
		if err != nil {
			unreachable(w)
			return
		}
		if h.Scheme == store.Bearer && !Admits(h, r.Header, nil) {
			refuse(w)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, store.MaxBody+1))
		if err != nil {
			answer(w, http.StatusBadRequest)
			return
		}
		if len(body) > store.MaxBody {
			answer(w, http.StatusRequestEntityTooLarge)
			return
		}
		if h.Scheme == store.GitHubHMAC && !Admits(h, r.Header, body) {
			refuse(w)
			return
		}
		d, err := cfg.Store.Receive(r.Context(), h.ID, store.Arrival{ContentType: r.Header.Get("Content-Type"), GitHubEvent: r.Header.Get(GitHubEventHeader), GitHubDelivery: r.Header.Get(GitHubDeliveryHeader), Body: body})
		if errors.Is(err, store.ErrNotFound) {
			refuse(w)
			return
		}
		if err != nil {
			unreachable(w)
			return
		}
		answer(w, http.StatusAccepted)
		caller, _ := identity.FromContext(r.Context())
		ctx := identity.NewContext(context.Background(), identity.Caller{UserID: h.OwnerID, Email: h.OwnerEmail, RequestID: caller.RequestID})
		trail.Receive(ctx, cfg.Telemetry, cfg.Events, h, d)
	})
}
