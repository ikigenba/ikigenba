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
	"fmt"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	// Register the catalog SQLite driver.
	_ "modernc.org/sqlite"
)

// Catalog vocabulary and entity id prefixes.
const (
	ScriptPrefix            = "scr_"
	RunPrefix               = "run_"
	StatusRunning           = "running"
	StatusExited            = "exited"
	StatusKilled            = "killed"
	StatusTimedOut          = "timed_out"
	StatusFailed            = "failed"
	ReasonRepositoryMissing = "repository_missing"
	ReasonCommitMissing     = "commit_missing"
	ReasonTooLarge          = "too_large"
	ReasonGitFailed         = "git_failed"
	ReasonTimedOut          = "timed_out"
	ReasonStartFailed       = "start_failed"
	TriggerManual           = "manual"
	Unreachable             = "cannot reach the catalog; try again later"
)

// Sentinel errors distinguish content refusals from database failures.
var (
	ErrDatabase  = errors.New("database failure")
	ErrNotFound  = errors.New("not found")
	ErrNameTaken = errors.New("name taken")
	ErrEnded     = errors.New("run ended")
)

// Config supplies the catalog source and injected clock and randomness.
type Config struct {
	Source string
	Now    func() time.Time
	Rand   io.Reader
}

// Script holds one catalog entry and its newest run.
type Script struct {
	ID, Name, Owner, Repo, Ref string
	Created                    time.Time
	Last                       *Run
}

// Run holds the durable metadata of one execution.
type Run struct {
	ID, Script, SHA, Ref, User, RequestID, Trigger, Status string
	ExitCode                                               int
	Started, Finished                                      time.Time
	StdoutBytes, StderrBytes                               int64
	Truncated                                              bool
	Reason                                                 string
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
}
type content struct {
	Scripts map[string]Script
	Runs    map[string]Run
}

// Store is a concurrent durable catalog.
type Store struct {
	mu     sync.Mutex
	db     *sql.DB
	cfg    Config
	closed bool
	data   content
}
type databaseError struct{ err error }

func (e databaseError) Error() string { return strings.ReplaceAll(e.err.Error(), "\n", " ") }
func (e databaseError) Is(target error) bool {
	return target == ErrDatabase || errors.Is(e.err, target)
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
	if len(s) < 1 || len(s) > 64 || s[0] == '-' || s == "about" || s == "mcp" {
		return false
	}
	for _, b := range []byte(s) {
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
			return false
		}
	}
	return true
}
func normalize(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Second)
}

