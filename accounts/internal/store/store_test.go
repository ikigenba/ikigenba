package store_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/db"
)

var ctx = context.Background()

func open(t *testing.T, path string, migrations fs.FS, now func() time.Time) (*db.DB, *store.Store) {
	t.Helper()
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: migrations, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, store.New(d, store.Config{Now: now})
}

// R-OLAX-HT3S R-OMIT-VKUH R-ONQQ-9CL6 R-OQ6J-0W2K R-OSMB-SFJY
// R-OTU8-67AN R-OV24-JZ1C R-XEL7-LJKZ R-OXHX-BIIQ R-OZXQ-3204
// R-XFT3-ZBBO R-P4TB-M4YW R-P794-DOGA
func TestRecordLifecycleAndReopen(t *testing.T) {
	stamp := time.Date(2026, 10, 29, 9, 3, 7, 900000000, time.FixedZone("chosen", -5*3600))
	calls := 0
	now := func() time.Time { calls++; return stamp }
	path := filepath.Join(t.TempDir(), "record.db")
	d, st := open(t, path, accounts.Migrations(), now)
	for _, names := range [][]string{nil, {}, {"missing", "", "missing"}} {
		gs, err := st.Groups(ctx, names)
		if err != nil || gs == nil || len(gs) != 0 {
			t.Fatalf("initial groups %v %v", gs, err)
		}
		us, err := st.Users(ctx, names)
		if err != nil || us == nil || len(us) != 0 {
			t.Fatalf("initial users %v %v", us, err)
		}
	}
	if wrote, err := st.RemoveGroup(ctx, "missing"); err != nil || wrote {
		t.Fatalf("absent group %v %v", wrote, err)
	}
	if wrote, err := st.DeleteUser(ctx, "missing"); err != nil || wrote {
		t.Fatalf("absent user %v %v", wrote, err)
	}
	wantTime := stamp.UTC().Truncate(time.Second)
	before := calls
	g, added, err := st.AddGroup(ctx, "same", "creator-one")
	if err != nil || !added || calls != before+1 || g != (store.Group{"same", wantTime, "creator-one"}) || g.Created.Location() != time.UTC {
		t.Fatalf("first group %+v %v %v clock=%d", g, added, err, calls-before)
	}
	before = calls
	u, added, err := st.AddUser(ctx, "same", "creator-two")
	if err != nil || !added || calls != before+1 || u != (store.User{"same", wantTime, "creator-two"}) || u.Created.Location() != time.UTC {
		t.Fatalf("first user %+v %v %v", u, added, err)
	}
	stamp = stamp.Add(2 * time.Hour)
	if got, wrote, e := st.AddGroup(ctx, "same", "other"); e != nil || wrote || got != g {
		t.Fatalf("repeat group %+v %v %v", got, wrote, e)
	}
	if got, wrote, e := st.AddUser(ctx, "same", "other"); e != nil || wrote || got != u {
		t.Fatalf("repeat user %+v %v %v", got, wrote, e)
	}
	gCase, added, err := st.AddGroup(ctx, "Same", "case-maker")
	if err != nil || !added {
		t.Fatal(gCase, added, err)
	}
	uCase, added, err := st.AddUser(ctx, "Same", "case-maker")
	if err != nil || !added {
		t.Fatal(uCase, added, err)
	}
	check := func(s *store.Store, groups map[string]store.Group, users map[string]store.User) {
		t.Helper()
		gs, e := s.Groups(ctx, []string{"same", "Same", "absent"})
		if e != nil || !reflect.DeepEqual(gs, groups) {
			t.Fatalf("groups=%v want=%v err=%v", gs, groups, e)
		}
		us, e := s.Users(ctx, []string{"same", "Same", "absent"})
		if e != nil || !reflect.DeepEqual(us, users) {
			t.Fatalf("users=%v want=%v err=%v", us, users, e)
		}
	}
	check(st, map[string]store.Group{"same": g, "Same": gCase}, map[string]store.User{"same": u, "Same": uCase})
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	d, st = open(t, path, accounts.Migrations(), now)
	check(st, map[string]store.Group{"same": g, "Same": gCase}, map[string]store.User{"same": u, "Same": uCase})
	if wrote, e := st.RemoveGroup(ctx, "same"); e != nil || !wrote {
		t.Fatal(wrote, e)
	}
	if wrote, e := st.DeleteUser(ctx, "Same"); e != nil || !wrote {
		t.Fatal(wrote, e)
	}
	check(st, map[string]store.Group{"Same": gCase}, map[string]store.User{"same": u})
	if wrote, e := st.RemoveGroup(ctx, "same"); e != nil || wrote {
		t.Fatal(wrote, e)
	}
	if wrote, e := st.DeleteUser(ctx, "Same"); e != nil || wrote {
		t.Fatal(wrote, e)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	_, st = open(t, path, accounts.Migrations(), now)
	check(st, map[string]store.Group{"Same": gCase}, map[string]store.User{"same": u})
	stamp = stamp.Add(time.Hour)
	g2, added, err := st.AddGroup(ctx, "same", "new")
	if err != nil || !added || !g2.Created.Equal(stamp.UTC().Truncate(time.Second)) || g2.Created.Equal(g.Created) {
		t.Fatal(g2, added, err)
	}
	u2, added, err := st.AddUser(ctx, "Same", "new")
	if err != nil || !added || !u2.Created.Equal(stamp.UTC().Truncate(time.Second)) || u2.Created.Equal(uCase.Created) {
		t.Fatal(u2, added, err)
	}
}

// R-XH10-D32D R-XI8W-QUT2
func TestWindowLookups(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	_, st := open(t, filepath.Join(t.TempDir(), "record.db"), accounts.Migrations(), now)
	for _, name := range []string{"asked-a", "asked-b", "not-asked"} {
		if _, _, err := st.AddGroup(ctx, name, "owner"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.AddUser(ctx, name, "owner"); err != nil {
			t.Fatal(err)
		}
	}
	names := make([]string, 40000)
	for i := range names {
		names[i] = fmt.Sprintf("absent-%d", i)
	}
	names[1], names[39999] = "asked-a", "asked-b"
	for _, window := range [][]string{nil, {"asked-a", "missing", "missing", "", "asked-a"}, names} {
		gs, e := st.Groups(ctx, window)
		if e != nil || gs == nil {
			t.Fatal(gs, e)
		}
		us, e := st.Users(ctx, window)
		if e != nil || us == nil {
			t.Fatal(us, e)
		}
		want := 0
		if len(window) > 0 {
			want = 1
		}
		if len(window) == 40000 {
			want = 2
		}
		if len(gs) != want || len(us) != want {
			t.Fatalf("window %d answers %v %v", len(window), gs, us)
		}
		for name, g := range gs {
			if name == "not-asked" || g.Slug != name || g.CreatedUserID != "owner" || g.Created != now() {
				t.Fatal(g)
			}
		}
		for name, u := range us {
			if name == "not-asked" || u.Name != name || u.CreatedUserID != "owner" || u.Created != now() {
				t.Fatal(u)
			}
		}
	}
}

// R-OYPT-PA9F R-P15M-GTQT R-P617-ZWPL R-P8H0-RG6Z R-PC4P-WRF2
func TestInvalidArgumentsAndHandleFailures(t *testing.T) {
	errorVariables := []*error{&store.ErrUnreachable}
	const unreachable = store.Unreachable
	if *errorVariables[0] == nil || unreachable == "" || strings.ContainsAny(unreachable, "\r\n") {
		t.Fatal("error contract")
	}
	now := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	path := filepath.Join(t.TempDir(), "record.db")
	d, st := open(t, path, accounts.Migrations(), now)
	g, _, _ := st.AddGroup(ctx, "held", "owner")
	u, _, _ := st.AddUser(ctx, "held", "owner")
	assertFailure := func(c context.Context, name, owner string, done bool) {
		t.Helper()
		check := func(err error) {
			t.Helper()
			if !errors.Is(err, store.ErrUnreachable) || done && !errors.Is(err, c.Err()) {
				t.Fatalf("failure %v", err)
			}
		}
		got, added, err := st.AddGroup(c, name, owner)
		check(err)
		if got != (store.Group{}) || added {
			t.Fatal(got, added)
		}
		usr, added, err := st.AddUser(c, name, owner)
		check(err)
		if usr != (store.User{}) || added {
			t.Fatal(usr, added)
		}
		removed, err := st.RemoveGroup(c, name)
		check(err)
		if removed {
			t.Fatal("removed group")
		}
		removed, err = st.DeleteUser(c, name)
		check(err)
		if removed {
			t.Fatal("removed user")
		}
		if done || name != "" {
			gs, err := st.Groups(c, []string{name})
			check(err)
			if gs != nil {
				t.Fatal(gs)
			}
			us, err := st.Users(c, []string{name})
			check(err)
			if us != nil {
				t.Fatal(us)
			}
		}
	}
	assertFailure(ctx, "", "owner", false)
	for _, add := range []func() error{func() error { _, _, e := st.AddGroup(ctx, "held", ""); return e }, func() error { _, _, e := st.AddUser(ctx, "held", ""); return e }, func() error { _, _, e := st.AddGroup(ctx, "", ""); return e }, func() error { _, _, e := st.AddUser(ctx, "", ""); return e }} {
		if !errors.Is(add(), store.ErrUnreachable) {
			t.Fatal("empty creator")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for _, name := range []string{"", "held", "new"} {
		assertFailure(cancelled, name, "", true)
	}
	d.SetFailing(true)
	for _, name := range []string{"", "held", "new"} {
		assertFailure(ctx, name, "owner", false)
		if name == "" {
			gs, e := st.Groups(ctx, nil)
			if gs != nil || !errors.Is(e, store.ErrUnreachable) {
				t.Fatal(gs, e)
			}
			us, e := st.Users(ctx, nil)
			if us != nil || !errors.Is(e, store.ErrUnreachable) {
				t.Fatal(us, e)
			}
		}
	}
	d.SetFailing(false)
	gs, e := st.Groups(ctx, []string{"held", "new"})
	if e != nil || !reflect.DeepEqual(gs, map[string]store.Group{"held": g}) {
		t.Fatal(gs, e)
	}
	us, e := st.Users(ctx, []string{"held", "new"})
	if e != nil || !reflect.DeepEqual(us, map[string]store.User{"held": u}) {
		t.Fatal(us, e)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	assertFailure(ctx, "held", "owner", false)
	assertFailure(ctx, "", "", false)
	_, st = open(t, path, accounts.Migrations(), now)
	gs, e = st.Groups(ctx, []string{"held", "new"})
	if e != nil || len(gs) != 1 || gs["held"] != g {
		t.Fatal(gs, e)
	}
	us, e = st.Users(ctx, []string{"held", "new"})
	if e != nil || len(us) != 1 || us["held"] != u {
		t.Fatal(us, e)
	}
}

// R-PDCM-AJ5R R-XKOP-IEAG
func TestMigrationAndExpandCompatibility(t *testing.T) {
	data, err := fs.ReadFile(accounts.Migrations(), "0001_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE groups (slug TEXT PRIMARY KEY, created TEXT NOT NULL, created_user_id TEXT NOT NULL) WITHOUT ROWID;\nCREATE TABLE users (name TEXT PRIMARY KEY, created TEXT NOT NULL, created_user_id TEXT NOT NULL) WITHOUT ROWID;\n"
	if string(data) != want {
		t.Fatalf("migration %q", data)
	}
	expanded := fstest.MapFS{"0001_accounts.sql": {Data: data}, "0002_expand.sql": {Data: []byte("ALTER TABLE groups ADD COLUMN annotation TEXT; ALTER TABLE users ADD COLUMN annotation TEXT; ALTER TABLE users ADD COLUMN count INTEGER;\n")}}
	now := func() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC) }
	da, a := open(t, filepath.Join(t.TempDir(), "old.db"), accounts.Migrations(), now)
	db, b := open(t, filepath.Join(t.TempDir(), "new.db"), expanded, now)
	sequence := func(s *store.Store) []any {
		out := []any{}
		g, w, e := s.AddGroup(ctx, "business", "caller")
		out = append(out, g, w, e)
		u, w, e := s.AddUser(ctx, "person", "caller")
		out = append(out, u, w, e)
		g, w, e = s.AddGroup(ctx, "business", "other")
		out = append(out, g, w, e)
		u, w, e = s.AddUser(ctx, "person", "other")
		out = append(out, u, w, e)
		gs, e := s.Groups(ctx, []string{"business", "missing", ""})
		out = append(out, gs, e)
		us, e := s.Users(ctx, []string{"person", "missing", ""})
		out = append(out, us, e)
		w, e = s.RemoveGroup(ctx, "business")
		out = append(out, w, e)
		w, e = s.DeleteUser(ctx, "person")
		out = append(out, w, e)
		w, e = s.RemoveGroup(ctx, "business")
		out = append(out, w, e)
		w, e = s.DeleteUser(ctx, "person")
		out = append(out, w, e)
		gs, e = s.Groups(ctx, nil)
		out = append(out, gs, e)
		us, e = s.Users(ctx, nil)
		out = append(out, us, e)
		for _, args := range [][2]string{{"", "caller"}, {"business", ""}, {"", ""}} {
			g, w, e = s.AddGroup(ctx, args[0], args[1])
			out = append(out, g, w, e)
			u, w, e = s.AddUser(ctx, args[0], args[1])
			out = append(out, u, w, e)
		}
		w, e = s.RemoveGroup(ctx, "")
		out = append(out, w, e)
		w, e = s.DeleteUser(ctx, "")
		out = append(out, w, e)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		g, w, e = s.AddGroup(cancelled, "business", "caller")
		out = append(out, g, w, e)
		u, w, e = s.AddUser(cancelled, "person", "caller")
		out = append(out, u, w, e)
		w, e = s.RemoveGroup(cancelled, "business")
		out = append(out, w, e)
		w, e = s.DeleteUser(cancelled, "person")
		out = append(out, w, e)
		gs, e = s.Groups(cancelled, nil)
		out = append(out, gs, e)
		us, e = s.Users(cancelled, nil)
		out = append(out, us, e)
		return out
	}
	if x, y := sequence(a), sequence(b); !reflect.DeepEqual(x, y) {
		t.Fatalf("expand differs: %v / %v", x, y)
	}
	da.SetFailing(true)
	db.SetFailing(true)
	if x, y := sequence(a), sequence(b); !reflect.DeepEqual(x, y) {
		t.Fatalf("expand failing differs: %v / %v", x, y)
	}
	da.SetFailing(false)
	db.SetFailing(false)
	if x, y := sequence(a), sequence(b); !reflect.DeepEqual(x, y) {
		t.Fatalf("expand recovered differs: %v / %v", x, y)
	}
	if err := da.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if x, y := sequence(a), sequence(b); !reflect.DeepEqual(x, y) {
		t.Fatalf("expand closed differs: %v / %v", x, y)
	}
}
