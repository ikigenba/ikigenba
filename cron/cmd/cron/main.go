// Command cron is the loopback-only scheduled-event-emitter service behind
// nginx. It serves a bearer-gated MCP surface for agents and a
// dashboard-session-cookie-gated human web landing page under /srv/cron/. It trusts
// the X-Owner-Email / X-Client-Id headers nginx injects after a successful
// auth_request against the dashboard's authorization server, and performs no
// token logic of its own; nginx remains the sole trust boundary for both doors.
//
// The uniform chassis — the fixed subcommands, config-from-env, the migration
// runner + downgrade guard, the loopback HTTP server + PRM + identity gate, and
// the /feed producer mount — is owned by appkit. main.go declares only cron's
// identity (the Spec) and wires its
// domain surface: the crontab CRUD MCP tools, the minute-aligned tick worker that
// emits cron.<name> events, and the LIVE Publishes provider that reports those
// types from the crontab. RESOURCE_ID / AUTH_SERVER are composed in-binary by
// appkit/config from IKIGENBA_DOMAIN + MOUNT.
package main

import (
	"context"
	"fmt"
	"log/slog"

	"appkit"

	"cron/internal/crontab"
	"cron/internal/db"
	"cron/internal/event"
	"cron/internal/mcp"
	"cron/internal/tick"
	"cron/internal/web"

	"eventplane/outbox"
)

func main() {
	appkit.Main(cronSpec())
}

func cronSpec() appkit.Spec {
	var store *crontab.Store
	var worker *tick.Worker

	return appkit.Spec{
		App:        "cron",
		Mount:      "/srv/cron/",
		Port:       3005,
		MCP:        true,
		Feed:       "/feed",
		Migrations: db.FS,
		Publishes: func() outbox.Registry {
			if store == nil {
				return outbox.Registry{}
			}
			return event.Publishes(store)()
		},
		ManifestExtras: []appkit.ManifestKV{
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
		},
		Handlers: func(rt *appkit.Router) error {
			conn := rt.DB()
			if conn == nil {
				return fmt.Errorf("cron: no DB handle on router")
			}
			store = crontab.NewStore(conn)
			rt.Handle("GET /{$}", web.LandingHandler(rt.Service(), rt.Version()))
			rt.Handle("GET /static/", web.StaticHandler())
			rt.Handle("POST /mcp", rt.RequireIdentity(
				mcp.NewHandler(store, rt.Version(), rt.Service(), rt.Health(),
					rt.Publishes(), rt.Subscriptions())))
			return nil
		},
		Producer: func(ob *outbox.Outbox) error {
			if store == nil {
				return fmt.Errorf("cron: Producer called before Handlers built the Store")
			}
			worker = tick.New(store.DB(), store, ob, slog.Default())
			return nil
		},
		Workers: []func(context.Context) error{
			func(ctx context.Context) error {
				if worker == nil {
					return fmt.Errorf("cron: tick worker not wired (Producer did not run)")
				}
				return worker.Run(ctx)
			},
		},
	}
}
