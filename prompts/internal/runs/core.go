// Package runs owns run admission, execution and retention.
package runs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/runner"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// Run folder entry names are shared with run readers.
const (
	InputFile      = "input.json"
	StdoutFile     = "stdout"
	StderrFile     = "stderr"
	TranscriptFile = "transcript.jsonl"
	WorkDir        = "work"
)

// Admission copy is shared with the tools and delivery handler.
const (
	Stopping  = "Prompts is stopping."
	Starting  = "This event's run is starting."
	NoEventID = "An event id is required."
	NoRuns    = "Runs are unavailable: %s"
	QueueFull = "The run queue is full (%d)."
)

// Admission sentinels distinguish refusals from interrupted calls.
var (
	ErrDraining  = errors.New("runs draining")
	ErrStarting  = errors.New("event run starting")
	ErrQueueFull = errors.New("run queue full")
	ErrNoCgroup  = errors.New("runs unavailable")
	ErrCutOff    = errors.New("run cut off")
)

type refusal struct {
	text string
	kind error
}

func (e refusal) Error() string { return e.text }
func (e refusal) Unwrap() error { return e.kind }

// Config supplies the catalog, execution bounds and runtime hooks.
type Config struct {
	Store                                                                                                                 *store.Store
	Writer                                                                                                                *telemetry.Writer
	Runs, Path, Services, Cgroup, Unavailable                                                                             string
	Keys                                                                                                                  map[agentkit.Host]string
	BaseURL                                                                                                               string
	PromptSeconds, OutputMaxBytes, MaxToolCalls, KeepDays, KeepCount, RunMemoryMaxBytes, RunPidsMax, MaxActive, MaxQueued int64
	Now                                                                                                                   func() time.Time
	ScriptAfter                                                                                                           func(time.Duration) <-chan time.Time
	Rand                                                                                                                  io.Reader
}

// Request carries the run input and its caller and event identities.
type Request struct {
	Input  []byte
	Caller identity.Caller
	Cause  events.Cause
}
type liveRun struct {
	record   store.Run
	prompt   store.Prompt
	req      Request
	began    time.Time
	process  *runner.Process
	out, err *headWriter
	done     chan struct{}
	status   string
	endErr   error
	timer    <-chan time.Time
}

// Core coordinates admission, execution and retention of runs.
type Core struct {
	cfg                Config
	mu                 sync.Mutex
	randMu             sync.Mutex
	active             map[string]*liveRun
	queue              []*liveRun
	starting           map[[2]string]bool
	deleting           map[string]bool
	draining, deadline bool
	calls              int
	changed            chan struct{}
}

// New constructs a core and fills the optional runtime hooks.
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
	keys := make(map[agentkit.Host]string, len(cfg.Keys))
	for k, v := range cfg.Keys {
		keys[k] = v
	}
	cfg.Keys = keys
	return &Core{cfg: cfg, active: map[string]*liveRun{}, starting: map[[2]string]bool{}, deleting: map[string]bool{}, changed: make(chan struct{})}
}
func (c *Core) signal() { close(c.changed); c.changed = make(chan struct{}) }

// Folder locates a run without accessing the filesystem.
func (c *Core) Folder(r store.Run) string { return filepath.Join(c.cfg.Runs, r.Prompt, r.ID) }

// Gone reports whether the run folder has disappeared.
func (c *Core) Gone(r store.Run) bool { f, e := os.Lstat(c.Folder(r)); return e != nil || !f.IsDir() }
func regularSize(path string) int64 {
	f, e := os.Lstat(path)
	if e != nil || !f.Mode().IsRegular() {
		return 0
	}
	return f.Size()
}

// Sizes returns persisted stream sizes, or live regular-file sizes.
func (c *Core) Sizes(r store.Run) (int64, int64) {
	if r.Status != store.StatusRunning {
		return r.StdoutBytes, r.StderrBytes
	}
	return regularSize(filepath.Join(c.Folder(r), StdoutFile)), regularSize(filepath.Join(c.Folder(r), StderrFile))
}

