// Command wiki is the loopback-only knowledge-base service behind nginx. It
// trusts the X-Owner-Email / X-Client-Id headers nginx injects after a successful
// auth_request against the dashboard's authorization server, and performs no
// token logic of its own.
//
// The uniform chassis — the fixed subcommands (serve/version/manifest/migrate/
// backup/restore), config-from-env, the migration runner + downgrade guard, the
// loopback HTTP server + PRM + identity gate, and the /feed producer mount — is
// owned by appkit. main.go declares only wiki's identity (the Spec) and wires its
// surface (the wiki MCP tools and the two wiki.* producer events) through the
// Spec hooks.
//
// This is the P2 scaffold: the verb dispatcher, the MCP tool surface (domain
// tools stubbed not-implemented; health + reflection live), the producer outbox +
// the two declared event types, and the per-call-site config-injection seam
// (internal/config + internal/llm). The ingest path (P3), the worker spine (P4+),
// and the read side (P10) fill the stubs.
package main

import (
	"appkit"

	"wiki/internal/config"
	"wiki/internal/db"
	"wiki/internal/events"
	"wiki/internal/mcp"

	"eventplane/outbox"
)

func main() {
	appkit.Main(appkit.Spec{
		App:        "wiki",
		Mount:      "/srv/wiki/",
		Port:       3006,
		MCP:        true,
		Feed:       "/feed",                              // event-plane producer (design §8)
		Consumes:   []string{"dropbox", "crm", "ledger"}, // consumer doors land in P3
		Migrations: db.FS,
		Events:     events.Registry, // published wiki.* events: reflection + Append validation
		ManifestExtras: []appkit.ManifestKV{
			{Key: "WIKI_INBOX_INLINE_MAX", Value: "4096"},
			{Key: "WIKI_INGEST_MAX_BYTES", Value: "131072"},
			{Key: "WIKI_INTEGRATION_WORKERS", Value: "4"},
			{Key: "WIKI_EMBED_MODEL", Value: "text-embedding-3-large"},
			{Key: "WIKI_EMBED_DIMS", Value: "1024"},
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
		},
		// Config is the composition-root hook: read every non-secret knob plus the
		// secrets (ANTHROPIC_API_KEY / OPENAI_API_KEY / WIKI_OWNER) from env and
		// build the validated Config. Validating the per-call-site LLM triples here
		// fails startup loudly on a wrong model / rejected effort (design §10).
		Config: func(getenv func(string) string) (any, error) {
			return config.Load(getenv)
		},
		// Handlers mounts the wiki MCP surface, gated behind nginx-injected
		// identity. P2 carries no domain service yet — the domain tools are stubs.
		Handlers: func(rt *appkit.Router) error {
			rt.Handle("POST /mcp", rt.RequireIdentity(
				mcp.NewHandler(rt.Version(), rt.Service(), rt.Health(),
					rt.Events(), rt.Subscriptions())))
			return nil
		},
		// Producer fires after Handlers: P2 declares the outbox + the two event
		// types so /feed and reflection are live, but nothing is emitted yet (the
		// emit sites land in P3's ingest path and P4/P5's failure policy).
		Producer: func(ob *outbox.Outbox) error {
			_ = ob // emit wiring lands in P3/P4
			return nil
		},
	})
}
