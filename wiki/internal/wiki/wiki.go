// Package wiki wires the service skeleton into the shared appkit chassis.
package wiki

import (
	"fmt"

	"appkit"

	"wiki/internal/db"
	"wiki/internal/mcp"
)

const (
	App   = "wiki"
	Mount = "/srv/wiki/"
	Port  = 3006
)

// Spec returns the production-shaped appkit service declaration.
func Spec() appkit.Spec {
	return appkit.Spec{
		App:        App,
		Mount:      Mount,
		Port:       Port,
		MCP:        true,
		Migrations: db.FS,
		Handlers: func(rt *appkit.Router) error {
			if rt.DB() == nil {
				return fmt.Errorf("wiki: no DB handle on router")
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(
				mcp.NewHandler(rt.Version(), rt.Service(), rt.Health())))
			return nil
		},
	}
}

// Main enters the shared appkit dispatcher.
func Main() {
	appkit.Main(Spec())
}
