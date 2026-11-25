// Package trail records the outcomes of accounts changes.
package trail

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Event names.
const (
	EventChangeFinished      = "change.finished"
	EventOperationStarted    = "operation.started"
	EventOperationFinished   = "operation.finished"
	EventHostChanged         = "host.changed"
	EventAccountChanged      = "account.changed"
	EventHostSilent          = "host.silent"
	EventHostSynced          = "host.synced"
	EventHostApplyFailed     = "host.apply_failed"
	EventResourceUnexplained = "resource.unexplained"
	EventInviteCreated       = "invite.created"
	EventInviteRevoked       = "invite.revoked"
	EventInviteUsed          = "invite.used"
	EventAdminChanged        = "admin.changed"
)

// Change results, account removal and resource kinds.
const (
	ResultOK         = "ok"
	ResultRefused    = "refused"
	ResultFailed     = "failed"
	AccountRemoved   = "removed"
	ResourceInstance = "instance"
	ResourceVolume   = "volume"
	ResourceAddress  = "address"
)

// Change queues a changing tool's outcome.
func Change(ctx context.Context, w *telemetry.Writer, tool, host, account, user, ip, result string) {
	w.Emit(ctx, EventChangeFinished, telemetry.Attrs{
		"tool": tool, "host": host, "account": account, "user": user, "ip": ip, "result": result,
	})
}

// Result classifies the outcome against the tool's refusal errors.
func Result(err error, refusals ...error) string {
	if err == nil {
		return ResultOK
	}
	for _, refusal := range refusals {
		if errors.Is(err, refusal) {
			return ResultRefused
		}
	}
	return ResultFailed
}

// OperationStarted queues a newly recorded operation.
func OperationStarted(ctx context.Context, w *telemetry.Writer, operation, kind, host, account string) {
	w.Emit(ctx, EventOperationStarted, telemetry.Attrs{
		"operation": operation, "kind": kind, "host": host, "account": account,
	})
}

// OperationFinished queues an ended operation.
func OperationFinished(ctx context.Context, w *telemetry.Writer, operation, kind, host, account, status, reason string) {
	w.Emit(ctx, EventOperationFinished, telemetry.Attrs{
		"operation": operation, "kind": kind, "host": host, "account": account, "status": status, "reason": reason,
	})
}

// HostChanged queues a host state transition.
func HostChanged(ctx context.Context, w *telemetry.Writer, host, from, to, operation string) {
	w.Emit(ctx, EventHostChanged, telemetry.Attrs{"host": host, "from": from, "to": to, "operation": operation})
}

// AccountChanged queues an account state transition or removal.
func AccountChanged(ctx context.Context, w *telemetry.Writer, account, host, from, to string) {
	w.Emit(ctx, EventAccountChanged, telemetry.Attrs{"account": account, "host": host, "from": from, "to": to})
}

// HostSilent queues a missed report.
func HostSilent(ctx context.Context, w *telemetry.Writer, host string, applied int64) {
	w.Emit(ctx, EventHostSilent, telemetry.Attrs{"host": host, "applied": applied})
}

// HostSynced queues a host's return to sync.
func HostSynced(ctx context.Context, w *telemetry.Writer, host string, applied int64) {
	w.Emit(ctx, EventHostSynced, telemetry.Attrs{"host": host, "applied": applied})
}

// HostApplyFailed queues a failed attempt to apply the desired state.
func HostApplyFailed(ctx context.Context, w *telemetry.Writer, host string, attempted int64) {
	w.Emit(ctx, EventHostApplyFailed, telemetry.Attrs{"host": host, "attempted": attempted})
}

// Unexplained queues the first sighting of an unclaimed resource.
func Unexplained(ctx context.Context, w *telemetry.Writer, resource, kind, host string) {
	w.Emit(ctx, EventResourceUnexplained, telemetry.Attrs{"resource": resource, "kind": kind, "host": host})
}

// InviteCreated queues a new enrollment invitation.
func InviteCreated(ctx context.Context, w *telemetry.Writer, invite, account, host string, admin bool) {
	w.Emit(ctx, EventInviteCreated, telemetry.Attrs{"invite": invite, "account": account, "host": host, "admin": admin})
}

// InviteRevoked queues the revocation of an invitation.
func InviteRevoked(ctx context.Context, w *telemetry.Writer, invite, account, host string) {
	w.Emit(ctx, EventInviteRevoked, telemetry.Attrs{"invite": invite, "account": account, "host": host})
}

// InviteUsed queues a completed enrollment.
func InviteUsed(ctx context.Context, w *telemetry.Writer, invite, account, host, user string, admin bool) {
	w.Emit(ctx, EventInviteUsed, telemetry.Attrs{"invite": invite, "account": account, "host": host, "user": user, "admin": admin})
}

// AdminChanged queues a change to a user's administrator flag.
func AdminChanged(ctx context.Context, w *telemetry.Writer, account, host, user string, admin bool) {
	w.Emit(ctx, EventAdminChanged, telemetry.Attrs{"account": account, "host": host, "user": user, "admin": admin})
}
