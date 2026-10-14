package pages

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
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
			p := r.URL.Query().Get("page")
			var key string
			mode, valid := decodePage(p, "list", &key)
			if !valid {
				missing()
				return
			}
			var scripts []store.Script
			var err error
			if mode == pageBefore || mode == pageLast {
				scripts, err = cfg.Store.ListBefore(ctx, owner, key, paging.DefaultLimit+1)
			} else {
				scripts, err = cfg.Store.List(ctx, owner, key, paging.DefaultLimit+1)
			}
			if err != nil {
				refusal()
				return
			}
			if (mode == pageBefore || mode == pageLast) && len(scripts) <= paging.DefaultLimit {
				w.Header().Set("Location", "/")
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			more := len(scripts) > paging.DefaultLimit
			if more {
				scripts = scripts[:paging.DefaultLimit]
			}
			if mode == pageBefore || mode == pageLast {
				slices.Reverse(scripts)
			}
			d := LandingData{Banner: banner(nil), Page: p}
			if len(scripts) > 0 {
				if mode == pageBefore || (mode != pageLast && more) {
					d.Next = (paging.Token{Tool: "list"}).Encode(scripts[len(scripts)-1].Name)
				}
				if mode != pageFirst {
					d.Prev = (paging.Token{Tool: "list-before"}).Encode(scripts[0].Name)
				}
			}
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
		var p, started, id string
		var mode pageMode
		if len(parts) == 2 && parts[0] != "" && parts[1] == "" {
			p = r.URL.Query().Get("page")
			var valid bool
			mode, valid = decodePage(p, "runs", &started, &id)
			if !valid {
				missing()
				return
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
			var rs []store.Run
			if mode == pageBefore || mode == pageLast {
				rs, err = cfg.Store.RunsBefore(ctx, sc.ID, started, id, paging.DefaultLimit+1)
			} else {
				rs, err = cfg.Store.Runs(ctx, sc.ID, started, id, paging.DefaultLimit+1)
			}
			if err != nil {
				refusal()
				return
			}
			if (mode == pageBefore || mode == pageLast) && len(rs) <= paging.DefaultLimit {
				w.Header().Set("Location", "/"+sc.Name+"/")
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			count, err := cfg.Store.RunCount(ctx, sc.ID)
			if err != nil {
				refusal()
				return
			}
			d := ScriptData{Page: p, Banner: banner([]page.Level{{Name: sc.Name, URL: "/" + sc.Name + "/"}}), Script: ScriptCard{ID: sc.ID, Name: sc.Name, Repo: repository(ctx, cfg, sc), Ref: sc.Ref, Created: sc.Created.UTC().Format(CardLayout), CreatedAt: datetime(sc.Created), RunsKept: count, KeepNewest: cfg.KeepCount, KeepDays: cfg.KeepDays}}
			more := len(rs) > paging.DefaultLimit
			if more {
				rs = rs[:paging.DefaultLimit]
			}
			if mode == pageBefore || mode == pageLast {
				slices.Reverse(rs)
			}
			if len(rs) > 0 {
				if mode == pageBefore || (mode != pageLast && more) {
					last := rs[len(rs)-1]
					d.Next = (paging.Token{Tool: "runs"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
				}
				if mode != pageFirst {
					first := rs[0]
					d.Prev = (paging.Token{Tool: "runs-before"}).Encode(first.Started.UTC().Format(time.RFC3339), first.ID)
				}
			}
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

type pageMode uint8

const (
	pageFirst pageMode = iota
	pageAfter
	pageBefore
	pageLast
)

func decodePage(p, tool string, keys ...*string) (pageMode, bool) {
	if p == "" {
		return pageFirst, true
	}
	values := make([]any, len(keys))
	for i, key := range keys {
		values[i] = key
	}
	if (paging.Token{Tool: tool}).Decode(p, values...) == nil {
		return pageAfter, true
	}
	if (paging.Token{Tool: tool + "-before"}).Decode(p, values...) == nil {
		return pageBefore, true
	}
	if p == "last" {
		for _, key := range keys {
			*key = ""
		}
		return pageLast, true
	}
	return pageFirst, false
}
