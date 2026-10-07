// Package runs owns run folders, process lifetimes and catalog transitions.
package runs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// Run folder entry names.
const (
	InputFile  = "input.json"
	StdoutFile = "stdout"
	StderrFile = "stderr"
	TreeDir    = "tree"
	OutDir     = "out"
)

// ErrDraining refuses runs after the core starts stopping.
var ErrDraining = errors.New("scripts is draining")

// ErrStarting reports another call preparing the same event and script.
var ErrStarting = errors.New("a run for this event is starting")

// ErrQueueFull refuses admission when the queue is full.
var ErrQueueFull = errors.New("run queue full")

// ErrNoCgroup refuses runs when control group preparation failed.
var ErrNoCgroup = errors.New("runs unavailable")

// Config supplies the catalog, process environment, bounds and runtime hooks.
type Config struct {
	Store                                               *store.Store
	Source                                              *source.Source
	Writer                                              *telemetry.Writer
	Runs, Path, Services, Cgroup, Unavailable           string
	ScriptSeconds, OutputMaxBytes, KeepDays, KeepCount  int64
	RunMemoryMaxBytes, RunPidsMax, MaxActive, MaxQueued int64
	Now                                                 func() time.Time
	ScriptAfter                                         func(time.Duration) <-chan time.Time
	Rand                                                io.Reader
}

// Request gives one run its ref, input and caller.
type Request struct {
	Ref    string
	Input  []byte
	Caller identity.Caller
	Cause  events.Cause
}

type pending struct {
	script string
	event  string
	cancel context.CancelCauseFunc
}
type queued struct {
	record  store.Run
	started time.Time
	depth   int
}
type active struct {
	mu       sync.Mutex
	process  *runner.Process
	record   store.Run
	started  time.Time
	status   string
	out, err *headWriter
	done     chan struct{}
	ended    store.Run
	failure  error
}

// Core owns accepted calls and the processes they start.
type Core struct {
	cfg              Config
	mu               sync.Mutex
	randomMu         sync.Mutex
	pruneMu          sync.Mutex
	changed          chan struct{}
	draining, halted bool
	drainContext     context.Context
	pending          map[*pending]struct{}
	active           map[string]*active
	queue            []*queued
	starting         int64
	deleting         map[string]bool
}

// New constructs a core without touching the catalog or disk.
func New(cfg Config) *Core {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ScriptAfter == nil {
		cfg.ScriptAfter = time.After
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Core{cfg: cfg, changed: make(chan struct{}), pending: make(map[*pending]struct{}), active: make(map[string]*active), deleting: make(map[string]bool)}
}

func (c *Core) signal() { close(c.changed); c.changed = make(chan struct{}) }

// Folder locates a run folder without reading the disk.
func (c *Core) Folder(r store.Run) string { return filepath.Join(c.cfg.Runs, r.Script, r.ID) }

// Gone reports whether the run folder is absent or is not a directory.
func (c *Core) Gone(r store.Run) bool { s, e := os.Lstat(c.Folder(r)); return e != nil || !s.IsDir() }
func fileSize(path string) int64 {
	s, e := os.Lstat(path)
	if e != nil || !s.Mode().IsRegular() {
		return 0
	}
	return s.Size()
}

// Sizes reports live stream sizes or the recorded ending sizes.
func (c *Core) Sizes(r store.Run) (int64, int64) {
	if r.Status != store.StatusRunning {
		return r.StdoutBytes, r.StderrBytes
	}
	return fileSize(filepath.Join(c.Folder(r), StdoutFile)), fileSize(filepath.Join(c.Folder(r), StderrFile))
}

// permissions never follows links, including links pointing outside the run.
func permissions(dir string, writable bool) error {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return e
	}
	defer func() { _ = root.Close() }()
	return fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, e := root.Lstat(name)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		bits := info.Mode().Perm()
		if writable {
			bits |= 0700
		} else {
			bits &^= 0222
		}
		return root.Chmod(name, bits)
	})
}
func removeFolder(dir string) error {
	if s, e := os.Lstat(dir); e == nil && s.IsDir() {
		e = permissions(dir, true)
		if e != nil {
			return e
		}
	}
	return os.RemoveAll(dir)
}
func later(now, started time.Time) time.Time {
	if now.Before(started) {
		return started
	}
	return now
}
func (c *Core) emit(r store.Run, name string, d time.Duration) {
	attrs := StartedAttrs(r)
	if name == "run.finished" {
		attrs = FinishedAttrs(r, d)
	}
	c.cfg.Writer.Emit(identity.NewContext(context.Background(), Caller(r)), name, attrs)
}
func seconds(n int64) time.Duration {
	if n > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}

