package tools_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

type hostShape = struct {
	Host, DNSName, State, Reason, OnDelete, Health, Type string
	RootGB, DataGB                                       int
	EIP                                                  string
	Accounts                                             int
	Reported, Closed, Replace                            string
}
type accountShape = struct {
	Account, Host, DNSName, State, Reason string
	Users, Invites                        int
	Created, CreatedBy                    string
}

// R-GLZ8-TL5M R-0JQT-LABW R-W2NC-LF33 R-W3V8-Z6TS R-VYZN-G3V0
func TestOutputTypes(t *testing.T) {
	t.Helper()
	_ = hostShape(tools.Host{})
	_ = struct {
		Hosts []tools.Host
		Next  *string
	}(tools.HostList{})
	_ = struct{ Operation, Kind, Step, Status, Error, Started string }(tools.Operation{})
	_ = struct{ Host, State, OnDelete, Operation string }(tools.HostChange{})
	_ = struct {
		Host, State, Operation string
		Restarts               bool
	}(tools.HostResize{})
	_ = accountShape(tools.Account{})
	_ = struct {
		Accounts []tools.Account
		Next     *string
	}(tools.AccountList{})
	_ = struct {
		Account, Host, DNSName, State, Reason string
		Users, Invites                        int
		Created, CreatedBy, Template          string
		UserNames                             []string
		Operation                             *tools.Operation
	}(tools.AccountRead{})
	_ = struct {
		User, Account                            string
		Admin                                    bool
		State                                    string
		Pending                                  bool
		Keys                                     []string
		LastSignIn, Created, CreatedBy, Template string
	}(tools.User{})
	_ = struct {
		Users []tools.User
		Next  *string
	}(tools.UserList{})
	_ = struct {
		User, Account string
		Admin         bool
		State         string
		Pending       bool
	}(tools.UserChange{})
	_ = struct {
		User, Account string
		Admin         bool
		State         string
		Pending       bool
		KeysLeft      int
	}(tools.KeyRemoved{})
	_ = struct{ URL, Ref string }(tools.TeamTemplate{})
}

func wireMembers(t *testing.T, raw json.RawMessage, names ...string) {
	t.Helper()
	if got := objectMembers(t, raw); !slices.Equal(got, names) {
		t.Fatalf("members %v, want %v in %s", got, names, raw)
	}
}

func field(t *testing.T, raw json.RawMessage, name string) json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	return obj[name]
}

func testEncoding[R any](t *testing.T, value R) json.RawMessage {
	t.Helper()
	f := setup(t)
	f.serveLoopback()
	mcp.AddTool(f.srv, mcp.Tool[struct{}, R]{
		Name: "probe", Description: "Probe encoding.", Effect: mcp.Read,
		Handler: func(context.Context, identity.Caller, struct{}) (R, error) { return value, nil },
	})
	result := f.call("probe", map[string]any{})
	answer[R](t, result)
	return unpack(t, result).Structured
}

func wireArray(t *testing.T, obj json.RawMessage, name string, members ...string) {
	t.Helper()
	raw := field(t, obj, name)
	if len(members) == 0 {
		if string(raw) != "[]" {
			t.Fatalf("%s should be an empty array, got %s", name, raw)
		}
		return
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) != 1 {
		t.Fatalf("%s should contain one stylized answer, got %s (%v)", name, raw, err)
	}
	wireMembers(t, items[0], members...)
}

