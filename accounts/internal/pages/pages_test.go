package pages_test

import (
	"bytes"
	"context"
	"encoding/binary"
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

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
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

// operationIDs gives the store a distinct eight-byte draw for each operation.
func operationIDs() *bytes.Reader {
	var raw []byte
	for i := range uint64(1024) {
		raw = binary.BigEndian.AppendUint64(raw, i)
	}
	return bytes.NewReader(raw)
}

type fixture struct {
	cfg      pages.Config
	d        *db.DB
	writer   *telemetry.Writer
	advance  func(time.Duration)
	setClock func(time.Time)
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	clock := time.Date(2026, 11, 17, 9, 3, 7, 0, time.FixedZone("chosen-zone", -5*3600))
	now := func() time.Time { return clock }
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
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true, Trail: []page.Level{{Name: "discarded", URL: "/discarded"}}}
	}
	cfg := pages.Config{banner, set, "", store.New(d, store.Config{Now: now, Rand: operationIDs()}), mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), true, "work.test", now}
	return &fixture{
		cfg: cfg, d: d, writer: w,
		advance:  func(d time.Duration) { clock = clock.Add(d) },
		setClock: func(at time.Time) { clock = at },
	}
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
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) {
		t.Fatalf("answer status and headers = %d %v, want %d %v", a.Code, a.Header(), b.Code, b.Header())
	}
	got, want := a.Body.String(), b.Body.String()
	if got != want {
		first := 0
		for first < min(len(got), len(want)) && got[first] == want[first] {
			first++
		}
		t.Fatalf("bodies differ at byte %d: got %q, want %q", first, got[first:min(first+100, len(got))], want[first:min(first+100, len(want))])
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

// R-DMRY-9F8U R-A4XI-WIA1 R-A65F-AA0Q R-S4I5-RN8C R-K8CH-7HG3
func TestPageNamesAndTimeLayouts(t *testing.T) {
	const service, minute, second = pages.ServiceName, pages.MinuteLayout, pages.SecondLayout
	if service != "accounts" || minute != "2006-01-02 15:04" || second != "2006-01-02 15:04:05 UTC" {
		t.Fatal("service or time formats")
	}
	if pages.ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || pages.AboutLevel != (page.Level{Name: "about", URL: "/about"}) || pages.HostLevel("field-host") != (page.Level{Name: "field-host", URL: "/hosts/field-host"}) {
		t.Fatal("page levels")
	}
}

// R-2QRO-TAYI R-CDGQ-KT3P R-CEOM-YKUE R-CFWJ-CCL3 R-CH4F-Q4BS R-CICC-3W2H R-CM01-97AK R-CJK8-HNT6 R-CVUA-IWJR
func TestPageDataShapes(t *testing.T) {
	t.Helper()
	_ = struct {
		Banner                                   page.Banner
		Configured                               bool
		Domain                                   string
		Hosts                                    []pages.HostRow
		HostsPage, HostsPrev, HostsNext          string
		KeptVolumes                              []pages.KeptVolumeRow
		VolumesPage, VolumesPrev, VolumesNext    string
		Accounts                                 []pages.AccountRow
		AccountsPage, AccountsPrev, AccountsNext string
		Users                                    []pages.UserRow
		UsersPage, UsersPrev, UsersNext          string
	}(pages.LandingData{})
	_ = struct {
		Name, DNSName, State, Reason, OnDelete, Health, Type string
		RootGB, DataGB                                       int
		EIP                                                  string
		AccountCount                                         int
		Reported, ReportedText, Closed, ClosedText           string
		BackupFlagged                                        int
	}(pages.HostRow{})
	_ = struct {
		Slug, Host, DNSName, State, Reason                   string
		UserCount, InviteCount                               int
		Backup, Snapshot, SnapshotText, Created, CreatedText string
	}(pages.AccountRow{})
	_ = struct {
		Login, Account             string
		Admin                      bool
		State                      string
		Pending                    bool
		KeyCount                   int
		LastSignIn, LastSignInText string
	}(pages.UserRow{})
	_ = struct {
		Volume, Host, Cause  string
		SizeGB               int
		Deleted, DeletedText string
	}(pages.KeptVolumeRow{})
	_ = struct {
		Banner        page.Banner
		Host          pages.HostView
		Report        *pages.ReportView
		Backups       []pages.BackupRow
		DatabaseCount int
		Restores      []pages.RestoreRow
		RestoreTotal  int
	}(pages.HostData{})
	_ = struct {
		Name, DNSName, State, Reason, OnDelete, Health, Type  string
		RootGB, DataGB                                        int
		EIP                                                   string
		AccountCount                                          int
		Reported, ReportedText, Closed, ClosedText            string
		BackupFlagged                                         int
		Replace, InstanceID, DataVolume, Created, CreatedText string
		Allowlist                                             []string
	}(pages.HostView{})
	_ = struct {
		Account, Health                                                                           string
		FilesStale                                                                                bool
		Snapshot, SnapshotText, Checked, CheckedText, Error, Final, FinalAt, FinalAtText, Restore string
		Databases                                                                                 []pages.DatabaseRow
		DatabaseTotal                                                                             int
	}(pages.BackupRow{})
	_ = struct {
		Path, Health, Written, WrittenText, Error string
	}(pages.DatabaseRow{})
	_ = struct {
		Account, Kind, Path, AsOf, AsOfText, Copy, State, Reason, Asked, AskedText string
	}(pages.RestoreRow{})
	_ = struct {
		Banner                                                                                                                                                            page.Banner
		Description, DefaultType                                                                                                                                          string
		DefaultRootGB, DefaultDataGB, BackupEveryMinutes, BackupHourly, BackupDaily, BackupMonthly, BackupWindowDays, BackupFilesStaleMinutes, BackupDatabaseStaleMinutes int
	}(pages.AboutData{})
	const databaseRows, restoreRows int = pages.MaxDatabaseRows, pages.MaxRestoreRows
	if databaseRows != 50 || restoreRows != 25 {
		t.Fatalf("row limits = %d databases and %d restores, want 50 and 25", databaseRows, restoreRows)
	}
	_ = struct {
		Received, ReceivedText, Hostname, InstanceID, Type, AMI, OS, Kernel, DaemonVersion, Booted, BootedText string
		Applied, Wanted                                                                                        int64
		Error                                                                                                  string
		Load1, Load5, Load15, CPUPercent                                                                       float64
		MemoryUsed, MemoryAvailable, SwapUsed, SwapTotal                                                       int64
		Root, Data                                                                                             pages.VolumeView
		Sessions, Updates                                                                                      int
		Reboot                                                                                                 bool
		FailedUnits                                                                                            []string
		Slices                                                                                                 []pages.SliceRow
		Bans                                                                                                   []pages.BanRow
		BanTotal                                                                                               int
		Allowlist                                                                                              []string
	}(pages.ReportView{})
}

// R-A8L8-1TI4 R-A9T4-FL8T R-ATBI-JX3X R-CY71-2WPI R-CVUA-IWJR R-AICF-3ZFO R-AJKB-HR6D R-AM04-9ANR R-7E62-5GAS R-7FDY-J81H
func TestTemplateSet(t *testing.T) {
	t.Chdir(t.TempDir())
	independent, err := page.Templates().ParseFS(accounts.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	load := signature[func() (*pages.Set, error)](pages.Load)
	write := signature[func(*pages.Set, http.ResponseWriter, *http.Request, int, string, any)]((*pages.Set).Write)
	set, err := load()
	if err != nil {
		t.Fatal(err)
	}
	again, err := load()
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, data any) {
		t.Helper()
		var raw bytes.Buffer
		if err := independent.ExecuteTemplate(&raw, name, data); err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{"GET", "HEAD"} {
			want := raw.String()
			if method == "HEAD" {
				want = ""
			}
			for _, templates := range []*pages.Set{set, again} {
				w := httptest.NewRecorder()
				write(templates, w, request(method, "/", "viewer"), 207, name, data)
				if w.Code != 207 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != want {
					t.Fatalf("template %s method %s", name, method)
				}
			}
		}
	}
	var b page.Banner
	check("notfound", pages.NotFoundData{b})
	check("landing", pages.LandingData{Banner: b})
	check("landing", pages.LandingData{Banner: b, Configured: true, Hosts: []pages.HostRow{}, KeptVolumes: []pages.KeptVolumeRow{}, Accounts: []pages.AccountRow{}, Users: []pages.UserRow{}})
	reports := hostTemplateReports()
	states := []string{store.HostProvisioning, store.HostRunning, store.HostStopping, store.HostStopped, store.HostStarting, store.HostResizing, store.HostDeleting, store.HostClosing, store.HostOpening, store.HostReplacing, store.HostRestoring, store.HostFailed}
	healths := []string{store.HealthSync, store.HealthPending, store.HealthError, store.HealthSilent, store.HealthNone}
	for _, full := range []bool{false, true} {
		var dnsName, eip, reason, stamp, visible, instance, volume string
		var rows []pages.ToolRow
		if full {
			b.Release, b.Commit = "chosen-release", "chosen-commit"
			dnsName = "chosen-host.work.test"
			eip, reason, stamp, visible = "192.0.2.1", "chosen-reason", "2026-11-17T14:05:09Z", "2026-11-17 14:05"
			instance, volume = "chosen-instance", "chosen-volume"
			rows = []pages.ToolRow{{"chosen-tool", "chosen-copy"}}
		}
		var hosts []pages.HostRow
		for i, state := range states {
			health := healths[i%len(healths)]
			view := pages.HostView{Name: "chosen-host", DNSName: dnsName, State: state, Reason: reason, Health: health, Type: "chosen-type",
				RootGB: 20, DataGB: 50, EIP: eip, AccountCount: 3, Reported: stamp, ReportedText: visible, Closed: stamp, ClosedText: visible,
				InstanceID: instance, DataVolume: volume, Created: stamp, CreatedText: visible, Allowlist: []string{}}
			data := pages.HostData{Banner: b, Host: view}
			if full {
				data.Host.BackupFlagged = 2
				data.Host.Allowlist = []string{"192.0.2.0/24"}
			}
			for _, choice := range []string{"", store.OnDeleteKeep, store.OnDeleteDelete} {
				hosts = append(hosts, pages.HostRow{Name: view.Name, DNSName: dnsName, State: state, Reason: reason, OnDelete: choice, Health: health, Type: view.Type,
					RootGB: 20, DataGB: 50, EIP: eip, AccountCount: 3, Reported: stamp, ReportedText: visible, Closed: stamp, ClosedText: visible, BackupFlagged: data.Host.BackupFlagged})
				data.Host.OnDelete = choice
				for _, replace := range []string{"", store.ReplaceVolume, store.ReplaceBackup} {
					data.Host.Replace = replace
					checkHostTemplateLists(check, data, reports)
				}
			}
		}
		var accountsRows []pages.AccountRow
		for _, state := range []string{store.AccountPending, store.AccountActive, store.AccountFailed, store.AccountRemoving} {
			for _, invites := range []int{0, 2} {
				for _, backup := range []string{store.BackupNone, store.BackupHealthy, store.BackupStale, store.BackupError} {
					accountsRows = append(accountsRows, pages.AccountRow{Slug: "chosen-account", Host: "chosen-host", DNSName: "chosen-account.work.test",
						State: state, Reason: reason, UserCount: 3, InviteCount: invites, Backup: backup,
						Snapshot: stamp, SnapshotText: visible, Created: stamp, CreatedText: visible})
				}
			}
		}
		var users []pages.UserRow
		for _, state := range []string{store.UserActive, store.UserLocked, store.UserRemoving} {
			for _, pending := range []bool{false, true} {
				for _, admin := range []bool{false, true} {
					users = append(users, pages.UserRow{"chosen-user", "chosen-account", admin, state, pending, 2, stamp, visible})
				}
			}
		}
		volumes := []pages.KeptVolumeRow{
			{Volume: "vol-deleted", Host: "deleted-host", Cause: store.CauseDeleted, SizeGB: 50, Deleted: "2026-11-17T14:04:09Z", DeletedText: "2026-11-17 14:04"},
			{Volume: "vol-replaced", Host: "chosen-host", Cause: store.CauseReplaced, SizeGB: 50, Deleted: "2026-11-17T14:05:09Z", DeletedText: "2026-11-17 14:05"},
		}
		check("landing", pages.LandingData{b, true, "work.test", hosts, "hp", "hprev", "hnext", volumes, "vp", "vprev", "vnext", accountsRows, "ap", "aprev", "anext", users, "up", "uprev", "unext"})
		check("tools", pages.ToolsData{b, rows})
		check("about", aboutData(b, store.DefaultBackupSettings()))
		check("about", aboutData(b, otherBackupSettings))
	}
	checkJoinTemplates(t, check)
	for _, tc := range []struct {
		name string
		data any
	}{{"missing", nil}, {"landing", struct{ Banner page.Banner }{}}} {
		for _, method := range []string{"GET", "HEAD"} {
			w := httptest.NewRecorder()
			write(set, w, request(method, "/", "viewer"), 207, tc.name, tc.data)
			if w.Code != 500 || w.Body.Len() != 0 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
				t.Fatal("template failure leaked partial output")
			}
		}
	}
}

