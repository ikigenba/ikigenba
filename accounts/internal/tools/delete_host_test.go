package tools_test

import (
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-1397-PM70 R-14H4-3DXP R-1BSI-E0DV R-0R27-VWS2
func TestDeleteHostChoice(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	assertChoice := func(host, state, choice string) {
		t.Helper()
		if detail := readHost(t, f, host); detail.State != state || detail.OnDelete != choice {
			t.Fatalf("host detail %+v, want state %q and choice %q", detail, state, choice)
		}
		list := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{}))
		for _, row := range list.Hosts {
			if row.Host == host && row.State == state && row.OnDelete == choice {
				return
			}
		}
		t.Fatalf("hosts %+v, want %s in %s with choice %q", list, host, state, choice)
	}
	for _, tc := range []struct {
		name   string
		choice string
		add    func(map[string]any)
	}{
		{"omitted", store.OnDeleteKeep, func(map[string]any) {}},
		{"null", store.OnDeleteKeep, func(args map[string]any) { args["keep_data"] = nil }},
		{"true", store.OnDeleteKeep, func(args map[string]any) { args["keep_data"] = true }},
		{"false", store.OnDeleteDelete, func(args map[string]any) { args["keep_data"] = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, _ := f.seedHost(true)
			assertChoice(host.Name, store.HostRunning, "")
			args := map[string]any{"host": host.Name}
			tc.add(args)
			before := len(toolEvents(t, f))
			operation := answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, args))
			assertDeleteHostChange(t, f, operation, host.Name, tc.choice)
			assertChoice(host.Name, store.HostDeleting, tc.choice)
			assertHostDeleteEvents(t, toolEvents(t, f)[before:], f, tools.ToolDeleteHost, store.HostRunning, operation)
			if tc.choice == store.OnDeleteDelete {
				f.fail(operation.Operation, "provider failed")
				assertChoice(host.Name, store.HostFailed, store.OnDeleteDelete)
				before = len(toolEvents(t, f))
				retry := answer[tools.HostChange](t, f.call(tools.ToolRetryHost, tools.HostArgs{Host: host.Name}))
				if retry != operation {
					t.Fatalf("retry %+v, want original delete %+v", retry, operation)
				}
				assertHostDeleteEvents(t, toolEvents(t, f)[before:], f, tools.ToolRetryHost, store.HostFailed, retry)
				f.fail(retry.Operation, "provider failed again")
				keep := true
				replacement := answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: host.Name, KeepData: &keep}))
				if replacement.Operation == operation.Operation {
					t.Fatalf("new delete reused failed operation %+v", replacement)
				}
				assertDeleteHostChange(t, f, replacement, host.Name, store.OnDeleteKeep)
			}
		})
	}

	protected, _ := f.seedHost(true)
	account, op := f.seedAccount("acme", protected.Name)
	f.finish(op.ID, store.Update{})
	before := f.host(protected.Name)
	wakes := f.wakes
	refusal(t, f.call(tools.ToolDeleteHost, map[string]any{"host": protected.Name, "keep_data": false}), tools.HostHasAccounts)
	if after := f.host(protected.Name); !reflect.DeepEqual(after, before) || f.wakes != wakes {
		t.Fatalf("refusal changed host %+v to %+v or woke engine", before, after)
	}
	if got := readAccount(t, f, account.Slug); got.State != store.AccountActive || got.Host != protected.Name {
		t.Fatal(got)
	}

	removed, _ := f.seedHost(true)
	f.finishKeeping(f.host(removed.Name))
	refusal(t, f.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: removed.Name}), tools.NoHost)
}

func assertDeleteHostChange(t *testing.T, f *fixture, got tools.HostChange, host, choice string) {
	t.Helper()
	if got.Host != host || got.State != store.HostDeleting || got.OnDelete != choice || got.Operation == "" {
		t.Fatalf("delete answer %+v, want %s deleting with choice %q", got, host, choice)
	}
	stored := f.host(host)
	if stored.State != got.State || stored.OnDelete != got.OnDelete || stored.Operation != got.Operation {
		t.Fatalf("delete answer differs from store: %+v, host %+v", got, stored)
	}
	op := f.operation(got.Operation)
	if op.Kind != store.OpDelete || op.Status != store.OpRunning || op.Caller.UserID != f.caller.UserID {
		t.Fatalf("delete operation %+v", op)
	}
}

func assertHostDeleteEvents(t *testing.T, events []telemetry.Event, f *fixture, tool, before string, operation tools.HostChange) {
	t.Helper()
	want := []struct {
		name  string
		attrs telemetry.Attrs
	}{
		{trail.EventHostChanged, telemetry.Attrs{"host": operation.Host, "from": before, "to": store.HostDeleting, "operation": operation.Operation}},
		{trail.EventOperationStarted, telemetry.Attrs{"operation": operation.Operation, "kind": store.OpDelete, "host": operation.Host, "account": "", "data": operation.OnDelete}},
		{trail.EventChangeFinished, telemetry.Attrs{"tool": tool, "host": operation.Host, "account": "", "user": "", "ip": "", "result": trail.ResultOK}},
	}
	if len(events) != len(want) {
		t.Fatalf("events %+v, want %+v", events, want)
	}
	for i, event := range events {
		expected := want[i]
		if event.Name != expected.name || !reflect.DeepEqual(event.Attrs, expected.attrs) || event.User != f.caller.UserID {
			t.Fatalf("event %d = %+v, want %+v", i, event, expected)
		}
	}
}