// StartedAttrs forms the attributes of a run.started event.
func StartedAttrs(r store.Run) telemetry.Attrs {
	return telemetry.Attrs{"prompt_run": r.ID, "prompt": r.Prompt, "model": r.Model, "trigger": r.Trigger}
}

// FinishedAttrs forms the attributes of a run.finished event.
func FinishedAttrs(r store.Run, d time.Duration) telemetry.Attrs {
	us := d.Microseconds()
	if us < 0 {
		us = 0
	}
	a := telemetry.Attrs{"prompt_run": r.ID, "status": r.Status, "duration_us": us, "truncated": r.Truncated(), "calls": r.Usage.Calls, "tool_calls": r.Usage.ToolCalls, "input_tokens": r.Usage.InputTokens, "output_tokens": r.Usage.OutputTokens, "cost_nanos": r.Usage.CostNanos}
	if r.Status == store.StatusExited {
		a["exit_code"] = int64(r.ExitCode)
	}
	if r.Status == store.StatusFailed {
		a["reason"] = r.Reason
	}
	return a
}

// Caller restores the identity that caused the run.
func Caller(r store.Run) identity.Caller {
	return identity.Caller{UserID: r.User, RequestID: r.RequestID}
}
func (c *Core) emit(r store.Run, name string, d time.Duration) {
	a := StartedAttrs(r)
	if name == "run.finished" {
		a = FinishedAttrs(r, d)
	}
	c.cfg.Writer.Emit(identity.NewContext(context.Background(), Caller(r)), name, a)
}
func (c *Core) cut(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if c.deadline {
		return ErrCutOff
	}
	return nil
}

