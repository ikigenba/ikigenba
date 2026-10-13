// Package store owns scripts' durable catalog.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Catalog vocabulary and entity id prefixes.
const (
	ScriptPrefix                   = "scr_"
	RunPrefix                      = "run_"
	StatusQueued                   = "queued"
	StatusRunning                  = "running"
	StatusExited                   = "exited"
	StatusKilled                   = "killed"
	StatusTimedOut                 = "timed_out"
	StatusFailed                   = "failed"
	ReasonRepositoryMissing        = "repository_missing"
	ReasonCommitMissing            = "commit_missing"
	ReasonTooLarge                 = "too_large"
	ReasonGitFailed                = "git_failed"
	ReasonTimedOut                 = "timed_out"
	ReasonStartFailed              = "start_failed"
	ReasonQueueAbandoned           = "queue_abandoned"
	TriggerManual                  = "manual"
	TriggerEvent                   = "event"
	Unreachable             string = "cannot reach the catalog; try again later"
)

// Sentinel errors distinguish content refusals from database failures.
var (
	ErrNotFound      = errors.New("not found")
	ErrNameTaken     = errors.New("name taken")
	ErrEnded         = errors.New("run ended")
	ErrNotSubscribed = errors.New("not subscribed")
	ErrDelivered     = errors.New("event delivered")
)

// errCatalog is the stable error for a failure of the database handle.
var errCatalog = errors.New("catalog database failure")

// Config supplies the injected clock and randomness.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Script holds one catalog entry and its newest run.
type Script struct {
	ID, Name, Owner, Repo, Ref string
	Created                    time.Time
	Subscriptions              []Subscription
	Last                       *Run
}

// Subscription records a subscribed event pattern and creation time.
type Subscription struct {
	Event   string
	Created time.Time
}

// Run holds the durable metadata of one execution.
type Run struct {
	ID, Script, SHA, Ref, User, RequestID, Trigger, Event, Status string
	ExitCode                                                      int
	Started, Finished                                             time.Time
	StdoutBytes, StderrBytes                                      int64
	Truncated                                                     bool
	Reason                                                        string
}

// Draft supplies the fields of a new script.
type Draft struct{ Owner, Name, Repo, Ref string }

// Ending supplies the final outcome of a running run.
type Ending struct {
	Status                   string
	ExitCode                 int
	Finished                 time.Time
	StdoutBytes, StderrBytes int64
	Truncated                bool
	Reason                   string
}

// Store provides catalog operations over a caller-owned handle.
type Store struct {
	db  *db.DB
	cfg Config
}

// catalog is the content of a single transaction, never shared between calls.
type catalog struct {
	tx            *sql.Tx
	cfg           Config
	scripts       map[string]Script
	records       map[string]Run
	subscriptions map[string][]Subscription
}

func newID(prefix string, r io.Reader) (string, error) {
	b := make([]byte, 8)
	for offset, empty := 0, 0; offset < len(b); {
		n, err := r.Read(b[offset:])
		if err != nil {
			return "", err
		}
		offset += n
		if n == 0 {
			empty++
			if empty >= 100 {
				return "", io.ErrNoProgress
			}
		} else {
			empty = 0
		}
	}
	return prefix + hex.EncodeToString(b), nil
}

// NewScriptID draws an eight-byte script id.
func NewScriptID(r io.Reader) (string, error) { return newID(ScriptPrefix, r) }

// NewRunID draws an eight-byte run id.
func NewRunID(r io.Reader) (string, error) { return newID(RunPrefix, r) }
func validHex(s string) bool {
	for _, b := range []byte(s) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidScriptID reports whether s is a canonical script id.
func ValidScriptID(s string) bool {
	return len(s) == 20 && strings.HasPrefix(s, ScriptPrefix) && validHex(s[4:])
}

// ValidRunID reports whether s is a canonical run id.
func ValidRunID(s string) bool {
	return len(s) == 20 && strings.HasPrefix(s, RunPrefix) && validHex(s[4:])
}

// ValidName reports whether s is an available script-name shape.
func ValidName(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] == '-' || s == "about" || s == "tools" || s == "mcp" || s == "events" || s == "declarations" {
		return false
	}
	for _, b := range []byte(s) {
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
			return false
		}
	}
	return true
}

var eventPattern = regexp.MustCompile(`^([a-z][a-z0-9]*(_[a-z0-9]+)*|\*)(\.([a-z][a-z0-9]*(_[a-z0-9]+)*|\*))+$`)

// ValidEvent reports whether s is a canonical event subscription pattern.
func ValidEvent(s string) bool { return eventPattern.MatchString(s) }

func normalize(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Second)
}

// New builds a store over the caller's database handle.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{db: d, cfg: cfg}
}

