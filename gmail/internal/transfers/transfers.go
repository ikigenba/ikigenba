// Package transfers validates and runs host-side mail transfers.
package transfers

import (
	"context"
	"errors"
	"net/mail"
	"path"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
)

// Drafts checks a draft before a transfer is accepted.
type Drafts interface {
	Attachable(context.Context, identity.Caller, connections.Token, string, store.Attachment) (store.Draft, error)
}

// Config supplies the storage, sibling clients, limits and clock.
type Config struct {
	Store                          *store.Store
	Gmail                          *gmail.Client
	Artifacts                      *artifacts.Client
	Connections                    *connections.Client
	Drafts                         Drafts
	Telemetry                      *telemetry.Writer
	Events                         *events.Emitter
	ConnectURL                     string
	MaxBytes, MaxActive, MaxQueued int64
	Now                            func() time.Time
}

// PullRequest names a message or part and its artifact destination.
type PullRequest struct {
	Message, Attachment, Path string
	Expires                   *string
}

// PushRequest names an artifact and the draft to receive it.
type PushRequest struct{ Path, Draft string }

// Refusal carries a machine word and an optional connection remedy.
type Refusal struct{ Word, URL string }

func (r *Refusal) Error() string { return r.Word }

// Refusal words and transfer constants form the public contract.
const (
	RefusalNotConnected = "not_connected"
	RefusalReconnect    = "reconnect"
	RefusalExpires      = "expires"
	RefusalTooLarge     = "too_large"
	RefusalNotFound     = "not_found"
	RefusalNotOpen      = "not_open"
	RefusalBusy         = "busy"
	RefusalTo           = "to"
	RefusalAddress      = "address"
	RefusalProvider     = "provider"
	RefusalPath         = "path"
	RefusalUnavailable  = "unavailable"
	RefusalStopping     = "stopping"
	RefusalQueueFull    = "queue_full"
	Provider            = "google"
	ContentTypeMessage  = "message/rfc822"
	ProgressStep        = 1 << 20
)

func refuse(w string) error { return &Refusal{Word: w} }

// Token obtains the connected account as the explicit caller.
func Token(ctx context.Context, cc *connections.Client, u string, c identity.Caller) (connections.Token, error) {
	if cc == nil {
		return connections.Token{}, refuse(RefusalUnavailable)
	}
	t, err := cc.Token(identity.NewContext(ctx, c), Provider)
	if err == nil {
		return t, nil
	}
	r := &Refusal{}
	var reconnect *connections.ReconnectError
	switch {
	case errors.Is(err, connections.ErrNotConnected):
		r.Word, r.URL = RefusalNotConnected, u
	case errors.As(err, &reconnect):
		r.Word, r.URL = RefusalReconnect, reconnect.URL
	case errors.Is(err, connections.ErrUnknownProvider), errors.Is(err, connections.ErrProvider):
		r.Word = RefusalProvider
	default:
		r.Word = RefusalUnavailable
	}
	return connections.Token{}, r
}

// Composition maps a stored draft to the shared message builder.
func Composition(d store.Draft, from string) gmail.Composition {
	c := gmail.Composition{From: mail.Address{Address: from}, Subject: d.Subject, Text: d.Body, InReplyTo: d.HeaderInReplyTo, References: d.HeaderReferences}
	parse := func(in []string) []mail.Address {
		out := []mail.Address{}
		for _, s := range in {
			if a, err := mail.ParseAddress(s); err == nil {
				out = append(out, *a)
			}
		}
		return out
	}
	c.To, c.Cc, c.Bcc = parse(d.To), parse(d.Cc), parse(d.Bcc)
	for _, a := range d.Attachments {
		c.Attachments = append(c.Attachments, gmail.Part{Name: a.Name, ContentType: a.ContentType, Bytes: a.Bytes})
	}
	return c
}

type job struct {
	t       store.Transfer
	ctx     context.Context
	cancel  context.CancelFunc
	expires time.Time
}

// Engine owns the global queue and running transfers.
type Engine struct {
	cfg      Config
	mu       sync.Mutex
	stopping bool
	active   map[string]*job
	queue    []*job
	changed  chan struct{}
}

// New constructs an idle engine from its required dependencies.
func New(cfg Config) *Engine {
	return &Engine{cfg: cfg, active: map[string]*job{}, changed: make(chan struct{})}
}

// ConnectURL returns the configured connection page.
func (e *Engine) ConnectURL() string { return e.cfg.ConnectURL }

