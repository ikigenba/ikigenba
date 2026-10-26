package pages

import (
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

// Handler answers browser requests without changing the book.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := identity.FromContext(r.Context())
		if !ok || c.UserID == "" {
			w.Header().Set("Location", SignIn(r, cfg.ServicesPath))
			w.WriteHeader(http.StatusFound)
			return
		}
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: Auth(r, cfg.ServicesPath) + "/", LogoutURL: Auth(r, cfg.ServicesPath) + "/logout"})
		b.Trail = nil
		path := r.URL.EscapedPath()
		if path != "/" && path != "/tools" && path != "/about" {
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: b})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch path {
		case "/":
			s, err := cfg.Store.Status(r.Context())
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				if r.Method != http.MethodHead {
					_, _ = w.Write([]byte(store.Unreachable + "\n"))
				}
				return
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", LandingData{Banner: b, Currency: s.Book.Currency, LockDate: s.Book.LockDate, FiscalYearStart: s.Book.FiscalYearStart, OpenAccounts: s.OpenAccounts, Transactions: s.Transactions, LatestDate: s.LatestDate, FailingAssertions: s.FailingAssertions})
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
