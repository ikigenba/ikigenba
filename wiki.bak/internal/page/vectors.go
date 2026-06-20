package page

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

// The page_vectors store (design §9.3): the embedding lane's durable side. One
// row per page — a single vector over (canonical name + alias list + truncated
// body) — keyed by subject. The vector is stored as a little-endian float32 BLOB
// (4 bytes per dimension); the model column carries the provider model id + dims
// (e.g. "text-embedding-3-large@1024") so a model/dims change makes prior rows
// read-invalid (cross-model cosine is garbage — only model==current rows serve).
//
// `embedded_version` records pages.version at embed time. It drives the catch-up
// WORK LIST ONLY (re-embed a page whose version advanced past the embedded one);
// reads never compare it to pages.version — a stale vector serves until replaced
// (design §9.3: "Stale vectors serve until replaced").

// encodeVector packs a float32 slice into a little-endian BLOB (4 bytes/dim).
func encodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeVector unpacks a little-endian float32 BLOB. A length not divisible by 4
// is a corrupt row and yields an error.
func decodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("page: vector blob length %d not a multiple of 4", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v, nil
}

// PageVector is one stored page embedding: the subject it indexes and its vector.
// Type/Kind/Title/Body are NOT carried here — a vector hit resolves to its whole
// page through the registry like any other hit.
type PageVector struct {
	Subject string
	Vector  []float32
}

// VectorWork is one page the catch-up embedder must (re-)embed (design §9.3 work
// list): the subject, the text to embed (canonical name + aliases + truncated
// body), and the pages.version that text was read at (stamped as embedded_version
// on write so the next sweep knows this row is current).
type VectorWork struct {
	Subject string
	Text    string
	Version int
}

// EmbedTextChars is the body-truncation length (in bytes, cut on a rune boundary)
// for the embedding unit (design
// §9.3: "canonical name + alias list + body (truncated)"). A whole page can be
// long; the embedding budget is bounded, and the identity-bearing lead carries
// the signal. Kept here (not config) — it is the embed UNIT definition, not a
// retrieval knob; changing it changes what a vector means, a model-equivalent
// decision the catch-up worker absorbs via re-embed, not a sweep tunable.
const EmbedTextChars = 4000

