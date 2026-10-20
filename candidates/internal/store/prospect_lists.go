package store

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"strings"
)

var prospectListColumns = columns("id:ID name:Name filters:Filters ordering:Order size:Size created_user_id:CreatedUserID created_user_email:CreatedUserEmail created:Created changed_user_id:ChangedUserID changed_user_email:ChangedUserEmail changed:Changed")
var rejectColumns = columns("kind:Kind prospect_list:ProspectList prospect_list_name:ProspectListName candidate:Candidate why:Why user_id:UserID user_email:UserEmail at:At")

func normalFilters(f ProspectListFilters) ProspectListFilters {
	f.Categories = slices.Clone(f.Categories)
	slices.Sort(f.Categories)
	f.Categories = slices.Compact(f.Categories)
	if len(f.Categories) == 0 {
		f.Categories = nil
	}
	return f
}
func normalOrder(o []ProspectListOrder) []ProspectListOrder {
	if len(o) == 0 {
		return []ProspectListOrder{{Field: OrderOperatingConfidence, Direction: DirectionDesc}}
	}
	return slices.Clone(o)
}
func prospectListKey(ctx context.Context, tx *sql.Tx, id string) (ProspectList, error) {
	cs, err := readRecords[ProspectList](ctx, tx, "prospect_lists", prospectListColumns, " WHERE id=?", id)
	if err != nil {
		return ProspectList{}, err
	}
	if len(cs) == 0 {
		return ProspectList{}, ErrNotFound
	}
	return cs[0], nil
}
func validProspectList(c ProspectList) bool {
	return ValidProspectListName(c.Name) && ValidProspectListSize(c.Size) && ValidProspectListFilters(c.Filters) && ValidProspectListOrder(c.Order)
}

// CreateProspectList records a new saved search.
func (s *Store) CreateProspectList(ctx context.Context, c ProspectList) (ProspectList, error) {
	var out ProspectList
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if c.CreatedUserID == "" {
			return ErrUnreachable
		}
		if !validProspectList(c) {
			return ErrInvalid
		}
		id, err := s.mintKey(ctx, tx, ProspectListIDPrefix)
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
		if err = insertRecord(ctx, tx, "prospect_lists", prospectListColumns, c, nil); err != nil {
			return err
		}
		out = c
		return nil
	}))
	if err != nil {
		return ProspectList{}, err
	}
	return out, nil
}

// EditProspectList replaces the supplied parts, preserving the stamp on a no-op.
func (s *Store) EditProspectList(ctx context.Context, e ProspectListEdit) (ProspectList, error) {
	var out ProspectList
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if e.UserID == "" {
			return ErrUnreachable
		}
		if e.Name != nil && !ValidProspectListName(*e.Name) || e.Size != nil && !ValidProspectListSize(*e.Size) || e.Filters != nil && !ValidProspectListFilters(*e.Filters) || e.Order != nil && !ValidProspectListOrder(*e.Order) {
			return ErrInvalid
		}
		c, err := prospectListKey(ctx, tx, e.ID)
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
			if err = insertRecord(ctx, tx, "prospect_lists", prospectListColumns, c, nil); err != nil {
				return err
			}
		}
		out = c
		return nil
	}))
	if err != nil {
		return ProspectList{}, err
	}
	return out, nil
}

// DeleteProspectList removes a definition and retains its reject history.
func (s *Store) DeleteProspectList(ctx context.Context, id string) (ProspectList, error) {
	var out ProspectList
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c, err := prospectListKey(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = deleteRecord(ctx, tx, "prospect_lists", "id=?", id); err != nil {
			return err
		}
		out = c
		return nil
	}))
	if err != nil {
		return ProspectList{}, err
	}
	return out, nil
}

// GetProspectList reads one definition.
func (s *Store) GetProspectList(ctx context.Context, id string) (ProspectList, error) {
	var out ProspectList
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error { var err error; out, err = prospectListKey(ctx, tx, id); return err }))
	if err != nil {
		return ProspectList{}, err
	}
	return out, nil
}

