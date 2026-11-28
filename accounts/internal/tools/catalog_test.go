package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type toolName string

var _ func(*mcp.Server, tools.Config) = tools.Register

type entry struct {
	name               toolName
	wire, description  string
	effect             mcp.Effect
	optional, required []string
	register           func(*mcp.Server)
}

func entryFor[A any](name toolName, wire, description string, effect mcp.Effect, optional, required []string) entry {
	return entry{
		name: name, wire: wire, description: description, effect: effect, optional: optional, required: required,
		register: func(srv *mcp.Server) {
			mcp.AddTool(srv, mcp.Tool[A, struct{}]{
				Name: wire, Description: description, Effect: effect,
				Handler: func(context.Context, identity.Caller, A) (struct{}, error) { return struct{}{}, nil },
			})
		},
	}
}

func catalog() []entry {
	pagingFields := []string{"page", "limit"}
	sizeFields := []string{"type", "root_gb", "data_gb"}
	host := []string{"host"}
	account := []string{"account"}
	user := []string{"account", "user"}
	key := []string{"account", "user", "key"}
	return []entry{
		entryFor[tools.HostsArgs](tools.ToolHosts, "hosts", tools.DescribeHosts, mcp.Read, pagingFields, nil),
		entryFor[tools.HostArgs](tools.ToolHost, "host", tools.DescribeHost, mcp.Read, nil, host),
		entryFor[tools.CreateHostArgs](tools.ToolCreateHost, "create_host", tools.DescribeCreateHost, mcp.Additive, sizeFields, nil),
		entryFor[tools.HostArgs](tools.ToolStopHost, "stop_host", tools.DescribeStopHost, mcp.Additive, nil, host),
		entryFor[tools.HostArgs](tools.ToolStartHost, "start_host", tools.DescribeStartHost, mcp.Additive, nil, host),
		entryFor[tools.ResizeHostArgs](tools.ToolResizeHost, "resize_host", tools.DescribeResizeHost, mcp.Additive, sizeFields, host),
		entryFor[tools.HostArgs](tools.ToolRetryHost, "retry_host", tools.DescribeRetryHost, mcp.Additive, nil, host),
		entryFor[tools.DeleteHostArgs](tools.ToolDeleteHost, "delete_host", tools.DescribeDeleteHost, mcp.Destructive, []string{"keep_data", "lost"}, host),
		entryFor[tools.KeptVolumesArgs](tools.ToolKeptVolumes, "kept_volumes", tools.DescribeKeptVolumes, mcp.Read, pagingFields, nil),
		entryFor[tools.KeptVolumeArgs](tools.ToolDeleteKeptVolume, "delete_kept_volume", tools.DescribeDeleteKeptVolume, mcp.Destructive, nil, []string{"volume"}),
		entryFor[tools.AccountsArgs](tools.ToolAccounts, "accounts", tools.DescribeAccounts, mcp.Read, pagingFields, nil),
		entryFor[tools.AccountArgs](tools.ToolAccount, "account", tools.DescribeAccount, mcp.Read, nil, account),
		entryFor[tools.CreateAccountArgs](tools.ToolCreateAccount, "create_account", tools.DescribeCreateAccount, mcp.Additive, nil, []string{"account", "host"}),
		entryFor[tools.AccountArgs](tools.ToolRemoveAccount, "remove_account", tools.DescribeRemoveAccount, mcp.Destructive, nil, account),
		entryFor[tools.UsersArgs](tools.ToolUsers, "users", tools.DescribeUsers, mcp.Read, []string{"account", "page", "limit"}, nil),
		entryFor[tools.UserKeyArgs](tools.ToolAddUser, "add_user", tools.DescribeAddUser, mcp.Additive, nil, key),
		entryFor[tools.UserKeyArgs](tools.ToolAddKey, "add_key", tools.DescribeAddKey, mcp.Additive, nil, key),
		entryFor[tools.UserKeyArgs](tools.ToolRemoveKey, "remove_key", tools.DescribeRemoveKey, mcp.Destructive, nil, key),
		entryFor[tools.UserArgs](tools.ToolLockUser, "lock_user", tools.DescribeLockUser, mcp.Additive, nil, user),
		entryFor[tools.UserArgs](tools.ToolUnlockUser, "unlock_user", tools.DescribeUnlockUser, mcp.Additive, nil, user),
		entryFor[tools.UserArgs](tools.ToolDeleteUser, "delete_user", tools.DescribeDeleteUser, mcp.Destructive, nil, user),
		entryFor[tools.SetAdminArgs](tools.ToolSetAdmin, "set_admin", tools.DescribeSetAdmin, mcp.Additive, nil, []string{"account", "user", "admin"}),
		entryFor[tools.CreateInviteArgs](tools.ToolCreateInvite, "create_invite", tools.DescribeCreateInvite, mcp.Additive, []string{"admin"}, account),
		entryFor[tools.AccountArgs](tools.ToolListInvites, "list_invites", tools.DescribeListInvites, mcp.Read, nil, account),
		entryFor[tools.InviteArgs](tools.ToolRevokeInvite, "revoke_invite", tools.DescribeRevokeInvite, mcp.Destructive, nil, []string{"invite"}),
		entryFor[tools.UnbanArgs](tools.ToolUnbanIP, "unban_ip", tools.DescribeUnbanIP, mcp.Additive, host, []string{"ip"}),
		entryFor[tools.HostIPArgs](tools.ToolAllowIP, "allow_ip", tools.DescribeAllowIP, mcp.Additive, nil, []string{"host", "ip"}),
		entryFor[tools.HostIPArgs](tools.ToolDisallowIP, "disallow_ip", tools.DescribeDisallowIP, mcp.Destructive, nil, []string{"host", "ip"}),
		entryFor[tools.HostArgs](tools.ToolCloseHost, "close_host", tools.DescribeCloseHost, mcp.Additive, nil, host),
		entryFor[tools.HostArgs](tools.ToolOpenHost, "open_host", tools.DescribeOpenHost, mcp.Additive, nil, host),
		entryFor[tools.ReplaceHostArgs](tools.ToolReplaceHost, "replace_host", tools.DescribeReplaceHost, mcp.Destructive, sizeFields, []string{"host", "from"}),
		entryFor[tools.BackupStatusArgs](tools.ToolBackupStatus, "backup_status", tools.DescribeBackupStatus, mcp.Read, []string{"host", "page", "limit"}, nil),
		entryFor[tools.AccountArgs](tools.ToolRestorePoints, "restore_points", tools.DescribeRestorePoints, mcp.Read, nil, account),
		entryFor[tools.RestoreDatabaseArgs](tools.ToolRestoreDatabase, "restore_database", tools.DescribeRestoreDatabase, mcp.Additive, nil, []string{"account", "path", "as_of"}),
		entryFor[tools.RestoreFilesArgs](tools.ToolRestoreFiles, "restore_files", tools.DescribeRestoreFiles, mcp.Additive, []string{"user", "path"}, []string{"account", "snapshot"}),
		entryFor[tools.AccountArgs](tools.ToolDeleteAccountBackups, "delete_account_backups", tools.DescribeDeleteAccountBackups, mcp.Destructive, nil, account),
		entryFor[tools.TeamTemplateArgs](tools.ToolTeamTemplate, "team_template", tools.DescribeTeamTemplate, mcp.Read, nil, nil),
		entryFor[tools.SetTeamTemplateArgs](tools.ToolSetTeamTemplate, "set_team_template", tools.DescribeSetTeamTemplate, mcp.Additive, []string{"url", "ref"}, nil),
	}
}

