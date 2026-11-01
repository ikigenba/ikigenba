package tools_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

func sample() []tools.PushSession {
	return []tools.PushSession{
		{Tool: "claude", ID: "r", Host: "ld01", Directory: "/mnt/projects/ikigenba/wip", Command: &tools.Command{Name: "/deliver", Argument: "~/Documents/x.md"}, Turns: []tools.PushTurn{
			{Moment: "2026-10-13T10:00:00Z", Whose: "person"}, {Moment: "2026-10-13T10:00:30Z", Whose: "agent", Model: "claude-opus-5-5", Input: 2000000, Cached: 500000, Output: 100000}, {Moment: "2026-10-13T10:02:30Z", Whose: "person"}, {Moment: "2026-10-13T10:03:00Z", Whose: "agent", Model: "claude-opus-5-5"}}},
		{Tool: "claude", ID: "r/a1", Parent: &tools.SessionRef{Tool: "claude", ID: "r"}, Host: "ld01", Directory: "/mnt/projects/ikigenba/wip", Turns: []tools.PushTurn{{Moment: "2026-10-13T10:00:40Z", Whose: "person"}, {Moment: "2026-10-13T10:01:40Z", Whose: "agent", Model: "claude-haiku-5-5", Input: 50000}}},
	}
}
func sampleRow() tools.Session {
	return tools.Session{Tool: "claude", ID: "r", Host: "ld01", Person: "e", Directory: "/mnt/projects/ikigenba/wip", Headless: false, Command: &tools.Command{Name: "/deliver", Argument: "~/Documents/x.md"}, First: "2026-10-13T10:00:00.000Z", Last: "2026-10-13T10:03:00.000Z", Kind: "build", AttentionSeconds: 120, WallSeconds: 180, AgentSeconds: 120, Tokens: tools.Tokens{Input: 2050000, Cached: 500000, Output: 100000, Reasoning: 0, Total: 2650000}, Unpriced: 0, Dollars: "10.11", Subagents: 1}
}
func sampleGroup() tools.GroupTotals {
	return tools.GroupTotals{Group: "/deliver", AttentionSeconds: 120, WallSeconds: 180, AgentSeconds: 120, Tokens: tools.Tokens{Input: 2050000, Cached: 500000, Output: 100000, Reasoning: 0, Total: 2650000}, Unpriced: 0, Dollars: "10.11", Sessions: 1}
}
func weekArgs() tools.TotalSessionsArgs {
	return tools.TotalSessionsArgs{Period: "week", From: "2026-10-13", Group: "command"}
}
func list(t *testing.T, r *rig, a tools.ListSessionsArgs) tools.SessionList {
	t.Helper()
	return success[tools.SessionList](t, r, owner, tools.ToolListSessions, a)
}
func totals(t *testing.T, r *rig, a tools.TotalSessionsArgs) tools.Totals {
	t.Helper()
	return success[tools.Totals](t, r, owner, tools.ToolTotalSessions, a)
}
func seedSample(t *testing.T, r *rig) { t.Helper(); push(t, r, owner, sample()...) }

// R-HYTZ-ZWR9 R-HAG0-CHXD R-H983-YQ6O R-HBNW-Q9O2
func TestCompletePushAndFigures(t *testing.T) {
	r := newRig(t)
	latest := success[tools.LatestList](t, r, owner, tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{{Tool: "claude", ID: "r"}, {Tool: "claude", ID: "r/a1"}}})
	equal(t, latest, tools.LatestList{Sessions: []tools.Latest{{Tool: "claude", ID: "r"}, {Tool: "claude", ID: "r/a1"}}})
	want := tools.LatestList{Sessions: []tools.Latest{{Tool: "claude", ID: "r", Latest: ptr("2026-10-13T10:03:00.000Z")}, {Tool: "claude", ID: "r/a1", Latest: ptr("2026-10-13T10:01:40.000Z")}}}
	equal(t, push(t, r, owner, sample()...), want)
	q := tools.ListSessionsArgs{Command: ptr("/deliver")}
	equal(t, list(t, r, q), tools.SessionList{Sessions: []tools.Session{sampleRow()}})
	total := tools.Totals{Periods: []tools.PeriodTotals{{Start: "2026-10-12", Groups: []tools.GroupTotals{sampleGroup()}}}}
	equal(t, totals(t, r, weekArgs()), total)
	equal(t, push(t, r, owner, sample()...), want)
	equal(t, list(t, r, q), tools.SessionList{Sessions: []tools.Session{sampleRow()}})
	equal(t, totals(t, r, weekArgs()), total)
}

