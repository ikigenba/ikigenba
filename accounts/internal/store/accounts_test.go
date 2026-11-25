package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// R-H8VP-5ATJ R-HL2O-Z08H R-0MPJ-YPN7 R-IAOL-06T2
// R-RCYV-Z8X0 R-0NXG-CHDW R-RFUT-UU0N R-IFK6-J9RU R-IGS2-X1IJ R-I5SZ-H3UA
// R-I3D6-PKCW R-0LHN-KXWI R-IU6Z-4IO6 R-HW1S-EXWQ R-I4L3-3C3L
func TestAccountLifecycle(t *testing.T) {
	_, s, now := fixture(t)
	h, c := create(t, s)
	b, _ := create(t, s)
	_, _, err := s.PlaceAccount(ctx, "missing", "missing", caller)
	checkErr(t, err, store.ErrNotFound)
	a, placeOp := place(t, s, "charlie", h.Name)
	stamp := now.UTC().Truncate(time.Second)
	eq(t, a, store.Account{Slug: "charlie", Host: h.Name, State: store.AccountPending, GID: store.FirstID,
		Version: 2, Operation: placeOp.ID, Created: stamp, CreatedBy: caller.UserID})
	eq(t, placeOp, store.Operation{ID: placeOp.ID, Host: h.Name, Account: a.Slug, Kind: store.OpPlace, Status: store.OpRunning,
		Generation: 1, Started: stamp, StepStarted: stamp, NextTry: stamp, Caller: caller})
	eq(t, value(t, result(s.Account(ctx, a.Slug))), a)
	u := value(t, result(s.AddUser(ctx, a.Slug, "user", firstKey, caller)))
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Users, 1)
	_, _, err = s.PlaceAccount(ctx, u.Name, b.Name, caller)
	checkErr(t, err, store.ErrTaken)
	ac, acOp := place(t, s, "alfa", h.Name)
	eq(t, u.UID, a.GID+1)
	eq(t, ac.GID, u.UID+1)
	bc, bcOp := place(t, s, "bravo", b.Name)
	_, _, err = s.PlaceAccount(ctx, a.Slug, b.Name, caller)
	checkErr(t, err, store.ErrTaken)
	eq(t, value(t, result(s.Host(ctx, h.Name))).Accounts, 2)
	_, _, err = s.RemoveAccount(ctx, "missing", caller)
	checkErr(t, err, store.ErrNotFound)
	_, _, err = s.RemoveAccount(ctx, a.Slug, caller)
	checkErr(t, err, store.ErrHasUsers)
	checkErr(t, s.RecordChange(ctx, placeOp.ID, bc.Slug, "change"), store.ErrNotFound)
	checkErr(t, s.RecordChange(ctx, placeOp.ID, "missing", "change"), store.ErrNotFound)
	checkErr(t, s.RecordChange(ctx, placeOp.ID, a.Slug, "change"), nil)
	a.Change, a.Users = "change", 1
	eq(t, value(t, result(s.Account(ctx, a.Slug))), a)

	eq(t, value(t, result(s.ListAccounts(ctx, "alfa", 1))), []store.Account{bc})
	eq(t, value(t, result(s.ListAccountsBefore(ctx, "charlie", 2))), []store.Account{ac, bc})
	eq(t, value(t, result(s.LastAccounts(ctx, 2))), []store.Account{bc, a})
	eq(t, value(t, result(s.AccountsOn(ctx, h.Name))), []store.Account{ac, a})
	before := value(t, result(s.Host(ctx, h.Name)))
	checkErr(t, s.Advance(ctx, placeOp.ID, store.Update{InstanceID: "ignore", DataGB: 100}), nil)
	eq(t, value(t, result(s.Host(ctx, h.Name))), before)
	checkErr(t, s.Finish(ctx, placeOp.ID, store.Update{InstanceID: "ignore", DataGB: 100}), nil)
	eq(t, value(t, result(s.Host(ctx, h.Name))), before)
	a.State = store.AccountActive
	eq(t, value(t, result(s.Account(ctx, a.Slug))), a)
	checkErr(t, s.Fail(ctx, acOp.ID, "record conflict"), nil)
	ac.State, ac.Reason = store.AccountFailed, "record conflict"
	eq(t, value(t, result(s.Account(ctx, ac.Slug))), ac)

	*now = now.Add(time.Minute)
	bc, unplace := remove(t, s, bc.Slug)
	eq(t, unplace.Supersedes, bcOp.ID)
	eq(t, unplace.ResumeState, store.AccountPending)
	eq(t, bc.State, store.AccountRemoving)
	eq(t, bc.RemovedVersion, int64(3))
	eq(t, bc.Operation, unplace.ID)
	cancelled := value(t, result(s.Operation(ctx, bcOp.ID)))
	eq(t, cancelled.Status, store.OpCancelled)
	eq(t, cancelled.Finished, now.UTC().Truncate(time.Second))
	checkErr(t, s.Advance(ctx, bcOp.ID, store.Update{}), store.ErrConflict)
	_, _, err = s.RemoveAccount(ctx, bc.Slug, caller)
	checkErr(t, err, store.ErrAccountRemoving)
	checkErr(t, s.Fail(ctx, unplace.ID, "temporary error"), nil)
	eq(t, value(t, result(s.Account(ctx, bc.Slug))), bc)
	again, retry := remove(t, s, bc.Slug)
	bc.Operation = retry.ID
	eq(t, again, bc)
	eq(t, retry.Supersedes, "")
	eq(t, retry.ResumeState, store.AccountRemoving)
	eq(t, value(t, result(s.Host(ctx, b.Name))).Version, bc.RemovedVersion)
	checkErr(t, s.Finish(ctx, retry.ID, store.Update{}), nil)
	_, err = s.Account(ctx, bc.Slug)
	checkErr(t, err, store.ErrNotFound)
	eq(t, value(t, result(s.Host(ctx, b.Name))).Version, bc.RemovedVersion)
	eq(t, value(t, result(s.AccountsOn(ctx, b.Name))), []store.Account{})

	// R-K3GV-OEHB: host deletion completes both the current and the failed unplace.
	bc, _ = place(t, s, bc.Slug, b.Name)
	bc, old := remove(t, s, bc.Slug)
	checkErr(t, s.Fail(ctx, old.ID, "failed removal"), nil)
	bc, current := remove(t, s, bc.Slug)
	second, _ := place(t, s, "delta", b.Name)
	_, secondRemoval := remove(t, s, second.Slug)
	b = value(t, result(s.Host(ctx, b.Name)))
	checkErr(t, s.Finish(ctx, b.Operation, store.Update{}), nil)
	_, del := start(t, s, b.Name, store.OpDelete, store.Target{})
	_, _, err = s.PlaceAccount(ctx, bc.Slug, b.Name, caller)
	checkErr(t, err, store.ErrHostGone)
	*now = now.Add(time.Minute)
	checkErr(t, s.Finish(ctx, del.ID, store.Update{}), nil)
	_, _, err = s.PlaceAccount(ctx, bc.Slug, b.Name, caller)
	checkErr(t, err, store.ErrHostGone)
	for _, slug := range []string{bc.Slug, second.Slug} {
		_, err = s.Account(ctx, slug)
		checkErr(t, err, store.ErrNotFound)
	}
	eq(t, value(t, result(s.ListAccounts(ctx, "", 10))), []store.Account{ac, a})
	for _, id := range []string{old.ID, current.ID, secondRemoval.ID} {
		op := value(t, result(s.Operation(ctx, id)))
		eq(t, op.Status, store.OpDone)
		eq(t, op.Finished, now.UTC().Truncate(time.Second))
	}
	place(t, s, bc.Slug, h.Name)
	place(t, s, second.Slug, h.Name)
	checkErr(t, s.Fail(ctx, c.ID, "host failure"), nil)
	place(t, s, "on-failed-host", h.Name)
}

