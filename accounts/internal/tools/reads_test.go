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

// R-Y2FP-WJZT R-Z30P-DO8N R-1BGW-0TPL R-1COS-ELGA R-Z947-AIY4
func TestPaging(t *testing.T) {
	f := setup(t)
	if got := answer[tools.AccountList](t, f.call(tools.ToolAccounts, tools.AccountsArgs{})); got.Accounts == nil || len(got.Accounts) != 0 || got.Next != nil {
		t.Fatalf("empty account window %+v", got)
	}
	var wantHosts []string
	for i := 1; i <= 105; i++ {
		f.seedHost(i == 2)
		if i != 2 {
			wantHosts = append(wantHosts, fmt.Sprintf("host%02d", i))
		}
	}
	_, deleted, err := f.st.DeleteHost(t.Context(), "host02", true, false, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	f.finish(deleted.ID, store.Update{})
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

// R-11PO-YNS1 R-HBL4-URQ7 R-16LA-HQQT R-HCT1-8JGW R-HAD8-GZZI
func TestReadAnswers(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	ctx := context.Background()
	h, op := f.seedHost(false)
	created := f.now.UTC().Format(time.RFC3339)
	provisioning := readHost(t, f, h.Name)
	dnsName := h.Name + "." + f.hosts.Domain
	want := tools.HostRead{
		Host: h.Name, DNSName: dnsName, State: store.HostProvisioning, Health: store.Health(h, f.now), Type: h.Type,
		RootGB: h.RootGB, DataGB: h.DataGB, Version: h.Version, Created: created, CreatedBy: f.caller.UserID,
		AccountNames: []string{}, Allowlist: []string{}, Operation: &tools.Operation{Operation: op.ID, Kind: store.OpCreate, Status: store.OpRunning, Started: created},
	}
	if !reflect.DeepEqual(provisioning, want) {
		t.Fatalf("provisioning %+v, want %+v", provisioning, want)
	}
	const recordReason = "DNS record already exists"
	if err := f.st.NoteHostRecord(ctx, h.Name, "", recordReason); err != nil {
		t.Fatal(err)
	}
	f.fail(op.ID, "provisioning failed")
	failed := readHost(t, f, h.Name)
	if failed.State != store.HostFailed || failed.DNSName != dnsName || failed.Reason != "provisioning failed" ||
		failed.Operation == nil || failed.Operation.Status != store.OpFailed || failed.Operation.Error != failed.Reason {
		t.Fatalf("failed host %+v", failed)
	}
	if listed := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{})); len(listed.Hosts) != 1 ||
		listed.Hosts[0].State != store.HostFailed || listed.Hosts[0].Reason != failed.Reason || listed.Hosts[0].DNSName != dnsName {
		t.Fatalf("failed host list %+v", listed)
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
		Protocol: protocol.Version, Applied: h.Version, Attempted: h.Version, InstanceID: h.InstanceID, Hostname: "chosen.report.test",
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
	if running.Operation != nil || running.State != store.HostRunning || running.DNSName != dnsName || running.Reason != recordReason || running.InstanceID != h.InstanceID ||
		running.Applied != h.Version || running.Version != h.Version || running.DataVolume != h.DataVolume || running.EIP != h.EIP ||
		running.Accounts != 2 || !slices.Equal(running.AccountNames, []string{"acme", "beta"}) ||
		!slices.Equal(running.Allowlist, []string{"192.0.2.7", "198.51.100.0/24"}) ||
		running.Reported != f.now.Format(time.RFC3339) || running.Health != store.Health(f.host(h.Name), f.now) {
		t.Fatalf("running host %+v", running)
	}
	fields := []string{"host", "dns_name", "state", "reason", "on_delete", "health", "type", "root_gb", "data_gb", "eip", "accounts", "reported", "closed", "replace",
		"instance_id", "data_volume", "version", "applied", "created", "created_by", "account_names", "allowlist", "report"}
	wireMembers(t, unpack(t, result).Structured, fields...)
	wantList := tools.Host{
		Host: h.Name, DNSName: dnsName, State: store.HostRunning, Reason: recordReason, Health: store.HealthSync, Type: h.Type,
		RootGB: h.RootGB, DataGB: h.DataGB, EIP: h.EIP, Accounts: 2, Reported: f.now.Format(time.RFC3339),
	}
	if listed := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{})); !slices.Equal(listed.Hosts, []tools.Host{wantList}) {
		t.Fatalf("running host list %+v, want %+v", listed, wantList)
	}
	if err := f.st.NoteHostRecord(ctx, h.Name, "", ""); err != nil {
		t.Fatal(err)
	}
	wantList.Reason = ""
	if cleared := readHost(t, f, h.Name); cleared.Reason != "" || cleared.State != store.HostRunning || cleared.DNSName != dnsName {
		t.Fatalf("host after clearing record reason %+v", cleared)
	}
	if listed := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{})); !slices.Equal(listed.Hosts, []tools.Host{wantList}) {
		t.Fatalf("host list after clearing record reason %+v, want %+v", listed, wantList)
	}

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
	deleteHost := answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: deleted.Name}))
	f.finish(deleteHost.Operation, store.Update{})
	refusal(t, f.call(tools.ToolHost, tools.HostArgs{Host: deleted.Name}), tools.NoHost)
}

