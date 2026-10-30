package tools

import (
	"context"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

// Register installs the thirteen tools in catalog order.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[GroupsArgs, GroupList]{Name: ToolGroups, Description: DescribeGroups, Effect: mcp.Read, Handler: cfg.groups})
	mcp.AddTool(srv, mcp.Tool[GroupArgs, GroupRead]{Name: ToolGroup, Description: DescribeGroup, Effect: mcp.Read, Handler: cfg.group})
	mcp.AddTool(srv, mcp.Tool[GroupArgs, GroupDone]{Name: ToolCreateGroup, Description: DescribeCreateGroup, Effect: mcp.Additive, Handler: func(ctx context.Context, c identity.Caller, a GroupArgs) (GroupDone, error) {
		err := cfg.change(ctx, ToolCreateGroup, a.Group, "", func() error { return cfg.Host.GroupAdd(ctx, a.Group) }, func() error { _, _, e := cfg.Store.AddGroup(ctx, a.Group, c.UserID); return e })
		return GroupDone(a), err
	}})
	mcp.AddTool(srv, mcp.Tool[GroupArgs, GroupDone]{Name: ToolRemoveGroup, Description: DescribeRemoveGroup, Effect: mcp.Destructive, Handler: func(ctx context.Context, _ identity.Caller, a GroupArgs) (GroupDone, error) {
		err := cfg.change(ctx, ToolRemoveGroup, a.Group, "", func() error { return cfg.Host.GroupRemove(ctx, a.Group) }, func() error { _, e := cfg.Store.RemoveGroup(ctx, a.Group); return e })
		return GroupDone(a), err
	}})
	mcp.AddTool(srv, mcp.Tool[UsersArgs, UserList]{Name: ToolUsers, Description: DescribeUsers, Effect: mcp.Read, Handler: cfg.users})
	mcp.AddTool(srv, mcp.Tool[AddUserArgs, MemberDone]{Name: ToolAddUser, Description: DescribeAddUser, Effect: mcp.Additive, Handler: func(ctx context.Context, c identity.Caller, a AddUserArgs) (MemberDone, error) {
		err := cfg.change(ctx, ToolAddUser, a.Group, a.User, func() error { return cfg.Host.UserAdd(ctx, a.User, a.Group, a.Key) }, func() error { _, _, e := cfg.Store.AddUser(ctx, a.User, c.UserID); return e })
		return MemberDone{User: a.User, Group: a.Group}, err
	}})
	mcp.AddTool(srv, mcp.Tool[KeyArgs, UserDone]{Name: ToolAddKey, Description: DescribeAddKey, Effect: mcp.Additive, Handler: func(ctx context.Context, _ identity.Caller, a KeyArgs) (UserDone, error) {
		return UserDone{User: a.User}, cfg.change(ctx, ToolAddKey, "", a.User, func() error { return cfg.Host.UserKeyAdd(ctx, a.User, a.Key) }, nil)
	}})
	mcp.AddTool(srv, mcp.Tool[KeyArgs, KeyRemoved]{Name: ToolRemoveKey, Description: DescribeRemoveKey, Effect: mcp.Destructive, Handler: cfg.removeKey})
	mcp.AddTool(srv, mcp.Tool[MemberArgs, MemberDone]{Name: ToolAddMember, Description: DescribeAddMember, Effect: mcp.Additive, Handler: func(ctx context.Context, _ identity.Caller, a MemberArgs) (MemberDone, error) {
		return MemberDone(a), cfg.change(ctx, ToolAddMember, a.Group, a.User, func() error { return cfg.Host.UserJoin(ctx, a.User, a.Group) }, nil)
	}})
	mcp.AddTool(srv, mcp.Tool[MemberArgs, MemberDone]{Name: ToolRemoveMember, Description: DescribeRemoveMember, Effect: mcp.Destructive, Handler: func(ctx context.Context, _ identity.Caller, a MemberArgs) (MemberDone, error) {
		return MemberDone(a), cfg.change(ctx, ToolRemoveMember, a.Group, a.User, func() error { return cfg.Host.UserLeave(ctx, a.User, a.Group) }, nil)
	}})
	mcp.AddTool(srv, mcp.Tool[UserArgs, UserDone]{Name: ToolLockUser, Description: DescribeLockUser, Effect: mcp.Additive, Handler: func(ctx context.Context, _ identity.Caller, a UserArgs) (UserDone, error) {
		return UserDone(a), cfg.change(ctx, ToolLockUser, "", a.User, func() error { return cfg.Host.UserLock(ctx, a.User) }, nil)
	}})
	mcp.AddTool(srv, mcp.Tool[UserArgs, UserDone]{Name: ToolUnlockUser, Description: DescribeUnlockUser, Effect: mcp.Additive, Handler: func(ctx context.Context, _ identity.Caller, a UserArgs) (UserDone, error) {
		return UserDone(a), cfg.change(ctx, ToolUnlockUser, "", a.User, func() error { return cfg.Host.UserUnlock(ctx, a.User) }, nil)
	}})
	mcp.AddTool(srv, mcp.Tool[UserArgs, UserDone]{Name: ToolDeleteUser, Description: DescribeDeleteUser, Effect: mcp.Destructive, Handler: func(ctx context.Context, _ identity.Caller, a UserArgs) (UserDone, error) {
		err := cfg.change(ctx, ToolDeleteUser, "", a.User, func() error { return cfg.Host.UserDelete(ctx, a.User) }, func() error { _, e := cfg.Store.DeleteUser(ctx, a.User); return e })
		return UserDone(a), err
	}})
}

