// Package sites is the slug/registry domain: CRUD over the `sites` table plus
// the path helpers (layout.go) that pin where each site lives under SITES_ROOT.
// Each row is one hosted static website keyed by its slug.
package sites

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// timeFormat is the canonical storage timestamp rendering (RFC3339-ish, UTC,
// nanosecond precision) — matches the suite's peer services so stored values
// round-trip identically across the box.
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// slugRe is the slug grammar pinned by migration 002_sites.sql: a leading
// lowercase-alnum char then up to 62 more lowercase-alnum-or-hyphen chars
// (1..63 total). Compiled once at package load.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// reservedNames are slugs the registry refuses defensively: they collide with
// reserved routes/dirs in the served tree. `.well-known` wouldn't match slugRe
// anyway (the dot), but we guard it explicitly so the intent is enforced and not
// merely incidental.
var reservedNames = map[string]bool{
	"mcp":         true,
	".well-known": true,
}

// Error sentinels. Callers match with errors.Is.
var (
	// ErrInvalidSlug — name fails the slug grammar (bad chars, too long, empty).
	ErrInvalidSlug = errors.New("sites: invalid slug")
	// ErrReservedName — name is syntactically valid but reserved.
	ErrReservedName = errors.New("sites: reserved name")
	// ErrExists — a row with that name already exists.
	ErrExists = errors.New("sites: already exists")
	// ErrNotFound — no row with that name.
	ErrNotFound = errors.New("sites: not found")
	// ErrInvalidVisibility — value is outside the closed visibility enum.
	ErrInvalidVisibility = errors.New("sites: invalid visibility")
	// ErrInvalidName — display name is empty, too long, or contains a control character.
	ErrInvalidName = errors.New("invalid site name")
)

// Visibility is the closed set of site visibility states stored on a row.
type Visibility string

const (
	Public   Visibility = "public"
	Private  Visibility = "private"
	Unlisted Visibility = "unlisted"
)

// ParseVisibility maps a wire value to the visibility enum.
func ParseVisibility(value string) (Visibility, error) {
	v := Visibility(value)
	switch v {
	case Public, Private, Unlisted:
		return v, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidVisibility, value)
	}
}

// Site mirrors one `sites` row. Slug is its address and Name its display label.
// SourcePath records the originating Dropbox
// subtree for a sync-managed site (empty ⇒ SQL NULL ⇒ hand-authored / not
// import-managed; ADR Decision 2).
type Site struct {
	Slug       string
	Name       string
	Visibility Visibility
	OwnerID    string
	OwnerEmail string
	SourcePath string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Store is the registry's data-access boundary over the `sites` table. Now is
// injected so tests can pin time; it defaults to time.Now. Layout is retained
// for composition sites that construct the store alongside filesystem layout.
type Store struct {
	db     *sql.DB
	Layout Layout
	Now    func() time.Time
}

// NewStore wraps an open *sql.DB with a zero (DefaultRoot) Layout. Now defaults
// to time.Now (UTC is applied at write time).
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, Now: time.Now}
}

// NewStoreWithLayout wraps an open *sql.DB and pins the Layout used at process
// wiring once the SITES_ROOT-derived Layout is available.
func NewStoreWithLayout(db *sql.DB, layout Layout) *Store {
	return &Store{db: db, Layout: layout, Now: time.Now}
}

// ValidateSlug exposes the slug grammar + reserved-name guard for callers that
// must pre-check a slug before a create attempt (the `sync` verb derives a slug
// from a source-path basename and wants a clean validation error rather than a
// raw create failure). It is the exported twin of validateName and returns the
// receive untrusted slugs. Store mutators trust their internal callers.
func ValidateSlug(name string) error { return validateName(name) }

// ValidateName trims surrounding Unicode whitespace and validates a display label.
func ValidateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", ErrInvalidName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalidName
		}
	}
	return name, nil
}

// validateName runs the slug grammar then the reserved-name guard. It returns
// ErrInvalidSlug or ErrReservedName (wrapped with the offending value).
func validateName(name string) error {
	if reservedNames[name] {
		return fmt.Errorf("%w: %q", ErrReservedName, name)
	}
	if !slugRe.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidSlug, name)
	}
	// Belt-and-suspenders: a reserved name with mixed/odd casing can't reach here
	// because slugRe already rejects it, but normalize-and-recheck makes the guard
	// independent of regex details.
	if reservedNames[strings.ToLower(name)] {
		return fmt.Errorf("%w: %q", ErrReservedName, name)
	}
	return nil
}

// fmtTime renders t as the canonical UTC storage string.
func fmtTime(t time.Time) string { return t.UTC().Format(timeFormat) }

// parseTime parses a stored timestamp; a malformed value yields the zero time
// (storage is always written by fmtTime, so this is defensive).
func parseTime(s string) time.Time {
	t, _ := time.Parse(timeFormat, s)
	return t
}

