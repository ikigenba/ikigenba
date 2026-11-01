// Package trail records sessions' changes and startup refresh.
package trail

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
)

// Event names and results are shared by the trail recorders and their callers.
const (
	EventChangeFinished  = "change.finished"
	EventRefreshFinished = "refresh.finished"
	ResultOK             = "ok"
	ResultRefused        = "refused"
	ResultFailed         = "failed"
)

// PushTurns records the counts answered by a push.
func PushTurns(ctx context.Context, w *telemetry.Writer, sessions, turnsNew, turnsChanged int, result string) {
	w.Emit(ctx, EventChangeFinished, telemetry.Attrs{"tool": "push_turns", "sessions": sessions, "turns_new": turnsNew, "turns_changed": turnsChanged, "result": result})
}

// AddRule records a rule addition and its refresh count.
func AddRule(ctx context.Context, w *telemetry.Writer, host, directory, command, kind string, roots int, result string) {
	rule(ctx, w, "add_rule", host, directory, command, kind, roots, result)
}

// RemoveRule records a rule removal and its refresh count.
func RemoveRule(ctx context.Context, w *telemetry.Writer, host, directory, command, kind string, roots int, result string) {
	rule(ctx, w, "remove_rule", host, directory, command, kind, roots, result)
}

func rule(ctx context.Context, w *telemetry.Writer, tool, host, directory, command, kind string, roots int, result string) {
	w.Emit(ctx, EventChangeFinished, telemetry.Attrs{"tool": tool, "host": host, "directory": directory, "command": command, "kind": kind, "roots": roots, "result": result})
}

// SetPrice records a price change and its refresh count.
func SetPrice(ctx context.Context, w *telemetry.Writer, model, effective string, roots int, result string) {
	price(ctx, w, "set_price", model, effective, roots, result)
}

// RemovePrice records a price removal and its refresh count.
func RemovePrice(ctx context.Context, w *telemetry.Writer, model, effective string, roots int, result string) {
	price(ctx, w, "remove_price", model, effective, roots, result)
}

func price(ctx context.Context, w *telemetry.Writer, tool, model, effective string, roots int, result string) {
	w.Emit(ctx, EventChangeFinished, telemetry.Attrs{"tool": tool, "model": model, "effective": effective, "roots": roots, "result": result})
}

// SetAwayLimit records the requested limit and its refresh count.
func SetAwayLimit(ctx context.Context, w *telemetry.Writer, minutes, roots int, result string) {
	w.Emit(ctx, EventChangeFinished, telemetry.Attrs{"tool": "set_away_limit", "minutes": minutes, "roots": roots, "result": result})
}

// Refresh records the refresh completed at startup.
func Refresh(ctx context.Context, w *telemetry.Writer, roots int, result string) {
	w.Emit(ctx, EventRefreshFinished, telemetry.Attrs{"roots": roots, "result": result})
}

// Result classifies a store outcome for the trail.
func Result(err error) string {
	if err == nil {
		return ResultOK
	}
	var refused *store.RefusedError
	if errors.As(err, &refused) {
		return ResultRefused
	}
	return ResultFailed
}
