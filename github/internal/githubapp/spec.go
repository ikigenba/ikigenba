// Package githubapp wires github's service skeleton into the shared appkit chassis.
package githubapp

import (
	"appkit"

	"github/internal/db"
)

// Spec returns the production-shaped appkit service declaration.
func Spec() appkit.Spec {
	return appkit.Spec{
		App:        "github",
		Mount:      "/srv/github/",
		Port:       3203,
		MCP:        true,
		Migrations: db.FS,
		Handlers: func(rt *appkit.Router) error {
			return nil
		},
	}
}
