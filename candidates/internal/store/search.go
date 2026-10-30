package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SearchDefaultLimit is the number of matches returned when a caller omits a limit.
const SearchDefaultLimit = 10

// SearchMaxLimit bounds the number of matches in one answer.
const SearchMaxLimit = 50

// Match holds the candidate, the stored name matched, and its search score.
type Match struct {
	Candidate Candidate
	Matched   string
	Score     float64
}

// searchWrite queues the final index state of a touched candidate. Flushing at
// the end of the same transaction lets one scan remove the old rows for an
// entire write unit; candidate is deliberately unindexed in the FTS table.
type searchWriteKey struct{}
type searchWrite struct {
	existed bool
	name    string
	names   []string
	active  bool
}

func prepareSearchWrite(ctx context.Context, tx *sql.Tx, id string) error {
	writes, _ := ctx.Value(searchWriteKey{}).(map[string]searchWrite)
	if writes == nil {
		return nil
	}
	if _, ok := writes[id]; ok {
		return nil
	}
	var exists bool
	if err := scanSQL(ctx, tx, "SELECT EXISTS(SELECT 1 FROM candidates WHERE id=?)", []any{id}, &exists); err != nil {
		return err
	}
	writes[id] = searchWrite{existed: exists}
	return nil
}

func indexCandidate(ctx context.Context, tx *sql.Tx, c Candidate) error {
	writes, _ := ctx.Value(searchWriteKey{}).(map[string]searchWrite)
	if writes == nil {
		if err := execSQL(ctx, tx, "DELETE FROM search_index WHERE candidate=?", c.ID); err != nil {
			return err
		}
		return insertSearchNames(ctx, tx, c.ID, searchWrite{name: c.Name, names: c.Names, active: c.Status == StatusActive})
	}
	writes[c.ID] = searchWrite{existed: writes[c.ID].existed, name: c.Name, names: slices.Clone(c.Names), active: c.Status == StatusActive}
	return nil
}

func flushSearchWrites(ctx context.Context, tx *sql.Tx) error {
	writes, _ := ctx.Value(searchWriteKey{}).(map[string]searchWrite)
	ids := make([]string, 0, len(writes))
	for id, w := range writes {
		if w.existed {
			ids = append(ids, id)
		}
	}
	// Bound statement arguments independently of the caller's batch size.
	for len(ids) > 0 {
		n := min(len(ids), 500)
		args := make([]any, n)
		for i, id := range ids[:n] {
			args[i] = id
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", n), ",")
		if err := execSQL(ctx, tx, "DELETE FROM search_index WHERE candidate IN ("+marks+")", args...); err != nil {
			return err
		}
		ids = ids[n:]
	}
	for id, w := range writes {
		if err := insertSearchNames(ctx, tx, id, w); err != nil {
			return err
		}
	}
	return nil
}

func insertSearchNames(ctx context.Context, tx *sql.Tx, id string, w searchWrite) error {
	if !w.active {
		return nil
	}
	seen := map[string]bool{"": true}
	for _, name := range append([]string{w.name}, w.names...) {
		if seen[name] {
			continue
		}
		seen[name] = true
		if err := execSQL(ctx, tx, "INSERT INTO search_index(name,candidate,display) VALUES (?,?,?)", strings.ReplaceAll(name, "\x00", " "), id, name == w.name); err != nil {
			return err
		}
	}
	return nil
}

type searchHit struct {
	id, name string
	score    float64
	position int
}

func compareSearch(a, b searchHit) int {
	if v := cmp.Compare(b.score, a.score); v != 0 {
		return v
	}
	return cmp.Compare(a.id, b.id)
}

// Search finds active candidates by name, using bounded trigram recall and exact ranking.
func (s *Store) Search(ctx context.Context, q string, limit int) ([]Match, error) {
	var out []Match
	err := s.readTx(ctx, func(tx *sql.Tx) error {
		q = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(q, "\x00", " ")))
		if q == "" || limit < 1 || limit > SearchMaxLimit {
			return ErrInvalid
		}
		var hits []searchHit
		var err error
		if utf8.RuneCountInString(q) < 3 {
			hits, err = shortSearch(ctx, tx, q, limit)
		} else {
			hits, err = indexedSearch(ctx, tx, q, limit)
		}
		if err != nil {
			return err
		}
		ctx = ownerReadContext(ctx)
		out = make([]Match, 0, len(hits))
		for _, hit := range hits {
			c, e := candidateKey(ctx, tx, hit.id)
			if e != nil {
				return e
			}
			if c.Status == StatusActive {
				out = append(out, Match{c, hit.name, hit.score})
			}
		}
		return nil
	})
	if err != nil {
		return nil, unreachable(err)
	}
	return out, nil
}

