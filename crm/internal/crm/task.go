package crm

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"crm/internal/ids"
)

// taskStore is the SQL-only data layer for the tasks table. A task is a simple
// follow-up: a title, an open|done status, optional due_at/done_at timestamps,
// and an OPTIONAL subject reference (one of contact_id/org_id/deal_id, PLAN.md
// §3). There is no owner/assignee — the box is single-tenant. Tasks own no child
// sets, so this is the closest analog to deal.go's scalar-only shape.
//
// Normalization and rich validation happen at the dispatcher seam; Save trusts
// the typed input but enforces the create-required title with a corrective
// message rather than surfacing a raw NOT NULL error.
type taskStore struct{}

func (taskStore) Save(tx *sql.Tx, id string, in TaskInput, now time.Time) (Summary, error) {
	if id == "" {
		return taskInsert(tx, in, now)
	}
	return taskUpdate(tx, id, in, now)
}

func taskInsert(tx *sql.Tx, in TaskInput, now time.Time) (Summary, error) {
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return Summary{}, invalid("title", "task title is required")
	}
	id := ids.NewULID()
	ts := fmtTime(now)
	status := "open"
	if in.Status != nil {
		status = *in.Status
	}
	_, err := tx.Exec(`
		INSERT INTO tasks (id, title, status, due_at, done_at, contact_id, org_id, deal_id, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		id, *in.Title, status,
		nullStrPtrOrNil(in.DueAt), nullStrPtrOrNil(in.DoneAt),
		nullStrPtrOrNil(in.ContactID), nullStrPtrOrNil(in.OrgID), nullStrPtrOrNil(in.DealID),
		ts, ts)
	if err != nil {
		return Summary{}, mapUniqueErr(err, "task")
	}
	s, err := taskSummary(tx, id)
	if err != nil {
		return Summary{}, err
	}
	s.isCreate = true
	return s, nil
}

func taskUpdate(tx *sql.Tx, id string, in TaskInput, now time.Time) (Summary, error) {
	sets := []string{"updated_at = ?"}
	args := []any{fmtTime(now)}
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			return Summary{}, invalid("title", "task title must not be empty")
		}
		sets = append(sets, "title = ?")
		args = append(args, *in.Title)
	}
	if in.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *in.Status)
	}
	if in.DueAt != nil {
		sets = append(sets, "due_at = ?")
		args = append(args, nullStrOrNil(*in.DueAt))
	}
	if in.DoneAt != nil {
		sets = append(sets, "done_at = ?")
		args = append(args, nullStrOrNil(*in.DoneAt))
	}
	if in.ContactID != nil {
		sets = append(sets, "contact_id = ?")
		args = append(args, nullStrOrNil(*in.ContactID))
	}
	if in.OrgID != nil {
		sets = append(sets, "org_id = ?")
		args = append(args, nullStrOrNil(*in.OrgID))
	}
	if in.DealID != nil {
		sets = append(sets, "deal_id = ?")
		args = append(args, nullStrOrNil(*in.DealID))
	}
	// done_at convenience (PLAN.md §3): completing a task is a save with
	// status:"done". When the caller flips status but does not state done_at
	// itself, stamp/clear it to match — an explicit done_at in the input always
	// wins (handled by the block above, which already appended a done_at set).
	if in.Status != nil && in.DoneAt == nil {
		switch *in.Status {
		case "done":
			sets = append(sets, "done_at = ?")
			args = append(args, fmtTime(now))
		case "open":
			sets = append(sets, "done_at = ?")
			args = append(args, nil)
		}
	}
	args = append(args, id)
	res, err := tx.Exec(`UPDATE tasks SET `+strings.Join(sets, ", ")+` WHERE id = ? AND deleted_at IS NULL`, args...)
	if err != nil {
		return Summary{}, mapUniqueErr(err, "task")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Summary{}, ErrNotFound
	}
	return taskSummary(tx, id)
}

// nullStrPtrOrNil binds a *string into a nullable column on insert: a nil pointer
// or a pointer to "" is SQL NULL (the subject ref is optional, PLAN.md §3).
func nullStrPtrOrNil(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// ── reads ────────────────────────────────────────────────────────────────────

// taskSummary re-reads the row and builds the trimmed search summary.
func taskSummary(tx *sql.Tx, id string) (Summary, error) {
	var title, status, updated string
	var due sql.NullString
	err := tx.QueryRow(`SELECT title, status, due_at, updated_at FROM tasks WHERE id = ?`, id).
		Scan(&title, &status, &due, &updated)
	if err != nil {
		return Summary{}, fmt.Errorf("task summary: %w", err)
	}
	s := Summary{ID: id, Type: "task", Label: title, UpdatedAt: updated, sortKey: parseTime(updated),
		Fields: map[string]any{"status": status}}
	if due.Valid {
		s.Fields["due_at"] = due.String
	}
	return s, nil
}

// Get composes the task card: self fields + the resolved subject reference, when
// one is set and still live (orphan tolerated — a soft-deleted/absent subject is
// simply omitted, PLAN.md §8).
func (taskStore) Get(tx *sql.Tx, id string) (Card, error) {
	var title, status, created, updated string
	var due, done, contactID, orgID, dealID sql.NullString
	err := tx.QueryRow(`
		SELECT title, status, due_at, done_at, contact_id, org_id, deal_id, created_at, updated_at
		FROM tasks WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&title, &status, &due, &done, &contactID, &orgID, &dealID, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	card := Card{"id": id, "type": "task", "title": title, "status": status,
		"created_at": created, "updated_at": updated}
	if due.Valid {
		card["due_at"] = due.String
	}
	if done.Valid {
		card["done_at"] = done.String
	}
	if contactID.Valid {
		card["contact_id"] = contactID.String
	}
	if orgID.Valid {
		card["org_id"] = orgID.String
	}
	if dealID.Valid {
		card["deal_id"] = dealID.String
	}

	subject, err := taskSubjectRef(tx, contactID, orgID, dealID)
	if err != nil {
		return nil, err
	}
	if subject != nil {
		card["subject"] = subject
	}
	return card, nil
}

