// Package cronapp wires cron's service skeleton into the shared appkit chassis.
package cronapp

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

// Spec returns the production-shaped appkit service declaration.
func Spec() appkit.Spec {
	var store *crontab.Store
	var worker *tick.Worker

	return appkit.Spec{
		App:        "cron",
		Mount:      "/srv/cron/",
		Port:       3007,
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
