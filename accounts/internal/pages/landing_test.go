package pages_test

import (
	"cmp"
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

// R-CYA3-AG15 R-CZHZ-O7RU R-35EH-EJUU
func TestLandingRows(t *testing.T) {
	f := setup(t)
	data := pages.LandingData{Configured: true, Domain: f.cfg.Domain, Hosts: []pages.HostRow{}, KeptVolumes: []pages.KeptVolumeRow{}, Accounts: []pages.AccountRow{}, Users: []pages.UserRow{}}
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
		a, _, err := st.PlaceAccount(t.Context(), fmt.Sprintf("account-%03d", n-i-1), h.Name, caller)
		if err != nil {
			t.Fatal(err)
		}
		logins := []string{fmt.Sprintf("user-%03d", i+1)}
		switch {
		case i == 0 && n > 1:
			logins = []string{"user-002", "user-001"}
		case i == 1:
			logins = nil
		}
		for _, login := range logins {
			u, err := st.AddUser(t.Context(), a.Slug, login, key, caller)
			if err != nil {
				t.Fatal(err)
			}
			out.Users = append(out.Users, pages.UserRow{Login: u.Name, Account: a.Slug, State: store.UserActive, KeyCount: 1, Pending: true})
		}
		name := fmt.Sprintf("host%02d", i+1)
		out.Hosts = append(out.Hosts, pages.HostRow{Name: name, DNSName: name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, AccountCount: 1})
		out.Accounts = append(out.Accounts, pages.AccountRow{Slug: a.Slug, Host: h.Name, DNSName: a.Slug + "." + f.cfg.Domain, State: store.AccountPending, UserCount: len(logins), Created: created, CreatedText: createdMinute})
	}
	slices.SortFunc(out.Accounts, func(a, b pages.AccountRow) int { return cmp.Compare(a.Slug, b.Slug) })
	slices.SortFunc(out.Users, func(a, b pages.UserRow) int {
		return cmp.Or(cmp.Compare(a.Account, b.Account), cmp.Compare(a.Login, b.Login))
	})
	for i := range n {
		if i%2 == 0 {
			f.advance(time.Second)
		}
		// Later pairs have smaller prefixes, and ties alternate which id is inserted first.
		pair := i / 2
		volume := fmt.Sprintf("vol-%03d-%d", n-pair, (i+pair)%2)
		out.KeptVolumes = append(out.KeptVolumes, createKeptVolume(t, f, volume))
	}
	slices.SortFunc(out.KeptVolumes, func(a, b pages.KeptVolumeRow) int {
		return cmp.Or(cmp.Compare(b.Deleted, a.Deleted), cmp.Compare(b.Volume, a.Volume))
	})
	return out
}

func createRunningHost(t *testing.T, f *fixture, volume string) store.Host {
	t.Helper()
	h, op, err := f.cfg.Store.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Store.Finish(t.Context(), op.ID, store.Update{DataVolume: volume}); err != nil {
		t.Fatal(err)
	}
	return h
}

