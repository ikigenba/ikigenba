// Package githubapp wires github's service skeleton into the shared appkit chassis.
package githubapp

import (
	"appkit"
	"context"
	"fmt"
	"github/internal/db"
	"github/internal/mcp"
	"net/http"
	"os"
	"time"

	appweb "appkit/web"

	gh "github/internal/gh"
)

var newGitHubClient = gh.NewClient

// Spec returns the production-shaped appkit service declaration.
func Spec() appkit.Spec {
	var client *gh.Client
	health := func(ctx context.Context) (map[string]any, error) {
		if client == nil {
			return nil, fmt.Errorf("github: client not initialized")
		}
		if _, err := client.ReposList(ctx); err != nil {
			return nil, err
		}
		return map[string]any{"github_auth": "ok"}, nil
	}

	return appkit.Spec{
		App:        "github",
		Mount:      "/srv/github/",
		Port:       3203,
		MCP:        true,
		WWW:        true,
		Migrations: db.FS,
		Health:     health,
		Config: func(getenv func(string) string) (any, error) {
			cfg := gh.ConfigFromEnv(getenv)
			_, err := newGitHubClient(cfg, nil)
			return cfg, err
		},
		Handlers: func(rt *appkit.Router) error {
			cfg := gh.ConfigFromEnv(os.Getenv)
			var err error
			client, err = newGitHubClient(cfg, rt.HTTPClient(30*time.Second))
			if err != nil {
				return err
			}
			rt.Handle("GET /{$}", landingHandler(rt.WWW(), rt.Service(), rt.Version()))
			rt.HandleLoopback("GET /pr", client.PRHandler())
			rt.HandleLoopback("GET /token", client.TokenHandler())
			handler, err := mcp.NewHandler(client, rt)
			if err != nil {
				return err
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(handler))
			return nil
		},
	}
}

func landingHandler(site *appweb.Site, service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = site.Render(w, "landing.html", struct{ Service, Version string }{service, version})
	})
}
