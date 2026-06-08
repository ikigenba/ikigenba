package server

import (
	"fmt"
	"net/http"
	"strings"

	"appkit/inventory"
)

// agent is one coding-agent target the /install/<agent> route can wire up: a
// display label (for the restart message) and the CLI command shapes it uses to
// (re)register an MCP server. Two literal instances back the two routes — a
// fixed list, not a registry.
type agent struct {
	label      string
	removeLine func(name string) string
	addLine    func(name, resource string) string
}

var claudeAgent = agent{
	label: "Claude Code",
	removeLine: func(name string) string {
		return fmt.Sprintf("claude mcp remove --scope user %s >/dev/null 2>&1 || true", name)
	},
	addLine: func(name, resource string) string {
		return fmt.Sprintf("claude mcp add --scope user --transport http %s %s", name, resource)
	},
}

var codexAgent = agent{
	label:      "Codex",
	removeLine: func(name string) string { return fmt.Sprintf("codex mcp remove %s >/dev/null 2>&1 || true", name) },
	addLine:    func(name, resource string) string { return fmt.Sprintf("codex mcp add %s --url %s", name, resource) },
}

// installScript builds the bash script served at GET /install/<agent> — the
// target of the landing page's `curl -fsSL https://<host>/install/<agent> | bash`
// one-paste. For every MCP-exposing service on the box it emits a remove-then-add
// pair (at user scope for the agents that have one), self-templated to the
// caller's host, using the command shapes the chosen agent descriptor supplies.
// The remove (`|| true`) makes a re-run authoritative even though `mcp add` alone
// errors on a duplicate name; `set -euo pipefail` still aborts loudly on a real
// failure (missing CLI, network).
//
// The script is bash with strict mode (`set -euo pipefail`): it is provably safe
// under -u/pipefail because every value is baked in here as a literal (no shell
// variable references → no unset-var risk) and the only tolerated failure (the
// `mcp remove`) is guarded by an explicit `|| true` rather than relying on
// lenient pipe behavior. The landing page already runs it via `| bash`, so bash
// (not POSIX sh) is the correct interpreter for `pipefail`.
func installScript(ag agent, scheme, host string, svcs []inventory.Service) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	for _, s := range svcs {
		resource := mcpResourceURL(scheme, host, s.Mount)
		name := mcpLocalName(s.Name)
		b.WriteString(ag.removeLine(name) + "\n")
		b.WriteString(ag.addLine(name, resource) + "\n\n")
	}
	fmt.Fprintf(&b, "echo \"Done. Restart %s for the new MCP servers to load.\"\n", ag.label)
	return b.String()
}

// handleInstall serves GET /install/<agent>: a public (no-auth) bash script that
// wires the box's MCP services into the given coding agent's install via its
// `mcp add` command. Public for the same reason as /services — it only emits the
// box's own resource URLs, which are themselves OAuth-protected. text/plain so
// curl gets verbatim bytes.
func (a *app) handleInstall(ag agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svcs, err := inventory.Read(a.manifestRoot)
		if err != nil {
			a.logger.Error("install.read_inventory", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		script := installScript(ag, requestScheme(r), r.Host, svcs)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(script))
	}
}
