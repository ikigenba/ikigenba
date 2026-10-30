package pages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"unicode"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func signature[T any](value T) T { return value }

type fakeHost struct {
	groups            []host.Group
	users             []host.User
	groupErr, userErr error
	reads             []string
	mutations         []string
}

func (h *fakeHost) GroupList(context.Context) ([]host.Group, error) {
	h.reads = append(h.reads, "groups")
	return h.groups, h.groupErr
}
func (h *fakeHost) UserList(context.Context) ([]host.User, error) {
	h.reads = append(h.reads, "users")
	return h.users, h.userErr
}
func (h *fakeHost) GroupMembers(context.Context, string) (host.Members, error) {
	h.reads = append(h.reads, "members")
	return host.Members{}, nil
}
func (h *fakeHost) GroupAdd(context.Context, string) error {
	h.mutations = append(h.mutations, "GroupAdd")
	return nil
}
func (h *fakeHost) GroupRemove(context.Context, string) error {
	h.mutations = append(h.mutations, "GroupRemove")
	return nil
}
func (h *fakeHost) UserAdd(context.Context, string, string, string) error {
	h.mutations = append(h.mutations, "UserAdd")
	return nil
}
func (h *fakeHost) UserKeyAdd(context.Context, string, string) error {
	h.mutations = append(h.mutations, "UserKeyAdd")
	return nil
}
func (h *fakeHost) UserKeyRemove(context.Context, string, string) error {
	h.mutations = append(h.mutations, "UserKeyRemove")
	return nil
}
func (h *fakeHost) UserJoin(context.Context, string, string) error {
	h.mutations = append(h.mutations, "UserJoin")
	return nil
}
func (h *fakeHost) UserLeave(context.Context, string, string) error {
	h.mutations = append(h.mutations, "UserLeave")
	return nil
}
func (h *fakeHost) UserLock(context.Context, string) error {
	h.mutations = append(h.mutations, "UserLock")
	return nil
}
func (h *fakeHost) UserUnlock(context.Context, string) error {
	h.mutations = append(h.mutations, "UserUnlock")
	return nil
}
func (h *fakeHost) UserDelete(context.Context, string) error {
	h.mutations = append(h.mutations, "UserDelete")
	return nil
}

type fixture struct {
	cfg    pages.Config
	d      *db.DB
	host   *fakeHost
	writer *telemetry.Writer
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time {
		return time.Date(2026, 10, 29, 9, 3, 7, 900000000, time.FixedZone("chosen-zone", -5*3600))
	}
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "accounts.db"), Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 100000)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	h := &fakeHost{}
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true, Trail: []page.Level{{Name: "discarded", URL: "/discarded"}}}
	}
	cfg := pages.Config{banner, set, "", store.New(d, store.Config{Now: now}), h, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w})}
	return &fixture{cfg: cfg, d: d, host: h, writer: w}
}
func request(method, target, user string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader("chosen body"))
	r.Host = "accounts.example.test:8443"
	r.Header["X-User-Email"] = []string{"chosen@example.test", "ignored@example.test"}
	r.Header.Set("X-Forwarded-Proto", "https")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	return r
}
func answer(f *fixture, method, target, user string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Optional(pages.Handler(f.cfg)).ServeHTTP(w, request(method, target, user))
	return w
}
func banner(f *fixture, target string, trail []page.Level) page.Banner {
	r := request("GET", target, "viewer")
	a := pages.Auth(r, f.cfg.ServicesPath)
	b := f.cfg.Banner(page.User{Email: "chosen@example.test", ProfileURL: a + "/", LogoutURL: a + "/logout"})
	b.Trail = trail
	return b
}
func expected(f *fixture, method, target, name string, status int, data any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.cfg.Pages.Write(w, request(method, target, "viewer"), status, name, data)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %v %q; want %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-DMRY-9F8U R-A4XI-WIA1 R-A65F-AA0Q R-E484-61LZ
func TestConstants(t *testing.T) {
	const service = pages.ServiceName
	const description string = pages.Description
	const layout = pages.MinuteLayout
	if service != "accounts" || layout != "2006-01-02 15:04" || pages.ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || pages.AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("public constants")
	}
	if description == "" || !utf8.ValidString(description) || strings.ContainsAny(description, "\"\\\n") {
		t.Fatal("description cannot be embedded in manifest")
	}
	for _, r := range description {
		if unicode.IsControl(r) {
			t.Fatal("description contains control rune")
		}
	}
}

