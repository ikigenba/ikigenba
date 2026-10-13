package store_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	event "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

func TestCatalog(t *testing.T) {
	// R-YLFP-E8EU R-YMNL-S05J
	now := instant
	s, _, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	must(t, s.Declare(ctx, "z", store.Declaration{Emits: []event.Emission{{Event: "item.created", Attrs: []string{"b", "a"}}, {Event: "item.created", Attrs: []string{"ignored"}}, {Event: "item.never"}}, Accepts: []string{"item.created", "item.created"}}))
	must(t, s.Declare(ctx, "a", store.Declaration{Emits: []event.Emission{{Event: "item.created"}}, Accepts: []string{"*", "*"}}))
	declare(t, s, "producer")
	must(t, s.Deliver(ctx, emit(1)))
	now = now.Add(time.Second)
	must(t, s.Deliver(ctx, emit(2)))
	orphan := emit(3)
	orphan.Service = "old"
	orphan.Name = "old.observed"
	must(t, s.Declare(ctx, "old", store.Declaration{Emits: []event.Emission{{Event: "old.observed"}}}))
	must(t, s.Deliver(ctx, orphan))
	must(t, s.Forget(ctx, "old"))
	got, err := s.Catalog(ctx, "", "")
	must(t, err)
	expected := []store.CatalogEntry{
		{Event: "item.changed", Emits: []store.Producer{{Service: "producer", Attrs: []string{}}}, Accepts: []string{"a"}},
		{Event: "item.created", Emits: []store.Producer{{Service: "a", Attrs: []string{}}, {Service: "producer", Attrs: []string{"count", "flag", "text"}}, {Service: "z", Attrs: []string{"b", "a"}}}, Accepts: []string{"a", "z"}, Count: 2, LastSeen: now.UTC().Truncate(time.Microsecond)},
		{Event: "item.never", Emits: []store.Producer{{Service: "z", Attrs: []string{}}}, Accepts: []string{"a"}},
		{Event: "old.observed", Emits: []store.Producer{}, Accepts: []string{"a"}, Count: 1, LastSeen: now.UTC().Truncate(time.Microsecond)},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("got %#v want %#v", got, expected)
	}
	for _, tt := range []struct {
		service, event string
		indices        []int
	}{{"a", "", []int{0, 1, 2, 3}}, {"z", "", []int{1, 2}}, {"old", "", []int{}}, {"missing", "", []int{}}, {"", "item.created", []int{1}}, {"z", "item.never", []int{2}}, {"producer", "old.observed", []int{}}} {
		v, err := s.Catalog(ctx, tt.service, tt.event)
		must(t, err)
		want := []store.CatalogEntry{}
		for _, i := range tt.indices {
			want = append(want, expected[i])
		}
		if !reflect.DeepEqual(v, want) {
			t.Fatal(tt, v)
		}
	}
}