// UpsertVector writes (or replaces) one page's vector, stamping the model id and
// the version the embedded text was read at. Called only by the catch-up embedder
// (design §9.3: the write path is async catch-up, NEVER the integration commit).
func (s *Store) UpsertVector(ctx context.Context, subject string, embeddedVersion int, model string, vector []float32) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO page_vectors (subject, embedded_version, model, vector)
		VALUES (?,?,?,?)
		ON CONFLICT(subject) DO UPDATE SET
			embedded_version = excluded.embedded_version,
			model            = excluded.model,
			vector           = excluded.vector`,
		subject, embeddedVersion, model, encodeVector(vector))
	if err != nil {
		return fmt.Errorf("page: upsert vector %q: %w", subject, err)
	}
	return nil
}

// VectorWorkList returns the pages whose vector is missing, behind the page's
// current version, or embedded under a different model (design §9.3 work-list
// query). These are exactly the rows the catch-up embedder must (re-)embed; a
// page already current under `model` is skipped. The embedding text is assembled
// here (canonical name + normalized alias list + truncated body) so the worker
// embeds the same unit a search query is later compared against. `limit<=0`
// means no cap (one full sweep).
func (s *Store) VectorWorkList(ctx context.Context, model string, limit int) ([]VectorWork, error) {
	q := `
		SELECT p.subject, p.version, s.canonical_name, COALESCE(p.body, '')
		FROM pages p
		JOIN subjects s ON s.id = p.subject
		LEFT JOIN page_vectors v ON v.subject = p.subject
		WHERE v.subject IS NULL
		   OR v.embedded_version < p.version
		   OR v.model <> ?
		ORDER BY p.subject`
	args := []any{model}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("page: vector work list: %w", err)
	}
	// Drain the work rows FULLY before issuing any per-subject alias subquery: the
	// alias lookups run on the same *sql.DB, and holding the outer rows open across
	// a nested query can starve a single-connection pool (a self-deadlock). Collect
	// first, then resolve aliases + assemble text with the cursor closed.
	type row struct {
		subject, canonical, body string
		version                  int
	}
	var raw []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.subject, &r.version, &r.canonical, &r.body); err != nil {
			rows.Close()
			return nil, fmt.Errorf("page: vector work scan: %w", err)
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := make([]VectorWork, 0, len(raw))
	for _, r := range raw {
		aliases, err := s.aliasKeys(ctx, r.subject)
		if err != nil {
			return nil, err
		}
		out = append(out, VectorWork{
			Subject: r.subject,
			Text:    embedText(r.canonical, aliases, r.body),
			Version: r.version,
		})
	}
	return out, nil
}

// aliasKeys returns a subject's full normalized alias-key set in stable order.
func (s *Store) aliasKeys(ctx context.Context, subject string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT norm FROM aliases WHERE subject_id = ? ORDER BY norm`, subject)
	if err != nil {
		return nil, fmt.Errorf("page: alias keys %q: %w", subject, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("page: alias key scan: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// embedText assembles the embedding unit (design §9.3): canonical name, then the
// alias keys, then the truncated body — joined by newlines so the embedder sees
// the identity-bearing lead first. The body is truncated on a rune boundary to
// EmbedTextChars.
func embedText(canonical string, aliases []string, body string) string {
	parts := make([]string, 0, len(aliases)+2)
	parts = append(parts, canonical)
	parts = append(parts, aliases...)
	parts = append(parts, truncateRunes(body, EmbedTextChars))
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out
}

// LoadVectors loads every CURRENT-MODEL page vector for the brute-force cosine
// scan (design §9.3: "brute-force cosine scan in Go"). Only rows whose model
// matches the configured model are returned — a row embedded under a different
// model is read-invalid (cross-model cosine is garbage). ~5k × 1024-dim is a
// sub-10ms scan, so loading the whole set per query is acceptable and exact.
func (s *Store) LoadVectors(ctx context.Context, model string) ([]PageVector, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT subject, vector FROM page_vectors WHERE model = ? ORDER BY subject`, model)
	if err != nil {
		return nil, fmt.Errorf("page: load vectors: %w", err)
	}
	defer rows.Close()
	var out []PageVector
	for rows.Next() {
		var pv PageVector
		var blob []byte
		if err := rows.Scan(&pv.Subject, &blob); err != nil {
			return nil, fmt.Errorf("page: load vector scan: %w", err)
		}
		vec, err := decodeVector(blob)
		if err != nil {
			return nil, err
		}
		pv.Vector = vec
		out = append(out, pv)
	}
	return out, rows.Err()
}

// WholePagesByIDs loads the whole pages for a set of subject ids (the vector
// lane's resolve step: cosine ranks subject ids, then the page bodies are joined
// in for the hit). Missing subjects are silently skipped (a vector with no page
// is not a hit). The result order follows the input id order so the caller's
// ranking is preserved.
func (s *Store) WholePagesByIDs(ctx context.Context, ids []string) ([]WholePage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	byID := make(map[string]WholePage, len(ids))
	for _, id := range ids {
		var wp WholePage
		err := s.db.QueryRowContext(ctx, `
			SELECT s.id, s.type, s.kind, COALESCE(p.title,''), COALESCE(p.body,'')
			FROM subjects s
			JOIN pages p ON p.subject = s.id
			WHERE s.id = ?`, id,
		).Scan(&wp.Subject, &wp.Type, &wp.Kind, &wp.Title, &wp.Body)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("page: whole page by id %q: %w", id, err)
		}
		byID[id] = wp
	}
	out := make([]WholePage, 0, len(byID))
	for _, id := range ids {
		if wp, ok := byID[id]; ok {
			out = append(out, wp)
		}
	}
	return out, nil
}