// R-A8L8-1TI4 R-A9T4-FL8T R-AEOP-YO7L R-AFWM-CFYA R-AH4I-Q7OZ R-AICF-3ZFO R-AJKB-HR6D R-AKS7-VIX2 R-AM04-9ANR R-AS3M-65D8 R-ATBI-JX3X
func TestTemplateSet(t *testing.T) {
	t.Chdir(t.TempDir())
	f := setup(t)
	independent, err := page.Templates().ParseFS(accounts.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	load := signature[func() (*pages.Set, error)](pages.Load)
	write := signature[func(*pages.Set, http.ResponseWriter, *http.Request, int, string, any)]((*pages.Set).Write)
	for _, full := range []bool{false, true} {
		set, err := load()
		if err != nil {
			t.Fatal(err)
		}
		b := banner(f, "/", nil)
		b.Release = ""
		b.Commit = ""
		groups := []pages.GroupRow{}
		users := []pages.UserRow{}
		toolRows := []pages.ToolRow{}
		token := ""
		if full {
			b.Release = "template-release"
			b.Commit = "template-commit"
			token = "chosen-token"
			groups = []pages.GroupRow{{"business", 7, []string{"person"}, "2026-10-29T14:03:07Z", "2026-10-29 14:03"}, {"other", 0, []string{}, "", ""}}
			users = []pages.UserRow{{"person", host.StateActive, []string{"business"}, 2, "2026-10-29T14:03:07Z", "2026-10-29 14:03"}, {"other", host.StateLocked, []string{}, 0, "", ""}}
			toolRows = []pages.ToolRow{{"chosen-tool", "chosen-description"}}
		}
		data := map[string]any{"landing": pages.LandingData{b, groups, token, token, token, users, token, token, token}, "tools": pages.ToolsData{b, toolRows}, "about": pages.AboutData{b, pages.Description}, "notfound": pages.NotFoundData{b}}
		for name, value := range data {
			var raw bytes.Buffer
			if err := independent.ExecuteTemplate(&raw, name, value); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"GET", "HEAD"} {
				w := httptest.NewRecorder()
				write(set, w, request(method, "/", "viewer"), 207, name, value)
				want := raw.String()
				if method == "HEAD" {
					want = ""
				}
				if w.Code != 207 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != want {
					t.Fatalf("template %s method %s", name, method)
				}
			}
		}
	}
	for _, tc := range []struct {
		name string
		data any
	}{{"missing", nil}, {"landing", struct{ Banner page.Banner }{}}} {
		w := httptest.NewRecorder()
		f.cfg.Pages.Write(w, request("GET", "/", "viewer"), 207, tc.name, tc.data)
		if w.Code != 500 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatal("template failure leaked partial output")
		}
	}
}