// Run records a run and starts or queues it without waiting for completion.
func (c *Core) Run(ctx context.Context, p store.Prompt, req Request) (store.Run, error) {
	return c.run(ctx, p, req, false)
}
func (c *Core) run(ctx context.Context, p store.Prompt, req Request, bypassQueue bool) (store.Run, error) {
	c.mu.Lock()
	if c.draining {
		c.mu.Unlock()
		return store.Run{}, refusal{Stopping, ErrDraining}
	}
	if c.cfg.Unavailable != "" {
		c.mu.Unlock()
		return store.Run{}, refusal{fmt.Sprintf(NoRuns, c.cfg.Unavailable), ErrNoCgroup}
	}
	if e := c.cut(ctx); e != nil {
		c.mu.Unlock()
		return store.Run{}, e
	}
	pair := [2]string{p.ID, req.Cause.ID}
	if req.Cause.ID != "" {
		yes, e := c.cfg.Store.Delivered(ctx, p.ID, req.Cause.ID)
		if e != nil {
			if cause := c.cut(ctx); cause != nil {
				e = cause
			}
			c.mu.Unlock()
			return store.Run{}, e
		}
		if yes {
			c.mu.Unlock()
			return store.Run{}, store.ErrDelivered
		}
		if c.starting[pair] {
			c.mu.Unlock()
			return store.Run{}, refusal{Starting, ErrStarting}
		}
	}
	if !bypassQueue && int64(len(c.queue)) >= c.cfg.MaxQueued {
		c.mu.Unlock()
		return store.Run{}, refusal{fmt.Sprintf(QueueFull, c.cfg.MaxQueued), ErrQueueFull}
	}
	if req.Cause.ID != "" {
		c.starting[pair] = true
	}
	c.calls++
	c.signal()
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.starting, pair); c.calls--; c.signal(); c.mu.Unlock() }()
	began := c.cfg.Now()
	c.randMu.Lock()
	id, e := store.NewRunID(c.cfg.Rand)
	c.randMu.Unlock()
	if e != nil {
		c.mu.Lock()
		if cause := c.cut(ctx); cause != nil {
			e = cause
		}
		c.mu.Unlock()
		return store.Run{}, e
	}
	r := store.Run{ID: id, Prompt: p.ID, Model: p.Model, User: req.Caller.UserID, RequestID: req.Caller.RequestID, Trigger: store.TriggerManual, Started: began.UTC().Truncate(time.Second)}
	if req.Cause.ID != "" {
		r.Trigger = store.TriggerEvent
		r.Event = req.Cause.ID
	}
	// Snapshot mutable slices before the run can wait in the queue.
	p.Tools = append([]string(nil), p.Tools...)
	p.Schema = append(json.RawMessage(nil), p.Schema...)
	req.Input = append([]byte(nil), req.Input...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e = c.cut(ctx); e != nil {
		return store.Run{}, e
	}
	if c.deleting[p.ID] {
		return store.Run{}, store.ErrNotFound
	}
	folder := c.Folder(r)
	// Mkdir refuses an existing run id instead of overwriting another run.
	if e = os.MkdirAll(filepath.Dir(folder), 0700); e != nil {
		return store.Run{}, e
	}
	if e = os.Mkdir(folder, 0700); e != nil {
		return store.Run{}, e
	}
	cleanup := func() { _ = removeTree(folder) }
	input := req.Input
	if len(input) == 0 {
		input = []byte("{}")
	}
	if e = os.WriteFile(filepath.Join(folder, InputFile), input, 0600); e != nil {
		cleanup()
		return store.Run{}, e
	}
	if e = os.Mkdir(filepath.Join(folder, WorkDir), 0700); e != nil {
		cleanup()
		return store.Run{}, e
	}
	finishRead := began
	l := &liveRun{record: r, prompt: p, req: req, began: began, done: make(chan struct{})}
	if e = c.cut(ctx); e != nil {
		cleanup()
		return store.Run{}, e
	}
	if int64(len(c.active)) >= c.cfg.MaxActive || len(c.queue) > 0 {
		if c.draining {
			r.Status = store.StatusFailed
			r.Reason = store.ReasonQueueAbandoned
			finishRead = c.cfg.Now()
			r.Finished = maxTime(finishRead, r.Started)
		} else {
			r.Status = store.StatusQueued
		}
	} else {
		r.Status = store.StatusRunning
		if e = c.launch(ctx, l, input); e != nil {
			if ce := c.cut(ctx); ce != nil {
				cleanup()
				return store.Run{}, ce
			}
			r.Status = store.StatusFailed
			r.Reason = store.ReasonStartFailed
			finishRead = c.cfg.Now()
			r.Finished = maxTime(finishRead, r.Started)
		}
	}
	if e = c.cut(ctx); e != nil {
		if l.process != nil {
			l.process.Kill()
			l.process.Wait()
			l.out.close()
			l.err.close()
		}
		cleanup()
		return store.Run{}, e
	}
	r, e = c.cfg.Store.AddRun(ctx, r)
	if e != nil {
		if cause := c.cut(ctx); cause != nil {
			e = cause
		}
		if l.process != nil {
			l.process.Kill()
			l.process.Wait()
			l.out.close()
			l.err.close()
		}
		cleanup()
		return store.Run{}, e
	}
	l.record = r
	switch r.Status {
	case store.StatusRunning:
		c.active[r.ID] = l
		c.emit(r, "run.started", 0)
		go c.watch(context.WithoutCancel(ctx), l)
	case store.StatusQueued:
		c.queue = append(c.queue, l)
	case store.StatusFailed:
		c.report(r, began, finishRead)
	}
	c.signal()
	return r, nil
}
func maxTime(now, started time.Time) time.Time {
	if now.Before(started) {
		return started
	}
	return now
}
func seconds(n int64) time.Duration {
	if n > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}

type headWriter struct {
	file               *os.File
	limit, kept, total int64
}

