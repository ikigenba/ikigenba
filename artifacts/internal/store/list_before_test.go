package store_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/artifacts/internal/store"
)

// R-ZEAY-ROWI R-ZFIV-5GN7
func TestListBeforeWindows(t *testing.T) {
	f := setup(t, nil)
	empty, err := f.s.ListBefore(ctx, nil, 3)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	paths := []string{"a0", "a/b", "a.b", "a-b", "a", "B"}
	want := make([]store.Artifact, len(paths))
	for i := len(paths) - 1; i >= 0; i-- {
		d := draft(paths[i])
		d.OwnerID = paths[i]
		d.Expires = f.now.Add(time.Hour)
		want[i] = put(t, f, d, paths[i])
	}
	f.now = f.now.Add(2 * time.Hour)
	for _, tc := range []struct {
		name       string
		before     *string
		n          int
		start, end int
	}{
		{name: "all", n: 10, end: 6},
		{name: "exact", n: 6, end: 6},
		{name: "last", n: 2, end: 2},
		{name: "one", n: 1, end: 1},
		{name: "zero", n: 0},
		{name: "negative", n: -1},
		{name: "minimum", n: math.MinInt},
		{name: "empty position", before: new(""), n: 10, start: 6, end: 6},
		{name: "existing position", before: new("a0"), n: 2, start: 1, end: 3},
		{name: "short window", before: new("a"), n: 2, start: 5, end: 6},
		{name: "absent position", before: new("a-"), n: 2, start: 4, end: 6},
		{name: "arbitrary position", before: new("a%"), n: 2, start: 4, end: 6},
		{name: "bound parameter", before: new("a' OR 1=1 --"), n: 2, start: 4, end: 6},
		{name: "first position", before: new("B"), n: 2, start: 6, end: 6},
		{name: "before start", before: new("A"), n: 2, start: 6, end: 6},
		{name: "past end", before: new("z"), n: 2, end: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.s.ListBefore(ctx, tc.before, tc.n)
			if err != nil || got == nil || !reflect.DeepEqual(got, want[tc.start:tc.end]) {
				t.Fatalf("got %+v, %v; want %+v", got, err, want[tc.start:tc.end])
			}
		})
	}
	if _, err := f.s.Delete(ctx, "a0", "a0"); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.ListBefore(ctx, new("a0"), 2)
	if err != nil || !reflect.DeepEqual(got, want[1:3]) {
		t.Fatal(got, err)
	}
}

// R-FSO1-3ZKG R-FTVX-HRB5
func TestListBeforeUnavailable(t *testing.T) {
	for _, condition := range []string{"done", "failing", "done and failing"} {
		t.Run(condition, func(t *testing.T) {
			f := setup(t, nil)
			a := put(t, f, draft("kept"), "body")
			files := snapshot(t, f)
			c := ctx
			if condition != "failing" {
				done, cancel := context.WithCancel(ctx)
				cancel()
				c = done
			}
			if condition != "done" {
				f.d.SetFailing(true)
			}
			for _, before := range []*string{nil, new("z")} {
				for _, n := range []int{2, 0, -1} {
					_, err := f.s.ListBefore(c, before, n)
					if !errors.Is(err, store.ErrUnreachable) {
						t.Fatal(err)
					}
					if condition != "failing" && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				}
			}
			f.d.SetFailing(false)
			got, err := f.s.ListBefore(ctx, nil, 2)
			if err != nil || !reflect.DeepEqual(got, []store.Artifact{a}) {
				t.Fatal(got, err)
			}
			if !reflect.DeepEqual(files, snapshot(t, f)) {
				t.Fatal("blob directory changed")
			}
			if read(t, f.s, a.Path) != "body" {
				t.Fatal("blob changed")
			}
		})
	}
}
