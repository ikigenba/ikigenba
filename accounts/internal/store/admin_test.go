package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

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

// R-G0D7-NT9R R-G1L4-1L0G
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
		k.Line += " another comment"
		eq(t, value(t, result(s.AddKeyAs(ctx, "bugs", k))), want)
	}
	overflow := store.Key{Type: firstKey.Type, Body: "extra", Line: "extra"}
	held := value(t, result(s.UserNamed(ctx, "bugs")))
	observe(s.AddKeyAs(ctx, "bugs", overflow)).assertZero(t, store.ErrTooManyKeys)
	observe2(s.RemoveKeyAs(ctx, "bugs", overflow)).assertZero(t, store.ErrKeyNotHeld)
	eq(t, value(t, result(s.UserNamed(ctx, "bugs"))), held)
	for _, k := range held.Keys {
		if k == firstKey {
			continue
		}
		want, left, err := central.RemoveKey(ctx, "acme", "bugs", k)
		checkErr(t, err, nil)
		k.Line += " another comment"
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
	locked := value(t, result(s.Lock(ctx, "acme", "bugs")))
	for _, actor := range []string{"bugs", "unknown"} {
		observe(s.AddKeyAs(ctx, actor, overflow)).assertZero(t, store.ErrNotActive)
		observe2(s.RemoveKeyAs(ctx, actor, overflow)).assertZero(t, store.ErrNotActive)
	}
	eq(t, value(t, result(s.UserNamed(ctx, "bugs"))), locked)
	eq(t, value(t, result(s.NameHeld(ctx, "unknown"))), false)
}

// R-G2T0-FCR5
func TestAdminGuardPrecedesTargetAndInviteChecks(t *testing.T) {
	d, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	for _, name := range []string{"plain", "locked", "removing", "demoted"} {
		value(t, result(s.AddUser(ctx, a.Slug, name, firstKey, caller)))
		if name != "plain" {
			_, _, err := s.SetAdmin(ctx, a.Slug, name, true)
			checkErr(t, err, nil)
			checkErr(t, s.AdminCheck(ctx, name), nil)
		}
	}
	value(t, result(s.Lock(ctx, a.Slug, "locked")))
	value(t, result(s.Lock(ctx, a.Slug, "removing")))
	value(t, result(s.Delete(ctx, a.Slug, "removing")))
	_, _, err := s.SetAdmin(ctx, a.Slug, "demoted", false)
	checkErr(t, err, nil)

	foreign := value(t, result(s.AddUser(ctx, b.Slug, "elmer", firstKey, caller)))
	foreignInvite, _ := invite(t, s, b.Slug, true)
	beforeUsers := value(t, result(s.UsersOf(ctx, a.Slug)))
	beforeHost := value(t, result(s.Host(ctx, h.Name)))
	random := bytes.NewReader(make([]byte, 64))
	guarded := store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: random})
	for _, actor := range []string{"plain", "locked", "removing", "demoted", "missing"} {
		t.Run(actor, func(t *testing.T) {
			for _, call := range adminCalls(guarded, actor, "", "") {
				call.assertZero(t, store.ErrNotAdmin)
			}
			for _, call := range adminCalls(guarded, actor, foreign.Name, foreignInvite.ID) {
				call.assertZero(t, store.ErrNotAdmin)
			}
		})
	}
	eq(t, random.Len(), 64)
	eq(t, value(t, result(s.UsersOf(ctx, a.Slug))), beforeUsers)
	eq(t, value(t, result(s.UserNamed(ctx, foreign.Name))), foreign)
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{})
	eq(t, value(t, result(s.Invites(ctx, b.Slug))), []store.Invite{foreignInvite})
	eq(t, value(t, result(s.Host(ctx, h.Name))), beforeHost)
}