// R-I01W-DOHY R-IOR1-8DA2 R-IPYX-M50R R-IR6T-ZWRG
func TestChangesRecomputeExistingFigures(t *testing.T) {
	r := newRig(t)
	seedSample(t, r)
	key := tools.AddRuleArgs{Host: "ld01", Directory: "/mnt/projects/ikigenba/wip", Command: ptr("/deliver"), Kind: "publish"}
	wantRule := tools.Rule{Host: key.Host, Directory: key.Directory, Command: "/deliver", Kind: "publish"}
	equal(t, success[tools.Rule](t, r, owner, tools.ToolAddRule, key), wantRule)
	wantRow := sampleRow()
	wantRow.Kind = "publish"
	equal(t, list(t, r, tools.ListSessionsArgs{Kind: ptr("publish")}), tools.SessionList{Sessions: []tools.Session{wantRow}})
	equal(t, list(t, r, tools.ListSessionsArgs{Kind: ptr("build")}), tools.SessionList{Sessions: []tools.Session{}})
	q := weekArgs()
	q.Group = "kind"
	g := sampleGroup()
	g.Group = "publish"
	equal(t, totals(t, r, q), tools.Totals{Periods: []tools.PeriodTotals{{Start: "2026-10-12", Groups: []tools.GroupTotals{g}}}})
	equal(t, success[tools.Rule](t, r, owner, tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: key.Host, Directory: key.Directory, Command: key.Command}), wantRule)
	equal(t, list(t, r, tools.ListSessionsArgs{}).Sessions, []tools.Session{sampleRow()})
	for _, minutes := range []int{1, 15} {
		equal(t, success[tools.AwayLimit](t, r, owner, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: minutes}), tools.AwayLimit{Minutes: minutes})
		g = sampleGroup()
		if minutes == 1 {
			g.AttentionSeconds = 60
		}
		equal(t, totals(t, r, weekArgs()), tools.Totals{Periods: []tools.PeriodTotals{{Start: "2026-10-12", Groups: []tools.GroupTotals{g}}}})
	}
	equal(t, success[tools.PriceRef](t, r, owner, tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "claude-haiku-5-5", Effective: "2026-01-01"}), tools.PriceRef{Model: "claude-haiku-5-5", Effective: "2026-01-01"})
	wantRow = sampleRow()
	wantRow.Unpriced = 50000
	wantRow.Dollars = "10.10"
	equal(t, list(t, r, tools.ListSessionsArgs{}).Sessions, []tools.Session{wantRow})
	p := tools.SetPriceArgs{Model: "claude-haiku-5-5", Effective: "2026-01-01", Input: "0.10", Cached: "0.01", Output: "0.50", Reasoning: "0.50"}
	equal(t, success[tools.Price](t, r, owner, tools.ToolSetPrice, p), tools.Price{Model: p.Model, Effective: p.Effective, Input: "0.1", Cached: "0.01", Output: "0.5", Reasoning: "0.5"})
	equal(t, list(t, r, tools.ListSessionsArgs{}).Sessions, []tools.Session{sampleRow()})
}
func secondSession() tools.PushSession {
	return tools.PushSession{Tool: "codex", ID: "h", Host: "ld02", Directory: "/srv/h", Headless: true, Turns: []tools.PushTurn{{Moment: "2026-10-20T09:00:00Z", Whose: "person"}, {Moment: "2026-10-20T09:05:00Z", Whose: "agent", Model: "gpt-6.1-sol", Input: 1000, Output: 100, Reasoning: 400}, {Moment: "2026-10-20T09:20:00Z", Whose: "person"}, {Moment: "2026-10-20T09:21:00Z", Whose: "agent", Model: "gpt-6.1-sol"}}}
}