// Run prepares and starts a run, returning before its process exits.
func (c *Core) Run(ctx context.Context, sc store.Script, req Request) (store.Run, error) {
	return c.run(ctx, sc, req, false)
}

func (c *Core) unavailable() error {
	return &refusal{"runs are unavailable: " + c.cfg.Unavailable, ErrNoCgroup}
}
func (c *Core) queueFull() error {
	return &refusal{fmt.Sprintf("the run queue is full (%d queued); try again later", c.cfg.MaxQueued), ErrQueueFull}
}

type refusal struct {
	text  string
	cause error
}

func (e *refusal) Error() string { return e.text }
func (e *refusal) Unwrap() error { return e.cause }

func (c *Core) run(ctx context.Context, sc store.Script, req Request, delivery bool) (result store.Run, failure error) {
	c.mu.Lock()
	if c.draining {
		c.mu.Unlock()
		return store.Run{}, ErrDraining
	}
	if c.cfg.Unavailable != "" {
		c.mu.Unlock()
		return store.Run{}, c.unavailable()
	}
	if c.deleting[sc.ID] {
		c.mu.Unlock()
		return store.Run{}, store.ErrNotFound
	}
	if e := context.Cause(ctx); e != nil {
		c.mu.Unlock()
		return store.Run{}, e
	}
	if req.Cause.ID != "" {
		delivered, e := c.cfg.Store.Delivered(ctx, sc.ID, req.Cause.ID)
		if e != nil {
			c.mu.Unlock()
			return store.Run{}, e
		}
		if delivered {
			c.mu.Unlock()
			return store.Run{}, store.ErrDelivered
		}
		for p := range c.pending {
			if p.script == sc.ID && p.event == req.Cause.ID {
				c.mu.Unlock()
				return store.Run{}, ErrStarting
			}
		}
	}
	if !delivery && int64(len(c.queue)) >= c.cfg.MaxQueued {
		c.mu.Unlock()
		return store.Run{}, c.queueFull()
	}
	callCtx, cancel := context.WithCancelCause(ctx)
	p := &pending{script: sc.ID, event: req.Cause.ID, cancel: cancel}
	c.pending[p] = struct{}{}
	c.mu.Unlock()
	defer func() { cancel(nil); c.mu.Lock(); delete(c.pending, p); c.signal(); c.mu.Unlock() }()
	started := c.cfg.Now()
	c.randomMu.Lock()
	id, e := store.NewRunID(c.cfg.Rand)
	c.randomMu.Unlock()
	if e != nil {
		return store.Run{}, e
	}
	ref := req.Ref
	if ref == "" {
		ref = sc.Ref
	}
	r := store.Run{ID: id, Script: sc.ID, Ref: ref, User: req.Caller.UserID, RequestID: req.Caller.RequestID, Trigger: store.TriggerManual, Status: store.StatusRunning, Started: started, Event: req.Cause.ID}
	if req.Cause.ID != "" {
		r.Trigger = store.TriggerEvent
	}
	dir := c.Folder(r)
	var proc *runner.Process
	var out, errOut *headWriter
	ownedFolder := false
	defer func() {
		if failure != nil {
			if proc != nil {
				proc.Kill()
				proc.Wait()
			}
			if out != nil {
				out.close()
			}
			if errOut != nil {
				errOut.close()
			}
			if ownedFolder {
				_ = removeFolder(dir)
			}
		}
	}()
	if e = context.Cause(callCtx); e != nil {
		return store.Run{}, e
	}
	if e = os.MkdirAll(filepath.Dir(dir), 0700); e != nil {
		return store.Run{}, e
	}
	if e = os.Mkdir(dir, 0700); e != nil {
		return store.Run{}, e
	}
	ownedFolder = true
	input := req.Input
	if len(input) == 0 {
		input = []byte("{}")
	}
	if e = os.WriteFile(filepath.Join(dir, InputFile), input, 0600); e != nil {
		return store.Run{}, e
	}
	sha, runErr := c.cfg.Source.Resolve(callCtx, sc.Repo, ref)
	r.SHA = sha
	if runErr == nil {
		runErr = c.cfg.Source.Archive(callCtx, sc.Repo, sha, filepath.Join(dir, TreeDir))
		if _, e = os.Lstat(filepath.Join(dir, TreeDir)); e == nil {
			if e = permissions(filepath.Join(dir, TreeDir), false); e != nil && runErr == nil {
				runErr = e
			}
		}
	}
	if e = context.Cause(callCtx); e != nil {
		return store.Run{}, e
	}
	if errors.Is(runErr, limits.ErrHalted) {
		return store.Run{}, runErr
	}
	reason := source.Reason(runErr)
	if runErr == nil {
		if e = os.Mkdir(filepath.Join(dir, OutDir), 0700); e != nil {
			return store.Run{}, e
		}
	}
	// Admission and slot acquisition share one boundary.
	c.mu.Lock()
	defer c.mu.Unlock()
	if e = context.Cause(callCtx); e != nil {
		return store.Run{}, e
	}
	if c.halted || (c.drainContext != nil && c.drainContext.Err() != nil) {
		return store.Run{}, limits.ErrHalted
	}
	if c.deleting[sc.ID] {
		return store.Run{}, store.ErrNotFound
	}
	var timer <-chan time.Time
	if runErr == nil {
		if int64(len(c.active))+c.starting >= c.cfg.MaxActive || len(c.queue) > 0 {
			if c.draining {
				runErr = errors.New("queue abandoned")
				reason = store.ReasonQueueAbandoned
			} else {
				r.Status = store.StatusQueued
			}
		} else {
			c.starting++
			c.mu.Unlock()
			proc, out, errOut, runErr = c.launch(callCtx, r, req.Cause.Depth)
			if proc != nil {
				timer = c.cfg.ScriptAfter(seconds(c.cfg.ScriptSeconds))
			}
			c.mu.Lock()
			c.starting--
			if e = context.Cause(callCtx); e != nil {
				return store.Run{}, e
			}
			if c.halted || (c.drainContext != nil && c.drainContext.Err() != nil) {
				return store.Run{}, limits.ErrHalted
			}
			if c.deleting[sc.ID] {
				return store.Run{}, store.ErrNotFound
			}
			if runErr != nil {
				reason = store.ReasonStartFailed
			}
		}
	}
	finished := time.Time{}
	if runErr != nil {
		r.Status = store.StatusFailed
		r.Reason = reason
		var ge *git.Error
		if errors.As(runErr, &ge) && ge.Stderr != "" {
			b := []byte(ge.Stderr)
			if int64(len(b)) > c.cfg.OutputMaxBytes {
				b = b[:c.cfg.OutputMaxBytes]
				r.Truncated = true
			}
			if e = os.WriteFile(filepath.Join(dir, StderrFile), b, 0600); e != nil {
				return store.Run{}, e
			}
			r.StderrBytes = int64(len(b))
		}
		finished = c.cfg.Now()
		r.Finished = later(finished, started)
	}
	r, e = c.cfg.Store.AddRun(callCtx, r)
	if e != nil {
		return store.Run{}, e
	}
	switch {
	case proc != nil:
		a := &active{process: proc, record: r, started: started, out: out, err: errOut, done: make(chan struct{})}
		c.active[id] = a
		c.emit(r, "run.started", 0)
		go c.watch(context.WithoutCancel(ctx), a, timer)
	case r.Status == store.StatusQueued:
		c.queue = append(c.queue, &queued{record: r, started: started, depth: req.Cause.Depth})
	default:
		_ = c.Prune(context.Background())
		c.emit(r, "run.finished", finished.Sub(started))
	}
	return r, nil
}