func (s *Store) transaction(ctx context.Context, write bool, fn func(*catalog) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	call := s.db.Read
	if write {
		call = s.db.Write
	}
	var operationErr error
	err := call(ctx, func(tx *sql.Tx) error {
		c := &catalog{tx: tx, cfg: s.cfg, scripts: map[string]Script{}, records: map[string]Run{}}
		if write {
			if err := c.load(ctx); err != nil {
				return err
			}
		}
		operationErr = fn(c)
		return operationErr
	})
	if err == nil {
		return nil
	}
	if operationErr != nil {
		return operationErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errCatalog
}

func (s *catalog) save(ctx context.Context, scripts map[string]Script, runs map[string]Run) error {
	if err := s.persist(ctx, scripts, runs); err != nil {
		return err
	}
	s.scripts, s.records = scripts, runs
	return nil
}
func (s *catalog) copy() (map[string]Script, map[string]Run) {
	scripts := map[string]Script{}
	runs := map[string]Run{}
	for id, r := range s.scripts {
		scripts[id] = r
	}
	for id, r := range s.records {
		runs[id] = r
	}
	return scripts, runs
}
func (s *catalog) runs(script string) []Run {
	out := []Run{}
	for _, r := range s.records {
		if r.Script == script {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Started.Equal(out[j].Started) {
			return out[i].ID > out[j].ID
		}
		return out[i].Started.After(out[j].Started)
	})
	return out
}
func (s *catalog) script(sc Script) Script {
	sc.Last = nil
	sc.Subscriptions = append([]Subscription{}, s.subscriptions[sc.ID]...)
	rr := s.runs(sc.ID)
	if len(rr) > 0 {
		r := rr[0]
		sc.Last = &r
	}
	return sc
}

func (s *catalog) taken(name string) bool {
	for _, sc := range s.scripts {
		if sc.Name == name {
			return true
		}
	}
	return false
}
func validDraft(d Draft) bool {
	return ValidName(d.Name) && d.Owner != "" && d.Repo != "" && d.Ref != ""
}

// Create records a script with a fresh id and created timestamp.
func (s *catalog) create(ctx context.Context, d Draft) (Script, error) {
	if !validDraft(d) {
		return Script{}, errors.New("invalid script")
	}
	if s.taken(d.Name) {
		return Script{}, ErrNameTaken
	}
	created := normalize(s.cfg.Now())
	for i := 0; i < 8; i++ {
		id, err := NewScriptID(s.cfg.Rand)
		if err != nil {
			return Script{}, errors.New(err.Error())
		}
		if _, ok := s.scripts[id]; ok {
			continue
		}
		sc := Script{ID: id, Name: d.Name, Owner: d.Owner, Repo: d.Repo, Ref: d.Ref, Created: created}
		scripts, runs := s.copy()
		scripts[id] = sc
		if err = s.save(ctx, scripts, runs); err != nil {
			return Script{}, err
		}
		return s.script(sc), nil
	}
	return Script{}, errors.New("script id collision")
}

// SetRef updates a script ref and reports whether it changed.
func (s *catalog) setRef(ctx context.Context, id, ref string) (Script, bool, error) {
	if ref == "" {
		return Script{}, false, errors.New("empty ref")
	}
	sc, ok := s.scripts[id]
	if !ok {
		return Script{}, false, ErrNotFound
	}
	changed := sc.Ref != ref
	sc.Ref = ref
	scripts, runs := s.copy()
	scripts[id] = sc
	if err := s.save(ctx, scripts, runs); err != nil {
		return Script{}, false, err
	}
	return s.script(sc), changed, nil
}

// Delete removes a script and all of its runs atomically.
func (s *catalog) delete(ctx context.Context, id string) error {
	if _, ok := s.scripts[id]; !ok {
		return ErrNotFound
	}
	scripts, runs := s.copy()
	delete(scripts, id)
	for key, r := range runs {
		if r.Script == id {
			delete(runs, key)
		}
	}
	for _, query := range []string{"DELETE FROM subscriptions WHERE script=?", "DELETE FROM event_runs WHERE script=?"} {
		if _, err := s.tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return s.save(ctx, scripts, runs)
}
func validReason(r string) bool {
	switch r {
	case ReasonRepositoryMissing, ReasonCommitMissing, ReasonTooLarge, ReasonGitFailed, ReasonTimedOut, ReasonStartFailed, ReasonQueueAbandoned:
		return true
	}
	return false
}
func validRun(r Run) bool {
	if !ValidRunID(r.ID) || r.Ref == "" || r.User == "" || r.Script == "" || (r.Trigger != TriggerManual || r.Event != "") && (r.Trigger != TriggerEvent || r.Event == "") || normalize(r.Started).IsZero() || r.StdoutBytes < 0 || r.StderrBytes < 0 || r.SHA != "" && (len(r.SHA) != 40 || !validHex(r.SHA)) {
		return false
	}
	switch r.Status {
	case StatusRunning, StatusQueued:
		return r.SHA != "" && r.ExitCode == 0 && r.StdoutBytes == 0 && r.StderrBytes == 0 && r.Finished.IsZero() && !r.Truncated && r.Reason == ""
	case StatusFailed:
		return validReason(r.Reason) && r.ExitCode == 0 && !normalize(r.Finished).IsZero() && !normalize(r.Finished).Before(normalize(r.Started))
	}
	return false
}
func validEnding(e Ending) bool {
	return !normalize(e.Finished).IsZero() && e.StdoutBytes >= 0 && e.StderrBytes >= 0 && (e.Status == StatusExited && e.ExitCode >= 0 && e.ExitCode <= 255 && e.Reason == "" || (e.Status == StatusKilled || e.Status == StatusTimedOut) && e.ExitCode == 0 && e.Reason == "" || e.Status == StatusFailed && e.ExitCode == 0 && (e.Reason == ReasonStartFailed || e.Reason == ReasonQueueAbandoned))
}

// AddRun records a newly running or failed run.
func (s *catalog) addRun(ctx context.Context, r Run) (Run, error) {
	_, duplicate := s.records[r.ID]
	if !validRun(r) || duplicate {
		return Run{}, errors.New("invalid run")
	}
	if _, ok := s.scripts[r.Script]; !ok {
		return Run{}, ErrNotFound
	}
	if r.Trigger == TriggerEvent {
		delivered, err := s.delivered(ctx, r.Script, r.Event)
		if err != nil {
			return Run{}, err
		}
		if delivered {
			return Run{}, ErrDelivered
		}
		if _, err = s.tx.ExecContext(ctx, "INSERT INTO event_runs(script,event) VALUES(?,?)", r.Script, r.Event); err != nil {
			return Run{}, err
		}
	}
	r.Started = normalize(r.Started)
	r.Finished = normalize(r.Finished)
	scripts, runs := s.copy()
	runs[r.ID] = r
	if err := s.save(ctx, scripts, runs); err != nil {
		return Run{}, err
	}
	return r, nil
}

// FinishRun ends a running run exactly once.
func (s *catalog) finishRun(ctx context.Context, id string, e Ending) (Run, error) {
	if !validEnding(e) {
		return Run{}, errors.New("invalid ending")
	}
	r, ok := s.records[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	if r.Status != StatusRunning && r.Status != StatusQueued {
		return Run{}, ErrEnded
	}
	if r.Status == StatusRunning && e.Status == StatusFailed || r.Status == StatusQueued && (e.Status != StatusKilled && e.Status != StatusFailed || e.StdoutBytes != 0 || e.StderrBytes != 0 || e.Truncated) {
		return Run{}, errors.New("ending does not fit run")
	}
	if normalize(e.Finished).Before(r.Started) {
		return Run{}, errors.New("ending precedes start")
	}
	r.Status = e.Status
	r.ExitCode = e.ExitCode
	r.Finished = normalize(e.Finished)
	r.StdoutBytes = e.StdoutBytes
	r.StderrBytes = e.StderrBytes
	r.Truncated = e.Truncated
	r.Reason = e.Reason
	scripts, runs := s.copy()
	runs[id] = r
	if err := s.save(ctx, scripts, runs); err != nil {
		return Run{}, err
	}
	return r, nil
}

// DeleteRun removes a single run record.
func (s *catalog) deleteRun(ctx context.Context, id string) error {
	if _, ok := s.records[id]; !ok {
		return ErrNotFound
	}
	scripts, runs := s.copy()
	delete(runs, id)
	return s.save(ctx, scripts, runs)
}

// Find performs a catalog operation through the supplied handle.
func (s *Store) Find(ctx context.Context, owner, name string) (Script, error) {
	var out Script
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readScript(ctx, "SELECT id,name,owner,repo,ref,created FROM scripts WHERE owner=? AND name=?", owner, name)
		return err
	})
	if err != nil {
		var zero Script
		return zero, err
	}
	return out, nil
}

// List performs a catalog operation through the supplied handle.
func (s *Store) List(ctx context.Context, owner, after string, n int) ([]Script, error) {
	var out []Script
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readScripts(ctx, "SELECT id,name,owner,repo,ref,created FROM scripts WHERE owner=? AND name>? ORDER BY name LIMIT ?", owner, after, max(n, 0))
		return err
	})
	if err != nil {
		var zero []Script
		return zero, err
	}
	return out, nil
}

