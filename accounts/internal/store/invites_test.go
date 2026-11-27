package store_test

import (
	"bytes"
	"encoding/base32"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func invite(t *testing.T, s *store.Store, account string, admin bool) (store.Invite, string) {
	t.Helper()
	i, token, err := s.CreateInvite(ctx, account, admin, caller)
	checkErr(t, err, nil)
	return i, token
}

// R-BW0D-VINL R-C0VZ-ELMD
func TestInviteFormats(t *testing.T) {
	eq(t, store.InviteLife, 72*time.Hour)
	eq(t, store.MaxInvites, 20)
	eq(t, store.InvitePrefix, "iv_")
	eq(t, store.TokenPrefix, "inv_")
	eq(t, store.ValidInviteID("iv_0123456789abcdef"), true)
	for _, invalid := range []string{"iv_0123456789abcde", "iv_0123456789abcdef0", "iv_0123456789ABCDEf", "op_0123456789abcdef", "iv_0123456789abcdeg"} {
		eq(t, store.ValidInviteID(invalid), false)
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	eq(t, store.ValidToken("inv_"+alphabet+strings.Repeat("0", 20)), true)
	for _, bad := range []string{"I", "L", "O", "U", "a", "-", " "} {
		eq(t, store.ValidToken("inv_"+strings.Repeat("Z", 51)+bad), false)
	}
	eq(t, store.ValidToken("inv_"+strings.Repeat("Z", 51)), false)
	eq(t, store.ValidToken("inv_"+strings.Repeat("Z", 53)), false)
	eq(t, store.ValidToken("inv-"+strings.Repeat("Z", 52)), false)
}

// R-RGML-4K53 R-RYX2-V49I
func TestEmptyStoreInviteAndBoxCalls(t *testing.T) {
	_, s, _ := fixture(t)
	const unknownID = "iv_0123456789abcdef"
	unknownToken := "inv_" + strings.Repeat("0", 52)
	for _, call := range []observation{
		observe(s.Invites(ctx, "acme")), observe2(s.SetAdmin(ctx, "acme", "person", true)),
		observe2(s.CreateInvite(ctx, "acme", false, caller)), observe(s.InviteByToken(ctx, "")),
		observe(s.RevokeInvite(ctx, "")), observe2(s.Enroll(ctx, "", "person", firstKey)),
		observe(s.RevokeInvite(ctx, unknownID)), observe(s.InviteByToken(ctx, unknownToken)),
		observe2(s.Enroll(ctx, unknownToken, "person", firstKey)),
		observe(s.UserNamed(ctx, "person")), observe(s.ActiveUserByUID(ctx, "host01", 0)),
	} {
		call.assertZero(t, store.ErrNotFound)
	}
	for _, call := range adminCalls(s, "person", "", "") {
		call.assertZero(t, store.ErrNotAdmin)
	}
	observe(s.AddKeyAs(ctx, "person", firstKey)).assertZero(t, store.ErrNotActive)
	observe2(s.RemoveKeyAs(ctx, "person", firstKey)).assertZero(t, store.ErrNotActive)
	eq(t, value(t, result(s.NameHeld(ctx, "acme"))), false)
}

// R-C23V-SDD2 R-C87D-P82J R-CAN6-GRJX R-CBV2-UJAM R-CD2Z-8B1B
func TestInviteRecordAndExpiration(t *testing.T) {
	d, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	before := value(t, result(s.Desired(ctx, h.Name)))
	ch, release := s.Watch(h.Name)
	defer release()

	first, token := invite(t, s, a.Slug, true)
	stamp := now.UTC().Truncate(time.Second)
	eq(t, first, store.Invite{ID: first.ID, Account: a.Slug, Admin: true, Created: stamp, Expires: stamp.Add(store.InviteLife), CreatedBy: caller.UserID})
	eq(t, value(t, result(s.InviteByToken(ctx, token))), first)
	eq(t, value(t, result(s.InviteByToken(ctx, token))), first)
	observe(s.InviteByToken(ctx, "")).assertZero(t, store.ErrNotFound)
	observe(s.InviteByToken(ctx, token[:len(token)-1]+"Z")).assertZero(t, store.ErrNotFound)
	second, secondToken := invite(t, s, a.Slug, false)
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{first, second})
	a.Invites = 2
	eq(t, value(t, result(s.Account(ctx, a.Slug))), a)
	eq(t, value(t, result(s.AccountsOn(ctx, h.Name))), []store.Account{a})
	eq(t, value(t, result(s.ListAccounts(ctx, "", 1))), []store.Account{a})
	eq(t, value(t, result(s.ListAccountsBefore(ctx, "z", 1))), []store.Account{a})
	eq(t, value(t, result(s.LastAccounts(ctx, 1))), []store.Account{a})
	eq(t, value(t, result(s.UsersOf(ctx, a.Slug))), []store.User{})
	eq(t, value(t, result(s.Desired(ctx, h.Name))), before)

	eq(t, value(t, result(s.RevokeInvite(ctx, second.ID))), second)
	observe(s.RevokeInvite(ctx, second.ID)).assertZero(t, store.ErrNotFound)
	observe(s.InviteByToken(ctx, secondToken)).assertZero(t, store.ErrNotFound)
	observe2(s.Enroll(ctx, secondToken, "candidate", firstKey)).assertZero(t, store.ErrNotFound)
	observe(s.RevokeInvite(ctx, "nonexistent")).assertZero(t, store.ErrNotFound)

	*now = first.Expires.Add(-time.Second)
	eq(t, value(t, result(s.InviteByToken(ctx, token))), first)
	*now = first.Expires
	observe(s.InviteByToken(ctx, token)).assertZero(t, store.ErrNotFound)
	observe(s.RevokeInvite(ctx, first.ID)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{})
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Invites, 0)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), before)
	closed(t, ch, false)

	cfg := store.Config{Now: func() time.Time { return first.Created }, Rand: new(counterRandom)}
	persisted := store.New(d, cfg)
	eq(t, value(t, result(persisted.InviteByToken(ctx, token))), first)
}