// R-IUUJ-57ZJ R-IXAB-WRGX R-INJ4-ULJD
func TestSecondCallerFiltersAndTotals(t *testing.T) {
	r := newRig(t)
	seedSample(t, r)
	push(t, r, other, secondSession())
	h := tools.Session{Tool: "codex", ID: "h", Host: "ld02", Person: "e2", Directory: "/srv/h", Headless: true, First: "2026-10-20T09:00:00.000Z", Last: "2026-10-20T09:21:00.000Z", Kind: "untagged", AttentionSeconds: 0, WallSeconds: 1260, AgentSeconds: 360, Tokens: tools.Tokens{Input: 1000, Cached: 0, Output: 100, Reasoning: 400, Total: 1500}, Unpriced: 0, Dollars: "0.01", Subagents: 0}
	equal(t, list(t, r, tools.ListSessionsArgs{Tool: ptr("codex")}), tools.SessionList{Sessions: []tools.Session{h}})
	cases := []struct {
		q    tools.ListSessionsArgs
		rows []tools.Session
	}{
		{tools.ListSessionsArgs{Person: ptr("e")}, []tools.Session{sampleRow()}}, {tools.ListSessionsArgs{Tool: ptr("claude")}, []tools.Session{sampleRow()}}, {tools.ListSessionsArgs{Host: ptr("ld01")}, []tools.Session{sampleRow()}}, {tools.ListSessionsArgs{Period: ptr("week"), Of: ptr("2026-10-14")}, []tools.Session{sampleRow()}},
		{tools.ListSessionsArgs{Person: ptr("e2")}, []tools.Session{h}}, {tools.ListSessionsArgs{Host: ptr("ld02")}, []tools.Session{h}}, {tools.ListSessionsArgs{Period: ptr("week"), Of: ptr("2026-10-20")}, []tools.Session{h}}, {tools.ListSessionsArgs{Period: ptr("month"), Of: ptr("2026-10-01")}, []tools.Session{h, sampleRow()}}, {tools.ListSessionsArgs{Person: ptr("u2")}, []tools.Session{}},
	}
	for _, c := range cases {
		equal(t, list(t, r, c.q), tools.SessionList{Sessions: c.rows})
	}
	hg := tools.GroupTotals{Group: "codex", WallSeconds: 1260, AgentSeconds: 360, Tokens: h.Tokens, Dollars: "0.01", Sessions: 1}
	equal(t, totals(t, r, tools.TotalSessionsArgs{Period: "week", From: "2026-10-13", To: ptr("2026-10-20"), Group: "tool", Person: ptr("e2")}), tools.Totals{Periods: []tools.PeriodTotals{{Start: "2026-10-12", Groups: []tools.GroupTotals{}}, {Start: "2026-10-19", Groups: []tools.GroupTotals{hg}}}})
	rg := sampleGroup()
	rg.Group = "build"
	hg.Group = "untagged"
	equal(t, totals(t, r, tools.TotalSessionsArgs{Period: "month", From: "2026-10-01", Group: "kind"}), tools.Totals{Periods: []tools.PeriodTotals{{Start: "2026-10-01", Groups: []tools.GroupTotals{rg, hg}}}})
	rg.Group = "/deliver"
	hg.Group = ""
	equal(t, totals(t, r, tools.TotalSessionsArgs{Period: "month", From: "2026-10-01", Group: "command"}), tools.Totals{Periods: []tools.PeriodTotals{{Start: "2026-10-01", Groups: []tools.GroupTotals{hg, rg}}}})
}
