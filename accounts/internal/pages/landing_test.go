package pages_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

func token(list string, key ...any) string { return (paging.Token{Tool: list}).Encode(key...) }

const created = "2026-11-17T14:03:07Z"
const createdMinute = "2026-11-17 14:03"
const createdSecond = "2026-11-17 14:03:07 UTC"

// R-CYA3-AG15 R-CZHZ-O7RU R-D35O-TIZX R-D4DL-7AQM
func TestLandingRows(t *testing.T) {
	f := setup(t)
	data := pages.LandingData{Configured: true, Domain: f.cfg.Domain, Hosts: []pages.HostRow{}, Accounts: []pages.AccountRow{}, Users: []pages.UserRow{}}
	checkLanding(t, f, "/", data)
	h, a, u, op := seedRecord(t, f)
	st := f.cfg.Store
	place, err := st.Operation(t.Context(), a.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), place.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	abandoned, pending, err := st.PlaceAccount(t.Context(), "unplaced", h.Name, caller)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Fail(t.Context(), pending.ID, ops.ReasonRecordConflict); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), op.ID, store.Update{InstanceID: "assigned-instance", EIP: "192.0.2.7"}); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteHostRecord(t.Context(), h.Name, "", ops.ReasonRecordConflict); err != nil {
		t.Fatal(err)
	}
	received := f.cfg.Now().Add(2*time.Minute + 2*time.Second)
	seen := received.Add(-time.Second)
	_, err = st.RecordReport(t.Context(), h.Name, protocol.Report{Protocol: protocol.Version, Attempted: u.Version, Applied: u.Version, SignIns: []protocol.SignIn{{User: u.Name, At: seen}}}, received)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddUser(t.Context(), a.Slug, "other-user", key, caller); err != nil {
		t.Fatal(err)
	}
	f.cfg.Now = func() time.Time { return received.Add(store.SilentAfter + time.Second) }
	data.Hosts = []pages.HostRow{{Name: h.Name, DNSName: h.Name + "." + f.cfg.Domain, State: store.HostRunning, Reason: ops.ReasonRecordConflict, Health: store.HealthSilent, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, AccountCount: 2, EIP: "192.0.2.7", Reported: "2026-11-17T14:05:09Z", ReportedText: "2026-11-17 14:05"}}
	data.Accounts = []pages.AccountRow{
		{Slug: a.Slug, Host: h.Name, DNSName: a.Slug + "." + f.cfg.Domain, State: store.AccountActive, UserCount: 2, Created: created, CreatedText: createdMinute},
		{Slug: abandoned.Slug, Host: h.Name, DNSName: abandoned.Slug + "." + f.cfg.Domain, State: store.AccountFailed, Reason: ops.ReasonRecordConflict, Created: created, CreatedText: createdMinute},
	}
	data.Users = []pages.UserRow{
		{Login: u.Name, Account: a.Slug, State: store.UserActive, KeyCount: 1, LastSignIn: "2026-11-17T14:05:08Z", LastSignInText: "2026-11-17 14:05"},
		{Login: "other-user", Account: a.Slug, State: store.UserActive, KeyCount: 1, Pending: true},
	}
	checkLanding(t, f, "/?ignored=one", data)

	_, stop, err := st.StartHostOp(t.Context(), h.Name, store.OpStop, store.Target{}, caller)
	if err != nil {
		t.Fatal(err)
	}
	const failedReason = "chosen stop failure"
	if err := st.Fail(t.Context(), stop.ID, failedReason); err != nil {
		t.Fatal(err)
	}
	data.Hosts[0].State, data.Hosts[0].Reason, data.Hosts[0].Health = store.HostFailed, failedReason, store.HealthPending
	checkLanding(t, f, "/", data)

	f.cfg.Domain, data.Domain, data.Hosts[0].DNSName = "", "", ""
	for i := range data.Accounts {
		data.Accounts[i].DNSName = data.Accounts[i].Slug + "."
	}
	checkLanding(t, f, "/", data)
}