// R-GLZ8-TL5M R-YS1L-XQKE R-0JQT-LABW R-W2NC-LF33 R-W3V8-Z6TS R-CG6A-GDEQ R-XTWF-85SY R-VYZN-G3V0
func TestOutputEncoding(t *testing.T) {
	hostFields := []string{"host", "dns_name", "state", "reason", "on_delete", "health", "type", "root_gb", "data_gb", "eip", "accounts", "reported", "closed", "replace"}
	accountFields := []string{"account", "host", "dns_name", "state", "reason", "users", "invites", "created", "created_by"}
	userFields := []string{"user", "account", "admin", "state", "pending", "keys", "last_sign_in", "created", "created_by", "template"}
	opFields := []string{"operation", "kind", "step", "status", "error", "started"}
	next := "next-page"
	hosts := testEncoding(t, tools.HostList{Hosts: []tools.Host{{}}, Next: &next})
	wireMembers(t, hosts, "hosts", "next")
	wireArray(t, hosts, "hosts", hostFields...)
	accounts := testEncoding(t, tools.AccountList{Accounts: []tools.Account{{}}, Next: &next})
	wireMembers(t, accounts, "accounts", "next")
	wireArray(t, accounts, "accounts", accountFields...)
	users := testEncoding(t, tools.UserList{Users: []tools.User{{}}, Next: &next})
	wireMembers(t, users, "users", "next")
	wireArray(t, users, "users", userFields...)
	var usersWire []json.RawMessage
	if err := json.Unmarshal(field(t, users, "users"), &usersWire); err != nil {
		t.Fatal(err)
	}
	if string(field(t, usersWire[0], "admin")) != "false" {
		t.Fatalf("admin should be a JSON boolean, got %s", usersWire[0])
	}
	wireArray(t, usersWire[0], "keys")
	for _, raw := range []json.RawMessage{hosts, accounts, users} {
		encodedNext, err := json.Marshal(next)
		if err != nil || string(field(t, raw, "next")) != string(encodedNext) {
			t.Fatalf("bad next in %s", raw)
		}
	}
	for _, rows := range [][]tools.Host{nil, {}} {
		emptyHosts := testEncoding(t, tools.HostList{Hosts: rows})
		wireMembers(t, emptyHosts, "hosts")
		wireArray(t, emptyHosts, "hosts")
	}
	emptyAccounts := testEncoding(t, tools.AccountList{})
	wireMembers(t, emptyAccounts, "accounts")
	wireArray(t, emptyAccounts, "accounts")
	emptyUsers := testEncoding(t, tools.UserList{})
	wireMembers(t, emptyUsers, "users")
	wireArray(t, emptyUsers, "users")

	accountReadFields := slices.Concat(accountFields, []string{"template", "user_names"})
	emptyAccount := testEncoding(t, tools.AccountRead{})
	wireMembers(t, emptyAccount, accountReadFields...)
	wireArray(t, emptyAccount, "user_names")
	account := testEncoding(t, tools.AccountRead{Operation: &tools.Operation{}})
	wireMembers(t, account, append(accountReadFields, "operation")...)
	wireMembers(t, field(t, account, "operation"), opFields...)

	wireMembers(t, testEncoding(t, tools.HostChange{}), "host", "state", "on_delete", "operation")
	wireMembers(t, testEncoding(t, tools.HostResize{}), "host", "state", "operation", "restarts")
	wireMembers(t, testEncoding(t, tools.UserChange{}), "user", "account", "admin", "state", "pending")
	wireMembers(t, testEncoding(t, tools.KeyRemoved{}), "user", "account", "admin", "state", "pending", "keys_left")
	wireMembers(t, testEncoding(t, tools.TeamTemplate{}), "url", "ref")

	f := setup(t)
	refusal(t, f.call(tools.ToolHost, tools.HostArgs{Host: "missing"}), tools.NoHost)
}

// R-GN75-7CWB
func TestHostReadEncoding(t *testing.T) {
	_ = struct {
		Host, DNSName, State, Reason, OnDelete, Health, Type string
		RootGB, DataGB                                       int
		EIP                                                  string
		Accounts                                             int
		Reported, Closed, Replace, InstanceID, DataVolume    string
		Version, Applied                                     int64
		Created, CreatedBy                                   string
		AccountNames, Allowlist                              []string
		Operation                                            *tools.Operation
		Report                                               json.RawMessage
	}(tools.HostRead{})
	base := []string{"host", "dns_name", "state", "reason", "on_delete", "health", "type", "root_gb", "data_gb", "eip", "accounts", "reported", "closed", "replace",
		"instance_id", "data_volume", "version", "applied", "created", "created_by", "account_names", "allowlist"}
	for _, names := range [][]string{nil, {}} {
		raw := testEncoding(t, tools.HostRead{AccountNames: names, Allowlist: names})
		wireMembers(t, raw, base...)
		wireArray(t, raw, "account_names")
		wireArray(t, raw, "allowlist")
	}
	op := tools.Operation{Operation: "op_example", Kind: store.OpCreate, Status: store.OpRunning}
	report := json.RawMessage(`{"z":7,"a":{"nested":true}}`)
	raw := testEncoding(t, tools.HostRead{AccountNames: []string{"chosen"}, Allowlist: []string{"192.0.2.7"}, Operation: &op, Report: report})
	wireMembers(t, raw, append(base, "operation", "report")...)
	var names []string
	if err := json.Unmarshal(field(t, raw, "account_names"), &names); err != nil || !slices.Equal(names, []string{"chosen"}) {
		t.Fatalf("account names = %v (%v)", names, err)
	}
	var allowlist []string
	if err := json.Unmarshal(field(t, raw, "allowlist"), &allowlist); err != nil || !slices.Equal(allowlist, []string{"192.0.2.7"}) {
		t.Fatalf("allowlist = %v (%v)", allowlist, err)
	}
	wireMembers(t, field(t, raw, "operation"), "operation", "kind", "step", "status", "error", "started")
	var got tools.Operation
	if err := json.Unmarshal(field(t, raw, "operation"), &got); err != nil || got != op {
		t.Fatalf("operation = %+v (%v), want %+v", got, err, op)
	}
	if got := field(t, raw, "report"); string(got) != string(report) {
		t.Fatalf("report = %s, want %s", got, report)
	}
}

