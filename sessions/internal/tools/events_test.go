package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
)

func change(t *testing.T, r *rig, name string, args any, want telemetry.Attrs) {
	t.Helper()
	before := len(flush(t, r))
	v := call(t, r, owner, name, args)
	if (want["result"] != trail.ResultOK) != v.IsError() {
		t.Fatalf("unexpected result %s", raw(t, v))
	}
	events := flush(t, r)
	equal(t, len(events), before+1)
	e := events[len(events)-1]
	equal(t, e.Name, trail.EventChangeFinished)
	equal(t, e.User, owner.UserID)
	equal(t, e.RequestID, owner.RequestID)
	equal(t, e.Attrs, want)
}

// R-ISEQ-DOI5
func TestExactlyOneChangeEventPerChangingCall(t *testing.T) {
	r := newRig(t)
	x := entry("a", now)
	x.Turns = append(x.Turns, tools.PushTurn{Moment: "2026-11-01T12:00:01Z", Whose: "agent"}, tools.PushTurn{Moment: "2026-11-01T12:00:02Z", Whose: "tool"})
	y := x
	y.ID = "b"
	y.Turns = append([]tools.PushTurn{}, x.Turns...)
	a := tools.PushTurnsArgs{Sessions: []tools.PushSession{x, y}}
	attrs := telemetry.Attrs{"tool": tools.ToolPushTurns, "sessions": int64(2), "turns_new": int64(6), "turns_changed": int64(0), "result": trail.ResultOK}
	change(t, r, tools.ToolPushTurns, a, attrs)
	attrs["turns_new"] = int64(0)
	change(t, r, tools.ToolPushTurns, a, attrs)
	a.Sessions[0].Turns[1].Output = 5
	attrs["turns_changed"] = int64(1)
	change(t, r, tools.ToolPushTurns, a, attrs)
	change(t, r, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{}}, telemetry.Attrs{"tool": tools.ToolPushTurns, "sessions": int64(0), "turns_new": int64(0), "turns_changed": int64(0), "result": trail.ResultRefused})
	change(t, r, tools.ToolAddRule, tools.AddRuleArgs{Host: "test-host", Directory: "/work", Kind: "build"}, telemetry.Attrs{"tool": tools.ToolAddRule, "host": "test-host", "directory": "/work", "command": "", "kind": "build", "roots": int64(2), "result": trail.ResultOK})
	change(t, r, tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz"}, telemetry.Attrs{"tool": tools.ToolRemoveRule, "host": "ld01", "directory": "/mnt/projects/biz", "command": "", "kind": "plan", "roots": int64(2), "result": trail.ResultOK})
	change(t, r, tools.ToolSetPrice, tools.SetPriceArgs{Model: "model", Effective: "2026-01-01", Input: "1", Cached: "2", Output: "3", Reasoning: "4"}, telemetry.Attrs{"tool": tools.ToolSetPrice, "model": "model", "effective": "2026-01-01", "roots": int64(2), "result": trail.ResultOK})
	change(t, r, tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "model", Effective: "2026-01-01"}, telemetry.Attrs{"tool": tools.ToolRemovePrice, "model": "model", "effective": "2026-01-01", "roots": int64(2), "result": trail.ResultOK})
	change(t, r, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}, telemetry.Attrs{"tool": tools.ToolSetAwayLimit, "minutes": int64(30), "roots": int64(2), "result": trail.ResultOK})
	r.d.SetFailing(true)
	for _, tc := range validCalls() {
		var attrs telemetry.Attrs
		switch tc.name {
		case tools.ToolPushTurns:
			attrs = telemetry.Attrs{"sessions": int64(0), "turns_new": int64(0), "turns_changed": int64(0)}
		case tools.ToolAddRule:
			attrs = telemetry.Attrs{"host": "host", "directory": "/work", "command": "", "kind": "build", "roots": int64(0)}
		case tools.ToolRemoveRule:
			attrs = telemetry.Attrs{"host": "ld01", "directory": "/mnt/projects/biz", "command": "", "kind": "", "roots": int64(0)}
		case tools.ToolSetPrice:
			attrs = telemetry.Attrs{"model": "model", "effective": "2026-01-01", "roots": int64(0)}
		case tools.ToolRemovePrice:
			attrs = telemetry.Attrs{"model": "claude-opus-5-5", "effective": "2026-01-01", "roots": int64(0)}
		case tools.ToolSetAwayLimit:
			attrs = telemetry.Attrs{"minutes": int64(30), "roots": int64(0)}
		default:
			continue
		}
		attrs["tool"] = tc.name
		attrs["result"] = trail.ResultFailed
		change(t, r, tc.name, tc.args, attrs)
	}
	r.d.SetFailing(false)
	change(t, r, tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "missing", Directory: "/missing"}, telemetry.Attrs{"tool": tools.ToolRemoveRule, "host": "missing", "directory": "/missing", "command": "", "kind": "", "roots": int64(0), "result": trail.ResultRefused})
}

// R-ITMM-RG8U R-GN9X-2UU6
func TestReadsAndSchemaRefusalsNeverEmitChanges(t *testing.T) {
	r := newRig(t)
	for _, failing := range []bool{false, true} {
		r.d.SetFailing(failing)
		for _, tc := range validCalls() {
			switch tc.name {
			case tools.ToolGetLatestTurns, tools.ToolListSessions, tools.ToolTotalSessions, tools.ToolListRules, tools.ToolListPrices, tools.ToolGetAwayLimit:
				_ = call(t, r, owner, tc.name, tc.args)
			}
		}
		refused(t, r, tools.ToolListSessions, tools.ListSessionsArgs{Page: ptr("bad-page")}, paging.BadPage)
	}
	r.d.SetFailing(false)
	required := map[string]string{tools.ToolPushTurns: "sessions", tools.ToolGetLatestTurns: "sessions", tools.ToolTotalSessions: "period", tools.ToolAddRule: "host", tools.ToolRemoveRule: "directory", tools.ToolSetPrice: "model", tools.ToolRemovePrice: "effective", tools.ToolSetAwayLimit: "minutes"}
	for _, tc := range validCalls() {
		var m map[string]any
		if err := json.Unmarshal(raw(t, tc.args), &m); err != nil {
			t.Fatal(err)
		}
		m["unknown_member"] = 1
		if v := call(t, r, owner, tc.name, m); !v.IsError() {
			t.Fatalf("%s accepted unknown member", tc.name)
		}
		delete(m, "unknown_member")
		if key := required[tc.name]; key != "" {
			delete(m, key)
			if v := call(t, r, owner, tc.name, m); !v.IsError() {
				t.Fatalf("%s accepted omitted %s", tc.name, key)
			}
			m[key] = false
			if v := call(t, r, owner, tc.name, m); !v.IsError() {
				t.Fatalf("%s accepted wrong type %s", tc.name, key)
			}
		} else {
			if v := call(t, r, owner, tc.name, []int{}); !v.IsError() {
				t.Fatalf("%s accepted array arguments", tc.name)
			}
		}
	}
	equal(t, len(flush(t, r)), 0)
	// Store refusals reached through reads also leave the trail empty.
	refused(t, r, tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{}}, store.NoSessions)
	q := weekArgs()
	q.Group = "day"
	refused(t, r, tools.ToolTotalSessions, q, store.UnknownGroup)
	equal(t, len(flush(t, r)), 0)
}
