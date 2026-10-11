// Package transfers validates and streams background file transfers.
package transfers

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/trail"
)

// Refusal words, provider and progress cadence are stable protocol values.
const (
	RefusalNotConnected  = "not_connected"
	RefusalReconnect     = "reconnect"
	RefusalNotAFile      = "not_a_file"
	RefusalNotExportable = "not_exportable"
	RefusalFormat        = "format"
	RefusalExpires       = "expires"
	RefusalTooLarge      = "too_large"
	RefusalNotFound      = "not_found"
	RefusalProvider      = "provider"
	RefusalPath          = "path"
	RefusalUnavailable   = "unavailable"
	RefusalStopping      = "stopping"
	RefusalQueueFull     = "queue_full"
	Provider             = "google"
	ProgressStep         = 1 << 20
)

// Refusal carries a machine-readable refusal and optional connection address.
type Refusal struct{ Word, URL string }

func (r *Refusal) Error() string { return r.Word }

// Config supplies the storage, clients, limits and clock.
type Config struct {
	Store                                               *store.Store
	Drive                                               *drive.Client
	Artifacts                                           *artifacts.Client
	Connections                                         *connections.Client
	Telemetry                                           *telemetry.Writer
	Events                                              *events.Emitter
	ConnectURL                                          string
	MaxBytes, MaxActive, MaxQueued, KeepDays, KeepCount int64
	Now                                                 func() time.Time
}

// PullRequest describes a file to copy into artifacts.
type PullRequest struct {
	File, Path, Format string
	Expires            *string
}

// PushRequest describes an artifact to upload to Drive.
type PushRequest struct{ Path, Folder, Name, File string }
type job struct {
	t       store.Transfer
	ctx     context.Context
	cancel  context.CancelFunc
	expires time.Time
	link    string
	push    PushRequest
}

// Engine coordinates the shared queue and active transfers.
type Engine struct {
	cfg      Config
	mu       sync.Mutex
	stopping bool
	queue    []*job
	active   map[string]*job
	changed  chan struct{}
}

// New constructs an engine with the supplied dependencies.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Engine{cfg: cfg, active: make(map[string]*job), changed: make(chan struct{})}
}
func refuse(word string) error { return &Refusal{Word: word} }

// Token obtains a caller token or a typed refusal.
func (e *Engine) Token(ctx context.Context, c identity.Caller) (string, error) {
	if e.cfg.Connections == nil {
		return "", refuse(RefusalUnavailable)
	}
	tok, err := e.cfg.Connections.Token(identity.NewContext(ctx, c), Provider)
	if err == nil {
		return tok.AccessToken, nil
	}
	var reconnect *connections.ReconnectError
	switch {
	case errors.Is(err, connections.ErrNotConnected):
		return "", &Refusal{RefusalNotConnected, e.cfg.ConnectURL}
	case errors.As(err, &reconnect):
		return "", &Refusal{RefusalReconnect, reconnect.URL}
	case errors.Is(err, connections.ErrUnknownProvider), errors.Is(err, connections.ErrProvider):
		return "", refuse(RefusalProvider)
	default:
		return "", refuse(RefusalUnavailable)
	}
}
func driveRefusal(err error) error {
	if errors.Is(err, drive.ErrNotFound) {
		return refuse(RefusalNotFound)
	}
	return refuse(RefusalProvider)
}

