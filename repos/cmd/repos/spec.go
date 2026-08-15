package main

import (
	"appkit"
	"appkit/config"
	"appkit/web"
	"context"
	"encoding/json"
	"eventplane/outbox"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"registry"
	"repos/internal/mcp"
	"repos/internal/repos"
	"time"

	reposdb "repos/internal/db"
)

type runtimeKnobs struct {
	maxCommitBytes int64
}

func resolveStateDir(getenv func(string) string) (string, error) {
	if stateDir := getenv("REPOS_STATE_DIR"); stateDir != "" {
		resolved, err := filepath.Abs(stateDir)
		if err != nil {
			return "", fmt.Errorf("repos: resolve REPOS_STATE_DIR: %w", err)
		}
		return resolved, nil
	}
	if root := getenv("IKIGENBA_ROOT"); root != "" {
		return filepath.Join(root, "repos", "state"), nil
	}
	return "", fmt.Errorf("repos: REPOS_STATE_DIR or IKIGENBA_ROOT is required")
}

func resolveRuntimeKnobs(getenv func(string) string) (runtimeKnobs, error) {
	maxCommitBytes, err := config.EnvOrInt(getenv, "REPOS_MAX_COMMIT_BYTES", 67108864)
	if err != nil || maxCommitBytes <= 0 {
		return runtimeKnobs{}, fmt.Errorf("repos: REPOS_MAX_COMMIT_BYTES must be a positive integer")
	}
	return runtimeKnobs{maxCommitBytes: int64(maxCommitBytes)}, nil
}

func reposSpec() appkit.Spec {
	var service *repos.Service
	var custody *repos.Custody
	var store *repos.Store

	return appkit.Spec{
		App:   "repos",
		Mount: "/srv/repos/",
		Port:  registry.MustPort("repos"),
		MCP:   true,
		WWW:   true,
		Feed:  "/feed",
		ManifestExtras: []appkit.ManifestKV{
			{Key: "REPOS_MAX_COMMIT_BYTES", Value: "67108864"},
		},
		Migrations: reposdb.FS,
		Events:     repos.Events,
		Handlers: func(rt *appkit.Router) error {
			if rt.DB() == nil {
				return fmt.Errorf("repos: no DB handle on router")
			}
			stateDir, err := resolveStateDir(os.Getenv)
			if err != nil {
				return err
			}
			knobs, err := resolveRuntimeKnobs(os.Getenv)
			if err != nil {
				return err
			}
			gitBin := config.EnvOr(os.Getenv, "REPOS_GIT_BIN", "git")
			custody, err = repos.NewCustody(stateDir, repos.NewCommandGit(gitBin, stateDir), nil)
			if err != nil {
				return err
			}
			store = repos.NewStore(rt.DB())
			service = repos.NewService(store)
			service.SetCustody(custody)
			service.SetMaxCommitBytes(knobs.maxCommitBytes)
			rt.Handle("/git/", repos.GitDoorHandler(service))
			rt.HandleLoopback("POST /run-token", repos.RunTokenHandler(service))
			rt.HandleLoopback("GET /content", repos.ContentHandler(service))
			rt.HandleLoopback("GET /list", repos.ListHandler(service))
			rt.HandleLoopback("GET /stat", repos.StatHandler(service))
			rt.HandleLoopback("GET /archive", repos.ArchiveHandler(service))
			rt.HandleLoopback("PUT /content", repos.PutContentHandler(service))
			rt.HandleLoopback("DELETE /content", repos.DeleteContentHandler(service))
			rt.HandleLoopback("POST /commit", repos.CommitHandler(service))
			handler, err := mcp.NewHandler(service, rt)
			if err != nil {
				return err
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(handler))
			rt.Handle("GET /{$}", landingHandler(rt.WWW(), store, custody, rt.Service(), rt.Version()))
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
		}, func(ctx context.Context) error {
			if store == nil || custody == nil {
				return fmt.Errorf("repos: run token worker started before handlers")
			}
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					tx, err := store.BeginTx(ctx)
					if err != nil {
						return err
					}
					if _, err := store.SweepExpiredTokens(ctx, tx, custody.Now()); err != nil {
						_ = tx.Rollback()
						return err
					}
					if err := tx.Commit(); err != nil {
						return err
					}
				}
			}
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

type landingView struct {
	Service   string
	Version   string
	Repos     []repoRow
	ReposData template.JS
}

type repoRow struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	Sha       string `json:"sha"`
	ClonePath string `json:"clonePath"`
}

func landingHandler(site *web.Site, store *repos.Store, custody *repos.Custody, service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(w, request)
			return
		}
		repositories, err := store.ListAllRepositories(request.Context())
		if err != nil {
			http.Error(w, "list repositories", http.StatusInternalServerError)
			return
		}
		view := landingView{Service: service, Version: version, Repos: make([]repoRow, 0, len(repositories))}
		for _, repository := range repositories {
			refs, err := custody.Refs(request.Context(), repository.Kind, repository.Name)
			if err != nil {
				http.Error(w, "read repository refs", http.StatusInternalServerError)
				return
			}
			sha := refs["refs/heads/"+repository.DefaultBranch]
			if len(sha) > 7 {
				sha = sha[:7]
			}
			view.Repos = append(view.Repos, repoRow{
				Kind:      repository.Kind,
				Name:      repository.Name,
				Key:       repository.Kind + "/" + repository.Name,
				Sha:       sha,
				ClonePath: "/srv/repos/git/" + repository.Kind + "/" + repository.Name + ".git",
			})
		}
		data, err := json.Marshal(view.Repos)
		if err != nil {
			http.Error(w, "marshal repositories", http.StatusInternalServerError)
			return
		}
		view.ReposData = template.JS(data)
		if err := site.Render(w, "landing.html", view); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