func completeKeptHost(t *testing.T, f *fixture, h store.Host, volume string) pages.KeptVolumeRow {
	t.Helper()
	_, op, err := f.cfg.Store.DeleteHost(t.Context(), h.Name, true, caller)
	if err != nil {
		t.Fatal(err)
	}
	deleted := f.cfg.Now().UTC()
	if err := f.cfg.Store.Finish(t.Context(), op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	return pages.KeptVolumeRow{
		Volume: volume, Host: h.Name, SizeGB: h.DataGB,
		Deleted: deleted.Format(time.RFC3339), DeletedText: deleted.Format(pages.MinuteLayout),
	}
}

func createKeptVolume(t *testing.T, f *fixture, volume string) pages.KeptVolumeRow {
	t.Helper()
	return completeKeptHost(t, f, createRunningHost(t, f, volume), volume)
}

// R-2WV6-Q5NZ R-31QS-98MR R-32YO-N0DG R-3F5O-GPSE R-3GDK-UHJ3 R-35EH-EJUU
func TestLandingChoicesAndKeptVolumeRows(t *testing.T) {
	f := setup(t)
	st := f.cfg.Store
	first := createRunningHost(t, f, "vol-first")
	second, _, err := st.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	earliest := completeKeptHost(t, f, first, "vol-first")
	if _, err := st.DeleteKeptVolume(t.Context(), earliest.Volume, caller); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Second)
	removed := createKeptVolume(t, f, "vol-removed")
	if _, err := st.DeleteKeptVolume(t.Context(), removed.Volume, caller); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishVolumeDelete(t.Context(), removed.Volume); err != nil {
		t.Fatal(err)
	}
	reused, _, err := st.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Name != first.Name {
		t.Fatalf("fixture host name = %q, want reused %q", reused.Name, first.Name)
	}
	f.advance(3*time.Minute + 2*time.Second)
	latest := createRunningHost(t, f, "vol-last")
	if latest.Name == first.Name {
		t.Fatalf("fixture has only one historical host name: %q", latest.Name)
	}
	completeKeptHost(t, f, latest, "vol-last")
	wantLatest := pages.KeptVolumeRow{Volume: "vol-last", Host: latest.Name, SizeGB: hostSpec.DataGB, Deleted: "2026-11-17T14:06:10Z", DeletedText: "2026-11-17 14:06"}
	data := pages.LandingData{
		Configured: true, Domain: f.cfg.Domain, KeptVolumes: []pages.KeptVolumeRow{wantLatest, earliest},
		Hosts: []pages.HostRow{
			{Name: second.Name, DNSName: second.Name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB},
			{Name: reused.Name, DNSName: reused.Name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB},
		},
	}
	for _, keepData := range []bool{true, false} {
		h := createRunningHost(t, f, "")
		if _, _, err := st.DeleteHost(t.Context(), h.Name, keepData, caller); err != nil {
			t.Fatal(err)
		}
		choice := store.OnDeleteDelete
		if keepData {
			choice = store.OnDeleteKeep
		}
		row := pages.HostRow{Name: h.Name, DNSName: h.Name + "." + f.cfg.Domain, State: store.HostDeleting, OnDelete: choice, Health: store.HealthNone, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB}
		data.Hosts = append(data.Hosts, row)
		target := "/hosts/" + h.Name
		createdAt := f.cfg.Now().UTC()
		view := pages.HostView{
			Name: row.Name, DNSName: row.DNSName, State: row.State, OnDelete: choice, Health: row.Health,
			Type: row.Type, RootGB: row.RootGB, DataGB: row.DataGB, Created: createdAt.Format(time.RFC3339), CreatedText: createdAt.Format(pages.SecondLayout),
			Allowlist: []string{},
		}
		b := banner(f, target, []page.Level{pages.HostLevel(h.Name)})
		same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "host", 200, pages.HostData{Banner: b, Host: view}))
	}
	checkLanding(t, f, "/", data)
}

type bounds struct {
	start, end        int
	value, prev, next string
}

func renderListWindows(data pages.LandingData, b [4]bounds) pages.LandingData {
	data.Hosts = data.Hosts[b[0].start:b[0].end]
	data.Accounts = data.Accounts[b[1].start:b[1].end]
	data.Users = data.Users[b[2].start:b[2].end]
	data.KeptVolumes = data.KeptVolumes[b[3].start:b[3].end]
	data.HostsPage, data.HostsPrev, data.HostsNext = b[0].value, b[0].prev, b[0].next
	data.AccountsPage, data.AccountsPrev, data.AccountsNext = b[1].value, b[1].prev, b[1].next
	data.UsersPage, data.UsersPrev, data.UsersNext = b[2].value, b[2].prev, b[2].next
	data.VolumesPage, data.VolumesPrev, data.VolumesNext = b[3].value, b[3].prev, b[3].next
	return data
}

var listNames = [4]string{"hosts", "accounts", "users", "kept_volumes"}
var listParams = [4]string{"hosts_page", "accounts_page", "users_page", "volumes_page"}

func keyFor(t *testing.T, row int, list int, data pages.LandingData) []any {
	t.Helper()
	switch list {
	case 0:
		return []any{int64(row + 1)}
	case 1:
		return []any{data.Accounts[row].Slug}
	case 2:
		return []any{data.Users[row].Account, data.Users[row].Login}
	default:
		volume := data.KeptVolumes[row]
		stamp, err := time.Parse(time.RFC3339, volume.Deleted)
		if err != nil {
			t.Fatal(err)
		}
		return []any{stamp.UnixNano(), volume.Volume}
	}
}

func pageURL(b [4]bounds) string {
	query := url.Values{}
	for i, param := range listParams {
		if b[i].value != "" {
			query.Set(param, b[i].value)
		}
	}
	if len(query) == 0 {
		return "/"
	}
	return "/?" + query.Encode()
}

