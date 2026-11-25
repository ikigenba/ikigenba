package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

func wantToken(t *testing.T, got *string, token paging.Token, fields ...any) {
	t.Helper()
	if got == nil || *got != token.Encode(fields...) {
		t.Fatalf("next=%v, want %q", got, token.Encode(fields...))
	}
}

// R-Y2FP-WJZT R-Z30P-DO8N R-F3B6-QGWI R-F4J3-48N7 R-Z947-AIY4
func TestPaging(t *testing.T) {
	f := setup(t)
	if got := answer[tools.AccountList](t, f.call(tools.ToolAccounts, tools.AccountsArgs{})); got.Accounts == nil || len(got.Accounts) != 0 || got.Next != nil {
		t.Fatalf("empty account window %+v", got)
	}
	var wantHosts []string
	for i := 1; i <= 105; i++ {
		h, _ := f.seedHost(i == 2)
		if i == 2 {
			_, op, err := f.st.StartHostOp(context.Background(), h.Name, store.OpDelete, store.Target{}, f.caller)
			if err != nil {
				t.Fatal(err)
			}
			f.finish(op.ID, store.Update{})
			continue
		}
		wantHosts = append(wantHosts, fmt.Sprintf("host%02d", i))
	}
	limit := 25
	var hosts []string
	token := paging.Token{Tool: tools.ToolHosts}
	for page := (*string)(nil); ; {
		got := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{Page: page, Limit: &limit}))
		for _, h := range got.Hosts {
			hosts = append(hosts, h.Host)
		}
		if got.Next == nil {
			break
		}
		wantToken(t, got.Next, token, int64(len(hosts)+1))
		page = got.Next
	}
	if !slices.Equal(hosts, wantHosts) {
		t.Fatalf("hosts %v, want %v", hosts, wantHosts)
	}
	refusal(t, f.call(tools.ToolHost, tools.HostArgs{Host: "host02"}), tools.NoHost)

	f.seedAccount("beta", "host01")
	f.seedAccount("acme", "host01")
	one := 1
	accounts := answer[tools.AccountList](t, f.call(tools.ToolAccounts, tools.AccountsArgs{Limit: &one}))
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Account != "acme" {
		t.Fatal(accounts)
	}
	wantToken(t, accounts.Next, paging.Token{Tool: tools.ToolAccounts}, "acme")
	last := answer[tools.AccountList](t, f.call(tools.ToolAccounts, tools.AccountsArgs{Limit: &one, Page: accounts.Next}))
	if len(last.Accounts) != 1 || last.Accounts[0].Account != "beta" || last.Next != nil {
		t.Fatal(last)
	}
	for _, u := range []struct{ account, name string }{{"acme", "daffy"}, {"beta", "elmer"}, {"acme", "bugs"}} {
		answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: u.account, User: u.name, Key: key(1)}))
	}
	changeCount := len(f.changes())
	empty := ""
	first := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{Page: &empty, Limit: &one}))
	if len(first.Users) != 1 || first.Users[0].User != "bugs" {
		t.Fatal(first)
	}
	wantToken(t, first.Next, paging.Token{Tool: tools.ToolUsers}, "acme", "bugs")
	next := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{Page: first.Next}))
	if len(next.Users) != 2 || next.Users[0].User != "daffy" || next.Users[1].User != "elmer" || next.Next != nil {
		t.Fatal(next)
	}
	beta := "beta"
	filtered := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{Account: &beta}))
	if len(filtered.Users) != 1 || filtered.Users[0].User != "elmer" || filtered.Next != nil {
		t.Fatal(filtered)
	}
	nulls := answer[tools.UserList](t, f.call(tools.ToolUsers, json.RawMessage(`{"account":null,"page":null,"limit":null}`)))
	if len(nulls.Users) != 3 {
		t.Fatal(nulls)
	}
	missing := "missing"
	refusal(t, f.call(tools.ToolUsers, tools.UsersArgs{Account: &missing}), tools.NoAccount)
	refusal(t, f.call(tools.ToolUsers, tools.UsersArgs{Account: &missing, Page: accounts.Next}), paging.BadPage)

	f.d.SetFailing(true)
	badPage := "bad-page"
	zero := 0
	_, wantErr := paging.Window(&zero)
	for _, name := range []string{tools.ToolHosts, tools.ToolAccounts, tools.ToolUsers} {
		refusal(t, f.call(name, tools.HostsArgs{Page: &badPage, Limit: &zero}), wantErr.Error())
		refusal(t, f.call(name, tools.HostsArgs{Page: &badPage}), paging.BadPage)
	}
	if got := len(f.changes()); got != changeCount || f.wakes != 0 || len(f.cloud.lookups) != 0 {
		t.Fatalf("reads had effects: changes=%d (%d before), wakes=%d, cloud=%+v", got, changeCount, f.wakes, f.cloud.lookups)
	}
}

