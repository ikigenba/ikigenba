package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

func handleConfig(path string) db.Config {
	return db.Config{Path: path, Migrations: fstest.MapFS{"0001_counter.sql": &fstest.MapFile{Data: []byte("CREATE TABLE counter(n INTEGER); INSERT INTO counter VALUES(0);")}}, Now: func() time.Time { return time.Unix(42, 0) }}
}
func handleOpen(t *testing.T, cfg db.Config) *db.DB {
	t.Helper()
	d, e := db.Open(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	if d == nil {
		t.Fatal("nil handle")
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	return d
}
func handleCount(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if e := d.Read(context.Background(), func(tx *sql.Tx) error { return tx.QueryRow("SELECT n FROM counter").Scan(&n) }); e != nil {
		t.Fatal(e)
	}
	return n
}
func handleIncrement(tx *sql.Tx) error { _, e := tx.Exec("UPDATE counter SET n=n+1"); return e }

// R-1097-YE7C R-PQE6-KK0G R-LJ0T-HFKI R-1CZF-Y7YJ
func TestHandleAPI(t *testing.T) {
	// An unkeyed literal requires exactly the declared field sequence.
	cfg := db.Config{filepath.Join(t.TempDir(), "db"), fstest.MapFS{}, func() time.Time { return time.Unix(42, 0) }, "dummy", io.Discard}
	var shape struct {
		Path       string
		Migrations fs.FS
		Now        func() time.Time
		Service    string
		Stderr     io.Writer
	} = cfg
	cfg = db.Config{shape.Path, shape.Migrations, shape.Now, shape.Service, shape.Stderr}
	api := struct {
		Open       func(context.Context, db.Config) (*db.DB, error)
		Read       func(context.Context, func(*sql.Tx) error) error
		Write      func(context.Context, func(*sql.Tx) error) error
		Close      func() error
		SetFailing func(bool)
	}{Open: db.Open}
	d, e := api.Open(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	api.Read, api.Write, api.Close, api.SetFailing = d.Read, d.Write, d.Close, d.SetFailing
	const readersByte uint8 = db.Readers
	const readersInt int = db.Readers
	if readersByte != 4 || readersInt != 4 {
		t.Fatalf("Readers=%d/%d", readersByte, readersInt)
	}
	api.SetFailing(false)
	for _, run := range []func(context.Context, func(*sql.Tx) error) error{api.Read, api.Write} {
		if e := run(context.Background(), func(tx *sql.Tx) error {
			if tx == nil {
				t.Fatal("nil tx")
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
	if e := api.Close(); e != nil {
		t.Fatal(e)
	}
}

// R-P04U-UW38 R-P1CR-8NTX R-4K7T-JS93 R-4LFP-XJZS R-4MNM-BBQH R-LQC7-S20O
func TestHandleCreatesAndReopens(t *testing.T) {
	root := t.TempDir()
	if e := handleChmod(root, 0750); e != nil {
		t.Fatal(e)
	}
	t.Chdir(root)
	cfg := handleConfig(filepath.Join("one", "two", "data.db"))
	d := handleOpen(t, cfg)
	info, e := os.Stat(cfg.Path)
	if e != nil || !info.Mode().IsRegular() {
		t.Fatalf("file: %v %v", info, e)
	}
	for _, p := range []string{"one", "one/two"} {
		info, e := os.Stat(p)
		if e != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("directory %s: %v %v", p, info, e)
		}
	}
	info, e = os.Stat(root)
	if e != nil || info.Mode().Perm() != 0750 {
		t.Fatalf("existing mode: %v %v", info, e)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	handleOpen(t, cfg)
}

// R-I5LL-ONP9
func TestHandleAdoptsSQLite(t *testing.T) {
	cfg := handleConfig(filepath.Join(t.TempDir(), "db"))
	raw, e := sql.Open("sqlite", cfg.Path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = raw.Exec("CREATE TABLE legacy(value TEXT); INSERT INTO legacy VALUES('kept')"); e != nil {
		t.Fatal(e)
	}
	if e = raw.Close(); e != nil {
		t.Fatal(e)
	}
	handleOpen(t, cfg)
}

// R-PSTZ-C3HU R-LRK4-5TRD R-4NVI-P3H6 R-4P3F-2V7V
func TestHandleRejectsPathsAndCanceledOpen(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, p := range []string{"", ":memory:", "file:deep/db", "deep/db?option"} {
		d, e := db.Open(context.Background(), handleConfig(p))
		if d != nil || e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, e := db.Open(ctx, handleConfig("deep/db"))
	if d != nil || !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel: %v %v", d, e)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatalf("created entries: %v %v", entries, e)
	}
	if e := os.WriteFile(filepath.Join(root, "block"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{filepath.Join(root, "block", "child", "db"), root} {
		d, e := db.Open(context.Background(), handleConfig(p))
		if d != nil || e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
}

// R-4QBB-GMYK R-4RJ7-UEP9 R-4SR4-86FY R-PWHO-HEPX
func TestHandleRefusalsPreserveFiles(t *testing.T) {
	for _, kind := range []string{"invalid", "file-permission", "directory-permission", "missing-permission"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			cfg := handleConfig(filepath.Join(root, "db"))
			switch kind {
			case "invalid":
				if e := os.WriteFile(cfg.Path, []byte("not sqlite data!"), 0600); e != nil {
					t.Fatal(e)
				}
			case "file-permission", "directory-permission":
				d := handleOpen(t, cfg)
				if e := d.Close(); e != nil {
					t.Fatal(e)
				}
			}
			before, e := os.ReadFile(cfg.Path)
			if e != nil && kind != "missing-permission" {
				t.Fatal(e)
			}
			if kind == "file-permission" {
				if e := handleChmod(cfg.Path, 0400); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "directory-permission" || kind == "missing-permission" {
				if e := handleChmod(root, 0500); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() {
					if e := handleChmod(root, 0700); e != nil {
						t.Error(e)
					}
				})
			}
			var mode os.FileMode
			if kind != "missing-permission" {
				info, e := os.Stat(cfg.Path)
				if e != nil {
					t.Fatal(e)
				}
				mode = info.Mode()
			}
			entries, e := os.ReadDir(root)
			if e != nil {
				t.Fatal(e)
			}
			d, e := db.Open(context.Background(), cfg)
			if d != nil || e == nil {
				t.Fatalf("accepted %s", kind)
			}
			after, e := os.ReadFile(cfg.Path)
			if kind == "missing-permission" {
				if !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("created file: %v", e)
				}
			} else {
				if e != nil || !bytes.Equal(before, after) {
					t.Fatalf("changed bytes: %v", e)
				}
				info, e := os.Stat(cfg.Path)
				if e != nil || info.Mode() != mode {
					t.Fatalf("changed mode: %v %v", info, e)
				}
			}
			afterEntries, e := os.ReadDir(root)
			if e != nil || len(afterEntries) != len(entries) {
				t.Fatalf("created sidecars: %v %v", afterEntries, e)
			}
		})
	}
}

// R-M1BB-7ZOX R-M2J7-LRFM R-1E7C-BZP8 R-R3AL-WW3P R-MCAE-NXD6 R-4XMP-R9EQ R-MH60-70BY
func TestHandleTransactions(t *testing.T) {
	d := handleOpen(t, handleConfig(filepath.Join(t.TempDir(), "db")))
	ctx := context.Background()
	sentinel := errors.New("callback error")
	calls := 0
	if e := d.Read(ctx, func(tx *sql.Tx) error {
		calls++
		if tx == nil {
			t.Fatal("nil tx")
		}
		return nil
	}); e != nil || calls != 1 {
		t.Fatalf("read: %d %v", calls, e)
	}
	if e := d.Read(ctx, func(*sql.Tx) error { return sentinel }); !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	if e := d.Read(ctx, func(tx *sql.Tx) error {
		if e := handleIncrement(tx); e == nil {
			t.Fatal("read permitted mutation")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if n := handleCount(t, d); n != 0 {
		t.Fatal(n)
	}
	calls = 0
	if e := d.Write(ctx, func(tx *sql.Tx) error {
		calls++
		if tx == nil {
			t.Fatal("nil tx")
		}
		return handleIncrement(tx)
	}); e != nil || calls != 1 {
		t.Fatalf("write: %d %v", calls, e)
	}
	if n := handleCount(t, d); n != 1 {
		t.Fatal(n)
	}
	calls = 0
	if e := d.Write(ctx, func(tx *sql.Tx) error {
		calls++
		if e := handleIncrement(tx); e != nil {
			return e
		}
		return sentinel
	}); !errors.Is(e, sentinel) || calls != 1 {
		t.Fatalf("callback error: calls=%d err=%v", calls, e)
	}
	if n := handleCount(t, d); n != 1 {
		t.Fatal(n)
	}
	canceled, cancel := context.WithCancel(ctx)
	calls = 0
	if e := d.Write(canceled, func(tx *sql.Tx) error {
		calls++
		if e := handleIncrement(tx); e != nil {
			return e
		}
		cancel()
		return nil
	}); e == nil || calls > 1 {
		t.Fatalf("canceled write: calls=%d err=%v", calls, e)
	}
	if n := handleCount(t, d); n != 1 {
		t.Fatal(n)
	}
}

// R-4V6W-ZPXC R-4WET-DHO1 R-AZRT-F9EE R-B0ZP-T153 R-MJLS-YJTC
func TestHandlePanicsReleaseTransactions(t *testing.T) {
	d := handleOpen(t, handleConfig(filepath.Join(t.TempDir(), "db")))
	value := &struct{ n int }{42}
	for i := 0; i < db.Readers+1; i++ {
		func() {
			defer func() {
				if got := recover(); got != value {
					t.Fatalf("panic=%v", got)
				}
			}()
			_ = d.Read(context.Background(), func(*sql.Tx) error { panic(value) })
		}()
	}
	if n := handleCount(t, d); n != 0 {
		t.Fatal(n)
	}
	func() {
		defer func() {
			if got := recover(); got != value {
				t.Fatalf("panic=%v", got)
			}
		}()
		_ = d.Write(context.Background(), func(tx *sql.Tx) error {
			if e := handleIncrement(tx); e != nil {
				t.Fatal(e)
			}
			panic(value)
		})
	}()
	if n := handleCount(t, d); n != 0 {
		t.Fatal(n)
	}
	if e := d.Write(context.Background(), handleIncrement); e != nil {
		t.Fatal(e)
	}
	if n := handleCount(t, d); n != 1 {
		t.Fatal(n)
	}
}

// R-R4II-ANUE R-MOHE-HMS4 R-B4NE-YCD6 R-B5VB-C43V R-MQX7-969I R-B737-PVUK R-R5QE-OFL3 R-Q9WK-OVVK
func TestHandleCloseAndFailureSeam(t *testing.T) {
	cfg := handleConfig(filepath.Join(t.TempDir(), "db"))
	d := handleOpen(t, cfg)
	other := handleOpen(t, cfg)
	ctx := context.Background()
	if e := d.Write(ctx, handleIncrement); e != nil {
		t.Fatal(e)
	}
	d.SetFailing(true)
	for _, run := range []func(context.Context, func(*sql.Tx) error) error{d.Read, d.Write} {
		if e := run(ctx, func(*sql.Tx) error { t.Fatal("failing callback ran"); return nil }); e == nil {
			t.Fatal("failure seam succeeded")
		}
	}
	if n := handleCount(t, other); n != 1 {
		t.Fatal(n)
	}
	if e := other.Write(ctx, func(*sql.Tx) error { return nil }); e != nil {
		t.Fatal(e)
	}
	d.SetFailing(false)
	if n := handleCount(t, d); n != 1 {
		t.Fatal(n)
	}
	if e := d.Write(ctx, handleIncrement); e != nil {
		t.Fatal(e)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	for _, run := range []func(context.Context, func(*sql.Tx) error) error{d.Read, d.Write} {
		if e := run(ctx, func(*sql.Tx) error { t.Fatal("closed callback ran"); return nil }); e == nil {
			t.Fatal("closed call succeeded")
		}
	}
	reopened := handleOpen(t, cfg)
	if n := handleCount(t, reopened); n != 2 {
		t.Fatal(n)
	}
}

// waitContext signals when database/sql has reached its pool-wait select:
// the first Done call checks cancellation, the second is the waiting select.
type waitContext struct {
	context.Context
	calls   atomic.Int32
	waiting chan struct{}
}

func (c *waitContext) Done() <-chan struct{} {
	if c.calls.Add(1) == 2 {
		close(c.waiting)
	}
	return c.Context.Done()
}

// R-M4Z0-DAX0 R-Q50Z-5SWS R-AYJX-1HNP R-Q68V-JKNH R-Q7GR-XCE6 R-MFY3-T8L9
func TestHandleScheduling(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "write"}[write], func(t *testing.T) {
			d := handleOpen(t, handleConfig(filepath.Join(t.TempDir(), "db")))
			run := d.Read
			slots := db.Readers
			if write {
				run = d.Write
				slots = 1
			}
			entered := make(chan struct{}, slots)
			releases := make([]chan struct{}, slots)
			done := make(chan error, slots)
			for i := 0; i < slots; i++ {
				releases[i] = make(chan struct{})
				release := releases[i]
				go func() {
					done <- run(context.Background(), func(*sql.Tx) error { entered <- struct{}{}; <-release; return nil })
				}()
			}
			for i := 0; i < slots; i++ {
				<-entered
			}
			if write {
				if e := d.Read(context.Background(), func(*sql.Tx) error { return nil }); e != nil {
					t.Fatal(e)
				}
			}
			for _, cancelWait := range []bool{true, false} {
				base, cancel := context.WithCancel(context.Background())
				waiting := &waitContext{Context: base, waiting: make(chan struct{})}
				called := make(chan struct{}, 1)
				result := make(chan error, 1)
				go func() { result <- run(waiting, func(*sql.Tx) error { called <- struct{}{}; return nil }) }()
				<-waiting.waiting
				select {
				case <-called:
					t.Fatal("callback ran without a slot")
				default:
				}
				select {
				case e := <-result:
					t.Fatalf("returned without a slot: %v", e)
				default:
				}
				if cancelWait {
					cancel()
					if e := <-result; !errors.Is(e, context.Canceled) {
						t.Fatal(e)
					}
					select {
					case <-called:
						t.Fatal("canceled callback ran")
					default:
					}
				} else {
					close(releases[0])
					if e := <-result; e != nil {
						t.Fatal(e)
					}
					<-called
					cancel()
				}
			}
			for i := 1; i < slots; i++ {
				close(releases[i])
			}
			for i := 0; i < slots; i++ {
				if e := <-done; e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-MDIB-1P3V R-7RH2-NJKK
func TestHandleConcurrentUse(t *testing.T) {
	d := handleOpen(t, handleConfig(filepath.Join(t.TempDir(), "db")))
	const n = 16
	var wg sync.WaitGroup
	var active atomic.Int32
	for i := 0; i < n; i++ {
		wg.Go(func() {
			if e := d.Write(context.Background(), func(tx *sql.Tx) error {
				if active.Add(1) != 1 {
					t.Error("concurrent writer callbacks")
				}
				defer active.Add(-1)
				var count int
				if e := tx.QueryRow("SELECT n FROM counter").Scan(&count); e != nil {
					return e
				}
				_, e := tx.Exec("UPDATE counter SET n=?", count+1)
				return e
			}); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if got := handleCount(t, d); got != n {
		t.Fatal(got)
	}
	for i := 0; i < n; i++ {
		wg.Go(func() {
			d.SetFailing(true)
			_ = d.Read(context.Background(), func(*sql.Tx) error { return nil })
			_ = d.Write(context.Background(), func(*sql.Tx) error { return nil })
			d.SetFailing(false)
		})
	}
	wg.Wait()
	d.SetFailing(false)
	if got := handleCount(t, d); got != n {
		t.Fatal(got)
	}
}

func handleChmod(path string, mode os.FileMode) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.Chmod(filepath.Base(path), mode)
}