func firstWindows(t *testing.T, rows pages.LandingData, size int) [4]bounds {
	t.Helper()
	var b [4]bounds
	for i, name := range listNames {
		b[i].end = min(size, paging.DefaultLimit)
		if size > paging.DefaultLimit {
			b[i].next = token(name, keyFor(t, b[i].end-1, i, rows)...)
		}
	}
	return b
}

// R-2WV6-Q5NZ R-2Y33-3XEO R-2ZAZ-HP5D R-30IV-VGW2 R-346L-0S45 R-3BHZ-BEKB R-3CPV-P6B0 R-3DXS-2Y1P
func TestIndependentPaging(t *testing.T) {
	f := setup(t)
	const l = paging.DefaultLimit
	const size = 4*l + 2
	data := createListRows(t, f, size)
	initial := firstWindows(t, data, size)
	checkLanding(t, f, "/", renderListWindows(data, initial))
	for i, name := range listNames {
		t.Run(name, func(t *testing.T) {
			beforeToken := func(row int) string { return token(name+"-before", keyFor(t, row, i, data)...) }
			afterToken := func(row int) string { return token(name, keyFor(t, row, i, data)...) }
			b := initial
			// Preserve one other list's position throughout this walk.
			other := (i + 2) % len(b)
			b[other] = bounds{l, 2 * l, initial[other].next, token(listNames[other]+"-before", keyFor(t, l, other, data)...), token(listNames[other], keyFor(t, 2*l-1, other, data)...)}
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
			// A position beyond the end is a key with no matching entry.
			positions := [4][]any{{int64(size + 7)}, {"zzz"}, {"zzz", "nonexistent"}, {int64(0), "vol-nonexistent"}}
			b[i].value = token(name+"-before", positions[i]...)
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			b[i] = bounds{size, size, token(name, positions[i]...), "", ""}
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
		})
	}
	hosts := token("hosts", int64(13))
	users := token("users", "account-003", "nonexistent")
	volumes := token("kept_volumes", keyFor(t, l-1, 3, data)...)
	target := "/?ignored=one&volumes_page=" + url.QueryEscape(volumes) + "&users_page=" + url.QueryEscape(users) + "&accounts_page=" + url.QueryEscape(token("accounts-before", "a")) + "&hosts_page=" + url.QueryEscape(hosts)
	canonical := "/?hosts_page=" + url.QueryEscape(hosts) + "&users_page=" + url.QueryEscape(users) + "&volumes_page=" + url.QueryEscape(volumes)
	same(t, answer(f, "GET", target, "viewer"), statusWithHeader(303, "Location", canonical))

	accounts := token("accounts", "account-003")
	volumesUnderflow := token("kept_volumes-before", keyFor(t, l, 3, data)...)
	target = "/?users_page=" + url.QueryEscape(users) + "&volumes_page=" + url.QueryEscape(volumesUnderflow) + "&hosts_page=" + url.QueryEscape(hosts) + "&accounts_page=" + url.QueryEscape(accounts)
	canonical = "/?hosts_page=" + url.QueryEscape(hosts) + "&accounts_page=" + url.QueryEscape(accounts) + "&users_page=" + url.QueryEscape(users)
	for _, method := range []string{"GET", "HEAD"} {
		same(t, answer(f, method, target, "viewer"), statusWithHeader(303, "Location", canonical))
	}
	checkLanding(t, f, "/?volumes_page=&volumes_page=bogus", renderListWindows(data, initial))

	empty := setup(t)
	fullFirstPage := setup(t)
	full := createListRows(t, fullFirstPage, l)
	checkLanding(t, fullFirstPage, "/", renderListWindows(full, firstWindows(t, full, l)))
	target = "/?hosts_page=last&accounts_page=last&users_page=last&volumes_page=last&ignored=out"
	for _, pageFixture := range []*fixture{empty, fullFirstPage} {
		for _, method := range []string{"GET", "HEAD"} {
			same(t, answer(pageFixture, method, target, "viewer"), statusWithHeader(303, "Location", "/"))
		}
	}
}

