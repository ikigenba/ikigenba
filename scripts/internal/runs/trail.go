package runs

import (
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// StartedAttrs forms the metadata of a run's start from its record.
func StartedAttrs(r store.Run) telemetry.Attrs {
	return telemetry.Attrs{"run": r.ID, "script": r.Script, "sha": r.SHA, "trigger": r.Trigger}
}

// FinishedAttrs forms the metadata of a run's end with its measured duration.
func FinishedAttrs(r store.Run, d time.Duration) telemetry.Attrs {
	if d < 0 {
		d = 0
	}
	attrs := telemetry.Attrs{"duration_us": d.Microseconds(), "run": r.ID, "status": r.Status, "truncated": r.Truncated}
	switch r.Status {
	case store.StatusExited:
		attrs["exit_code"] = int64(r.ExitCode)
	case store.StatusFailed:
		attrs["reason"] = r.Reason
	}
	return attrs
}

// Caller restores the identity of the request that caused a run.
func Caller(r store.Run) identity.Caller {
	return identity.Caller{UserID: r.User, RequestID: r.RequestID}
}
