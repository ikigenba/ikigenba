package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"appkit"
	"appkit/web"
	"eventplane/outbox"
	"registry"

	reposdb "repos/internal/db"
	"repos/internal/mcp"
	"repos/internal/repos"
)

func reposSpec() appkit.Spec {
	var service *repos.Service
	var custody *repos.Custody

	return appkit.Spec{
		App:        "repos",
		Mount:      "/srv/repos/",
		Port:       registry.MustPort("repos"),
		MCP:        true,
		WWW:        true,
		Feed:       "/feed",
		Migrations: reposdb.FS,
		Events:     repos.Events,
		Handlers: func(rt *appkit.Router) error {
			if rt.DB() == nil {
				return fmt.Errorf("repos: no DB handle on router")
			}
			stateDir := os.Getenv("REPOS_STATE_DIR")
			if stateDir == "" {
				return fmt.Errorf("repos: REPOS_STATE_DIR is required")
			}
			var err error
			custody, err = repos.NewCustody(stateDir, repos.NewCommandGit(os.Getenv("REPOS_GIT_BIN"), stateDir), nil)
			if err != nil {
				return err
			}
			service = repos.NewService(repos.NewStore(rt.DB()))
			service.SetCustody(custody)
			handler, err := mcp.NewHandler(service, rt)
			if err != nil {
				return err
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(handler))
			rt.Handle("GET /{$}", landingHandler(rt.WWW(), rt.Service(), rt.Version()))
			return nil
		},
		Workers: []func(context.Context) error{func(ctx context.Context) error {
			if custody == nil {
				return fmt.Errorf("repos: custody worker started before handlers")
			}
			if err := custody.SweepHooks(ctx); err != nil {
				return err
			}
			<-ctx.Done()
			return nil
		}},
		Producer: func(producer *outbox.Outbox) error {
			if service == nil {
				return fmt.Errorf("repos: Producer called before Handlers")
			}
			service.SetProducer(producer)
			return nil
		},
		Health: func(context.Context) (map[string]any, error) {
			if service == nil {
				return nil, fmt.Errorf("repos: health called before Handlers")
			}
			return map[string]any{"repositories": 0}, nil
		},
	}
}

func landingHandler(site *web.Site, service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(w, request)
			return
		}
		if err := site.Render(w, "landing.html", struct {
			Service string
			Version string
		}{Service: service, Version: version}); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
