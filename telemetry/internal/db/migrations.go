package db

import "embed"

// FS contains telemetry's forward-only database migrations.
//
//go:embed migrations/*.sql
var FS embed.FS
