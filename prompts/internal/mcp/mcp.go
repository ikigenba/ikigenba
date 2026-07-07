// Package mcp exposes prompts' domain tools through the shared appkit MCP
// transport. Authentication, JSON-RPC framing, chassis health, and event
// reflection are owned by appkit/mcp; this package declares only prompts'
// instructions and domain tool handlers.
package mcp

import (
	"net/http"

	"appkit"
	appkitmcp "appkit/mcp"

	"prompts/internal/prompt"
)

// Instructions is the short initialize-time guidance returned by the shared
// MCP transport. The deeper usage guide is loaded on demand through describe.
const Instructions = "Prompts runs sandboxed Claude agent sessions on your behalf. " +
	"If you haven't used prompts before, call describe first — it explains " +
	"what a prompt and a run are, the create→run→poll→read lifecycle, and the " +
	"output format — then use the other tools."

// NewHandler assembles prompts' MCP surface over the shared appkit transport.
func NewHandler(svc *prompt.Service, rt *appkit.Router) (http.Handler, error) {
	return appkitmcp.New(appkitmcp.Options{
		Service:       rt.Service(),
		Version:       rt.Version(),
		Instructions:  Instructions,
		Tools:         Tools(svc),
		Health:        rt.Health(),
		Events:        rt.Events(),
		Publishes:     rt.Publishes(),
		Subscriptions: rt.Subscriptions(),
	})
}