func hostError(err error) error {
	if err == nil {
		return nil
	}
	var refused *host.RefusedError
	if errors.As(err, &refused) {
		return errors.New(refused.Diagnostic)
	}
	return errors.New(Failed)
}

func (cfg Config) change(ctx context.Context, tool, group, user string, run, record func() error) error {
	err := run()
	result := trail.Result(err)
	answer := hostError(err)
	if err == nil && record != nil {
		if record() != nil {
			result = trail.ResultFailed
			answer = errors.New(store.Unreachable)
		}
	}
	trail.Change(ctx, cfg.Telemetry, tool, group, user, result)
	return answer
}

func (cfg Config) removeKey(ctx context.Context, _ identity.Caller, a KeyArgs) (KeyRemoved, error) {
	err := cfg.Host.UserKeyRemove(ctx, a.User, a.Key)
	result := trail.Result(err)
	answer := hostError(err)
	out := KeyRemoved{User: a.User, NoKeyLeft: true}
	if err == nil {
		users, e := cfg.Host.UserList(ctx)
		if e != nil {
			result = trail.ResultFailed
			answer = errors.New(KeysUnread)
		} else {
			for _, u := range users {
				if u.Name == a.User {
					out.NoKeyLeft = len(u.Keys) == 0
					break
				}
			}
		}
	}
	trail.Change(ctx, cfg.Telemetry, ToolRemoveKey, "", a.User, result)
	return out, answer
}

func window(tool string, page *string, limit *int) (string, int, error) {
	size, err := paging.Window(limit)
	if err != nil {
		return "", 0, err
	}
	var after string
	if page != nil && *page != "" {
		if (paging.Token{Tool: tool}).Decode(*page, &after) != nil {
			return "", 0, errors.New(paging.BadPage)
		}
	}
	return after, size, nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (cfg Config) groups(ctx context.Context, _ identity.Caller, a GroupsArgs) (GroupList, error) {
	after, size, err := window(ToolGroups, a.Page, a.Limit)
	if err != nil {
		return GroupList{}, err
	}
	all, err := cfg.Host.GroupList(ctx)
	if err != nil {
		return GroupList{}, hostError(err)
	}
	entries := make([]host.Group, 0)
	for _, g := range all {
		if a.Page == nil || *a.Page == "" || g.Slug > after {
			entries = append(entries, g)
		}
	}
	var next *string
	if len(entries) > size {
		s := (paging.Token{Tool: ToolGroups}).Encode(entries[size-1].Slug)
		next = &s
		entries = entries[:size]
	}
	out := GroupList{Groups: make([]Group, 0, len(entries)), Next: next}
	if len(entries) == 0 {
		return out, nil
	}
	slugs := make([]string, len(entries))
	for i, g := range entries {
		slugs[i] = g.Slug
	}
	records, err := cfg.Store.Groups(ctx, slugs)
	if err != nil {
		return GroupList{}, errors.New(store.Unreachable)
	}
	for _, g := range entries {
		r := records[g.Slug]
		out.Groups = append(out.Groups, Group{Slug: g.Slug, MemberCount: g.Members, Created: stamp(r.Created), CreatedBy: r.CreatedUserID})
	}
	return out, nil
}

func (cfg Config) group(ctx context.Context, _ identity.Caller, a GroupArgs) (GroupRead, error) {
	members, err := cfg.Host.GroupMembers(ctx, a.Group)
	if err != nil {
		return GroupRead{}, hostError(err)
	}
	records, err := cfg.Store.Groups(ctx, []string{a.Group})
	if err != nil {
		return GroupRead{}, errors.New(store.Unreachable)
	}
	r := records[a.Group]
	return GroupRead{Slug: a.Group, Members: members.Members, Created: stamp(r.Created), CreatedBy: r.CreatedUserID}, nil
}

func (cfg Config) users(ctx context.Context, _ identity.Caller, a UsersArgs) (UserList, error) {
	after, size, err := window(ToolUsers, a.Page, a.Limit)
	if err != nil {
		return UserList{}, err
	}
	all, err := cfg.Host.UserList(ctx)
	if err != nil {
		return UserList{}, hostError(err)
	}
	entries := make([]host.User, 0)
	for _, u := range all {
		if a.Page == nil || *a.Page == "" || u.Name > after {
			entries = append(entries, u)
		}
	}
	var next *string
	if len(entries) > size {
		s := (paging.Token{Tool: ToolUsers}).Encode(entries[size-1].Name)
		next = &s
		entries = entries[:size]
	}
	out := UserList{Users: make([]User, 0, len(entries)), Next: next}
	if len(entries) == 0 {
		return out, nil
	}
	names := make([]string, len(entries))
	for i, u := range entries {
		names[i] = u.Name
	}
	records, err := cfg.Store.Users(ctx, names)
	if err != nil {
		return UserList{}, errors.New(store.Unreachable)
	}
	for _, u := range entries {
		r := records[u.Name]
		out.Users = append(out.Users, User{Name: u.Name, Groups: u.Groups, Keys: u.Keys, State: u.State, LastSignIn: stamp(u.LastSignIn), Created: stamp(r.Created), CreatedBy: r.CreatedUserID})
	}
	return out, nil
}