func servicesFile(t *testing.T, auth string, sockets ...string) string {
	t.Helper()
	entries := []map[string]any{}
	names := []string{"auth", "other"}
	for i, socket := range sockets {
		entries = append(entries, map[string]any{"name": names[i], "url": auth, "description": "chosen-description", "socket": socket, "enabled": true, "mcp": false})
	}
	if len(sockets) == 0 && auth != "" {
		entries = append(entries, map[string]any{"name": "auth", "url": auth, "description": "chosen-description", "socket": "", "enabled": true, "mcp": false})
	}
	raw, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// R-AN80-N2EG R-AOFX-0U55 R-APNT-ELVU R-AQVP-SDMJ
func TestAddresses(t *testing.T) {
	fpath := servicesFile(t, "https://chosen-auth.test///")
	empty := servicesFile(t, "")
	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(bad, []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, proto, path, want string }{{"accounts.example.test:443", "https", "", "https://auth.example.test"}, {"ACCOUNTS.example.test:123", "HTTP", empty, "http://auth.example.test"}, {"other.test:abc", "unknown", bad, "https://auth.other.test:abc"}, {"accounts.example.test:443", "http", fpath, "https://chosen-auth.test"}, {"example.test", "", filepath.Join(t.TempDir(), "missing-services.json"), "https://auth.example.test"}} {
		r := request("GET", "/about?x=one%20two&x=second", "viewer")
		r.Host = tc.host
		r.Header["X-Forwarded-Proto"] = []string{tc.proto, "https"}
		auth := signature[func(*http.Request, string) string](pages.Auth)
		signin := signature[func(*http.Request, string) string](pages.SignIn)
		if got := auth(r, tc.path); got != tc.want {
			t.Fatalf("auth %q want %q", got, tc.want)
		}
		scheme := "https"
		if strings.EqualFold(tc.proto, "http") {
			scheme = "http"
		}
		want := tc.want + "/?return=" + url.QueryEscape(scheme+"://"+tc.host+r.RequestURI)
		if got := signin(r, tc.path); got != want {
			t.Fatalf("sign-in %q want %q", got, want)
		}
	}
}

// R-AC8X-74Q7 R-ADGT-KWGW R-AUJE-XOUM R-AWZ7-P8C0 R-AY74-302P R-AZF0-GRTE R-B0MW-UJK3 R-B1UT-8BAS R-BF9P-FSGF
func TestDecisionOrder(t *testing.T) {
	f := setup(t)
	f.host.groupErr = &host.FailedError{Status: 1}
	f.d.SetFailing(true)
	handler := signature[func(pages.Config) http.Handler](pages.Handler)
	_ = handler(f.cfg)
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
		for _, target := range []string{"/", "/?groups_page=bogus", "/tools", "/about", "/nope", "/tools/", "/%74ools", "//", "/TOOLS", "/index.html", "/groups", "/users/alice"} {
			guest := answer(f, method, target, "")
			r := request(method, target, "")
			if guest.Code != 302 || !reflect.DeepEqual(guest.Header().Values("Location"), []string{pages.SignIn(r, f.cfg.ServicesPath)}) || guest.Body.Len() != 0 {
				t.Fatal("guest ordering", method, target)
			}
			before := len(f.host.reads)
			got := answer(f, method, target, "viewer")
			path := r.URL.EscapedPath()
			known := path == "/" || path == "/tools" || path == "/about"
			switch {
			case !known:
				same(t, got, expected(f, method, target, "notfound", 404, pages.NotFoundData{Banner: banner(f, target, nil)}))
			case method != "GET" && method != "HEAD":
				if got.Code != 405 || !reflect.DeepEqual(got.Header().Values("Allow"), []string{"GET, HEAD"}) || got.Body.Len() != 0 {
					t.Fatal("method ordering", method, target)
				}
			case strings.Contains(target, "bogus"):
				same(t, got, expected(f, method, target, "notfound", 404, pages.NotFoundData{Banner: banner(f, target, nil)}))
			}
			landingRead := path == "/" && (method == "GET" || method == "HEAD") && !strings.Contains(target, "bogus")
			if !landingRead && len(f.host.reads) != before {
				t.Fatal("early answer read host", method, target)
			}
		}
	}

	f.host.reads = nil
	for _, list := range []string{tools.ToolGroups, tools.ToolUsers} {
		other := tools.ToolGroups
		if list == tools.ToolGroups {
			other = tools.ToolUsers
		}
		for _, value := range []string{"bogus", "LAST", "last/", token(other, "x"), token(other+"-before", "x")} {
			for _, both := range []bool{false, true} {
				values := url.Values{list + "_page": []string{value}}
				if both {
					values.Set(other+"_page", "bogus")
				}
				target := "/?" + values.Encode()
				for _, method := range []string{"GET", "HEAD"} {
					same(t, answer(f, method, target, "viewer"), expected(f, method, target, "notfound", 404, pages.NotFoundData{Banner: banner(f, target, nil)}))
				}
			}
		}
	}
	if len(f.host.reads) != 0 {
		t.Fatal("invalid value read host", f.host.reads)
	}
	for _, headers := range [][]string{nil, {""}, {"", "ignored-user"}} {
		r := request("GET", "/about", "")
		if headers != nil {
			r.Header["X-User-Id"] = headers
		}
		w := httptest.NewRecorder()
		identity.Optional(pages.Handler(f.cfg)).ServeHTTP(w, r)
		if w.Code != 302 || w.Header().Get("Location") != pages.SignIn(r, f.cfg.ServicesPath) || w.Body.Len() != 0 {
			t.Fatal("empty first identity was not a guest", headers)
		}
	}

}

