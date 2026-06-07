// Command gmail is the loopback-only Gmail connector + event-plane producer
// behind nginx. It trusts the X-Owner-Email / X-Client-Id headers nginx injects
// after a successful auth_request against the dashboard's authorization server,
// and performs no token logic of its own. See appkit/server for the auth
// contract.
//
// The uniform chassis — the fixed subcommands (serve/version/manifest/migrate/
// backup/restore), config-from-env, the migration runner + downgrade guard, the
// loopback HTTP server + PRM + identity gate, and the /feed producer mount — is
// owned by appkit. main.go declares only gmail's identity (the Spec) and wires
// its domain surface through the Spec hooks. RESOURCE_ID / AUTH_SERVER are
// composed in-binary by appkit/config from IKIGENBA_DOMAIN + MOUNT.
//
// gmail is structurally dropbox's twin (decisions §1): an external-OAuth
// connector with an MCP surface, an internal poll daemon, and an event-plane
// producer half. This is the P1 SCAFFOLD: the Spec is producer-shaped (Feed +
// the static mail.* Events + the outbox retention manifest keys) and serves a
// STUB MCP handler exposing only health + reflection. The Gmail REST client (P2),
// the History-API producer + poll daemon (P3, wired through Producer/Workers),
// and the full mailbox MCP tool set (P4) are intentionally NOT here yet.
package main

import (
	"appkit"

	"gmail/internal/db"
	"gmail/internal/mcp"
)

func main() {
	appkit.Main(appkit.Spec{
		App:        "gmail",
		Mount:      "/srv/gmail/",
		Port:       3008,
		MCP:        true,
		Feed:       "/feed", // event-plane producer
		Migrations: db.FS,
		// Events is the static published-event registry — the three mail.* types
		// the producer will emit (decisions §1). Declared in P1 so the reflection
		// tool self-describes the producer; the emission engine lands in P3.
		Events: mcp.Events,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
		},
		// Handlers mounts the P1 STUB MCP surface (health + reflection only) behind
		// the nginx-injected identity gate. The full mailbox tool set arrives in P4;
		// the producer-injection (Producer) and poll-daemon (Workers) hooks are left
		// unset until P3.
		Handlers: func(rt *appkit.Router) error {
			rt.Handle("POST /mcp", rt.RequireIdentity(
				mcp.NewHandler(rt.Version(), rt.Service(), rt.Health(),
					rt.Events(), rt.Subscriptions())))
			return nil
		},
	})
}
