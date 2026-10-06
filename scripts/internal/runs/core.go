// Package runs owns run folders, process lifetimes and catalog transitions.
package runs

import (
	"context"
	"crypto/rand"
	"errors"
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

// Config supplies the catalog, process environment, bounds and runtime hooks.
type Config struct {
	Store                                              *store.Store
	Source                                             *source.Source
	Writer                                             *telemetry.Writer
	Runs, Path, Services                               string
	ScriptSeconds, OutputMaxBytes, KeepDays, KeepCount int64
	Now                                                func() time.Time
	ScriptAfter                                        func(time.Duration) <-chan time.Time
	Rand                                               io.Reader
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
func (c *Core) Run(ctx context.Context, sc store.Script, req Request) (result store.Run, failure error) {
	c.mu.Lock()
	if c.draining {
		c.mu.Unlock()
		return store.Run{}, ErrDraining
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
		out, e = newHead(filepath.Join(dir, StdoutFile), c.cfg.OutputMaxBytes)
		if e != nil {
			return store.Run{}, e
		}
		errOut, e = newHead(filepath.Join(dir, StderrFile), c.cfg.OutputMaxBytes)
		if e != nil {
			return store.Run{}, e
		}
		abs, e := filepath.Abs(dir)
		if e != nil {
			return store.Run{}, e
		}
		env := []string{"PATH=" + c.cfg.Path, "HOME=" + abs, "LANG=C.UTF-8", "IKIGENBA_RUN_ID=" + id, "IKIGENBA_SCRIPT=" + sc.ID, "IKIGENBA_SHA=" + sha, "IKIGENBA_RUN_DIR=" + abs, "IKIGENBA_OUT_DIR=" + filepath.Join(abs, OutDir), "IKIGENBA_INPUT=" + filepath.Join(abs, InputFile), "IKIGENBA_USER_ID=" + req.Caller.UserID, "IKIGENBA_REQUEST_ID=" + req.Caller.RequestID, "IKIGENBA_EVENT_ID=" + req.Cause.ID, "IKIGENBA_EVENT_DEPTH=" + strconv.Itoa(req.Cause.Depth), "IKIGENBA_SERVICES=" + c.cfg.Services}
		proc, runErr = runner.Start(callCtx, runner.Spec{Dir: filepath.Join(abs, TreeDir), Env: env, Stdout: out, Stderr: errOut})
		if runErr != nil {
			reason = store.ReasonStartFailed
			out.close()
			errOut.close()
			out = nil
			errOut = nil
			if e = os.Remove(filepath.Join(dir, StdoutFile)); e != nil {
				return store.Run{}, e
			}
			if e = os.Remove(filepath.Join(dir, StderrFile)); e != nil {
				return store.Run{}, e
			}
		}
	}
	var timer <-chan time.Time
	if proc != nil {
		timer = c.cfg.ScriptAfter(seconds(c.cfg.ScriptSeconds))
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
	// Admission, deletion and the drain deadline share this catalog boundary.
	c.mu.Lock()
	if e = context.Cause(callCtx); e != nil {
		c.mu.Unlock()
		return store.Run{}, e
	}
	if c.halted || (c.drainContext != nil && c.drainContext.Err() != nil) {
		c.mu.Unlock()
		return store.Run{}, limits.ErrHalted
	}
	if c.deleting[sc.ID] {
		c.mu.Unlock()
		return store.Run{}, store.ErrNotFound
	}
	r, e = c.cfg.Store.AddRun(callCtx, r)
	if e != nil {
		c.mu.Unlock()
		return store.Run{}, e
	}
	if proc != nil {
		a := &active{process: proc, record: r, started: started, out: out, err: errOut, done: make(chan struct{})}
		c.active[id] = a
		c.emit(r, "run.started", 0)
		c.mu.Unlock()
		go c.watch(context.WithoutCancel(ctx), a, timer)
	} else {
		c.mu.Unlock()
		_ = c.Prune(context.Background())
		c.emit(r, "run.finished", finished.Sub(started))
	}
	return r, nil
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
	close(a.done)
	c.signal()
	c.mu.Unlock()
}

// Cancel kills a running run and waits for its ending event.
func (c *Core) Cancel(ctx context.Context, id string) (store.Run, error) {
	r, e := c.cfg.Store.RunByID(ctx, id)
	if e != nil {
		return store.Run{}, e
	}
	if r.Status != store.StatusRunning {
		return store.Run{}, store.ErrEnded
	}
	c.mu.Lock()
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
		if r.Status == store.StatusRunning {
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