// launch makes streams only for a process that can start.
func (c *Core) launch(ctx context.Context, r store.Run, depth int) (*runner.Process, *headWriter, *headWriter, error) {
	dir := c.Folder(r)
	out, e := newHead(filepath.Join(dir, StdoutFile), c.cfg.OutputMaxBytes)
	if e != nil {
		return nil, nil, nil, e
	}
	errOut, e := newHead(filepath.Join(dir, StderrFile), c.cfg.OutputMaxBytes)
	if e != nil {
		out.close()
		_ = os.Remove(filepath.Join(dir, StdoutFile))
		return nil, nil, nil, e
	}
	abs, e := filepath.Abs(dir)
	if e != nil {
		out.close()
		errOut.close()
		return nil, nil, nil, e
	}
	env := []string{"PATH=" + c.cfg.Path, "HOME=" + abs, "LANG=C.UTF-8", "IKIGENBA_RUN_ID=" + r.ID, "IKIGENBA_SCRIPT=" + r.Script, "IKIGENBA_SHA=" + r.SHA, "IKIGENBA_RUN_DIR=" + abs, "IKIGENBA_OUT_DIR=" + filepath.Join(abs, OutDir), "IKIGENBA_INPUT=" + filepath.Join(abs, InputFile), "IKIGENBA_USER_ID=" + r.User, "IKIGENBA_REQUEST_ID=" + r.RequestID, "IKIGENBA_EVENT_ID=" + r.Event, "IKIGENBA_EVENT_DEPTH=" + strconv.Itoa(depth), "IKIGENBA_SERVICES=" + c.cfg.Services}
	group := ""
	if c.cfg.Cgroup != "" {
		group = filepath.Join(c.cfg.Cgroup, r.ID)
	}
	proc, e := runner.Start(ctx, runner.Spec{Dir: filepath.Join(abs, TreeDir), Env: env, Stdout: out, Stderr: errOut, Cgroup: group, MemoryMax: c.cfg.RunMemoryMaxBytes, PidsMax: c.cfg.RunPidsMax})
	if e != nil {
		out.close()
		errOut.close()
		_ = os.Remove(filepath.Join(dir, StdoutFile))
		_ = os.Remove(filepath.Join(dir, StderrFile))
		return nil, nil, nil, e
	}
	return proc, out, errOut, nil
}

