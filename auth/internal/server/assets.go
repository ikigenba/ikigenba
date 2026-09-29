package server

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/auth"
)

func assetContentType(name string) string {
	switch path.Ext(name) {
	case ".css":
		return "text/css; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	entries, err := auth.Assets.ReadDir("assets")
	var asset fs.DirEntry
	if name != "" && !strings.Contains(name, "/") && err == nil {
		for _, entry := range entries {
			if entry.Name() == name && entry.Type().IsRegular() {
				asset = entry
				break
			}
		}
	}
	if asset == nil {
		assetError(w, r, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		assetError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, err := auth.Assets.ReadFile("assets/" + asset.Name())
	if err != nil {
		assetError(w, r, http.StatusNotFound, "not found")
		return
	}
	tag := fmt.Sprintf("\"%x\"", sha256.Sum256(body))
	w.Header().Set("Content-Type", assetContentType(name))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "no-cache")
	for _, entry := range strings.Split(strings.Join(r.Header.Values("If-None-Match"), ","), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "*" || entry == tag || entry == "W/"+tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func assetError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Del("ETag")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = fmt.Fprintln(w, message)
	}
}
