package store

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

func (s *Store) openFile(ctx context.Context, id string) (*db.Immutable, error) {
	path := s.file(id)
	info, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) || errors.Is(e, syscall.ENOTDIR) {
		return nil, ErrMissing
	}
	if e != nil {
		return nil, normalize(e)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnreachable
	}
	f, e := os.Open(filepath.Clean(path))
	if e != nil {
		return nil, normalize(e)
	}
	var header [16]byte
	_, e = io.ReadFull(f, header[:])
	_ = f.Close()
	if e != nil || string(header[:]) != "SQLite format 3\x00" {
		return nil, ErrUnreachable
	}
	d, e := db.OpenImmutable(ctx, path)
	return d, normalize(e)
}
func (s *Store) rowFile(ctx context.Context, id string, valid bool) (*db.Immutable, error) {
	var run Run
	e := s.transaction(ctx, false, func(tx *sql.Tx) error {
		if !valid {
			return ErrUnreachable
		}
		var err error
		run, err = readRun(ctx, tx, id)
		return err
	})
	if e != nil {
		return nil, e
	}
	if run.Status != StatusDone {
		return nil, nil
	}
	return s.openFile(ctx, id)
}

// GetRow returns one complete envelope and raw record.
func (s *Store) GetRow(ctx context.Context, run, id string) (source.Row, error) {
	d, e := s.rowFile(ctx, run, true)
	if e != nil {
		return source.Row{}, e
	}
	if d == nil {
		return source.Row{}, ErrNotFound
	}
	defer func() { _ = d.Close() }()
	var out source.Row
	e = d.Read(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanRow(tx.QueryRowContext(ctx, "SELECT "+rowCols+" FROM rows WHERE source_id=?", id))
		return err
	})
	if e != nil {
		return source.Row{}, normalize(e)
	}
	return out, nil
}

// ListRowsAfter returns a forward envelope window.
func (s *Store) ListRowsAfter(ctx context.Context, run, name, after string, limit int) ([]source.Row, error) {
	return s.rowsWindow(ctx, run, name, after, limit, false)
}

// ListRowsBefore returns a backward envelope window.
func (s *Store) ListRowsBefore(ctx context.Context, run, name, before string, limit int) ([]source.Row, error) {
	return s.rowsWindow(ctx, run, name, before, limit, true)
}
func (s *Store) rowsWindow(ctx context.Context, run, name, pos string, limit int, reverse bool) ([]source.Row, error) {
	d, e := s.rowFile(ctx, run, limit > 0)
	if e != nil {
		return nil, e
	}
	out := []source.Row{}
	if d == nil {
		return out, nil
	}
	defer func() { _ = d.Close() }()
	e = d.Read(ctx, func(tx *sql.Tx) error {
		const cols = `"run",source_id,scraper,collected,digest,name,names,street,city,state,zip,county,lat,lon,people,facts,NULL`
		q := "SELECT " + cols + " FROM rows r WHERE 1=1"
		args := []any{}
		if pos != "" {
			if reverse {
				q += " AND source_id<?"
			} else {
				q += " AND source_id>?"
			}
			args = append(args, pos)
		}
		if name != "" {
			q += " AND EXISTS (SELECT 1 FROM names n WHERE n.source_id=r.source_id AND instr(n.name_lower,?)>0)"
			args = append(args, strings.ToLower(name))
		}
		if reverse {
			q += " ORDER BY source_id DESC LIMIT ?"
		} else {
			q += " ORDER BY source_id LIMIT ?"
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if e != nil {
		return nil, normalize(e)
	}
	return out, nil
}

// EachRow streams full rows through one immutable handle.
func (s *Store) EachRow(ctx context.Context, run string, fn func(source.Row) error) error {
	d, e := s.rowFile(ctx, run, true)
	if e != nil {
		return e
	}
	if d == nil {
		return nil
	}
	defer func() { _ = d.Close() }()
	var callback error
	e = d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT "+rowCols+" FROM rows ORDER BY source_id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			if callback = fn(r); callback != nil {
				return callback
			}
		}
		return rows.Err()
	})
	if callback != nil {
		return callback
	}
	return normalize(e)
}
