package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// maxBodyBytes caps the inbound webhook payload at 1 MiB. A request whose body
// exceeds this (after a correct secret) is rejected with 413; a body of exactly
// maxBodyBytes is accepted.
const maxBodyBytes = 1 << 20 // 1 MiB

// notFoundBody is the single response body shared by every authentication outcome
// — wrong secret, unknown name, missing/malformed header — so the public ingress
// is byte-identical across all failures and leaks nothing about which check failed.
const notFoundBody = "not found\n"

// NewIngressHandler builds the public ingress endpoint for POST /in/<name>. It is
// the only surface a third party reaches directly (no front-door auth chain), so
// it trusts nothing: it never echoes caller identity and returns a byte-identical
// 404 for every authentication failure. Bearer authenticates before reading;
// github-hmac reads under the cap first because its signature covers the body.
func NewIngressHandler(svc *Service, log *slog.Logger) http.Handler {
	return &ingressHandler{svc: svc, log: log}
}

type ingressHandler struct {
	svc *Service
	log *slog.Logger
}

func (h *ingressHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
		return
	}
	if hasFrontDoorIdentity(r) {
		notFound(w)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/in/")
	if name == "" {
		notFound(w)
		return
	}

	wh, secretHash, secret, found, err := h.svc.store.GetByName(ctx, name)
	if err != nil {
		h.log.ErrorContext(ctx, "ingress: GetByName failed", "error", err)
		notFound(w)
		return
	}
	if !found {
		notFound(w)
		return
	}

	body, ok := authenticateAndRead(w, r, wh.Verification, secretHash, secret, h.log)
	if !ok {
		return
	}
	if err := h.svc.Record(ctx, wh, r.Header.Get("Content-Type"), body, capturedGitHubHeaders(r, wh.Verification)); err != nil {
		h.log.ErrorContext(ctx, "ingress: Record failed", "error", err)
		http.Error(w, "internal error\n", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, `{"status":"accepted"}`)
}

// hasFrontDoorIdentity rejects identity injected by the authenticated internal
// front door while allowing legitimate proxy headers such as X-Forwarded-Proto.
func hasFrontDoorIdentity(r *http.Request) bool {
	return r.Header.Get("X-Owner-Id") != "" ||
		r.Header.Get("X-Owner-Email") != "" ||
		r.Header.Get("X-Client-Id") != ""
}

// authenticateAndRead preserves each scheme's required ordering: bearer auth
// happens before reading, while GitHub HMAC verifies the capped raw body.
func authenticateAndRead(w http.ResponseWriter, r *http.Request, verification, secretHash, secret string, log *slog.Logger) ([]byte, bool) {
	if verification == "bearer" {
		presented, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || !verifySecret(presented, secretHash) {
			notFound(w)
			return nil, false
		}
	} else if verification != "github-hmac" {
		notFound(w)
		return nil, false
	}

	body, ok := readBody(w, r, log)
	if !ok {
		return nil, false
	}
	if verification == "github-hmac" && !verifyGitHubHMAC(r.Header.Get("X-Hub-Signature-256"), body, secret) {
		notFound(w)
		return nil, false
	}
	return body, true
}

func readBody(w http.ResponseWriter, r *http.Request, log *slog.Logger) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err == nil {
		return body, true
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		http.Error(w, "payload too large\n", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	log.ErrorContext(r.Context(), "ingress: read body failed", "error", err)
	http.Error(w, "bad request\n", http.StatusBadRequest)
	return nil, false
}

func capturedGitHubHeaders(r *http.Request, verification string) map[string]string {
	if verification != "github-hmac" {
		return nil
	}
	headers := make(map[string]string)
	for _, key := range []string{"x-github-event", "x-github-delivery"} {
		if value := r.Header.Get(key); value != "" {
			headers[key] = value
		}
	}
	return headers
}

func verifyGitHubHMAC(header string, body []byte, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	presented, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil || len(presented) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return subtle.ConstantTimeCompare(presented, mac.Sum(nil)) == 1
}

// notFound writes the single byte-identical 404 shared by every authentication
// failure so the public ingress leaks nothing about which check rejected the call.
func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, notFoundBody)
}

// bearerToken extracts the secret from an "Authorization: Bearer <secret>" header.
// ok is false for a missing, malformed, or empty-secret header.
func bearerToken(header string) (secret string, ok bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	secret = header[len(prefix):]
	if secret == "" {
		return "", false
	}
	return secret, true
}
