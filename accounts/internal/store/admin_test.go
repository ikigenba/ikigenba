package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func adminCalls(s *store.Store, actor, target, id string) []observation {
	return boxAdminCalls(ctx, s, actor, target, id)
}

func boxAdminCalls(c context.Context, s *store.Store, actor, target, id string) []observation {
	return []observation{
		noOutput(s.AdminCheck(c, actor)), observe(s.UsersAs(c, actor)),
		observe(s.LockAs(c, actor, target)), observe(s.UnlockAs(c, actor, target)),
		observe(s.DeleteAs(c, actor, target)), observe2(s.SetAdminAs(c, actor, target, false)),
		observe2(s.CreateInviteAs(c, actor, caller)), observe(s.InvitesAs(c, actor)),
		observe(s.RevokeInviteAs(c, actor, id)),
	}
}

// R-P3S1-RWFY R-CJ6H-55QS
func TestAdminFlagFollowsUserWithoutChangingDesired(t *testing.T) {
	_, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	u := value(t, result(s.AddUser(ctx, a.Slug, "bugs", firstKey, caller)))
	eq(t, u.Admin, false)
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))), u)
	eq(t, value(t, result(s.ListUsers(ctx, a.Slug, "", "", 5))), []store.User{u})
	observe2(s.SetAdmin(ctx, a.Slug, "missing", true)).assertZero(t, store.ErrNotFound)
	observe2(s.SetAdmin(ctx, b.Slug, u.Name, true)).assertZero(t, store.ErrNotInAccount)
	u = value(t, result(s.Lock(ctx, a.Slug, u.Name)))
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: u.Version}, *now)))
	u.Pending = false
	desired := value(t, result(s.Desired(ctx, h.Name)))
	ch, release := s.Watch(h.Name)
	defer release()
	for _, tc := range []struct{ flag, changed bool }{{true, true}, {true, false}, {false, true}, {true, true}} {
		changedUser, changed, err := s.SetAdmin(ctx, a.Slug, u.Name, tc.flag)
		checkErr(t, err, nil)
		eq(t, changed, tc.changed)
		u.Admin = tc.flag
		eq(t, changedUser, u)
		eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))), u)
		eq(t, value(t, result(s.ListUsersBefore(ctx, a.Slug, "z", "z", 5))), []store.User{u})
		eq(t, value(t, result(s.LastUsers(ctx, a.Slug, 5))), []store.User{u})
		eq(t, value(t, result(s.UsersOf(ctx, a.Slug))), []store.User{u})
		eq(t, value(t, result(s.Desired(ctx, h.Name))), desired)
		closed(t, ch, false)
	}
	eq(t, value(t, result(s.Unlock(ctx, a.Slug, u.Name))).Admin, true)
	otherKey := store.Key{Type: firstKey.Type, Body: "other", Line: "ssh-ed25519 other"}
	eq(t, value(t, result(s.AddKey(ctx, a.Slug, u.Name, otherKey))).Admin, true)
	after, _, err := s.RemoveKey(ctx, a.Slug, u.Name, otherKey)
	checkErr(t, err, nil)
	eq(t, after.Admin, true)
	eq(t, value(t, result(s.Lock(ctx, a.Slug, u.Name))).Admin, true)
	deleted := value(t, result(s.Delete(ctx, a.Slug, u.Name)))
	eq(t, deleted.Admin, true)
	observe2(s.SetAdmin(ctx, a.Slug, u.Name, false)).assertZero(t, store.ErrUserRemoving)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: deleted.Version}, *now)))
	readded := value(t, result(s.AddUser(ctx, a.Slug, u.Name, firstKey, caller)))
	eq(t, readded.Admin, false)
}