// Token returns the caller's access token for the read tools.
func (e *Engine) Token(ctx context.Context, c identity.Caller) (string, error) {
	t, err := Token(ctx, e.cfg.Connections, e.cfg.ConnectURL, c)
	if err != nil {
		return "", err
	}
	return t.AccessToken, nil
}
func (e *Engine) gmailRefusal(err error) error {
	switch {
	case errors.Is(err, gmail.ErrNotFound):
		return refuse(RefusalNotFound)
	case errors.Is(err, gmail.ErrUnauthorized):
		return &Refusal{Word: RefusalReconnect, URL: e.cfg.ConnectURL}
	default:
		return refuse(RefusalProvider)
	}
}

// Pull validates and records a pull before moving bytes asynchronously.
func (e *Engine) Pull(ctx context.Context, c identity.Caller, r PullRequest) (store.Transfer, error) {
	tok, err := Token(ctx, e.cfg.Connections, e.cfg.ConnectURL, c)
	if err != nil {
		return store.Transfer{}, err
	}
	ctx = identity.NewContext(ctx, c)
	var expiry time.Time
	if r.Expires != nil {
		expiry, err = time.Parse(time.RFC3339, *r.Expires)
		if err != nil || !expiry.Truncate(time.Second).After(e.cfg.Now().Truncate(time.Second)) {
			return store.Transfer{}, refuse(RefusalExpires)
		}
	}
	if e.cfg.Artifacts == nil {
		return store.Transfer{}, refuse(RefusalUnavailable)
	}
	_, err = e.cfg.Artifacts.Head(ctx, r.Path)
	if errors.Is(err, artifacts.ErrBadPath) {
		return store.Transfer{}, refuse(RefusalPath)
	}
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		return store.Transfer{}, refuse(RefusalUnavailable)
	}
	m, err := e.cfg.Gmail.Show(ctx, tok.AccessToken, r.Message)
	if err != nil {
		return store.Transfer{}, e.gmailRefusal(err)
	}
	t := store.Transfer{Direction: store.DirectionPull, Message: r.Message, Attachment: r.Attachment, Path: r.Path, Name: m.Subject, ContentType: ContentTypeMessage}
	var size int64
	if r.Attachment != "" {
		found := false
		for _, a := range m.Attachments {
			if a.ID == r.Attachment {
				found = true
				t.Name, t.ContentType, size = a.Name, a.MimeType, a.Bytes
				break
			}
		}
		if !found {
			return store.Transfer{}, refuse(RefusalNotFound)
		}
	} else {
		s, err := e.cfg.Gmail.Stat(ctx, tok.AccessToken, r.Message)
		if err != nil {
			return store.Transfer{}, e.gmailRefusal(err)
		}
		size = s.SizeEstimate
	}
	if size > e.cfg.MaxBytes {
		return store.Transfer{}, refuse(RefusalTooLarge)
	}
	return e.accept(ctx, c, t, expiry)
}