func checkLanding(t *testing.T, f *fixture, target string, data pages.LandingData) {
	t.Helper()
	data.Banner = banner(f, target, nil)
	same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "landing", 200, data))
}

// R-D0PW-1ZIJ R-D1XS-FR98
func TestLandingInviteCountsAndAdmins(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	st := f.cfg.Store
	h, _, err := st.CreateHost(ctx, hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := st.PlaceAccount(ctx, "acme", h.Name, caller)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.AddUser(ctx, a.Slug, "daffy", key, caller)
	if err != nil {
		t.Fatal(err)
	}
	data := pages.LandingData{
		Configured: true,
		Domain:     f.cfg.Domain,
		Hosts:      []pages.HostRow{{Name: h.Name, DNSName: h.Name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, AccountCount: 1}},
		Accounts:   []pages.AccountRow{{Slug: a.Slug, Host: h.Name, DNSName: a.Slug + "." + f.cfg.Domain, State: store.AccountPending, UserCount: 1, Created: created, CreatedText: createdMinute}},
		Users:      []pages.UserRow{{Login: u.Name, Account: a.Slug, State: store.UserActive, KeyCount: 1, Pending: true}},
	}
	checkLanding(t, f, "/", data)
	revoked, _, err := st.CreateInvite(ctx, a.Slug, false, caller)
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, err := st.CreateInvite(ctx, a.Slug, true, caller)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetAdmin(ctx, a.Slug, u.Name, true); err != nil {
		t.Fatal(err)
	}
	data.Accounts[0].InviteCount = 2
	data.Users[0].Admin = true
	checkLanding(t, f, "/", data)

	if _, err := st.RevokeInvite(ctx, revoked.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetAdmin(ctx, a.Slug, u.Name, false); err != nil {
		t.Fatal(err)
	}
	data.Accounts[0].InviteCount = 1
	data.Users[0].Admin = false
	checkLanding(t, f, "/", data)

	if _, _, err := st.Enroll(ctx, adminToken, "joined-user", key); err != nil {
		t.Fatal(err)
	}
	data.Accounts[0].InviteCount = 0
	data.Accounts[0].UserCount = 2
	data.Users = append(data.Users, pages.UserRow{Login: "joined-user", Account: a.Slug, Admin: true, State: store.UserActive, KeyCount: 1, Pending: true})
	checkLanding(t, f, "/", data)

	if _, _, err := st.CreateInvite(ctx, a.Slug, false, caller); err != nil {
		t.Fatal(err)
	}
	f.advance(store.InviteLife + time.Second)
	checkLanding(t, f, "/", data)
}

func createListRows(t *testing.T, f *fixture, n int) pages.LandingData {
	t.Helper()
	out := pages.LandingData{Configured: true, Domain: f.cfg.Domain}
	st := f.cfg.Store
	for i := range n {
		h, _, err := st.CreateHost(t.Context(), hostSpec, caller)
		if err != nil {
			t.Fatal(err)
		}
		a, _, err := st.PlaceAccount(t.Context(), fmt.Sprintf("account-%03d", i), h.Name, caller)
		if err != nil {
			t.Fatal(err)
		}
		u, err := st.AddUser(t.Context(), a.Slug, fmt.Sprintf("user-%03d", n-i), key, caller)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("host%02d", i+1)
		out.Hosts = append(out.Hosts, pages.HostRow{Name: name, DNSName: name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, AccountCount: 1})
		out.Accounts = append(out.Accounts, pages.AccountRow{Slug: a.Slug, Host: h.Name, DNSName: a.Slug + "." + f.cfg.Domain, State: store.AccountPending, UserCount: 1, Created: created, CreatedText: createdMinute})
		out.Users = append(out.Users, pages.UserRow{Login: u.Name, Account: a.Slug, State: store.UserActive, KeyCount: 1, Pending: true})
	}
	return out
}

type bounds struct {
	start, end        int
	value, prev, next string
}

func renderListWindows(data pages.LandingData, b [3]bounds) pages.LandingData {
	data.Hosts = data.Hosts[b[0].start:b[0].end]
	data.Accounts = data.Accounts[b[1].start:b[1].end]
	data.Users = data.Users[b[2].start:b[2].end]
	data.HostsPage, data.HostsPrev, data.HostsNext = b[0].value, b[0].prev, b[0].next
	data.AccountsPage, data.AccountsPrev, data.AccountsNext = b[1].value, b[1].prev, b[1].next
	data.UsersPage, data.UsersPrev, data.UsersNext = b[2].value, b[2].prev, b[2].next
	return data
}

var listNames = [3]string{"hosts", "accounts", "users"}

func keyFor(row int, list int, data pages.LandingData) []any {
	switch list {
	case 0:
		return []any{int64(row + 1)}
	case 1:
		return []any{data.Accounts[row].Slug}
	default:
		return []any{data.Users[row].Account, data.Users[row].Login}
	}
}

func pageURL(b [3]bounds) string {
	query := url.Values{}
	for i, name := range listNames {
		if b[i].value != "" {
			query.Set(name+"_page", b[i].value)
		}
	}
	if len(query) == 0 {
		return "/"
	}
	return "/?" + query.Encode()
}

func firstWindows(rows pages.LandingData, size int) [3]bounds {
	var b [3]bounds
	for i, name := range listNames {
		b[i].end = min(size, paging.DefaultLimit)
		if size > paging.DefaultLimit {
			b[i].next = token(name, keyFor(b[i].end-1, i, rows)...)
		}
	}
	return b
}

// R-RVYV-391H R-RYEN-USIV R-RONG-SMLB R-RR39-K62P R-S22D-03QY R-SO0J-VZ3G R-SP8G-9QU5 R-D81A-CLYP
func TestIndependentPaging(t *testing.T) {
	f := setup(t)
	const l = paging.DefaultLimit
	const size = 4*l + 2
	data := createListRows(t, f, size)
	initial := firstWindows(data, size)
	checkLanding(t, f, "/", renderListWindows(data, initial))
	for i, name := range listNames {
		t.Run(name, func(t *testing.T) {
			beforeToken := func(row int) string { return token(name+"-before", keyFor(row, i, data)...) }
			afterToken := func(row int) string { return token(name, keyFor(row, i, data)...) }
			b := initial
			// Preserve one other list's position throughout this walk.
			other := (i + 2) % len(b)
			b[other] = bounds{l, 2 * l, initial[other].next, token(listNames[other]+"-before", keyFor(l, other, data)...), token(listNames[other], keyFor(2*l-1, other, data)...)}
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			for start := 0; start < size; start += l {
				if start != 0 {
					stop := min(start+l, size)
					b[i] = bounds{start, stop, b[i].next, beforeToken(start), ""}
					if stop < size {
						b[i].next = afterToken(stop - 1)
					}
				}
				checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			}
			for stop := b[i].start; stop > l; stop -= l {
				b[i] = bounds{stop - l, stop, b[i].prev, beforeToken(stop - l), afterToken(stop - 1)}
				checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			}
			b[i].value = b[i].prev
			target := pageURL(b) + "&ignored=one"
			b[i] = initial[i]
			for _, method := range []string{"GET", "HEAD"} {
				same(t, answer(f, method, target, "viewer"), statusWithHeader(303, "Location", pageURL(b)))
			}
			b[i] = bounds{size - l, size, "last", beforeToken(size - l), ""}
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			// A before-token beyond the end has the last window and no next token.
			positions := []string{token(name+"-before", int64(size+7)), token(name+"-before", "zzz"), token(name+"-before", "zzz", "nonexistent")}
			b[i].value = positions[i]
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			positions = []string{token(name, int64(size+7)), token(name, "zzz"), token(name, "zzz", "nonexistent")}
			b[i] = bounds{size, size, positions[i], "", ""}
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
		})
	}
	hosts := token("hosts", int64(13))
	users := token("users", "account-003", "nonexistent")
	target := "/?ignored=one&users_page=" + url.QueryEscape(users) + "&accounts_page=" + url.QueryEscape(token("accounts-before", "a")) + "&hosts_page=" + url.QueryEscape(hosts)
	canonical := "/?hosts_page=" + url.QueryEscape(hosts) + "&users_page=" + url.QueryEscape(users)
	same(t, answer(f, "GET", target, "viewer"), statusWithHeader(303, "Location", canonical))

	empty := setup(t)
	target = "/?hosts_page=last&accounts_page=last&users_page=last&ignored=out"
	same(t, answer(empty, "GET", target, "viewer"), statusWithHeader(303, "Location", "/"))
}

// R-D81A-CLYP holds one list's position fixed across different positions of other lists.
func TestUsersWindowIgnoresHostPosition(t *testing.T) {
	f := setup(t)
	const l = paging.DefaultLimit
	const size = 4*l + 2
	data := createListRows(t, f, size)
	b := firstWindows(data, size)
	b[2] = bounds{l, 2 * l, b[2].next, token("users-before", keyFor(l, 2, data)...), token("users", keyFor(2*l-1, 2, data)...)}
	for _, hostPosition := range []bounds{
		b[0],
		{l, 2 * l, token("hosts", int64(l)), token("hosts-before", int64(l+1)), token("hosts", int64(2*l))},
		{l, 2 * l, token("hosts-before", int64(2*l+1)), token("hosts-before", int64(l+1)), token("hosts", int64(2*l))},
		{size - l, size, "last", token("hosts-before", int64(size-l+1)), ""},
	} {
		b[0] = hostPosition
		checkLanding(t, f, pageURL(b), renderListWindows(data, b))
	}
}

// R-D6TD-YU80
func TestToolAndPageWindowsAgree(t *testing.T) {
	f := setup(t)
	const size = paging.DefaultLimit + 1
	data := createListRows(t, f, size)
	tools.Register(f.cfg.MCP, tools.Config{Store: f.cfg.Store, Cloud: struct{ cloud.Cloud }{}, Hosts: settings.SandboxHosts, Configured: true, Wake: func() {}, Telemetry: f.writer})
	srv := httptest.NewServer(identity.Require(f.cfg.MCP))
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	initial := firstWindows(data, size)
	for i, name := range listNames {
		b := initial
		for start := 0; start < size; start += paging.DefaultLimit {
			args := map[string]string{}
			if b[i].value != "" {
				args["page"] = b[i].value
			}
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.CallTool(t.Context(), caller, name, raw)
			if err != nil || result.IsError() {
				t.Fatalf("%s: %v %v", name, result, err)
			}
			encoded, err := result.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Content struct {
					Hosts    []struct{ Host string }
					Accounts []struct{ Account string }
					Users    []struct{ Account, User string }
					Next     string
				} `json:"structuredContent"`
			}
			if err := json.Unmarshal(encoded, &envelope); err != nil {
				t.Fatal(err)
			}
			content := envelope.Content
			var got, want []string
			switch i {
			case 0:
				for _, row := range content.Hosts {
					got = append(got, row.Host)
				}
				for _, row := range data.Hosts[b[i].start:b[i].end] {
					want = append(want, row.Name)
				}
			case 1:
				for _, row := range content.Accounts {
					got = append(got, row.Account)
				}
				for _, row := range data.Accounts[b[i].start:b[i].end] {
					want = append(want, row.Slug)
				}
			case 2:
				for _, row := range content.Users {
					got = append(got, row.Account+"/"+row.User)
				}
				for _, row := range data.Users[b[i].start:b[i].end] {
					want = append(want, row.Account+"/"+row.Login)
				}
			}
			if !slices.Equal(got, want) || content.Next != b[i].next {
				t.Fatalf("%s tool identities and next = %v %q, want %v %q", name, got, content.Next, want, b[i].next)
			}
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			b[i] = bounds{paging.DefaultLimit, size, b[i].next, token(name+"-before", keyFor(paging.DefaultLimit, i, data)...), ""}
		}
	}
}

// R-UY9S-SWPB R-UZHP-6OG0 R-SWJU-KDAB R-K8CH-7HG3
func TestHostDetailsAndLatestReport(t *testing.T) {
	f := setup(t)
	st := f.cfg.Store
	h, op, err := st.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	target := "/hosts/" + h.Name
	b := banner(f, target, []page.Level{pages.HostLevel(h.Name)})
	view := pages.HostView{Name: h.Name, DNSName: h.Name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, Created: created, CreatedText: createdSecond, Allowlist: []string{}}
	same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "host", 200, pages.HostData{Banner: b, Host: view}))
	for _, slug := range []string{"alpha", "zulu"} {
		if _, _, err := st.PlaceAccount(t.Context(), slug, h.Name, caller); err != nil {
			t.Fatal(err)
		}
	}
	const instance, volume, eip = "i-chosen", "vol-chosen", "192.0.2.7"
	if err := st.Finish(t.Context(), op.ID, store.Update{InstanceID: instance, DataVolume: volume, EIP: eip}); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteHostRecord(t.Context(), h.Name, "", ops.ReasonRecordConflict); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"198.51.100.0/24", "192.0.2.7"} {
		if _, err := st.Allow(t.Context(), h.Name, entry); err != nil {
			t.Fatal(err)
		}
	}
	received := f.cfg.Now().Add(2*time.Minute + 2*time.Second)
	banned := received.In(time.FixedZone("test", 2*60*60))
	const reportedHostname = "chosen-host.reported.test"
	report := protocol.Report{
		Protocol: protocol.Version, Attempted: 2, Applied: 1, Error: "chosen apply error", InstanceID: "reported-instance",
		Hostname: reportedHostname, Type: "reported-type", AMI: "reported-ami", OS: "reported-os", Kernel: "reported-kernel", DaemonVersion: "reported-daemon", Booted: f.cfg.Now(),
		Load: [3]float64{1.21, 2.43, 3.65}, CPUPercent: 4.3, MemoryUsed: 101, MemoryAvailable: 102, SwapUsed: 103, SwapTotal: 104,
		Root: protocol.Disk{UsedBytes: 201, SizeBytes: 202, InodesUsed: 203, InodesTotal: 204},
		Data: protocol.Disk{UsedBytes: 301, SizeBytes: 302, InodesUsed: 303, InodesTotal: 304}, Sessions: 5, Updates: 6, Reboot: true,
		FailedUnits: []string{"chosen-z.service", "chosen-a.service"},
		Slices:      []protocol.Slice{{Account: "zulu", CPUPercent: 9.8, MemoryBytes: 410}, {Account: "alpha", CPUPercent: 8.7, MemoryBytes: 420}},
		Bans: []protocol.Ban{
			{Address: "203.0.113.9", Banned: banned, Expires: banned.Add(10 * time.Minute)},
			{Address: "192.0.2.9", Banned: banned.Add(-time.Minute), Expires: banned.Add(9 * time.Minute)},
		},
		BanTotal: 300, Allowlist: []string{"192.0.2.7"},
	}
	if _, err := st.RecordReport(t.Context(), h.Name, protocol.Report{Protocol: protocol.Version, OS: "old-os"}, f.cfg.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordReport(t.Context(), h.Name, report, received); err != nil {
		t.Fatal(err)
	}
	f.cfg.Now = func() time.Time { return received }
	recorded, err := st.Host(t.Context(), h.Name)
	if err != nil {
		t.Fatal(err)
	}
	view.State, view.Health, view.AccountCount = store.HostRunning, store.HealthError, 2
	view.Reason = ops.ReasonRecordConflict
	view.InstanceID, view.DataVolume, view.EIP = instance, volume, eip
	view.Allowlist = []string{"192.0.2.7", "198.51.100.0/24"}
	view.Reported, view.ReportedText = "2026-11-17T14:05:09Z", "2026-11-17 14:05"
	reportView := &pages.ReportView{
		Received: "2026-11-17T14:05:09Z", ReceivedText: "2026-11-17 14:05:09 UTC", Hostname: reportedHostname, InstanceID: "reported-instance", Type: "reported-type",
		AMI: "reported-ami", OS: "reported-os", Kernel: "reported-kernel", DaemonVersion: "reported-daemon", Booted: created, BootedText: createdSecond,
		Applied: 1, Wanted: recorded.Version, Error: "chosen apply error", Load1: 1.21, Load5: 2.43, Load15: 3.65, CPUPercent: 4.3,
		MemoryUsed: 101, MemoryAvailable: 102, SwapUsed: 103, SwapTotal: 104, Root: pages.VolumeView{UsedBytes: 201, SizeBytes: 202, InodesUsed: 203, InodesTotal: 204},
		Data: pages.VolumeView{UsedBytes: 301, SizeBytes: 302, InodesUsed: 303, InodesTotal: 304}, Sessions: 5, Updates: 6, Reboot: true,
		FailedUnits: []string{"chosen-z.service", "chosen-a.service"}, Slices: []pages.SliceRow{{Account: "alpha", CPUPercent: 8.7, MemoryBytes: 420}, {Account: "zulu", CPUPercent: 9.8, MemoryBytes: 410}},
		Bans: []pages.BanRow{
			{Address: "203.0.113.9", Banned: "2026-11-17T14:05:09Z", BannedText: "2026-11-17 14:05:09 UTC", Expires: "2026-11-17T14:15:09Z", ExpiresText: "2026-11-17 14:15:09 UTC"},
			{Address: "192.0.2.9", Banned: "2026-11-17T14:04:09Z", BannedText: "2026-11-17 14:04:09 UTC", Expires: "2026-11-17T14:14:09Z", ExpiresText: "2026-11-17 14:14:09 UTC"},
		},
		BanTotal: 300, Allowlist: []string{"192.0.2.7"},
	}
	for _, method := range []string{"GET", "HEAD"} {
		same(t, answer(f, method, target, "viewer"), expected(f, method, target, "host", 200, pages.HostData{Banner: b, Host: view, Report: reportView}))
	}
	empty, create, err := st.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), create.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	_, deleteOp, err := st.StartHostOp(t.Context(), empty.Name, store.OpDelete, store.Target{}, caller)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), deleteOp.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"/hosts/nope", "/hosts/%68ost01", "/hosts/" + empty.Name} {
		same(t, answer(f, "GET", missing, "viewer"), notFound(f, "GET", missing))
	}
	get, head := answer(f, "GET", target, "viewer"), answer(f, "HEAD", target, "viewer")
	if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
		t.Fatal("host HEAD differs from GET")
	}
	legacy := protocol.Report{Protocol: protocol.Version, Applied: report.Applied, Attempted: report.Attempted, Error: report.Error, OS: "old-os"}
	emptyView := &pages.ReportView{
		Received: reportView.Received, ReceivedText: reportView.ReceivedText, Applied: legacy.Applied, Wanted: recorded.Version,
		OS: legacy.OS, Error: legacy.Error, FailedUnits: []string{}, Slices: []pages.SliceRow{}, Bans: []pages.BanRow{}, Allowlist: []string{},
	}
	for _, total := range []int{0, 1, 517} {
		t.Run(fmt.Sprintf("no ban rows with total %d", total), func(t *testing.T) {
			emptyReport := legacy
			emptyReport.BanTotal = total
			if _, err := st.RecordReport(t.Context(), h.Name, emptyReport, received); err != nil {
				t.Fatal(err)
			}
			want := *emptyView
			want.BanTotal = total
			same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "host", 200, pages.HostData{Banner: b, Host: view, Report: &want}))
		})
	}
}