// Open creates or loads a catalog from cfg.Source.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, databaseError{err}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	memory := cfg.Source == "" || cfg.Source == ":memory:"
	source := cfg.Source
	if memory {
		source = ":memory:"
	}
	existed := false
	sidecars := map[string]bool{}
	if !memory {
		parent := sourceParent(source)
		if err := os.MkdirAll(parent, 0700); err != nil {
			return nil, databaseError{err}
		}
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			_, sideErr := os.Stat(source + suffix)
			sidecars[suffix] = !os.IsNotExist(sideErr)
		}
		info, err := os.Stat(source)
		if err == nil {
			existed = true
			if !info.Mode().IsRegular() {
				return nil, databaseError{fmt.Errorf("%s is not a regular database file", source)}
			}
			if info.Mode().Perm()&0222 == 0 {
				return nil, databaseError{fmt.Errorf("%s is not writable", source)}
			}
		} else if !os.IsNotExist(err) {
			return nil, databaseError{err}
		}
		info, err = os.Stat(parent)
		if err != nil {
			return nil, databaseError{err}
		}
		if info.Mode().Perm()&0222 == 0 {
			return nil, databaseError{fmt.Errorf("%s is not writable", parent)}
		}
	}
	databaseSource := source
	if !memory {
		if !filepath.IsAbs(databaseSource) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, databaseError{err}
			}
			databaseSource = cwd + string(os.PathSeparator) + databaseSource
		}
		databaseSource = (&url.URL{Scheme: "file", Path: databaseSource}).String()
	}
	db, err := sql.Open("sqlite", databaseSource)
	if err != nil {
		return nil, databaseError{err}
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) {
		_ = db.Close()
		if !memory {
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				if !sidecars[suffix] {
					_ = os.Remove(source + suffix)
				}
			}
			if !existed {
				_ = os.Remove(source)
			}
		}
		return nil, databaseError{err}
	}
	if _, err = db.ExecContext(ctx, "SELECT count(*) FROM sqlite_master"); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS catalog (id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL)"); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return fail(err)
	}
	s := &Store{db: db, cfg: cfg, data: content{Scripts: map[string]Script{}, Runs: map[string]Run{}}}
	var b []byte
	err = db.QueryRowContext(ctx, "SELECT value FROM catalog WHERE id=1").Scan(&b)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fail(err)
	}
	if len(b) > 0 {
		if err = gob.NewDecoder(bytes.NewReader(b)).Decode(&s.data); err != nil {
			return fail(err)
		}
	}
	for id, sc := range s.data.Scripts {
		sc.Created = normalize(sc.Created)
		sc.Last = nil
		s.data.Scripts[id] = sc
	}
	for id, r := range s.data.Runs {
		r.Started = normalize(r.Started)
		r.Finished = normalize(r.Finished)
		s.data.Runs[id] = r
	}
	return s, nil
}

// Close releases the database and makes this store unavailable.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}
func (s *Store) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return errors.New("catalog closed")
	}
	var n int
	return s.db.QueryRowContext(ctx, "SELECT count(*) FROM catalog").Scan(&n)
}
func (s *Store) save(ctx context.Context, c content) error {
	var encoded bytes.Buffer
	err := gob.NewEncoder(&encoded).Encode(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO catalog(id,value) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", encoded.Bytes())
	if err == nil {
		s.data = c
	}
	return err
}
func (s *Store) copy() content {
	c := content{Scripts: map[string]Script{}, Runs: map[string]Run{}}
	for id, r := range s.data.Scripts {
		c.Scripts[id] = r
	}
	for id, r := range s.data.Runs {
		c.Runs[id] = r
	}
	return c
}
func (s *Store) runs(script string) []Run {
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
func (s *Store) script(sc Script) Script {
	sc.Last = nil
	rr := s.runs(sc.ID)
	if len(rr) > 0 {
		r := rr[0]
		sc.Last = &r
	}
	return sc
}

// Find reads a script by owner and name.
func (s *Store) Find(ctx context.Context, owner, name string) (Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Script{}, err
	}
	for _, sc := range s.data.Scripts {
		if sc.Owner == owner && sc.Name == name {
			return s.script(sc), nil
		}
	}
	return Script{}, ErrNotFound
}

// List reads an owner's scripts in name order.
func (s *Store) List(ctx context.Context, owner string) ([]Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	out := []Script{}
	for _, sc := range s.data.Scripts {
		if sc.Owner == owner {
			out = append(out, s.script(sc))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Taken checks the space-wide name namespace.
func (s *Store) Taken(ctx context.Context, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return false, err
	}
	return s.taken(name), nil
}
func (s *Store) taken(name string) bool {
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
func (s *Store) Create(ctx context.Context, d Draft) (Script, error) {
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return Script{}, err
	}
	if !validDraft(d) {
		s.mu.Unlock()
		return Script{}, errors.New("invalid script")
	}
	if s.taken(d.Name) {
		s.mu.Unlock()
		return Script{}, ErrNameTaken
	}
	s.mu.Unlock()
	created := normalize(s.cfg.Now())
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Script{}, err
	}
	if s.taken(d.Name) {
		return Script{}, ErrNameTaken
	}
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
		return sc, nil
	}
	return Script{}, errors.New("script id collision")
}

// SetRef updates a script ref and reports whether it changed.
func (s *Store) SetRef(ctx context.Context, id, ref string) (Script, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Script{}, false, err
	}
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
func (s *Store) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return err
	}
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
	return s.save(ctx, c)
}
func validReason(r string) bool {
	switch r {
	case ReasonRepositoryMissing, ReasonCommitMissing, ReasonTooLarge, ReasonGitFailed, ReasonTimedOut, ReasonStartFailed:
		return true
	}
	return false
}
func validRun(r Run) bool {
	if !ValidRunID(r.ID) || r.Ref == "" || r.User == "" || r.Script == "" || r.Trigger != TriggerManual || normalize(r.Started).IsZero() || r.StdoutBytes < 0 || r.StderrBytes < 0 || r.SHA != "" && (len(r.SHA) != 40 || !validHex(r.SHA)) {
		return false
	}
	switch r.Status {
	case StatusRunning:
		return r.SHA != "" && r.ExitCode == 0 && r.StdoutBytes == 0 && r.StderrBytes == 0 && r.Finished.IsZero() && !r.Truncated && r.Reason == ""
	case StatusFailed:
		return validReason(r.Reason) && r.ExitCode == 0 && !normalize(r.Finished).IsZero() && !normalize(r.Finished).Before(normalize(r.Started))
	}
	return false
}
func validEnding(e Ending) bool {
	return !normalize(e.Finished).IsZero() && e.StdoutBytes >= 0 && e.StderrBytes >= 0 && (e.Status == StatusExited && e.ExitCode >= 0 && e.ExitCode <= 255 || (e.Status == StatusKilled || e.Status == StatusTimedOut) && e.ExitCode == 0)
}