func readHost(t *testing.T, f *fixture, host string) tools.HostRead {
	t.Helper()
	return answer[tools.HostRead](t, f.call(tools.ToolHost, tools.HostArgs{Host: host}))
}

func readAccount(t *testing.T, f *fixture, account string) tools.AccountRead {
	t.Helper()
	return answer[tools.AccountRead](t, f.call(tools.ToolAccount, tools.AccountArgs{Account: account}))
}

// R-EVZS-FUGC R-0V8U-N3U2 R-EX7O-TM71 R-F5QZ-I0DW R-F6YV-VS4L
func TestReadAnswers(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	ctx := context.Background()
	h, op := f.seedHost(false)
	created := f.now.UTC().Format(time.RFC3339)
	provisioning := readHost(t, f, h.Name)
	want := tools.HostRead{
		Host: h.Name, State: store.HostProvisioning, Health: store.Health(h, f.now), Type: h.Type,
		RootGB: h.RootGB, DataGB: h.DataGB, Version: h.Version, Created: created, CreatedBy: f.caller.UserID,
		AccountNames: []string{}, Allowlist: []string{}, Operation: &tools.Operation{Operation: op.ID, Kind: store.OpCreate, Status: store.OpRunning, Started: created},
	}
	if !reflect.DeepEqual(provisioning, want) {
		t.Fatalf("provisioning %+v, want %+v", provisioning, want)
	}
	f.fail(op.ID, "provisioning failed")
	failed := readHost(t, f, h.Name)
	if failed.State != store.HostFailed || failed.Reason != "provisioning failed" ||
		failed.Operation == nil || failed.Operation.Status != store.OpFailed || failed.Operation.Error != failed.Reason {
		t.Fatalf("failed host %+v", failed)
	}
	retry := answer[tools.HostChange](t, f.call(tools.ToolRetryHost, tools.HostArgs{Host: h.Name}))
	f.finish(retry.Operation, store.Update{InstanceID: "instance", DataVolume: "data-volume", EIP: "192.0.2.10"})
	acme, place := f.seedAccount("acme", h.Name)
	f.seedAccount("beta", h.Name)
	for _, name := range []string{"daffy", "bugs"} {
		answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: acme.Slug, User: name, Key: key(1)}))
	}
	answer[tools.UserChange](t, f.call(tools.ToolAddKey, tools.UserKeyArgs{Account: acme.Slug, User: "daffy", Key: key(0)}))
	f.fail(place.ID, "record conflict")
	for _, entry := range []string{"198.51.100.0/24", "192.0.2.7"} {
		if _, err := f.st.Allow(ctx, h.Name, entry); err != nil {
			t.Fatal(err)
		}
	}
	h = f.host(h.Name)
	f.now = f.now.Add(time.Minute).UTC().Truncate(time.Second)
	signIn := f.now.Add(-time.Second)
	body := protocol.Report{
		Protocol: protocol.Version, Applied: h.Version, Attempted: h.Version, InstanceID: h.InstanceID,
		Booted: f.now.Add(-2 * time.Hour), Load: [3]float64{0.25, 0.5, 0.75},
		SignIns:  []protocol.SignIn{{User: "daffy", At: signIn}},
		Bans:     []protocol.Ban{{Address: "203.0.113.9", Banned: signIn, Expires: signIn.Add(10 * time.Minute)}},
		BanTotal: 3, Allowlist: []string{"192.0.2.7"},
	}
	if _, err := f.st.RecordReport(ctx, h.Name, body, f.now); err != nil {
		t.Fatal(err)
	}
	result := f.call(tools.ToolHost, tools.HostArgs{Host: h.Name})
	running := answer[tools.HostRead](t, result)
	var report protocol.Report
	if err := json.Unmarshal(running.Report, &report); err != nil || !reflect.DeepEqual(report, body) {
		t.Fatalf("report %+v, want %+v (%v)", report, body, err)
	}
	if running.Operation != nil || running.State != store.HostRunning || running.InstanceID != h.InstanceID ||
		running.Applied != h.Version || running.Version != h.Version || running.DataVolume != h.DataVolume || running.EIP != h.EIP ||
		running.Accounts != 2 || !slices.Equal(running.AccountNames, []string{"acme", "beta"}) ||
		!slices.Equal(running.Allowlist, []string{"192.0.2.7", "198.51.100.0/24"}) ||
		running.Reported != f.now.Format(time.RFC3339) || running.Health != store.Health(f.host(h.Name), f.now) {
		t.Fatalf("running host %+v", running)
	}
	fields := []string{"host", "state", "reason", "health", "type", "root_gb", "data_gb", "eip", "accounts", "reported",
		"instance_id", "data_volume", "version", "applied", "created", "created_by", "account_names", "allowlist", "report"}
	wireMembers(t, unpack(t, result).Structured, fields...)

	a := readAccount(t, f, acme.Slug)
	if a.Reason != "record conflict" || a.State != store.AccountFailed || a.Account != acme.Slug || a.Host != h.Name ||
		a.DNSName != acme.Slug+"."+f.hosts.Domain || a.Users != 2 || a.Created != created || a.CreatedBy != f.caller.UserID ||
		!slices.Equal(a.UserNames, []string{"bugs", "daffy"}) || a.Operation == nil ||
		a.Operation.Operation != place.ID || a.Operation.Kind != store.OpPlace || a.Operation.Status != store.OpFailed || a.Operation.Error != a.Reason {
		t.Fatalf("failed account %+v", a)
	}
	listed := answer[tools.AccountList](t, f.call(tools.ToolAccounts, tools.AccountsArgs{}))
	if len(listed.Accounts) != 2 || listed.Accounts[0].Reason != a.Reason || listed.Accounts[1].Reason != "" {
		t.Fatal(listed)
	}
	list := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{}))
	keys := []string{key(0), key(1)}
	slices.Sort(keys)
	expected := tools.User{
		User: "daffy", Account: "acme", State: store.UserActive, Keys: keys,
		LastSignIn: signIn.Format(time.RFC3339), Created: created, CreatedBy: f.caller.UserID,
	}
	if len(list.Users) != 2 || !reflect.DeepEqual(list.Users[1], expected) {
		t.Fatalf("users %+v, want daffy %+v", list.Users, expected)
	}
	refusal(t, f.call(tools.ToolAccount, tools.AccountArgs{Account: "missing"}), tools.NoAccount)
	deleted, _ := f.seedHost(true)
	deleteHost := answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, tools.HostArgs{Host: deleted.Name}))
	f.finish(deleteHost.Operation, store.Update{})
	refusal(t, f.call(tools.ToolHost, tools.HostArgs{Host: deleted.Name}), tools.NoHost)
}

