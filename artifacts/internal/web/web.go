// Package web serves the embedded owner-facing artifact inventory.
package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"artifacts/internal/artifacts"
	"github.com/dustin/go-humanize"
)

const downloadMount = "/srv/artifacts/"

var (
	landingTemplate = template.Must(template.ParseFS(assets, "landing.html"))
	staticAssets    = mustSub(assets, "static")
)

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

// LandingHandler renders the current inventory directly from the service store.
func LandingHandler(svc *artifacts.Service, service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stored, err := svc.Store.ListArtifacts(r.Context())
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
				CreatedAtSort: created.Format("2006-01-02T15:04:05Z07:00"),
				Downloads:     item.DownloadCount,
			})
		}

		var body bytes.Buffer
		if err := landingTemplate.Execute(&body, landingView{Service: service, Version: version, Artifacts: rows}); err != nil {
			http.Error(w, "render landing page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = body.WriteTo(w)
	})
}

// StaticHandler serves the package's embedded controller, CSS, and fonts.
func StaticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := staticAssetName(r.URL.Path)
		if name == "" {
			http.NotFound(w, r)
			return
		}
		body, err := fs.ReadFile(staticAssets, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", staticContentType(name, body))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	})
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func staticAssetName(urlPath string) string {
	for _, segment := range strings.Split(urlPath, "/") {
		if segment == ".." {
			return ""
		}
	}
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	name = strings.TrimPrefix(name, "static/")
	if name == "." || name == "" || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return ""
	}
	return name
}

func staticContentType(name string, body []byte) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	default:
		return http.DetectContentType(body)
	}
}
