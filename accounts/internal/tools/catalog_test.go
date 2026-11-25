package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
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
}

func entryFor(name toolName, wire, description string, effect mcp.Effect, optional, required []string) entry {
	return entry{name: name, wire: wire, description: description, effect: effect, optional: optional, required: required}
}

func catalog() []entry {
	pagingFields := []string{"page", "limit"}
	sizeFields := []string{"type", "root_gb", "data_gb"}
	host := []string{"host"}
	account := []string{"account"}
	user := []string{"account", "user"}
	key := []string{"account", "user", "key"}
	return []entry{
		entryFor(tools.ToolHosts, "hosts", tools.DescribeHosts, mcp.Read, pagingFields, nil),
		entryFor(tools.ToolHost, "host", tools.DescribeHost, mcp.Read, nil, host),
		entryFor(tools.ToolCreateHost, "create_host", tools.DescribeCreateHost, mcp.Additive, sizeFields, nil),
		entryFor(tools.ToolStopHost, "stop_host", tools.DescribeStopHost, mcp.Additive, nil, host),
		entryFor(tools.ToolStartHost, "start_host", tools.DescribeStartHost, mcp.Additive, nil, host),
		entryFor(tools.ToolResizeHost, "resize_host", tools.DescribeResizeHost, mcp.Additive, sizeFields, host),
		entryFor(tools.ToolRetryHost, "retry_host", tools.DescribeRetryHost, mcp.Additive, nil, host),
		entryFor(tools.ToolDeleteHost, "delete_host", tools.DescribeDeleteHost, mcp.Destructive, nil, host),
		entryFor(tools.ToolAccounts, "accounts", tools.DescribeAccounts, mcp.Read, pagingFields, nil),
		entryFor(tools.ToolAccount, "account", tools.DescribeAccount, mcp.Read, nil, account),
		entryFor(tools.ToolCreateAccount, "create_account", tools.DescribeCreateAccount, mcp.Additive, nil, []string{"account", "host"}),
		entryFor(tools.ToolRemoveAccount, "remove_account", tools.DescribeRemoveAccount, mcp.Destructive, nil, account),
		entryFor(tools.ToolUsers, "users", tools.DescribeUsers, mcp.Read, []string{"account", "page", "limit"}, nil),
		entryFor(tools.ToolAddUser, "add_user", tools.DescribeAddUser, mcp.Additive, nil, key),
		entryFor(tools.ToolAddKey, "add_key", tools.DescribeAddKey, mcp.Additive, nil, key),
		entryFor(tools.ToolRemoveKey, "remove_key", tools.DescribeRemoveKey, mcp.Destructive, nil, key),
		entryFor(tools.ToolLockUser, "lock_user", tools.DescribeLockUser, mcp.Additive, nil, user),
		entryFor(tools.ToolUnlockUser, "unlock_user", tools.DescribeUnlockUser, mcp.Additive, nil, user),
		entryFor(tools.ToolDeleteUser, "delete_user", tools.DescribeDeleteUser, mcp.Destructive, nil, user),
		entryFor(tools.ToolSetAdmin, "set_admin", tools.DescribeSetAdmin, mcp.Additive, nil, []string{"account", "user", "admin"}),
		entryFor(tools.ToolCreateInvite, "create_invite", tools.DescribeCreateInvite, mcp.Additive, []string{"admin"}, account),
		entryFor(tools.ToolListInvites, "list_invites", tools.DescribeListInvites, mcp.Read, nil, account),
		entryFor(tools.ToolRevokeInvite, "revoke_invite", tools.DescribeRevokeInvite, mcp.Destructive, nil, []string{"invite"}),
		entryFor(tools.ToolUnbanIP, "unban_ip", tools.DescribeUnbanIP, mcp.Additive, host, []string{"ip"}),
		entryFor(tools.ToolAllowIP, "allow_ip", tools.DescribeAllowIP, mcp.Additive, nil, []string{"host", "ip"}),
		entryFor(tools.ToolDisallowIP, "disallow_ip", tools.DescribeDisallowIP, mcp.Destructive, nil, []string{"host", "ip"}),
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

// R-51DJ-TTH0 R-YDET-CHO2 R-YEMP-Q9ER R-YODW-SFCB R-CL1V-ZGDI R-CM9S-D847 R-CNHO-QZUW R-COPL-4RLL R-D0WK-YH0J R-CTL6-NUKD R-CUT3-1MB2 R-CW0Z-FE1R R-Z7CV-COS5 R-Z8KR-QGIU R-ZDGD-9JHM R-ZEO9-NB8B
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
	if len(list) != len(want) || len(registered) != len(want) {
		t.Fatal(len(list), len(registered))
	}
	for i, info := range list {
		expected := want[i]
		if string(expected.name) != expected.wire || info.Name != expected.wire || registered[i].Name != expected.wire ||
			info.Description != expected.description || registered[i].Description != expected.description ||
			tools.FirstLine(expected.description) == "" || info.Effect() != expected.effect {
			t.Fatalf("catalog entry %+v (%+v)", info, registered[i])
		}
		checkInput(t, info, expected)
		var out schema
		if err := json.Unmarshal(info.OutputSchema, &out); err != nil {
			t.Fatal(err)
		}
		checkRequired(t, out)

		valid := validArgs(expected)
		valid["unknown"] = true
		if !f.call(info.Name, valid).IsError() {
			t.Fatal("unknown member accepted", info.Name)
		}
		for n := range validArgs(expected) {
			wrong := validArgs(expected)
			wrong[n] = []string{"wrong"}
			if !f.call(info.Name, wrong).IsError() {
				t.Fatal("wrong member type accepted", info.Name, n)
			}
		}
		for _, n := range expected.required {
			missing := validArgs(expected)
			delete(missing, n)
			if !f.call(info.Name, missing).IsError() {
				t.Fatal("missing required member accepted", info.Name, n)
			}
		}
	}
	if len(f.changes()) != 0 || len(f.cloud.lookups) != 0 || f.wakes != 0 {
		t.Fatal("invalid argument reached a handler")
	}
}

func checkInput(t *testing.T, info mcp.ToolInfo, expected entry) {
	t.Helper()
	copyByField := map[string]string{
		"page": paging.PageDescription, "limit": paging.LimitDescription,
		"host": tools.ArgHost, "account": tools.ArgAccount, "user": tools.ArgUser, "ip": tools.ArgIP,
		"key": tools.ArgKey, "admin": tools.ArgAdmin, "invite": tools.ArgInvite, "type": tools.ArgType, "root_gb": tools.ArgRootGB, "data_gb": tools.ArgDataGB,
	}
	var in schema
	if err := json.Unmarshal(info.InputSchema, &in); err != nil {
		t.Fatal(err)
	}
	fields := slices.Concat(expected.required, expected.optional)
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
		case "admin":
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
		case "ip":
			args[n] = "192.0.2.7"
		case "admin":
			args[n] = false
		case "invite":
			args[n] = store.InvitePrefix + "0000000000000000"
		case "page":
			args[n] = ""
		case "limit", "root_gb", "data_gb":
			args[n] = 25
		default:
			args[n] = "value"
		}
	}
	return args
}

// R-CDQH-OTXC R-YIAE-VKMU R-YJIB-9CDJ R-YKQ7-N448 R-YLY4-0VUX R-YN60-ENLM
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