// R-RMQ3-1EUK R-RP5V-SYBY R-RKAA-9VD6
func TestActiveUserResolutionUsesCurrentAccountAndUser(t *testing.T) {
	d, s, now := fixture(t)
	h, _ := create(t, s)
	otherHost, _ := create(t, s)
	a, op := place(t, s, "acme", h.Name)
	u := value(t, result(s.AddUser(ctx, a.Slug, "bugs", firstKey, caller)))
	eq(t, value(t, result(s.UserNamed(ctx, u.Name))), u)
	eq(t, value(t, result(s.ActiveUserByUID(ctx, h.Name, u.UID))), u)
	for _, uid := range []int64{0, -1, a.GID} {
		observe(s.ActiveUserByUID(ctx, h.Name, uid)).assertZero(t, store.ErrNotFound)
	}
	observe(s.ActiveUserByUID(ctx, otherHost.Name, u.UID)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.AdminCheck(ctx, u.Name), store.ErrNotAdmin)
	checkErr(t, s.Fail(ctx, op.ID, "failure"), nil)
	changed, promoted, err := s.SetAdmin(ctx, a.Slug, u.Name, true)
	checkErr(t, err, nil)
	eq(t, promoted, true)
	eq(t, value(t, result(s.ActiveUserByUID(ctx, h.Name, u.UID))), changed)
	checkErr(t, s.AdminCheck(ctx, u.Name), nil)
	changeAccount := func(state string) {
		t.Helper()
		checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE accounts SET state = ? WHERE slug = ?", state, a.Slug)
			return err
		}), nil)
	}
	changeAccount(store.AccountRemoving)
	observe(s.ActiveUserByUID(ctx, h.Name, u.UID)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.AdminCheck(ctx, u.Name), store.ErrNotAdmin)
	observe(s.AddKeyAs(ctx, u.Name, firstKey)).assertZero(t, store.ErrNotActive)
	changeAccount(store.AccountFailed)
	locked := value(t, result(s.Lock(ctx, a.Slug, u.Name)))
	eq(t, value(t, result(s.UserNamed(ctx, u.Name))), locked)
	observe(s.ActiveUserByUID(ctx, h.Name, u.UID)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.AdminCheck(ctx, u.Name), store.ErrNotAdmin)
	removed := value(t, result(s.Delete(ctx, a.Slug, u.Name)))
	eq(t, value(t, result(s.UserNamed(ctx, u.Name))), removed)
	observe(s.ActiveUserByUID(ctx, h.Name, u.UID)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.AdminCheck(ctx, u.Name), store.ErrNotAdmin)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: removed.Version}, *now)))
	observe(s.UserNamed(ctx, u.Name)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.AdminCheck(ctx, u.Name), store.ErrNotAdmin)
}

func TestGuardedKeysUseTheCentralMutationAndKeepLastKey(t *testing.T) {
	_, s, _ := fixture(t)
	_, central, _ := fixture(t)
	for _, st := range []*store.Store{s, central} {
		h, _ := create(t, st)
		a, _ := place(t, st, "acme", h.Name)
		value(t, result(st.AddUser(ctx, a.Slug, "bugs", firstKey, caller)))
	}
	for i := range valid.MaxKeysPerUser - 1 {
		k := store.Key{Type: firstKey.Type, Body: fmt.Sprintf("key-%d", i), Line: fmt.Sprintf("%s key-%d", firstKey.Type, i)}
		want := value(t, result(central.AddKey(ctx, "acme", "bugs", k)))
		eq(t, value(t, result(s.AddKeyAs(ctx, "bugs", k))), want)
		eq(t, value(t, result(s.AddKeyAs(ctx, "bugs", k))), want)
	}
	overflow := store.Key{Type: firstKey.Type, Body: "extra", Line: "extra"}
	observe(s.AddKeyAs(ctx, "bugs", overflow)).assertZero(t, store.ErrTooManyKeys)
	observe2(s.RemoveKeyAs(ctx, "bugs", overflow)).assertZero(t, store.ErrKeyNotHeld)
	held := value(t, result(s.UserNamed(ctx, "bugs")))
	for _, k := range held.Keys {
		if k == firstKey {
			continue
		}
		want, left, err := central.RemoveKey(ctx, "acme", "bugs", k)
		checkErr(t, err, nil)
		got, n, err := s.RemoveKeyAs(ctx, "bugs", k)
		checkErr(t, err, nil)
		eq(t, got, want)
		eq(t, n, left)
	}
	observe2(s.RemoveKeyAs(ctx, "bugs", firstKey)).assertZero(t, store.ErrLastKey)
	eq(t, value(t, result(s.UserNamed(ctx, "bugs"))).Keys, []store.Key{firstKey})
	_, left, err := s.RemoveKey(ctx, "acme", "bugs", firstKey)
	checkErr(t, err, nil)
	eq(t, left, 0)
	value(t, result(s.Lock(ctx, "acme", "bugs")))
	for _, actor := range []string{"bugs", "unknown"} {
		observe(s.AddKeyAs(ctx, actor, firstKey)).assertZero(t, store.ErrNotActive)
		observe2(s.RemoveKeyAs(ctx, actor, firstKey)).assertZero(t, store.ErrNotActive)
	}
}

