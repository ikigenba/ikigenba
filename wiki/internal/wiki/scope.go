package wiki

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const InstructionsCharCap = 4000

var (
	ErrInvalidScopeName    = errors.New("wiki: invalid scope name")
	ErrScopeNotFound       = errors.New("wiki: scope not found")
	ErrScopeExists         = errors.New("wiki: scope already exists")
	ErrScopeIsDefault      = errors.New("wiki: the default scope cannot be deleted")
	ErrInstructionsTooLong = errors.New("wiki: scope instructions exceed 4000 characters")
)

var scopeNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// ValidateScopeName rejects invalid scope names without normalizing them.
func ValidateScopeName(name string) error {
	if !scopeNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidScopeName, name)
	}
	return nil
}

// Scope is an entry in the explicit scope registry.
type Scope struct {
	Name         string
	Visibility   string
	Instructions string
	CreatedAt    int64
}

// ScopeStore persists the scope registry and owns scope deletion.
type ScopeStore struct {
	read          *sql.DB
	write         *sql.DB
	AskInvalidate func(scope string)
}

func NewScopeStore(db any) *ScopeStore {
	c := mustConns(db)
	return &ScopeStore{read: c.Read, write: c.Write}
}

func (s *ScopeStore) Create(ctx context.Context, name string) (Scope, error) {
	if err := ValidateScopeName(name); err != nil {
		return Scope{}, err
	}
	_, err := s.write.ExecContext(ctx, `
		INSERT INTO scopes (name, visibility, created_at)
		VALUES (?, 'private', unixepoch())`, name)
	if err != nil {
		if isUniqueConstraint(err) {
			return Scope{}, fmt.Errorf("%w: %s", ErrScopeExists, name)
		}
		return Scope{}, fmt.Errorf("create scope %q: %w", name, err)
	}
	return s.Get(ctx, name)
}

func (s *ScopeStore) Get(ctx context.Context, name string) (Scope, error) {
	var scope Scope
	err := s.read.QueryRowContext(ctx, `
		SELECT name, visibility, instructions, created_at FROM scopes WHERE name = ?`, name).
		Scan(&scope.Name, &scope.Visibility, &scope.Instructions, &scope.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, fmt.Errorf("%w: %s", ErrScopeNotFound, name)
	}
	if err != nil {
		return Scope{}, fmt.Errorf("get scope %q: %w", name, err)
	}
	return scope, nil
}

func (s *ScopeStore) List(ctx context.Context) ([]Scope, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT name, visibility, instructions, created_at FROM scopes ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}
	defer rows.Close()
	var scopes []Scope
	for rows.Next() {
		var scope Scope
		if err := rows.Scan(&scope.Name, &scope.Visibility, &scope.Instructions, &scope.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}
	return scopes, nil
}

// SetInstructions stores the scope's standing inference instructions byte-exact.
func (s *ScopeStore) SetInstructions(ctx context.Context, name, text string) error {
	if utf8.RuneCountInString(text) > InstructionsCharCap {
		return ErrInstructionsTooLong
	}
	result, err := s.write.ExecContext(ctx, `UPDATE scopes SET instructions = ? WHERE name = ?`, text, name)
	if err != nil {
		return fmt.Errorf("set scope %q instructions: %w", name, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set scope %q instructions: %w", name, err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrScopeNotFound, name)
	}
	if s.AskInvalidate != nil {
		s.AskInvalidate(name)
	}
	return nil
}

// ComposeSystem appends scope instructions to a stage's base system prompt.
func ComposeSystem(base, instructions string) string {
	if instructions == "" {
		return base
	}
	return base + "\n\nScope instructions (these override the general rules above where they conflict):\n" + instructions
}

func (s *ScopeStore) SetVisibility(ctx context.Context, name, visibility string) error {
	result, err := s.write.ExecContext(ctx, `UPDATE scopes SET visibility = ? WHERE name = ?`, visibility, name)
	if err != nil {
		return fmt.Errorf("set scope %q visibility: %w", name, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set scope %q visibility: %w", name, err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrScopeNotFound, name)
	}
	return nil
}

func (s *ScopeStore) Delete(ctx context.Context, name string) error {
	if name == "default" {
		return ErrScopeIsDefault
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete scope %q: begin: %w", name, err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM scopes WHERE name = ?`, name).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrScopeNotFound, name)
	} else if err != nil {
		return fmt.Errorf("delete scope %q: lookup: %w", name, err)
	}
	statements := []string{
		`DELETE FROM page_embeddings WHERE subject_id IN (SELECT id FROM subjects WHERE scope = ?)`,
		`DELETE FROM pages WHERE subject_id IN (SELECT id FROM subjects WHERE scope = ?)`,
		`DELETE FROM claims WHERE subject_id IN (SELECT id FROM subjects WHERE scope = ?)`,
		`DELETE FROM aliases WHERE scope = ?`,
		`DELETE FROM subject_merges WHERE job_id IN (SELECT id FROM jobs WHERE scope = ?)`,
		`DELETE FROM subjects WHERE scope = ?`,
		`DELETE FROM jobs WHERE scope = ?`,
		`DELETE FROM scopes WHERE name = ?`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement, name); err != nil {
			return fmt.Errorf("delete scope %q content: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pages_fts(pages_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("delete scope %q search content: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete scope %q: commit: %w", name, err)
	}
	if s.AskInvalidate != nil {
		s.AskInvalidate(name)
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "PRIMARY KEY")
}