func (w *headWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.total += int64(n)
	left := w.limit - w.kept
	if left > 0 {
		q := p
		if int64(len(q)) > left {
			q = q[:int(left)]
		}
		k, _ := w.file.Write(q)
		w.kept += int64(k)
	}
	return n, nil
}
func (w *headWriter) close() { _ = w.file.Close() }
func createStream(folder, name string) (*os.File, error) {
	root, e := os.OpenRoot(folder)
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
}
func (c *Core) launch(ctx context.Context, l *liveRun, input []byte) error {
	folder, e := filepath.Abs(c.Folder(l.record))
	if e != nil {
		return e
	}
	p, req, r := l.prompt, l.req, l.record
	key := ""
	if o, e := agent.Offering(p.Model); e == nil {
		key = c.cfg.Keys[o.Host]
	}
	limit := c.cfg.MaxToolCalls
	if limit > int64(math.MaxInt) {
		limit = int64(math.MaxInt)
	}
	stdin, e := json.Marshal(agent.Spec{Model: p.Model, Key: key, System: p.System, Prompt: p.Prompt, Tools: p.Tools, Schema: p.Schema, Input: input, MaxToolCalls: int(limit), UserID: req.Caller.UserID, Email: req.Caller.Email, RequestID: req.Caller.RequestID, EventID: req.Cause.ID, EventDepth: req.Cause.Depth, BaseURL: c.cfg.BaseURL})
	if e != nil {
		return e
	}
	out, e := createStream(folder, StdoutFile)
	if e != nil {
		return e
	}
	errFile, e := createStream(folder, StderrFile)
	if e != nil {
		_ = out.Close()
		_ = os.Remove(filepath.Join(folder, StdoutFile))
		return e
	}
	l.out = &headWriter{file: out, limit: c.cfg.OutputMaxBytes}
	l.err = &headWriter{file: errFile, limit: c.cfg.OutputMaxBytes}
	env := []string{"PATH=" + c.cfg.Path, "HOME=" + folder, "LANG=C.UTF-8", "IKIGENBA_RUN_ID=" + r.ID, "IKIGENBA_PROMPT=" + p.ID, "IKIGENBA_RUN_DIR=" + folder, "IKIGENBA_WORK_DIR=" + filepath.Join(folder, WorkDir), "IKIGENBA_INPUT=" + filepath.Join(folder, InputFile), "IKIGENBA_USER_ID=" + req.Caller.UserID, "IKIGENBA_REQUEST_ID=" + req.Caller.RequestID, "IKIGENBA_EVENT_ID=" + req.Cause.ID, "IKIGENBA_EVENT_DEPTH=" + strconv.Itoa(req.Cause.Depth), "IKIGENBA_SERVICES=" + c.cfg.Services}
	group := ""
	if c.cfg.Cgroup != "" {
		group = filepath.Join(c.cfg.Cgroup, r.ID)
	}
	l.process, e = runner.Start(ctx, runner.Spec{Dir: folder, Env: env, Stdin: stdin, Stdout: l.out, Stderr: l.err, Cgroup: group, MemoryMax: c.cfg.RunMemoryMaxBytes, PidsMax: c.cfg.RunPidsMax})
	if e == nil {
		l.timer = c.cfg.ScriptAfter(seconds(c.cfg.PromptSeconds))
	}
	if e != nil {
		l.out.close()
		l.err.close()
		_ = os.Remove(filepath.Join(folder, StdoutFile))
		_ = os.Remove(filepath.Join(folder, StderrFile))
	}
	return e
}
func (c *Core) watch(ctx context.Context, l *liveRun) {
	timer := l.timer
	exited := make(chan int, 1)
	go func() { exited <- l.process.Wait() }()
	var code int
	select {
	case code = <-exited:
	case <-timer:
		c.mu.Lock()
		if l.status == "" && l.process.Kill() {
			l.status = store.StatusTimedOut
		}
		c.mu.Unlock()
		code = <-exited
	}
	l.out.close()
	l.err.close()
	c.mu.Lock()
	defer c.mu.Unlock()
	status := l.status
	if status == "" {
		status = store.StatusExited
	}
	if status != store.StatusExited {
		code = 0
	}
	now := c.cfg.Now()
	r, e := c.cfg.Store.FinishRun(ctx, l.record.ID, store.Ending{Status: status, ExitCode: code, Finished: maxTime(now, l.record.Started), StdoutBytes: l.out.kept, StderrBytes: l.err.kept, StdoutTruncated: l.out.total > c.cfg.OutputMaxBytes, StderrTruncated: l.err.total > c.cfg.OutputMaxBytes, Usage: readUsage(filepath.Join(c.Folder(l.record), TranscriptFile))})
	l.endErr = e
	if e == nil {
		l.record = r
		c.report(r, l.began, now)
	}
	delete(c.active, l.record.ID)
	close(l.done)
	c.signal()
	c.advance()
}
func (c *Core) report(r store.Run, began, now time.Time) {
	_ = c.Prune(context.Background())
	c.emit(r, "run.finished", now.Sub(began))
}
func (c *Core) advance() {
	for !c.draining && len(c.queue) > 0 && int64(len(c.active)) < c.cfg.MaxActive {
		l := c.queue[0]
		c.queue = c.queue[1:]
		input := l.req.Input
		if len(input) == 0 {
			input = []byte("{}")
		}
		if e := c.launch(context.Background(), l, input); e != nil {
			_ = c.endQueued(l, store.StatusFailed, store.ReasonStartFailed)
			continue
		}
		r, e := c.cfg.Store.StartRun(context.Background(), l.record.ID)
		if e != nil {
			l.process.Kill()
			l.process.Wait()
			l.out.close()
			l.err.close()
			close(l.done)
			continue
		}
		l.record = r
		c.active[r.ID] = l
		c.emit(r, "run.started", 0)
		go c.watch(context.Background(), l)
	}
	c.signal()
}
func (c *Core) endQueued(l *liveRun, status, reason string) error {
	now := c.cfg.Now()
	r, e := c.cfg.Store.FinishRun(context.Background(), l.record.ID, store.Ending{Status: status, Reason: reason, Finished: maxTime(now, l.record.Started)})
	if e == nil {
		l.record = r
		c.report(r, l.began, now)
	}
	close(l.done)
	return e
}