// R-J04B-VKT7
func TestAccountInviteCountsAtExpiry(t *testing.T) {
	_, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	initial, initialToken := invite(t, s, a.Slug, false)
	*now = initial.Expires.Add(-time.Second)
	live, liveToken := invite(t, s, a.Slug, false)
	revoked, _ := invite(t, s, a.Slug, false)
	value(t, result(s.RevokeInvite(ctx, revoked.ID)))
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Invites, 2)
	*now = initial.Expires
	observe(s.InviteByToken(ctx, initialToken)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.InviteByToken(ctx, liveToken))), live)
	a.Invites = 1
	eq(t, value(t, result(s.Account(ctx, a.Slug))), a)
	eq(t, value(t, result(s.Account(ctx, b.Slug))), b)
	eq(t, value(t, result(s.AccountsOn(ctx, h.Name))), []store.Account{a, b})
	eq(t, value(t, result(s.ListAccounts(ctx, "", 2))), []store.Account{a, b})
	eq(t, value(t, result(s.ListAccountsBefore(ctx, "z", 2))), []store.Account{a, b})
	eq(t, value(t, result(s.LastAccounts(ctx, 2))), []store.Account{a, b})
}

// R-CAN6-GRJX
func TestInviteListSortsByCreatedBeforeID(t *testing.T) {
	d, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	draws := bytes.Join([][]byte{
		bytes.Repeat([]byte{0xff}, 8), bytes.Repeat([]byte{0x55}, 32),
		bytes.Repeat([]byte{0x77}, 8), bytes.Repeat([]byte{0x44}, 32),
		bytes.Repeat([]byte{0x11}, 8), bytes.Repeat([]byte{0x33}, 32),
	}, nil)
	s = store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: bytes.NewReader(draws)})

	first, _ := invite(t, s, a.Slug, false)
	second, _ := invite(t, s, a.Slug, false)
	*now = now.Add(time.Second)
	third, _ := invite(t, s, a.Slug, false)
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{second, first, third})
	observe(s.Invites(ctx, "missing")).assertZero(t, store.ErrNotFound)
}

