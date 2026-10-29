// Package trail records the outcomes of host changes.
package trail

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// EventChangeFinished names the event for an attempted change.
const EventChangeFinished = "change.finished"

// Change result values.
const (
	ResultOK      = "ok"
	ResultRefused = "refused"
	ResultFailed  = "failed"
)

// Change queues one change outcome under the caller on ctx.
func Change(ctx context.Context, w *telemetry.Writer, tool, group, user, result string) {
	w.Emit(ctx, EventChangeFinished, telemetry.Attrs{"tool": tool, "group": group, "user": user, "result": result})
}

// Result classifies the host's answer.
func Result(err error) string {
	if err == nil {
		return ResultOK
	}
	var refused *host.RefusedError
	if errors.As(err, &refused) {
		return ResultRefused
	}
	return ResultFailed
}