// Cancel ends an active or queued run and returns its ending.
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
	for i, l := range c.queue {
		if l.record.ID == id {
			c.queue = append(c.queue[:i], c.queue[i+1:]...)
			e = c.endQueued(l, store.StatusKilled, "")
			c.signal()
			c.mu.Unlock()
			if e != nil {
				return store.Run{}, e
			}
			return l.record, nil
		}
	}
	if l := c.active[id]; l != nil {
		won := l.status == "" && l.process.Kill()
		if won {
			l.status = store.StatusKilled
		}
		done := l.done
		c.mu.Unlock()
		<-done
		if !won {
			return store.Run{}, store.ErrEnded
		}
		if l.endErr != nil {
			return store.Run{}, l.endErr
		}
		return l.record, nil
	}
	now := c.cfg.Now()
	out, errBytes := c.Sizes(r)
	reason := ""
	status := store.StatusKilled
	usage := store.Usage{}
	if r.Status == store.StatusRunning {
		usage = readUsage(filepath.Join(c.Folder(r), TranscriptFile))
	} else {
		out = 0
		errBytes = 0
	}
	ended, e := c.cfg.Store.FinishRun(ctx, id, store.Ending{Status: status, Reason: reason, Finished: maxTime(now, r.Started), StdoutBytes: out, StderrBytes: errBytes, Usage: usage})
	if e == nil {
		c.report(ended, r.Started, now)
	}
	c.mu.Unlock()
	return ended, e
}

