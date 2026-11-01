package tools_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// R-HQAP-BIKE R-IOR1-8DA2 R-IPYX-M50R R-IR6T-ZWRG
func TestConfigurationTables(t *testing.T) {
	r := newRig(t)
	rs := success[tools.RuleList](t, r, owner, tools.ToolListRules, tools.NoArgs{})
	equal(t, rs, tools.RuleList{Rules: []tools.Rule{{Host: "ld01", Directory: "/mnt/projects/biz", Command: "", Kind: "plan"}, {Host: "ld01", Directory: "/mnt/projects/ikigenba", Command: "", Kind: "build"}, {Host: "ld01", Directory: "/mnt/projects/sites-root", Command: "", Kind: "build"}}})
	ps := success[tools.PriceList](t, r, owner, tools.ToolListPrices, tools.NoArgs{})
	equal(t, len(ps.Prices), 12)
	equal(t, ps.Prices[0], tools.Price{Model: "claude-fable-5-1", Effective: "2026-01-01", Input: "10", Cached: "0.25", Output: "50", Reasoning: "50"})
	equal(t, success[tools.AwayLimit](t, r, owner, tools.ToolGetAwayLimit, tools.NoArgs{}), tools.AwayLimit{Minutes: 15})
	equal(t, success[tools.Rule](t, r, owner, tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz"}), rs.Rules[0])
	equal(t, success[tools.RuleList](t, r, owner, tools.ToolListRules, tools.NoArgs{}).Rules, rs.Rules[1:])
	p := tools.SetPriceArgs{Model: "m-new", Effective: "2026-03-01", Input: "5.500", Cached: "0.50", Output: "25", Reasoning: "025"}
	want := tools.Price{Model: p.Model, Effective: p.Effective, Input: "5.5", Cached: "0.5", Output: "25", Reasoning: "25"}
	equal(t, success[tools.Price](t, r, owner, tools.ToolSetPrice, p), want)
	ps = success[tools.PriceList](t, r, owner, tools.ToolListPrices, tools.NoArgs{})
	equal(t, ps.Prices[len(ps.Prices)-1], want)
	equal(t, success[tools.AwayLimit](t, r, owner, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 1440}), tools.AwayLimit{Minutes: 1440})
	refused(t, r, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 1441}, store.AwayOutOfRange)
	equal(t, success[tools.AwayLimit](t, r, owner, tools.ToolGetAwayLimit, tools.NoArgs{}), tools.AwayLimit{Minutes: 1440})
}

// R-IHFM-XQTW R-IINJ-BIKL R-HBNW-Q9O2 R-HAG0-CHXD
func TestPushLatestOrderPrecisionAndOwnership(t *testing.T) {
	r := newRig(t)
	x := entry("s", now)
	x.Host = " host "
	x.Directory = "/work/space here"
	x.Command = &tools.Command{Name: " /command ", Argument: "argument\n\t"}
	x.Turns = []tools.PushTurn{{Moment: "2026-10-13T16:02:11.2509+02:00", Whose: "agent", Model: " model ", Input: 1, Cached: 2, Output: 3, Reasoning: 4}, {Moment: "2026-10-13T14:02:10Z", Whose: "person"}}
	want := tools.Latest{Tool: "claude", ID: "s", Latest: ptr("2026-10-13T14:02:11.250Z")}
	equal(t, push(t, r, owner, x), tools.LatestList{Sessions: []tools.Latest{want}})
	empty := entry("not-held", now)
	empty.Turns = []tools.PushTurn{}
	equal(t, push(t, r, owner, empty), tools.LatestList{Sessions: []tools.Latest{{Tool: "claude", ID: "not-held"}}})
	equal(t, push(t, r, owner, x, x), tools.LatestList{Sessions: []tools.Latest{want, want}})
	row := list(t, r, tools.ListSessionsArgs{}).Sessions[0]
	equal(t, row.Host, x.Host)
	equal(t, row.Directory, x.Directory)
	equal(t, row.Command, x.Command)
	equal(t, row.Person, owner.Email)
	equal(t, row.Tokens, tools.Tokens{Input: 1, Cached: 2, Output: 3, Reasoning: 4, Total: 10})
	equal(t, row.Unpriced, int64(10))
	equal(t, row.WallSeconds, int64(1))
	y := entry("s", now)
	y.Tool = "codex"
	push(t, r, other, y)
	v := call(t, r, other, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{x}})
	if !v.IsError() {
		t.Fatal("another owner's write accepted")
	}
	equal(t, wire(t, v).Content[0].Text, store.OtherOwner)
	latest := success[tools.LatestList](t, r, other, tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{{Tool: "codex", ID: "s"}, {Tool: "claude", ID: "s"}, {Tool: "claude", ID: "missing"}, {Tool: "claude", ID: "s"}}})
	equal(t, latest, tools.LatestList{Sessions: []tools.Latest{{Tool: "codex", ID: "s", Latest: ptr(now.Format("2006-01-02T15:04:05.000Z07:00"))}, want, {Tool: "claude", ID: "missing"}, want}})
	refused(t, r, tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{}}, store.NoSessions)
	// Whole seconds truncate an otherwise subsecond wall duration.
	z := entry("duration", now)
	z.Turns = append(z.Turns, tools.PushTurn{Moment: now.Add(time.Minute + 59*time.Second + 999*time.Millisecond).Format(time.RFC3339Nano), Whose: "agent"})
	push(t, r, owner, z)
	for _, s := range list(t, r, tools.ListSessionsArgs{}).Sessions {
		if s.ID == "duration" {
			equal(t, s.WallSeconds, int64(119))
			return
		}
	}
	t.Fatal("duration row missing")
}