// R-RFEO-QSEE R-CLM9-WP86 R-23QB-GJ8E R-CEAV-M2S0
func TestInviteCapsAndAccountRemoval(t *testing.T) {
	d, s, now := fixture(t)
	h, _ := create(t, s)
	a, op := place(t, s, "acme", h.Name)
	b, bOp := place(t, s, "bravo", h.Name)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	checkErr(t, s.Fail(ctx, bOp.ID, "failure"), nil)
	invite(t, s, b.Slug, false)
	random := bytes.NewReader(make([]byte, 64))
	refusals := store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: random})
	observe2(refusals.CreateInvite(ctx, "missing", true, caller)).assertZero(t, store.ErrNotFound)
	var first store.Invite
	for i := range store.MaxInvites + 5 {
		if i >= store.MaxInvites {
			observe2(refusals.CreateInvite(ctx, a.Slug, false, caller)).assertZero(t, store.ErrTooManyInvites)
			continue
		}
		inv, _ := invite(t, s, a.Slug, false)
		if i == 0 {
			first = inv
		}
	}
	eq(t, len(value(t, result(s.Invites(ctx, a.Slug)))), store.MaxInvites)
	value(t, result(s.RevokeInvite(ctx, first.ID)))
	_, token := invite(t, s, a.Slug, false)
	_, _, err := s.Enroll(ctx, token, "joiner", firstKey)
	checkErr(t, err, nil)
	invite(t, s, a.Slug, false)
	*now = first.Expires
	for range store.MaxInvites {
		invite(t, s, a.Slug, false)
	}
	eq(t, len(value(t, result(s.Invites(ctx, a.Slug)))), store.MaxInvites)

	c, _ := place(t, s, "charlie", h.Name)
	agedID := bytes.Repeat([]byte{0x72}, 8)
	withRandom := func(secret byte) *store.Store {
		draw := append(bytes.Clone(agedID), bytes.Repeat([]byte{secret}, 32)...)
		return store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: bytes.NewReader(draw)})
	}
	aged, _ := invite(t, withRandom(0x76), c.Slug, false)
	*now = now.Add(time.Second)
	_, token = invite(t, s, c.Slug, false)
	*now = aged.Expires
	eq(t, len(value(t, result(s.Invites(ctx, c.Slug)))), 1)
	got, unplace := remove(t, s, c.Slug)
	eq(t, got.Invites, 0)
	eq(t, value(t, result(s.Account(ctx, c.Slug))).Invites, 0)
	eq(t, value(t, result(s.Invites(ctx, c.Slug))), []store.Invite{})
	observe(s.InviteByToken(ctx, token)).assertZero(t, store.ErrNotFound)
	observe2(s.Enroll(ctx, token, "joiner", firstKey)).assertZero(t, store.ErrNotFound)
	observe2(refusals.CreateInvite(ctx, c.Slug, true, caller)).assertZero(t, store.ErrAccountRemoving)
	eq(t, random.Len(), 64)
	reused, _ := invite(t, withRandom(0x77), b.Slug, false)
	eq(t, reused.ID, aged.ID)
	checkErr(t, s.Finish(ctx, unplace.ID, store.Update{}), nil)
	eq(t, value(t, result(s.NameHeld(ctx, c.Slug))), false)
	place(t, s, c.Slug, h.Name)
	observe(s.InviteByToken(ctx, token)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.Account(ctx, c.Slug))).Invites, 0)
}

