// Package store persists an append-only agent session in a SQLite database.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Register the SQLite database/sql driver.
	_ "modernc.org/sqlite"
)

// Kind identifies the purpose of a stored entry.
type Kind string

// Entry kinds supported by the session store.
const (
	KindNote       Kind = "note"
	KindPrompt     Kind = "prompt"
	KindReport     Kind = "report"
	KindTranscript Kind = "transcript"
)

const (
	// PageSize is the number of hits on a search result page.
	PageSize = 20
	// PreviewRunes is the maximum number of runes in a search hit preview.
	PreviewRunes = 160
)

// Entry is a complete stored session entry.
type Entry struct {
	ID      int64
	Address string
	Kind    Kind
	Text    string
	Raw     json.RawMessage
	Created time.Time
}

// Hit is the abbreviated form of an entry returned by search.
type Hit struct {
	ID      int64
	Address string
	Kind    Kind
	Preview string
}

// Page is one page of search hits and its pagination metadata.
type Page struct {
	Hits  []Hit
	Page  int
	Pages int
	Total int
}

// Filter limits the entries considered by a search.
type Filter struct {
	Kinds   []Kind
	Exclude []Kind
	Address string
	Page    int
}

// ErrNotFound indicates that an entry id does not exist.
var ErrNotFound = errors.New("store: entry not found")

// Store is an open append-only session store.
type Store struct {
	db   *sql.DB
	id   string
	root string
	now  func() time.Time
}

// Create exclusively creates and initializes a session database at path.
func Create(path, id, root string, now func() time.Time) (*Store, error) {
	if err := createDatabaseFile(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		removeDatabaseFiles(path)
		return nil, fmt.Errorf("connect new session database: %w", err)
	}
	db.SetMaxOpenConns(1)

	storeClock := clock(now)
	if err := initializeDatabase(db, id, root, storeClock()); err != nil {
		_ = db.Close()
		removeDatabaseFiles(path)
		return nil, err
	}
	return &Store{db: db, id: id, root: root, now: storeClock}, nil
}

func createDatabaseFile(path string) error {
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	parent, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("access session database directory: %w", err)
	}
	file, err := parent.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	_ = parent.Close()
	if err != nil {
		return fmt.Errorf("create session database: %w", err)
	}
	if err := file.Close(); err != nil {
		removeDatabaseFiles(path)
		return fmt.Errorf("prepare new session database: %w", err)
	}
	return nil
}