// R-Z947-AIY4
func TestReadOnlyEffects(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	h, _ := f.seedHost(true)
	a, place := f.seedAccount("acme", h.Name)
	f.finish(place.ID, store.Update{})
	h = f.host(h.Name)
	original, err := f.st.Account(ctx, a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	for _, read := range []callCase{
		{tools.ToolHosts, tools.HostsArgs{}, ""},
		{tools.ToolHost, tools.HostArgs{Host: h.Name}, ""},
		{tools.ToolAccounts, tools.AccountsArgs{}, ""},
		{tools.ToolAccount, tools.AccountArgs{Account: a.Slug}, ""},
		{tools.ToolUsers, tools.UsersArgs{}, ""},
		{tools.ToolAccount, tools.AccountArgs{Account: "absent"}, tools.NoAccount},
	} {
		res := f.call(read.name, read.args)
		if read.want != "" {
			refusal(t, res, read.want)
		} else if res.IsError() {
			t.Fatal(read.name, unpack(t, res))
		}
	}
	if got := f.host(h.Name); !reflect.DeepEqual(got, h) {
		t.Fatalf("host changed: %+v (%+v before)", got, h)
	}
	if got, err := f.st.Account(ctx, a.Slug); err != nil || got != original {
		t.Fatalf("account changed: %+v (%+v before): %v", got, original, err)
	}
	if f.wakes != 0 || len(f.cloud.lookups) != 0 {
		t.Fatalf("read invoked external effects: wakes %d, lookups %v", f.wakes, f.cloud.lookups)
	}
	if err := f.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	const appkitCall = "tool.called"
	events := f.capture.Events()
	if len(events) != 6 {
		t.Fatalf("unexpected read events %+v", events)
	}
	for _, event := range events {
		if event.Name != appkitCall {
			t.Fatalf("read emitted %+v", event)
		}
	}
}