// R-Z947-AIY4 R-2U44-D4IG
func TestReadOnlyEffects(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	h, _ := f.seedHost(true)
	a, place := f.seedAccount("acme", h.Name)
	f.finish(place.ID, store.Update{})
	backupReport(f, h.Name, protocol.AccountBackup{Account: a.Slug, Snapshot: f.now,
		Snapshots: []int64{f.now.Unix()}, DatabaseTotal: 1,
		Databases: []protocol.Database{{Path: "data/books.db", Oldest: f.now.Add(-time.Hour), Synced: f.now}}})
	if _, _, err := f.st.RequestRestore(ctx, store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreDatabase,
		Path: "data/books.db", AsOf: f.now.Add(-time.Minute)}, f.caller); err != nil {
		t.Fatal(err)
	}
	h = f.host(h.Name)
	original, err := f.st.Account(ctx, a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := f.st.BackupOf(ctx, a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	report, err := f.st.Report(ctx, h.Name)
	if err != nil {
		t.Fatal(err)
	}
	restores, total, err := f.st.RestoresOn(ctx, h.Name, 2)
	if err != nil || total != 1 {
		t.Fatalf("restores %+v, total %d: %v", restores, total, err)
	}
	at := len(allEvents(t, f))
	reads := []callCase{
		{tools.ToolHosts, tools.HostsArgs{}, ""},
		{tools.ToolHost, tools.HostArgs{Host: h.Name}, ""},
		{tools.ToolAccounts, tools.AccountsArgs{}, ""},
		{tools.ToolAccount, tools.AccountArgs{Account: a.Slug}, ""},
		{tools.ToolUsers, tools.UsersArgs{}, ""},
		{tools.ToolAccount, tools.AccountArgs{Account: "absent"}, tools.NoAccount},
		{tools.ToolBackupStatus, tools.BackupStatusArgs{Host: &h.Name}, ""},
		{tools.ToolRestorePoints, tools.AccountArgs{Account: a.Slug}, ""},
		{tools.ToolBackupStatus, map[string]any{"host": "absent"}, tools.NoHost},
		{tools.ToolRestorePoints, tools.AccountArgs{Account: "absent"}, tools.NoAccount},
	}
	for _, read := range reads {
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
	if got, err := f.st.BackupOf(ctx, a.Slug); err != nil || !reflect.DeepEqual(got, backup) {
		t.Fatalf("backup changed: %+v (%+v before): %v", got, backup, err)
	}
	if got, err := f.st.Report(ctx, h.Name); err != nil || !reflect.DeepEqual(got, report) {
		t.Fatalf("report changed: %+v (%+v before): %v", got, report, err)
	}
	if got, count, err := f.st.RestoresOn(ctx, h.Name, 2); err != nil || count != total || !reflect.DeepEqual(got, restores) {
		t.Fatalf("restores changed: %+v (%+v before): %v", got, restores, err)
	}
	if f.wakes != 0 || len(f.cloud.lookups)+len(f.cloud.backupLookups) != 0 {
		t.Fatalf("read invoked external effects: wakes %d, DNS %v, backups %v", f.wakes, f.cloud.lookups, f.cloud.backupLookups)
	}
	assertReadEvents(t, allEvents(t, f)[at:], len(reads))
}
