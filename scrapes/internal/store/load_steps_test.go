package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-RHXB-8EET
func TestLoadWriteBounds(t *testing.T) {
	for _, limit := range []int{1, 2, 3, 7} {
		boundaries := []int{0, 1, limit - 1, limit, limit + 1, 2 * limit, 2*limit + 1}
		slices.Sort(boundaries)
		for _, distinct := range slices.Compact(boundaries) {
			t.Run(fmt.Sprintf("limit_%d/rows_%d", limit, distinct), func(t *testing.T) {
				checkLoadWrites(t, limit, distinct, true)
			})
		}
	}
	t.Run("nil_step", func(t *testing.T) { checkLoadWrites(t, 2, 5, false) })
}

func checkLoadWrites(t *testing.T, limit, distinct int, observe bool) {
	t.Helper()
	s, _, runs := configured(t, 1, limit)
	r := add(t, s, "source")
	c := build(t, s, r, nil)
	var input []source.Row
	for i := distinct - 1; i >= 0; i-- {
		row := source.Row{SourceID: fmt.Sprintf("row_%03d", i), Raw: []byte(`{"payload":"` + strings.Repeat("x", 5000) + `"}`)}
		// Repeated input ids must not inflate the R used for the step bound.
		input = append(input, row, row)
	}
	if err := c.Add(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := c.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	chunks := map[string]bool{}
	entries, err := os.ReadDir(runs)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		chunks[entry.Name()] = true
	}
	var calls int
	var previousRows, previousBytes, totalBytes int64
	var observedFile os.FileInfo
	var step store.Step
	if observe {
		step = func(callCtx context.Context, n int64, _ time.Duration) error {
			calls++
			// The sealed chunks are known by observation; discover the load's
			// database without relying on its temporary filename.
			entries, e := os.ReadDir(runs)
			if e != nil {
				t.Fatal(e)
			}
			var file os.FileInfo
			var rows int64
			for _, entry := range entries {
				if chunks[entry.Name()] {
					continue
				}
				path := filepath.Join(runs, entry.Name())
				reader, e := db.OpenImmutable(callCtx, path)
				if e != nil {
					continue
				}
				var count int64
				e = reader.Read(callCtx, func(tx *sql.Tx) error {
					return tx.QueryRowContext(callCtx, "SELECT count(*) FROM rows").Scan(&count)
				})
				closeErr := reader.Close()
				if closeErr != nil {
					t.Fatal(closeErr)
				}
				if e != nil {
					continue
				}
				if file != nil {
					t.Fatal("more than one load database")
				}
				file, e = os.Stat(path)
				if e != nil {
					t.Fatal(e)
				}
				rows = count
			}
			if file == nil {
				t.Fatal("step has no readable load database")
			}
			if observedFile != nil && !os.SameFile(observedFile, file) {
				t.Fatal("step observed a different load file")
			}
			inserted := rows - previousRows
			if inserted < 0 || inserted > int64(limit) {
				t.Fatalf("step %d inserted %d rows; limit %d", calls, inserted, limit)
			}
			if n != file.Size()-previousBytes {
				t.Fatalf("step %d reports %d bytes; observed growth %d", calls, n, file.Size()-previousBytes)
			}
			observedFile, previousRows, previousBytes = file, rows, file.Size()
			totalBytes += n
			return nil
		}
	}
	if _, err = c.Load(ctx, step); err != nil {
		t.Fatal(err)
	}
	final, err := os.Lstat(filepath.Join(runs, r.ID+".db"))
	if err != nil {
		t.Fatal(err)
	}
	if !final.Mode().IsRegular() {
		t.Fatal("loaded run file is not regular")
	}
	entries, err = os.ReadDir(runs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != r.ID+".db" {
		t.Fatalf("load left entries besides its run file: %v", entries)
	}
	if observe {
		minimum := (distinct + limit - 1) / limit
		if calls < minimum || calls > minimum+1 {
			t.Fatalf("%d steps for %d distinct rows with limit %d; want %d..%d", calls, distinct, limit, minimum, minimum+1)
		}
		if previousRows != int64(distinct) {
			t.Fatalf("steps observed %d rows; want %d", previousRows, distinct)
		}
		if totalBytes != final.Size() {
			t.Fatalf("steps report %d total bytes; final size %d", totalBytes, final.Size())
		}
		if observedFile != nil && !os.SameFile(observedFile, final) {
			t.Fatal("published file differs from the file observed in steps")
		}
	}
}