// R-IHFM-XQTW
func TestPushBoundsAreStoreBounds(t *testing.T) {
	r := newRig(t)
	xs := make([]tools.PushSession, store.MaxPushSessions)
	for i := range xs {
		xs[i] = entry(fmt.Sprintf("s-%03d", i), now.Add(time.Duration(i)*time.Second))
	}
	equal(t, len(push(t, r, owner, xs...).Sessions), store.MaxPushSessions)
	before := list(t, r, tools.ListSessionsArgs{Limit: ptr(paging.MaxLimit)})
	xs = append(xs, entry("overflow", now))
	for i := range xs {
		xs[i].Turns = []tools.PushTurn{}
	}
	refused(t, r, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: xs}, store.TooManySessions)
	equal(t, list(t, r, tools.ListSessionsArgs{Limit: ptr(paging.MaxLimit)}), before)
}
func validCalls() []struct {
	name string
	args any
} {
	return []struct {
		name string
		args any
	}{
		{tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{entry("valid", now)}}}, {tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{{Tool: "claude", ID: "valid"}}}}, {tools.ToolListSessions, tools.ListSessionsArgs{}}, {tools.ToolTotalSessions, weekArgs()}, {tools.ToolListRules, tools.NoArgs{}}, {tools.ToolAddRule, tools.AddRuleArgs{Host: "host", Directory: "/work", Kind: "build"}}, {tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz"}}, {tools.ToolListPrices, tools.NoArgs{}}, {tools.ToolSetPrice, tools.SetPriceArgs{Model: "model", Effective: "2026-01-01", Input: "1", Cached: "2", Output: "3", Reasoning: "4"}}, {tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "claude-opus-5-5", Effective: "2026-01-01"}}, {tools.ToolGetAwayLimit, tools.NoArgs{}}, {tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}},
	}
}

// R-IG7Q-JZ37 R-HHRE-N4DJ R-IJVF-PABA R-IDRX-SFLT
func TestRefusalsAndDatabaseFailures(t *testing.T) {
	r := newRig(t)
	refused(t, r, tools.ToolAddRule, tools.AddRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz", Kind: "operate"}, store.RuleHeld)
	x := entry("bad-tool", now)
	x.Tool = "gemini"
	refused(t, r, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{x}}, store.UnknownTool)
	refused(t, r, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 0}, store.AwayOutOfRange)
	q := weekArgs()
	q.Group = "day"
	refused(t, r, tools.ToolTotalSessions, q, store.UnknownGroup)
	for _, failing := range []bool{false, true} {
		r.d.SetFailing(failing)
		refused(t, r, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{}}, store.NoSessions)
		for _, limit := range []int{0, paging.MaxLimit + 1} {
			_, err := paging.Window(&limit)
			if err == nil {
				t.Fatal("expected paging error")
			}
			refused(t, r, tools.ToolListSessions, tools.ListSessionsArgs{Page: ptr("bogus"), Limit: &limit, Kind: ptr("invalid")}, err.Error())
		}
		refused(t, r, tools.ToolListSessions, tools.ListSessionsArgs{Page: ptr((paging.Token{Tool: "list_widgets"}).Encode(now.UnixMilli(), "claude", "s"))}, paging.BadPage)
	}
	for _, tc := range validCalls() {
		refused(t, r, tc.name, tc.args, store.Unreachable)
	}
	r.d.SetFailing(false)
	_ = list(t, r, tools.ListSessionsArgs{Limit: ptr(paging.MaxLimit)})
	_ = success[tools.SessionList](t, r, owner, tools.ToolListSessions, map[string]any{"page": nil, "limit": nil})
	_ = list(t, r, tools.ListSessionsArgs{Page: ptr("")})
	if err := r.d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range validCalls() {
		refused(t, r, tc.name, tc.args, store.Unreachable)
	}
}