func hostTemplateReports() []*pages.ReportView {
	const stamp, visible = "2026-11-17T14:05:09Z", "2026-11-17 14:05:09 UTC"
	const hostname = "reported-host.work.test"
	empty := pages.ReportView{stamp, visible, hostname, "chosen-instance", "chosen-type", "chosen-ami", "chosen-os", "chosen-kernel",
		"chosen-daemon", stamp, visible, 2, 3, "chosen-reason", 1, 2, 3, 4, 10, 11, 12, 13, pages.VolumeView{1, 2, 3, 4},
		pages.VolumeView{5, 6, 7, 8}, 15, 16, true, []string{}, []pages.SliceRow{}, []pages.BanRow{}, 0, []string{}}
	withoutHostname := empty
	withoutHostname.Hostname = ""
	positiveBanTotal := empty
	positiveBanTotal.BanTotal = 517
	full := empty
	full.FailedUnits = []string{"chosen-unit"}
	full.Slices = []pages.SliceRow{{"chosen-slice", 17, 18}}
	full.Bans = []pages.BanRow{{"203.0.113.7", stamp, visible, stamp, visible}}
	full.BanTotal = len(full.Bans)
	full.Allowlist = []string{"192.0.2.7"}
	truncated := full
	truncated.BanTotal++
	return []*pages.ReportView{nil, &withoutHostname, &positiveBanTotal, &full, &truncated}
}

