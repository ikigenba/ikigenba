package tools_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// R-IL3C-321Z R-IDRX-SFLT
func TestListWindows(t *testing.T) {
	for _, n := range []int{0, 30, 50, 60} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := newRig(t)
			var xs []tools.PushSession
			for i := range n {
				xs = append(xs, entry(fmt.Sprintf("root-%03d", i), now.Add(time.Duration(i)*time.Second)))
			}
			if n > 0 {
				child := entry("child", now)
				child.Parent = &tools.SessionRef{Tool: "claude", ID: xs[0].ID}
				xs = append(xs, child)
				push(t, r, owner, xs...)
			}
			def := list(t, r, tools.ListSessionsArgs{})
			wantDefault := min(n, paging.DefaultLimit)
			equal(t, len(def.Sessions), wantDefault)
			equal(t, def.Next != nil, n > paging.DefaultLimit)
			got := []string{}
			var page *string
			for {
				v := list(t, r, tools.ListSessionsArgs{Page: page, Limit: ptr(25)})
				wantLen := min(25, n-len(got))
				equal(t, len(v.Sessions), wantLen)
				for _, s := range v.Sessions {
					got = append(got, s.ID)
				}
				if v.Next == nil {
					break
				}
				if len(got) >= n {
					t.Fatal("spurious next")
				}
				page = v.Next
			}
			want := []string{}
			for i := n - 1; i >= 0; i-- {
				want = append(want, fmt.Sprintf("root-%03d", i))
			}
			equal(t, got, want)
		})
	}
}

// R-IMB8-GTSO R-IDRX-SFLT
func TestPagingUsesPositionAndCurrentStore(t *testing.T) {
	r := newRig(t)
	base := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	x := entry("x", base.Add(11*time.Hour))
	a := entry("a", base.Add(10*time.Hour))
	a.Tool = "codex"
	c := entry("c", base.Add(10*time.Hour))
	b := entry("b", base.Add(10*time.Hour))
	z := entry("z", base.Add(9*time.Hour))
	push(t, r, owner, x, a, c, b, z)
	first := list(t, r, tools.ListSessionsArgs{Limit: ptr(2)})
	equal(t, []string{first.Sessions[0].ID, first.Sessions[1].ID}, []string{"x", "a"})
	if first.Next == nil {
		t.Fatal("missing next")
	}
	var ms int64
	var tool, id string
	if err := (paging.Token{Tool: tools.ToolListSessions}).Decode(*first.Next, &ms, &tool, &id); err != nil {
		t.Fatal(err)
	}
	equal(t, ms, base.Add(10*time.Hour).UnixMilli())
	equal(t, tool, "codex")
	equal(t, id, "a")
	n := entry("n", base.Add(10*time.Hour))
	n.Tool = "grok"
	d := entry("d", base.Add(10*time.Hour))
	push(t, r, owner, n, d)
	second := list(t, r, tools.ListSessionsArgs{Page: first.Next, Limit: ptr(2)})
	equal(t, []string{second.Sessions[0].ID, second.Sessions[1].ID}, []string{"d", "c"})
	if second.Next == nil {
		t.Fatal("missing second next")
	}
	third := list(t, r, tools.ListSessionsArgs{Page: second.Next, Limit: ptr(2)})
	equal(t, []string{third.Sessions[0].ID, third.Sessions[1].ID}, []string{"b", "z"})
	equal(t, third.Next, (*string)(nil))
	// A cursor may identify a position that no longer names a held root.
	arbitrary := (paging.Token{Tool: tools.ToolListSessions}).Encode(base.Add(10*time.Hour).UnixMilli(), "claude", "cc")
	result := list(t, r, tools.ListSessionsArgs{Page: &arbitrary, Host: ptr("test-host"), Limit: ptr(10)})
	equal(t, []string{result.Sessions[0].ID, result.Sessions[1].ID, result.Sessions[2].ID}, []string{"c", "b", "z"})
	equal(t, result.Next, (*string)(nil))
}

// R-INJ4-ULJD
func TestTotalsIncludeEmptyPeriods(t *testing.T) {
	r := newRig(t)
	push(t, r, owner, secondSession())
	got := totals(t, r, tools.TotalSessionsArgs{Period: "week", From: "2026-10-12", To: ptr("2026-10-26"), Group: "kind"})
	equal(t, len(got.Periods), 3)
	equal(t, got.Periods[0], tools.PeriodTotals{Start: "2026-10-12", Groups: []tools.GroupTotals{}})
	equal(t, got.Periods[1].Start, "2026-10-19")
	equal(t, len(got.Periods[1].Groups), 1)
	equal(t, got.Periods[2], tools.PeriodTotals{Start: "2026-10-26", Groups: []tools.GroupTotals{}})
}
