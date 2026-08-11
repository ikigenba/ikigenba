// Command artifacts is the composition root for the suite's artifact service.
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"appkit"
	artifactdata "artifacts/internal/artifacts"
	"artifacts/internal/db"
	"registry"
)

const defaultMaxUploadBytes int64 = 209715200

type config struct {
	MaxUploadBytes int64
}

func loadConfig(getenv func(string) string) (any, error) {
	const name = "ARTIFACTS_MAX_UPLOAD_BYTES"
	raw := getenv(name)
	if raw == "" {
		return config{MaxUploadBytes: defaultMaxUploadBytes}, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", name)
	}
	return config{MaxUploadBytes: value}, nil
}

func artifactsSpec() appkit.Spec {
	return appkit.Spec{
		App:        "artifacts",
		Mount:      "/srv/artifacts/",
		Port:       registry.MustPort("artifacts"),
		MCP:        true,
		Migrations: db.FS,
		Config:     loadConfig,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "ARTIFACTS_MAX_UPLOAD_BYTES", Value: "209715200"},
		},
		Handlers: func(rt *appkit.Router) error {
			loaded, err := loadConfig(os.Getenv)
			if err != nil {
				return err
			}
			root := os.Getenv("IKIGENBA_ROOT")
			if root == "" {
				root = "."
			}
			uploads := artifactdata.NewService(
				db.NewStore(rt.DB(), artifactdata.NewToken),
				&artifactdata.BlobStore{Root: filepath.Join(root, "artifacts", "state")},
				time.Now,
				loaded.(config).MaxUploadBytes,
			)
			rt.Handle("/u/", uploads.UploadHandler())
			uploads.MountDownloads(rt)
			rt.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = w.Write([]byte("artifacts\n"))
			}))
			return nil
		},
	}
}

func main() {
	if _, err := loadConfig(os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "artifacts: %v\n", err)
		os.Exit(1)
	}
	appkit.Main(artifactsSpec())
}