func initializeDatabase(db *sql.DB, id, root string, creationTime time.Time) error {
	created := encodeTime(creationTime)
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin session initialization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
		CREATE TABLE metadata (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			session_id TEXT NOT NULL,
			root TEXT NOT NULL,
			created BLOB NOT NULL
		);
		CREATE TABLE entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			address TEXT NOT NULL,
			kind TEXT NOT NULL,
			text TEXT NOT NULL,
			raw BLOB,
			created BLOB NOT NULL
		);
		CREATE VIRTUAL TABLE entries_fts USING fts5(
			text,
			content = 'entries',
			content_rowid = 'id'
		);
	`); err != nil {
		return fmt.Errorf("create session schema: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO metadata(singleton, session_id, root, created) VALUES (1, ?, ?, ?)",
		id, root, created,
	); err != nil {
		return fmt.Errorf("record session metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session initialization: %w", err)
	}
	return nil
}

// Open opens an existing session database at path.
func Open(path string, now func() time.Time) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	db.SetMaxOpenConns(1)

	var id, root string
	if err := db.QueryRow("SELECT session_id, root FROM metadata WHERE singleton = 1").Scan(&id, &root); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read session metadata: %w", err)
	}
	return &Store{db: db, id: id, root: root, now: clock(now)}, nil
}

// Close closes the session database.
func (s *Store) Close() error {
	return s.db.Close()
}

// ID returns the session identifier recorded when the store was created.
func (s *Store) ID() string {
	return s.id
}

// Root returns the tool root recorded when the store was created.
func (s *Store) Root() string {
	return s.root
}

// NextPass returns the next pass number derived from stored root prompts.
func (s *Store) NextPass() (int, error) {
	var prompts int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM entries WHERE kind = ? AND instr(address, '.') = 0",
		KindPrompt,
	).Scan(&prompts); err != nil {
		return 0, fmt.Errorf("count root prompts: %w", err)
	}
	return prompts + 1, nil
}

// Add appends an entry and returns its database row id.
func (s *Store) Add(address string, kind Kind, text string, raw json.RawMessage) (int64, error) {
	created := encodeTime(s.now())
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin append: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(
		"INSERT INTO entries(address, kind, text, raw, created) VALUES (?, ?, ?, ?, ?)",
		address, kind, text, nullableRaw(raw), created,
	)
	if err != nil {
		return 0, fmt.Errorf("append entry: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read appended entry id: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO entries_fts(rowid, text) VALUES (?, ?)", id, text); err != nil {
		return 0, fmt.Errorf("index appended entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit append: %w", err)
	}
	return id, nil
}

// Search returns a relevance-ordered page of entries matching an FTS5 query.
func (s *Store) Search(query string, filter Filter) (Page, error) {
	pageNumber := filter.Page
	if pageNumber <= 0 {
		pageNumber = 1
	}
	predicate, args := searchPredicate(query, filter)
	total, err := s.countSearchMatches(predicate, args)
	if err != nil {
		return Page{}, err
	}
	pages := (total + PageSize - 1) / PageSize
	page := Page{Hits: []Hit{}, Page: pageNumber, Pages: pages, Total: total}
	if pageNumber > pages {
		return page, nil
	}
	page.Hits, err = s.searchHits(predicate, args, pageNumber)
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

func (s *Store) countSearchMatches(predicate string, args []any) (int, error) {
	countQuery := strings.Join([]string{
		"SELECT COUNT(*) FROM entries_fts JOIN entries AS e ON e.id = entries_fts.rowid WHERE ",
		predicate,
	}, "")
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count search matches: %w", err)
	}
	return total, nil
}

func (s *Store) searchHits(predicate string, args []any, pageNumber int) ([]Hit, error) {
	hitArgs := append(append([]any{}, args...), PageSize, (pageNumber-1)*PageSize)
	hitQuery := strings.Join([]string{`
		SELECT e.id, e.address, e.kind, e.text
		FROM entries_fts
		JOIN entries AS e ON e.id = entries_fts.rowid
		WHERE `, predicate, `
		ORDER BY bm25(entries_fts) ASC, e.id DESC
		LIMIT ? OFFSET ?`}, "")
	rows, err := s.db.Query(hitQuery, hitArgs...)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	hits := []Hit{}
	for rows.Next() {
		var hit Hit
		var text string
		if err := rows.Scan(&hit.ID, &hit.Address, &hit.Kind, &text); err != nil {
			return nil, fmt.Errorf("read search hit: %w", err)
		}
		hit.Preview = preview(text)
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hits: %w", err)
	}
	return hits, nil
}

func searchPredicate(query string, filter Filter) (string, []any) {
	predicates := []string{"entries_fts MATCH ?"}
	args := []any{query}
	if len(filter.Kinds) > 0 {
		predicates = append(predicates, "e.kind IN ("+placeholders(len(filter.Kinds))+")")
		for _, kind := range filter.Kinds {
			args = append(args, kind)
		}
	}
	if len(filter.Exclude) > 0 {
		predicates = append(predicates, "e.kind NOT IN ("+placeholders(len(filter.Exclude))+")")
		for _, kind := range filter.Exclude {
			args = append(args, kind)
		}
	}
	if filter.Address != "" {
		predicates = append(predicates, "(e.address = ? OR substr(e.address, 1, length(?) + 1) = ? || '.')")
		args = append(args, filter.Address, filter.Address, filter.Address)
	}
	return strings.Join(predicates, " AND "), args
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func preview(text string) string {
	oneLine := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(text)
	runes := []rune(oneLine)
	if len(runes) > PreviewRunes {
		runes = runes[:PreviewRunes]
	}
	return string(runes)
}

// Fetch returns the complete entry identified by id.
func (s *Store) Fetch(id int64) (Entry, error) {
	var entry Entry
	var raw, created []byte
	err := s.db.QueryRow(
		"SELECT id, address, kind, text, raw, created FROM entries WHERE id = ?",
		id,
	).Scan(&entry.ID, &entry.Address, &entry.Kind, &entry.Text, &raw, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, fmt.Errorf("%w: entry %d", ErrNotFound, id)
	}
	if err != nil {
		return Entry{}, fmt.Errorf("fetch entry: %w", err)
	}
	if err := entry.Created.UnmarshalBinary(created); err != nil {
		return Entry{}, fmt.Errorf("decode entry creation time: %w", err)
	}
	if raw != nil {
		entry.Raw = json.RawMessage(raw)
	}
	return entry, nil
}

func clock(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}

func encodeTime(value time.Time) []byte {
	encoded, _ := value.MarshalBinary()
	return encoded
}

func nullableRaw(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	return []byte(raw)
}

func removeDatabaseFiles(path string) {
	for _, suffix := range []string{"", "-journal", "-shm", "-wal"} {
		_ = os.Remove(path + suffix)
	}
}
