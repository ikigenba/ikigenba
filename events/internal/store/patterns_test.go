package store_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	event "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

func TestPatternDeclarationsAndAsk(t *testing.T) {
	// R-YIZW-MOXG R-YXMP-7XTS R-YYUL-LPKH
	asks := []string{}
	var s *store.Store
	s, _, _ = openStore(t, store.Config{Ask: func(_ context.Context, service string) {
		asks = append(asks, service)
		if service == "fresh" {
			must(t, s.Declare(ctx, service, store.Declaration{Emits: []event.Emission{{Event: "cron.*.fired"}}}))
		}
	}})
	must(t, s.Declare(ctx, "producer", store.Declaration{Emits: []event.Emission{{Event: "cron.*.fired"}}}))
	for i, name := range []string{"cron.hourly.fired", "cron.daily.fired", "cron.fired", "cron.hourly.daily.fired", "cron.hourly.changed"} {
		e := emit(i + 1)
		e.Name = name
		before := all(t, s)
		head, err := s.Head(ctx)
		must(t, err)
		err = s.Deliver(ctx, e)
		if i < 2 {
			must(t, err)
			after := all(t, s)
			if len(after) != len(before)+1 || after[0].Name != name || after[0].Seq != head+1 || !reflect.DeepEqual(after[1:], before) || len(asks) != 0 {
				t.Fatal(after, asks)
			}
		} else if !errors.Is(err, store.ErrUndeclared) || len(asks) != i-1 || asks[len(asks)-1] != "producer" || !reflect.DeepEqual(all(t, s), before) {
			t.Fatal(err, asks)
		}
	}
	e := emit(10)
	e.Service = "fresh"
	e.Name = "cron.weekly.fired"
	must(t, s.Deliver(ctx, e))
	if len(asks) != 4 || asks[3] != "fresh" || all(t, s)[0].Name != e.Name {
		t.Fatal(asks, all(t, s))
	}
	withoutAsk, _, _ := openStore(t, store.Config{})
	if err := withoutAsk.Deliver(ctx, e); !errors.Is(err, store.ErrUndeclared) || len(all(t, withoutAsk)) != 0 {
		t.Fatal(err)
	}
}

func TestNamesPatternsAndSubscriberMembership(t *testing.T) {
	// R-YHS0-8X6R R-YK7T-0GO5 R-YQBA-XBDM R-YTZ0-2MLP
	for _, tc := range []struct {
		accept string
		name   bool
		sub    bool
	}{
		{"repo.pushed", true, true}, {"cron.hourly.fired", true, true}, {"a.b.c.d", true, true},
		{"cron.*.fired", false, false}, {"*.*", false, false}, {"*", false, true},
		{"pushed", false, false}, {"Repo.Pushed", false, false}, {"repo.", false, false}, {"cron.h*.fired", false, false}, {"", false, false},
	} {
		t.Run(tc.accept, func(t *testing.T) {
			s, _, _ := openStore(t, store.Config{})
			if event.Match(tc.accept, tc.accept) != tc.name {
				t.Fatal("name classification", tc)
			}
			must(t, s.Declare(ctx, "reader", store.Declaration{Accepts: []string{tc.accept}}))
			got := subs(t, s)
			if (len(got) == 1) != tc.sub {
				t.Fatal(tc, got)
			}
		})
	}
	if !event.Match("cron.*.fired", "cron.hourly.fired") || !event.Match("*.*", "repo.pushed") {
		t.Fatal("pattern classification")
	}
	now := instant
	s, _, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	must(t, s.Declare(ctx, "producer", store.Declaration{Emits: []event.Emission{{Event: "cron.*.fired"}}}))
	must(t, s.Declare(ctx, "patterns", store.Declaration{Accepts: []string{"cron.*.fired", "*.*"}}))
	must(t, s.Declare(ctx, "reader", store.Declaration{Accepts: []string{"cron.*.fired", "repo.pushed"}}))
	must(t, s.Declare(ctx, "all", store.Declaration{Accepts: []string{"cron.*.fired", "*"}}))
	e := emit(1)
	e.Name = "cron.hourly.fired"
	must(t, s.Deliver(ctx, e))
	if _, ok, err := s.Next(ctx, "patterns"); !errors.Is(err, store.ErrNoSubscriber) || ok {
		t.Fatal(ok, err)
	}
	if _, ok, err := s.Next(ctx, "reader"); err != nil || ok || one(t, s, "reader").Cursor != 1 {
		t.Fatal(ok, err)
	}
	if got, ok, err := s.Next(ctx, "all"); err != nil || !ok || got.ID != e.ID {
		t.Fatal(got, ok, err)
	}
	must(t, s.Pause(ctx, "all", 1, "stopped"))
	before := one(t, s, "all")
	now = now.Add(time.Second)
	must(t, s.Declare(ctx, "all", store.Declaration{Accepts: []string{"cron.*.fired"}}))
	gone := one(t, s, "all")
	if gone.Status != store.StatusGone || gone.Cursor != before.Cursor || gone.Reason != nil || gone.Since != now.UTC().Truncate(time.Microsecond) {
		t.Fatal(gone)
	}
	now = now.Add(time.Second)
	must(t, s.Declare(ctx, "all", store.Declaration{Accepts: []string{"*.*"}}))
	must(t, s.Forget(ctx, "all"))
	if !reflect.DeepEqual(gone, one(t, s, "all")) {
		t.Fatal("gone subscriber changed")
	}
}

