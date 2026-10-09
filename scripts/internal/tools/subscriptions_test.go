package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

func TestSubscriptionTypes(t *testing.T) {
	// R-XKD5-M7WL R-XLL1-ZZNA R-7DVM-2RT8
	for _, tt := range []struct {
		value any
		want  string
	}{
		{tools.SubscribeArgs{Name: "nightly-report", Event: "cron.*.fired"}, `{"name":"nightly-report","event":"cron.*.fired"}`},
		{tools.UnsubscribeArgs{Name: "sync-crm", Event: "repo.pushed"}, `{"name":"sync-crm","event":"repo.pushed"}`},
		{tools.Subscription{Event: "repo.pushed", Created: "2025-01-02T02:04:05Z"}, `{"event":"repo.pushed","created":"2025-01-02T02:04:05Z"}`},
	} {
		b, e := json.Marshal(tt.value)
		must(t, e)
		if string(b) != tt.want {
			t.Fatalf("%T %s", tt.value, b)
		}
	}
	assertShape(t, tools.SubscribeArgs{}, struct {
		Name  string `json:"name" mcp:"required" description:"fixture"`
		Event string `json:"event" mcp:"required" description:"fixture"`
	}{}, true)
	assertShape(t, tools.UnsubscribeArgs{}, struct {
		Name  string `json:"name" mcp:"required" description:"fixture"`
		Event string `json:"event" mcp:"required" description:"fixture"`
	}{}, true)
	assertShape(t, tools.Subscription{}, struct {
		Event   string `json:"event" mcp:"required"`
		Created string `json:"created" mcp:"required"`
	}{}, true)
}

func subscriptionTrail(t *testing.T, h *fixture, id, tool, outcome string) {
	t.Helper()
	h.flush()
	count := 0
	for _, ev := range h.capture.Events() {
		if ev.RequestID != id {
			continue
		}
		count++
		if ev.Name != "tool.called" || ev.Attrs["tool"] != tool || ev.Attrs["outcome"] != outcome {
			t.Fatal(ev)
		}
		kind := "additive"
		if tool == "unsubscribe" {
			kind = "destructive"
		}
		if ev.Attrs["kind"] != kind {
			t.Fatal(ev)
		}
	}
	if count != 1 {
		t.Fatal("subscription trail count", count)
	}
}

func TestSubscriptionRulesAndInertRefusals(t *testing.T) {
	// R-YHAF-Y0XC R-YIIC-BSO1 R-8O7S-M1ZM R-T3OP-T4GN R-8FOH-XNSR R-XVC9-25KU
	h, sc, _, conn := paused(t)
	ctx := context.Background()
	foreign, e := h.st.Create(ctx, store.Draft{Owner: "bob", Name: "foreign", Repo: sc.Repo, Ref: "main"})
	must(t, e)
	_, e = h.st.Subscribe(ctx, foreign.ID, "repo.pushed")
	must(t, e)
	i := 0
	check := func(tool, name, event, want string) {
		t.Helper()
		before := snapshot(t, h)
		removeTrace(t, h)
		id := fmt.Sprintf("sub-refusal-%d", i)
		i++
		refusal(t, h.raw(tool, fmt.Sprintf(`{"name":%q,"event":%q}`, name, event), identity.Caller{UserID: "alice", RequestID: id}), want)
		unchanged(t, h, before)
		noGit(t, h)
		stillLive(t, conn)
		subscriptionTrail(t, h, id, tool, "error")
	}
	for _, tool := range []string{"subscribe", "unsubscribe"} {
		check(tool, foreign.Name, "repo.pushed", fmt.Sprintf(tools.MissingScript, foreign.Name))
		for _, name := range []string{"foreign", "absent", sc.ID, "Nightly Report"} {
			check(tool, name, "Repo.Pushed", fmt.Sprintf(tools.MissingScript, name))
		}
		for _, event := range []string{"Repo.Pushed", "pushed", "*", "repo..pushed", "repo.pushed.", "re*po.pushed", "repo.push*", "repo.**", "repo-x.pushed", " repo.pushed", ""} {
			check(tool, sc.Name, event, fmt.Sprintf(tools.InvalidEvent, event))
		}
	}
	check("unsubscribe", sc.Name, "repo.pushed", fmt.Sprintf(tools.NotSubscribed, sc.Name, "repo.pushed"))
	_, e = h.st.Subscribe(ctx, sc.ID, "repo.pushed")
	must(t, e)
	check("unsubscribe", sc.Name, "crm.contact_added", fmt.Sprintf(tools.NotSubscribed, sc.Name, "crm.contact_added"))
	_, e = h.st.Subscribe(ctx, sc.ID, "cron.*.fired")
	must(t, e)
	check("unsubscribe", sc.Name, "cron.hourly.fired", fmt.Sprintf(tools.NotSubscribed, sc.Name, "cron.hourly.fired"))
	_, e = h.st.Unsubscribe(ctx, sc.ID, "cron.*.fired")
	must(t, e)
	_, e = h.st.Subscribe(ctx, sc.ID, "cron.hourly.fired")
	must(t, e)
	check("unsubscribe", sc.Name, "cron.*.fired", fmt.Sprintf(tools.NotSubscribed, sc.Name, "cron.*.fired"))
	for _, name := range []string{"events", "declarations"} {
		refusal(t, h.call("create", tools.CreateArgs{Name: name, Repo: sc.Repo}), fmt.Sprintf(tools.InvalidName, name))
	}
}

