package pages

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sessions/internal/figures"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// Handler serves read-only pages to callers carried by identity.Optional.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		if caller.UserID == "" {
			w.Header().Set("Location", SignIn(r, cfg.ServicesPath))
			w.WriteHeader(http.StatusFound)
			return
		}
		path := r.URL.EscapedPath()
		auth := Auth(r, cfg.ServicesPath)
		banner := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		banner.Trail = nil
		if path != "/" && path != "/tools" && path != "/about" {
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: banner})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch path {
		case "/":
			landing, err := cfg.Store.Landing(r.Context())
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				if r.Method != http.MethodHead {
					_, _ = fmt.Fprintln(w, store.Unreachable)
				}
				return
			}
			weeks := make([]Week, 0, len(landing.Weeks))
			for _, week := range landing.Weeks {
				weeks = append(weeks, presentWeek(week))
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", LandingData{Banner: banner, AwayLimit: duration(landing.AwayLimit), Weeks: weeks})
		case "/tools":
			banner.Trail = []page.Level{ToolsLevel}
			rows := make([]ToolRow, 0)
			for _, tool := range cfg.MCP.Tools() {
				rows = append(rows, ToolRow{Name: tool.Name, Description: tools.FirstLine(tool.Description)})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", ToolsData{Banner: banner, Tools: rows})
		case "/about":
			banner.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: banner, Description: Description})
		}
	})
}

func duration(d time.Duration) Duration {
	s := int(figures.Seconds(d))
	return Duration{Seconds: s, Hours: s / 3600, Minutes: (s % 3600) / 60}
}

func grouped(n int64) string {
	text := strconv.FormatInt(n, 10)
	var out strings.Builder
	for i, c := range text {
		if i > 0 && (len(text)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(c)
	}
	return out.String()
}

func row(kind string, f figures.Figures) FiguresRow {
	return FiguresRow{Kind: kind, Attention: duration(f.Attention), Wall: duration(f.Wall), Agent: duration(f.Agent), Tokens: grouped(f.Tokens.Total()), Dollars: f.Dollars.Dollars()}
}

func share(w store.Week) int {
	total := figures.Seconds(w.Total.Attention)
	if total == 0 {
		return 0
	}
	for _, k := range w.Kinds {
		if k.Kind == figures.KindUntagged {
			return int((100*figures.Seconds(k.Figures.Attention) + total/2) / total)
		}
	}
	return 0
}

func presentWeek(w store.Week) Week {
	kinds := make([]FiguresRow, 0, len(w.Kinds))
	for _, kind := range w.Kinds {
		kinds = append(kinds, row(string(kind.Kind), kind.Figures))
	}
	return Week{Start: w.Start.UTC().Format("2006-01-02"), Kinds: kinds, Total: row("", w.Total), UntaggedShare: share(w)}
}
