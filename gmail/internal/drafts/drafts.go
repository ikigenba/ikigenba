// Package drafts manages compositions and reconciles their Gmail state.
package drafts

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

// Config supplies the store, provider and sinks of a service.
type Config struct {
	Store       *store.Store
	Gmail       *gmail.Client
	Connections *connections.Client
	Telemetry   *telemetry.Writer
	Events      *events.Emitter
	ConnectURL  string
}

// Service coordinates calls that mutate or reconcile drafts.
type Service struct {
	cfg Config
	mu  sync.Mutex
}

// New constructs the service.
func New(cfg Config) *Service { return &Service{cfg: cfg} }

// ComposeRequest holds a new composition or reply.
type ComposeRequest struct {
	To, Cc, Bcc   []string
	Subject       *string
	Body, ReplyTo string
}

func refusal(word string) error { return &transfers.Refusal{Word: word} }
func (s *Service) provider(err error) error {
	if errors.Is(err, gmail.ErrUnauthorized) {
		return &transfers.Refusal{Word: transfers.RefusalReconnect, URL: s.cfg.ConnectURL}
	}
	return refusal(transfers.RefusalProvider)
}

// Compose creates Gmail's draft and then records its composition.
func (s *Service) Compose(ctx context.Context, c identity.Caller, r ComposeRequest) (store.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := transfers.Token(ctx, s.cfg.Connections, s.cfg.ConnectURL, c)
	if err != nil {
		return store.Draft{}, err
	}
	if len(r.To) == 0 && r.ReplyTo == "" {
		return store.Draft{}, refusal(transfers.RefusalTo)
	}
	for _, list := range [][]string{r.To, r.Cc, r.Bcc} {
		for _, a := range list {
			if _, err = mail.ParseAddress(a); err != nil {
				return store.Draft{}, refusal(transfers.RefusalAddress)
			}
		}
	}
	d := store.Draft{To: r.To, Cc: r.Cc, Bcc: r.Bcc, Body: r.Body, ReplyTo: r.ReplyTo, UserID: c.UserID}
	thread := ""
	if r.ReplyTo != "" {
		original, e := s.cfg.Gmail.Show(ctx, tok.AccessToken, r.ReplyTo)
		if e != nil {
			if errors.Is(e, gmail.ErrNotFound) {
				return store.Draft{}, refusal(transfers.RefusalNotFound)
			}
			return store.Draft{}, s.provider(e)
		}
		thread = original.Thread
		d.HeaderInReplyTo = original.MessageID
		d.HeaderReferences = strings.TrimSpace(original.References)
		if original.MessageID != "" {
			if d.HeaderReferences != "" {
				d.HeaderReferences += " "
			}
			d.HeaderReferences += original.MessageID
		}
		if len(d.To) == 0 {
			header := strings.TrimSpace(original.ReplyTo)
			if header == "" {
				header = original.From
			}
			addresses, e := mail.ParseAddressList(header)
			if e != nil || len(addresses) == 0 {
				return store.Draft{}, refusal(transfers.RefusalTo)
			}
			for _, a := range addresses {
				d.To = append(d.To, a.Address)
			}
		}
		d.Subject = original.Subject
		if !strings.HasPrefix(strings.ToLower(strings.TrimLeftFunc(d.Subject, unicode.IsSpace)), "re:") {
			d.Subject = "Re: " + d.Subject
		}
	}
	if r.Subject != nil {
		d.Subject = *r.Subject
	}
	g, err := s.cfg.Gmail.CreateDraft(ctx, tok.AccessToken, thread, gmail.Build(transfers.Composition(d, tok.Account)))
	if err != nil {
		if errors.Is(err, gmail.ErrTooLarge) {
			return store.Draft{}, refusal(transfers.RefusalTooLarge)
		}
		return store.Draft{}, s.provider(err)
	}
	d.GmailDraft, d.Message, d.Thread = g.ID, g.Message, g.Thread
	d, err = s.cfg.Store.AddDraft(ctx, d)
	if err != nil {
		_ = s.cfg.Gmail.DeleteDraft(ctx, tok.AccessToken, g.ID)
		return store.Draft{}, store.ErrUnreachable
	}
	trail.RecordDraft(ctx, s.cfg.Telemetry, s.cfg.Events, trail.DraftCreated, d)
	return d, nil
}

func (s *Service) lookup(ctx context.Context, c identity.Caller, id string) (store.Draft, error) {
	d, err := s.cfg.Store.GetDraft(ctx, c.UserID, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Draft{}, refusal(transfers.RefusalNotFound)
	}
	if err != nil {
		return store.Draft{}, store.ErrUnreachable
	}
	return d, nil
}

func (s *Service) markError(ctx context.Context, c identity.Caller, id string, err error, get bool) (store.Draft, error) {
	if errors.Is(err, store.ErrNotOpen) {
		if get {
			return s.lookup(ctx, c, id)
		}
		return store.Draft{}, refusal(transfers.RefusalNotOpen)
	}
	return store.Draft{}, store.ErrUnreachable
}