// Delete ends a prompt's runs and removes its folders and catalog entry.
func (c *Core) Delete(ctx context.Context, prompt string) error {
	c.mu.Lock()
	if _, _, e := c.cfg.Store.Update(ctx, prompt, store.Change{}); e != nil {
		c.mu.Unlock()
		return e
	}
	rs, e := c.cfg.Store.Runs(ctx, prompt)
	if e != nil {
		c.mu.Unlock()
		return e
	}
	c.deleting[prompt] = true
	kept := c.queue[:0]
	for _, l := range c.queue {
		if l.record.Prompt == prompt {
			if e = c.endQueued(l, store.StatusKilled, ""); e != nil {
				c.mu.Unlock()
				return e
			}
		} else {
			kept = append(kept, l)
		}
	}
	c.queue = kept
	waits := []chan struct{}{}
	for _, l := range c.active {
		if l.record.Prompt == prompt {
			if l.status == "" && l.process.Kill() {
				l.status = store.StatusKilled
			}
			waits = append(waits, l.done)
		}
	}
	c.mu.Unlock()
	for _, done := range waits {
		<-done
	}
	for _, r := range rs {
		if r.Status == store.StatusQueued || r.Status == store.StatusRunning {
			if _, e = c.Cancel(ctx, r.ID); e != nil && !errors.Is(e, store.ErrEnded) && !errors.Is(e, store.ErrNotFound) {
				return e
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e = removeTree(filepath.Join(c.cfg.Runs, prompt)); e != nil {
		return e
	}
	return c.cfg.Store.Delete(ctx, prompt)
}

// Recover settles the running and queued records left by a dead process.
func (c *Core) Recover(ctx context.Context) error {
	if e := os.MkdirAll(c.cfg.Runs, 0700); e != nil {
		return e
	}
	running, e := c.cfg.Store.Running(ctx)
	if e != nil {
		return e
	}
	queued, e := c.cfg.Store.Queued(ctx)
	if e != nil {
		return e
	}
	for _, r := range append(running, queued...) {
		now := c.cfg.Now()
		end := store.Ending{Status: store.StatusKilled, Finished: maxTime(now, r.Started)}
		if r.Status == store.StatusRunning {
			end.StdoutBytes, end.StderrBytes = c.Sizes(r)
			end.Usage = readUsage(filepath.Join(c.Folder(r), TranscriptFile))
		} else {
			end.Status = store.StatusFailed
			end.Reason = store.ReasonQueueAbandoned
		}
		ended, e := c.cfg.Store.FinishRun(ctx, r.ID, end)
		if e != nil {
			return e
		}
		c.emit(ended, "run.finished", now.Sub(r.Started))
	}
	return nil
}
func removeTree(path string) error {
	// Lstat prevents following a model's links while repairing directory permissions.
	f, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if !f.IsDir() {
		return os.Remove(path)
	}
	if e = os.Chmod(path, f.Mode().Perm()|0700); e != nil {
		return e
	}
	entries, e := os.ReadDir(path)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if e = removeTree(filepath.Join(path, entry.Name())); e != nil {
			return e
		}
	}
	return os.Remove(path)
}

// Prune removes past-keeping folders before deleting their records.
func (c *Core) Prune(ctx context.Context) error {
	rs, e := c.cfg.Store.PastKeeping(ctx, c.cfg.Now(), c.cfg.KeepDays, c.cfg.KeepCount)
	if e != nil {
		return e
	}
	for _, r := range rs {
		if removeTree(c.Folder(r)) != nil {
			continue
		}
		if e = c.cfg.Store.DeleteRun(ctx, r.ID); e != nil {
			return e
		}
	}
	return nil
}

// Drain refuses new runs, abandons the queue and waits or kills at its deadline.
func (c *Core) Drain(ctx context.Context) {
	c.mu.Lock()
	c.draining = true
	queued := c.queue
	c.queue = nil
	for _, l := range queued {
		_ = c.endQueued(l, store.StatusFailed, store.ReasonQueueAbandoned)
	}
	c.signal()
	for len(c.active) > 0 || c.calls > 0 {
		if ctx.Err() != nil && !c.deadline {
			c.deadline = true
			for _, l := range c.active {
				if l.status == "" && l.process.Kill() {
					l.status = store.StatusKilled
				}
			}
			c.signal()
		}
		changed := c.changed
		c.mu.Unlock()
		if ctx.Err() == nil {
			select {
			case <-changed:
			case <-ctx.Done():
			}
		} else {
			<-changed
		}
		c.mu.Lock()
		if ctx.Err() != nil && !c.deadline {
			c.deadline = true
			for _, l := range c.active {
				if l.status == "" && l.process.Kill() {
					l.status = store.StatusKilled
				}
			}
			c.signal()
		}
	}
	c.mu.Unlock()
}