type schema struct {
	Type, Description string
	Properties        json.RawMessage
	Required          []string
	Items             *schema
}

func objectMembers(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	brace, err := dec.Token()
	if err != nil || brace != json.Delim('{') {
		t.Fatalf("not an object: %s (%v)", raw, err)
	}
	var names []string
	for dec.More() {
		name, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		member, ok := name.(string)
		if !ok {
			t.Fatal(name)
		}
		names = append(names, member)
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			t.Fatal(err)
		}
	}
	return names
}

func checkRequired(t *testing.T, s schema) {
	t.Helper()
	if len(s.Properties) != 0 {
		var children map[string]schema
		if err := json.Unmarshal(s.Properties, &children); err != nil {
			t.Fatal(err)
		}
		var required []string
		for _, n := range objectMembers(t, s.Properties) {
			switch n {
			case "next":
				if children[n].Description != paging.NextDescription {
					t.Fatal(children[n])
				}
			case "operation", "report":
			default:
				required = append(required, n)
			}
			if n != "report" {
				checkRequired(t, children[n])
			}
		}
		if !slices.Equal(s.Required, required) {
			t.Fatalf("required %v, want %v in %s", s.Required, required, s.Properties)
		}
	}
	if s.Items != nil {
		checkRequired(t, *s.Items)
	}
}

