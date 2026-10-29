package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
)

// R-D7KA-T610
func TestToolDescriptionCopy(t *testing.T) {
	const (
		groups       string = tools.DescribeGroups
		group        string = tools.DescribeGroup
		createGroup  string = tools.DescribeCreateGroup
		removeGroup  string = tools.DescribeRemoveGroup
		users        string = tools.DescribeUsers
		addUser      string = tools.DescribeAddUser
		addKey       string = tools.DescribeAddKey
		removeKey    string = tools.DescribeRemoveKey
		addMember    string = tools.DescribeAddMember
		removeMember string = tools.DescribeRemoveMember
		lockUser     string = tools.DescribeLockUser
		unlockUser   string = tools.DescribeUnlockUser
		deleteUser   string = tools.DescribeDeleteUser
	)
	want := map[string]string{
		tools.ToolGroups: groups, tools.ToolGroup: group,
		tools.ToolCreateGroup: createGroup, tools.ToolRemoveGroup: removeGroup,
		tools.ToolUsers: users, tools.ToolAddUser: addUser,
		tools.ToolAddKey: addKey, tools.ToolRemoveKey: removeKey,
		tools.ToolAddMember: addMember, tools.ToolRemoveMember: removeMember,
		tools.ToolLockUser: lockUser, tools.ToolUnlockUser: unlockUser,
		tools.ToolDeleteUser: deleteUser,
	}
	f := setup(t)
	list, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range list {
		if description, ok := want[info.Name]; ok {
			if tools.FirstLine(description) == "" || info.Description != description {
				t.Fatalf("tool %s does not carry its description copy", info.Name)
			}
			delete(want, info.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing described tools: %v", want)
	}
}

// R-D8S7-6XRP
func TestArgumentDescriptionCopy(t *testing.T) {
	const (
		group string = tools.ArgGroup
		user  string = tools.ArgUser
		key   string = tools.ArgKey
	)
	want := map[string]string{"group": group, "user": user, "key": key}
	for _, description := range want {
		if description == "" || strings.Contains(description, "\n") {
			t.Fatalf("invalid argument description %q", description)
		}
	}
	f := setup(t)
	list, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, info := range list {
		var in schema
		if err := json.Unmarshal(info.InputSchema, &in); err != nil {
			t.Fatal(err)
		}
		for name, property := range in.Properties {
			if description, ok := want[name]; ok {
				if property.Description != description {
					t.Fatalf("%s member %s does not carry its description copy", info.Name, name)
				}
				seen[name] = true
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Fatalf("no input member %s", name)
		}
	}
}

// R-DA03-KPIE
func TestFailureCopy(t *testing.T) {
	const (
		failed     string = tools.Failed
		keysUnread string = tools.KeysUnread
	)
	for _, line := range []string{failed, keysUnread} {
		if line == "" || strings.Contains(line, "\n") || line == store.Unreachable {
			t.Fatalf("invalid failure copy %q", line)
		}
	}
	if failed == keysUnread {
		t.Fatal("failure copies are equal")
	}
	f := setup(t)
	f.h.err = errors.New("chosen host failure")
	refusal(t, f.call(tools.ToolCreateGroup, tools.GroupArgs{Group: "business"}), failed)
	f.h.err = nil
	if err := f.h.Host.GroupAdd(t.Context(), "business"); err != nil {
		t.Fatal(err)
	}
	publicKey := key(1)
	if err := f.h.Host.UserAdd(t.Context(), "person", "business", publicKey); err != nil {
		t.Fatal(err)
	}
	f.h.readErr = errors.New("chosen remaining-key read failure")
	refusal(t, f.call(tools.ToolRemoveKey, tools.KeyArgs{User: "person", Key: publicKey}), keysUnread)
	users, err := f.h.Host.UserList(t.Context())
	if err != nil || len(users) != 1 || len(users[0].Keys) != 0 {
		t.Fatalf("key removal did not stand: %v, %v", users, err)
	}
}

// R-D1GS-WBBJ
func TestRecordFailureCopy(t *testing.T) {
	f := setup(t)
	f.h.passthrough = true
	f.h.groups = []host.Group{{Slug: "business"}}
	f.h.users = []host.User{{Name: "person"}}
	f.h.members = &host.Members{Group: "business"}
	f.d.SetFailing(true)
	for _, tc := range []struct {
		name string
		args any
	}{
		{tools.ToolGroups, tools.GroupsArgs{}},
		{tools.ToolUsers, tools.UsersArgs{}},
		{tools.ToolGroup, tools.GroupArgs{Group: "business"}},
		{tools.ToolCreateGroup, tools.GroupArgs{Group: "business"}},
		{tools.ToolRemoveGroup, tools.GroupArgs{Group: "business"}},
		{tools.ToolAddUser, tools.AddUserArgs{User: "person", Group: "business", Key: "chosen key"}},
		{tools.ToolDeleteUser, tools.UserArgs{User: "person"}},
	} {
		refusal(t, f.call(tc.name, tc.args), store.Unreachable)
	}
}