// R-RLI6-NN3V R-RU1H-C1AQ
func TestGuardedAdminToolsShareCentralRecord(t *testing.T) {
	_, s, _ := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	admin := value(t, result(s.AddUser(ctx, a.Slug, "bugs", firstKey, caller)))
	target := value(t, result(s.AddUser(ctx, a.Slug, "daffy", firstKey, caller)))
	foreign := value(t, result(s.AddUser(ctx, b.Slug, "elmer", firstKey, caller)))
	foreignInvite, foreignToken := invite(t, s, b.Slug, true)
	for _, call := range adminCalls(s, admin.Name, foreign.Name, foreignInvite.ID) {
		call.assertZero(t, store.ErrNotAdmin)
	}
	_, _, err := s.SetAdmin(ctx, a.Slug, admin.Name, true)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.UsersAs(ctx, admin.Name))), value(t, result(s.UsersOf(ctx, a.Slug))))
	eq(t, value(t, result(s.InvitesAs(ctx, admin.Name))), []store.Invite{})
	for _, name := range []string{"", "missing", foreign.Name} {
		calls := []observation{
			observe(s.LockAs(ctx, admin.Name, name)), observe(s.UnlockAs(ctx, admin.Name, name)),
			observe(s.DeleteAs(ctx, admin.Name, name)), observe2(s.SetAdminAs(ctx, admin.Name, name, true)),
		}
		for _, call := range calls {
			call.assertZero(t, store.ErrNotFound)
			eq(t, call.err, store.ErrNotFound)
		}
	}
	eq(t, value(t, result(s.UserNamed(ctx, foreign.Name))), foreign)
	observe(s.LockAs(ctx, admin.Name, admin.Name)).assertZero(t, store.ErrSelf)
	observe(s.DeleteAs(ctx, admin.Name, admin.Name)).assertZero(t, store.ErrSelf)
	observe2(s.SetAdminAs(ctx, admin.Name, admin.Name, false)).assertZero(t, store.ErrSelf)
	_, changed, err := s.SetAdminAs(ctx, admin.Name, admin.Name, true)
	checkErr(t, err, nil)
	eq(t, changed, false)
	observe(s.DeleteAs(ctx, admin.Name, target.Name)).assertZero(t, store.ErrNotLocked)
	promoted, changed, err := s.SetAdminAs(ctx, admin.Name, target.Name, true)
	checkErr(t, err, nil)
	eq(t, changed, true)
	eq(t, value(t, result(s.User(ctx, a.Slug, target.Name))), promoted)
	checkErr(t, s.AdminCheck(ctx, target.Name), nil)
	_, changed, err = s.SetAdminAs(ctx, admin.Name, target.Name, false)
	checkErr(t, err, nil)
	eq(t, changed, true)
	checkErr(t, s.AdminCheck(ctx, target.Name), store.ErrNotAdmin)
	locked := value(t, result(s.LockAs(ctx, admin.Name, target.Name)))
	eq(t, locked.State, store.UserLocked)
	eq(t, value(t, result(s.UnlockAs(ctx, admin.Name, target.Name))).State, store.UserActive)
	value(t, result(s.LockAs(ctx, admin.Name, target.Name)))
	eq(t, value(t, result(s.DeleteAs(ctx, admin.Name, target.Name))).State, store.UserRemoving)
	for _, call := range adminCalls(s, target.Name, "", "") {
		call.assertZero(t, store.ErrNotAdmin)
	}

	actor := identity.Caller{UserID: "ssh:" + admin.Name}
	i, token, err := s.CreateInviteAs(ctx, admin.Name, actor)
	checkErr(t, err, nil)
	eq(t, i.Account, a.Slug)
	eq(t, i.Admin, false)
	eq(t, i.CreatedBy, actor.UserID)
	eq(t, value(t, result(s.InvitesAs(ctx, admin.Name))), []store.Invite{i})
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{i})
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Invites, 1)
	eq(t, value(t, result(s.InviteByToken(ctx, token))), i)
	for _, id := range []string{"", "invalid", foreignInvite.ID} {
		observe(s.RevokeInviteAs(ctx, admin.Name, id)).assertZero(t, store.ErrNotFound)
	}
	eq(t, value(t, result(s.InviteByToken(ctx, foreignToken))), foreignInvite)
	enrolled, u, err := s.Enroll(ctx, token, "sylvester", firstKey)
	checkErr(t, err, nil)
	eq(t, enrolled, i)
	eq(t, u.Admin, false)
	eq(t, u.CreatedBy, actor.UserID)
	i, token = invite(t, s, a.Slug, true)
	eq(t, value(t, result(s.RevokeInviteAs(ctx, admin.Name, i.ID))), i)
	observe(s.InviteByToken(ctx, token)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Invites, 0)
	eq(t, value(t, result(s.InvitesAs(ctx, admin.Name))), []store.Invite{})

	_, _, err = s.SetAdmin(ctx, a.Slug, admin.Name, false)
	checkErr(t, err, nil)
	for _, call := range adminCalls(s, admin.Name, foreign.Name, foreignInvite.ID) {
		call.assertZero(t, store.ErrNotAdmin)
	}
}
