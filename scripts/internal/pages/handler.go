package pages

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/urls"
)

// Handler answers the catalog, about, tools, script and run page paths.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		banner := func(trail []page.Level) page.Banner {
			b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, cfg.ServicesPath), LogoutURL: urls.AuthLogout(r, cfg.ServicesPath)})
			b.Trail = trail
			return b
		}
		missing := func() {
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NoticeData{Banner: cfg.Banner(page.User{})})
		}
		refusal := func() {
			cfg.Pages.Write(w, r, http.StatusServiceUnavailable, "unavailable", NoticeData{Banner: cfg.Banner(page.User{})})
		}
		redirect := func(path string) {
			if r.URL.RawQuery != "" {
				path += "?" + r.URL.RawQuery
			}
			w.Header().Set("Location", path)
			w.WriteHeader(http.StatusMovedPermanently)
		}
		ctx := r.Context()
		caller, _ := identity.FromContext(ctx)
		owner := caller.UserID
		if r.URL.Path == "/about" {
			cfg.Pages.Write(w, r, 200, "about", AboutData{banner([]page.Level{{Name: "about", URL: "/about"}}), Description})
			return
		}
		if r.URL.Path == "/tools" {
			d := ToolsData{Banner: banner([]page.Level{{Name: "tools", URL: "/tools"}})}
			for _, tool := range cfg.MCP.Tools() {
				d.Tools = append(d.Tools, Tool{Name: tool.Name, Description: tool.Description})
			}
			cfg.Pages.Write(w, r, 200, "tools", d)
			return
		}
		if r.URL.Path == "/" {
			scripts, err := cfg.Store.List(ctx, owner)
			if err != nil {
				refusal()
				return
			}
			d := LandingData{Banner: banner(nil)}
			for _, sc := range scripts {
				row := ScriptRow{Name: sc.Name, URL: "/" + sc.Name + "/", Repo: repository(ctx, cfg, sc), Ref: sc.Ref}
				if sc.Last != nil {
					v := runRow(*sc.Last, sc.Name)
					row.LastRun = &v
				}
				d.Scripts = append(d.Scripts, row)
			}
			cfg.Pages.Write(w, r, 200, "landing", d)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
		for i, p := range parts {
			if v, err := url.PathUnescape(p); err == nil {
				parts[i] = v
			}
		}
		sc, err := cfg.Store.Find(ctx, owner, parts[0])
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				missing()
			} else {
				refusal()
			}
			return
		}
		if len(parts) == 1 {
			redirect("/" + sc.Name + "/")
			return
		}
		if len(parts) == 2 && parts[1] == "" {
			rs, err := cfg.Store.Runs(ctx, sc.ID)
			if err != nil {
				refusal()
				return
			}
			d := ScriptData{Banner: banner([]page.Level{{Name: sc.Name, URL: "/" + sc.Name + "/"}}), Script: ScriptCard{ID: sc.ID, Name: sc.Name, Repo: repository(ctx, cfg, sc), Ref: sc.Ref, Created: sc.Created.UTC().Format(CardLayout), CreatedAt: datetime(sc.Created), RunsKept: len(rs), KeepNewest: cfg.KeepCount, KeepDays: cfg.KeepDays}}
			for _, u := range rs {
				d.Runs = append(d.Runs, runRow(u, sc.Name))
			}
			for _, sub := range sc.Subscriptions {
				d.Subscriptions = append(d.Subscriptions, sub.Event)
			}
			cfg.Pages.Write(w, r, 200, "script", d)
			return
		}
		if (len(parts) == 3 || len(parts) == 4 && parts[3] == "") && parts[1] == "runs" && parts[2] != "" {
			u, err := cfg.Store.FindRun(ctx, owner, parts[2])
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					missing()
				} else {
					refusal()
				}
				return
			}
			if u.Script != sc.ID {
				missing()
				return
			}
			if len(parts) == 3 {
				redirect("/" + sc.Name + "/runs/" + u.ID + "/")
				return
			}
			cfg.Pages.Write(w, r, 200, "run", runData(ctx, cfg, sc, u, banner([]page.Level{{Name: sc.Name, URL: "/" + sc.Name + "/"}, {Name: u.ID, URL: "/" + sc.Name + "/runs/" + u.ID + "/"}})))
			return
		}
		missing()
	})
}