// R-2ZAZ-HP5D R-30IV-VGW2
func TestLandingVolumePagingWithMissingPosition(t *testing.T) {
	f := setup(t)
	const limit = paging.DefaultLimit
	const count = limit + 3
	data := createListRows(t, f, count)
	// Removing a row from the second window leaves its key between retained entries.
	const removed = limit + 1
	key := keyFor(t, removed, 3, data)
	volume := data.KeptVolumes[removed].Volume
	st := f.cfg.Store
	if _, err := st.DeleteKeptVolume(t.Context(), volume, caller); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishVolumeDelete(t.Context(), volume); err != nil {
		t.Fatal(err)
	}
	data.KeptVolumes = slices.Delete(data.KeptVolumes, removed, removed+1)
	b := firstWindows(t, data, count)
	b[3] = bounds{1, removed, token("kept_volumes-before", key...), token("kept_volumes-before", keyFor(t, 1, 3, data)...), token("kept_volumes", keyFor(t, removed-1, 3, data)...)}
	checkLanding(t, f, pageURL(b), renderListWindows(data, b))
	b[3] = bounds{removed, count - 1, token("kept_volumes", key...), token("kept_volumes-before", keyFor(t, removed, 3, data)...), ""}
	checkLanding(t, f, pageURL(b), renderListWindows(data, b))
}

// R-37UA-63C8 holds a list's position fixed across changes in the other lists.
func TestUsersWindowIgnoresOtherPositions(t *testing.T) {
	f := setup(t)
	const l = paging.DefaultLimit
	const size = 4*l + 2
	data := createListRows(t, f, size)
	b := firstWindows(t, data, size)
	b[2] = bounds{l, 2 * l, b[2].next, token("users-before", keyFor(t, l, 2, data)...), token("users", keyFor(t, 2*l-1, 2, data)...)}
	checkLanding(t, f, pageURL(b), renderListWindows(data, b))
	for _, direction := range []string{"after", "before", "last"} {
		for _, i := range []int{0, 1, 3} {
			name := listNames[i]
			switch direction {
			case "after":
				b[i] = bounds{l, 2 * l, token(name, keyFor(t, l-1, i, data)...), token(name+"-before", keyFor(t, l, i, data)...), token(name, keyFor(t, 2*l-1, i, data)...)}
			case "before":
				b[i] = bounds{l, 2 * l, token(name+"-before", keyFor(t, 2*l, i, data)...), token(name+"-before", keyFor(t, l, i, data)...), token(name, keyFor(t, 2*l-1, i, data)...)}
			case "last":
				b[i] = bounds{size - l, size, "last", token(name+"-before", keyFor(t, size-l, i, data)...), ""}
			}
		}
		checkLanding(t, f, pageURL(b), renderListWindows(data, b))
	}
}