// R-51DJ-TTH0 R-YDET-CHO2 R-YEMP-Q9ER R-CL1V-ZGDI R-W1FG-7NCE R-CNHO-QZUW R-COPL-4RLL R-CTL6-NUKD R-CUT3-1MB2 R-CW0Z-FE1R R-Z7CV-COS5 R-Z8KR-QGIU R-ZDGD-9JHM R-0SA4-9OIR R-0TI0-NG9G
// R-GQUU-CO4E R-GS2Q-QFV3 R-HMK8-APEG R-HNS4-OH55 R-HP01-28VU R-HQ7X-G0MJ R-HF8U-02YA R-HGGQ-DUOZ R-HHOM-RMFO R-0YDM-6J88 R-YFUM-415G R-0UPX-1805
// R-VSW5-J95J R-VU41-X0W8 R-VXRR-2C4B R-W8QU-I9SK
func TestCatalogAndSchemas(t *testing.T) {
	const (
		_ = tools.DescribeHosts
		_ = tools.DescribeHost
		_ = tools.DescribeCreateHost
		_ = tools.DescribeStopHost
		_ = tools.DescribeStartHost
		_ = tools.DescribeResizeHost
		_ = tools.DescribeRetryHost
		_ = tools.DescribeDeleteHost
		_ = tools.DescribeKeptVolumes
		_ = tools.DescribeDeleteKeptVolume
		_ = tools.DescribeAccounts
		_ = tools.DescribeAccount
		_ = tools.DescribeCreateAccount
		_ = tools.DescribeRemoveAccount
		_ = tools.DescribeUsers
		_ = tools.DescribeAddUser
		_ = tools.DescribeAddKey
		_ = tools.DescribeRemoveKey
		_ = tools.DescribeLockUser
		_ = tools.DescribeUnlockUser
		_ = tools.DescribeDeleteUser
		_ = tools.DescribeSetAdmin
		_ = tools.DescribeCreateInvite
		_ = tools.DescribeListInvites
		_ = tools.DescribeRevokeInvite
		_ = tools.DescribeUnbanIP
		_ = tools.DescribeAllowIP
		_ = tools.DescribeDisallowIP
		_ = tools.DescribeCloseHost
		_ = tools.DescribeOpenHost
		_ = tools.DescribeReplaceHost
		_ = tools.DescribeBackupStatus
		_ = tools.DescribeRestorePoints
		_ = tools.DescribeRestoreDatabase
		_ = tools.DescribeRestoreFiles
		_ = tools.DescribeDeleteAccountBackups
		_ = tools.ToolTeamTemplate
		_ = tools.ToolSetTeamTemplate
		_ = tools.DescribeTeamTemplate
		_ = tools.DescribeSetTeamTemplate
		_ = tools.ArgFrom
		_ = tools.ArgLost
		_ = tools.ArgPath
		_ = tools.ArgAsOf
		_ = tools.ArgSnapshot
	)
	f := setup(t)
	f.serveLoopback()
	_ = struct {
		IP   string
		Host *string
	}(tools.UnbanArgs{})
	_ = struct{ Host, IP string }(tools.HostIPArgs{})
	list, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	registered, want := f.srv.Tools(), catalog()
	client := schemaProbeClient(t, f.w, want)
	wantSchema, err := client.ListTools(t.Context(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(want) || len(registered) != len(want) || len(wantSchema) != len(want) {
		t.Fatal(len(list), len(registered))
	}
	for i, info := range list {
		expected := want[i]
		if string(expected.name) != expected.wire || info.Name != expected.wire || registered[i].Name != expected.wire ||
			info.Description != expected.description || registered[i].Description != expected.description ||
			tools.FirstLine(expected.description) == "" || info.Effect() != expected.effect {
			t.Fatalf("catalog entry %+v (%+v)", info, registered[i])
		}
		if !bytes.Equal(info.InputSchema, wantSchema[i].InputSchema) {
			t.Fatalf("%s input schema %s, want %s", info.Name, info.InputSchema, wantSchema[i].InputSchema)
		}
		checkInput(t, info, expected)
		var out schema
		if err := json.Unmarshal(info.OutputSchema, &out); err != nil {
			t.Fatal(err)
		}
		checkRequired(t, out)
	}
}

func schemaProbeClient(t *testing.T, writer *telemetry.Writer, entries []entry) *mcp.Client {
	t.Helper()
	probe := mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: writer})
	for _, tool := range entries {
		tool.register(probe)
	}
	server := httptest.NewServer(identity.Require(probe))
	t.Cleanup(server.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL})
}

