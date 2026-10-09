// Package store owns scripts' durable catalog.
package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
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
type content struct {
	Scripts map[string]Script
	Runs    map[string]Run
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
	data          content
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
	if len(s) < 1 || len(s) > 64 || s[0] == '-' || s == "about" || s == "mcp" || s == "events" || s == "declarations" {
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
		c := &catalog{tx: tx, cfg: s.cfg, data: content{Scripts: map[string]Script{}, Runs: map[string]Run{}}}
		var encoded []byte
		err := tx.QueryRowContext(ctx, "SELECT value FROM catalog WHERE id=1").Scan(&encoded)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if err = gob.NewDecoder(bytes.NewReader(encoded)).Decode(&c.data); err != nil {
				return err
			}
		}
		c.subscriptions = map[string][]Subscription{}
		rows, err := tx.QueryContext(ctx, "SELECT script,event,created FROM subscriptions ORDER BY event")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, event string
			var created int64
			if err = rows.Scan(&id, &event, &created); err != nil {
				_ = rows.Close()
				return err
			}
			c.subscriptions[id] = append(c.subscriptions[id], Subscription{Event: event, Created: time.Unix(created, 0).UTC()})
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for id, sc := range c.data.Scripts {
			sc.Created = normalize(sc.Created)
			sc.Last = nil
			c.data.Scripts[id] = sc
		}
		for id, r := range c.data.Runs {
			r.Started = normalize(r.Started)
			r.Finished = normalize(r.Finished)
			c.data.Runs[id] = r
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

func (s *catalog) save(ctx context.Context, c content) error {
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(c); err != nil {
		return err
	}
	_, err := s.tx.ExecContext(ctx, "INSERT INTO catalog(id,value) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", encoded.Bytes())
	if err == nil {
		s.data = c
	}
	return err
}
func (s *catalog) copy() content {
	c := content{Scripts: map[string]Script{}, Runs: map[string]Run{}}
	for id, r := range s.data.Scripts {
		c.Scripts[id] = r
	}
	for id, r := range s.data.Runs {
		c.Runs[id] = r
	}
	return c
}
func (s *catalog) runs(script string) []Run {
	out := []Run{}
	for _, r := range s.data.Runs {
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

// Find reads a script by owner and name.
func (s *catalog) find(owner, name string) (Script, error) {
	for _, sc := range s.data.Scripts {
		if sc.Owner == owner && sc.Name == name {
			return s.script(sc), nil
		}
	}
	return Script{}, ErrNotFound
}

// List reads an owner's scripts in name order.
func (s *catalog) list(owner string) []Script {
	out := []Script{}
	for _, sc := range s.data.Scripts {
		if sc.Owner == owner {
			out = append(out, s.script(sc))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Taken checks the space-wide name namespace.
func (s *catalog) readTaken(name string) bool {
	return s.taken(name)
}
func (s *catalog) taken(name string) bool {
	for _, sc := range s.data.Scripts {
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
		if _, ok := s.data.Scripts[id]; ok {
			continue
		}
		sc := Script{ID: id, Name: d.Name, Owner: d.Owner, Repo: d.Repo, Ref: d.Ref, Created: created}
		c := s.copy()
		c.Scripts[id] = sc
		if err = s.save(ctx, c); err != nil {
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
	sc, ok := s.data.Scripts[id]
	if !ok {
		return Script{}, false, ErrNotFound
	}
	changed := sc.Ref != ref
	sc.Ref = ref
	c := s.copy()
	c.Scripts[id] = sc
	if err := s.save(ctx, c); err != nil {
		return Script{}, false, err
	}
	return s.script(sc), changed, nil
}

// Delete removes a script and all of its runs atomically.
func (s *catalog) delete(ctx context.Context, id string) error {
	if _, ok := s.data.Scripts[id]; !ok {
		return ErrNotFound
	}
	c := s.copy()
	delete(c.Scripts, id)
	for key, r := range c.Runs {
		if r.Script == id {
			delete(c.Runs, key)
		}
	}
	for _, query := range []string{"DELETE FROM subscriptions WHERE script=?", "DELETE FROM event_runs WHERE script=?"} {
		if _, err := s.tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return s.save(ctx, c)
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
	_, duplicate := s.data.Runs[r.ID]
	if !validRun(r) || duplicate {
		return Run{}, errors.New("invalid run")
	}
	if _, ok := s.data.Scripts[r.Script]; !ok {
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
	c := s.copy()
	c.Runs[r.ID] = r
	if err := s.save(ctx, c); err != nil {
		return Run{}, err
	}
	return r, nil
}

// FinishRun ends a running run exactly once.
func (s *catalog) finishRun(ctx context.Context, id string, e Ending) (Run, error) {
	if !validEnding(e) {
		return Run{}, errors.New("invalid ending")
	}
	r, ok := s.data.Runs[id]
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
	c := s.copy()
	c.Runs[id] = r
	if err := s.save(ctx, c); err != nil {
		return Run{}, err
	}
	return r, nil
}

// FindRun reads a run belonging to the given owner.
func (s *catalog) findRun(owner, id string) (Run, error) {
	r, ok := s.data.Runs[id]
	if !ok || s.data.Scripts[r.Script].Owner != owner || owner == "" {
		return Run{}, ErrNotFound
	}
	return r, nil
}

// RunByID reads a run regardless of owner.
func (s *catalog) runByID(id string) (Run, error) {
	r, ok := s.data.Runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return r, nil
}

// Runs reads a script's runs newest first.
func (s *catalog) readRuns(script string) []Run {
	return s.runs(script)
}

// Running reads all running runs in ascending id order.
func (s *catalog) running() []Run {
	out := []Run{}
	for _, r := range s.data.Runs {
		if r.Status == StatusRunning {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PastKeeping reads ended runs beyond both retention thresholds.
func (s *catalog) pastKeeping(now time.Time, days, count int64) ([]Run, error) {
	if days < 1 || count < 1 {
		return nil, errors.New("invalid retention")
	}
	out := []Run{}
	ids := []string{}
	for id := range s.data.Scripts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for rank, r := range s.runs(id) {
			if int64(rank) >= count && r.Status != StatusRunning && r.Status != StatusQueued && pastAge(now, r.Started, days) {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// DeleteRun removes a single run record.
func (s *catalog) deleteRun(ctx context.Context, id string) error {
	if _, ok := s.data.Runs[id]; !ok {
		return ErrNotFound
	}
	c := s.copy()
	delete(c.Runs, id)
	return s.save(ctx, c)
}

func pastAge(now, started time.Time, days int64) bool {
	elapsed := new(big.Int).Sub(big.NewInt(now.Unix()), big.NewInt(started.Unix()))
	elapsed.Mul(elapsed, big.NewInt(int64(time.Second)))
	elapsed.Add(elapsed, big.NewInt(int64(now.Nanosecond()-started.Nanosecond())))
	threshold := new(big.Int).Mul(big.NewInt(days), big.NewInt(int64(24*time.Hour)))
	return elapsed.Cmp(threshold) > 0
}

// Find performs a catalog operation through the supplied handle.
func (s *Store) Find(ctx context.Context, owner, name string) (Script, error) {
	var out Script
	err := s.transaction(ctx, false, func(c *catalog) error {
		var err error
		out, err = c.find(owner, name)
		return err
	})
	if err != nil {
		var zero Script
		return zero, err
	}
	return out, nil
}

// List performs a catalog operation through the supplied handle.
func (s *Store) List(ctx context.Context, owner string) ([]Script, error) {
	var out []Script
	err := s.transaction(ctx, false, func(c *catalog) error {
		out = c.list(owner)
		return nil
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
		out = c.readTaken(name)
		return nil
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
		out, err = c.findRun(owner, id)
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
		out, err = c.runByID(id)
		return err
	})
	if err != nil {
		var zero Run
		return zero, err
	}
	return out, nil
}

// Runs performs a catalog operation through the supplied handle.
func (s *Store) Runs(ctx context.Context, script string) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(c *catalog) error {
		out = c.readRuns(script)
		return nil
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
		out = c.running()
		return nil
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
		out, err = c.pastKeeping(now, days, count)
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
		r, ok := c.data.Runs[id]
		if !ok {
			return ErrNotFound
		}
		if r.Status != StatusQueued {
			return ErrEnded
		}
		r.Status = StatusRunning
		data := c.copy()
		data.Runs[id] = r
		if err := c.save(ctx, data); err != nil {
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
		out = []Run{}
		for _, r := range c.data.Runs {
			if r.Status == StatusQueued {
				out = append(out, r)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	return out, err
}