func TestSubscriptionMutationsPreserveEventRun(t *testing.T) {
	// R-0TFI-C739 R-0UNE-PYTY R-8O7S-M1ZM R-T3OP-T4GN R-7JZ3-ZMIP R-T179-4S0M R-7MEW-R603
	h, sc, r, conn := pausedCause(t, events.Cause{ID: "evt_1122334455667788", Depth: 2})
	ctx := context.Background()
	other, e := h.st.Create(ctx, store.Draft{Owner: "bob", Name: "another", Repo: sc.Repo, Ref: "main"})
	must(t, e)
	_, e = h.st.Subscribe(ctx, other.ID, "repo.pushed")
	must(t, e)
	must(t, os.RemoveAll(h.repo))
	i := 0
	call := func(tool, event string) tools.Script {
		t.Helper()
		before := snapshot(t, h)
		removeTrace(t, h)
		id := fmt.Sprintf("sub-success-%d", i)
		i++
		reply := h.raw(tool, fmt.Sprintf(`{"name":%q,"event":%q}`, sc.Name, event), identity.Caller{UserID: "alice", RequestID: id})
		current, e := h.st.Find(ctx, "alice", sc.Name)
		must(t, e)
		assertWire(t, reply, expectedScript(t, current, true))
		after := snapshot(t, h)
		for j := range before.Scripts {
			if before.Scripts[j].ID == sc.ID {
				before.Scripts[j].Subscriptions = after.Scripts[j].Subscriptions
			}
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("subscription changed other state: %#v %#v", before, after)
		}
		noGit(t, h)
		stillLive(t, conn)
		subscriptionTrail(t, h, id, tool, "ok")
		assertWire(t, h.call("show", tools.ShowArgs{Name: sc.Name}), expectedScript(t, current, true))
		assertWire(t, h.call("runs", tools.RunsArgs{Name: sc.Name}), `{"runs":[`+expectedEntry(t, r)+`]}`)
		assertWire(t, h.call("result", tools.ResultArgs{Run: r.ID}), expectedResult(t, r, 0, 0, "", "", []tools.File{}, false))
		listed := decode[tools.ScriptList](t, h.call("list", tools.ListArgs{}))
		if len(listed.Scripts) != 1 || listed.Scripts[0].Subscriptions != len(current.Subscriptions) {
			t.Fatal(listed)
		}
		return decode[tools.Script](t, reply)
	}
	first := call("subscribe", "repo.pushed")
	h.now = h.now.Add(time.Hour)
	again := call("subscribe", "repo.pushed")
	if !reflect.DeepEqual(first, again) {
		t.Fatal("repeat changed subscription", first, again)
	}
	both := call("subscribe", "crm.contact_added")
	if len(both.Subscriptions) != 2 || both.Subscriptions[0].Event != "crm.contact_added" || both.Subscriptions[1].Event != "repo.pushed" {
		t.Fatal(both)
	}
	call("unsubscribe", "crm.contact_added")
	empty := call("unsubscribe", "repo.pushed")
	if len(empty.Subscriptions) != 0 {
		t.Fatal(empty)
	}
	// Store's clock is injected independently so a re-subscription's time advances.
	later := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)
	h.now = later
	// The new subscription gets the later clock reading.
	readded := call("subscribe", "repo.pushed")
	if len(readded.Subscriptions) != 1 || readded.Subscriptions[0].Created != later.Format("2006-01-02T15:04:05Z") {
		t.Fatal(readded)
	}
	call("unsubscribe", "repo.pushed")
	pattern := call("subscribe", "cron.*.fired")
	h.now = h.now.Add(time.Hour)
	if duplicate := call("subscribe", "cron.*.fired"); !reflect.DeepEqual(pattern, duplicate) {
		t.Fatal("repeat changed pattern subscription", pattern, duplicate)
	}
	overlap := call("subscribe", "cron.hourly.fired")
	if len(overlap.Subscriptions) != 2 || overlap.Subscriptions[0].Event != "cron.*.fired" || overlap.Subscriptions[1].Event != "cron.hourly.fired" {
		t.Fatal(overlap)
	}
	literal := call("unsubscribe", "cron.*.fired")
	if len(literal.Subscriptions) != 1 || literal.Subscriptions[0] != overlap.Subscriptions[1] {
		t.Fatal("removing pattern changed literal subscription", literal)
	}
	newPattern := call("subscribe", "cron.*.fired")
	if newPattern.Subscriptions[0].Created != h.now.Format("2006-01-02T15:04:05Z") || newPattern.Subscriptions[0].Created == pattern.Subscriptions[0].Created {
		t.Fatal("readded pattern did not use later time", newPattern)
	}
	for _, event := range []string{"repo.git.pushed", "repo.*", "*.*"} {
		got := call("subscribe", event)
		found := false
		for _, sub := range got.Subscriptions {
			found = found || sub.Event == event
		}
		if !found {
			t.Fatal("accepted pattern not stored", event, got)
		}
	}
	ended := h.call("cancel", tools.CancelArgs{Run: r.ID})
	rr, e := h.st.RunByID(ctx, r.ID)
	must(t, e)
	assertWire(t, ended, expectedEntry(t, rr))
	if decode[tools.RunEntry](t, ended).Event == nil {
		t.Fatal("cancel omitted event")
	}
}