// finishQueued runs under the core lock, excluding promotion and cancellation.
func (c *Core) finishQueued(ctx context.Context, q *queued, status, reason string, prune bool) (store.Run, error) {
	now := c.cfg.Now()
	r, e := c.cfg.Store.FinishRun(ctx, q.record.ID, store.Ending{Status: status, Reason: reason, Finished: later(now, q.record.Started)})
	if e != nil {
		return store.Run{}, e
	}
	if prune {
		_ = c.Prune(ctx)
	}
	c.emit(r, "run.finished", now.Sub(q.started))
	return r, nil
}

// promote runs under the core lock after the released slot's finishing event.
func (c *Core) promote() {
	for !c.draining && len(c.queue) > 0 && int64(len(c.active))+c.starting < c.cfg.MaxActive {
		q := c.queue[0]
		c.queue = c.queue[1:]
		proc, out, errOut, e := c.launch(context.Background(), q.record, q.depth)
		if e != nil {
			_, _ = c.finishQueued(context.Background(), q, store.StatusFailed, store.ReasonStartFailed, true)
			continue
		}
		timer := c.cfg.ScriptAfter(seconds(c.cfg.ScriptSeconds))
		r, e := c.cfg.Store.StartRun(context.Background(), q.record.ID)
		if e != nil {
			proc.Kill()
			proc.Wait()
			out.close()
			errOut.close()
			continue
		}
		a := &active{process: proc, record: r, started: q.started, out: out, err: errOut, done: make(chan struct{})}
		c.active[r.ID] = a
		c.emit(r, "run.started", 0)
		go c.watch(context.Background(), a, timer)
	}
	c.signal()
}

