package main

import (
	"context"
	"fmt"

	"appkit"
	"registry"
	"telemetry/internal/db"
	"telemetry/internal/ingest"
	telemetrymcp "telemetry/internal/mcp"
	"telemetry/internal/retention"
	telemetrytime "telemetry/internal/telemetry"
)

func main() {
	appkit.Main(telemetrySpec())
}

func telemetrySpec() appkit.Spec {
	var store *db.Store
	return appkit.Spec{
		App:              "telemetry",
		Mount:            "/srv/telemetry/",
		Port:             registry.MustPort("telemetry"),
		MCP:              true,
		Migrations:       db.FS,
		TelemetryExclude: []string{ingest.Path},
		ManifestExtras: []appkit.ManifestKV{
			{Key: "TELEMETRY_RETENTION_DAYS", Value: "90"},
		},
		Handlers: func(rt *appkit.Router) error {
			if rt.DB() == nil {
				return fmt.Errorf("telemetry: no DB handle on router")
			}
			store = db.NewStore(rt.DB())
			ingest.Mount(rt, store, telemetrytime.RealClock{})
			retention.Start(rt, store, telemetrytime.RealClock{})
			mcpHandler, err := telemetrymcp.NewHandler(store, telemetrytime.RealClock{}, rt)
			if err != nil {
				return fmt.Errorf("telemetry: create MCP handler: %w", err)
			}
			rt.Handle("/mcp", rt.RequireIdentity(mcpHandler))
			return nil
		},
		Health: func(ctx context.Context) (map[string]any, error) {
			if store == nil {
				return nil, fmt.Errorf("telemetry: store is not initialized")
			}
			total, err := store.DroppedTotal(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]any{"dropped_total": total}, nil
		},
	}
}
