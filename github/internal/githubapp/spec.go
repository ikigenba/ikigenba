// Package githubapp wires github's service skeleton into the shared appkit chassis.
package githubapp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"appkit"

	"github/internal/db"
	gh "github/internal/gh"
	"github/internal/mcp"
	"github/internal/web"
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
			staticHandler := web.StaticHandler()
			rt.Handle("GET /{$}", web.LandingHandler(rt.Service(), rt.Version()))
			rt.Handle("GET /static/", http.StripPrefix("/static/", staticHandler))
			rt.Handle("GET /favicon.ico", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				staticRequest := *r
				staticURL := *r.URL
				staticURL.Path = "/static/favicon.ico"
				staticRequest.URL = &staticURL
				staticRequest.RequestURI = ""
				staticHandler.ServeHTTP(w, &staticRequest)
			}))
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
