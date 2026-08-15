package wiki

import (
	"context"
	"fmt"
	"wiki/internal/page"
)

// Alias records a historical or alternate name that resolves to a canonical subject.
type Alias struct {
	NormName   string
	SubjectID  string
	Name       string
	OwnerID    string
	OwnerEmail string
	CreatedAt  string
}

// AliasStore persists subject aliases.
type AliasStore struct {
	db sqlStore
}

func NewAliasStore(db sqlStore) *AliasStore {
	return &AliasStore{db: db}
}

func (a *AliasStore) Insert(ctx context.Context, args ...any) error {
	scope, al, err := aliasArgs(args)
	if err != nil {
		return err
	}
	if err := requireScope(ctx, a.db, scope); err != nil {
		return err
	}
	normName := al.NormName
	if normName == "" {
		normName = al.Name
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO aliases (scope, norm_name, subject_id, name, owner_id, owner_email, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		scope, Normalize(normName), al.SubjectID, al.Name, al.OwnerID, al.OwnerEmail, al.CreatedAt)
	return err
}

func (a *AliasStore) RepointSubject(ctx context.Context, from, to string) error {
	_, err := a.db.ExecContext(ctx,
		`UPDATE aliases SET subject_id = ? WHERE subject_id = ?`, to, from)
	return err
}

func (a *AliasStore) GetByNormName(ctx context.Context, args ...string) (Alias, error) {
	scope, normName, err := scopedStringArgs(args)
	if err != nil {
		return Alias{}, err
	}
	if err := requireScope(ctx, a.db, scope); err != nil {
		return Alias{}, err
	}
	var al Alias
	err = a.db.QueryRowContext(ctx, `
		SELECT norm_name, subject_id, name, owner_id, owner_email, created_at
		FROM aliases
		WHERE scope = ? AND norm_name = ?`,
		scope, Normalize(normName)).
		Scan(&al.NormName, &al.SubjectID, &al.Name, &al.OwnerID, &al.OwnerEmail, &al.CreatedAt)
	return al, err
}

func (a *AliasStore) ListAll(ctx context.Context) ([]Alias, error) {
	return a.ListAllInScope(ctx, "default")
}

func (a *AliasStore) ListAllInScope(ctx context.Context, scope string) ([]Alias, error) {
	if err := requireScope(ctx, a.db, scope); err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT norm_name, subject_id, name, owner_id, owner_email, created_at
		FROM aliases
		WHERE scope = ?
		ORDER BY norm_name`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []Alias
	for rows.Next() {
		var al Alias
		if err := rows.Scan(&al.NormName, &al.SubjectID, &al.Name, &al.OwnerID, &al.OwnerEmail, &al.CreatedAt); err != nil {
			return nil, err
		}
		aliases = append(aliases, al)
	}
	return aliases, rows.Err()
}

func aliasArgs(args []any) (string, Alias, error) {
	if len(args) == 1 {
		if al, ok := args[0].(Alias); ok {
			return "default", al, nil
		}
	}
	if len(args) == 2 {
		scope, scopeOK := args[0].(string)
		al, aliasOK := args[1].(Alias)
		if scopeOK && aliasOK {
			return scope, al, nil
		}
	}
	return "", Alias{}, fmt.Errorf("wiki: expected alias or scope and alias")
}

func (a *AliasStore) ListMerges(ctx context.Context, p page.Params) ([]Alias, string, error) {
	return a.ListMergesInScope(ctx, "default", p)
}

// ListMergesInScope returns merge audit rows belonging to one explicit scope.
func (a *AliasStore) ListMergesInScope(ctx context.Context, scope string, p page.Params) ([]Alias, string, error) {
	if err := requireScope(ctx, a.db, scope); err != nil {
		return nil, "", err
	}
	cursor, err := decodeCursor(p.Cursor, 2)
	if err != nil {
		return nil, "", err
	}
	limit := p.ResolvedLimit()
	args := []any{scope}
	query := `
		SELECT norm_name, subject_id, name, owner_id, owner_email, created_at
		FROM aliases
		WHERE scope = ?`
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
		if err := rows.Scan(&al.NormName, &al.SubjectID, &al.Name, &al.OwnerID, &al.OwnerEmail, &al.CreatedAt); err != nil {
			return nil, "", err
		}
		aliases = append(aliases, al)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return pageAliases(aliases, limit), nextAliasCursor(aliases, limit), nil
}