// Taken performs a catalog operation through the supplied handle.
func (s *Store) Taken(ctx context.Context, name string) (bool, error) {
	var out bool
	err := s.transaction(ctx, false, func(c *catalog) error {
		return c.tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM scripts WHERE name=?)", name).Scan(&out)
	})
	if err != nil {
		var zero bool
		return zero, err
	}
	return out, nil
}

// Create performs a catalog operation through the supplied handle.
func (s *Store) Create(ctx context.Context, d Draft) (Script, error) {
	var out Script
	err := s.transaction(ctx, true, func(c *catalog) error {
		var err error
		out, err = c.create(ctx, d)
		return err
	})
	if err != nil {
		var zero Script
		return zero, err
	}
	return out, nil
}

// Delete performs a catalog operation through the supplied handle.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.transaction(ctx, true, func(c *catalog) error { return c.delete(ctx, id) })
}

// AddRun performs a catalog operation through the supplied handle.
func (s *Store) AddRun(ctx context.Context, r Run) (Run, error) {
	var out Run
	err := s.transaction(ctx, true, func(c *catalog) error {
		var err error
		out, err = c.addRun(ctx, r)
		return err
	})
	if err != nil {
		var zero Run
		return zero, err
	}
	return out, nil
}

// FinishRun performs a catalog operation through the supplied handle.
func (s *Store) FinishRun(ctx context.Context, id string, e Ending) (Run, error) {
	var out Run
	err := s.transaction(ctx, true, func(c *catalog) error {
		var err error
		out, err = c.finishRun(ctx, id, e)
		return err
	})
	if err != nil {
		var zero Run
		return zero, err
	}
	return out, nil
}

