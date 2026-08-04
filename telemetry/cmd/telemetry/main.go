package main

import (
	"fmt"

	"appkit"
	"registry"
	"telemetry/internal/db"
)

func main() {
	appkit.Main(telemetrySpec())
}

func telemetrySpec() appkit.Spec {
	return appkit.Spec{
		App:              "telemetry",
		Mount:            "/srv/telemetry/",
		Port:             registry.MustPort("telemetry"),
		MCP:              true,
		Migrations:       db.FS,
		TelemetryExclude: []string{"/ingest"},
		ManifestExtras: []appkit.ManifestKV{
			{Key: "TELEMETRY_RETENTION_DAYS", Value: "90"},
		},
		Handlers: func(rt *appkit.Router) error {
			if rt.DB() == nil {
				return fmt.Errorf("telemetry: no DB handle on router")
			}
			return nil
		},
	}
}
