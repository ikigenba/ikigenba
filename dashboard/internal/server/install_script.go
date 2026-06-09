package server

import (
	"fmt"
	"net/http"
	"strings"

	"appkit/inventory"
)

// installTokenEnvVar is the environment variable the served install scripts
// (and the reference ~/.local/bin/install-mcp-* scripts) expect to hold the
// caller's bearer token. The agents store only this name and expand it at
// runtime — nothing secret is written into their config.
const installTokenEnvVar = "IKIGENBA_TOKEN"

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
		return fmt.Sprintf("claude mcp add --scope user --transport http %s %s --header 'Authorization: Bearer ${%s}'", name, resource, installTokenEnvVar)
	},
}

var codexAgent = agent{
	label:      "Codex",
	removeLine: func(name string) string { return fmt.Sprintf("codex mcp remove %s >/dev/null 2>&1 || true", name) },
	addLine: func(name, resource string) string {
		return fmt.Sprintf("codex mcp add %s --url %s --bearer-token-env-var %s", name, resource, installTokenEnvVar)
	},
}

// installScript builds the bash script served at GET /install/<agent> — the
// target of the landing page's `curl -fsSL https://<host>/install/<agent> | bash`
// one-paste. It opens with an `Installing N MCP` banner, then for every
// MCP-exposing service on the box it emits a remove-then-add pair (at user scope
// for the agents that have one), self-templated to the caller's host, using the
// command shapes the chosen agent descriptor supplies. Each `mcp add` is wrapped
// in an `if` that swallows its output and prints a single 🟢/🔴 status line, and
// a final `N of N successfully installed.` summarises the run. The remove
// (`|| true`) makes a re-run authoritative even though `mcp add` alone errors on
// a duplicate name; the `if` around the add means a failed add reports 🔴 and the
// run continues rather than aborting under `set -e`.
//
// The script is bash with strict mode (`set -euo pipefail`) and is still
// strict-mode safe. It references one shell variable — the bearer token env var
// IKIGENBA_TOKEN — but does so safely: the missing-token guard tests it via the
// `-u`-safe `${IKIGENBA_TOKEN:-}` default-empty form, and the agent `mcp add`
// lines pass the `${IKIGENBA_TOKEN}` reference single-quoted so bash does not
// expand it (the agent expands it at runtime — nothing secret is written into
// the script or the agent config). The tolerated failures (the `mcp remove`, a
// failing `mcp add`) are caught explicitly (`|| true`, the `if`) rather than
// relying on lenient pipe behavior. The landing page already runs it via
// `| bash`, so bash (not POSIX sh) is the correct interpreter for `pipefail`.
func installScript(ag agent, scheme, host string, svcs []inventory.Service) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")

	// Missing-token guard: the progressive-discovery moment. A brand-new caller
	// runs the one-paste with no token set; we explain how to mint one in the UI,
	// export it, and re-run, then exit non-zero. Self-templated to the request
	// origin so the instructions point at this box.
	fmt.Fprintf(&b, "if [ -z \"${%s:-}\" ]; then\n", installTokenEnvVar)
	fmt.Fprintf(&b, "  echo \"Error: %s is not set — this installer needs a token to authenticate.\" >&2\n", installTokenEnvVar)
	b.WriteString("  echo >&2\n")
	fmt.Fprintf(&b, "  echo \"  1. Sign in at %s://%s/ and, under \\\"Personal access tokens\\\", create a token.\" >&2\n", scheme, host)
	fmt.Fprintf(&b, "  echo \"  2. Add this to your ~/.bashrc:  export %s=\\\"<your token>\\\"\" >&2\n", installTokenEnvVar)
	b.WriteString("  echo \"  3. Open a new terminal (or: source ~/.bashrc), then re-run this installer.\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n\n")

	fmt.Fprintf(&b, "echo \"Installing %d MCP\"\n\n", len(svcs))
	b.WriteString("ok=0\n")
	for _, s := range svcs {
		resource := mcpResourceURL(scheme, host, s.Mount)
		name := mcpLocalName(s.Name)
		b.WriteString(ag.removeLine(name) + "\n")
		b.WriteString("if " + ag.addLine(name, resource) + " >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "  echo \"🟢 %s\"\n", name)
		b.WriteString("  ok=$((ok + 1))\n")
		b.WriteString("else\n")
		fmt.Fprintf(&b, "  echo \"🔴 %s\"\n", name)
		b.WriteString("fi\n\n")
	}
	fmt.Fprintf(&b, "echo \"${ok} of %d successfully installed.\"\n", len(svcs))
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
