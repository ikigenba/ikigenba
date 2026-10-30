package passes

import (
	"context"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// DeliverCampaign records campaign history without starting background work.
func (e *Engine) DeliverCampaign(ctx context.Context, d events.Delivery) events.Outcome {
	if e.stopping.Load() {
		return events.Fail(Stopping)
	}
	campaign, _ := d.Event.Attrs["campaign"].(string)
	candidate, _ := d.Event.Attrs["candidate"].(string)
	if campaign == "" || candidate == "" || d.Event.Time.IsZero() {
		return events.Skip()
	}
	var err error
	switch d.Event.Name {
	case trail.AcceptedAdded:
		list, ok := d.Event.Attrs["prospect_list"].(string)
		if !ok {
			legacy, _ := d.Event.Attrs["cohort"].(string)
			list = store.MapListID(legacy)
		}
		err = e.cfg.Store.AddCampaignUse(ctx, candidate, campaign, list, d.Event.Time)
	case trail.AcceptedRejected:
		reason, _ := d.Event.Attrs["reason"].(string)
		err = e.cfg.Store.RejectCampaignUse(ctx, candidate, campaign, reason, d.Event.Time)
	case trail.AcceptedDelivered:
		err = e.cfg.Store.DeliverCampaignUse(ctx, candidate, campaign, d.Event.Time)
	case trail.AcceptedOpened:
		opened := d.Event.Time
		if at, ok := d.Event.Attrs["at"].(string); ok {
			if parsed, parseErr := time.Parse(time.RFC3339, at); parseErr == nil {
				opened = parsed
			}
		}
		if opened.IsZero() {
			return events.Skip()
		}
		err = e.cfg.Store.MarkCampaignUseOpened(ctx, candidate, campaign, opened)
	default:
		return events.Skip()
	}
	if errors.Is(err, store.ErrNotFound) {
		return events.Skip()
	}
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	return events.OK()
}