func TestSearchFiltersAndTimeBoundaries(t *testing.T) {
	// R-E7IH-AI8J R-90RT-CPME
	s, _, _ := openStore(t, store.Config{DepthMax: 3})
	declare(t, s, "producer")
	declare(t, s, "other")
	a := emit(1)
	a.Time = instant.UTC().Truncate(time.Microsecond)
	a.User = ""
	a.RequestID = ""
	a.Attrs = event.Attrs{"string": "true", "bool": true, "integer": int64(9), "float": 1.5, "zero": 0, "large": uint64(math.MaxUint64)}
	b := emit(2)
	b.Service = "other"
	b.Name = "item.changed"
	b.Time = a.Time.Add(time.Microsecond)
	b.Cause = "evt_00000000000000ff"
	b.Depth = 1
	b.Attrs = event.Attrs{"string": "false", "bool": false, "integer": 9.5}
	c := emit(3)
	c.Time = a.Time.Add(2 * time.Microsecond)
	c.Attrs = event.Attrs{}
	must(t, s.Deliver(ctx, a))
	must(t, s.Deliver(ctx, b))
	must(t, s.Deliver(ctx, c))
	empty := ""
	alice := "alice"
	cause := b.Cause
	request := "request"
	boundary := a.Time.Add(time.Nanosecond)
	exact := b.Time
	cases := []struct {
		name   string
		filter store.Filter
		ids    []int64
	}{
		{"all", store.Filter{}, []int64{3, 2, 1}},
		{"since submicro", store.Filter{Since: &boundary}, []int64{3, 2}},
		{"until submicro", store.Filter{Until: &boundary}, []int64{1}},
		{"since inclusive", store.Filter{Since: &exact}, []int64{3, 2}},
		{"until exclusive", store.Filter{Until: &exact}, []int64{1}},
		{"services", store.Filter{Services: []string{"missing", "other"}}, []int64{2}},
		{"events", store.Filter{Events: []string{"item.created", "missing"}}, []int64{3, 1}},
		{"user empty", store.Filter{User: &empty}, []int64{1}},
		{"user", store.Filter{User: &alice}, []int64{3, 2}},
		{"request empty", store.Filter{RequestID: &empty}, []int64{1}},
		{"request", store.Filter{RequestID: &request}, []int64{3, 2}},
		{"cause empty", store.Filter{Cause: &empty}, []int64{3, 1}},
		{"cause", store.Filter{Cause: &cause}, []int64{2}},
		{"combined", store.Filter{Services: []string{"producer"}, Events: []string{"item.created"}, User: &empty, RequestID: &empty, Cause: &empty, Attrs: event.Attrs{"bool": true, "integer": int8(9)}}, []int64{1}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p, err := s.Search(ctx, tt.filter, nil, 20)
			must(t, err)
			ids := []int64{}
			for _, e := range p {
				ids = append(ids, e.Seq)
			}
			if !reflect.DeepEqual(ids, tt.ids) {
				t.Fatal(p)
			}
		})
	}
	attrCases := []struct {
		key   string
		value any
		ids   []int64
	}{{"string", "true", []int64{1}}, {"bool", true, []int64{1}}, {"bool", false, []int64{2}}, {"integer", int(9), []int64{1}}, {"integer", uint16(9), []int64{1}}, {"integer", float64(9), []int64{1}}, {"integer", 9.5, []int64{2}}, {"float", float32(1.5), []int64{1}}, {"zero", 0, []int64{1}}, {"large", uint64(math.MaxUint64), []int64{1}}, {"integer", "9", []int64{}}, {"absent", 0, []int64{}}, {"string", true, []int64{}}, {"zero", nil, []int64{}}, {"zero", []int{0}, []int64{}}, {"zero", map[string]int{}, []int64{}}, {"float", math.NaN(), []int64{}}, {"float", math.Inf(1), []int64{}}, {"zero", complex(0, 0), []int64{}}}
	for _, tt := range attrCases {
		p, err := s.Search(ctx, store.Filter{Attrs: event.Attrs{tt.key: tt.value}}, nil, 10)
		must(t, err)
		ids := []int64{}
		for _, e := range p {
			ids = append(ids, e.Seq)
		}
		if !reflect.DeepEqual(ids, tt.ids) {
			t.Fatal(tt, p)
		}
	}
}

