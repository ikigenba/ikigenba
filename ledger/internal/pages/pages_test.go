package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

type fixture struct {
	d   *db.DB
	st  *store.Store
	set *Set
	cfg Config
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	now := func() time.Time { return time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC) }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "book.db"), Migrations: ledger.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	f := &fixture{d: d}
	f.st = store.New(d, store.Config{Now: now, Rand: &sequenceReader{}})
	f.set, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	w := telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 20000)), Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	f.cfg = Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Service: ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true}
	}, Pages: f.set, Store: f.st, MCP: mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Telemetry: w})}
	return f
}
func request(method, target, user string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "ledger.example.test:443"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-User-Email", "chosen@example.test")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	return r
}
func answer(f *fixture, method, target, user string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Optional(Handler(f.cfg)).ServeHTTP(w, request(method, target, user))
	return w
}
func banner(f *fixture, target string, trail []page.Level) page.Banner {
	r := request("GET", target, "viewer")
	b := f.cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: Auth(r, f.cfg.ServicesPath) + "/", LogoutURL: Auth(r, f.cfg.ServicesPath) + "/logout"})
	b.Trail = trail
	return b
}
func expected(f *fixture, method, target, name string, status int, data any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.set.Write(w, request(method, target, "viewer"), status, name, data)
	return w
}
func equalResponse(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("response mismatch: %d %v %q; want %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-NWQ8-8SO7 R-PXKA-0ZO3 R-Q002-SJ5H R-Q17Z-6AW6 R-Q4VO-BM49 R-Q63K-PDUY R-Q7BH-35LN R-Q8JD-GXCC R-Q9R9-UP31 R-QH2O-5BJ7 R-QIAK-J39W
func TestTemplateSet(t *testing.T) {
	t.Chdir(t.TempDir())
	f := setup(t)
	if ServiceName != "ledger" || ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("public constants")
	}
	independent, err := page.Templates().ParseFS(ledger.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, full := range []bool{false, true} {
		b := banner(f, "/", nil)
		date, release := "", ""
		count := 0
		rows := []ToolRow{}
		if full {
			date = "2026-10-01"
			release = "a-release"
			count = 7
			rows = []ToolRow{{Name: "chosen-tool", Description: "chosen-description"}}
		}
		b.Release = release
		b.Commit = release
		for name, data := range map[string]any{"landing": LandingData{Banner: b, Currency: "USD", LockDate: date, FiscalYearStart: "04-01", OpenAccounts: count, Transactions: count, LatestDate: date, FailingAssertions: count}, "tools": ToolsData{Banner: b, Tools: rows}, "about": AboutData{Banner: b, Description: Description}, "notfound": NotFoundData{Banner: b}} {
			var buf bytes.Buffer
			if err := independent.ExecuteTemplate(&buf, name, data); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"GET", "HEAD"} {
				w := expected(f, method, "/", name, 201, data)
				want := buf.String()
				if method == "HEAD" {
					want = ""
				}
				if w.Code != 201 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != want {
					t.Fatal(name, w)
				}
			}
		}
	}
	for _, name := range []string{"absent", "landing"} {
		w := expected(f, "GET", "/", name, 202, struct{}{})
		if w.Code != 500 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatal(w)
		}
	}
}

// R-QAZ6-8GTQ R-QC72-M8KF R-QDEZ-00B4 R-QEMV-DS1T
func TestAddresses(t *testing.T) {
	for _, c := range []struct{ host, proto, want string }{{"ledger.sbx.example.test:443", "https", "https://auth.sbx.example.test"}, {"LEDGER.sbx.example.test", "HTTP", "http://auth.sbx.example.test"}, {"ledger.example.test:abc", "", "https://auth.example.test:abc"}, {"example.test:80", "http", "http://auth.example.test"}} {
		r := request("GET", "/about?x=1", "")
		r.Host = c.host
		r.Header.Set("X-Forwarded-Proto", c.proto)
		if Auth(r, "") != c.want {
			t.Fatal(Auth(r, ""), c.want)
		}
		scheme := "https"
		if strings.EqualFold(c.proto, "http") {
			scheme = "http"
		}
		want := c.want + "/?return=" + url.QueryEscape(scheme+"://"+r.Host+r.RequestURI)
		if SignIn(r, "") != want {
			t.Fatal(SignIn(r, ""), want)
		}
	}
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"auth","url":"https://login.example.test///","description":"chosen","socket":"/tmp/none","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := request("GET", "/tools?x=1", "")
	if Auth(r, path) != "https://login.example.test" {
		t.Fatal(Auth(r, path))
	}
	if SignIn(r, path) != "https://login.example.test/?return="+url.QueryEscape("https://"+r.Host+r.RequestURI) {
		t.Fatal(SignIn(r, path))
	}
}