type headWriter struct {
	mu     sync.Mutex
	f      *os.File
	max, n int64
	cut    bool
}

func newHead(path string, bound int64) (*headWriter, error) {
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	f, e := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	return &headWriter{f: f, max: bound}, nil
}
func (w *headWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(b)
	keep := int64(n)
	if keep > w.max-w.n {
		keep = w.max - w.n
		w.cut = true
	}
	if keep > 0 {
		k, e := w.f.Write(b[:keep])
		w.n += int64(k)
		if e != nil {
			return n, nil
		}
	}
	return n, nil
}
func (w *headWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
}
func (c *Core) kill(a *active, status string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status != "" {
		return false
	}
	if !a.process.Kill() {
		return false
	}
	a.status = status
	return true
}
func (c *Core) watch(ctx context.Context, a *active, timer <-chan time.Time) {
	exited := make(chan int, 1)
	go func() { exited <- a.process.Wait() }()
	var code int
	select {
	case code = <-exited:
	case <-timer:
		c.kill(a, store.StatusTimedOut)
		code = <-exited
	}
	a.mu.Lock()
	status := a.status
	if status == "" {
		status = store.StatusExited
	} else {
		code = 0
	}
	a.out.close()
	a.err.close()
	now := c.cfg.Now()
	e := store.Ending{Status: status, ExitCode: code, Finished: later(now, a.record.Started), StdoutBytes: a.out.n, StderrBytes: a.err.n, Truncated: a.out.cut || a.err.cut}
	a.ended, a.failure = c.cfg.Store.FinishRun(ctx, a.record.ID, e)
	if a.failure == nil {
		_ = c.Prune(ctx)
		c.emit(a.ended, "run.finished", now.Sub(a.started))
	}
	a.mu.Unlock()
	c.mu.Lock()
	delete(c.active, a.record.ID)
	c.promote()
	close(a.done)
	c.signal()
	c.mu.Unlock()
}

// Cancel kills a running run and waits for its ending event.
func (c *Core) Cancel(ctx context.Context, id string) (store.Run, error) {
	c.mu.Lock()
	r, e := c.cfg.Store.RunByID(ctx, id)
	if e != nil {
		c.mu.Unlock()
		return store.Run{}, e
	}
	if r.Status != store.StatusRunning && r.Status != store.StatusQueued {
		c.mu.Unlock()
		return store.Run{}, store.ErrEnded
	}
	if r.Status == store.StatusQueued {
		defer c.mu.Unlock()
		q := &queued{record: r, started: r.Started}
		for i, v := range c.queue {
			if v.record.ID == id {
				q = v
				c.queue = append(c.queue[:i], c.queue[i+1:]...)
				break
			}
		}
		result, e := c.finishQueued(ctx, q, store.StatusKilled, "", true)
		c.signal()
		return result, e
	}
	a := c.active[id]
	c.mu.Unlock()
	if a == nil {
		return c.settle(ctx, r, true)
	}
	killed := c.kill(a, store.StatusKilled)
	<-a.done
	if a.failure != nil {
		return store.Run{}, a.failure
	}
	if !killed {
		return store.Run{}, store.ErrEnded
	}
	return a.ended, nil
}
func (c *Core) settle(ctx context.Context, r store.Run, prune bool) (store.Run, error) {
	now := c.cfg.Now()
	out, errOut := c.Sizes(r)
	ended, e := c.cfg.Store.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusKilled, Finished: later(now, r.Started), StdoutBytes: out, StderrBytes: errOut})
	if e != nil {
		return store.Run{}, e
	}
	if prune {
		_ = c.Prune(context.WithoutCancel(ctx))
	}
	c.emit(ended, "run.finished", now.Sub(r.Started))
	return ended, nil
}