func TestPatternCatalogAndFilters(t *testing.T) {
	// R-YLFP-E8EU R-YMNL-S05J
	now := instant
	s, _, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	must(t, s.Declare(ctx, "producer", store.Declaration{Emits: []event.Emission{
		{Event: "cron.*.fired", Attrs: []string{"first", "ordered"}},
		{Event: "cron.hourly.*", Attrs: []string{"second"}},
		{Event: "cron.hourly.fired", Attrs: []string{"exact", "ordered"}},
		{Event: "cron.hourly.fired", Attrs: []string{"ignored"}},
		{Event: "cron.*.fired", Attrs: []string{"ignored"}},
	}}))
	must(t, s.Declare(ctx, "a", store.Declaration{Emits: []event.Emission{{Event: "*.*.*"}}}))
	must(t, s.Declare(ctx, "pattern", store.Declaration{Accepts: []string{"cron.*.fired", "cron.*.fired"}}))
	must(t, s.Declare(ctx, "exact", store.Declaration{Accepts: []string{"cron.hourly.fired", "cron.hourly.fired"}}))
	must(t, s.Declare(ctx, "wild", store.Declaration{Accepts: []string{"*", "*", "cron.hourly.fired"}}))
	for i, name := range []string{"cron.daily.fired", "cron.hourly.fired", "cron.hourly.fired"} {
		now = now.Add(time.Second)
		e := emit(i + 1)
		e.Name = name
		must(t, s.Deliver(ctx, e))
	}
	got, err := s.Catalog(ctx, "", "")
	must(t, err)
	want := []store.CatalogEntry{
		{Event: "*.*.*", Emits: []store.Producer{{Service: "a", Attrs: []string{}}}, Accepts: []string{"wild"}},
		{Event: "cron.*.fired", Emits: []store.Producer{{Service: "producer", Attrs: []string{"first", "ordered"}}}, Accepts: []string{"pattern", "wild"}},
		{Event: "cron.daily.fired", Emits: []store.Producer{{Service: "a", Attrs: []string{}}, {Service: "producer", Attrs: []string{"first", "ordered"}}}, Accepts: []string{"wild"}, Count: 1, LastSeen: instant.Add(time.Second).UTC().Truncate(time.Microsecond)},
		{Event: "cron.hourly.*", Emits: []store.Producer{{Service: "producer", Attrs: []string{"second"}}}, Accepts: []string{"wild"}},
		{Event: "cron.hourly.fired", Emits: []store.Producer{{Service: "a", Attrs: []string{}}, {Service: "producer", Attrs: []string{"exact", "ordered"}}}, Accepts: []string{"exact", "wild"}, Count: 2, LastSeen: now.UTC().Truncate(time.Microsecond)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, tc := range []struct {
		service, name string
		indices       []int
	}{{"producer", "", []int{1, 2, 3, 4}}, {"a", "", []int{0, 2, 4}}, {"pattern", "", []int{1}}, {"wild", "", []int{0, 1, 2, 3, 4}}, {"exact", "", []int{4}}, {"", "cron.*.fired", []int{1}}, {"producer", "cron.daily.fired", []int{2}}, {"pattern", "cron.hourly.fired", []int{}}, {"absent", "", []int{}}} {
		filtered, err := s.Catalog(ctx, tc.service, tc.name)
		must(t, err)
		expected := []store.CatalogEntry{}
		for _, i := range tc.indices {
			expected = append(expected, want[i])
		}
		if !reflect.DeepEqual(filtered, expected) {
			t.Fatal(tc, filtered)
		}
	}
	// With the exact element removed, both patterns match and the first wins.
	must(t, s.Declare(ctx, "producer", store.Declaration{Emits: []event.Emission{
		{Event: "cron.*.fired", Attrs: []string{"first", "ordered"}},
		{Event: "cron.hourly.*", Attrs: []string{"second"}},
	}}))
	matched, err := s.Catalog(ctx, "", "cron.hourly.fired")
	must(t, err)
	firstPattern := want[4]
	firstPattern.Emits = []store.Producer{{Service: "a", Attrs: []string{}}, {Service: "producer", Attrs: []string{"first", "ordered"}}}
	if !reflect.DeepEqual(matched, []store.CatalogEntry{firstPattern}) {
		t.Fatal("first matching pattern did not supply attrs", matched)
	}
	must(t, s.Forget(ctx, "producer"))
	filtered, err := s.Catalog(ctx, "producer", "")
	must(t, err)
	if filtered == nil || len(filtered) != 0 {
		t.Fatal(filtered)
	}
}

func TestDeclarationRowsKeepPatternsAndOrder(t *testing.T) {
	// R-YWES-U633
	now := instant
	s, d, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	declaration := store.Declaration{Emits: []event.Emission{{Event: "cron.*.fired", Attrs: []string{"z", "a", "z"}}, {Event: "cron.hourly.fired"}}, Accepts: []string{"cron.*.fired", "*", "cron.*.fired"}}
	must(t, s.Declare(ctx, "cron", declaration))
	now = now.Add(time.Second)
	must(t, s.Declare(ctx, "cron", declaration))
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM declarations").Scan(&count); err != nil {
			return err
		}
		var service, emits, accepts string
		var asked int64
		if err := tx.QueryRow("SELECT service,emits,accepts,asked FROM declarations").Scan(&service, &emits, &accepts, &asked); err != nil {
			return err
		}
		if count != 1 || service != "cron" || emits != `[{"event":"cron.*.fired","attrs":["z","a","z"]},{"event":"cron.hourly.fired","attrs":[]}]` || accepts != `["cron.*.fired","*","cron.*.fired"]` || asked != now.UnixMicro() {
			t.Fatal(count, service, emits, accepts, asked)
		}
		return nil
	}))
	must(t, s.Forget(ctx, "cron"))
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM declarations").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatal(count)
		}
		return nil
	}))
}
