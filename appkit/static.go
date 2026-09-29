package appkit

import "net/http"

// StaticPrefix is the URL prefix for the shared browser assets.
const StaticPrefix = "/_appkit/"

// Static returns a handler for the shared stylesheet, script, fonts, and licences.
func Static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		data, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			http.Error(w, "asset unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			// ResponseWriter reports connection failures to the HTTP server;
			// there is no additional response to send after writing has begun.
			_, _ = w.Write(data)
		}
	})
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