// Delete ends a script's runs, removes its folders and deletes its catalog entry.
func (c *Core) Delete(ctx context.Context, script string) error {
	c.mu.Lock()
	c.deleting[script] = true
	for i := 0; i < len(c.queue); {
		q := c.queue[i]
		if q.record.Script != script {
			i++
			continue
		}
		c.queue = append(c.queue[:i], c.queue[i+1:]...)
		if _, e := c.finishQueued(ctx, q, store.StatusKilled, "", true); e != nil {
			c.mu.Unlock()
			return e
		}
	}
	for p := range c.pending {
		if p.script == script {
			p.cancel(store.ErrNotFound)
		}
	}
	for {
		busy := false
		for p := range c.pending {
			if p.script == script {
				busy = true
				break
			}
		}
		if !busy {
			break
		}
		changed := c.changed
		c.mu.Unlock()
		<-changed
		c.mu.Lock()
	}

	c.mu.Unlock()
	rs, e := c.cfg.Store.Runs(ctx, script)
	if e != nil {
		return e
	}
	for _, r := range rs {
		if r.Status == store.StatusRunning || r.Status == store.StatusQueued {
			_, e = c.Cancel(ctx, r.ID)
			if e != nil && !errors.Is(e, store.ErrEnded) {
				return e
			}
		}
	}
	if e = removeFolder(filepath.Join(c.cfg.Runs, script)); e != nil {
		return e
	}
	return c.cfg.Store.Delete(ctx, script)
}

// Recover settles records left running by a previous service process.
func (c *Core) Recover(ctx context.Context) error {
	if e := os.MkdirAll(c.cfg.Runs, 0700); e != nil {
		return e
	}
	rs, e := c.cfg.Store.Running(ctx)
	if e != nil {
		return e
	}
	for _, r := range rs {
		if _, e = c.settle(ctx, r, false); e != nil {
			return e
		}
	}
	rs, e = c.cfg.Store.Queued(ctx)
	if e != nil {
		return e
	}
	for _, r := range rs {
		if _, e = c.finishQueued(ctx, &queued{record: r, started: r.Started}, store.StatusFailed, store.ReasonQueueAbandoned, false); e != nil {
			return e
		}
	}
	return nil
}

// Prune removes past-keeping folders before their catalog records.
func (c *Core) Prune(ctx context.Context) error {
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()
	rs, e := c.cfg.Store.PastKeeping(ctx, c.cfg.Now(), c.cfg.KeepDays, c.cfg.KeepCount)
	if e != nil {
		return e
	}
	for _, r := range rs {
		if e = removeFolder(c.Folder(r)); e != nil {
			continue
		}
		if e = c.cfg.Store.DeleteRun(ctx, r.ID); e != nil {
			return e
		}
	}
	return nil
}

// Drain refuses new work and waits, killing remaining work at its deadline.
func (c *Core) Drain(ctx context.Context) {
	c.mu.Lock()
	c.draining = true
	c.drainContext = ctx
	for _, q := range c.queue {
		_, _ = c.finishQueued(context.Background(), q, store.StatusFailed, store.ReasonQueueAbandoned, true)
	}
	c.queue = nil
	c.signal()
	c.mu.Unlock()
	deadline := ctx.Done()
	for {
		c.mu.Lock()
		if len(c.pending) == 0 && len(c.active) == 0 {
			c.mu.Unlock()
			return
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-deadline:
			c.mu.Lock()
			c.halted = true
			for p := range c.pending {
				p.cancel(limits.ErrHalted)
			}
			as := make([]*active, 0, len(c.active))
			for _, a := range c.active {
				as = append(as, a)
			}
			c.mu.Unlock()
			for _, a := range as {
				c.kill(a, store.StatusKilled)
			}
			deadline = nil
		}
	}
}