// Push validates and records an artifact attachment for asynchronous work.
func (e *Engine) Push(ctx context.Context, c identity.Caller, r PushRequest) (store.Transfer, error) {
	tok, err := Token(ctx, e.cfg.Connections, e.cfg.ConnectURL, c)
	if err != nil {
		return store.Transfer{}, err
	}
	ctx = identity.NewContext(ctx, c)
	if e.cfg.Artifacts == nil {
		return store.Transfer{}, refuse(RefusalUnavailable)
	}
	m, err := e.cfg.Artifacts.Head(ctx, r.Path)
	if err != nil {
		w := RefusalUnavailable
		if errors.Is(err, artifacts.ErrBadPath) {
			w = RefusalPath
		} else if errors.Is(err, artifacts.ErrNotFound) {
			w = RefusalNotFound
		}
		return store.Transfer{}, refuse(w)
	}
	if m.Bytes > e.cfg.MaxBytes {
		return store.Transfer{}, refuse(RefusalTooLarge)
	}
	a := store.Attachment{Path: r.Path, Name: path.Base(r.Path), ContentType: m.ContentType, Bytes: m.Bytes}
	d, err := e.cfg.Drafts.Attachable(ctx, c, tok, r.Draft, a)
	if err != nil {
		var refusal *Refusal
		if errors.As(err, &refusal) {
			return store.Transfer{}, refusal
		}
		return store.Transfer{}, store.ErrUnreachable
	}
	return e.accept(ctx, c, store.Transfer{Direction: store.DirectionPush, Message: d.Message, Draft: d.ID, Name: a.Name, Path: a.Path, ContentType: a.ContentType}, time.Time{})
}
func (e *Engine) blocked(t store.Transfer) bool {
	if t.Direction != store.DirectionPush {
		return false
	}
	for _, j := range e.active {
		if j.t.Direction == store.DirectionPush && j.t.Draft == t.Draft {
			return true
		}
	}
	for _, j := range e.queue {
		if j.t.Direction == store.DirectionPush && j.t.Draft == t.Draft {
			return true
		}
	}
	return false
}
func (e *Engine) signal() { close(e.changed); e.changed = make(chan struct{}) }
func (e *Engine) accept(ctx context.Context, c identity.Caller, t store.Transfer, expiry time.Time) (store.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
		return store.Transfer{}, refuse(RefusalStopping)
	}
	t.Status = store.StatusRunning
	if int64(len(e.active)) >= e.cfg.MaxActive || e.blocked(t) {
		t.Status = store.StatusQueued
		if int64(len(e.queue)) >= e.cfg.MaxQueued {
			return store.Transfer{}, refuse(RefusalQueueFull)
		}
	}
	t.UserID, t.UserEmail, t.RequestID = c.UserID, c.Email, c.RequestID
	t, err := e.cfg.Store.AddTransfer(ctx, t)
	if err != nil {
		return store.Transfer{}, store.ErrUnreachable
	}
	work, cancel := context.WithCancel(identity.NewContext(context.WithoutCancel(ctx), c))
	j := &job{t: t, ctx: work, cancel: cancel, expires: expiry}
	if t.Status == store.StatusQueued {
		e.queue = append(e.queue, j)
	} else {
		e.start(j)
	}
	return t, nil
}
func (e *Engine) start(j *job) {
	e.active[j.t.ID] = j
	trail.Record(j.ctx, e.cfg.Telemetry, e.cfg.Events, trail.TransferStarted, j.t)
	go e.work(j)
}
func (e *Engine) finish(j *job, end store.Ending) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if j.ctx.Err() != nil {
		end = store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted, Bytes: end.Bytes}
	}
	t, err := e.cfg.Store.FinishTransfer(context.WithoutCancel(j.ctx), j.t.ID, end)
	if err == nil {
		trail.Record(context.WithoutCancel(j.ctx), e.cfg.Telemetry, e.cfg.Events, trail.TransferFinished, t)
	}
	j.cancel()
	delete(e.active, j.t.ID)
	if !e.stopping {
		e.schedule()
	}
	e.signal()
}
func (e *Engine) schedule() {
	for int64(len(e.active)) < e.cfg.MaxActive {
		index := -1
		seen := map[string]bool{}
		for i, j := range e.queue {
			blocked := false
			if j.t.Direction == store.DirectionPush {
				blocked = seen[j.t.Draft]
				seen[j.t.Draft] = true
				for _, a := range e.active {
					if a.t.Direction == store.DirectionPush && a.t.Draft == j.t.Draft {
						blocked = true
					}
				}
			}
			if !blocked {
				index = i
				break
			}
		}
		if index < 0 {
			return
		}
		j := e.queue[index]
		e.queue = append(e.queue[:index], e.queue[index+1:]...)
		t, err := e.cfg.Store.StartTransfer(context.WithoutCancel(j.ctx), j.t.ID)
		if err != nil {
			j.cancel()
			continue
		}
		j.t = t
		e.start(j)
	}
}

// Recover settles transfers abandoned by a previous process.
func (e *Engine) Recover(ctx context.Context) error {
	ts, err := e.cfg.Store.UnfinishedTransfers(ctx)
	if err != nil {
		return store.ErrUnreachable
	}
	for _, t := range ts {
		ended, err := e.cfg.Store.FinishTransfer(ctx, t.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted, Bytes: t.Bytes})
		if err != nil {
			return store.ErrUnreachable
		}
		trail.Record(identity.NewContext(context.Background(), trail.Caller(t)), e.cfg.Telemetry, e.cfg.Events, trail.TransferFinished, ended)
	}
	return nil
}

// Drain refuses new work, abandons the queue and waits for running work.
func (e *Engine) Drain(ctx context.Context) {
	e.mu.Lock()
	e.stopping = true
	for _, j := range e.queue {
		t, err := e.cfg.Store.FinishTransfer(context.WithoutCancel(j.ctx), j.t.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
		if err == nil {
			trail.Record(j.ctx, e.cfg.Telemetry, e.cfg.Events, trail.TransferFinished, t)
		}
		j.cancel()
	}
	e.queue = nil
	e.signal()
	for len(e.active) > 0 {
		ch := e.changed
		e.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			e.mu.Lock()
			for _, j := range e.active {
				j.cancel()
			}
			e.mu.Unlock()
			<-ch
		}
		e.mu.Lock()
	}
	e.mu.Unlock()
}