// R-Q2FV-K2MV R-Q3NR-XUDK R-YZ7L-QK9F R-QKQD-AMRA R-QLY9-OEHZ R-Z0FI-4C04 R-QOE2-FXZD R-QPLY-TPQ2 R-QT9N-Z0Y5 R-QVPG-QKFJ R-QWXD-4C68 R-QZD5-VVNM
func TestRoutingAndFailures(t *testing.T) {
	f := setup(t)
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, path := range []string{"/", "/?page=last", "/tools?x=1", "/about", "/nope", "/tools/", "/%74ools", "/TOOLS", "//"} {
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
				guest := answer(f, method, path, "")
				if guest.Code != 302 || guest.Body.Len() != 0 || !reflect.DeepEqual(guest.Header().Values("Location"), []string{SignIn(request(method, path, ""), f.cfg.ServicesPath)}) {
					t.Fatal(guest)
				}
				r := request(method, path, "viewer")
				escaped := r.URL.EscapedPath()
				isPage := escaped == "/" || escaped == "/tools" || escaped == "/about"
				got := answer(f, method, path, "viewer")
				switch {
				case !isPage:
					equalResponse(t, got, expected(f, method, path, "notfound", 404, NotFoundData{Banner: banner(f, path, nil)}))
				case method != "GET" && method != "HEAD":
					if got.Code != 405 || got.Body.Len() != 0 || !reflect.DeepEqual(got.Header().Values("Allow"), []string{"GET, HEAD"}) {
						t.Fatal(got)
					}
				case escaped == "/tools":
					equalResponse(t, got, expected(f, method, path, "tools", 200, ToolsData{Banner: banner(f, path, []page.Level{ToolsLevel}), Tools: []ToolRow{}}))
				case escaped == "/about":
					equalResponse(t, got, expected(f, method, path, "about", 200, AboutData{Banner: banner(f, path, []page.Level{AboutLevel}), Description: Description}))
				case escaped == "/" && failing:
					want := store.Unreachable + "\n"
					if method == "HEAD" {
						want = ""
					}
					if got.Code != 503 || got.Body.String() != want || !reflect.DeepEqual(got.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
						t.Fatal(got)
					}
				}
				if got.Header().Get("Set-Cookie") != "" {
					t.Fatal("cookie")
				}
				other := answer(f, method, path, "other-user")
				equalResponse(t, got, other)
				if method == "HEAD" {
					get := answer(f, "GET", path, "viewer")
					if got.Code != get.Code || !reflect.DeepEqual(got.Header(), get.Header()) || got.Body.Len() != 0 {
						t.Fatal("HEAD differs")
					}
				}
			}
		}
	}
	f.d.SetFailing(false)
}