func checkHostTemplateLists(check func(string, any), data pages.HostData, reports []*pages.ReportView) {
	const stamp, visible = "2026-11-17T14:05:09Z", "2026-11-17 14:05:09 UTC"
	var populated pages.HostData
	fillBackupTemplates(&populated, stamp, visible, "chosen-reason")
	for _, report := range reports {
		data.Report = report
		for _, backups := range [][]pages.BackupRow{{}, populated.Backups} {
			data.Backups, data.DatabaseCount = backups, 0
			for _, backup := range backups {
				data.DatabaseCount += len(backup.Databases)
			}
			data.Restores, data.RestoreTotal = []pages.RestoreRow{}, 0
			check("host", data)
			data.Restores, data.RestoreTotal = populated.Restores, len(populated.Restores)
			check("host", data)
			data.RestoreTotal++
			check("host", data)
		}
	}
}

func checkJoinTemplates(t *testing.T, check func(string, any)) {
	t.Helper()
	type outcomeValue string
	const form, unusable, name, key, limited, full, done outcomeValue = pages.JoinForm, pages.JoinUnusable, pages.JoinName, pages.JoinKey, pages.JoinLimited, pages.JoinFull, pages.JoinDone
	for _, outcome := range []struct{ constant, want outcomeValue }{
		{form, ""}, {unusable, "unusable"}, {name, "name"}, {key, "key"}, {limited, "limited"}, {full, "full"}, {done, "done"},
	} {
		if outcome.constant != outcome.want {
			t.Fatalf("join outcome %q, want %q", outcome.constant, outcome.want)
		}
	}
	check("join", pages.JoinData{pages.JoinUnusable, "", "", false, "", ""})
	check("join", pages.JoinData{pages.JoinLimited, "", "", false, "", ""})
	for _, admin := range []bool{false, true} {
		check("join", pages.JoinData{pages.JoinForm, "chosen-account", "chosen-account.work.test", admin, "", ""})
		check("join", pages.JoinData{pages.JoinDone, "chosen-account", "chosen-account.work.test", admin, "chosen-user", ""})
		for _, outcome := range []string{pages.JoinName, pages.JoinKey, pages.JoinFull} {
			for _, user := range []string{"", "chosen-user"} {
				for _, submittedKey := range []string{"", "chosen-key"} {
					check("join", pages.JoinData{outcome, "chosen-account", "chosen-account.work.test", admin, user, submittedKey})
				}
			}
		}
	}
}