// R-W8QU-I9SK
func TestMCPArgumentValidation(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	host, _ := f.seedHost(true)
	accountHost, _ := f.seedHost(true)
	const account, username = "acme", "daffy"
	f.seedAccount(account, accountHost.Name)
	answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: account, User: username, Key: key(1)}))
	answer[tools.UserChange](t, f.call(tools.ToolSetAdmin, tools.SetAdminArgs{Account: account, User: username, Admin: true}))
	deletedHost, _ := f.seedHost(true)
	volume := f.finishKeeping(f.host(deletedHost.Name)).Volume
	beforeHost, beforeUser, beforeVolume := f.host(host.Name), f.user(account, username), f.keptOnRecord(volume)
	beforeTemplate, err := f.st.TemplateSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entries := catalog()
	probe := schemaProbeClient(t, f.w, entries)
	for _, tool := range entries {
		t.Run(tool.wire, func(t *testing.T) {
			args := validArgs(tool)
			if _, ok := args["host"]; ok {
				args["host"] = host.Name
			}
			if _, ok := args["volume"]; ok {
				args["volume"] = volume
			}
			extra := maps.Clone(args)
			extra["unknown"] = true
			assertAppkitRefusal(t, f, probe, tool.wire, extra)

			for _, n := range slices.Concat(tool.required, tool.optional) {
				wrong := maps.Clone(args)
				wrong[n] = []string{"wrong"}
				assertAppkitRefusal(t, f, probe, tool.wire, wrong)
				switch n {
				case "admin":
					wrong[n] = "true"
					assertAppkitRefusal(t, f, probe, tool.wire, wrong)
				case "keep_data", "lost":
					wrong[n] = "false"
					assertAppkitRefusal(t, f, probe, tool.wire, wrong)
				case "ref":
					wrong[n] = 2
					assertAppkitRefusal(t, f, probe, tool.wire, wrong)
				}
			}
			if tool.wire == tools.ToolTeamTemplate {
				assertAppkitRefusal(t, f, probe, tool.wire, map[string]any{"ref": "v2"})
			}
			for _, n := range tool.required {
				missing := maps.Clone(args)
				delete(missing, n)
				assertAppkitRefusal(t, f, probe, tool.wire, missing)
			}
		})
	}
	if len(f.cloud.lookups) != 0 || len(f.cloud.backupLookups) != 0 || f.wakes != 0 {
		t.Fatalf("invalid calls made %d DNS requests, %d backup requests and %d wake calls", len(f.cloud.lookups), len(f.cloud.backupLookups), f.wakes)
	}

	d, err := db.Open(t.Context(), db.Config{
		Path: f.path, Migrations: accounts.Migrations(), Now: func() time.Time { return f.now }, Service: "accounts",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	reopened := store.New(d, store.Config{Now: func() time.Time { return f.now }})
	if after, err := reopened.Host(t.Context(), host.Name); err != nil || !reflect.DeepEqual(after, beforeHost) {
		t.Errorf("host after invalid calls: %+v, want %+v (%v)", after, beforeHost, err)
	}
	if after, err := reopened.User(t.Context(), account, username); err != nil || !reflect.DeepEqual(after, beforeUser) {
		t.Errorf("user after invalid calls: %+v, want %+v (%v)", after, beforeUser, err)
	}
	if after, err := reopened.KeptVolume(t.Context(), volume); err != nil || after != beforeVolume {
		t.Errorf("kept volume after invalid calls: %+v, want %+v (%v)", after, beforeVolume, err)
	}
	if after, err := reopened.TemplateSettings(t.Context()); err != nil || after != beforeTemplate {
		t.Errorf("team template after invalid calls: %+v, want %+v (%v)", after, beforeTemplate, err)
	}
}

func assertAppkitRefusal(t *testing.T, f *fixture, probe *mcp.Client, tool string, args map[string]any) {
	t.Helper()
	wire, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	before := len(allEvents(t, f))
	want, err := probe.CallTool(t.Context(), f.caller, tool, wire)
	if err != nil {
		t.Fatal(err)
	}
	if !want.IsError() {
		t.Fatalf("appkit accepted invalid %s args %s", tool, wire)
	}
	probeEvents := allEvents(t, f)[before:]
	got, err := f.client.CallTool(t.Context(), f.caller, tool, wire)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := want.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := got.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("%s args %s: got %s, want %s", tool, wire, gotJSON, wantJSON)
	}
	gotEvents := allEvents(t, f)[before+len(probeEvents):]
	// The probe's call outcome identifies an appkit refusal before the handler runs.
	if !reflect.DeepEqual(gotEvents, probeEvents) {
		t.Fatalf("%s args %s: captured events %+v, want %+v", tool, wire, gotEvents, probeEvents)
	}
}