// taskSubjectRef probes whichever subject FK is set and returns a compact
// {type,id,label} reference (label = contact display_name / org name / deal
// name). Only a live subject row yields a ref; a soft-deleted/absent subject
// returns nil (orphan tolerated, PLAN.md §8). At most one ref is returned,
// contact → org → deal in precedence.
func taskSubjectRef(tx *sql.Tx, contactID, orgID, dealID sql.NullString) (map[string]any, error) {
	probe := func(typ, table, labelCol, id string) (map[string]any, error) {
		var label string
		err := tx.QueryRow(`SELECT `+labelCol+` FROM `+table+` WHERE id = ? AND deleted_at IS NULL`, id).Scan(&label)
		switch err {
		case nil:
			return map[string]any{"type": typ, "id": id, "label": label}, nil
		case sql.ErrNoRows:
			return nil, nil
		default:
			return nil, fmt.Errorf("task subject ref (%s): %w", typ, err)
		}
	}
	switch {
	case contactID.Valid:
		return probe("contact", "contacts", "display_name", contactID.String)
	case orgID.Valid:
		return probe("organization", "organizations", "name", orgID.String)
	case dealID.Valid:
		return probe("deal", "deals", "name", dealID.String)
	}
	return nil, nil
}

// Search matches live tasks by title substring, with optional status / contact_id
// / org_id / deal_id filters, recency-ordered.
func (taskStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	where := []string{"deleted_at IS NULL"}
	var args []any
	if q := strings.TrimSpace(p.Query); q != "" {
		where = append(where, "title LIKE ? COLLATE NOCASE")
		args = append(args, "%"+q+"%")
	}
	for _, f := range []struct{ key, col string }{
		{"status", "status"},
		{"contact_id", "contact_id"},
		{"org_id", "org_id"},
		{"deal_id", "deal_id"},
	} {
		if v, ok := filterString(p.Filters, f.key); ok {
			where = append(where, f.col+" = ?")
			args = append(args, v)
		}
	}
	pred, pArgs, err := keysetAfter(tx, "tasks", p.AfterID)
	if err != nil {
		return nil, err
	}
	where = append(where, pred)
	args = append(args, pArgs...)
	args = append(args, p.limit())
	rows, err := tx.Query(
		`SELECT id, title, status, due_at, updated_at FROM tasks WHERE `+strings.Join(where, " AND ")+
			` ORDER BY updated_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("search tasks: %w", err)
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var id, title, status, updated string
		var due sql.NullString
		if err := rows.Scan(&id, &title, &status, &due, &updated); err != nil {
			return nil, err
		}
		s := Summary{ID: id, Type: "task", Label: title, UpdatedAt: updated, sortKey: parseTime(updated),
			Fields: map[string]any{"status": status}}
		if due.Valid {
			s.Fields["due_at"] = due.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete soft-deletes the task. Shallow (PLAN.md §8): tasks own no children.
func (taskStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	ts := fmtTime(at)
	res, err := tx.Exec(`UPDATE tasks SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ts, ts, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
