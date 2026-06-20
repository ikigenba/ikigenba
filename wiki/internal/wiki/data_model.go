package wiki

import (
	"context"
	"database/sql"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Subject is a canonical entity, event, or concept in the wiki.
type Subject struct {
	ID       string
	Name     string
	NormName string
	Type     string
}

// Claim is an extracted statement about a subject.
type Claim struct {
	ID        string
	SubjectID string
	JobID     string
	Body      string
}

// Page is a generated wiki page for a subject.
type Page struct {
	ID        string
	SubjectID string
	Title     string
	Body      string
}

// Job is a phase-1 wiki data-model job.
type Job struct {
	ID     string
	Status string
}

func normalize(name string) string {
	s := norm.NFKC.String(name)
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	return stripDiacritics(s)
}

func stripDiacritics(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

// JobStore persists wiki jobs.
type JobStore struct {
	db *sql.DB
}

func NewJobStore(db *sql.DB) *JobStore {
	return &JobStore{db: db}
}

func (s *JobStore) Save(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, status) VALUES (?, ?)`,
		job.ID, job.Status)
	return err
}

func (s *JobStore) Get(ctx context.Context, id string) (Job, error) {
	var job Job
	err := s.db.QueryRowContext(ctx,
		`SELECT id, status FROM jobs WHERE id = ?`, id).
		Scan(&job.ID, &job.Status)
	return job, err
}

// SubjectStore persists canonical subjects.
type SubjectStore struct {
	db *sql.DB
}

func NewSubjectStore(db *sql.DB) *SubjectStore {
	return &SubjectStore{db: db}
}

func (s *SubjectStore) Save(ctx context.Context, subject Subject) error {
	normName := subject.NormName
	if normName == "" {
		normName = normalize(subject.Name)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subjects (id, name, norm_name, type) VALUES (?, ?, ?, ?)`,
		subject.ID, subject.Name, normName, subject.Type)
	return err
}

func (s *SubjectStore) GetByNormName(ctx context.Context, name string) (Subject, error) {
	var subject Subject
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, norm_name, type FROM subjects WHERE norm_name = ?`,
		normalize(name)).
		Scan(&subject.ID, &subject.Name, &subject.NormName, &subject.Type)
	return subject, err
}

// ClaimStore persists extracted claims.
type ClaimStore struct {
	db *sql.DB
}

func NewClaimStore(db *sql.DB) *ClaimStore {
	return &ClaimStore{db: db}
}

func (s *ClaimStore) Save(ctx context.Context, claim Claim) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO claims (id, subject_id, job_id, body) VALUES (?, ?, ?, ?)`,
		claim.ID, claim.SubjectID, claim.JobID, claim.Body)
	return err
}

func (s *ClaimStore) ListBySubject(ctx context.Context, subjectID string) ([]Claim, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, subject_id, job_id, body FROM claims WHERE subject_id = ? ORDER BY id`,
		subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claims []Claim
	for rows.Next() {
		var claim Claim
		if err := rows.Scan(&claim.ID, &claim.SubjectID, &claim.JobID, &claim.Body); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

// PageStore persists pages and synchronizes their external-content FTS rows.
type PageStore struct {
	db *sql.DB
}

func NewPageStore(db *sql.DB) *PageStore {
	return &PageStore{db: db}
}

func (s *PageStore) Upsert(ctx context.Context, page Page) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldRowID int64
	var oldTitle, oldBody string
	err = tx.QueryRowContext(ctx,
		`SELECT rowid, title, body FROM pages WHERE id = ?`,
		page.ID).
		Scan(&oldRowID, &oldTitle, &oldBody)
	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO pages_fts (pages_fts, rowid, title, body) VALUES ('delete', ?, ?, ?)`,
			oldRowID, oldTitle, oldBody); err != nil {
			return err
		}
	case err == sql.ErrNoRows:
	default:
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pages (id, subject_id, title, body)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			subject_id = excluded.subject_id,
			title = excluded.title,
			body = excluded.body`,
		page.ID, page.SubjectID, page.Title, page.Body); err != nil {
		return err
	}

	var rowID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT rowid FROM pages WHERE id = ?`,
		page.ID).
		Scan(&rowID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO pages_fts (rowid, title, body) VALUES (?, ?, ?)`,
		rowID, page.Title, page.Body); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PageStore) Get(ctx context.Context, id string) (Page, error) {
	var page Page
	err := s.db.QueryRowContext(ctx,
		`SELECT id, subject_id, title, body FROM pages WHERE id = ?`, id).
		Scan(&page.ID, &page.SubjectID, &page.Title, &page.Body)
	return page, err
}

func (s *PageStore) Search(ctx context.Context, query string, limit int) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.subject_id, p.title, p.body
		FROM pages_fts
		JOIN pages AS p ON p.rowid = pages_fts.rowid
		WHERE pages_fts MATCH ?
		ORDER BY rank
		LIMIT ?`,
		query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var page Page
		if err := rows.Scan(&page.ID, &page.SubjectID, &page.Title, &page.Body); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, rows.Err()
}
