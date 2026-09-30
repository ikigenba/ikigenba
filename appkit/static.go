package appkit

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// StaticPrefix is the URL prefix for the shared browser assets.
const StaticPrefix = "/_appkit/"

// Static returns a handler for the shared stylesheet, script, fonts, and licences.
func Static() http.Handler {
	return staticHandler{files: assetsFS}
}

type staticHandler struct {
	files fs.ReadFileFS
}

func (h staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, contentType := staticAsset(r.URL.Path)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := h.files.ReadFile("assets/" + name)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	digest := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if values := r.Header.Values("If-None-Match"); len(values) == 1 && matchesEntityTag(values[0], etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		// ResponseWriter reports connection failures to the HTTP server;
		// there is no additional response to send after writing has begun.
		_, _ = w.Write(data)
	}
}

func matchesEntityTag(value, etag string) bool {
	if value == "*" {
		return true
	}
	matched := false
	for value != "" {
		value = strings.TrimLeft(value, " \t")
		if value == "" {
			break
		}
		if value[0] == ',' {
			value = value[1:]
			continue
		}
		value = strings.TrimPrefix(value, "W/")
		if value == "" || value[0] != '"' {
			return false
		}
		end := 1
		for end < len(value) && value[end] != '"' {
			character := value[end]
			if character < 0x21 || character == 0x7f {
				return false
			}
			end++
		}
		if end == len(value) {
			return false
		}
		matched = matched || value[:end+1] == etag
		value = strings.TrimLeft(value[end+1:], " \t")
		if value != "" {
			if value[0] != ',' {
				return false
			}
			value = value[1:]
		}
	}
	return matched
}

func staticAsset(path string) (name, contentType string) {
	switch path {
	case StaticPrefix + "theme.css":
		return "theme.css", "text/css; charset=utf-8"
	case StaticPrefix + "launcher.js":
		return "launcher.js", "text/javascript; charset=utf-8"
	case StaticPrefix + "InterVariable.woff2":
		return "InterVariable.woff2", "font/woff2"
	case StaticPrefix + "InterVariable-Italic.woff2":
		return "InterVariable-Italic.woff2", "font/woff2"
	case StaticPrefix + "JetBrainsMono.woff2":
		return "JetBrainsMono.woff2", "font/woff2"
	case StaticPrefix + "OFL.txt":
		return "OFL.txt", "text/plain; charset=utf-8"
	case StaticPrefix + "TABLER-LICENSE.txt":
		return "TABLER-LICENSE.txt", "text/plain; charset=utf-8"
	default:
		return "", ""
	}
}