type bounds struct {
	start, end int
	prev, next string
	value      string
}

func token(name, key string) string { return (paging.Token{Tool: name}).Encode(key) }
func fill(f *fixture, n int) {
	for i := 0; i < n; i++ {
		g := fmt.Sprintf("group%03d", i)
		u := fmt.Sprintf("user%03d", i)
		f.host.groups = append(f.host.groups, host.Group{Slug: g, Members: i + 3})
		state := host.StateActive
		if i%2 == 1 {
			state = host.StateLocked
		}
		groups := []string{g, "unlisted"}
		if i%3 == 0 {
			groups = nil
		}
		last := time.Time{}
		if i%2 == 1 {
			last = time.Date(2026, 10, 28, 8, 7, 6, 777, time.FixedZone("chosen-zone", 7200))
		}
		f.host.users = append(f.host.users, host.User{Name: u, State: state, Groups: groups, Keys: []string{"first-key", "second-key"}, LastSignIn: last})
	}
}
func landing(f *fixture, target string, g, u bounds, records map[string]store.Group) pages.LandingData {
	rows := []pages.GroupRow{}
	people := []pages.UserRow{}
	for _, item := range f.host.groups[g.start:g.end] {
		members := []string{}
		for _, user := range f.host.users {
			for _, slug := range user.Groups {
				if slug == item.Slug {
					members = append(members, user.Name)
					break
				}
			}
		}
		created, text := "", ""
		if rec, ok := records[item.Slug]; ok {
			created = rec.Created.UTC().Format(time.RFC3339)
			text = rec.Created.UTC().Format(pages.MinuteLayout)
		}
		rows = append(rows, pages.GroupRow{Slug: item.Slug, MemberCount: item.Members, Members: members, Created: created, CreatedText: text})
	}
	for _, item := range f.host.users[u.start:u.end] {
		stamp, text := "", ""
		if !item.LastSignIn.IsZero() {
			stamp = item.LastSignIn.UTC().Format(time.RFC3339)
			text = item.LastSignIn.UTC().Format(pages.MinuteLayout)
		}
		people = append(people, pages.UserRow{Login: item.Name, State: item.State, Groups: append([]string{}, item.Groups...), KeyCount: len(item.Keys), LastSignIn: stamp, LastSignInText: text})
	}
	return pages.LandingData{Banner: banner(f, target, nil), Groups: rows, GroupsPage: g.value, GroupsPrev: g.prev, GroupsNext: g.next, Users: people, UsersPage: u.value, UsersPrev: u.prev, UsersNext: u.next}
}
func query(g, u string) string {
	v := url.Values{}
	if g != "" {
		v.Set("groups_page", g)
	}
	if u != "" {
		v.Set("users_page", u)
	}
	if len(v) == 0 {
		return "/"
	}
	return "/?" + v.Encode()
}

