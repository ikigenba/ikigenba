package store

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"strings"
)

var cohortColumns = columns("id:ID name:Name filters:Filters ordering:Order size:Size created_user_id:CreatedUserID created_user_email:CreatedUserEmail created:Created changed_user_id:ChangedUserID changed_user_email:ChangedUserEmail changed:Changed")
var rejectColumns = columns("kind:Kind cohort:Cohort cohort_name:CohortName candidate:Candidate why:Why user_id:UserID user_email:UserEmail at:At")

func normalFilters(f CohortFilters) CohortFilters {
	f.Categories = slices.Clone(f.Categories)
	slices.Sort(f.Categories)
	f.Categories = slices.Compact(f.Categories)
	if len(f.Categories) == 0 {
		f.Categories = nil
	}
	return f
}
func normalOrder(o []CohortOrder) []CohortOrder {
	if len(o) == 0 {
		return []CohortOrder{{Field: OrderOperatingConfidence, Direction: DirectionDesc}}
	}
	return slices.Clone(o)
}
func cohortKey(ctx context.Context, tx *sql.Tx, id string) (Cohort, error) {
	cs, err := readRecords[Cohort](ctx, tx, "cohorts", cohortColumns, " WHERE id=?", id)
	if err != nil {
		return Cohort{}, err
	}
	if len(cs) == 0 {
		return Cohort{}, ErrNotFound
	}
	return cs[0], nil
}
func validCohort(c Cohort) bool {
	return ValidCohortName(c.Name) && ValidCohortSize(c.Size) && ValidCohortFilters(c.Filters) && ValidCohortOrder(c.Order)
}

// CreateCohort records a new saved search.
func (s *Store) CreateCohort(ctx context.Context, c Cohort) (Cohort, error) {
	var out Cohort
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if c.CreatedUserID == "" {
			return ErrUnreachable
		}
		if !validCohort(c) {
			return ErrInvalid
		}
		id, err := s.mintKey(ctx, tx, CohortIDPrefix)
		if err != nil {
			return err
		}
		c.ID = id
		c.Filters = normalFilters(c.Filters)
		c.Order = normalOrder(c.Order)
		c.Created = s.now()
		c.Changed = c.Created
		c.ChangedUserID = c.CreatedUserID
		c.ChangedUserEmail = c.CreatedUserEmail
		if err = insertRecord(ctx, tx, "cohorts", cohortColumns, c, nil); err != nil {
			return err
		}
		out = c
		return nil
	}))
	if err != nil {
		return Cohort{}, err
	}
	return out, nil
}

// EditCohort replaces the supplied parts, preserving the stamp on a no-op.
func (s *Store) EditCohort(ctx context.Context, e CohortEdit) (Cohort, error) {
	var out Cohort
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if e.UserID == "" {
			return ErrUnreachable
		}
		if e.Name != nil && !ValidCohortName(*e.Name) || e.Size != nil && !ValidCohortSize(*e.Size) || e.Filters != nil && !ValidCohortFilters(*e.Filters) || e.Order != nil && !ValidCohortOrder(*e.Order) {
			return ErrInvalid
		}
		c, err := cohortKey(ctx, tx, e.ID)
		if err != nil {
			return err
		}
		old := c
		if e.Name != nil {
			c.Name = *e.Name
		}
		if e.Size != nil {
			c.Size = *e.Size
		}
		if e.Filters != nil {
			c.Filters = normalFilters(*e.Filters)
		}
		if e.Order != nil {
			c.Order = normalOrder(*e.Order)
		}
		if !reflect.DeepEqual(old, c) {
			c.Changed = s.now()
			c.ChangedUserID = e.UserID
			c.ChangedUserEmail = e.UserEmail
			if err = insertRecord(ctx, tx, "cohorts", cohortColumns, c, nil); err != nil {
				return err
			}
		}
		out = c
		return nil
	}))
	if err != nil {
		return Cohort{}, err
	}
	return out, nil
}

// DeleteCohort removes a definition and retains its reject history.
func (s *Store) DeleteCohort(ctx context.Context, id string) (Cohort, error) {
	var out Cohort
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c, err := cohortKey(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = deleteRecord(ctx, tx, "cohorts", "id=?", id); err != nil {
			return err
		}
		out = c
		return nil
	}))
	if err != nil {
		return Cohort{}, err
	}
	return out, nil
}

// GetCohort reads one definition.
func (s *Store) GetCohort(ctx context.Context, id string) (Cohort, error) {
	var out Cohort
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error { var err error; out, err = cohortKey(ctx, tx, id); return err }))
	if err != nil {
		return Cohort{}, err
	}
	return out, nil
}

