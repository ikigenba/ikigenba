package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (cfg Config) setAdmin(ctx context.Context, _ identity.Caller, a SetAdminArgs, r *changeRecord) (UserChange, error) {
	if err := cfg.readUserAccount(ctx, r); err != nil {
		return UserChange{}, err
	}
	u, changed, err := cfg.Store.SetAdmin(ctx, a.Account, a.User, a.Admin)
	if err != nil {
		return UserChange{}, storeError(err, NoUser)
	}
	if changed {
		trail.AdminChanged(ctx, cfg.Telemetry, r.account, r.host, r.user, u.Admin)
	}
	return userChange(u), nil
}

// InviteAnswer converts a stored invite without disclosing its token.
func InviteAnswer(i store.Invite) Invite {
	return Invite{Invite: i.ID, Account: i.Account, Admin: i.Admin,
		Created: stamp(i.Created), Expires: stamp(i.Expires), CreatedBy: i.CreatedBy}
}

// InviteMadeAnswer includes the link when an invite is first made.
func InviteMadeAnswer(i store.Invite, base, token string) InviteMade {
	out := InviteAnswer(i)
	return InviteMade{Invite: out.Invite, Account: out.Account, Admin: out.Admin,
		Created: out.Created, Expires: out.Expires, CreatedBy: out.CreatedBy, Link: base + "/join/" + token}
}

// InviteListAnswer converts a bounded list of stored invites.
func InviteListAnswer(invites []store.Invite) InviteList {
	out := InviteList{Invites: make([]Invite, 0, len(invites))}
	for _, invite := range invites {
		out.Invites = append(out.Invites, InviteAnswer(invite))
	}
	return out
}

func (cfg Config) createInvite(ctx context.Context, c identity.Caller, a CreateInviteArgs, r *changeRecord) (InviteMade, error) {
	if cfg.AccountsURL == nil {
		return InviteMade{}, errors.New(NoLink)
	}
	base, err := cfg.AccountsURL()
	if err != nil {
		return InviteMade{}, errors.New(NoLink)
	}
	account, err := cfg.Store.Account(ctx, a.Account)
	if err != nil {
		return InviteMade{}, storeError(err, NoAccount)
	}
	r.host = account.Host
	invite, token, err := cfg.Store.CreateInvite(ctx, a.Account, value(a.Admin, false), c)
	if err != nil {
		return InviteMade{}, storeError(err, NoAccount)
	}
	trail.InviteCreated(ctx, cfg.Telemetry, invite.ID, r.account, r.host, invite.Admin)
	return InviteMadeAnswer(invite, base, token), nil
}

func (cfg Config) listInvites(ctx context.Context, a AccountArgs) (InviteList, error) {
	invites, err := cfg.Store.Invites(ctx, a.Account)
	if err != nil {
		return InviteList{}, storeError(err, NoAccount)
	}
	return InviteListAnswer(invites), nil
}

func (cfg Config) revokeInvite(ctx context.Context, _ identity.Caller, a InviteArgs, r *changeRecord) (Invite, error) {
	if !store.ValidInviteID(a.Invite) {
		return Invite{}, errors.New(NoInvite)
	}
	invite, err := cfg.Store.RevokeInvite(ctx, a.Invite)
	if err != nil {
		return Invite{}, storeError(err, NoInvite)
	}
	r.account = invite.Account
	if account, err := cfg.Store.Account(ctx, invite.Account); err == nil {
		r.host = account.Host
	}
	trail.InviteRevoked(ctx, cfg.Telemetry, invite.ID, r.account, r.host)
	return InviteAnswer(invite), nil
}
