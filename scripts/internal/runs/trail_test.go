package runs_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// R-T0C7-LK7O: consumers call each exported function through its declared type.
// R-T1K3-ZBYD: the start contains exactly the record's four string values.
func TestStartedAttrs(t *testing.T) {
	started := runs.StartedAttrs
	for _, r := range []store.Run{
		{},
		{ID: "run_0123456789abcdef", Script: "scr_fedcba9876543210", SHA: "0123456789abcdef0123456789abcdef01234567", Trigger: "manual", Ref: "private-ref", User: "private-user", RequestID: "private-request", Reason: "private-reason", Truncated: true},
		{ID: "another-run", Script: "another-script", SHA: "another-sha", Trigger: "another-trigger"},
	} {
		got := started(r)
		want := telemetry.Attrs{"run": r.ID, "script": r.Script, "sha": r.SHA, "trigger": r.Trigger}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("StartedAttrs(%+v) = %#v, want %#v", r, got, want)
		}
	}
}

// R-T0C7-LK7O: the duration and result have the declared public types.
// R-T2S0-D3P2: status selects conditional fields and duration clamps at zero.
func TestFinishedAttrs(t *testing.T) {
	finished := runs.FinishedAttrs
	for _, status := range []string{store.StatusRunning, store.StatusExited, store.StatusFailed, store.StatusKilled, store.StatusTimedOut} {
		for _, truncated := range []bool{false, true} {
			for _, duration := range []struct {
				d  time.Duration
				us int64
			}{
				{-1500 * time.Microsecond, 0},
				{-time.Nanosecond, 0},
				{0, 0},
				{time.Nanosecond, 0},
				{1500 * time.Microsecond, 1500},
				{1500999 * time.Nanosecond, 1500},
			} {
				for _, exitCode := range []int{0, 137, 255} {
					r := store.Run{ID: "run_0123456789abcdef", Script: "scr_fedcba9876543210", SHA: "private-sha", Ref: "private-ref", User: "private-user", RequestID: "private-request", Trigger: "manual", Status: status, ExitCode: exitCode, Reason: store.ReasonCommitMissing, Truncated: truncated}
					want := telemetry.Attrs{"duration_us": duration.us, "run": r.ID, "status": status, "truncated": truncated}
					switch status {
					case store.StatusExited:
						want["exit_code"] = int64(exitCode)
					case store.StatusFailed:
						want["reason"] = r.Reason
					}
					if got := finished(r, duration.d); !reflect.DeepEqual(got, want) {
						t.Errorf("FinishedAttrs(%+v, %v) = %#v, want %#v", r, duration.d, got, want)
					}
				}
			}
		}
	}
}

// R-T2S0-D3P2: failed metadata carries the supplied reason, including empty.
func TestFinishedAttrsReasons(t *testing.T) {
	for _, reason := range []string{"", store.ReasonRepositoryMissing, store.ReasonCommitMissing, store.ReasonTooLarge, store.ReasonGitFailed, store.ReasonTimedOut, store.ReasonStartFailed} {
		r := store.Run{ID: "run_0123456789abcdef", Status: store.StatusFailed, Reason: reason}
		want := telemetry.Attrs{"duration_us": int64(1500), "run": r.ID, "status": store.StatusFailed, "truncated": false, "reason": reason}
		if got := runs.FinishedAttrs(r, 1500*time.Microsecond); !reflect.DeepEqual(got, want) {
			t.Errorf("FinishedAttrs reason %q = %#v, want %#v", reason, got, want)
		}
	}
}

// R-T0C7-LK7O: consumers obtain an identity.Caller from a store.Run.
// R-T3ZW-QVFR: only the recorded user and request id are restored.
func TestCaller(t *testing.T) {
	caller := runs.Caller
	for _, r := range []store.Run{{}, {User: "owner", RequestID: "origin-request", Ref: "secret-ref", ID: "run_0123456789abcdef"}} {
		want := identity.Caller{UserID: r.User, RequestID: r.RequestID}
		if got := caller(r); got != want {
			t.Errorf("Caller(%+v) = %+v, want %+v", r, got, want)
		}
	}
}
