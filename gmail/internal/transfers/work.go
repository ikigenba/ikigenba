package transfers

import (
	"context"
	"errors"
	"io"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

type counter struct {
	io.Reader
	n        int64
	progress func(int64)
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	before := c.n / ProgressStep
	c.n += int64(n)
	if c.n/ProgressStep > before {
		c.progress(c.n / ProgressStep * ProgressStep)
	}
	return n, err
}

type countedBody struct {
	*counter
	io.Closer
}

func (e *Engine) work(j *job) {
	tok, err := Token(j.ctx, e.cfg.Connections, e.cfg.ConnectURL, trail.Caller(j.t))
	if err != nil {
		reason := store.ReasonProvider
		var r *Refusal
		if errors.As(err, &r) && (r.Word == RefusalReconnect || r.Word == RefusalNotConnected) {
			reason = store.ReasonReconnect
		}
		e.finish(j, store.Ending{Status: store.StatusFailed, Reason: reason})
		return
	}
	if j.t.Direction == store.DirectionPull {
		e.pullWork(j, tok)
	} else {
		e.pushWork(j, tok)
	}
}
func (e *Engine) progress(j *job, n int64) {
	_, _ = e.cfg.Store.SetTransferBytes(context.WithoutCancel(j.ctx), j.t.ID, n)
}
func gmailReason(err error) string {
	switch {
	case errors.Is(err, gmail.ErrNotFound):
		return store.ReasonNotFound
	case errors.Is(err, gmail.ErrUnauthorized):
		return store.ReasonReconnect
	case errors.Is(err, gmail.ErrTooLarge):
		return store.ReasonTooLarge
	default:
		return store.ReasonProvider
	}
}
func (e *Engine) pullWork(j *job, tok connections.Token) {
	var content gmail.Content
	var err error
	if j.t.Attachment != "" {
		content, err = e.cfg.Gmail.Attachment(j.ctx, tok.AccessToken, j.t.Message, j.t.Attachment, e.cfg.MaxBytes)
	} else {
		content, err = e.cfg.Gmail.Raw(j.ctx, tok.AccessToken, j.t.Message, e.cfg.MaxBytes)
	}
	if err != nil {
		e.finish(j, store.Ending{Status: store.StatusFailed, Reason: gmailReason(err)})
		return
	}
	count := &counter{Reader: content.Reader, progress: func(n int64) { e.progress(j, n) }}
	m, err := e.cfg.Artifacts.Put(j.ctx, j.t.Path, artifacts.PutOptions{ContentType: j.t.ContentType, Expires: j.expires}, count)
	end := store.Ending{Status: store.StatusDone, Bytes: count.n, URL: m.URL, ArtifactID: m.ID, Link: urls.MessageLink(j.t.Message)}
	if err != nil {
		reason := store.ReasonArtifacts
		switch {
		case errors.Is(err, artifacts.ErrForbidden):
			reason = store.ReasonForbidden
		case errors.Is(err, artifacts.ErrTooLarge):
			reason = store.ReasonTooLarge
		}
		end = store.Ending{Status: store.StatusFailed, Reason: reason, Bytes: count.n}
	}
	e.finish(j, end)
}
func (e *Engine) pushWork(j *job, tok connections.Token) {
	var bodies []io.ReadCloser
	finish := func(end store.Ending) {
		for _, body := range bodies {
			_ = body.Close()
		}
		e.finish(j, end)
	}
	d, err := e.cfg.Store.GetDraft(j.ctx, j.t.UserID, j.t.Draft)
	if err != nil || d.Status != store.DraftOpen {
		reason := store.ReasonNotFound
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			reason = store.ReasonInterrupted
		}
		finish(store.Ending{Status: store.StatusFailed, Reason: reason})
		return
	}
	c := Composition(d, tok.Account)
	defer func() {
		for _, b := range bodies {
			_ = b.Close()
		}
	}()
	var pushed artifacts.Metadata
	var count *counter
	for i := 0; i <= len(d.Attachments); i++ {
		p := j.t.Path
		if i < len(d.Attachments) {
			p = d.Attachments[i].Path
		}
		m, b, err := e.cfg.Artifacts.Get(j.ctx, p)
		if err != nil {
			reason := store.ReasonArtifacts
			if errors.Is(err, artifacts.ErrNotFound) {
				reason = store.ReasonNotFound
			}
			finish(store.Ending{Status: store.StatusFailed, Reason: reason})
			return
		}
		bodies = append(bodies, b)
		var part gmail.Part
		if i < len(d.Attachments) {
			part = c.Attachments[i]
		} else {
			pushed = m
			count = &counter{Reader: b, progress: func(n int64) { e.progress(j, n) }}
			b = &countedBody{counter: count, Closer: b}
			part = gmail.Part{Name: j.t.Name, ContentType: j.t.ContentType}
		}
		part.Bytes = m.Bytes
		body := b
		part.Open = func() (io.ReadCloser, error) { return body, nil }
		if i < len(d.Attachments) {
			c.Attachments[i] = part
		} else {
			c.Attachments = append(c.Attachments, part)
		}
	}
	outgoing := gmail.Build(c)
	defer func() { _ = outgoing.Close() }()
	g, err := e.cfg.Gmail.UpdateDraft(j.ctx, tok.AccessToken, d.GmailDraft, d.Thread, outgoing)
	if err != nil {
		reason := gmailReason(err)
		if !errors.Is(err, gmail.ErrNotFound) && !errors.Is(err, gmail.ErrUnauthorized) && !errors.Is(err, gmail.ErrTooLarge) && !errors.Is(err, gmail.ErrProvider) {
			reason = store.ReasonArtifacts
		}
		finish(store.Ending{Status: store.StatusFailed, Reason: reason, Bytes: count.n})
		return
	}
	if j.ctx.Err() != nil {
		finish(store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted, Bytes: count.n})
		return
	}
	_, err = e.cfg.Store.AttachToDraft(j.ctx, d.ID, store.Attachment{Path: j.t.Path, Name: j.t.Name, ContentType: j.t.ContentType, Bytes: count.n}, g.Message, g.Thread)
	if err != nil {
		reason := store.ReasonInterrupted
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNotOpen) {
			reason = store.ReasonNotFound
		}
		finish(store.Ending{Status: store.StatusFailed, Reason: reason, Bytes: count.n})
		return
	}
	finish(store.Ending{Status: store.StatusDone, Bytes: count.n, Message: g.Message, URL: pushed.URL, Link: urls.DraftLink(g.Message)})
}
