// Command artifacts is the composition root for the suite's artifact service.
package main

import (
	"appkit"
	"artifacts/internal/db"
	"artifacts/internal/mcp"
	"bytes"
	"eventplane/outbox"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"registry"
	"strconv"
	"strings"
	"time"

	artifactdata "artifacts/internal/artifacts"

	"github.com/dustin/go-humanize"
)

const defaultMaxUploadBytes int64 = 209715200

type config struct {
	MaxUploadBytes int64
}

const downloadMount = "/srv/artifacts/"

type landingRenderer interface {
	Render(w http.ResponseWriter, name string, data any) error
}

type landingView struct {
	Service   string
	Version   string
	Artifacts []artifactRow
}

type artifactRow struct {
	ID            string `json:"id"`
	Filename      string `json:"filename"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	Visibility    string `json:"visibility"`
	Size          string `json:"size"`
	SizeBytes     int64  `json:"sizeBytes"`
	CreatedBy     string `json:"createdBy"`
	CreatedAt     string `json:"createdAt"`
	CreatedAtSort string `json:"createdAtSort"`
	Downloads     int64  `json:"downloads"`
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
	var svc *artifactdata.Service
	return appkit.Spec{
		App:        "artifacts",
		Mount:      "/srv/artifacts/",
		Port:       registry.MustPort("artifacts"),
		MCP:        true,
		WWW:        true,
		Feed:       "/feed",
		Migrations: db.FS,
		Events:     artifactdata.Events,
		Config:     loadConfig,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "ARTIFACTS_MAX_UPLOAD_BYTES", Value: "209715200"},
		},
		Producer: func(ob *outbox.Outbox) error {
			if svc == nil {
				return fmt.Errorf("artifacts: Producer called before service available")
			}
			svc.Outbox = artifactdata.NewOutboxProducer(ob)
			return nil
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
			baseURL := strings.TrimSuffix(rt.ResourceID(), "mcp")
			svc = artifactdata.NewService(
				db.NewStore(rt.DB(), artifactdata.NewToken),
				&artifactdata.BlobStore{Root: filepath.Join(root, "artifacts", "state")},
				time.Now,
				loaded.(config).MaxUploadBytes,
				baseURL,
			)
			handler, err := mcp.New(svc, rt.Version())
			if err != nil {
				return err
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(handler))
			rt.Handle("/u/", svc.UploadHandler())
			svc.MountDownloads(rt)
			svc.MountContent(rt)
			rt.Handle("GET /{$}", landingHandler(svc.Store, rt.WWW(), rt.Service(), rt.Version()))
			return nil
		},
	}
}

// landingHandler renders the current inventory through the chassis-loaded site.
func landingHandler(store *db.Store, renderer landingRenderer, service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stored, err := store.ListArtifacts(r.Context())
		if err != nil {
			http.Error(w, "load artifact inventory", http.StatusInternalServerError)
			return
		}
		rows := make([]artifactRow, 0, len(stored))
		for _, item := range stored {
			prefix := "p"
			if item.Visibility == "public" {
				prefix = "f"
			}
			created := item.CreatedAt.UTC()
			rows = append(rows, artifactRow{
				ID:            item.ID,
				Filename:      item.Filename,
				Description:   item.Description,
				URL:           downloadMount + prefix + "/" + url.PathEscape(item.ID) + "/" + url.PathEscape(item.Filename),
				Visibility:    item.Visibility,
				Size:          humanize.Bytes(uint64(item.Size)),
				SizeBytes:     item.Size,
				CreatedBy:     item.OwnerEmail,
				CreatedAt:     created.Format("2 Jan 2006, 15:04 UTC"),
				CreatedAtSort: created.Format(time.RFC3339),
				Downloads:     item.DownloadCount,
			})
		}

		buffered := newBufferedResponse()
		if err := renderer.Render(buffered, "landing.html", landingView{Service: service, Version: version, Artifacts: rows}); err != nil {
			http.Error(w, "render landing page", http.StatusInternalServerError)
			return
		}
		for name, values := range buffered.header {
			w.Header()[name] = append([]string(nil), values...)
		}
		status := buffered.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = buffered.body.WriteTo(w)
	})
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func main() {
	if _, err := loadConfig(os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "artifacts: %v\n", err)
		os.Exit(1)
	}
	appkit.Main(artifactsSpec())
}
