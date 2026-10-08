// Package tools registers cron's trigger tools.
package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
)

// Config supplies the common store and scheduler.
type Config struct {
	Store     *store.Store
	Scheduler *scheduler.Scheduler
}

// ListArgs has no arguments.
type ListArgs struct{}

// ShowArgs names a trigger.
type ShowArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The trigger's slug."`
}

// CreateArgs supplies a new trigger.
type CreateArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The new trigger's slug: a lowercase letter, then lowercase letters and digits with single underscores between them, 1 to 64 characters, not already a trigger's slug in the space."`
	When string `json:"when" mcp:"required" description:"The schedule, read in UTC: five cron fields, or @hourly, @daily, @weekly, @monthly, or @yearly."`
}

// UpdateArgs changes a schedule.
type UpdateArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The trigger's slug."`
	When string `json:"when" mcp:"required" description:"The new schedule, under the rules of create."`
}

// PauseArgs names a trigger to pause.
type PauseArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The trigger's slug."`
}

// ResumeArgs names a trigger to resume.
type ResumeArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The trigger's slug."`
}

// DeleteArgs names a trigger to remove.
type DeleteArgs struct {
	Slug string `json:"slug" mcp:"required" description:"The trigger's slug."`
}

// Trigger describes a single trigger.
type Trigger struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	When      string  `json:"when"`
	Owner     string  `json:"owner"`
	Status    string  `json:"status"`
	Created   string  `json:"created"`
	LastFired *string `json:"last_fired"`
	Next      *string `json:"next"`
}

// ListedTrigger omits the created time.
type ListedTrigger struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	When      string  `json:"when"`
	Owner     string  `json:"owner"`
	Status    string  `json:"status"`
	LastFired *string `json:"last_fired"`
	Next      *string `json:"next"`
}

// TriggerList holds triggers in slug order.
type TriggerList struct {
	Triggers []ListedTrigger `json:"triggers"`
}

// Deleted confirms removal.
type Deleted struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// Register registers the seven public tools.
func Register(srv *mcp.Server, cfg Config) {
	h := handlers{cfg}
	mcp.AddTool(srv, mcp.Tool[ListArgs, TriggerList]{Name: "list", Description: listDescription, Effect: mcp.Read, Handler: h.list})
	mcp.AddTool(srv, mcp.Tool[ShowArgs, Trigger]{Name: "show", Description: showDescription, Effect: mcp.Read, Handler: h.show})
	mcp.AddTool(srv, mcp.Tool[CreateArgs, Trigger]{Name: "create", Description: createDescription, Effect: mcp.Additive, Handler: h.create})
	mcp.AddTool(srv, mcp.Tool[UpdateArgs, Trigger]{Name: "update", Description: updateDescription, Effect: mcp.Additive, Handler: h.update})
	mcp.AddTool(srv, mcp.Tool[PauseArgs, Trigger]{Name: "pause", Description: pauseDescription, Effect: mcp.Destructive, Handler: h.pause})
	mcp.AddTool(srv, mcp.Tool[ResumeArgs, Trigger]{Name: "resume", Description: resumeDescription, Effect: mcp.Additive, Handler: h.resume})
	mcp.AddTool(srv, mcp.Tool[DeleteArgs, Deleted]{Name: "delete", Description: deleteDescription, Effect: mcp.Destructive, Handler: h.delete})
}

type handlers struct{ cfg Config }