// reconcile probes the current Gmail draft and, only when absent, its thread.
func (s *Service) reconcile(ctx context.Context, c identity.Caller, tok connections.Token, d store.Draft, get bool) (store.Draft, error) {
	g, err := s.cfg.Gmail.GetDraft(ctx, tok.AccessToken, d.GmailDraft)
	if err == nil {
		if g.Message != "" && (g.Message != d.Message || g.Thread != "" && g.Thread != d.Thread) {
			updated, e := s.cfg.Store.SetDraftMessage(ctx, d.ID, g.Message, g.Thread)
			if e != nil {
				return s.markError(ctx, c, d.ID, e, get)
			}
			d = updated
		}
		return d, nil
	}
	if !errors.Is(err, gmail.ErrNotFound) {
		return store.Draft{}, s.provider(err)
	}
	stats, err := s.cfg.Gmail.ThreadStats(ctx, tok.AccessToken, d.Thread)
	if err != nil && !errors.Is(err, gmail.ErrNotFound) {
		return store.Draft{}, s.provider(err)
	}
	var newest *gmail.Stat
	for i := range stats {
		v := &stats[i]
		if v.Sent && v.Date.After(d.Modified) && (newest == nil || !v.Date.Before(newest.Date)) {
			newest = v
		}
	}
	id := d.ID
	name := trail.DraftDiscarded
	if newest != nil {
		d, err = s.cfg.Store.MarkDraftSent(ctx, d.ID, newest.ID, newest.Thread, newest.Date)
		name = trail.DraftSent
	} else {
		d, err = s.cfg.Store.MarkDraftDiscarded(ctx, d.ID)
	}
	if err != nil {
		return s.markError(ctx, c, id, err, get)
	}
	trail.RecordDraft(ctx, s.cfg.Telemetry, s.cfg.Events, name, d)
	return d, nil
}

// Get returns the stored draft, reconciling it while it remains open.
func (s *Service) Get(ctx context.Context, c identity.Caller, id string) (store.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.lookup(ctx, c, id)
	if err != nil || d.Status != store.DraftOpen {
		return d, err
	}
	tok, err := transfers.Token(ctx, s.cfg.Connections, s.cfg.ConnectURL, c)
	if err != nil {
		return store.Draft{}, err
	}
	return s.reconcile(ctx, c, tok, d, true)
}

// Attachable checks the cap and reconciles a draft without reading its artifacts.
func (s *Service) Attachable(ctx context.Context, c identity.Caller, tok connections.Token, id string, a store.Attachment) (store.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.lookup(ctx, c, id)
	if err != nil {
		return store.Draft{}, err
	}
	composition := transfers.Composition(d, tok.Account)
	composition.Attachments = append(composition.Attachments, gmail.Part{Name: a.Name, ContentType: a.ContentType, Bytes: a.Bytes})
	if gmail.Build(composition).Size() > gmail.UploadCap {
		return store.Draft{}, refusal(transfers.RefusalTooLarge)
	}
	if d.Status != store.DraftOpen {
		return store.Draft{}, refusal(transfers.RefusalNotOpen)
	}
	d, err = s.reconcile(ctx, c, tok, d, false)
	if err != nil {
		return store.Draft{}, err
	}
	if d.Status != store.DraftOpen {
		return store.Draft{}, refusal(transfers.RefusalNotOpen)
	}
	return d, nil
}

func (s *Service) busy(ctx context.Context, c identity.Caller, id string) error {
	all, err := s.cfg.Store.ListTransfers(ctx, c.UserID)
	if err != nil {
		return store.ErrUnreachable
	}
	for _, t := range all {
		if t.Direction == store.DirectionPush && t.Draft == id && (t.Status == store.StatusQueued || t.Status == store.StatusRunning) {
			return refusal(transfers.RefusalBusy)
		}
	}
	return nil
}

// Send sends the message Gmail currently holds for the open draft.
func (s *Service) Send(ctx context.Context, c identity.Caller, id string) (store.Draft, error) {
	return s.finish(ctx, c, id, true)
}

// Discard deletes the open Gmail draft.
func (s *Service) Discard(ctx context.Context, c identity.Caller, id string) (store.Draft, error) {
	return s.finish(ctx, c, id, false)
}

func (s *Service) ended(d store.Draft, send bool) (store.Draft, error) {
	if !send && d.Status == store.DraftDiscarded {
		return d, nil
	}
	return store.Draft{}, refusal(transfers.RefusalNotOpen)
}
func (s *Service) finish(ctx context.Context, c identity.Caller, id string, send bool) (store.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := transfers.Token(ctx, s.cfg.Connections, s.cfg.ConnectURL, c)
	if err != nil {
		return store.Draft{}, err
	}
	d, err := s.lookup(ctx, c, id)
	if err != nil {
		return store.Draft{}, err
	}
	if d.Status != store.DraftOpen {
		return store.Draft{}, refusal(transfers.RefusalNotOpen)
	}
	d, err = s.reconcile(ctx, c, tok, d, false)
	if err != nil {
		return store.Draft{}, err
	}
	if d.Status != store.DraftOpen {
		return s.ended(d, send)
	}
	if err = s.busy(ctx, c, id); err != nil {
		return store.Draft{}, err
	}
	var g gmail.Draft
	if send {
		g, err = s.cfg.Gmail.SendDraft(ctx, tok.AccessToken, d.GmailDraft)
	} else {
		err = s.cfg.Gmail.DeleteDraft(ctx, tok.AccessToken, d.GmailDraft)
	}
	if errors.Is(err, gmail.ErrNotFound) {
		d, e := s.reconcile(ctx, c, tok, d, false)
		if e != nil {
			return store.Draft{}, e
		}
		if d.Status != store.DraftOpen {
			return s.ended(d, send)
		}
		return store.Draft{}, refusal(transfers.RefusalProvider)
	}
	if err != nil {
		return store.Draft{}, s.provider(err)
	}
	name := trail.DraftDiscarded
	if send {
		d, err = s.cfg.Store.MarkDraftSent(ctx, id, g.Message, g.Thread, time.Time{})
		name = trail.DraftSent
	} else {
		d, err = s.cfg.Store.MarkDraftDiscarded(ctx, id)
	}
	if err != nil {
		return s.markError(ctx, c, id, err, false)
	}
	trail.RecordDraft(ctx, s.cfg.Telemetry, s.cfg.Events, name, d)
	return d, nil
}
