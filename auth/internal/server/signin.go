package server

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

// pkceVerifierBytes is the octet length of a PKCE code verifier before
// Crockford encoding. 32 bytes encode to 52 characters, inside RFC 7636's
// 43..128 range, and match the length idcodec uses for secrets.
const pkceVerifierBytes = 32

const signInHTMLContentType = "text/html; charset=utf-8"

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	identity, signedIn, err := s.sessionIdentity(r)
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	if !signedIn {
		writeSignInPage(w, r.Host, s.cfg.WorkspaceDomain, returnQuery(r.URL.RawQuery))
		return
	}

	rows, err := s.tokenRows(identity.UserID, s.now())
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	writeAuthPage(w, http.StatusOK, authPageData{Email: identity.Email, Profile: &profilePageData{
		Apex: apexName(r.Host), Email: identity.Email, Workspace: s.cfg.WorkspaceDomain,
		Rows: rows, Create: tokenCreateValues("", "90d", false),
	}})
}

func (s *Server) handleLoginGoogle(w http.ResponseWriter, r *http.Request) {
	verifier, err := mintPKCEVerifier(s.rand)
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	returnURL := returnQuery(r.URL.RawQuery)
	loginState, err := s.st.CreateLoginState(verifier, returnURL)
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	redirect := redirectURI(r.Host)
	authURL, err := s.gc.AuthCodeURL(loginState.State, verifier, redirect)
	if err != nil {
		s.writeDiagnostic(r, err)
		_, _ = s.st.ConsumeLoginState(loginState.State)
		writePlainError(w, http.StatusBadGateway, "Google sign-in failed")
		return
	}

	// Absolute authorization URL: same 302 as http.Redirect, Location left as authURL.
	h := w.Header()
	_, hadCT := h["Content-Type"]
	h.Set("Location", authURL)
	if !hadCT && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		h.Set("Content-Type", "text/html; charset=utf-8")
	}
	w.WriteHeader(http.StatusFound)
	if !hadCT && r.Method == http.MethodGet {
		_, _ = fmt.Fprintln(w, `<a href="`+html.EscapeString(authURL)+`">`+http.StatusText(http.StatusFound)+`</a>.`+"\n")
	}
}

func mintPKCEVerifier(rand io.Reader) (string, error) {
	raw := make([]byte, pkceVerifierBytes)
	if _, err := io.ReadFull(rand, raw); err != nil {
		return "", fmt.Errorf("mint PKCE verifier: %w", err)
	}
	return idcodec.Encode(raw), nil
}

func (s *Server) handleLoginGoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateValue := r.URL.Query().Get("state")
	if r.URL.Query().Get("error") == "access_denied" {
		if stateValue != "" {
			_, err := s.st.ConsumeLoginState(stateValue)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				s.writeServerError(w, r, err)
				return
			}
		}
		writeCancelledPage(w, r.Host)
		return
	}

	loginState, err := s.st.ConsumeLoginState(stateValue)
	if errors.Is(err, store.ErrNotFound) {
		writePlainError(w, http.StatusBadRequest, "invalid login state")
		return
	}
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	claims, err := s.gc.Exchange(r.Context(), r.URL.Query().Get("code"), loginState.Verifier, redirectURI(r.Host))
	if err != nil {
		s.writeDiagnostic(r, err)
		writePlainError(w, http.StatusBadGateway, "Google sign-in failed")
		return
	}
	if !claims.EmailVerified || claims.HostedDomain != s.cfg.WorkspaceDomain {
		writeNonMemberPage(w, r.Host, s.cfg.WorkspaceDomain, claims.Email)
		return
	}

	user, err := s.st.UpsertUserOnLogin(claims.Issuer, claims.Subject, claims.Email, s.now())
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	session, err := s.st.CreateSession(user.ID, s.now())
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	http.SetCookie(w, cookieForHost(r.Host, session.ID, false))

	location := "/"
	if loginState.ReturnURL != "" && inSpace(loginState.ReturnURL, r.Host) {
		location = loginState.ReturnURL
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || !onSpaceOrigin(origins[0], r.Host) {
		writePlainError(w, http.StatusForbidden, "forbidden")
		return
	}

	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := s.st.DeleteSession(cookie.Value); err != nil {
			s.writeServerError(w, r, err)
			return
		}
	}
	http.SetCookie(w, cookieForHost(r.Host, "", true))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) sessionIdentity(r *http.Request) (store.Identity, bool, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return store.Identity{}, false, nil
	}
	identity, err := s.st.LookupSessionIdentity(cookie.Value, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return store.Identity{}, false, nil
	}
	return identity, err == nil, err
}

func writePlainError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, message)
}
