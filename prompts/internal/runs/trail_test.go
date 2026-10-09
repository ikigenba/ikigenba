package runs_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// R-KQEW-63TJ R-KRMS-JVK8 R-KVAH-P6SB
func TestTrailHelperAPI(t *testing.T) {
	api := struct {
		started  func(store.Run) telemetry.Attrs
		finished func(store.Run, time.Duration) telemetry.Attrs
		caller   func(store.Run) identity.Caller
	}{runs.StartedAttrs, runs.FinishedAttrs, runs.Caller}
	r := store.Run{ID: "prr_0123456789abcdef", Prompt: "prm_fedcba9876543210", Model: "supplied-model", Trigger: store.TriggerEvent, User: "supplied-user", RequestID: "supplied-request"}
	want := telemetry.Attrs{"prompt_run": r.ID, "prompt": r.Prompt, "model": r.Model, "trigger": r.Trigger}
	if !reflect.DeepEqual(api.started(r), want) {
		t.Fatal(api.started(r))
	}
	if api.finished(r, 0) == nil {
		t.Fatal("nil finished attributes")
	}
	if got := api.caller(r); got != (identity.Caller{UserID: r.User, RequestID: r.RequestID}) {
		t.Fatal(got)
	}
}

// R-KSUO-XNAX R-KU2L-BF1M
func TestTrailFinishedAttributes(t *testing.T) {
	for _, status := range []string{store.StatusExited, store.StatusFailed, store.StatusKilled, store.StatusTimedOut} {
		for _, d := range []time.Duration{-1500 * time.Microsecond, 0, 1500*time.Microsecond + 999*time.Nanosecond} {
			for _, out := range []bool{false, true} {
				for _, err := range []bool{false, true} {
					r := store.Run{ID: "prr_0123456789abcdef", Prompt: "prm_fedcba9876543210", Model: "supplied-model", Status: status, ExitCode: 1, Reason: store.ReasonStartFailed, StdoutTruncated: out, StderrTruncated: err, Usage: store.Usage{Calls: 2, ToolCalls: 1, InputTokens: 300, CachedTokens: 50, OutputTokens: 40, ReasoningTokens: 7, CostNanos: 9000}}
					us := d.Microseconds()
					if us < 0 {
						us = 0
					}
					want := telemetry.Attrs{"prompt_run": r.ID, "status": status, "duration_us": us, "truncated": out || err, "calls": int64(2), "tool_calls": int64(1), "input_tokens": int64(300), "output_tokens": int64(40), "cost_nanos": int64(9000)}
					if status == store.StatusExited {
						want["exit_code"] = int64(1)
					}
					if status == store.StatusFailed {
						want["reason"] = r.Reason
					}
					if got := runs.FinishedAttrs(r, d); !reflect.DeepEqual(got, want) {
						t.Fatalf("%s %v: %#v want %#v", status, d, got, want)
					}
				}
			}
		}
	}
	r := store.Run{ID: "prr_0123456789abcdef", Status: store.StatusExited}
	want := telemetry.Attrs{"prompt_run": r.ID, "status": r.Status, "duration_us": int64(1500), "exit_code": int64(0), "truncated": false, "calls": int64(0), "tool_calls": int64(0), "input_tokens": int64(0), "output_tokens": int64(0), "cost_nanos": int64(0)}
	if got := runs.FinishedAttrs(r, 1500*time.Microsecond); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
