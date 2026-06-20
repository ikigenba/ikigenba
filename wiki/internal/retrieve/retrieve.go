// Package retrieve owns wiki search and context retrieval.
package retrieve

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Hit is a whole-page retrieval result, lane-agnostic.
type Hit struct {
	SubjectID string
	PageID    string
	Version   int
	Score     float64
	Snippet   string
	Title     string
}

// Retriever is the search seam ask and search depend on.
type Retriever interface {
	Search(ctx context.Context, query string, k int) ([]Hit, error)
}

// NewKeyword returns an FTS5-backed keyword retriever.
func NewKeyword(db *sql.DB) Retriever {
	return keywordRetriever{db: db}
}

// SearchLimits clamps caller-provided result counts into the retrieval contract.
type SearchLimits struct {
	Default int
	Cap     int
}

// Resolve clamps k so k<=0 resolves to Default and k>Cap resolves to Cap.
func (l SearchLimits) Resolve(k int) int {
	if k <= 0 {
		return l.Default
	}
	if k > l.Cap {
		return l.Cap
	}
	return k
}

// Service composes registry-first pinning over a retriever.
type Service struct {
	db     *sql.DB
	r      Retriever
	limits SearchLimits
}

// NewService creates a retrieval service.
func NewService(db *sql.DB, r Retriever, limits SearchLimits) *Service {
	return &Service{db: db, r: r, limits: limits}
}

// Search returns registry-pinned exact subject matches ahead of retriever hits.
func (s *Service) Search(ctx context.Context, query string, k int) ([]Hit, error) {
	limit := s.limits.Resolve(k)
	if limit <= 0 || strings.TrimSpace(query) == "" {
		return nil, nil
	}

	hits, err := s.r.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	pin, ok, err := s.pinnedHit(ctx, query)
	if err != nil {
		return nil, err
	}
	if !ok {
		return trimHits(hits, limit), nil
	}

	out := make([]Hit, 0, limit)
	out = append(out, pin)
	for _, hit := range hits {
		if hit.PageID == pin.PageID {
			continue
		}
		out = append(out, hit)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (s *Service) pinnedHit(ctx context.Context, query string) (Hit, bool, error) {
	if s.db == nil {
		return Hit{}, false, nil
	}

	versionExpr, err := pageVersionExpr(ctx, s.db)
	if err != nil {
		return Hit{}, false, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT p.subject_id, p.subject_id, `+versionExpr+`, p.title, p.body
		FROM subjects AS s
		JOIN pages AS p ON p.subject_id = s.id
		WHERE s.norm_name = ?
		ORDER BY p.id
		LIMIT 1`,
		normalize(query))

	var hit Hit
	var body string
	if err := row.Scan(&hit.SubjectID, &hit.PageID, &hit.Version, &hit.Title, &body); err != nil {
		if err == sql.ErrNoRows {
			return Hit{}, false, nil
		}
		return Hit{}, false, err
	}
	hit.Snippet = truncate(body, 240)
	return hit, true, nil
}

type keywordRetriever struct {
	db *sql.DB
}

func (r keywordRetriever) Search(ctx context.Context, query string, k int) ([]Hit, error) {
	if r.db == nil || k <= 0 || strings.TrimSpace(query) == "" {
		return nil, nil
	}

	versionExpr, err := pageVersionExpr(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.subject_id,
		       p.subject_id,
		       `+versionExpr+`,
		       bm25(pages_fts) AS score,
		       snippet(pages_fts, -1, '', '', '...', 24) AS snippet,
		       p.title
		FROM pages_fts
		JOIN pages AS p ON p.rowid = pages_fts.rowid
		WHERE pages_fts MATCH ?
		ORDER BY score, p.id
		LIMIT ?`,
		ftsPhrase(query), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var hit Hit
		if err := rows.Scan(&hit.SubjectID, &hit.PageID, &hit.Version, &hit.Score, &hit.Snippet, &hit.Title); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func pageVersionExpr(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(pages)`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return "", err
		}
		if name == "version" {
			return "p.version", nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "0", nil
}

func trimHits(hits []Hit, limit int) []Hit {
	if len(hits) <= limit {
		return hits
	}
	return hits[:limit]
}

func ftsPhrase(query string) string {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return `""`
	}
	for i, term := range terms {
		terms[i] = strings.ReplaceAll(term, `"`, `""`)
	}
	return `"` + strings.Join(terms, " ") + `"`
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

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if maxRunes == 0 {
			return b.String()
		}
		b.WriteRune(r)
		maxRunes--
		if maxRunes == 0 {
			return b.String() + "..."
		}
		if i == len(s)-1 {
			return b.String()
		}
	}
	return b.String()
}
