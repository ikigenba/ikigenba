package server

import (
	"bytes"
	"errors"
	"net/http"
	"unicode"
	"unicode/utf8"

	"appkit/inventory"

	"dashboard/internal/session"
)

// indexData is the data passed to the index template. Host self-templates the
// page from the request so the same binary serves any account's apex (e.g.
// int.ikigenba.com). Owner is the signed-in user's email, or "" when logged out;
// the template branches on it ({{if .Owner}}).
type indexData struct {
	Host         string
	Scheme       string
	Owner        string
	OwnerInitial string
	Services     []serviceRow
}

// handleIndex renders the index template. It is identity-aware: a valid
// dashboard_session cookie makes the page show the owner and a sign-out control;
// otherwise it shows the logged-out landing. It renders into a buffer first so a
// template-execution failure becomes a clean 500 instead of a half-written 200.
func (a *app) handleIndex() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := indexData{Host: r.Host, Scheme: requestScheme(r)}
		if c, err := r.Cookie(sessionCookieName); err == nil {
			sess, lerr := a.sessions.Lookup(r.Context(), c.Value)
			switch {
			case lerr == nil:
				data.Owner = sess.OwnerEmail
			case errors.Is(lerr, session.ErrInvalid):
				// Present but dead cookie (revoked / expired / unknown): clear it
				// so the browser stops resending a value that can never redeem.
				clearSessionCookie(w, r)
			default:
				// Internal failure (db/parse) — render logged-out but keep the
				// cookie: a transient store error must not log the user out.
				a.logger.Error("index.session_lookup", "err", lerr)
			}
		}
		if data.Owner != "" {
			data.OwnerInitial = ownerInitial(data.Owner)

			// The LIST table: the box's MCP services as name/url rows, the raw
			// reference for manual entry into any other MCP client. A manifest read
			// failure drops the table, never 500s the page.
			if svcs, err := inventory.Read(a.manifestRoot); err != nil {
				a.logger.Error("index.read_inventory", "err", err)
			} else {
				data.Services = serviceRows(requestScheme(r), r.Host, svcs)
			}
		}

		var buf bytes.Buffer
		if err := a.tmpl.Execute(&buf, data); err != nil {
			a.logger.Error("index.render", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

func ownerInitial(email string) string {
	r, size := utf8.DecodeRuneInString(email)
	if r == utf8.RuneError && size <= 1 {
		return "?"
	}
	return string(unicode.ToUpper(r))
}