func checkInput(t *testing.T, info mcp.ToolInfo, expected entry) {
	t.Helper()
	copyByField := map[string]string{
		"page": paging.PageDescription, "limit": paging.LimitDescription,
		"host": tools.ArgHost, "account": tools.ArgAccount, "user": tools.ArgUser, "ip": tools.ArgIP,
		"key": tools.ArgKey, "admin": tools.ArgAdmin, "invite": tools.ArgInvite, "type": tools.ArgType, "root_gb": tools.ArgRootGB, "data_gb": tools.ArgDataGB,
		"keep_data": tools.ArgKeepData, "volume": tools.ArgVolume, "lost": tools.ArgLost,
		"from": tools.ArgFrom, "path": tools.ArgPath, "as_of": tools.ArgAsOf, "snapshot": tools.ArgSnapshot,
		"url": tools.ArgURL, "ref": tools.ArgRef,
	}
	var in schema
	if err := json.Unmarshal(info.InputSchema, &in); err != nil {
		t.Fatal(err)
	}
	fields := slices.Concat(expected.required, expected.optional)
	if expected.wire == tools.ToolRestoreFiles {
		fields = []string{"account", "user", "path", "snapshot"}
	}
	if len(fields) == 0 && len(in.Properties) == 0 && len(in.Required) == 0 {
		return
	}
	if !slices.Equal(objectMembers(t, in.Properties), fields) || !slices.Equal(in.Required, expected.required) {
		t.Fatalf("input schema %s", info.InputSchema)
	}
	var properties map[string]struct {
		Type        json.RawMessage
		Description string
	}
	if err := json.Unmarshal(in.Properties, &properties); err != nil {
		t.Fatal(err)
	}
	for _, n := range fields {
		kind := "string"
		switch n {
		case "admin", "keep_data", "lost":
			kind = "boolean"
		case "limit", "root_gb", "data_gb":
			kind = "integer"
		}
		expectedType := `"` + kind + `"`
		if properties[n].Description != copyByField[n] || string(properties[n].Type) != expectedType {
			t.Fatalf("unexpected schema for %s.%s: %s", info.Name, n, info.InputSchema)
		}
	}
}

