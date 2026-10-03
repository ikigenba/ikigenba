package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	identity, token, err := s.identity(r, true)
	if err != nil {
		outcome := "failed"
		if errors.Is(err, store.ErrNotFound) {
			outcome = "unauthenticated"
			if token {
				outcome = "forbidden"
			}
		}
		s.recordCheck(r, outcome, store.Identity{})
		s.writeIdentityError(w, r, token, err)
		return
	}

	s.recordCheck(r, "allowed", identity)
	w.Header().Set(HeaderUserID, identity.UserID)
	w.Header().Set(HeaderUserEmail, identity.Email)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	identity, token, err := s.identity(r, false)
	if err != nil {
		s.writeIdentityError(w, r, token, err)
		return
	}

	body, err := json.Marshal(struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}{ID: identity.UserID, Email: identity.Email})
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) identity(r *http.Request, touch bool) (store.Identity, bool, error) {
	if kind, secret, malformed := tokenCredential(r); kind != "" {
		if malformed {
			return store.Identity{}, true, store.ErrNotFound
		}
		if touch {
			identity, err := s.st.TouchTokenIdentity(secret, s.now())
			return identity, true, err
		}
		identity, err := s.st.LookupTokenIdentity(secret, s.now())
		return identity, true, err
	}

	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return store.Identity{}, false, store.ErrNotFound
	}
	if touch {
		identity, err := s.st.TouchSession(cookie.Value, s.now())
		return identity, false, err
	}
	identity, err := s.st.LookupSessionIdentity(cookie.Value, s.now())
	return identity, false, err
}

func tokenCredential(r *http.Request) (kind, secret string, malformed bool) {
	authorization := r.Header.Get("Authorization")
	if secret, ok := strings.CutPrefix(authorization, "Bearer "); ok {
		return "token", secret, false
	}
	if encoded, ok := strings.CutPrefix(authorization, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		_, secret, colon := strings.Cut(string(decoded), ":")
		return "basic", secret, encoded == "" || err != nil || !colon
	}
	return "", "", false
}

func (s *Server) writeIdentityError(w http.ResponseWriter, r *http.Request, token bool, err error) {
	if !errors.Is(err, store.ErrNotFound) {
		s.writeServerError(w, r, err)
		return
	}
	if token {
		writePlainError(w, http.StatusForbidden, "token refused")
		return
	}
	writePlainError(w, http.StatusUnauthorized, "sign in required")
}

func (s *Server) recordCheck(r *http.Request, outcome string, resolved store.Identity) {
	credential := "none"
	if kind, _, _ := tokenCredential(r); kind != "" {
		credential = kind
	} else if _, err := r.Cookie(SessionCookieName); err == nil {
		credential = "session"
	}
	path, _, _ := strings.Cut(r.Header.Get("X-Original-URI"), "?")
	attrs := telemetry.Attrs{"outcome": outcome, "credential": credential, "method": r.Header.Get("X-Original-Method"), "host": r.Header.Get("X-Original-Host"), "path": path}
	name := "check.refused"
	switch outcome {
	case "allowed":
		name = "check.allowed"
		if credential == "token" || credential == "basic" {
			attrs["token"] = resolved.TokenID
		}
	case "failed":
		name = "check.failed"
	}
	s.record(r, name, resolved.UserID, attrs)
}