// R-Y7BB-FMYL
func TestSuccessfulToolsUseDeclaredOutput(t *testing.T) {
	f := setup(t)
	h := answer[tools.HostChange](t, f.call(tools.ToolCreateHost, tools.CreateHostArgs{}))
	host := tools.HostArgs{Host: h.Host}
	place := tools.CreateAccountArgs{Account: "acme", Host: h.Host}
	account := tools.AccountArgs{Account: place.Account}
	user := tools.UserArgs{Account: place.Account, User: "daffy"}
	userKey := func(i byte) tools.UserKeyArgs {
		return tools.UserKeyArgs{Account: user.Account, User: user.User, Key: key(i)}
	}
	answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{}))
	answer[tools.HostRead](t, f.call(tools.ToolHost, host))
	answer[tools.Account](t, f.call(tools.ToolCreateAccount, place))
	answer[tools.AccountList](t, f.call(tools.ToolAccounts, tools.AccountsArgs{}))
	answer[tools.AccountRead](t, f.call(tools.ToolAccount, account))
	answer[tools.UserChange](t, f.call(tools.ToolAddUser, userKey(1)))
	answer[tools.UserChange](t, f.call(tools.ToolAddKey, userKey(2)))
	answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{}))
	answer[tools.KeyRemoved](t, f.call(tools.ToolRemoveKey, userKey(2)))
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, user))
	answer[tools.UserChange](t, f.call(tools.ToolUnlockUser, user))
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, user))
	answer[tools.UserChange](t, f.call(tools.ToolDeleteUser, user))
	remaining := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{}))
	if len(remaining.Users) != 1 || remaining.Users[0].User != user.User || remaining.Users[0].Account != user.Account ||
		remaining.Users[0].State != store.UserRemoving || !remaining.Users[0].Pending ||
		!slices.Equal(remaining.Users[0].Keys, []string{key(1)}) {
		t.Fatalf("removing user %+v", remaining)
	}
	details := readAccount(t, f, account.Account)
	if details.State != store.AccountPending || !slices.Equal(details.UserNames, []string{user.User}) {
		t.Fatal(details)
	}
	firstHost := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{}))
	if len(firstHost.Hosts) != 1 || firstHost.Hosts[0].Accounts != 1 {
		t.Fatal(firstHost)
	}
	refusal(t, f.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: host.Host}), tools.HostBusy)
	refusal(t, f.call(tools.ToolRemoveAccount, account), tools.AccountHasUsers)

	second, _ := f.seedHost(true)
	host.Host = second.Name
	place.Host, place.Account = second.Name, "beta"
	answer[tools.Account](t, f.call(tools.ToolCreateAccount, place))
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, tools.AccountArgs{Account: place.Account}))
	stopping := answer[tools.HostChange](t, f.call(tools.ToolStopHost, host))
	f.finish(stopping.Operation, store.Update{})
	starting := answer[tools.HostChange](t, f.call(tools.ToolStartHost, host))
	f.fail(starting.Operation, "start failed")
	retry := answer[tools.HostChange](t, f.call(tools.ToolRetryHost, host))
	f.finish(retry.Operation, store.Update{})
	typeName := "t4g.large"
	resize := answer[tools.HostResize](t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host, Type: &typeName}))
	f.finish(resize.Operation, store.Update{Type: typeName})
	answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: host.Host}))
}