func notFound(f *fixture, method, target string) *httptest.ResponseRecorder {
	return expected(f, method, target, "notfound", 404, pages.NotFoundData{Banner: banner(f, target, nil)})
}

func statusWithHeader(status int, name, value string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	w.Header().Set(name, value)
	w.WriteHeader(status)
	return w
}

// R-ADGT-KWGW R-CQYO-ZTKZ R-AY74-302P R-RSB5-XXTE R-RZMK-8K9K R-SFH9-7KWL R-RX6R-H0S6 R-SGP5-LCNA R-SHX1-Z4DZ R-MNRM-LE5Z R-SQGC-NIKU R-CVR8-BD84
func TestPageDecisionOrder(t *testing.T) {
	f := setup(t)
	f.cfg.ServicesPath = servicesFile(t, "https://chosen-auth.test/")
	handler := signature[func(pages.Config) http.Handler](pages.Handler)
	for _, failing := range []bool{true, false} {
		f.d.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, target := range []string{"/", "/hosts/host01", "/?hosts_page=bogus", "/about", "/tools", "/nope"} {
				r := request(method, target, "")
				r.Header["X-User-Id"] = []string{"", "second-user"}
				w := httptest.NewRecorder()
				identity.Optional(handler(f.cfg)).ServeHTTP(w, r)
				same(t, w, statusWithHeader(302, "Location", pages.SignIn(r, f.cfg.ServicesPath)))
			}
			for _, target := range []string{"/nope", "/index.html", "/tools/", "/about/", "/%74ools", "/TOOLS", "//", "/hosts", "/hosts/", "/hosts/host01/", "/hosts/host01/x"} {
				same(t, answer(f, method, target, "viewer"), notFound(f, method, target))
			}
		}
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			for _, target := range []string{"/?hosts_page=bogus", "/tools?ignored=one", "/about", "/hosts/host01", "/hosts/nope", "/hosts/escaped%2Fsegment"} {
				same(t, answer(f, method, target, "viewer"), statusWithHeader(405, "Allow", "GET, HEAD"))
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, target := range []string{
				"/?hosts_page=bogus", "/?users_page=LAST", "/?accounts_page=" + token("users", "account", "user"),
				"/?volumes_page=" + token("hosts", int64(25)) + "&hosts_page=last",
				"/?volumes_page=" + token("kept_volumes", "wrong-member-type", "vol-chosen"),
			} {
				same(t, answer(f, method, target, "viewer"), notFound(f, method, target))
			}
			if failing {
				for _, target := range []string{"/", "/?hosts_page=last", "/?volumes_page=last", "/hosts/host01", "/hosts/nope", "/hosts/escaped%2Fsegment"} {
					w := statusWithHeader(503, "Content-Type", "text/plain; charset=utf-8")
					if method == "GET" {
						w.Body.WriteString(store.Unreachable + "\n")
					}
					same(t, answer(f, method, target, "viewer"), w)
				}
			}
			f.cfg.Configured = false
			target := "/?hosts_page=bogus"
			same(t, answer(f, method, target, "viewer"), expected(f, method, target, "landing", 200, pages.LandingData{Banner: banner(f, target, nil)}))
			same(t, answer(f, method, "/hosts/host01", "viewer"), notFound(f, method, "/hosts/host01"))
			f.cfg.Configured = true
		}
	}
}

