package store

import (
	"bytes"
	"container/heap"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Step observes a completed disk write before further work begins.
type Step func(context.Context, int64, time.Duration) error

// Collection owns the temporary files of a single build.
type Collection struct {
	mu                                sync.Mutex
	s                                 *Store
	run                               Run
	step                              Step
	buffer                            []encodedRow
	size                              int64
	sequence                          uint64
	chunks                            []string
	owned                             []string
	sealed, loaded, discarded, failed bool
}
type storedRow source.Row
type wireRow struct {
	Sequence uint64
	Row      storedRow
}
type encodedRow struct {
	id   string
	data []byte
}

// Collect starts a build without creating files.
func (s *Store) Collect(ctx context.Context, id string, step Step) (*Collection, error) {
	r, e := s.GetRun(ctx, id)
	if e != nil {
		return nil, e
	}
	if r.Status != StatusRunning {
		return nil, ErrEnded
	}
	return &Collection{s: s, run: r, step: step}, nil
}
func (c *Collection) check(ctx context.Context, valid bool) error {
	if e := ctx.Err(); e != nil {
		return c.poison(e)
	}
	if c.failed || c.discarded || !valid {
		return ErrUnreachable
	}
	return nil
}
func (c *Collection) poison(err error) error { c.failed = true; return normalize(err) }
func (c *Collection) chunkBytes() int64 {
	if c.s.cfg.ChunkBytes < 1 {
		return DefaultChunkBytes
	}
	return c.s.cfg.ChunkBytes
}
func (c *Collection) loadRows() int {
	if c.s.cfg.LoadRows < 1 {
		return DefaultLoadRows
	}
	return c.s.cfg.LoadRows
}
func (c *Collection) temp() (string, error) {
	if e := os.MkdirAll(c.s.cfg.Runs, 0700); e != nil {
		return "", e
	}
	f, e := os.CreateTemp(c.s.cfg.Runs, c.run.ID+".")
	if e != nil {
		return "", e
	}
	p := f.Name()
	c.owned = append(c.owned, p)
	return p, f.Close()
}
func (c *Collection) notify(ctx context.Context, step Step, n int64, start time.Time) error {
	end := c.s.cfg.Now()
	if step == nil {
		return nil
	}
	return step(ctx, n, end.Sub(start))
}

// Add copies a source page, spilling sorted chunks at the configured threshold.
func (c *Collection) Add(ctx context.Context, rows []source.Row) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.check(ctx, !c.sealed); e != nil {
		return e
	}
	for _, r := range rows {
		if r.SourceID == "" {
			return ErrUnreachable
		}
	}
	for _, r := range rows {
		r.Run = c.run.ID
		r.Scraper = c.run.Scraper
		r.Collected = stamp(r.Collected)
		var b bytes.Buffer
		c.sequence++
		if e := gob.NewEncoder(&b).Encode(wireRow{c.sequence, storedRow(r)}); e != nil {
			return c.poison(e)
		}
		c.buffer = append(c.buffer, encodedRow{r.SourceID, b.Bytes()})
		c.size += int64(b.Len() + 8)
		if c.size >= c.chunkBytes() {
			if e := c.flush(ctx); e != nil {
				return c.poison(e)
			}
		}
	}
	return nil
}
func (c *Collection) flush(ctx context.Context) error {
	if len(c.buffer) == 0 {
		return nil
	}
	start := c.s.cfg.Now()
	p, e := c.temp()
	if e != nil {
		return e
	}
	c.chunks = append(c.chunks, p)
	sort.SliceStable(c.buffer, func(i, j int) bool { return c.buffer[i].id < c.buffer[j].id })
	f, e := os.OpenFile(filepath.Clean(p), os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	for _, r := range c.buffer {
		if e = binary.Write(f, binary.LittleEndian, uint64(len(r.data))); e == nil {
			_, e = f.Write(r.data)
		}
		if e != nil {
			_ = f.Close()
			return e
		}
	}
	if e = f.Close(); e != nil {
		return e
	}
	n := c.size
	c.buffer = nil
	c.size = 0
	return c.notify(ctx, c.step, n, start)
}

// Seal writes the final partial chunk.
func (c *Collection) Seal(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.check(ctx, !c.sealed); e != nil {
		return e
	}
	if e := c.flush(ctx); e != nil {
		return c.poison(e)
	}
	c.sealed = true
	return nil
}

// Discard removes this collection's temporary files, retaining its published file.
func (c *Collection) Discard() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.owned {
		_ = os.Remove(p)
	}
	c.buffer = nil
	c.discarded = true
	return nil
}