// R-GOF1-L4N0
func TestKeptVolumeOutputEncoding(t *testing.T) {
	_ = struct {
		Volume, Host, Cause    string
		SizeGB                 int
		Deleted, State, Reason string
	}(tools.KeptVolume{})
	_ = struct {
		KeptVolumes []tools.KeptVolume
		Next        *string
	}(tools.KeptVolumeList{})

	const next = "next-page"
	nextPage := next
	volume := tools.KeptVolume{
		Volume: "vol-example", Host: "host-example", Cause: store.CauseReplaced, SizeGB: 43,
		Deleted: "2026-10-29T14:03:07Z", State: store.VolumeKept, Reason: "example reason",
	}
	list := testEncoding(t, tools.KeptVolumeList{KeptVolumes: []tools.KeptVolume{volume}, Next: &nextPage})
	wireMembers(t, list, "kept_volumes", "next")
	fields := []string{"volume", "host", "cause", "size_gb", "deleted", "state", "reason"}
	wireArray(t, list, "kept_volumes", fields...)
	if raw := field(t, list, "next"); string(raw) != `"next-page"` {
		t.Fatal(string(raw))
	}
	for _, vols := range [][]tools.KeptVolume{nil, {}} {
		empty := testEncoding(t, tools.KeptVolumeList{KeptVolumes: vols})
		wireMembers(t, empty, "kept_volumes")
		wireArray(t, empty, "kept_volumes")
	}
	one := testEncoding(t, volume)
	wireMembers(t, one, fields...)
	want, err := json.Marshal(volume)
	if err != nil || string(one) != string(want) {
		t.Fatalf("encoded volume = %s, want %s (%v)", one, want, err)
	}
}

// R-XAGL-KTXQ R-HSNQ-7K3X R-HTVM-LBUM R-HV3I-Z3LB
func TestBackupOutputEncoding(t *testing.T) {
	_ = struct {
		Account, Host, State, Health, Snapshot, Checked, Error                   string
		Databases, DatabaseErrors                                                int
		Synced, Written, Final, FinalAt, Restore, Reported, Delete, DeleteReason string
	}(tools.AccountBackups{})
	_ = struct {
		Accounts []tools.AccountBackups
		Next     *string
	}(tools.BackupStatusList{})
	_ = struct{ Path, Health, Oldest, Written, Synced, Error string }(tools.DatabasePoint{})
	_ = struct {
		Account, Host, Reported string
		Snapshots               []string
		Databases               []tools.DatabasePoint
		DatabaseTotal           int
	}(tools.RestorePoints{})
	_ = struct{ Restore, Account, Host, Kind, Path, User, AsOf, State string }(tools.RestoreMade{})
	_ = struct{ Account, State string }(tools.BackupsDeletion{})

	next := "answer-token"
	list := testEncoding(t, tools.BackupStatusList{Accounts: []tools.AccountBackups{{Account: "acme", Databases: 3, DatabaseErrors: 2}}, Next: &next})
	wireMembers(t, list, "accounts", "next")
	wireArray(t, list, "accounts", "account", "host", "state", "health", "snapshot", "checked", "error",
		"databases", "database_errors", "synced", "written", "final", "final_at", "restore", "reported", "delete", "delete_reason")
	if got := string(field(t, list, "next")); got != `"answer-token"` {
		t.Fatal(got)
	}
	for _, entries := range [][]tools.AccountBackups{nil, {}} {
		empty := testEncoding(t, tools.BackupStatusList{Accounts: entries})
		wireMembers(t, empty, "accounts")
		wireArray(t, empty, "accounts")
	}
	for _, points := range []tools.RestorePoints{{}, {Snapshots: []string{}, Databases: []tools.DatabasePoint{}}} {
		raw := testEncoding(t, points)
		wireMembers(t, raw, "account", "host", "reported", "snapshots", "databases", "database_total")
		wireArray(t, raw, "snapshots")
		wireArray(t, raw, "databases")
	}
	raw := testEncoding(t, tools.RestorePoints{Snapshots: []string{"one"}, Databases: []tools.DatabasePoint{{Path: "data/example"}}})
	wireArray(t, raw, "databases", "path", "health", "oldest", "written", "synced", "error")
	if got := string(field(t, raw, "snapshots")); got != `["one"]` {
		t.Fatal(got)
	}
	wireMembers(t, testEncoding(t, tools.RestoreMade{}), "restore", "account", "host", "kind", "path", "user", "as_of", "state")
	wireMembers(t, testEncoding(t, tools.BackupsDeletion{}), "account", "state")
}
