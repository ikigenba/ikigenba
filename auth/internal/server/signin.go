package server

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"

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
		writeSignInPage(w, r.URL.Query().Get("return"))
		return
	}

	var rows bytes.Buffer
	if err := s.renderTokenRows(&rows, identity.UserID); err != nil {
		s.writeServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", signInHTMLContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html><body><p>%s</p><form method="post" action="/logout"><button type="submit">Log out</button></form><form method="post" action="/tokens"><input name="name"><select name="expires"><option value="never">never</option><option value="30d">30d</option><option value="90d">90d</option><option value="365d">365d</option></select><button type="submit">Create token</button></form><table>`, html.EscapeString(identity.Email))
	_, _ = rows.WriteTo(w)
	_, _ = io.WriteString(w, `</table></body></html>`)
}

func (s *Server) handleLoginGoogle(w http.ResponseWriter, r *http.Request) {
	verifier, err := mintPKCEVerifier(s.rand)
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	returnURL := r.URL.Query().Get("return")
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
		writeSignInPage(w, "")
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
		w.Header().Set("Content-Type", signInHTMLContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<!doctype html><html><body><p>Workspace membership required.</p></body></html>`)
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
	if r.Header.Get("Origin") != ownOrigin(r.Host) {
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

func writeSignInPage(w http.ResponseWriter, returnURL string) {
	target := "/login/google"
	if returnURL != "" {
		target += "?return=" + url.QueryEscape(returnURL)
	}
	w.Header().Set("Content-Type", signInHTMLContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html><body><a href="%s">Sign in with Google</a></body></html>`, html.EscapeString(target))
}

func writePlainError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, message)
}