// ProspectListsFirst selects the first prospectList window.
const ProspectListsFirst = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM prospect_lists ORDER BY name,id LIMIT ?1`

// ProspectListsAfter selects the window following a prospectList position.
const ProspectListsAfter = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM prospect_lists WHERE (name,id)>(?1,?2) ORDER BY name,id LIMIT ?3`

// ProspectListsLast selects the reversed window from the end.
const ProspectListsLast = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM prospect_lists ORDER BY name DESC,id DESC LIMIT ?1`

// ProspectListsPreceding selects the preceding reversed window.
const ProspectListsPreceding = `SELECT id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM prospect_lists WHERE (name,id)<(?1,?2) ORDER BY name DESC,id DESC LIMIT ?3`

// ListProspectLists reads a window in name and identifier order.
func (s *Store) ListProspectLists(ctx context.Context, name, id string, n int) ([]ProspectList, error) {
	return s.prospectListWindow(ctx, name, id, n, false)
}

// ListProspectListsReversed reads a preceding window, nearest first.
func (s *Store) ListProspectListsReversed(ctx context.Context, name, id string, n int) ([]ProspectList, error) {
	return s.prospectListWindow(ctx, name, id, n, true)
}
func (s *Store) prospectListWindow(ctx context.Context, name, id string, n int, reverse bool) ([]ProspectList, error) {
	var out []ProspectList
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		first, after := ProspectListsFirst, ProspectListsAfter
		if reverse {
			first, after = ProspectListsLast, ProspectListsPreceding
		}
		q := first
		args := []any{n}
		if id != "" {
			q = after
			args = []any{name, id, n}
		}
		var err error
		out, err = readQueryRecords[ProspectList](ctx, tx, prospectListColumns, q, args...)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

const rejectRead = `SELECT r.kind,r.prospect_list,coalesce(k.name,''),r.candidate,r.why,r.user_id,r.user_email,r.at FROM rejects AS r LEFT JOIN prospect_lists AS k ON k.id=r.prospect_list`

func standingRejects(ctx context.Context, tx *sql.Tx, id string) ([]Reject, error) {
	return readQueryRecords[Reject](ctx, tx, rejectColumns, rejectRead+` WHERE r.prospect_list=? AND r.kind=? AND NOT EXISTS (SELECT 1 FROM rejects AS n WHERE n.prospect_list=r.prospect_list AND n.candidate=r.candidate AND n.seq>r.seq) ORDER BY r.seq DESC`, id, KindReject)
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
		k, err := prospectListKey(ctx, tx, r.ProspectList)
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
		xs, err := readQueryRecords[Reject](ctx, tx, rejectColumns, rejectRead+` WHERE r.prospect_list=? AND r.candidate=? ORDER BY r.seq DESC LIMIT 1`, r.ProspectList, r.Candidate)
		if err != nil {
			return err
		}
		standing := len(xs) > 0 && xs[0].Kind == KindReject
		if r.Kind == KindReject && standing {
			out = xs[0]
			return nil
		}
		if r.Kind == KindUnreject && !standing {
			out = Reject{Kind: KindUnreject, ProspectList: r.ProspectList, Candidate: r.Candidate, ProspectListName: k.Name}
			return nil
		}
		r.ProspectListName = k.Name
		r.At = s.now()
		err = execSQL(ctx, tx, `INSERT INTO rejects(cohort,prospect_list,candidate,kind,user_id,user_email,at,why) VALUES ('',?,?,?,?,?,?,?)`, r.ProspectList, r.Candidate, r.Kind, r.UserID, r.UserEmail, r.At.Format("2006-01-02T15:04:05Z"), r.Why)
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

// Rejects reads every prospectList ruling naming a candidate, newest first.
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