func shortSearch(ctx context.Context, tx *sql.Tx, q string, limit int) ([]searchHit, error) {
	rows, err := querySQL(ctx, tx, "SELECT id,name FROM candidates WHERE status='active' AND instr(names_lower,?)=1", q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	hits := make([]searchHit, 0, limit+1)
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		n := strings.ToLower(strings.ReplaceAll(name, "\x00", " "))
		if !strings.HasPrefix(n, q) {
			continue
		}
		tier := 2
		if n == q {
			tier = 1
		}
		hit := searchHit{id: id, name: name, score: searchScore(q, n, tier, true, 0, 0)}
		at, _ := slices.BinarySearchFunc(hits, hit, compareSearch)
		if at < limit {
			if len(hits) == limit {
				hits = hits[:limit-1]
			}
			hits = slices.Insert(hits, at, hit)
		}
	}
	return hits, rows.Err()
}

func indexedSearch(ctx context.Context, tx *sql.Tx, q string, limit int) ([]searchHit, error) {
	best := map[string]searchHit{}
	first := searchExpression(q)
	count := 0
	var err error
	if first != "" {
		count, err = recallSearch(ctx, tx, q, first, best)
		if err != nil {
			return nil, err
		}
	}
	if count < limit {
		terms := searchTrigrams(q)
		if len(terms) > 0 {
			args := make([]any, len(terms))
			for i, v := range terms {
				args[i] = v
			}
			marks := strings.TrimSuffix(strings.Repeat("?,", len(terms)), ",")
			rows, e := querySQL(ctx, tx, "SELECT term FROM search_index_vocab WHERE term IN ("+marks+") ORDER BY doc,term LIMIT 8", args...)
			if e != nil {
				return nil, e
			}
			rare := []string{}
			for rows.Next() {
				var term string
				if e = rows.Scan(&term); e != nil {
					_ = rows.Close()
					return nil, e
				}
				rare = append(rare, quoteSearch(term))
			}
			e = rows.Err()
			closeErr := rows.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if len(rare) > 0 {
				if _, err = recallSearch(ctx, tx, q, strings.Join(rare, " OR "), best); err != nil {
					return nil, err
				}
			}
		}
	}
	hits := make([]searchHit, 0, len(best))
	for _, hit := range best {
		hits = append(hits, hit)
	}
	slices.SortFunc(hits, compareSearch)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func recallSearch(ctx context.Context, tx *sql.Tx, q, expression string, best map[string]searchHit) (int, error) {
	rows, err := querySQL(ctx, tx, `SELECT r.name,r.candidate,r.display,c.status,c.name,c.names FROM (SELECT name,candidate,display FROM search_index WHERE search_index MATCH ? ORDER BY rank LIMIT 200) AS r JOIN candidates c ON c.id=r.candidate`, expression)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var name, id, status, displayName, namesJSON string
		var display bool
		if err = rows.Scan(&name, &id, &display, &status, &displayName, &namesJSON); err != nil {
			return 0, err
		}
		count++
		if status != StatusActive {
			continue
		}
		position := 0
		if display {
			name = displayName
		} else {
			var names []string
			if err = json.Unmarshal([]byte(namesJSON), &names); err != nil {
				return 0, err
			}
			for i, raw := range names {
				if strings.ReplaceAll(raw, "\x00", " ") == name {
					name, position = raw, i+1
					break
				}
			}
		}
		folded := strings.ToLower(strings.ReplaceAll(name, "\x00", " "))
		tier, edits, allowance := searchTier(q, folded)
		if tier == 0 {
			continue
		}
		hit := searchHit{id: id, name: name, score: searchScore(q, folded, tier, display, edits, allowance), position: position}
		old, exists := best[id]
		if !exists || hit.score > old.score || (hit.score == old.score && hit.position < old.position) {
			best[id] = hit
		}
	}
	return count, rows.Err()
}

func searchWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
func searchTrigrams(s string) []string {
	r := []rune(s)
	terms := []string{}
	seen := map[string]bool{}
	for i := 0; i+3 <= len(r); i++ {
		term := string(r[i : i+3])
		if !seen[term] {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	return terms
}
func quoteSearch(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func searchExpression(q string) string {
	parts := []string{}
	for _, word := range searchWords(q) {
		terms := searchTrigrams(word)
		if len(terms) == 0 {
			continue
		}
		for i := range terms {
			terms[i] = quoteSearch(terms[i])
		}
		parts = append(parts, "("+strings.Join(terms, " OR ")+")")
	}
	return strings.Join(parts, " AND ")
}
func searchTier(q, n string) (tier, edits, allowance int) {
	if n == q {
		return 1, 0, 0
	}
	if strings.HasPrefix(n, q) {
		return 2, 0, 0
	}
	qw, nw := searchWords(q), searchWords(n)
	prefix := len(qw) > 0
	for _, u := range qw {
		found := false
		for _, v := range nw {
			if strings.HasPrefix(v, u) {
				found = true
				break
			}
		}
		if !found {
			prefix = false
			break
		}
	}
	if prefix {
		return 3, 0, 0
	}
	if strings.Contains(n, q) {
		return 4, 0, 0
	}
	if len(qw) == 0 {
		return 0, 0, 0
	}
	for _, u := range qw {
		distance, pairAllowance := math.MaxInt, -1
		for _, v := range nw {
			a := utf8.RuneCountInString(v) / 5
			if absSearch(utf8.RuneCountInString(u)-utf8.RuneCountInString(v)) > a {
				continue
			}
			d := searchDistance([]rune(u), []rune(v))
			if d <= a && (d < distance || (d == distance && a > pairAllowance)) {
				distance, pairAllowance = d, a
			}
		}
		if pairAllowance < 0 {
			return 0, 0, 0
		}
		edits += distance
		allowance += pairAllowance
	}
	return 5, edits, allowance
}
func absSearch(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func searchScore(q, n string, tier int, display bool, edits, allowance int) float64 {
	if tier == 1 {
		return 1
	}
	low, high := 0.1, 0.4
	switch tier {
	case 2:
		low, high = 0.8, 1
	case 3:
		low, high = 0.6, 0.8
	case 4:
		low, high = 0.4, 0.6
	}
	d := 0.0
	if display {
		d = 1
	}
	coverage := min(1, float64(utf8.RuneCountInString(q))/float64(utf8.RuneCountInString(n)))
	closeness := 1.0
	if tier == 5 {
		closeness -= float64(edits) / float64(allowance)
	}
	score := low + (high-low)*(0.6*d+0.25*coverage+0.15*closeness)
	if score >= high {
		return math.Nextafter(high, 0)
	}
	return score
}

// searchDistance computes unrestricted Damerau-Levenshtein distance. Last-seen
// positions let an edit cross a transposition, unlike optimal string alignment.
func searchDistance(a, b []rune) int {
	inf := len(a) + len(b)
	width := len(b) + 2
	matrix := make([]int, (len(a)+2)*width)
	at := func(i, j int) int { return i*width + j }
	matrix[0] = inf
	for i := 0; i <= len(a); i++ {
		matrix[at(i+1, 0)] = inf
		matrix[at(i+1, 1)] = i
	}
	for j := 0; j <= len(b); j++ {
		matrix[at(0, j+1)] = inf
		matrix[at(1, j+1)] = j
	}
	last := map[rune]int{}
	for i := 1; i <= len(a); i++ {
		match := 0
		for j := 1; j <= len(b); j++ {
			previous, previousMatch := last[b[j-1]], match
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
				match = j
			}
			matrix[at(i+1, j+1)] = min(matrix[at(i, j)]+cost, matrix[at(i+1, j)]+1, matrix[at(i, j+1)]+1, matrix[at(previous, previousMatch)]+i-previous-1+1+j-previousMatch-1)
		}
		last[a[i-1]] = i
	}
	return matrix[at(len(a)+1, len(b)+1)]
}