// CohortsFirst selects the first cohort window.
const CohortsFirst = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM cohorts ORDER BY name,id LIMIT ?1`

// CohortsAfter selects the window following a cohort position.
const CohortsAfter = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM cohorts WHERE (name,id)>(?1,?2) ORDER BY name,id LIMIT ?3`

// CohortsLast selects the reversed window from the end.
const CohortsLast = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM cohorts ORDER BY name DESC,id DESC LIMIT ?1`

// CohortsPreceding selects the preceding reversed window.
const CohortsPreceding = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM cohorts WHERE (name,id)<(?1,?2) ORDER BY name DESC,id DESC LIMIT ?3`

// ListCohorts reads a window in name and identifier order.
func (s *Store) ListCohorts(ctx context.Context, name, id string, n int) ([]Cohort, error) {
	return s.cohortWindow(ctx, name, id, n, false)
}

// ListCohortsReversed reads a preceding window, nearest first.
func (s *Store) ListCohortsReversed(ctx context.Context, name, id string, n int) ([]Cohort, error) {
	return s.cohortWindow(ctx, name, id, n, true)
}
func (s *Store) cohortWindow(ctx context.Context, name, id string, n int, reverse bool) ([]Cohort, error) {
	var out []Cohort
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		first, after := CohortsFirst, CohortsAfter
		if reverse {
			first, after = CohortsLast, CohortsPreceding
		}
		q := first
		args := []any{n}
		if id != "" {
			q = after
			args = []any{name, id, n}
		}
		var err error
		out, err = readQueryRecords[Cohort](ctx, tx, cohortColumns, q, args...)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

const rejectRead = `SELECT r.kind,r.cohort,coalesce(k.name,''),r.candidate,r.why,r.user_id,r.user_email,r.at FROM rejects AS r LEFT JOIN cohorts AS k ON k.id=r.cohort`

func standingRejects(ctx context.Context, tx *sql.Tx, id string) ([]Reject, error) {
	return readQueryRecords[Reject](ctx, tx, rejectColumns, rejectRead+` WHERE r.cohort=? AND r.kind=? AND NOT EXISTS (SELECT 1 FROM rejects AS n WHERE n.cohort=r.cohort AND n.candidate=r.candidate AND n.seq>r.seq) ORDER BY r.seq DESC`, id, KindReject)
}

// Reject records an idempotent rejection or restoration of one pair.
func (s *Store) Reject(ctx context.Context, r Reject) (Reject, error) {
	var out Reject
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if r.UserID == "" || (r.Kind != KindReject && r.Kind != KindUnreject) {
			return ErrUnreachable
		}
		if strings.TrimSpace(r.Why) == "" {
			return ErrInvalid
		}
		k, err := cohortKey(ctx, tx, r.Cohort)
		if err != nil {
			return err
		}
		c, err := candidateKey(ctx, tx, r.Candidate)
		if err != nil {
			return err
		}
		if c.Status == StatusMerged {
			return ErrInvalid
		}
		xs, err := readQueryRecords[Reject](ctx, tx, rejectColumns, rejectRead+` WHERE r.cohort=? AND r.candidate=? ORDER BY r.seq DESC LIMIT 1`, r.Cohort, r.Candidate)
		if err != nil {
			return err
		}
		standing := len(xs) > 0 && xs[0].Kind == KindReject
		if r.Kind == KindReject && standing {
			out = xs[0]
			return nil
		}
		if r.Kind == KindUnreject && !standing {
			out = Reject{Kind: KindUnreject, Cohort: r.Cohort, Candidate: r.Candidate, CohortName: k.Name}
			return nil
		}
		r.CohortName = k.Name
		r.At = s.now()
		err = execSQL(ctx, tx, `INSERT INTO rejects(cohort,candidate,kind,user_id,user_email,at,why) VALUES (?,?,?,?,?,?,?)`, r.Cohort, r.Candidate, r.Kind, r.UserID, r.UserEmail, r.At.Format("2006-01-02T15:04:05Z"), r.Why)
		if err != nil {
			return err
		}
		out = r
		return nil
	}))
	if err != nil {
		return Reject{}, err
	}
	return out, nil
}

// Rejects reads every cohort ruling naming a candidate, newest first.
func (s *Store) Rejects(ctx context.Context, candidate string) ([]Reject, error) {
	var out []Reject
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, candidate); err != nil {
			return err
		}
		var err error
		out, err = readQueryRecords[Reject](ctx, tx, rejectColumns, rejectRead+` WHERE r.candidate=? ORDER BY r.seq DESC`, candidate)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}