// R-IYWF-HT2I
func TestInviteRandomDrawsAndRollback(t *testing.T) {
	d, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	cfg := func(random []byte) (*store.Store, *bytes.Reader) {
		r := bytes.NewReader(random)
		return store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: r}), r
	}
	run := func(random []byte, account string) observation {
		s, r := cfg(random)
		got := observe2(s.CreateInvite(ctx, account, false, caller))
		eq(t, r.Len(), 0)
		return got
	}
	id := bytes.Repeat([]byte{0x5a}, 8)
	secret := bytes.Repeat([]byte{0x36}, 32)
	initialBytes := append(bytes.Clone(id), secret...)
	script, r := cfg(initialBytes)
	first, token := invite(t, script, a.Slug, false)
	eq(t, r.Len(), 0)
	eq(t, first.ID, "iv_5a5a5a5a5a5a5a5a")
	eq(t, token, store.TokenPrefix+base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding).EncodeToString(secret))
	secondID := bytes.Repeat([]byte{0x38}, 8)
	secondSecret := bytes.Repeat([]byte{0x72}, 32)
	ninthDraw := append(bytes.Clone(secondID), secondSecret...)
	script, r = cfg(append(bytes.Repeat(id, 8), ninthDraw...))
	observe2(script.CreateInvite(ctx, a.Slug, false, caller)).assertZero(t, store.ErrUnreachable)
	eq(t, r.Len(), len(ninthDraw))
	run(append(bytes.Clone(secondID), secret...), a.Slug).assertZero(t, store.ErrUnreachable)
	run(bytes.Repeat([]byte{0x12}, 5), a.Slug).assertZero(t, store.ErrUnreachable)
	run(append(bytes.Clone(secondID), secret[:5]...), a.Slug).assertZero(t, store.ErrUnreachable)
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{first})

	script, r = cfg(append(bytes.Clone(id), ninthDraw...))
	second, secondToken := invite(t, script, a.Slug, false)
	eq(t, r.Len(), 0)
	eq(t, second.ID, "iv_3838383838383838")
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{second, first})
	*now = first.Expires
	run(bytes.Repeat(id, 8), b.Slug).assertZero(t, store.ErrUnreachable)
	run(append(bytes.Repeat([]byte{0x49}, 8), secret...), b.Slug).assertZero(t, store.ErrUnreachable)
	run(bytes.Clone(id), a.Slug).assertZero(t, store.ErrUnreachable)
	run(bytes.Repeat(id, 8), b.Slug).assertZero(t, store.ErrUnreachable)
	script, r = cfg(initialBytes)
	reused, _ := invite(t, script, a.Slug, false)
	eq(t, r.Len(), 0)
	eq(t, reused.ID, first.ID)
	*now = first.Created
	observe(s.InviteByToken(ctx, secondToken)).assertZero(t, store.ErrNotFound)
}

// R-CFIR-ZUIP R-CGQO-DM9E R-CEAV-M2S0
func TestEnrollCreatesOnlyAfterRefusals(t *testing.T) {
	_, s, now := fixture(t)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	b, _ := place(t, s, "bravo", h.Name)
	first, token := invite(t, s, a.Slug, true)
	eq(t, value(t, result(s.NameHeld(ctx, a.Slug))), true)
	eq(t, value(t, result(s.NameHeld(ctx, "new-user"))), false)
	observe2(s.Enroll(ctx, "not-a-token", a.Slug, firstKey)).assertZero(t, store.ErrNotFound)
	observe2(s.Enroll(ctx, token, b.Slug, firstKey)).assertZero(t, store.ErrTaken)
	eq(t, value(t, result(s.InviteByToken(ctx, token))), first)

	before := value(t, result(s.Host(ctx, h.Name)))
	i, u, err := s.Enroll(ctx, token, "new-user", firstKey)
	checkErr(t, err, nil)
	eq(t, i, first)
	eq(t, u, store.User{Name: "new-user", Account: a.Slug, Admin: true, UID: b.GID + 1,
		State: store.UserActive, Pending: true, Keys: []store.Key{firstKey}, Version: before.Version + 1,
		Created: now.UTC().Truncate(time.Second), CreatedBy: caller.UserID})
	eq(t, value(t, result(s.NameHeld(ctx, u.Name))), true)
	eq(t, value(t, result(s.Host(ctx, h.Name))).Version, u.Version)
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Users, 1)
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Invites, 0)
	observe(s.RevokeInvite(ctx, i.ID)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))), u)

	next, token := invite(t, s, a.Slug, false)
	for i := range valid.MaxUsersPerHost - 1 {
		value(t, result(s.AddUser(ctx, b.Slug, fmt.Sprintf("member-%d", i), firstKey, caller)))
	}
	observe2(s.Enroll(ctx, token, u.Name, firstKey)).assertZero(t, store.ErrTaken)
	observe2(s.Enroll(ctx, token, "overflow", firstKey)).assertZero(t, store.ErrAccountFull)
	eq(t, value(t, result(s.NameHeld(ctx, "overflow"))), false)
	eq(t, value(t, result(s.InviteByToken(ctx, token))), next)
	value(t, result(s.Lock(ctx, a.Slug, u.Name)))
	deleted := value(t, result(s.Delete(ctx, a.Slug, u.Name)))
	eq(t, value(t, result(s.NameHeld(ctx, u.Name))), true)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: deleted.Version}, *now)))
	eq(t, value(t, result(s.NameHeld(ctx, u.Name))), false)
}

