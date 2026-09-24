package server

import (
	"errors"
	"fmt"
	"html"
	"io"
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
		writeTokenCreateForm(w, http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	expiry, validExpiry := tokenExpiry(r.Form.Get("expires"))
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 || !validExpiry {
		writeTokenCreateForm(w, http.StatusBadRequest)
		return
	}

	_, secret, err := s.st.CreateToken(identity.UserID, name, expiry, s.now())
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", htmlDocumentContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html><body><p>Token created.</p><code id="token-secret">%s</code><button type="button" onclick="navigator.clipboard.writeText(document.getElementById('token-secret').textContent)">Copy</button><a href="/">Back to profile</a></body></html>`, html.EscapeString(secret))
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

// renderTokenRows writes the D07-owned part of a signed-in user's profile.
// The profile handler owns the surrounding page and calls this with the
// identity returned by LookupSessionIdentity.
func (s *Server) renderTokenRows(w io.Writer, userID string) error {
	tokens, err := s.st.ListTokens(userID)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		expires := "never"
		if token.ExpiresAt != nil {
			expires = token.ExpiresAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		lastUsed := "never"
		if token.LastUsedAt != nil {
			lastUsed = token.LastUsedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		action := "enable"
		if token.Enabled {
			action = "disable"
		}
		if _, err := fmt.Fprintf(w, `<tr data-token-id="%s"><td class="name">%s</td><td class="created">%s</td><td class="last-used">%s</td><td class="expiry">%s</td><td class="enabled">%t</td><td><form method="post" action="/tokens/%s/%s"><button type="submit">%s</button></form><form method="post" action="/tokens/%s/delete"><button type="submit">delete</button></form></td></tr>`,
			html.EscapeString(token.ID),
			html.EscapeString(token.Name),
			html.EscapeString(token.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")),
			html.EscapeString(lastUsed),
			html.EscapeString(expires),
			token.Enabled,
			html.EscapeString(token.ID),
			action,
			action,
			html.EscapeString(token.ID),
		); err != nil {
			return err
		}
	}
	return nil
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

func writeTokenCreateForm(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", htmlDocumentContentType)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `<!doctype html><html><body><form method="post" action="/tokens"><input name="name"><select name="expires"><option value="never">never</option><option value="30d">30d</option><option value="90d">90d</option><option value="365d">365d</option></select><button type="submit">Create token</button></form></body></html>`)
}

func writeTokenError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, message)
}