// R-SU41-STSX R-CWZ4-P4YT R-ZI18-EL12 R-SWJU-KDAB
func TestInformationPagesAndHead(t *testing.T) {
	f := setup(t)
	for _, registered := range []bool{false, true} {
		if registered {
			tools.Register(f.cfg.MCP, tools.Config{Store: f.cfg.Store, Cloud: struct{ cloud.Cloud }{}, Hosts: settings.SandboxHosts, Configured: true, Wake: func() {}, Telemetry: f.writer})
		}
		rows := []pages.ToolRow{}
		for _, tool := range f.cfg.MCP.Tools() {
			rows = append(rows, pages.ToolRow{Name: tool.Name, Description: tools.FirstLine(tool.Description)})
		}
		for _, configured := range []bool{false, true} {
			f.cfg.Configured = configured
			for _, failing := range []bool{false, true} {
				f.d.SetFailing(failing)
				for _, method := range []string{"GET", "HEAD"} {
					target := "/tools?ignored=one"
					same(t, answer(f, method, target, "viewer"), expected(f, method, target, "tools", 200, pages.ToolsData{Banner: banner(f, target, []page.Level{pages.ToolsLevel}), Tools: rows}))
					target = "/about?ignored=one"
					if failing {
						w := statusWithHeader(503, "Content-Type", "text/plain; charset=utf-8")
						if method == "GET" {
							w.Body.WriteString(store.Unreachable + "\n")
						}
						same(t, answer(f, method, target, "viewer"), w)
					} else {
						same(t, answer(f, method, target, "viewer"), expected(f, method, target, "about", 200,
							aboutData(banner(f, target, []page.Level{pages.AboutLevel}), store.DefaultBackupSettings())))
					}
				}
				for _, target := range []string{"/", "/?hosts_page=last", "/?users_page=bogus", "/nope", "/hosts/host01", "/tools", "/about"} {
					get, head := answer(f, "GET", target, "viewer"), answer(f, "HEAD", target, "viewer")
					get.Body.Reset()
					same(t, head, get)
				}
			}
		}
	}
}