// R-J1C8-9CJW
func TestConcurrentEnrollmentConsumesInvite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.db")
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	d, s := open(t, path, accounts.Migrations(), cfg)
	h, _ := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	_, token := invite(t, s, a.Slug, true)
	checkErr(t, d.Close(), nil)
	_, s = open(t, path, accounts.Migrations(), cfg)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for idx, name := range []string{"bugs", "daffy"} {
		wg.Go(func() {
			<-start
			_, _, errs[idx] = s.Enroll(ctx, token, name, firstKey)
		})
	}
	close(start)
	wg.Wait()
	successes := 0
	for idx, name := range []string{"bugs", "daffy"} {
		ok := errs[idx] == nil
		eq(t, value(t, result(s.NameHeld(ctx, name))), ok)
		if ok {
			successes++
		} else {
			checkErr(t, errs[idx], store.ErrNotFound)
		}
	}
	eq(t, successes, 1)
	observe(s.InviteByToken(ctx, token)).assertZero(t, store.ErrNotFound)
	observe2(s.Enroll(ctx, token, "elmer", firstKey)).assertZero(t, store.ErrNotFound)
}

// R-J2K4-N4AL
func TestInviteTokensStayOutOfStoreAnswers(t *testing.T) {
	const account, actor, member = "acme", "owner", "candidate"
	setup := func(t *testing.T) (*store.Store, string, int64) {
		t.Helper()
		d, s, now := fixture(t)
		h, _ := create(t, s)
		place(t, s, account, h.Name)
		u := value(t, result(s.AddUser(ctx, account, actor, firstKey, caller)))
		_, _, err := s.SetAdmin(ctx, account, actor, true)
		checkErr(t, err, nil)
		draws := bytes.Join([][]byte{
			bytes.Repeat([]byte{0x17}, 8), bytes.Repeat([]byte{0xff}, 32),
			bytes.Repeat([]byte{0x31}, 8), bytes.Repeat([]byte{0xee}, 32),
			bytes.Repeat([]byte{0x73}, 8), bytes.Repeat([]byte{0x99}, 32),
		}, nil)
		return store.New(d, store.Config{Now: func() time.Time { return *now }, Rand: bytes.NewReader(draws)}), h.Name, u.UID
	}
	check := func(t *testing.T, tokens []string, values ...any) {
		t.Helper()
		for _, issued := range tokens {
			eq(t, store.ValidToken(issued), true)
			body := strings.ToLower(strings.TrimPrefix(issued, store.TokenPrefix))
			for _, value := range values {
				text := strings.ToLower(fmt.Sprintf("%+v", value))
				for i := 0; i+8 <= len(body); i++ {
					if strings.Contains(text, body[i:i+8]) {
						t.Fatalf("value of type %T contains an invite token fragment", value)
					}
				}
			}
		}
	}
	t.Run("independent inputs", func(t *testing.T) {
		s, host, uid := setup(t)
		created, token := invite(t, s, account, true)
		createdAs, tokenAs, err := s.CreateInviteAs(ctx, actor, caller)
		checkErr(t, err, nil)
		other, otherToken := invite(t, s, account, false)
		unknown := store.TokenPrefix + strings.Repeat("G", 52)
		tokens := []string{token, tokenAs, otherToken}
		check(t, tokens, spec, caller, host, account, actor, member, firstKey, uid, unknown, created.ID, createdAs.ID, other.ID, "", "z", 2)
		check(t, tokens, created, createdAs, other,
			value(t, result(s.Invites(ctx, account))), value(t, result(s.InvitesAs(ctx, actor))),
			value(t, result(s.Account(ctx, account))), value(t, result(s.AccountsOn(ctx, host))),
			value(t, result(s.ListAccounts(ctx, "", 2))), value(t, result(s.ListAccountsBefore(ctx, "z", 2))),
			value(t, result(s.LastAccounts(ctx, 2))), value(t, result(s.User(ctx, account, actor))),
			value(t, result(s.UserNamed(ctx, actor))), value(t, result(s.ActiveUserByUID(ctx, host, uid))),
			value(t, result(s.UsersOf(ctx, account))), value(t, result(s.UsersAs(ctx, actor))),
			value(t, result(s.ListUsers(ctx, account, "", "", 2))),
			value(t, result(s.ListUsersBefore(ctx, account, "z", "z", 2))), value(t, result(s.LastUsers(ctx, account, 2))),
			value(t, result(s.RevokeInviteAs(ctx, actor, createdAs.ID))), value(t, result(s.RevokeInvite(ctx, other.ID))))
		for _, call := range []observation{observe(s.InviteByToken(ctx, unknown)), observe2(s.Enroll(ctx, unknown, member, firstKey))} {
			call.assertZero(t, store.ErrNotFound)
			check(t, tokens, call.out, call.err)
		}
	})
	t.Run("issued credential", func(t *testing.T) {
		s, _, _ := setup(t)
		created, token := invite(t, s, account, true)
		createdAs, tokenAs, err := s.CreateInviteAs(ctx, actor, caller)
		checkErr(t, err, nil)
		value(t, result(s.RevokeInviteAs(ctx, actor, createdAs.ID)))
		tokens := []string{token, tokenAs}
		found := value(t, result(s.InviteByToken(ctx, token)))
		eq(t, found, created)
		conflict := observe2(s.Enroll(ctx, token, actor, firstKey))
		conflict.assertZero(t, store.ErrTaken)
		enrolled, user, err := s.Enroll(ctx, token, member, firstKey)
		checkErr(t, err, nil)
		eq(t, enrolled, created)
		check(t, tokens, found, conflict.out, conflict.err, enrolled, user)
		for _, call := range []observation{observe(s.InviteByToken(ctx, token)), observe2(s.Enroll(ctx, tokenAs, member, firstKey))} {
			call.assertZero(t, store.ErrNotFound)
			check(t, tokens, call.out, call.err)
		}
	})
}

