// Package links records visits and sends visitors to their destination.
package links

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

// Config supplies the visit store, trail and fallback destination.
type Config struct {
	Store              *store.Store
	Telemetry          *telemetry.Writer
	Events             *events.Emitter
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
					if v.Opened {
						trail.MemberOpened(r.Context(), cfg.Telemetry, cfg.Events, v.Campaign, v.Candidate, v.First)
					}
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

// SweepBatchSize bounds each page's queued trail and bus events.
const SweepBatchSize = 500

// StartSweep asynchronously tells first opens left unmarked by earlier releases.
func StartSweep(ctx context.Context, cfg Config, stderr io.Writer) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		sweep(ctx, cfg, stderr)
	}()
	return done
}

func sweep(ctx context.Context, cfg Config, stderr io.Writer) {
	sctx := identity.NewContext(context.WithoutCancel(ctx), identity.Caller{})
	after := store.MemberKey{}
	told := false
	for {
		if ctx.Err() != nil {
			return
		}
		next, opened, err := cfg.Store.MarkOpened(ctx, after, SweepBatchSize)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if stderr != nil {
				_, _ = stderr.Write([]byte("campaigns: opened events failed\n\n" + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"))
			}
			return
		}
		for _, member := range opened {
			if ctx.Err() != nil {
				return
			}
			trail.MemberOpened(sctx, cfg.Telemetry, cfg.Events, member.Campaign, member.Candidate, member.First)
			told = true
		}
		if cfg.Telemetry != nil {
			_ = cfg.Telemetry.Flush(ctx)
		}
		if cfg.Events != nil {
			_ = cfg.Events.Flush(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		if next == (store.MemberKey{}) {
			if told && stderr != nil {
				_, _ = stderr.Write([]byte("campaigns: opened events sent\n"))
			}
			return
		}
		after = next
	}
}