// Pull validates and records an asynchronous download.
func (e *Engine) Pull(ctx context.Context, c identity.Caller, r PullRequest) (store.Transfer, error) {
	tok, err := e.Token(ctx, c)
	if err != nil {
		return store.Transfer{}, err
	}
	var expiry time.Time
	if r.Expires != nil {
		expiry, err = time.Parse(time.RFC3339, *r.Expires)
		if err != nil || !expiry.Truncate(time.Second).After(e.cfg.Now().Truncate(time.Second)) {
			return store.Transfer{}, refuse(RefusalExpires)
		}
	}
	ctx = identity.NewContext(ctx, c)
	if e.cfg.Artifacts == nil {
		return store.Transfer{}, refuse(RefusalUnavailable)
	}
	_, err = e.cfg.Artifacts.Head(ctx, r.Path)
	if err != nil && !errors.Is(err, artifacts.ErrNotFound) {
		if errors.Is(err, artifacts.ErrBadPath) {
			return store.Transfer{}, refuse(RefusalPath)
		}
		return store.Transfer{}, refuse(RefusalUnavailable)
	}
	f, err := e.cfg.Drive.Show(ctx, tok, r.File)
	if err != nil {
		return store.Transfer{}, driveRefusal(err)
	}
	if f.Kind == drive.KindShortcut && f.Target != nil {
		f, err = e.cfg.Drive.Show(ctx, tok, f.Target.ID)
		if err != nil {
			return store.Transfer{}, driveRefusal(err)
		}
	}
	if f.Kind == drive.KindFolder || f.Kind == drive.KindShortcut {
		return store.Transfer{}, refuse(RefusalNotAFile)
	}
	if f.Kind == drive.KindOther {
		return store.Transfer{}, refuse(RefusalNotExportable)
	}
	formats := drive.Formats(f.Kind)
	if f.Kind == drive.KindFile && r.Format != "" || len(formats) > 0 && r.Format != "" && !slices.Contains(formats, r.Format) {
		return store.Transfer{}, refuse(RefusalFormat)
	}
	if f.Kind == drive.KindFile && f.Bytes != nil && *f.Bytes > e.cfg.MaxBytes {
		return store.Transfer{}, refuse(RefusalTooLarge)
	}
	format, ct := r.Format, f.MimeType
	if f.Kind != drive.KindFile {
		if format == "" {
			format = drive.DefaultFormat(f.Kind)
		}
		ct = format
	}
	return e.add(ctx, c, &job{t: store.Transfer{Direction: store.DirectionPull, File: f.ID, Name: f.Name, Kind: string(f.Kind), Path: r.Path, ContentType: ct, Format: format}, expires: expiry, link: f.Link})
}