// R-RBQZ-LH6B R-RE6S-D0NP
func TestPlacementCapsAndPrecedence(t *testing.T) {
	_, s, _ := fixture(t)
	h, _ := create(t, s)
	other, _ := create(t, s)
	for i := range valid.MaxAccountsPerHost {
		place(t, s, fmt.Sprintf("account-%03d", i), h.Name)
	}
	_, _, err := s.PlaceAccount(ctx, "account-000", h.Name, caller)
	checkErr(t, err, store.ErrTaken)
	_, _, err = s.PlaceAccount(ctx, "overflow", h.Name, caller)
	checkErr(t, err, store.ErrHostFull)
	a, _ := place(t, s, "overflow", other.Name)
	remove(t, s, a.Slug)
	_, err = s.AddUser(ctx, a.Slug, "account-000", firstKey, caller)
	checkErr(t, err, store.ErrAccountRemoving)
	_, err = s.AddUser(ctx, "missing", "account-000", firstKey, caller)
	checkErr(t, err, store.ErrNotFound)
	for i := range valid.MaxUsersPerHost {
		value(t, result(s.AddUser(ctx, fmt.Sprintf("account-%03d", i%valid.MaxAccountsPerHost), fmt.Sprintf("user-%03d", i), firstKey, caller)))
	}
	_, err = s.AddUser(ctx, "account-000", "user-000", firstKey, caller)
	checkErr(t, err, store.ErrTaken)
	_, err = s.AddUser(ctx, "account-000", "one-more-user", firstKey, caller)
	checkErr(t, err, store.ErrAccountFull)
}