// R-B32P-M31H R-B4AL-ZUS6 R-B5II-DMIV R-BOC1-3LAQ R-BPJX-HD1F R-B967-IXQY R-BQRT-V4S4 R-BRZQ-8WIT R-BVNF-E7QW R-BY38-5R8A R-BZB4-JIYZ R-CIKL-5861
func TestLandingWindows(t *testing.T) {
	f := setup(t)
	n := 2*paging.DefaultLimit + 7
	fill(f, n)
	records := map[string]store.Group{}
	for _, slug := range []string{"group000", "group026", "group055", "record-only"} {
		rec, _, err := f.cfg.Store.AddGroup(t.Context(), slug, "creator")
		if err != nil {
			t.Fatal(err)
		}
		records[slug] = rec
	}
	l := paging.DefaultLimit
	type scenario struct{ g, u bounds }
	windows := func(name, prefix string) []bounds {
		return []bounds{
			{0, l, "", token(name, fmt.Sprintf("%s%03d", prefix, l-1)), ""},
			{l, 2 * l, token(name+"-before", fmt.Sprintf("%s%03d", prefix, l)), token(name, fmt.Sprintf("%s%03d", prefix, 2*l-1)), token(name, fmt.Sprintf("%s%03d", prefix, l-1))},
			{2 * l, n, token(name+"-before", fmt.Sprintf("%s%03d", prefix, 2*l)), "", token(name, fmt.Sprintf("%s%03d", prefix, 2*l-1))},
			{l, 2 * l, token(name+"-before", fmt.Sprintf("%s%03d", prefix, l)), token(name, fmt.Sprintf("%s%03d", prefix, 2*l-1)), token(name+"-before", fmt.Sprintf("%s%03d", prefix, 2*l))},
			{n - l, n, token(name+"-before", fmt.Sprintf("%s%03d", prefix, n-l)), "", "last"},
			{n - l, n, token(name+"-before", fmt.Sprintf("%s%03d", prefix, n-l)), "", token(name+"-before", "zzzz")},
			{l + 1, 2*l + 1, token(name+"-before", fmt.Sprintf("%s%03d", prefix, l+1)), token(name, fmt.Sprintf("%s%03d", prefix, 2*l)), token(name+"-before", fmt.Sprintf("%s%03da", prefix, 2*l))},
			{n, n, "", "", token(name, "zzzz")},
		}
	}
	gs, us := windows(tools.ToolGroups, "group"), windows(tools.ToolUsers, "user")
	cases := []scenario{}
	for _, g := range gs {
		cases = append(cases, scenario{g, us[1]})
	}
	for _, u := range us {
		cases = append(cases, scenario{gs[1], u})
	}
	for _, tc := range cases {
		target := query(tc.g.value, tc.u.value)
		f.host.reads = nil
		want := expected(f, "GET", target, "landing", 200, landing(f, target, tc.g, tc.u, records))
		for _, viewer := range []string{"creator", "other-viewer"} {
			same(t, answer(f, "GET", target, viewer), want)
		}
		if !reflect.DeepEqual(f.host.reads, []string{"groups", "users", "groups", "users"}) {
			t.Fatal("landing does not use exactly the two lists", f.host.reads)
		}
	}
	// Both lists accept their first query value, even when a later one is invalid.
	target := "/?groups_page=&groups_page=bogus&users_page=&users_page=bogus&ignored=value"
	same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "landing", 200, landing(f, target, gs[0], us[0], records)))
}

// R-BWVB-RZHL R-BY38-5R8A R-BPJX-HD1F R-BOC1-3LAQ
func TestUnderflowCanonicalAddress(t *testing.T) {
	f := setup(t)
	fill(f, paging.DefaultLimit+2)
	f.d.SetFailing(true)
	gp := token(tools.ToolGroups+"-before", "group025")
	up := token(tools.ToolUsers+"-before", "user025")
	ga := token(tools.ToolGroups, "group024")
	ua := token(tools.ToolUsers, "user024")
	for _, tc := range []struct{ g, u, want string }{{gp, "", "/"}, {gp, ua, "/?users_page=" + url.QueryEscape(ua)}, {ga, up, "/?groups_page=" + url.QueryEscape(ga)}, {gp, up, "/"}} {
		target := query(tc.g, tc.u) + "&irrelevant=discard"
		for _, method := range []string{"GET", "HEAD"} {
			got := answer(f, method, target, "viewer")
			if got.Code != 303 || !reflect.DeepEqual(got.Header().Values("Location"), []string{tc.want}) || got.Body.Len() != 0 {
				t.Fatal("underflow", target, got.Code, got.Header(), got.Body)
			}
		}
	}
	f.host.users = f.host.users[:paging.DefaultLimit]
	got := answer(f, "GET", query("last", "last"), "viewer")
	if got.Code != 303 || got.Header().Get("Location") != "/?groups_page=last" {
		t.Fatal("one last underflow", got.Code, got.Header())
	}
	f.host.groups = f.host.groups[:paging.DefaultLimit]
	got = answer(f, "GET", query("last", "last"), "viewer")
	if got.Code != 303 || got.Header().Get("Location") != "/" {
		t.Fatal("both last underflow")
	}
}