func TestEventRunWireAcrossStatuses(t *testing.T) {
	// R-7HJB-831B R-7IR7-LUS0 R-T179-4S0M R-7MEW-R603
	h := setup(t, "print(1)\n")
	sc := h.create("event-records")
	ctx := context.Background()
	records := []store.Run{}
	for i, status := range []string{store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed} {
		tm := time.Date(2025, 1, 2, 3, 4, 5+i, 0, time.UTC)
		r := store.Run{ID: fmt.Sprintf("run_%016x", i+1), Script: sc.ID, Ref: "main", User: "alice", RequestID: "delivered-request", Trigger: store.TriggerEvent, Event: fmt.Sprintf("evt_%016x", i+1), Status: store.StatusRunning, SHA: "cccccccccccccccccccccccccccccccccccccccc", Started: tm}
		if status == store.StatusFailed {
			r.Status = status
			r.SHA = ""
			r.Reason = store.ReasonCommitMissing
			r.Finished = tm
		}
		r, e := h.st.AddRun(ctx, r)
		must(t, e)
		if status != store.StatusRunning && status != store.StatusFailed {
			code := 0
			if status == store.StatusExited {
				code = 5
			}
			r, e = h.st.FinishRun(ctx, r.ID, store.Ending{Status: status, Finished: tm.Add(time.Second), ExitCode: code, StdoutBytes: 3, StderrBytes: 2, Truncated: true})
			must(t, e)
		}
		records = append([]store.Run{r}, records...)
		entries := []string{}
		for _, record := range records {
			entries = append(entries, expectedEntry(t, record))
		}
		assertWire(t, h.call("runs", tools.RunsArgs{Name: sc.Name}), `{"runs":[`+strings.Join(entries, ",")+`]}`)
		assertWire(t, h.call("result", tools.ResultArgs{Run: r.ID}), expectedResult(t, r, r.StdoutBytes, r.StderrBytes, "", "", nil, true))
		got := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: r.ID}))
		if got.Event == nil || *got.Event != r.Event || got.Trigger != store.TriggerEvent || got.Script != sc.ID || got.RequestID != r.RequestID || got.User != r.User {
			t.Fatal(got)
		}
	}
}
