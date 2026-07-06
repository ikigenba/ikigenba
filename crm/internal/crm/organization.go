package crm

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"appkit/logging"
)

// organizationStore is the SQL-only data layer for the organizations table.
// Every method takes a *sql.Tx so the dispatcher composes the transaction
// (PLAN.md §8). Normalization and rich validation happen at the dispatcher seam;
// Save trusts the typed input but enforces the create-required NOT NULL columns
// with a corrective message rather than surfacing a raw constraint error.
type organizationStore struct{}

func (organizationStore) Save(tx *sql.Tx, id string, in OrganizationInput, now time.Time) (Summary, error) {
	if id == "" {
		return orgInsert(tx, in, now)
	}
	return orgUpdate(tx, id, in, now)
}

func orgInsert(tx *sql.Tx, in OrganizationInput, now time.Time) (Summary, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return Summary{}, invalid("name", "organization name is required")
	}
	id := logging.NewULID()
	ts := fmtTime(now)
	_, err := tx.Exec(
		`INSERT INTO organizations (id, name, domain, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, NULL)`,
		id, *in.Name, domainVal(in.Domain), ts, ts,
	)
	if err != nil {
		return Summary{}, mapUniqueErr(err, "organization")
	}
	return orgSummary(tx, id)
}

func orgUpdate(tx *sql.Tx, id string, in OrganizationInput, now time.Time) (Summary, error) {
	sets := []string{"updated_at = ?"}
	args := []any{fmtTime(now)}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return Summary{}, invalid("name", "organization name must not be empty")
		}
		sets = append(sets, "name = ?")
		args = append(args, *in.Name)
	}
	if in.Domain != nil {
		sets = append(sets, "domain = ?")
		args = append(args, nullStrOrNil(*in.Domain))
	}
	args = append(args, id)
	res, err := tx.Exec(`UPDATE organizations SET `+strings.Join(sets, ", ")+` WHERE id = ? AND deleted_at IS NULL`, args...)
	if err != nil {
		return Summary{}, mapUniqueErr(err, "organization")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Summary{}, ErrNotFound
	}
	return orgSummary(tx, id)
}

// domainVal binds the domain column on insert: a nil or empty domain is NULL.
func domainVal(d *string) any {
	if d == nil || *d == "" {
		return nil
	}
	return *d
}

// orgSummary re-reads the row and builds the trimmed search summary.
func orgSummary(tx *sql.Tx, id string) (Summary, error) {
	var name string
	var domain sql.NullString
	var updated string
	err := tx.QueryRow(`SELECT name, domain, updated_at FROM organizations WHERE id = ?`, id).Scan(&name, &domain, &updated)
	if err != nil {
		return Summary{}, fmt.Errorf("org summary: %w", err)
	}
	s := Summary{ID: id, Type: "organization", Label: name, UpdatedAt: updated, sortKey: parseTime(updated)}
	if domain.Valid {
		s.Fields = map[string]any{"domain": domain.String}
	}
	return s, nil
}

// Get composes the organization card: self fields + {contacts, open deals, recent
// interactions} (PLAN.md §4).
func (organizationStore) Get(tx *sql.Tx, id string) (Card, error) {
	var name string
	var domain sql.NullString
	var created, updated string
	err := tx.QueryRow(
		`SELECT name, domain, created_at, updated_at FROM organizations WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&name, &domain, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	card := Card{"id": id, "type": "organization", "name": name, "created_at": created, "updated_at": updated}
	if domain.Valid {
		card["domain"] = domain.String
	}
	contacts, err := contactCardsByOrg(tx, id)
	if err != nil {
		return nil, err
	}
	card["contacts"] = contacts
	deals, err := openDealCardsByOrg(tx, id)
	if err != nil {
		return nil, err
	}
	card["open_deals"] = deals
	ints, err := recentInteractionCards(tx, "org_id", id, recentInteractionLimit)
	if err != nil {
		return nil, err
	}
	card["recent_interactions"] = ints
	return card, nil
}

// Search matches live organizations by name/domain substring, recency-ordered.
func (organizationStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	where := []string{"deleted_at IS NULL"}
	var args []any
	if q := strings.TrimSpace(p.Query); q != "" {
		like := "%" + q + "%"
		where = append(where, "(name LIKE ? COLLATE NOCASE OR domain LIKE ? COLLATE NOCASE)")
		args = append(args, like, like)
	}
	pred, pArgs, err := keysetAfter(tx, "organizations", p.AfterID)
	if err != nil {
		return nil, err
	}
	where = append(where, pred)
	args = append(args, pArgs...)
	args = append(args, p.limit())
	rows, err := tx.Query(
		`SELECT id, name, domain, updated_at FROM organizations WHERE `+strings.Join(where, " AND ")+
			` ORDER BY updated_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("search organizations: %w", err)
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var id, name, updated string
		var domain sql.NullString
		if err := rows.Scan(&id, &name, &domain, &updated); err != nil {
			return nil, err
		}
		s := Summary{ID: id, Type: "organization", Label: name, UpdatedAt: updated, sortKey: parseTime(updated)}
		if domain.Valid {
			s.Fields = map[string]any{"domain": domain.String}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete soft-deletes the organization row only. It is shallow (PLAN.md §8): an
// org owns no children, and contacts/deals/interactions that reference it are
// left intact and simply hidden from reads while it is deleted.
func (organizationStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	res, err := tx.Exec(
		`UPDATE organizations SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		fmtTime(at), fmtTime(at), id)
	if err != nil {
		return fmt.Errorf("delete organization: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
