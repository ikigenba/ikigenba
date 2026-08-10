package server

// This file builds the name/url service rows for the LIST table shown at the
// bottom of the logged-in index: the raw reference (local MCP name + resource
// URL) for manually wiring this box's MCP services into any MCP client. The
// scheme/host helpers it carries are shared with install_script.go so the
// served one-paste install scripts and the table can't drift.

import (
	"bytes"
	"errors"
	"net/http"

	"appkit/inventory"

	"dashboard/internal/session"
)

type installData struct {
	Host         string
	Scheme       string
	Owner        string
	OwnerInitial string
	CurrentPage  string
	Version      string
}

// handleInstallPage renders the session-gated human install page. The agent
// script endpoints remain owned by handleInstall in install_script.go.
func (a *app) handleInstallPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := a.sessionOwner(r)
		if !ok {
			if c, err := r.Cookie(sessionCookieName); err == nil {
				if _, lookupErr := a.sessions.Lookup(r.Context(), c.Value); errors.Is(lookupErr, session.ErrInvalid) {
					clearSessionCookie(w, r)
				}
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		data := installData{
			Host:         r.Host,
			Scheme:       requestScheme(r),
			Owner:        owner,
			OwnerInitial: ownerInitial(owner),
			CurrentPage:  "install",
			Version:      buildVersion(),
		}
		var buf bytes.Buffer
		if err := a.tmpl.ExecuteTemplate(&buf, "install.html", data); err != nil {
			a.logger.Error("install.render", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

// requestScheme resolves the external scheme for a request behind nginx: the
// front door terminates TLS and forwards X-Forwarded-Proto, so trust it and
// default to https when absent. Shared by the index install snippets and the
// /services inventory endpoint so the two can't drift.
func requestScheme(r *http.Request) string {
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "https"
}

// mcpResourceURL is the full MCP endpoint URL for a service, self-templated from
// the request: <scheme>://<host><mount>mcp. Mount carries its own trailing slash
// (e.g. "/srv/crm/"), so "mcp" appends directly.
func mcpResourceURL(scheme, host, mount string) string {
	return scheme + "://" + host + mount + "mcp"
}

// mcpLocalName is the local MCP registration handle for a service: the bare
// service name namespaced with an "ikigenba_" prefix (e.g. "ikigenba_crm"), used
// as the `claude mcp add`/`codex mcp add` <name> argument so the suite's MCP
// servers don't collide with generically-named servers in a user's config. Only
// the registration handle is prefixed — the resource URL (mcpResourceURL) and the
// on-box service name are unchanged. Shared by the index install snippets and the
// /install one-paste script so the two can't drift.
func mcpLocalName(svc string) string {
	return "ikigenba_" + svc
}

// serviceRow is one row in the index's LIST table: the local MCP registration
// handle (Name) and the service's resource URL (URL).
type serviceRow struct {
	Name string
	URL  string
}

// serviceRows turns the box's MCP-exposing services into the index LIST table:
// one row per service, ordered as inventory returns them (by name). Name is the
// local registration handle (mcpLocalName: "ikigenba_<svc>"); URL is the
// self-templated MCP resource URL (mcpResourceURL).
func serviceRows(scheme, host string, svcs []inventory.Service) []serviceRow {
	out := make([]serviceRow, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, serviceRow{
			Name: mcpLocalName(s.Name),
			URL:  mcpResourceURL(scheme, host, s.Mount),
		})
	}
	return out
}