type chunkCursor struct {
	file *os.File
	row  wireRow
}

func (c *chunkCursor) next() error {
	var n uint64
	if e := binary.Read(c.file, binary.LittleEndian, &n); e != nil {
		return e
	}
	c.row = wireRow{}
	return gob.NewDecoder(io.LimitReader(c.file, int64(n))).Decode(&c.row)
}

type mergeHeap []*chunkCursor

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	a, b := h[i].row, h[j].row
	if a.Row.SourceID == b.Row.SourceID {
		return a.Sequence < b.Sequence
	}
	return a.Row.SourceID < b.Row.SourceID
}
func (h mergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(v any)   { *h = append(*h, v.(*chunkCursor)) }
func (h *mergeHeap) Pop() any     { old := *h; n := len(old); v := old[n-1]; *h = old[:n-1]; return v }
func (h *mergeHeap) take() (wireRow, error) {
	var out wireRow
	for h.Len() > 0 {
		cursor := (*h)[0]
		if out.Row.SourceID != "" && cursor.row.Row.SourceID != out.Row.SourceID {
			break
		}
		out = cursor.row
		heap.Pop(h)
		e := cursor.next()
		if e == nil {
			heap.Push(h, cursor)
		} else if !errors.Is(e, io.EOF) {
			return wireRow{}, e
		}
	}
	return out, nil
}

const fileSchema = `CREATE TABLE rows ("run" TEXT,source_id TEXT PRIMARY KEY,scraper TEXT,collected TEXT,digest TEXT,name TEXT,names TEXT,street TEXT,city TEXT,state TEXT,zip TEXT,county TEXT,lat REAL,lon REAL,people TEXT,facts BLOB,raw BLOB) WITHOUT ROWID;
CREATE INDEX rows_source_digest ON rows(source_id,digest);
CREATE TABLE names(source_id TEXT,seq INTEGER,name_lower TEXT,PRIMARY KEY(source_id,seq)) WITHOUT ROWID;`

func insertRow(ctx context.Context, tx *sql.Tx, r source.Row) error {
	names, e := json.Marshal(r.Names)
	if e != nil {
		return e
	}
	people, e := json.Marshal(r.People)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO rows ("+rowCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", r.Run, r.SourceID, r.Scraper, encodeTime(r.Collected), r.Digest, r.Name, string(names), r.Address.Street, r.Address.City, r.Address.State, r.Address.Zip, r.Address.County, r.Lat, r.Lon, string(people), []byte(r.Facts), []byte(r.Raw))
	if e != nil {
		return e
	}
	for i, name := range r.Names {
		if _, e = tx.ExecContext(ctx, "INSERT INTO names VALUES(?,?,?)", r.SourceID, i, strings.ToLower(name)); e != nil {
			return e
		}
	}
	return nil
}
func (c *Collection) baseline(ctx context.Context) (*db.Immutable, error) {
	runs, e := c.s.ListRunsAfter(ctx, c.run.Scraper, "", "", int(^uint(0)>>1))
	if e != nil {
		return nil, e
	}
	for _, r := range runs {
		if r.ID == c.run.ID || r.Status != StatusDone {
			continue
		}
		d, err := c.s.openFile(ctx, r.ID)
		if err != nil {
			continue
		}
		err = d.Read(ctx, func(tx *sql.Tx) error {
			rows, e := tx.QueryContext(ctx, "SELECT "+rowCols+" FROM rows ORDER BY source_id")
			if e != nil {
				return e
			}
			for rows.Next() {
				if _, e = scanRow(rows); e != nil {
					_ = rows.Close()
					return e
				}
			}
			e = errors.Join(rows.Err(), rows.Close())
			if e != nil {
				return e
			}
			names, e := tx.QueryContext(ctx, "SELECT source_id,seq,name_lower FROM names")
			if e != nil {
				return e
			}
			defer func() { _ = names.Close() }()
			for names.Next() {
				var id, name string
				var seq int
				if e = names.Scan(&id, &seq, &name); e != nil {
					return e
				}
			}
			return names.Err()
		})
		if err == nil {
			return d, nil
		}
		_ = d.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, nil
}

// Load merges chunks, compares their digests and atomically publishes a run file.
func (c *Collection) Load(ctx context.Context, step Step) (Counts, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return Counts{}, c.poison(e)
	}
	// A failing catalog takes precedence over an invalid collection state.
	if e := c.s.transaction(ctx, false, func(*sql.Tx) error { return nil }); e != nil {
		return Counts{}, c.poison(e)
	}
	if e := c.check(ctx, c.sealed && !c.loaded); e != nil {
		return Counts{}, e
	}
	r, e := c.s.GetRun(ctx, c.run.ID)
	if e != nil {
		return Counts{}, c.poison(e)
	}
	if r.Status != StatusRunning {
		return Counts{}, c.poison(ErrEnded)
	}
	if info, err := os.Lstat(c.s.file(c.run.ID)); err == nil && info.IsDir() {
		return Counts{}, c.poison(ErrUnreachable)
	}
	base, e := c.baseline(ctx)
	if e != nil {
		return Counts{}, c.poison(e)
	}
	if base != nil {
		defer func() { _ = base.Close() }()
	}
	var counts Counts
	load := func(tx *sql.Tx) error { var err error; counts, err = c.load(ctx, step, tx); return err }
	if base != nil {
		e = base.Read(ctx, load)
	} else {
		e = load(nil)
	}
	if e != nil {
		return Counts{}, c.poison(e)
	}
	c.loaded = true
	for _, p := range c.owned {
		_ = os.Remove(p)
	}
	return counts, nil
}
func (c *Collection) load(ctx context.Context, step Step, base *sql.Tx) (Counts, error) {
	var counts Counts
	var previous *sql.Rows
	var prevID, prevDigest string
	var hasPrev bool
	advance := func() error {
		if previous == nil {
			return nil
		}
		hasPrev = previous.Next()
		if hasPrev {
			return previous.Scan(&prevID, &prevDigest)
		}
		return previous.Err()
	}
	if base != nil {
		var e error
		previous, e = base.QueryContext(ctx, "SELECT source_id,digest FROM rows ORDER BY source_id")
		if e != nil {
			return counts, e
		}
		defer func() { _ = previous.Close() }()
		if e = advance(); e != nil {
			return counts, e
		}
	}
	h := mergeHeap{}
	for _, path := range c.chunks {
		f, e := os.Open(filepath.Clean(path))
		if e != nil {
			return counts, e
		}
		defer func() { _ = f.Close() }()
		cursor := &chunkCursor{file: f}
		if e = cursor.next(); e != nil {
			if errors.Is(e, io.EOF) {
				continue
			}
			return counts, e
		}
		heap.Push(&h, cursor)
	}
	start := c.s.cfg.Now()
	path, e := c.temp()
	if e != nil {
		return counts, e
	}
	if e = os.Remove(path); e != nil {
		return counts, e
	}
	bulk, e := db.OpenBulk(ctx, path)
	if e != nil {
		return counts, e
	}
	defer func() { _ = bulk.Close() }()
	var size int64
	first := true
	for first || h.Len() > 0 {
		e = bulk.Write(ctx, func(tx *sql.Tx) error {
			if first {
				if _, err := tx.ExecContext(ctx, fileSchema); err != nil {
					return err
				}
			}
			for n := 0; n < c.loadRows() && h.Len() > 0; n++ {
				wire, err := h.take()
				if err != nil {
					return err
				}
				r := source.Row(wire.Row)
				for hasPrev && prevID < r.SourceID {
					counts.Gone++
					if err = advance(); err != nil {
						return err
					}
				}
				counts.Rows++
				if !hasPrev || prevID != r.SourceID {
					counts.New++
				} else {
					if prevDigest == r.Digest {
						counts.Unchanged++
					} else {
						counts.Changed++
					}
					if err = advance(); err != nil {
						return err
					}
				}
				if err = insertRow(ctx, tx, r); err != nil {
					return err
				}
			}
			return nil
		})
		if e != nil {
			return counts, e
		}
		info, err := os.Stat(path)
		if err != nil {
			return counts, err
		}
		growth := info.Size() - size
		size = info.Size()
		if e = c.notify(ctx, step, growth, start); e != nil {
			return counts, e
		}
		first = false
		if h.Len() > 0 {
			start = c.s.cfg.Now()
		}
	}
	for hasPrev {
		counts.Gone++
		if e = advance(); e != nil {
			return counts, e
		}
	}
	if e = bulk.Close(); e != nil {
		return counts, e
	}
	f, e := os.OpenFile(filepath.Clean(path), os.O_RDWR, 0600)
	if e != nil {
		return counts, e
	}
	e = errors.Join(f.Sync(), f.Close())
	if e != nil {
		return counts, e
	}
	if e = ctx.Err(); e != nil {
		return counts, e
	}
	if e = os.Rename(path, c.s.file(c.run.ID)); e != nil {
		return counts, e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return counts, e
	}
	e = errors.Join(dir.Sync(), dir.Close())
	if e != nil {
		_ = os.Remove(c.s.file(c.run.ID))
		return counts, e
	}
	return counts, nil
}
