// Package trail records campaign changes and visits.
package trail

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// EventMemberAdded names the addition of a member.
const EventMemberAdded = "campaign.member.added"

// EventMemberRejected names the rejection of a member.
const EventMemberRejected = "campaign.member.rejected"

// EventPieceDelivered names the delivery of a piece.
const EventPieceDelivered = "campaign.piece.delivered"

// EventMemberOpened names the first visit of a member's links.
const EventMemberOpened = "campaign.member.opened"

// EventVisited names a recorded visit.
const EventVisited = "campaign.visited"

// Emits returns independent declarations for the four bus events.
func Emits() []events.Emission {
	return []events.Emission{
		{Event: EventMemberAdded, Attrs: []string{"campaign", "candidate", "prospect_list"}},
		{Event: EventMemberRejected, Attrs: []string{"campaign", "candidate", "reason"}},
		{Event: EventPieceDelivered, Attrs: []string{"campaign", "candidate", "piece", "channel", "how"}},
		{Event: EventMemberOpened, Attrs: []string{"campaign", "candidate", "at"}},
	}
}
func both(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, attrs telemetry.Attrs) {
	if w != nil {
		w.Emit(ctx, name, attrs)
	}
	if em != nil {
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}

// MemberAdded queues the trail and bus records for a new member.
func MemberAdded(ctx context.Context, w *telemetry.Writer, em *events.Emitter, campaign, candidate, prospectList string) {
	both(ctx, w, em, EventMemberAdded, telemetry.Attrs{"campaign": campaign, "candidate": candidate, "prospect_list": prospectList})
}

// MemberRejected queues the trail and bus records for a rejection.
func MemberRejected(ctx context.Context, w *telemetry.Writer, em *events.Emitter, campaign, candidate, reason string) {
	both(ctx, w, em, EventMemberRejected, telemetry.Attrs{"campaign": campaign, "candidate": candidate, "reason": reason})
}

// PieceDelivered queues the trail and bus records for a delivery.
func PieceDelivered(ctx context.Context, w *telemetry.Writer, em *events.Emitter, campaign, candidate, piece, channel, how string) {
	both(ctx, w, em, EventPieceDelivered, telemetry.Attrs{"campaign": campaign, "candidate": candidate, "piece": piece, "channel": channel, "how": how})
}

// MemberOpened queues the first-open records with the request id and no user.
func MemberOpened(ctx context.Context, w *telemetry.Writer, em *events.Emitter, campaign, candidate string, at time.Time) {
	c, _ := identity.FromContext(ctx)
	ctx = identity.NewContext(ctx, identity.Caller{RequestID: c.RequestID})
	both(ctx, w, em, EventMemberOpened, telemetry.Attrs{"campaign": campaign, "candidate": candidate, "at": at.UTC().Format(time.RFC3339)})
}

// Visited queues a trail-only visit with its request id and no user.
func Visited(ctx context.Context, w *telemetry.Writer, campaign, piece string) {
	if w == nil {
		return
	}
	c, _ := identity.FromContext(ctx)
	ctx = identity.NewContext(ctx, identity.Caller{RequestID: c.RequestID})
	w.Emit(ctx, EventVisited, telemetry.Attrs{"campaign": campaign, "piece": piece})
}
