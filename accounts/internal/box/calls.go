package box

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (cfg Config) keys(ctx context.Context, c identity.Caller, _ NoArgs) (KeyList, error) {
	name := actor(c)
	if name == "" {
		return KeyList{}, errors.New(tools.NoUser)
	}
	u, err := cfg.Store.UserNamed(ctx, name)
	if err != nil {
		return KeyList{}, storeError(err, tools.NoUser)
	}
	out := KeyList{User: u.Name, Keys: make([]string, 0, len(u.Keys))}
	for _, key := range u.Keys {
		out.Keys = append(out.Keys, key.Line)
	}
	slices.Sort(out.Keys)
	return out, nil
}

func (cfg Config) addKey(ctx context.Context, _ identity.Caller, _ KeyArgs, r changeRecord) (tools.UserChange, error) {
	u, err := cfg.Store.AddKeyAs(ctx, r.actor, r.key)
	if err != nil {
		return tools.UserChange{}, storeError(err, tools.NoUser)
	}
	return userChange(u), nil
}

func (cfg Config) removeKey(ctx context.Context, _ identity.Caller, _ KeyArgs, r changeRecord) (tools.KeyRemoved, error) {
	u, left, err := cfg.Store.RemoveKeyAs(ctx, r.actor, r.key)
	if err != nil {
		return tools.KeyRemoved{}, storeError(err, tools.NoUser)
	}
	return tools.KeyRemoved{User: u.Name, Account: u.Account, Admin: u.Admin, State: u.State, Pending: u.Pending, KeysLeft: left}, nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (cfg Config) users(ctx context.Context, c identity.Caller, _ NoArgs) (UserList, error) {
	name := actor(c)
	if name == "" {
		return UserList{}, errors.New(OnlyAdmin)
	}
	users, err := cfg.Store.UsersAs(ctx, name)
	if err != nil {
		return UserList{}, storeError(err, tools.NoUser)
	}
	out := UserList{Users: make([]User, 0, len(users))}
	for _, u := range users {
		out.Users = append(out.Users, User{
			User: u.Name, Admin: u.Admin, State: u.State, Pending: u.Pending,
			LastSignIn: stamp(u.LastSignIn), Created: stamp(u.Created), CreatedBy: u.CreatedBy,
		})
	}
	return out, nil
}

func userChange(u store.User) tools.UserChange {
	return tools.UserChange{User: u.Name, Account: u.Account, Admin: u.Admin, State: u.State, Pending: u.Pending}
}

func userOperation(op func(context.Context, string, string) (store.User, error)) func(context.Context, identity.Caller, UserArgs, changeRecord) (tools.UserChange, error) {
	return func(ctx context.Context, _ identity.Caller, a UserArgs, r changeRecord) (tools.UserChange, error) {
		u, err := op(ctx, r.actor, a.User)
		if err != nil {
			return tools.UserChange{}, storeError(err, tools.NoUser)
		}
		return userChange(u), nil
	}
}

func (cfg Config) setAdmin(ctx context.Context, _ identity.Caller, a SetAdminArgs, r changeRecord) (tools.UserChange, error) {
	u, changed, err := cfg.Store.SetAdminAs(ctx, r.actor, a.User, a.Admin)
	if err != nil {
		return tools.UserChange{}, storeError(err, tools.NoUser)
	}
	if changed {
		trail.AdminChanged(ctx, cfg.Telemetry, r.account, r.host, a.User, u.Admin)
	}
	return userChange(u), nil
}

func (cfg Config) createInvite(ctx context.Context, c identity.Caller, _ NoArgs, r changeRecord) (tools.InviteMade, error) {
	if err := cfg.Store.AdminCheck(ctx, r.actor); err != nil {
		return tools.InviteMade{}, storeError(err, tools.NoUser)
	}
	if cfg.AccountsURL == nil {
		return tools.InviteMade{}, errors.New(tools.NoLink)
	}
	base, err := cfg.AccountsURL()
	if err != nil {
		return tools.InviteMade{}, errors.New(tools.NoLink)
	}
	i, token, err := cfg.Store.CreateInviteAs(ctx, r.actor, c)
	if err != nil {
		return tools.InviteMade{}, storeError(err, tools.NoUser)
	}
	trail.InviteCreated(ctx, cfg.Telemetry, i.ID, r.account, r.host, false)
	return tools.InviteMadeAnswer(i, base, token), nil
}

func (cfg Config) listInvites(ctx context.Context, c identity.Caller, _ NoArgs) (tools.InviteList, error) {
	name := actor(c)
	if name == "" {
		return tools.InviteList{}, errors.New(OnlyAdmin)
	}
	invites, err := cfg.Store.InvitesAs(ctx, name)
	if err != nil {
		return tools.InviteList{}, storeError(err, tools.NoInvite)
	}
	return tools.InviteListAnswer(invites), nil
}

func (cfg Config) revokeInvite(ctx context.Context, _ identity.Caller, a tools.InviteArgs, r changeRecord) (tools.Invite, error) {
	i, err := cfg.Store.RevokeInviteAs(ctx, r.actor, a.Invite)
	if err != nil {
		return tools.Invite{}, storeError(err, tools.NoInvite)
	}
	trail.InviteRevoked(ctx, cfg.Telemetry, i.ID, r.account, r.host)
	return tools.InviteAnswer(i), nil
}