// R-BLD7-CN5W R-C0J0-XAPO R-AY74-302P
func TestFailures(t *testing.T) {
	for _, closed := range []bool{false, true} {
		f := setup(t)
		if closed {
			_ = f.d.Close()
		} else {
			f.d.SetFailing(true)
		}
		for _, which := range []string{"groups", "users", "none"} {
			for _, err := range []error{&host.RefusedError{Status: 2, Diagnostic: "chosen host diagnostic"}, &host.FailedError{Status: 1}} {
				f.host.groupErr = nil
				f.host.userErr = nil
				line := store.Unreachable
				if which == "groups" {
					f.host.groupErr = err
					line = host.HostFailed
				}
				if which == "users" {
					f.host.userErr = err
					line = host.HostFailed
				}
				for _, target := range []string{"/", "/?groups_page=last"} {
					if which == "none" && strings.Contains(target, "last") {
						continue
					}
					for _, method := range []string{"GET", "HEAD"} {
						w := answer(f, method, target, "viewer")
						body := line + "\n"
						if method == "HEAD" {
							body = ""
						}
						if w.Code != 503 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != body {
							t.Fatal("failure answer", closed, which, target, w.Code, w.Header(), w.Body.String())
						}
					}
				}
			}
		}
	}
}

// R-BNT0-46NA R-BP0W-HYDZ
func TestToolsAndAboutReadNoState(t *testing.T) {
	f := setup(t)
	f.d.SetFailing(true)
	f.host.groupErr = &host.FailedError{Status: 1}
	f.host.userErr = f.host.groupErr
	checkTools := func() {
		rows := []pages.ToolRow{}
		for _, tool := range f.cfg.MCP.Tools() {
			rows = append(rows, pages.ToolRow{Name: tool.Name, Description: tools.FirstLine(tool.Description)})
		}
		target := "/tools?groups_page=bogus"
		same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "tools", 200, pages.ToolsData{Banner: banner(f, target, []page.Level{pages.ToolsLevel}), Tools: rows}))
	}
	checkTools()
	tools.Register(f.cfg.MCP, tools.Config{Store: f.cfg.Store, Host: f.cfg.Host, Telemetry: f.writer})
	checkTools()
	mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: "chosen_extra", Description: "chosen first.\n\nchosen second.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	checkTools()
	target := "/about?users_page=bogus"
	same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "about", 200, pages.AboutData{Banner: banner(f, target, []page.Level{pages.AboutLevel}), Description: pages.Description}))
	if len(f.host.reads) != 0 {
		t.Fatal("informational page read host")
	}
}

// R-BUFJ-0G07
func TestToolAndPageWindowsAgree(t *testing.T) {
	f := setup(t)
	fill(f, paging.DefaultLimit+1)
	tools.Register(f.cfg.MCP, tools.Config{Store: f.cfg.Store, Host: f.cfg.Host, Telemetry: f.writer})
	srv := httptest.NewServer(identity.Require(f.cfg.MCP))
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	for _, name := range []string{tools.ToolGroups, tools.ToolUsers} {
		p := ""
		start := 0
		for {
			args := map[string]string{}
			if p != "" {
				args["page"] = p
			}
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.CallTool(t.Context(), identity.Caller{UserID: "viewer"}, name, raw)
			if err != nil || result.IsError() {
				t.Fatal("tool", err, result)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				StructuredContent struct {
					Groups []tools.Group `json:"groups"`
					Users  []tools.User  `json:"users"`
					Next   *string       `json:"next"`
				} `json:"structuredContent"`
			}
			if err := json.Unmarshal(encoded, &envelope); err != nil {
				t.Fatal(err)
			}
			content := envelope.StructuredContent
			next := ""
			if content.Next != nil {
				next = *content.Next
			}
			end := min(start+paging.DefaultLimit, len(f.host.groups))
			g := bounds{0, paging.DefaultLimit, "", token(tools.ToolGroups, "group024"), ""}
			u := bounds{0, paging.DefaultLimit, "", token(tools.ToolUsers, "user024"), ""}
			prev := ""
			if p != "" {
				prefix := "group"
				if name == tools.ToolUsers {
					prefix = "user"
				}
				prev = token(name+"-before", fmt.Sprintf("%s%03d", prefix, start))
			}
			current := bounds{start, end, prev, next, p}
			if name == tools.ToolGroups {
				g = current
				if len(content.Groups) != end-start {
					t.Fatal("group count")
				}
				for i, row := range content.Groups {
					if row.Slug != f.host.groups[start+i].Slug {
						t.Fatal("group order")
					}
				}
			} else {
				u = current
				if len(content.Users) != end-start {
					t.Fatal("user count")
				}
				for i, row := range content.Users {
					if row.Name != f.host.users[start+i].Name {
						t.Fatal("user order")
					}
				}
			}
			target := query(g.value, u.value)
			same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "landing", 200, landing(f, target, g, u, nil)))
			if next == "" {
				break
			}
			p = next
			start = end
		}
	}
}