// Push validates and records an asynchronous upload.
func (e *Engine) Push(ctx context.Context, c identity.Caller, r PushRequest) (store.Transfer, error) {
	tok, err := e.Token(ctx, c)
	if err != nil {
		return store.Transfer{}, err
	}
	ctx = identity.NewContext(ctx, c)
	if e.cfg.Artifacts == nil {
		return store.Transfer{}, refuse(RefusalUnavailable)
	}
	m, err := e.cfg.Artifacts.Head(ctx, r.Path)
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrBadPath):
			return store.Transfer{}, refuse(RefusalPath)
		case errors.Is(err, artifacts.ErrNotFound):
			return store.Transfer{}, refuse(RefusalNotFound)
		default:
			return store.Transfer{}, refuse(RefusalUnavailable)
		}
	}
	if m.Bytes > e.cfg.MaxBytes {
		return store.Transfer{}, refuse(RefusalTooLarge)
	}
	name := r.Name
	if r.File != "" || r.Folder != "" {
		id, kind := r.File, drive.KindFile
		if id == "" {
			id, kind = r.Folder, drive.KindFolder
		}
		f, err := e.cfg.Drive.Show(ctx, tok, id)
		if err != nil {
			return store.Transfer{}, driveRefusal(err)
		}
		if f.Kind != kind {
			return store.Transfer{}, refuse(RefusalNotAFile)
		}
		if r.File != "" && name == "" {
			name = f.Name
		}
	}
	if name == "" && r.File == "" {
		name = r.Path[strings.LastIndex(r.Path, "/")+1:]
	}
	return e.add(ctx, c, &job{t: store.Transfer{Direction: store.DirectionPush, File: r.File, Name: name, Kind: string(drive.KindFile), Path: r.Path, ContentType: m.ContentType}, push: r})
}
func (e *Engine) signal() { close(e.changed); e.changed = make(chan struct{}) }
func (e *Engine) add(ctx context.Context, c identity.Caller, j *job) (store.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
		return store.Transfer{}, refuse(RefusalStopping)
	}
	if int64(len(e.queue)) >= e.cfg.MaxQueued {
		return store.Transfer{}, refuse(RefusalQueueFull)
	}
	j.t.UserID, j.t.UserEmail, j.t.RequestID = c.UserID, c.Email, c.RequestID
	j.t.Status = store.StatusRunning
	if int64(len(e.active)) >= e.cfg.MaxActive || len(e.queue) > 0 {
		j.t.Status = store.StatusQueued
	}
	t, err := e.cfg.Store.Add(ctx, j.t)
	if err != nil {
		return store.Transfer{}, err
	}
	j.t = t
	j.ctx, j.cancel = context.WithCancel(context.WithoutCancel(ctx))
	if t.Status == store.StatusQueued {
		e.queue = append(e.queue, j)
	} else {
		e.start(j)
	}
	e.signal()
	return t, nil
}
func (e *Engine) start(j *job) {
	e.active[j.t.ID] = j
	trail.Record(j.ctx, e.cfg.Telemetry, e.cfg.Events, trail.Started, j.t)
	go e.work(j)
}
func (e *Engine) work(j *job) {
	ending := store.Ending{Status: store.StatusFailed, Reason: store.ReasonProvider}
	token, err := e.Token(j.ctx, trail.Caller(j.t))
	switch {
	case err != nil:
		var r *Refusal
		if errors.As(err, &r) && (r.Word == RefusalReconnect || r.Word == RefusalNotConnected) {
			ending.Reason = store.ReasonReconnect
		}

	case j.t.Direction == store.DirectionPull:
		ending = e.pullWork(j, token)
	default:
		ending = e.pushWork(j, token)
	}
	if j.ctx.Err() != nil {
		ending.Status = store.StatusFailed
		ending.Reason = store.ReasonInterrupted
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.finish(j, ending)
	delete(e.active, j.t.ID)
	j.cancel()
	for !e.stopping && int64(len(e.active)) < e.cfg.MaxActive && len(e.queue) > 0 {
		next := e.queue[0]
		e.queue = e.queue[1:]
		t, startErr := e.cfg.Store.Start(context.Background(), next.t.ID)
		if startErr != nil {
			next.cancel()
			continue
		}
		next.t = t
		e.start(next)
	}
	e.signal()
}
func (e *Engine) finish(j *job, ending store.Ending) {
	t, err := e.cfg.Store.Finish(context.Background(), j.t.ID, ending)
	if err == nil {
		trail.Record(j.ctx, e.cfg.Telemetry, e.cfg.Events, trail.Finished, t)
	}
}

type counted struct {
	mu       sync.Mutex
	body     io.Reader
	n, step  int64
	err      error
	progress func(int64)
}

func (c *counted) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.body.Read(p)
	c.n += int64(n)
	if err != nil && err != io.EOF {
		c.err = err
	}
	if c.n/ProgressStep > c.step {
		c.step = c.n / ProgressStep
		c.progress(c.n)
	}
	return n, err
}
func (e *Engine) counter(j *job, body io.Reader) *counted {
	return &counted{body: body, progress: func(n int64) { _, _ = e.cfg.Store.SetBytes(context.Background(), j.t.ID, n) }}
}
func (e *Engine) pullWork(j *job, token string) store.Ending {
	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	out := store.Ending{Status: store.StatusFailed, Reason: store.ReasonProvider}
	var body io.ReadCloser
	var err error
	if j.t.Kind == string(drive.KindFile) {
		body, err = e.cfg.Drive.Media(ctx, token, j.t.File)
	} else {
		body, err = e.cfg.Drive.Export(ctx, token, j.t.File, j.t.Format)
	}
	if err != nil {
		if errors.Is(err, drive.ErrNotFound) {
			out.Reason = store.ReasonNotFound
		}
		if errors.Is(err, drive.ErrTooLarge) {
			out.Reason = store.ReasonTooLarge
		}
		return out
	}
	defer func() { _ = body.Close() }()
	count := e.counter(j, body)
	m, err := e.cfg.Artifacts.Put(ctx, j.t.Path, artifacts.PutOptions{ContentType: j.t.ContentType, Expires: j.expires}, count)
	cancel()
	_ = body.Close()
	count.mu.Lock()
	defer count.mu.Unlock()
	out.Bytes = count.n
	if count.err != nil && !errors.Is(count.err, context.Canceled) {
		if errors.Is(count.err, drive.ErrTooLarge) {
			out.Reason = store.ReasonTooLarge
		}
		return out
	}
	if err != nil {
		out.Reason = store.ReasonArtifacts
		if errors.Is(err, artifacts.ErrForbidden) {
			out.Reason = store.ReasonForbidden
		}
		if errors.Is(err, artifacts.ErrTooLarge) {
			out.Reason = store.ReasonTooLarge
		}
		return out
	}
	out.Status = store.StatusDone
	out.Reason = ""
	out.URL = m.URL
	out.ArtifactID = m.ID
	out.Link = j.link
	return out
}
func (e *Engine) pushWork(j *job, token string) store.Ending {
	out := store.Ending{Status: store.StatusFailed, Reason: store.ReasonArtifacts}
	m, body, err := e.cfg.Artifacts.Get(j.ctx, j.t.Path)
	if err != nil {
		if errors.Is(err, artifacts.ErrNotFound) {
			out.Reason = store.ReasonNotFound
		}
		return out
	}
	defer func() { _ = body.Close() }()
	u := drive.Upload{File: j.push.File, ContentType: m.ContentType, Bytes: m.Bytes}
	if u.File == "" {
		u.Name = j.t.Name
		u.Folder = j.push.Folder
	} else {
		u.Name = j.push.Name
	}
	count := e.counter(j, body)
	f, err := e.cfg.Drive.Upload(j.ctx, token, u, count)
	out.Bytes = count.n
	if err != nil {
		switch {
		case errors.Is(err, drive.ErrNotFound):
			out.Reason = store.ReasonNotFound
		case errors.Is(err, drive.ErrForbidden):
			out.Reason = store.ReasonForbidden
		case errors.Is(err, drive.ErrTooLarge):
			out.Reason = store.ReasonTooLarge
		case errors.Is(err, drive.ErrProvider):
			out.Reason = store.ReasonProvider
		}
		return out
	}
	out.Status = store.StatusDone
	out.Reason = ""
	out.File = f.ID
	out.Name = f.Name
	out.Link = f.Link
	out.URL = m.URL
	return out
}

// Recover settles transfers interrupted by an earlier process.
func (e *Engine) Recover(ctx context.Context) error {
	ts, err := e.cfg.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	for _, t := range ts {
		finished, err := e.cfg.Store.Finish(ctx, t.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted, Bytes: t.Bytes})
		if err != nil {
			return err
		}
		trail.Record(context.Background(), e.cfg.Telemetry, e.cfg.Events, trail.Finished, finished)
	}
	return nil
}

// Prune removes records past the retention limits.
func (e *Engine) Prune(ctx context.Context) error {
	_, err := e.cfg.Store.Prune(ctx, e.cfg.Now(), e.cfg.KeepDays, e.cfg.KeepCount)
	return err
}

// Drain stops accepting work and waits until running work ends.
func (e *Engine) Drain(ctx context.Context) {
	e.mu.Lock()
	e.stopping = true
	for _, j := range e.queue {
		e.finish(j, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
		j.cancel()
	}
	e.queue = nil
	e.signal()
	for len(e.active) > 0 {
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			e.mu.Lock()
			for _, j := range e.active {
				j.cancel()
			}
			e.mu.Unlock()
			<-changed
		}
		e.mu.Lock()
	}
	e.mu.Unlock()
}