func TestSearchSequenceWindows(t *testing.T) {
	// R-90RT-CPME
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "producer")
	for n := 1; n <= 6; n++ {
		e := emit(n)
		if n%2 == 0 {
			e.Name = "item.changed"
		}
		must(t, s.Deliver(ctx, e))
	}
	filters := []store.Filter{{}, {Events: []string{"item.changed"}}, {User: stringPtr("missing")}, {Attrs: event.Attrs{"flag": []bool{true}}}}
	for _, f := range filters {
		full, err := s.Search(ctx, f, nil, 100)
		must(t, err)
		for _, n := range []int{1, 2, 3, 6, 7, math.MaxInt} {
			var before *int64
			seen := []event.Event{}
			for {
				window, err := s.Search(ctx, f, before, n)
				must(t, err)
				if window == nil || len(window) > n {
					t.Fatal(window)
				}
				seen = append(seen, window...)
				if len(seen) > 6 {
					t.Fatal("window traversal repeated events", seen)
				}
				if len(window) < n {
					break
				}
				position := window[len(window)-1].Seq
				before = &position
			}
			if !reflect.DeepEqual(seen, full) {
				t.Fatal(n, seen, full)
			}
		}
		for _, n := range []int{0, -1, math.MinInt} {
			window, err := s.Search(ctx, f, nil, n)
			must(t, err)
			if window == nil || len(window) != 0 {
				t.Fatal(n, window)
			}
		}
	}
	for _, tt := range []struct {
		before int64
		want   []int64
	}{
		{math.MaxInt64, []int64{6, 4}}, {5, []int64{4, 2}}, {4, []int64{2}}, {1, []int64{}}, {0, []int64{}}, {math.MinInt64, []int64{}},
	} {
		window, err := s.Search(ctx, store.Filter{Events: []string{"item.changed"}}, &tt.before, 2)
		must(t, err)
		seqs := []int64{}
		for _, e := range window {
			seqs = append(seqs, e.Seq)
		}
		if window == nil || !reflect.DeepEqual(seqs, tt.want) {
			t.Fatal(tt, window)
		}
	}
	first, err := s.Search(ctx, store.Filter{Events: []string{"item.changed"}}, nil, 2)
	must(t, err)
	position := first[len(first)-1].Seq
	window, err := s.Search(ctx, store.Filter{}, &position, 2)
	must(t, err)
	if len(window) != 2 || window[0].Seq != 3 || window[1].Seq != 2 {
		t.Fatal(window)
	}
	must(t, s.Sweep(ctx, instant.Add(time.Second)))
	window, err = s.Search(ctx, store.Filter{}, &position, 2)
	must(t, err)
	if window == nil || len(window) != 0 {
		t.Fatal(window)
	}
	must(t, s.Deliver(ctx, emit(7)))
	position = 8
	window, err = s.Search(ctx, store.Filter{}, &position, 2)
	must(t, err)
	if len(window) != 1 || window[0].Seq != 7 {
		t.Fatal(window)
	}
}
func stringPtr(s string) *string { return &s }

func TestReadPurityAndReturnedCopies(t *testing.T) {
	// R-2XTW-G42H
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	must(t, s.Deliver(ctx, emit(1)))
	must(t, s.Pause(ctx, "reader", 1, "failed"))
	ch := s.Changed()
	firstDs, err := s.Declarations(ctx)
	must(t, err)
	firstSubs := subs(t, s)
	firstHead, err := s.Head(ctx)
	must(t, err)
	firstCat, err := s.Catalog(ctx, "", "")
	must(t, err)
	firstPage, err := s.Search(ctx, store.Filter{}, nil, 1)
	must(t, err)
	for i := 0; i < 3; i++ {
		ds, err := s.Declarations(ctx)
		must(t, err)
		v := subs(t, s)
		h, err := s.Head(ctx)
		must(t, err)
		cat, err := s.Catalog(ctx, "", "")
		must(t, err)
		p, err := s.Search(ctx, store.Filter{}, nil, 1)
		must(t, err)
		if !reflect.DeepEqual(ds, firstDs) || !reflect.DeepEqual(v, firstSubs) || h != firstHead || !reflect.DeepEqual(cat, firstCat) || !reflect.DeepEqual(p, firstPage) {
			t.Fatal("read changed answers")
		}
	}
	firstDs["producer"].Emits[0].Attrs[0] = "changed"
	firstSubs[0].Reason.Error = "changed"
	firstCat[0].Emits[0].Attrs = append(firstCat[0].Emits[0].Attrs, "changed")
	firstPage[0].Attrs["text"] = "changed"
	ds, err := s.Declarations(ctx)
	must(t, err)
	if ds["producer"].Emits[0].Attrs[0] != "count" || one(t, s, "reader").Reason.Error != "failed" || all(t, s)[0].Attrs["text"] != "value" {
		t.Fatal("read aliases retained state")
	}
	assertOpen(t, ch)
}

func TestSearchExtremeTimeBounds(t *testing.T) {
	// R-E7IH-AI8J R-90RT-CPME
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "producer")
	must(t, s.Deliver(ctx, emit(1)))
	future := time.Date(300000, 1, 1, 0, 0, 0, 0, time.UTC)
	past := time.Date(-300000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		filter store.Filter
		count  int
	}{{store.Filter{Since: &future}, 0}, {store.Filter{Until: &future}, 1}, {store.Filter{Since: &past}, 1}, {store.Filter{Until: &past}, 0}} {
		p, err := s.Search(ctx, tt.filter, nil, 10)
		must(t, err)
		if len(p) != tt.count || p == nil {
			t.Fatal(tt, p)
		}
	}
}