func validArgs(expected entry) map[string]any {
	args := map[string]any{}
	for _, n := range slices.Concat(expected.required, expected.optional) {
		switch n {
		case "host":
			args[n] = "host01"
		case "account":
			args[n] = "acme"
		case "user":
			args[n] = "daffy"
		case "key":
			args[n] = key(1)
		case "ip":
			args[n] = "192.0.2.7"
		case "admin", "keep_data", "lost":
			args[n] = false
		case "invite":
			args[n] = store.InvitePrefix + "0000000000000000"
		case "page":
			args[n] = ""
		case "limit":
			args[n] = 25
		case "root_gb":
			args[n] = valid.DefaultRootGB
		case "data_gb":
			args[n] = valid.DefaultDataGB
		case "type":
			args[n] = valid.DefaultType
		case "volume":
			args[n] = "vol-example"
		case "from":
			args[n] = store.ReplaceBackup
		case "path":
			args[n] = "data/test.db"
		case "as_of", "snapshot":
			args[n] = "2026-10-01T00:00:00Z"
		case "url":
			args[n] = "https://git.example.test/template"
		case "ref":
			args[n] = "v2"
		}
	}
	return args
}

// R-CDQH-OTXC R-YIAE-VKMU R-YJIB-9CDJ R-YKQ7-N448 R-YLY4-0VUX R-YN60-ENLM
// R-GS2Q-QFV3 R-0YDM-6J88 R-HMK8-APEG R-HNS4-OH55 R-HP01-28VU R-HQ7X-G0MJ
// R-VXRR-2C4B
func TestInputTypes(t *testing.T) {
	t.Helper()
	// Conversion to an anonymous struct checks the declared field names, order and types.
	_ = struct {
		Store       *store.Store
		Cloud       cloud.Cloud
		Hosts       settings.Hosts
		Configured  bool
		Wake        func()
		Telemetry   *telemetry.Writer
		AccountsURL func() (string, error)
	}(tools.Config{})
	_ = struct {
		Page  *string
		Limit *int
	}(tools.HostsArgs{})
	_ = struct {
		Page  *string
		Limit *int
	}(tools.AccountsArgs{})
	_ = struct {
		Account, Page *string
		Limit         *int
	}(tools.UsersArgs{})
	_ = struct{ Host string }(tools.HostArgs{})
	_ = struct {
		Host     string
		KeepData *bool
		Lost     *bool
	}(tools.DeleteHostArgs{})
	_ = struct {
		Host, From     string
		Type           *string
		RootGB, DataGB *int
	}(tools.ReplaceHostArgs{})
	_ = struct {
		Host, Page *string
		Limit      *int
	}(tools.BackupStatusArgs{})
	_ = struct{ Account, Path, AsOf string }(tools.RestoreDatabaseArgs{})
	_ = struct {
		Account    string
		User, Path *string
		Snapshot   string
	}(tools.RestoreFilesArgs{})
	_ = struct {
		Page  *string
		Limit *int
	}(tools.KeptVolumesArgs{})
	_ = struct{ Volume string }(tools.KeptVolumeArgs{})
	_ = struct{ Account string }(tools.AccountArgs{})
	_ = struct{ Account, Host string }(tools.CreateAccountArgs{})
	_ = struct {
		Type           *string
		RootGB, DataGB *int
	}(tools.CreateHostArgs{})
	_ = struct {
		Host           string
		Type           *string
		RootGB, DataGB *int
	}(tools.ResizeHostArgs{})
	_ = struct{ Account, User string }(tools.UserArgs{})
	_ = struct{}(tools.TeamTemplateArgs{})
	_ = struct{ URL, Ref *string }(tools.SetTeamTemplateArgs{})
	_ = struct{ Account, User, Key string }(tools.UserKeyArgs{})
	_ = struct {
		Account, User string
		Admin         bool
	}(tools.SetAdminArgs{})
	_ = struct {
		Account string
		Admin   *bool
	}(tools.CreateInviteArgs{})
	_ = struct{ Invite string }(tools.InviteArgs{})
}
