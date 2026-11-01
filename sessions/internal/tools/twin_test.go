package tools_test

import (
	"context"
	"testing"

	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

func compareStores(t *testing.T, a, b *store.Store) {
	t.Helper()
	ctx := context.Background()
	ar, err := a.Sessions(ctx, store.Filter{}, store.Position{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	br, err := b.Sessions(ctx, store.Filter{}, store.Position{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, ar, br)
	refs := []store.SessionRef{{Tool: "claude", ID: "root"}, {Tool: "codex", ID: "child"}, {Tool: "claude", ID: "valid"}, {Tool: "claude", ID: "missing"}}
	al, err := a.Latest(ctx, refs)
	if err != nil {
		t.Fatal(err)
	}
	bl, err := b.Latest(ctx, refs)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, al, bl)
	as, err := a.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := b.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, as, bs)
	ap, err := a.Prices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bp, err := b.Prices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, ap, bp)
	ad, err := a.AwayLimit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bd, err := b.AwayLimit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, ad, bd)
	q := store.TotalsQuery{Period: "month", From: "2026-10-01", To: "2026-11-01", Group: "kind"}
	at, err := a.Totals(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	bt, err := b.Totals(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, at, bt)
}

// R-IEZU-67CI R-HBNW-Q9O2 R-HV6A-ULJ6 R-HHRE-N4DJ
func TestToolsHaveTheirStoreCallEffects(t *testing.T) {
	r := newRig(t)
	_, twin := database(t)
	ctx := context.Background()
	// The twin receives explicit wire-to-store mappings, independent of tools.
	x := entry("root", now)
	x.Host = " host "
	x.Directory = "/work/path space"
	x.Command = &tools.Command{Name: " /cmd ", Argument: "raw\nargument"}
	x.Headless = true
	x.Turns[0].Whose = "agent"
	x.Turns[0].Model = "unknown model"
	x.Turns[0].Input = 1
	x.Turns[0].Cached = 2
	x.Turns[0].Output = 3
	x.Turns[0].Reasoning = 4
	child := entry("child", now)
	child.Tool = "codex"
	child.Parent = &tools.SessionRef{Tool: "claude", ID: "root"}
	_, err := twin.Push(ctx, owner.UserID, owner.Email, []store.PushSession{{Tool: "claude", ID: "root", Host: " host ", Directory: "/work/path space", Headless: true, Command: store.Command{Name: " /cmd ", Argument: "raw\nargument"}, Turns: []store.PushTurn{{Moment: x.Turns[0].Moment, Whose: "agent", Model: "unknown model", Input: 1, Cached: 2, Output: 3, Reasoning: 4}}}, {Tool: "codex", ID: "child", Parent: &store.SessionRef{Tool: "claude", ID: "root"}, Host: "test-host", Directory: "/work", Turns: []store.PushTurn{{Moment: child.Turns[0].Moment, Whose: "person"}}}})
	if err != nil {
		t.Fatal(err)
	}
	push(t, r, owner, x, child)
	compareStores(t, r.s, twin)
	_, _, err = twin.AddRule(ctx, " host ", "/work/path space", " /cmd ", "publish")
	if err != nil {
		t.Fatal(err)
	}
	_ = success[tools.Rule](t, r, owner, tools.ToolAddRule, tools.AddRuleArgs{Host: " host ", Directory: "/work/path space", Command: ptr(" /cmd "), Kind: "publish"})
	compareStores(t, r.s, twin)
	_, _, err = twin.SetPrice(ctx, store.PriceInput{Model: "unknown model", Effective: "2026-01-01", Input: "5.500", Cached: "0.50", Output: "25", Reasoning: "025"})
	if err != nil {
		t.Fatal(err)
	}
	_ = success[tools.Price](t, r, owner, tools.ToolSetPrice, tools.SetPriceArgs{Model: "unknown model", Effective: "2026-01-01", Input: "5.500", Cached: "0.50", Output: "25", Reasoning: "025"})
	compareStores(t, r.s, twin)
	_, err = twin.SetAwayLimit(ctx, 1440)
	if err != nil {
		t.Fatal(err)
	}
	_ = success[tools.AwayLimit](t, r, owner, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 1440})
	compareStores(t, r.s, twin)
	// Every read leaves the complete observable state unchanged, including the
	// configuration and figures, and filters carry raw strings without trimming.
	for _, tc := range validCalls() {
		switch tc.name {
		case tools.ToolGetLatestTurns, tools.ToolListSessions, tools.ToolTotalSessions, tools.ToolListRules, tools.ToolListPrices, tools.ToolGetAwayLimit:
			if v := call(t, r, other, tc.name, tc.args); v.IsError() {
				t.Fatalf("read failed %s", raw(t, v))
			}
			compareStores(t, r.s, twin)
		}
	}
	equal(t, len(list(t, r, tools.ListSessionsArgs{Host: ptr(" host "), Person: ptr("e"), Command: ptr(" /cmd "), Kind: ptr("publish"), Tool: ptr("claude")}).Sessions), 1)
	equal(t, len(totals(t, r, tools.TotalSessionsArgs{Period: "month", From: "2026-11-01", Group: "command", Host: ptr(" host "), Person: ptr("e"), Command: ptr(" /cmd "), Kind: ptr("publish"), Tool: ptr("claude")}).Periods[0].Groups), 1)
	compareStores(t, r.s, twin)
	refused(t, r, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{}}, store.NoSessions)
	compareStores(t, r.s, twin)
	_, _, err = twin.RemoveRule(ctx, " host ", "/work/path space", " /cmd ")
	if err != nil {
		t.Fatal(err)
	}
	_ = success[tools.Rule](t, r, owner, tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: " host ", Directory: "/work/path space", Command: ptr(" /cmd ")})
	compareStores(t, r.s, twin)
	_, err = twin.RemovePrice(ctx, "unknown model", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	_ = success[tools.PriceRef](t, r, owner, tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "unknown model", Effective: "2026-01-01"})
	compareStores(t, r.s, twin)
}