// R-HA3L-J2K8 R-IJ7V-OKZX R-IKFS-2CQM R-ILNO-G4HB R-IMVK-TW80
// R-IO3H-7NYP R-IPBD-LFPE R-IQJ9-Z7G3 R-IRR6-CZ6S R-IU6Z-4IO6
func TestUsersKeysAndPaging(t *testing.T) {
	_, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "alfa", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	u := value(t, result(s.AddUser(ctx, a.Slug, "charlie", firstKey, caller)))
	eq(t, u, store.User{Name: "charlie", Account: a.Slug, State: store.UserActive, UID: b.GID + 1, Version: 4,
		Pending: true, Keys: []store.Key{firstKey}, Created: now.UTC().Truncate(time.Second), CreatedBy: caller.UserID})
	x := value(t, result(s.AddUser(ctx, a.Slug, "alfa-user", firstKey, caller)))
	y := value(t, result(s.AddUser(ctx, b.Slug, "bravo-user", firstKey, caller)))
	eq(t, value(t, result(s.ListUsers(ctx, "", a.Slug, x.Name, 2))), []store.User{u, y})
	eq(t, value(t, result(s.ListUsersBefore(ctx, "", b.Slug, y.Name, 2))), []store.User{x, u})
	eq(t, value(t, result(s.LastUsers(ctx, "", 1))), []store.User{y})
	eq(t, value(t, result(s.ListUsers(ctx, a.Slug, "", "", 10))), []store.User{x, u})
	eq(t, value(t, result(s.UsersOf(ctx, a.Slug))), []store.User{x, u})
	_, err := s.UsersOf(ctx, "missing")
	checkErr(t, err, store.ErrNotFound)

	for _, tc := range []struct {
		account, name string
		want          error
	}{
		{b.Slug, u.Name, store.ErrNotInAccount}, {a.Slug, "missing", store.ErrNotFound},
	} {
		_, err := s.User(ctx, tc.account, tc.name)
		checkErr(t, err, tc.want)
		_, err = s.AddKey(ctx, tc.account, tc.name, firstKey)
		checkErr(t, err, tc.want)
		_, _, err = s.RemoveKey(ctx, tc.account, tc.name, firstKey)
		checkErr(t, err, tc.want)
		_, err = s.Lock(ctx, tc.account, tc.name)
		checkErr(t, err, tc.want)
		_, err = s.Unlock(ctx, tc.account, tc.name)
		checkErr(t, err, tc.want)
		_, err = s.Delete(ctx, tc.account, tc.name)
		checkErr(t, err, tc.want)
	}

	alias := firstKey
	alias.Line = "another comment"
	eq(t, value(t, result(s.AddKey(ctx, a.Slug, u.Name, alias))), u)
	for i := range valid.MaxKeysPerUser - 1 {
		k := store.Key{Type: firstKey.Type, Body: fmt.Sprintf("aa-%d", i), Line: fmt.Sprintf("ssh-ed25519 aa-%d", i)}
		u = value(t, result(s.AddKey(ctx, a.Slug, u.Name, k)))
		eq(t, u.Version, value(t, result(s.Host(ctx, h.Name))).Version)
		eq(t, u.Keys[len(u.Keys)-1], firstKey)
	}
	eq(t, value(t, result(s.AddKey(ctx, a.Slug, u.Name, alias))), u)
	_, err = s.AddKey(ctx, a.Slug, u.Name, store.Key{Type: firstKey.Type, Body: "overflow", Line: "ssh-ed25519 overflow"})
	checkErr(t, err, store.ErrTooManyKeys)
	for _, k := range u.Keys {
		before := value(t, result(s.Host(ctx, h.Name))).Version
		var n int
		k.Line = "different line"
		u, n, err = s.RemoveKey(ctx, a.Slug, u.Name, k)
		checkErr(t, err, nil)
		eq(t, n, len(u.Keys))
		eq(t, u.Version, before+1)
	}
	eq(t, len(u.Keys), 0)
	_, _, err = s.RemoveKey(ctx, a.Slug, u.Name, firstKey)
	checkErr(t, err, store.ErrKeyNotHeld)
	last := value(t, result(s.Host(ctx, h.Name))).Version
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: last}, *now)))
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))).Pending, false)
	u.Pending = false
	eq(t, value(t, result(s.Unlock(ctx, a.Slug, u.Name))), u)
	_, err = s.Delete(ctx, a.Slug, u.Name)
	checkErr(t, err, store.ErrNotLocked)
	u = value(t, result(s.Lock(ctx, a.Slug, u.Name)))
	eq(t, u.State, store.UserLocked)
	eq(t, u.Version, last+1)
	eq(t, value(t, result(s.Lock(ctx, a.Slug, u.Name))), u)
	u = value(t, result(s.Unlock(ctx, a.Slug, u.Name)))
	eq(t, u.State, store.UserActive)
	eq(t, u.Version, last+2)
	u = value(t, result(s.Lock(ctx, a.Slug, u.Name)))
	u = value(t, result(s.Delete(ctx, a.Slug, u.Name)))
	eq(t, u.State, store.UserRemoving)
	eq(t, u.Version, last+4)
	eq(t, value(t, result(s.Delete(ctx, a.Slug, u.Name))), u)
	_, err = s.Lock(ctx, a.Slug, u.Name)
	checkErr(t, err, store.ErrUserRemoving)
	_, err = s.Unlock(ctx, a.Slug, u.Name)
	checkErr(t, err, store.ErrUserRemoving)
	_, err = s.AddKey(ctx, a.Slug, u.Name, firstKey)
	checkErr(t, err, store.ErrUserRemoving)
	_, _, err = s.RemoveKey(ctx, a.Slug, u.Name, firstKey)
	checkErr(t, err, store.ErrUserRemoving)
	_, err = s.AddUser(ctx, b.Slug, u.Name, firstKey, caller)
	checkErr(t, err, store.ErrTaken)
}
