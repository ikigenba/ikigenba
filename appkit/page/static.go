package page

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// StaticPrefix is the URL prefix for the shared browser assets.
const StaticPrefix = "/_appkit/"

// PreloadURL returns the immutable URL of the upright Inter font.
func PreloadURL() string {
	return preloadPath
}

// Static returns a handler for the shared stylesheet, scripts, favicon, fonts, and licences.
func Static() http.Handler {
	return staticHandler{}
}

type browserAsset struct {
	data         []byte
	contentType  string
	etag         string
	cacheControl string
}

var servedAssets, preloadPath = prepareAssets()

func prepareAssets() (map[string]browserAsset, string) {
	files := map[string]string{
		"theme.css":                  "text/css; charset=utf-8",
		"feedback.js":                "text/javascript; charset=utf-8",
		"favicon.svg":                "image/svg+xml",
		"InterVariable.woff2":        "font/woff2",
		"InterVariable-Italic.woff2": "font/woff2",
		"JetBrainsMono.woff2":        "font/woff2",
		"OFL.txt":                    "text/plain; charset=utf-8",
		"TABLER-LICENSE.txt":         "text/plain; charset=utf-8",
	}
	assets := make(map[string]browserAsset, len(files))
	var replacements []string
	var preload string
	for name, contentType := range files {
		data, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			panic(err)
		}
		cacheControl := "no-cache"
		servedName := name
		if contentType == "font/woff2" {
			digest := sha256.Sum256(data)
			servedName = strings.TrimSuffix(name, ".woff2") + "." + hex.EncodeToString(digest[:8]) + ".woff2"
			cacheControl = "public, max-age=31536000, immutable"
			replacements = append(replacements, `url("`+name+`")`, `url("`+servedName+`")`)
			if name == "InterVariable.woff2" {
				preload = StaticPrefix + servedName
			}
		}
		assets[StaticPrefix+servedName] = browserAsset{data: data, contentType: contentType, cacheControl: cacheControl}
	}
	stylesheet := assets[StaticPrefix+"theme.css"]
	stylesheet.data = []byte(strings.NewReplacer(replacements...).Replace(string(stylesheet.data)))
	assets[StaticPrefix+"theme.css"] = stylesheet
	for path, asset := range assets {
		digest := sha256.Sum256(asset.data)
		asset.etag = `"` + hex.EncodeToString(digest[:]) + `"`
		assets[path] = asset
	}
	return assets, preload
}

type staticHandler struct{}

func (staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	asset, found := servedAssets[r.URL.Path]
	if !found {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	w.Header().Set("ETag", asset.etag)
	w.Header().Set("Cache-Control", asset.cacheControl)
	if values := r.Header.Values("If-None-Match"); len(values) == 1 && matchesEntityTag(values[0], asset.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		// ResponseWriter reports connection failures to the HTTP server;
		// there is no additional response to send after writing has begun.
		_, _ = w.Write(asset.data)
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