// R-RLI6-NN3V R-RU1H-C1AQ R-G40W-T4HU R-G6GP-KNZ8
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
	beforeTarget := value(t, result(s.UserNamed(ctx, target.Name)))
	beforeAdmin := value(t, result(s.UserNamed(ctx, admin.Name)))
	beforeHost := value(t, result(s.Host(ctx, h.Name)))
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
	eq(t, value(t, result(s.UserNamed(ctx, target.Name))), beforeTarget)
	eq(t, value(t, result(s.UserNamed(ctx, admin.Name))), beforeAdmin)
	eq(t, value(t, result(s.Host(ctx, h.Name))), beforeHost)
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
	for _, id := range []string{"", "invalid", "iv_ffffffffffffffff", foreignInvite.ID} {
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

// R-G40W-T4HU R-G58T-6W8J R-G6GP-KNZ8
func TestAdminMutationsMatchCentralTwins(t *testing.T) {
	const account, actor, target = "acme", "bugs", "daffy"
	d, s, now := fixture(t)
	_, central, _ := fixture(t)
	var host string
	for _, st := range []*store.Store{s, central} {
		h, _ := create(t, st)
		host = h.Name
		place(t, st, account, host)
		for _, admin := range []string{actor, target} {
			value(t, result(st.AddUser(ctx, account, admin, firstKey, caller)))
			_, _, err := st.SetAdmin(ctx, account, admin, true)
			checkErr(t, err, nil)
		}
	}
	compare := func(got, want observation) {
		t.Helper()
		checkErr(t, want.err, nil)
		checkErr(t, got.err, nil)
		eq(t, got.out, want.out)
		eq(t, value(t, result(s.Host(ctx, host))), value(t, result(central.Host(ctx, host))))
		eq(t, value(t, result(s.UsersOf(ctx, account))), value(t, result(central.UsersOf(ctx, account))))
		eq(t, value(t, result(s.Account(ctx, account))), value(t, result(central.Account(ctx, account))))
	}
	compare(observe2(s.SetAdminAs(ctx, actor, target, false)), observe2(central.SetAdmin(ctx, account, target, false)))
	compare(observe(s.LockAs(ctx, actor, target)), observe(central.Lock(ctx, account, target)))
	compare(observe(s.UnlockAs(ctx, actor, target)), observe(central.Unlock(ctx, account, target)))
	compare(observe(s.LockAs(ctx, actor, target)), observe(central.Lock(ctx, account, target)))
	compare(observe(s.DeleteAs(ctx, actor, target)), observe(central.Delete(ctx, account, target)))

	random := bytes.NewReader(make([]byte, 64))
	guarded := store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: random})
	atLimit := identity.Caller{UserID: strings.Repeat("x", valid.MaxUserIDBytes)}
	tooLong := identity.Caller{UserID: atLimit.UserID + "x"}
	observe2(guarded.CreateInviteAs(ctx, actor, tooLong)).assertZero(t, store.ErrUnreachable)
	eq(t, value(t, result(s.Invites(ctx, account))), []store.Invite{})
	first, token, err := s.CreateInviteAs(ctx, actor, atLimit)
	compare(observe2(first, token, err), observe2(central.CreateInvite(ctx, account, false, atLimit)))
	second, secondToken, err := s.CreateInviteAs(ctx, actor, caller)
	compare(observe2(second, secondToken, err), observe2(central.CreateInvite(ctx, account, false, caller)))
	for range store.MaxInvites - 2 {
		compare(observe2(s.CreateInviteAs(ctx, actor, caller)), observe2(central.CreateInvite(ctx, account, false, caller)))
	}
	observe2(guarded.CreateInviteAs(ctx, actor, caller)).assertZero(t, store.ErrTooManyInvites)
	eq(t, random.Len(), 64)
	eq(t, value(t, result(s.Invites(ctx, account))), value(t, result(central.Invites(ctx, account))))
	compare(observe2(s.Enroll(ctx, token, "joiner", firstKey)), observe2(central.Enroll(ctx, token, "joiner", firstKey)))
	observe(s.RevokeInviteAs(ctx, actor, first.ID)).assertZero(t, store.ErrNotFound)
	compare(observe(s.RevokeInviteAs(ctx, actor, second.ID)), observe(central.RevokeInvite(ctx, second.ID)))

	expired, _, err := s.CreateInviteAs(ctx, actor, caller)
	checkErr(t, err, nil)
	beforeExpiry := value(t, result(s.InvitesAs(ctx, actor)))
	*now = expired.Expires
	observe(s.RevokeInviteAs(ctx, actor, expired.ID)).assertZero(t, store.ErrNotFound)
	*now = expired.Created
	eq(t, value(t, result(s.InvitesAs(ctx, actor))), beforeExpiry)
}
