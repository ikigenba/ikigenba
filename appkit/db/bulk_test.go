package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/db"
)

func bulkOpen(t *testing.T, path string) *db.Bulk {
	t.Helper()
	b, err := db.OpenBulk(context.Background(), path)
	if err != nil || b == nil {
		t.Fatalf("open bulk: %v %v", b, err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return b
}

// R-FSE3-XJJ2 R-FTM0-BB9R R-GECA-TEVK
func TestBulkAPI(t *testing.T) {
	api := struct {
		open      func(context.Context, string) (*db.Bulk, error)
		write     func(context.Context, func(*sql.Tx) error) error
		closeBulk func() error
	}{open: db.OpenBulk}
	b, err := api.open(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	api.write, api.closeBulk = b.Write, b.Close
	t.Cleanup(func() {
		if err := api.closeBulk(); err != nil {
			t.Error(err)
		}
	})
	calls := 0
	if err := api.write(context.Background(), func(tx *sql.Tx) error {
		calls++
		if tx == nil {
			t.Fatal("nil transaction")
		}
		return nil
	}); err != nil || calls != 1 {
		t.Fatalf("write: calls=%d err=%v", calls, err)
	}
}

// R-FYHL-UE8J R-FZPI-85Z8 R-G0XE-LXPX R-G25A-ZPGM
func TestBulkCreatesPrivateParents(t *testing.T) {
	root := t.TempDir()
	if err := handleChmod(root, 0750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	path := filepath.Join("one", "two", "data")
	bulkOpen(t, path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("file: %v %v", info, err)
	}
	for _, p := range []string{"one", "one/two"} {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("parent: %v %v", info, err)
		}
	}
	info, err = os.Stat(root)
	if err != nil || info.Mode().Perm() != 0750 {
		t.Fatalf("existing parent: %v %v", info, err)
	}
}

// R-G3D7-DH7B R-G4L3-R8Y0 R-G70W-ISFE
func TestBulkRejectsInvalidPaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, path := range []string{"", ":memory:", "file:deep/data", "deep/data?option"} {
		b, err := db.OpenBulk(context.Background(), path)
		if b != nil || err == nil {
			t.Fatalf("accepted %q: %v %v", path, b, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b, err := db.OpenBulk(ctx, "deep/data")
	if b != nil || !errors.Is(err, ctx.Err()) {
		t.Fatalf("canceled: %v %v", b, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("created entries: %v %v", entries, err)
	}
	block := filepath.Join(root, "block")
	if err := os.WriteFile(block, []byte("blocker"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err = db.OpenBulk(context.Background(), filepath.Join(block, "child", "data"))
	if b != nil || err == nil {
		t.Fatalf("accepted non-directory parent: %v %v", b, err)
	}
}

// R-G5T0-50OP
func TestBulkPreservesExistingPaths(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing")
			data := []byte("preserve these bytes")
			if directory {
				if err := os.Mkdir(path, 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "child"), data, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			b, err := db.OpenBulk(context.Background(), path)
			if b != nil || !errors.Is(err, fs.ErrExist) {
				t.Fatalf("existing: %v %v", b, err)
			}
			after, err := os.Stat(path)
			if err != nil || before.Mode() != after.Mode() {
				t.Fatalf("mode changed: %v %v", after, err)
			}
			readPath := path
			if directory {
				entries, err := os.ReadDir(path)
				if err != nil || len(entries) != 1 || entries[0].Name() != "child" {
					t.Fatalf("entries: %v %v", entries, err)
				}
				readPath = filepath.Join(path, "child")
			}
			afterBytes, err := os.ReadFile(filepath.Clean(readPath))
			if err != nil || !bytes.Equal(afterBytes, data) {
				t.Fatalf("bytes changed: %q %v", afterBytes, err)
			}
		})
	}
}

// R-G88S-WK63
func TestBulkRefusesUnwritableParent(t *testing.T) {
	root := t.TempDir()
	if err := handleChmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handleChmod(root, 0700); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(root, "data")
	b, err := db.OpenBulk(context.Background(), path)
	if b != nil || err == nil {
		t.Fatalf("unwritable: %v %v", b, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file created: %v", err)
	}
}

// R-G9GP-ABWS R-GAOL-O3NH R-GBWI-1VE6 R-GD4E-FN4V
func TestBulkConnectionConfiguration(t *testing.T) {
	b := bulkOpen(t, filepath.Join(t.TempDir(), "data"))
	if err := b.Write(context.Background(), func(tx *sql.Tx) error {
		var mode string
		if err := tx.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			return err
		}
		if mode != "off" {
			t.Errorf("journal mode=%q", mode)
		}
		for query, want := range map[string]int{"PRAGMA synchronous": 0, "PRAGMA foreign_keys": 1, "SELECT count(*) FROM sqlite_schema": 0} {
			var got int
			if err := tx.QueryRow(query).Scan(&got); err != nil {
				return err
			}
			if got != want {
				t.Errorf("%s=%d want %d", query, got, want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// R-GFK7-76M9 R-GGS3-KYCY R-GJ7W-CHUC R-GKFS-Q9L1 R-GSZ3-ENRW
func TestBulkFailuresSpoilHandle(t *testing.T) {
	for _, failure := range []string{"callback", "panic", "context", "commit", "already-canceled"} {
		t.Run(failure, func(t *testing.T) {
			b := bulkOpen(t, filepath.Join(t.TempDir(), "data"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("callback failed")
			panicValue := &struct{}{}
			if failure == "already-canceled" {
				cancel()
			}
			calls := 0
			commitReady := false
			var writeErr error
			func() {
				defer func() {
					got := recover()
					if failure == "panic" && got != panicValue {
						t.Errorf("panic=%v", got)
					}
					if failure != "panic" && got != nil {
						t.Errorf("unexpected panic: %v", got)
					}
				}()
				writeErr = b.Write(ctx, func(tx *sql.Tx) error {
					calls++
					switch failure {
					case "callback":
						return sentinel
					case "panic":
						panic(panicValue)
					case "context":
						cancel()
						return nil
					case "commit":
						_, err := tx.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(parent_id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED); INSERT INTO child VALUES(1)")
						commitReady = err == nil
						return err
					default:
						t.Error("canceled callback ran")
						return nil
					}
				})
			}()
			if calls > 1 {
				t.Fatalf("callback called %d times", calls)
			}
			if failure != "already-canceled" && calls != 1 {
				t.Fatalf("callback not reached: %v", writeErr)
			}
			if failure == "commit" && !commitReady {
				t.Fatalf("failed before commit: %v", writeErr)
			}
			if failure != "panic" && writeErr == nil {
				t.Fatal("failure succeeded")
			}
			if failure == "callback" && !errors.Is(writeErr, sentinel) {
				t.Fatalf("lost callback error: %v", writeErr)
			}
			if err := b.Write(context.Background(), func(*sql.Tx) error { t.Error("spoiled callback ran"); return nil }); err == nil {
				t.Fatal("spoiled write succeeded")
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// bulkWaitingContext signals when Write evaluates its cancellation channel.
type bulkWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *bulkWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// R-GLNP-41BQ R-GJ7W-CHUC
func TestBulkCanceledQueuedWrite(t *testing.T) {
	b := bulkOpen(t, filepath.Join(t.TempDir(), "data"))
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- b.Write(context.Background(), func(*sql.Tx) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := &bulkWaitingContext{Context: ctx, waiting: make(chan struct{})}
	second := make(chan error, 1)
	var calls atomic.Int32
	go func() { second <- b.Write(waiting, func(*sql.Tx) error { calls.Add(1); return nil }) }()
	<-waiting.waiting
	cancel()
	err := <-second
	close(release)
	if firstErr := <-first; firstErr != nil {
		t.Fatal(firstErr)
	}
	if !errors.Is(err, ctx.Err()) || calls.Load() != 0 {
		t.Fatalf("queued write: calls=%d err=%v", calls.Load(), err)
	}
	if err := b.Write(context.Background(), func(*sql.Tx) error { t.Error("spoiled callback ran"); return nil }); err == nil {
		t.Fatal("spoiled write succeeded")
	}
}

// R-GMVL-HT2F R-GO3H-VKT4
func TestBulkConcurrentWrites(t *testing.T) {
	b := bulkOpen(t, filepath.Join(t.TempDir(), "data"))
	if err := b.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE counter(n INTEGER); INSERT INTO counter VALUES(0)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const count = 16
	var wg sync.WaitGroup
	var active atomic.Int32
	start := make(chan struct{})
	for range count {
		wg.Go(func() {
			<-start
			if err := b.Write(context.Background(), func(tx *sql.Tx) error {
				if active.Add(1) != 1 {
					t.Error("concurrent callbacks")
				}
				defer active.Add(-1)
				var n int
				if err := tx.QueryRow("SELECT n FROM counter").Scan(&n); err != nil {
					return err
				}
				_, err := tx.Exec("UPDATE counter SET n=?", n+1)
				return err
			}); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if err := b.Write(context.Background(), func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow("SELECT n FROM counter").Scan(&n); err != nil {
			return err
		}
		if n != count {
			t.Errorf("counter=%d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// R-GPBE-9CJT R-GQJA-N4AI R-GRR7-0W17 R-GU6Z-SFIL R-GVEW-679A
func TestBulkCloseAndImmutableRoundtrip(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "existing"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "data")
	b := bulkOpen(t, path)
	for _, statement := range []string{
		"CREATE TABLE things(id INTEGER PRIMARY KEY, name TEXT); CREATE INDEX things_name ON things(name); CREATE TABLE extra(value INTEGER)",
		"INSERT INTO things VALUES(1,'first'),(2,'second'); INSERT INTO extra VALUES(9)",
		"UPDATE things SET name='changed' WHERE id=2; DELETE FROM things WHERE id=1",
	} {
		if err := b.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(statement); return err }); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(context.Background(), func(*sql.Tx) error { t.Error("closed callback ran"); return nil }); err == nil {
		t.Fatal("closed write succeeded")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 || entries[0].Name() != "data" || entries[1].Name() != "existing" {
		t.Fatalf("sidecars: %v %v", entries, err)
	}
	immutable, err := db.OpenImmutable(context.Background(), path)
	if err != nil || immutable == nil {
		t.Fatalf("immutable open: %v %v", immutable, err)
	}
	t.Cleanup(func() {
		if err := immutable.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := immutable.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT type || ':' || name FROM sqlite_schema ORDER BY name")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		var schema []string
		for rows.Next() {
			var entry string
			if err := rows.Scan(&entry); err != nil {
				return err
			}
			schema = append(schema, entry)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !reflect.DeepEqual(schema, []string{"table:extra", "table:things", "index:things_name"}) {
			t.Errorf("schema=%v", schema)
		}
		var id int
		var name string
		var count int
		if err := tx.QueryRow("SELECT id,name FROM things").Scan(&id, &name); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT count(*) FROM things").Scan(&count); err != nil {
			return err
		}
		if id != 2 || name != "changed" || count != 1 {
			t.Errorf("things: %d %s count %d", id, name, count)
		}
		if err := tx.QueryRow("SELECT value FROM extra").Scan(&id); err != nil {
			return err
		}
		if id != 9 {
			t.Errorf("extra=%d", id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