// R-759B-PGTT R-RBQZ-LH6B R-RE6S-D0NP R-RFEO-QSEE
func TestRecordedCallersAreLimitedByBytes(t *testing.T) {
	d, s, _ := fixture(t)
	h, op := create(t, s)
	a, _ := place(t, s, "acme", h.Name)
	r := bytes.NewReader(make([]byte, 128))
	guarded := store.New(d, store.Config{Now: func() time.Time { return baseTime }, Rand: r})
	long := identity.Caller{UserID: strings.Repeat("é", valid.MaxUserIDBytes/2) + "x"}
	version := value(t, result(s.Host(ctx, h.Name))).Version
	for _, call := range []observation{
		observe2(guarded.CreateHost(ctx, spec, long)),
		observe2(guarded.StartHostOp(ctx, h.Name, store.OpStop, store.Target{}, long)),
		observe2(guarded.RetryHost(ctx, h.Name, long)),
		observe2(guarded.PlaceAccount(ctx, "other", h.Name, long)),
		observe3(guarded.RemoveAccount(ctx, a.Slug, long)),
		observe(guarded.AddUser(ctx, a.Slug, "user", firstKey, long)),
		observe2(guarded.CreateInvite(ctx, a.Slug, false, long)),
		observe2(guarded.CreateInviteAs(ctx, "user", long)),
	} {
		call.assertZero(t, store.ErrUnreachable)
	}
	eq(t, r.Len(), 128)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	eq(t, value(t, result(s.Host(ctx, h.Name))).Version, version)
	eq(t, value(t, result(s.NameHeld(ctx, "user"))), false)
	eq(t, value(t, result(s.NameHeld(ctx, "other"))), false)
	eq(t, value(t, result(s.Invites(ctx, a.Slug))), []store.Invite{})
	atLimit := identity.Caller{UserID: strings.Repeat("é", valid.MaxUserIDBytes/2)}
	eq(t, value(t, result(s.AddUser(ctx, a.Slug, "user", firstKey, atLimit))).CreatedBy, atLimit.UserID)
}
