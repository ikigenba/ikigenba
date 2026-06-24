package wiki

import (
	"context"

	"wiki/internal/page"
)

// Alias records a historical or alternate name that resolves to a canonical subject.
type Alias struct {
	NormName  string
	SubjectID string
	Name      string
	CreatedBy string
	CreatedAt string
}

// AliasStore persists subject aliases.
type AliasStore struct {
	db sqlStore
}

func NewAliasStore(db sqlStore) *AliasStore {
	return &AliasStore{db: db}
}

func (a *AliasStore) Insert(ctx context.Context, al Alias) error {
	normName := al.NormName
	if normName == "" {
		normName = al.Name
	}
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO aliases (norm_name, subject_id, name, created_by, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		normalize(normName), al.SubjectID, al.Name, al.CreatedBy, al.CreatedAt)
	return err
}

func (a *AliasStore) RepointSubject(ctx context.Context, from, to string) error {
	_, err := a.db.ExecContext(ctx,
		`UPDATE aliases SET subject_id = ? WHERE subject_id = ?`, to, from)
	return err
}

func (a *AliasStore) GetByNormName(ctx context.Context, normName string) (Alias, error) {
	var al Alias
	err := a.db.QueryRowContext(ctx, `
		SELECT norm_name, subject_id, name, created_by, created_at
		FROM aliases
		WHERE norm_name = ?`,
		normalize(normName)).
		Scan(&al.NormName, &al.SubjectID, &al.Name, &al.CreatedBy, &al.CreatedAt)
	return al, err
}

func (a *AliasStore) ListMerges(ctx context.Context, p page.Params) ([]Alias, string, error) {
	cursor, err := decodeCursor(p.Cursor, 2)
	if err != nil {
		return nil, "", err
	}
	limit := p.ResolvedLimit()
	var args []any
	query := `
		SELECT norm_name, subject_id, name, created_by, created_at
		FROM aliases
		WHERE 1 = 1`
	if len(cursor) > 0 {
		query += `
		  AND (created_at < ? OR (created_at = ? AND norm_name < ?))`
		args = append(args, cursor[0], cursor[0], cursor[1])
	}
	query += `
		ORDER BY created_at DESC, norm_name DESC
		LIMIT ?`
	args = append(args, limit+1)

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var aliases []Alias
	for rows.Next() {
		var al Alias
		if err := rows.Scan(&al.NormName, &al.SubjectID, &al.Name, &al.CreatedBy, &al.CreatedAt); err != nil {
			return nil, "", err
		}
		aliases = append(aliases, al)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return pageAliases(aliases, limit), nextAliasCursor(aliases, limit), nil
}