// Create inserts the caller-provided slug, display name, identity, and visibility
// verbatim; there is no store-side default. created_at/updated_at are set to now
// (UTC). Returns ErrExists if the slug is already taken.
func (s *Store) Create(ctx context.Context, slug, name, ownerID, ownerEmail string, visibility Visibility) (Site, error) {
	if _, err := ParseVisibility(string(visibility)); err != nil {
		return Site{}, err
	}
	now := s.Now().UTC()
	ts := fmtTime(now)
	// source_path is inserted NULL: a freshly created site is hand-authored until
	// a sync stamps it via SetSourcePath.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (slug, name, source_path, visibility, owner_id, owner_email, created_at, updated_at)
		 VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`,
		slug, name, visibility, ownerID, ownerEmail, ts, ts)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") ||
			strings.Contains(err.Error(), "PRIMARY KEY") {
			return Site{}, fmt.Errorf("%w: %q", ErrExists, slug)
		}
		return Site{}, fmt.Errorf("create site %q: %w", slug, err)
	}
	return Site{
		Slug:       slug,
		Name:       name,
		Visibility: visibility,
		OwnerID:    ownerID,
		OwnerEmail: ownerEmail,
		CreatedAt:  parseTime(ts),
		UpdatedAt:  parseTime(ts),
	}, nil
}

// Get fetches one site by slug. Returns ErrNotFound when absent.
func (s *Store) Get(ctx context.Context, slug string) (Site, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT slug, name, visibility, owner_id, owner_email, source_path, created_at, updated_at
		 FROM sites WHERE slug = ?`, slug)
	site, err := scanSite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	if err != nil {
		return Site{}, fmt.Errorf("get site %q: %w", slug, err)
	}
	return site, nil
}

// List returns every site ordered by slug (deterministic).
func (s *Store) List(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slug, name, visibility, owner_id, owner_email, source_path, created_at, updated_at
		 FROM sites ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, fmt.Errorf("list sites: %w", err)
		}
		out = append(out, site)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return out, nil
}

// Delete removes the row only. Filesystem/symlink teardown is a later phase.
// Returns ErrNotFound when no such row.
func (s *Store) Delete(ctx context.Context, slug string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sites WHERE slug = ?`, slug)
	if err != nil {
		return fmt.Errorf("delete site %q: %w", slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete site %q: %w", slug, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	return nil
}

// SetSourcePath stamps the originating Dropbox subtree on an existing site row
// (and bumps updated_at). The sites `sync` verb calls this after create-or-reuse
// to mark the site import-managed and record its provenance. Returns ErrNotFound
// when no such row.
func (s *Store) SetSourcePath(ctx context.Context, slug, sourcePath string) error {
	ts := fmtTime(s.Now().UTC())
	res, err := s.db.ExecContext(ctx,
		`UPDATE sites SET source_path = ?, updated_at = ? WHERE slug = ?`,
		sourcePath, ts, slug)
	if err != nil {
		return fmt.Errorf("set source_path %q: %w", slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set source_path %q: %w", slug, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	return nil
}

// SetVisibility changes visibility and optionally renames the slug in one
// statement. Moving files is handled by Layout.Move at the caller boundary.
func (s *Store) SetVisibility(ctx context.Context, slug string, visibility Visibility, newSlug string) error {
	if _, err := ParseVisibility(string(visibility)); err != nil {
		return err
	}
	if newSlug == "" {
		newSlug = slug
	}
	ts := fmtTime(s.Now().UTC())
	res, err := s.db.ExecContext(ctx,
		`UPDATE sites SET slug = ?, visibility = ?, updated_at = ? WHERE slug = ?`,
		newSlug, visibility, ts, slug)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return fmt.Errorf("%w: %q", ErrExists, newSlug)
		}
		return fmt.Errorf("set visibility %q: %w", slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set visibility %q: %w", slug, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	return nil
}

// Rename updates only the display label and timestamp.
func (s *Store) Rename(ctx context.Context, slug, name string) error {
	ts := fmtTime(s.Now().UTC())
	res, err := s.db.ExecContext(ctx, `UPDATE sites SET name = ?, updated_at = ? WHERE slug = ?`, name, ts, slug)
	if err != nil {
		return fmt.Errorf("rename site %q: %w", slug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rename site %q: %w", slug, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	return nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanSite maps a sites row into a Site, translating the nullable source_path.
func scanSite(sc rowScanner) (Site, error) {
	var (
		slug, name, createdAt, updatedAt string
		visibility                       string
		sourcePath                       sql.NullString
		ownerID, ownerEmail              string
	)
	if err := sc.Scan(&slug, &name, &visibility, &ownerID, &ownerEmail, &sourcePath, &createdAt, &updatedAt); err != nil {
		return Site{}, err
	}
	v, err := ParseVisibility(visibility)
	if err != nil {
		return Site{}, err
	}
	site := Site{
		Slug:       slug,
		Name:       name,
		Visibility: v,
		OwnerID:    ownerID,
		OwnerEmail: ownerEmail,
		CreatedAt:  parseTime(createdAt),
		UpdatedAt:  parseTime(updatedAt),
	}
	// NULL source_path (hand-authored) reads back as the empty string.
	if sourcePath.Valid {
		site.SourcePath = sourcePath.String
	}
	return site, nil
}