func missing(slug string) error { return fmt.Errorf("no trigger named '%s'", slug) }
func answerError(err error, slug, when string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return missing(slug)
	case errors.Is(err, store.ErrSlugTaken):
		return fmt.Errorf("a trigger named '%s' already exists", slug)
	case errors.Is(err, store.ErrInvalid):
		return fmt.Errorf("invalid when '%s'", when)
	default:
		return errors.New(store.Unreachable)
	}
}
func text(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
func (h handlers) object(x store.Trigger) Trigger {
	t := Trigger{ID: x.ID, Slug: x.Slug, When: x.When, Owner: x.OwnerEmail, Status: x.Status, Created: text(x.Created)}
	if !x.LastFired.IsZero() {
		v := text(x.LastFired)
		t.LastFired = &v
	}
	if n, ok := h.cfg.Scheduler.Next(x.ID); ok {
		v := text(n)
		t.Next = &v
	}
	return t
}
func (h handlers) list(ctx context.Context, _ identity.Caller, _ ListArgs) (TriggerList, error) {
	xs, err := h.cfg.Store.List(ctx)
	if err != nil {
		return TriggerList{}, answerError(err, "", "")
	}
	result := TriggerList{Triggers: make([]ListedTrigger, 0, len(xs))}
	for _, x := range xs {
		v := h.object(x)
		result.Triggers = append(result.Triggers, ListedTrigger{ID: v.ID, Slug: v.Slug, When: v.When, Owner: v.Owner, Status: v.Status, LastFired: v.LastFired, Next: v.Next})
	}
	return result, nil
}
func (h handlers) show(ctx context.Context, _ identity.Caller, a ShowArgs) (Trigger, error) {
	if !store.ValidSlug(a.Slug) {
		return Trigger{}, missing(a.Slug)
	}
	x, err := h.cfg.Store.Get(ctx, a.Slug)
	if err != nil {
		return Trigger{}, answerError(err, a.Slug, "")
	}
	return h.object(x), nil
}
func (h handlers) create(ctx context.Context, c identity.Caller, a CreateArgs) (Trigger, error) {
	if !store.ValidSlug(a.Slug) {
		return Trigger{}, fmt.Errorf("invalid slug '%s'", a.Slug)
	}
	_, err := h.cfg.Store.Get(ctx, a.Slug)
	if err == nil {
		return Trigger{}, fmt.Errorf("a trigger named '%s' already exists", a.Slug)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Trigger{}, answerError(err, a.Slug, a.When)
	}
	if !store.ValidWhen(a.When) {
		return Trigger{}, fmt.Errorf("invalid when '%s'", a.When)
	}
	x, err := h.cfg.Scheduler.Create(ctx, store.Draft{Slug: a.Slug, When: a.When, OwnerID: c.UserID, OwnerEmail: c.Email})
	if err != nil {
		return Trigger{}, answerError(err, a.Slug, a.When)
	}
	return h.object(x), nil
}
func (h handlers) update(ctx context.Context, c identity.Caller, a UpdateArgs) (Trigger, error) {
	if !store.ValidSlug(a.Slug) {
		return Trigger{}, missing(a.Slug)
	}
	x, err := h.cfg.Scheduler.Update(ctx, c.UserID, a.Slug, a.When)
	if err != nil {
		return Trigger{}, answerError(err, a.Slug, a.When)
	}
	return h.object(x), nil
}
func (h handlers) pause(ctx context.Context, c identity.Caller, a PauseArgs) (Trigger, error) {
	if !store.ValidSlug(a.Slug) {
		return Trigger{}, missing(a.Slug)
	}
	x, err := h.cfg.Scheduler.Pause(ctx, c.UserID, a.Slug)
	if err != nil {
		return Trigger{}, answerError(err, a.Slug, "")
	}
	return h.object(x), nil
}
func (h handlers) resume(ctx context.Context, c identity.Caller, a ResumeArgs) (Trigger, error) {
	if !store.ValidSlug(a.Slug) {
		return Trigger{}, missing(a.Slug)
	}
	x, err := h.cfg.Scheduler.Resume(ctx, c.UserID, a.Slug)
	if err != nil {
		return Trigger{}, answerError(err, a.Slug, "")
	}
	return h.object(x), nil
}
func (h handlers) delete(ctx context.Context, c identity.Caller, a DeleteArgs) (Deleted, error) {
	if !store.ValidSlug(a.Slug) {
		return Deleted{}, missing(a.Slug)
	}
	x, err := h.cfg.Scheduler.Delete(ctx, c.UserID, a.Slug)
	if err != nil {
		return Deleted{}, answerError(err, a.Slug, "")
	}
	return Deleted{Deleted: true, ID: x.ID}, nil
}

const listDescription = "Every trigger in the space, by slug.\n\nTakes no arguments. Every user's triggers are listed, not only yours. Each trigger has its id, slug, when (its schedule, as it was given), owner (the email of the user who created it), status (active or paused), last_fired (the slot it last fired for; absent when it has never fired), and next (the next slot it fires; absent while paused). Times are UTC. Use show for one trigger's created time."

const showDescription = "One trigger, with its schedule, its owner, and when it last fired and fires next.\n\nPass slug, the trigger's slug; any user's trigger can be shown. The result has its id, slug, when (its schedule, as it was given), owner (the email of the user who created it), status (active or paused), created, last_fired (the slot it last fired for; absent when it has never fired), and next (the next slot it fires; absent while paused). Times are UTC."

const createDescription = "Create a trigger that emits an event on a schedule.\n\nslug is 1 to 64 characters: a lowercase letter, then lowercase letters and digits, with single underscores between them; it must not already be a trigger's slug in the space, whoever owns that trigger. when is the schedule, read in UTC: five cron fields (minute hour day-of-month month day-of-week), such as */15 * * * * or 0 9 * * MON-FRI, separated by single spaces with nothing before or after, or exactly one of @hourly, @daily, @weekly, @monthly, and @yearly; there is no seconds field and no other descriptor. Each time the schedule comes due, the trigger emits cron.<slug>.fired on the suite's event bus, with attrs trigger (its id), when, and scheduled (the slot it fired for); a slot missed is never made up. Creating it emits cron.<slug>.created. The trigger is yours: only you can update, pause, resume, or delete it. The result is what show returns."

const updateDescription = "Change the schedule of a trigger you own.\n\nPass slug and when, the new schedule, under the rules of create. Only the schedule changes: the trigger keeps its id, slug, status, and last_fired, and its next slot follows the new schedule. No event is emitted. Another user's trigger is refused as one that does not exist. The result is what show returns."

const pauseDescription = "Stop a trigger you own from firing.\n\nPass slug. A paused trigger keeps its slug, its schedule, and last_fired, has no next, and fires nothing until you resume it; the slots that pass while it is paused are never fired. Pausing emits cron.<slug>.paused; pausing a trigger already paused changes nothing. Another user's trigger is refused as one that does not exist. The result is what show returns."

const resumeDescription = "Start a paused trigger you own firing again, from its next slot.\n\nPass slug. The trigger fires again from the first slot of its schedule after the call; the slots it missed while paused are not made up. Resuming emits cron.<slug>.resumed; resuming a trigger already active changes nothing. Another user's trigger is refused as one that does not exist. The result is what show returns."

const deleteDescription = "Delete a trigger you own.\n\nPass slug. The trigger never fires again, and deleting it emits cron.<slug>.deleted. Its slug is free for anyone to take. A trigger created with the slug later is a new trigger with its own id. Another user's trigger is refused as one that does not exist. The result is deleted, true, and the id of the deleted trigger."
