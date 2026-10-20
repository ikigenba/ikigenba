// Package links records visits and sends visitors to their destination.
package links

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

// Config supplies the visit store, trail and fallback destination.
type Config struct {
	Store              *store.Store
	Telemetry          *telemetry.Writer
	DefaultDestination string
}

// Handler returns the identity-independent tracking route.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destination := cfg.DefaultDestination
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			token := strings.TrimPrefix(r.URL.Path, "/t/")
			if strings.HasPrefix(r.URL.Path, "/t/") && tokenShape(token) {
				ref := r.URL.Query().Get("r")
				if ref == "" {
					ref = r.Header.Get("Referer")
				}
				v, err := cfg.Store.Visit(r.Context(), strings.ToLower(token), store.VisitInput{UserAgent: r.Header.Get("User-Agent"), Referrer: ref, Address: r.Header.Get("X-Real-IP")})
				if err == nil {
					destination = v.Destination
					trail.Visited(r.Context(), cfg.Telemetry, v.Campaign, v.Piece)
				}
			}
		}
		w.Header().Set("Location", destination)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusFound)
	})
}

func tokenShape(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}