// FindRun performs a catalog operation through the supplied handle.
func (s *Store) FindRun(ctx context.Context, owner, id string) (Run, error) {
	var out Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readRun(ctx, "SELECT r.id,r.script,r.sha,r.ref,r.user,r.request_id,r.trigger,r.event,r.status,r.exit_code,r.started,r.finished,r.stdout_bytes,r.stderr_bytes,r.truncated,r.reason FROM runs r JOIN scripts s ON s.id=r.script WHERE r.id=? AND s.owner=? AND s.owner<>''", id, owner)
		return err
	})
	if err != nil {
		var zero Run
		return zero, err
	}
	return out, nil
}

// RunByID performs a catalog operation through the supplied handle.
func (s *Store) RunByID(ctx context.Context, id string) (Run, error) {
	var out Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readRun(ctx, "SELECT "+runColumns+" FROM runs WHERE id=?", id)
		return err
	})
	if err != nil {
		var zero Run
		return zero, err
	}
	return out, nil
}

// Runs performs a catalog operation through the supplied handle.
func (s *Store) Runs(ctx context.Context, script, started, id string, n int) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.windowRuns(ctx, script, started, id, n)
		return err
	})
	if err != nil {
		var zero []Run
		return zero, err
	}
	return out, nil
}

// Running performs a catalog operation through the supplied handle.
func (s *Store) Running(ctx context.Context) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM runs WHERE status=? ORDER BY id", StatusRunning)
		return err
	})
	if err != nil {
		var zero []Run
		return zero, err
	}
	return out, nil
}

// PastKeeping performs a catalog operation through the supplied handle.
func (s *Store) PastKeeping(ctx context.Context, now time.Time, days, count int64) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readPastKeeping(ctx, now, days, count)
		return err
	})
	if err != nil {
		var zero []Run
		return zero, err
	}
	return out, nil
}

// DeleteRun performs a catalog operation through the supplied handle.
func (s *Store) DeleteRun(ctx context.Context, id string) error {
	return s.transaction(ctx, true, func(c *catalog) error { return c.deleteRun(ctx, id) })
}

// SetRef changes a script's ref and reports whether it changed.
func (s *Store) SetRef(ctx context.Context, id, ref string) (Script, bool, error) {
	var out Script
	var changed bool
	err := s.transaction(ctx, true, func(c *catalog) error {
		var err error
		out, changed, err = c.setRef(ctx, id, ref)
		return err
	})
	if err != nil {
		return Script{}, false, err
	}
	return out, changed, nil
}

// StartRun changes a queued run to running without changing its request time.
func (s *Store) StartRun(ctx context.Context, id string) (Run, error) {
	var out Run
	err := s.transaction(ctx, true, func(c *catalog) error {
		r, ok := c.records[id]
		if !ok {
			return ErrNotFound
		}
		if r.Status != StatusQueued {
			return ErrEnded
		}
		r.Status = StatusRunning
		scripts, runs := c.copy()
		runs[id] = r
		if err := c.save(ctx, scripts, runs); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

// Queued returns all waiting runs in ascending id order.
func (s *Store) Queued(ctx context.Context) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM runs WHERE status=? ORDER BY id", StatusQueued)
		return err
	})
	return out, err
}