// AddRun records a newly running or failed run.
func (s *Store) AddRun(ctx context.Context, r Run) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Run{}, err
	}
	_, duplicate := s.data.Runs[r.ID]
	if !validRun(r) || duplicate {
		return Run{}, errors.New("invalid run")
	}
	if _, ok := s.data.Scripts[r.Script]; !ok {
		return Run{}, ErrNotFound
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
func (s *Store) FinishRun(ctx context.Context, id string, e Ending) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Run{}, err
	}
	if !validEnding(e) {
		return Run{}, errors.New("invalid ending")
	}
	r, ok := s.data.Runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	if r.Status != StatusRunning {
		return Run{}, ErrEnded
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
	c := s.copy()
	c.Runs[id] = r
	if err := s.save(ctx, c); err != nil {
		return Run{}, err
	}
	return r, nil
}

// FindRun reads a run belonging to the given owner.
func (s *Store) FindRun(ctx context.Context, owner, id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Run{}, err
	}
	r, ok := s.data.Runs[id]
	if !ok || s.data.Scripts[r.Script].Owner != owner || owner == "" {
		return Run{}, ErrNotFound
	}
	return r, nil
}

// RunByID reads a run regardless of owner.
func (s *Store) RunByID(ctx context.Context, id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Run{}, err
	}
	r, ok := s.data.Runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return r, nil
}

// Runs reads a script's runs newest first.
func (s *Store) Runs(ctx context.Context, script string) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	return s.runs(script), nil
}

// Running reads all running runs in ascending id order.
func (s *Store) Running(ctx context.Context) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	out := []Run{}
	for _, r := range s.data.Runs {
		if r.Status == StatusRunning {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// PastKeeping reads ended runs beyond both retention thresholds.
func (s *Store) PastKeeping(ctx context.Context, now time.Time, days, count int64) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
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
			if int64(rank) >= count && r.Status != StatusRunning && pastAge(now, r.Started, days) {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// DeleteRun removes a single run record.
func (s *Store) DeleteRun(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return err
	}
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

func sourceParent(source string) string {
	i := strings.LastIndexByte(source, os.PathSeparator)
	if i < 0 {
		return "."
	}
	if i == 0 {
		return string(os.PathSeparator)
	}
	return source[:i]
}