// R-BQ8S-VQ4O
func TestHeadMatchesGet(t *testing.T) {
	f := setup(t)
	fill(f, paging.DefaultLimit+1)
	for _, failed := range []bool{false, true} {
		f.d.SetFailing(failed)
		for _, target := range []string{"/", "/tools?ignored=yes", "/about", "/nope", "/?groups_page=bogus", "/?groups_page=" + url.QueryEscape(token(tools.ToolGroups+"-before", "group001")), "/?groups_page=last"} {
			get, head := answer(f, "GET", target, "viewer"), answer(f, "HEAD", target, "viewer")
			if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
				t.Fatal("HEAD differs", target, get.Code, head.Code)
			}
		}
	}
}

// R-8IL2-DI2L R-CHCO-RGFC
func TestPagesAreReadOnlyAndReachNoSibling(t *testing.T) {
	f := setup(t)
	fill(f, 1)
	if _, _, err := f.cfg.Store.AddGroup(t.Context(), "group000", "creator"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.cfg.Store.AddUser(t.Context(), "user000", "creator"); err != nil {
		t.Fatal(err)
	}
	beforeGroups, err := f.cfg.Store.Groups(t.Context(), []string{"group000", "other"})
	if err != nil {
		t.Fatal(err)
	}
	beforeUsers, err := f.cfg.Store.Users(t.Context(), []string{"user000", "other"})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "pages-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	}()
	var contacts atomic.Int32
	sockets := []string{}
	for _, name := range []string{"auth", "other"} {
		path := filepath.Join(dir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				contacts.Add(1)
				_, _ = conn.Write([]byte{1})
				_ = conn.Close()
			}
		}()
		t.Cleanup(func() { _ = ln.Close(); <-done })
		sockets = append(sockets, path)
	}
	f.cfg.ServicesPath = servicesFile(t, "https://chosen-auth.test/", sockets...)
	for _, user := range []string{"", "viewer"} {
		for _, tc := range []struct{ method, target string }{{"GET", "/"}, {"GET", "/tools"}, {"GET", "/about"}, {"GET", "/nope"}, {"POST", "/"}, {"GET", "/?groups_page=last"}, {"HEAD", "/"}} {
			w := answer(f, tc.method, tc.target, user)
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("page set cookie")
			}
		}
	}
	// A deliberate accepted probe drains earlier queued connections on each socket.
	for _, socket := range sockets {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		var ack [1]byte
		_, err = io.ReadFull(conn, ack[:])
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if contacts.Load() != 2 {
		t.Fatal("page connected to sibling")
	}
	if len(f.host.mutations) != 0 {
		t.Fatal("page mutated host", f.host.mutations)
	}
	afterGroups, err := f.cfg.Store.Groups(t.Context(), []string{"group000", "other"})
	if err != nil {
		t.Fatal(err)
	}
	afterUsers, err := f.cfg.Store.Users(t.Context(), []string{"user000", "other"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeGroups, afterGroups) || !reflect.DeepEqual(beforeUsers, afterUsers) {
		t.Fatal("page changed record")
	}
}

// R-BRZQ-8WIT R-B5II-DMIV R-BQRT-V4S4
func TestLandingBoundarySizes(t *testing.T) {
	for _, n := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1} {
		f := setup(t)
		fill(f, n)
		size := min(n, paging.DefaultLimit)
		gn, un := "", ""
		if n > paging.DefaultLimit {
			gn = token(tools.ToolGroups, "group024")
			un = token(tools.ToolUsers, "user024")
		}
		g, u := bounds{0, size, "", gn, ""}, bounds{0, size, "", un, ""}
		same(t, answer(f, "GET", "/", "viewer"), expected(f, "GET", "/", "landing", 200, landing(f, "/", g, u, nil)))
	}
}