// R-Z1NE-I3QT
func TestLandingState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	check := func() {
		t.Helper()
		s, err := f.st.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"/", "/?page=bogus"} {
			equalResponse(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "landing", 200, LandingData{Banner: banner(f, target, nil), Currency: s.Book.Currency, LockDate: s.Book.LockDate, FiscalYearStart: s.Book.FiscalYearStart, OpenAccounts: s.OpenAccounts, Transactions: s.Transactions, LatestDate: s.LatestDate, FailingAssertions: s.FailingAssertions}))
		}
	}
	check()
	lock, fiscal := "2026-09-30", "04-01"
	if _, _, err := f.st.SetBook(ctx, store.BookEdit{LockDate: &lock, FiscalYearStart: &fiscal, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Assets:Bank", "Expenses:Office", "Assets:Spare", "Equity:Opening"} {
		if _, _, err := f.st.OpenAccount(ctx, store.AccountInput{Name: name, Date: "2026-10-01", UserID: "opener"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.st.CloseAccount(ctx, store.AccountInput{Name: "Assets:Spare", Date: "2026-10-02", UserID: "closer"}); err != nil {
		t.Fatal(err)
	}
	a := "1.00"
	tx, _, err := f.st.Post(ctx, store.PostInput{Date: "2026-10-01", Postings: []store.PostingInput{{Account: "Assets:Bank", Amount: &a}, {Account: "Expenses:Office"}}, UserID: "poster"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.Reverse(ctx, store.ReversalInput{Transaction: tx.ID, Date: "2026-10-05", UserID: "reverser"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.AssertBalance(ctx, store.AssertionInput{Account: "Assets:Bank", Date: "2026-10-06", Amount: "8.00", UserID: "assertor"}); err != nil {
		t.Fatal(err)
	}
	check()
	s, err := f.st.Status(ctx)
	if err != nil || s.OpenAccounts != 3 || s.Transactions != 2 || s.LatestDate != "2026-10-05" || s.FailingAssertions != 1 {
		t.Fatal(s, err)
	}
}

// R-QUHK-CSOU
func TestToolsPage(t *testing.T) {
	f := setup(t)
	equalResponse(t, answer(f, "GET", "/tools", "viewer"), expected(f, "GET", "/tools", "tools", 200, ToolsData{Banner: banner(f, "/tools", []page.Level{ToolsLevel}), Tools: []ToolRow{}}))
	tools.Register(f.cfg.MCP, tools.Config{Store: f.st})
	rows := make([]ToolRow, 0)
	catalog := tools.Catalog()
	for i, tv := range f.cfg.MCP.Tools() {
		if tv.Name != catalog[i].Name {
			t.Fatal("catalog order")
		}
		rows = append(rows, ToolRow{Name: tv.Name, Description: tools.FirstLine(tv.Description)})
	}
	mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: "chosen_extra", Description: "Reads chosen.\nSecond line.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	rows = append(rows, ToolRow{Name: "chosen_extra", Description: "Reads chosen."})
	equalResponse(t, answer(f, "GET", "/tools?x=1", "viewer"), expected(f, "GET", "/tools?x=1", "tools", 200, ToolsData{Banner: banner(f, "/tools?x=1", []page.Level{ToolsLevel}), Tools: rows}))
}

// R-Z2VA-VVHI
func TestNoMutationOrSiblingCalls(t *testing.T) {
	f := setup(t)
	var calls atomic.Int64
	dir, err := os.MkdirTemp("", "ledger-pages-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	entries := make([]map[string]any, 0)
	for _, name := range []string{"artifacts", "other"} {
		socket := filepath.Join(dir, name+".sock")
		ln, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{ReadHeaderTimeout: time.Second, ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateNew {
				calls.Add(1)
			}
		}, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })
		entries = append(entries, map[string]any{"name": name, "url": "https://" + name + ".example.test", "socket": socket, "description": "chosen", "enabled": true, "mcp": false})
	}
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "services.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	f.cfg.ServicesPath = path
	t.Setenv("IKIGENBA_SERVICES", path)
	ctx := context.Background()
	fiscal := "04-01"
	if _, _, err := f.st.SetBook(ctx, store.BookEdit{FiscalYearStart: &fiscal, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Assets:Bank", "Expenses:Office"} {
		if _, _, err := f.st.OpenAccount(ctx, store.AccountInput{Name: name, Date: "2026-10-01", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	a := "12.00"
	if _, _, err := f.st.Post(ctx, store.PostInput{Date: "2026-10-02", Postings: []store.PostingInput{{Account: "Assets:Bank", Amount: &a}, {Account: "Expenses:Office"}}, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	status, err := f.st.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := f.st.Accounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	txs, err := f.st.ListTransactions(context.Background(), store.JournalFilter{}, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path, user string }{{"GET", "/", "user"}, {"GET", "/tools", "user"}, {"GET", "/about", "user"}, {"GET", "/missing", "user"}, {"POST", "/", "user"}, {"GET", "/", ""}} {
		got := answer(f, c.method, c.path, c.user)
		if got.Header().Get("Set-Cookie") != "" {
			t.Fatal("cookie")
		}
	}
	afterStatus, err := f.st.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterAccounts, err := f.st.Accounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterTxs, err := f.st.ListTransactions(context.Background(), store.JournalFilter{}, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(status, afterStatus) || !reflect.DeepEqual(accounts, afterAccounts) || !reflect.DeepEqual(txs, afterTxs) || calls.Load() != 0 {
		t.Fatal("page changed state or called sibling")
	}
}

type sequenceReader struct{ n byte }

func (r *sequenceReader) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

// R-Q2FV-K2MV R-Q4VO-BM49 R-Q63K-PDUY R-Q7BH-35LN R-Q8JD-GXCC R-Q9R9-UP31
func TestDataShapes(t *testing.T) {
	f := setup(t)
	b := banner(f, "/", nil)
	cfg := Config{f.cfg.Banner, f.set, "", f.st, f.cfg.MCP}
	if cfg.Store != f.st {
		t.Fatal("config")
	}
	row := ToolRow{"chosen-name", "chosen-summary"}
	for name, data := range map[string]any{"landing": LandingData{b, "USD", "", "01-01", 0, 0, "", 0}, "tools": ToolsData{b, []ToolRow{row}}, "about": AboutData{b, Description}, "notfound": NotFoundData{b}} {
		w := expected(f, "GET", "/", name, 200, data)
		if w.Code != 200 {
			t.Fatal(name, w.Code)
		}
	}
}
