package pages

import (
	"net/http"
	"net/url"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
)

// Handler answers browser requests without changing the host or the record.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := identity.FromContext(r.Context())
		if !ok || c.UserID == "" {
			w.Header().Set("Location", SignIn(r, cfg.ServicesPath))
			w.WriteHeader(http.StatusFound)
			return
		}
		auth := Auth(r, cfg.ServicesPath)
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		b.Trail = nil
		path := r.URL.EscapedPath()
		notFound := func() { cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: b}) }
		if path != "/" && path != "/tools" && path != "/about" {
			notFound()
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch path {
		case "/":
			p, q := r.URL.Query().Get("groups_page"), r.URL.Query().Get("users_page")
			gp, gok := decode(tools.ToolGroups, p)
			up, uok := decode(tools.ToolUsers, q)
			if !gok || !uok {
				notFound()
				return
			}
			gs, err := cfg.Host.GroupList(r.Context())
			if err != nil {
				refusal(w, r, host.HostFailed)
				return
			}
			us, err := cfg.Host.UserList(r.Context())
			if err != nil {
				refusal(w, r, host.HostFailed)
				return
			}
			gw := listWindow(gs, tools.ToolGroups, p, gp, func(g host.Group) string { return g.Slug })
			uw := listWindow(us, tools.ToolUsers, q, up, func(u host.User) string { return u.Name })
			if gw.underflow || uw.underflow {
				location := "/"
				if p != "" && !gw.underflow {
					location += "?groups_page=" + url.QueryEscape(p)
				}
				if q != "" && !uw.underflow {
					location += "?users_page=" + url.QueryEscape(q)
				}
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			slugs := make([]string, 0, gw.end-gw.start)
			for _, g := range gs[gw.start:gw.end] {
				slugs = append(slugs, g.Slug)
			}
			records, err := cfg.Store.Groups(r.Context(), slugs)
			if err != nil {
				refusal(w, r, store.Unreachable)
				return
			}
			groups := make([]GroupRow, 0, len(slugs))
			for _, g := range gs[gw.start:gw.end] {
				row := GroupRow{Slug: g.Slug, MemberCount: g.Members, Members: []string{}}
				for _, u := range us {
					for _, slug := range u.Groups {
						if slug == g.Slug {
							row.Members = append(row.Members, u.Name)
							break
						}
					}
				}
				if record, ok := records[g.Slug]; ok {
					row.Created, row.CreatedText = stamps(record.Created)
				}
				groups = append(groups, row)
			}
			users := make([]UserRow, 0, uw.end-uw.start)
			for _, u := range us[uw.start:uw.end] {
				row := UserRow{Login: u.Name, State: u.State, Groups: append([]string{}, u.Groups...), KeyCount: len(u.Keys)}
				if !u.LastSignIn.IsZero() {
					row.LastSignIn, row.LastSignInText = stamps(u.LastSignIn)
				}
				users = append(users, row)
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", LandingData{Banner: b, Groups: groups, GroupsPage: p, GroupsPrev: gw.prev, GroupsNext: gw.next, Users: users, UsersPage: q, UsersPrev: uw.prev, UsersNext: uw.next})
		case "/tools":
			b.Trail = []page.Level{ToolsLevel}
			rows := make([]ToolRow, 0)
			for _, t := range cfg.MCP.Tools() {
				rows = append(rows, ToolRow{Name: t.Name, Description: tools.FirstLine(t.Description)})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", ToolsData{Banner: b, Tools: rows})
		case "/about":
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: b, Description: Description})
		}
	})
}
func stamps(t time.Time) (string, string) {
	return t.UTC().Format(time.RFC3339), t.UTC().Format(MinuteLayout)
}
func refusal(w http.ResponseWriter, r *http.Request, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(line + "\n"))
	}
}
