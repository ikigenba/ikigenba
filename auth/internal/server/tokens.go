package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/auth/internal/store"
)

const htmlDocumentContentType = "text/html; charset=utf-8"

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	if !tokenOriginAllowed(r) {
		writeTokenError(w, http.StatusForbidden, "forbidden")
		return
	}

	identity, err := s.tokenSessionIdentity(r)
	if errors.Is(err, store.ErrNotFound) {
		writeTokenError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	if err := r.ParseForm(); err != nil {
		create := tokenCreateValues(r.Form.Get("name"), r.Form.Get("expires"), true)
		writeAuthPage(w, http.StatusBadRequest, authPageData{Banner: s.pageBanner(identity.Email), Create: &create})
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	expiry, validExpiry := tokenExpiry(r.Form.Get("expires"))
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 || !validExpiry {
		create := tokenCreateValues(r.Form.Get("name"), r.Form.Get("expires"), true)
		writeAuthPage(w, http.StatusBadRequest, authPageData{Banner: s.pageBanner(identity.Email), Create: &create})
		return
	}

	_, secret, err := s.st.CreateToken(identity.UserID, name, expiry, s.now())
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	writeAuthPage(w, http.StatusOK, authPageData{Banner: s.pageBanner(identity.Email), Created: &tokenCreatedData{Name: name, Secret: secret}})
}

func (s *Server) handleTokenAction(w http.ResponseWriter, r *http.Request) {
	if !tokenOriginAllowed(r) {
		writeTokenError(w, http.StatusForbidden, "forbidden")
		return
	}

	identity, err := s.tokenSessionIdentity(r)
	if errors.Is(err, store.ErrNotFound) {
		writeTokenError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	switch r.PathValue("action") {
	case "enable":
		err = s.st.SetTokenEnabled(identity.UserID, r.PathValue("id"), true)
	case "disable":
		err = s.st.SetTokenEnabled(identity.UserID, r.PathValue("id"), false)
	case "delete":
		err = s.st.DeleteToken(identity.UserID, r.PathValue("id"))
	default:
		writeTokenError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeTokenError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) tokenSessionIdentity(r *http.Request) (store.Identity, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return store.Identity{}, store.ErrNotFound
	}
	return s.st.LookupSessionIdentity(cookie.Value, s.now())
}

func tokenExpiry(value string) (store.Expiry, bool) {
	expiry := store.Expiry(value)
	switch expiry {
	case store.ExpiryNever, store.Expiry30d, store.Expiry90d, store.Expiry365d:
		return expiry, true
	default:
		return "", false
	}
}

func tokenOriginAllowed(r *http.Request) bool {
	return r.Header.Get("Origin") == ownOrigin(r.Host)
}

func writeTokenError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, message)
}