// R-CHCO-RGFC R-3HLH-899S R-SYZN-BWRP
func TestPagesAreReadOnlyAndReachNoSibling(t *testing.T) {
	f := setup(t)
	h, a, u, op := seedRecord(t, f)
	createKeptVolume(t, f, "vol-read-only")
	before := readRecord(t, f, h.Name, a.Slug, u.Name, op.ID)
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
		for _, tc := range []struct{ method, target string }{{"GET", "/"}, {"GET", "/hosts/" + h.Name}, {"GET", "/tools"}, {"GET", "/about"}, {"GET", "/nope"}, {"POST", "/"}, {"GET", "/?hosts_page=last"}, {"HEAD", "/"}} {
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

	after := readRecord(t, f, h.Name, a.Slug, u.Name, op.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("page changed record")
	}
	for _, target := range []string{"/", "/hosts/" + h.Name, "/about", "/tools"} {
		same(t, answer(f, "GET", target, "creator"), answer(f, "GET", target, "second-viewer"))
	}
}

func readRecord(t *testing.T, f *fixture, h, a, u, id string) []any {
	t.Helper()
	host, he := f.cfg.Store.Host(t.Context(), h)
	account, ae := f.cfg.Store.Account(t.Context(), a)
	user, ue := f.cfg.Store.User(t.Context(), a, u)
	op, oe := f.cfg.Store.Operation(t.Context(), id)
	report, re := f.cfg.Store.Report(t.Context(), h)
	kept, ke := f.cfg.Store.ListKeptVolumes(t.Context(), time.Time{}, "", paging.DefaultLimit)
	for _, err := range []error{he, ae, ue, oe, re, ke} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return []any{host, account, user, op, report, kept}
}

var caller = identity.Caller{UserID: "creator"}
var hostSpec = store.HostSpec{Type: "chosen-instance-type", RootGB: 27, DataGB: 73}
var key = store.Key{Type: "ssh-ed25519", Body: "chosen-body", Line: "ssh-ed25519 chosen-body"}

func seedRecord(t *testing.T, f *fixture) (store.Host, store.Account, store.User, store.Operation) {
	t.Helper()
	st := f.cfg.Store
	h, op, err := st.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := st.PlaceAccount(t.Context(), "acme", h.Name, caller)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.AddUser(t.Context(), a.Slug, "daffy", key, caller)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.RecordReport(t.Context(), h.Name, protocol.Report{Protocol: protocol.Version, Applied: u.Version}, f.cfg.Now())
	if err != nil {
		t.Fatal(err)
	}
	return h, a, u, op
}