func pageToolClient(t *testing.T, f *fixture) *mcp.Client {
	t.Helper()
	tools.Register(f.cfg.MCP, tools.Config{Store: f.cfg.Store, Cloud: struct{ cloud.Cloud }{}, Hosts: settings.SandboxHosts, Configured: true, Wake: func() {}, Telemetry: f.writer})
	srv := httptest.NewServer(identity.Require(f.cfg.MCP))
	t.Cleanup(srv.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
}

func pageToolContent[T any](t *testing.T, client *mcp.Client, name string, args any) T {
	t.Helper()
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
		Content T `json:"structuredContent"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Content
}

// R-35EH-EJUU R-36MD-SBLJ
func TestToolAndPageWindowsAgree(t *testing.T) {
	f := setup(t)
	const size = paging.DefaultLimit + 1
	data := createListRows(t, f, size)
	client := pageToolClient(t, f)
	initial := firstWindows(t, data, size)
	for i, name := range listNames {
		b := initial
		for start := 0; start < size; start += paging.DefaultLimit {
			args := map[string]string{}
			if b[i].value != "" {
				args["page"] = b[i].value
			}
			content := pageToolContent[struct {
				Hosts       []struct{ Host string }
				Accounts    []struct{ Account string }
				Users       []struct{ Account, User string }
				KeptVolumes []tools.KeptVolume `json:"kept_volumes"`
				Next        string
			}](t, client, name, args)
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
			case 3:
				for _, row := range content.KeptVolumes {
					got = append(got, fmt.Sprintf("%s %s %d %s", row.Volume, row.Host, row.SizeGB, row.Deleted))
				}
				for _, row := range data.KeptVolumes[b[i].start:b[i].end] {
					want = append(want, fmt.Sprintf("%s %s %d %s", row.Volume, row.Host, row.SizeGB, row.Deleted))
				}
			}
			if !slices.Equal(got, want) || content.Next != b[i].next {
				t.Fatalf("%s tool rows and next = %v %q, want %v %q", name, got, content.Next, want, b[i].next)
			}
			checkLanding(t, f, pageURL(b), renderListWindows(data, b))
			b[i] = bounds{paging.DefaultLimit, size, b[i].next, token(name+"-before", keyFor(t, paging.DefaultLimit, i, data)...), ""}
		}
	}
}

// R-9QOG-KJ8S
func TestKeptVolumeDeletionRangeThroughPagesAndTool(t *testing.T) {
	f := setup(t)
	times := []time.Time{
		time.Date(2261, time.December, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1678, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	data := pages.LandingData{
		Configured: true, Domain: f.cfg.Domain, Hosts: []pages.HostRow{}, Accounts: []pages.AccountRow{}, Users: []pages.UserRow{},
		KeptVolumes: make([]pages.KeptVolumeRow, len(times)),
	}
	volumes := [3]string{"vol-range-latest", "vol-range-middle", "vol-range-earliest"}
	var hosts [3]store.Host
	for _, i := range []int{2, 0, 1} {
		hosts[i] = createRunningHost(t, f, volumes[i])
	}
	// Finish deletes out of sequence so neither host order nor insertion order gives the expected window.
	for _, i := range []int{1, 0, 2} {
		f.setClock(times[i])
		data.KeptVolumes[i] = completeKeptHost(t, f, hosts[i], volumes[i])
	}
	checkLanding(t, f, "/", data)
	for _, i := range []int{0, len(times) - 1} {
		value := token(tools.ToolKeptVolumes, times[i].UnixNano(), data.KeptVolumes[i].Volume)
		b := [4]bounds{{}, {}, {}, {start: i + 1, end: len(times), value: value}}
		if i+1 < len(times) {
			b[3].prev = token(tools.ToolKeptVolumes+"-before", times[i+1].UnixNano(), data.KeptVolumes[i+1].Volume)
		}
		checkLanding(t, f, pageURL(b), renderListWindows(data, b))
	}

	client := pageToolClient(t, f)
	var entries []tools.KeptVolume
	for i, row := range data.KeptVolumes {
		entries = append(entries, tools.KeptVolume{Volume: row.Volume, Host: row.Host, SizeGB: row.SizeGB, State: store.VolumeKept, Deleted: times[i].Format(time.RFC3339)})
	}
	if got := pageToolContent[tools.KeptVolumeList](t, client, tools.ToolKeptVolumes, tools.KeptVolumesArgs{}); !slices.Equal(got.KeptVolumes, entries) || got.Next != nil {
		t.Fatalf("kept volumes = %+v, want %v with no next page", got, entries)
	}
	one := 1
	args := tools.KeptVolumesArgs{Limit: &one}
	for i, entry := range entries {
		got := pageToolContent[tools.KeptVolumeList](t, client, tools.ToolKeptVolumes, args)
		var next *string
		if i+1 < len(entries) {
			value := token(tools.ToolKeptVolumes, times[i].UnixNano(), entry.Volume)
			next = &value
		}
		want := tools.KeptVolumeList{KeptVolumes: []tools.KeptVolume{entry}, Next: next}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tool window %d = %+v, want %+v", i, got, want)
		}
		args.Page = got.Next
	}
}

// R-3F5O-GPSE R-3GDK-UHJ3 R-SWJU-KDAB R-K8CH-7HG3
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
	if _, err := st.Allow(t.Context(), empty.Name, "192.0.2.19"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordReport(t.Context(), empty.Name, protocol.Report{Protocol: protocol.Version, Hostname: "earlier-host.work.test"}, received); err != nil {
		t.Fatal(err)
	}
	_, deleteOp, err := st.DeleteHost(t.Context(), empty.Name, true, caller)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), deleteOp.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"/hosts/nope", "/hosts/%68ost01", "/hosts/" + empty.Name} {
		same(t, answer(f, "GET", missing, "viewer"), notFound(f, "GET", missing))
	}
	reused, _, err := st.CreateHost(t.Context(), hostSpec, caller)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Name != empty.Name {
		t.Fatalf("fixture host name = %q, want %q", reused.Name, empty.Name)
	}
	reusedPath := "/hosts/" + reused.Name
	reusedBanner := banner(f, reusedPath, []page.Level{pages.HostLevel(reused.Name)})
	reusedView := pages.HostView{
		Name: reused.Name, DNSName: reused.Name + "." + f.cfg.Domain, State: store.HostProvisioning, Health: store.HealthNone,
		Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, Created: created, CreatedText: createdSecond, Allowlist: []string{},
	}
	same(t, answer(f, "GET", reusedPath, "viewer"), expected(f, "GET", reusedPath, "host", 200, pages.HostData{Banner: reusedBanner, Host: reusedView}))
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
