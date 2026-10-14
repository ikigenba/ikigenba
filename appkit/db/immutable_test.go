package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/db"
)

func immutableFixture(t *testing.T, path string) {
	t.Helper()
	b, err := db.OpenBulk(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := b.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE records(n INTEGER); INSERT INTO records VALUES(42)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

func immutableOpen(t *testing.T, path string) *db.Immutable {
	t.Helper()
	i, err := db.OpenImmutable(context.Background(), path)
	if err != nil || i == nil {
		t.Fatalf("open: %v %v", i, err)
	}
	t.Cleanup(func() {
		if err := i.Close(); err != nil {
			t.Error(err)
		}
	})
	return i
}

func immutableValue(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow("SELECT n FROM records").Scan(&n); err != nil {
		return err
	}
	if n != 42 {
		return fmt.Errorf("value = %d", n)
	}
	return nil
}

// R-FUTW-P30G R-FW1T-2UR5 R-GWMS-JYZZ R-H563-8D6U R-H6DZ-M4XJ
func TestImmutableOpenPreservesReadOnlyFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := "records #%25.db"
	immutableFixture(t, path)
	if err := handleChmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if err := handleChmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handleChmod(root, 0700); err != nil {
			t.Error(err)
		}
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	api := struct {
		Open  func(context.Context, string) (*db.Immutable, error)
		Read  func(context.Context, func(*sql.Tx) error) error
		Close func() error
	}{Open: db.OpenImmutable}
	i, err := api.Open(context.Background(), path)
	if err != nil || i == nil {
		t.Fatalf("open: %v %v", i, err)
	}
	t.Cleanup(func() { _ = i.Close() })
	api.Read, api.Close = i.Read, i.Close
	if err := api.Read(context.Background(), immutableValue); err != nil {
		t.Fatal(err)
	}
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("changed file: %v", err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil || afterInfo.Mode() != info.Mode() {
		t.Fatalf("changed mode: %v %v", afterInfo, err)
	}
	afterEntries, err := os.ReadDir(root)
	if err != nil || !reflect.DeepEqual(entries, afterEntries) {
		t.Fatalf("changed directory: %v %v", afterEntries, err)
	}
}

// R-GXUO-XQQO R-GZ2L-BIHD R-H1IE-31YR R-H2QA-GTPG
func TestImmutableRejectsPaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, path := range []string{"", ":memory:", "file:nested/db", "nested/db?option"} {
		i, err := db.OpenImmutable(context.Background(), path)
		if i != nil || err == nil {
			t.Fatalf("accepted %q: %v %v", path, i, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	i, err := db.OpenImmutable(ctx, "nested/db")
	if i != nil || !errors.Is(err, ctx.Err()) {
		t.Fatalf("canceled: %v %v", i, err)
	}
	i, err = db.OpenImmutable(context.Background(), "nested/missing")
	if i != nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v %v", i, err)
	}
	i, err = db.OpenImmutable(context.Background(), root)
	if i != nil || err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("directory: %v %v", i, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("created entries: %v %v", entries, err)
	}
}

// R-H3Y6-ULG5
func TestImmutableRefusesCorruptionAtOpen(t *testing.T) {
	for _, data := range [][]byte{[]byte("short"), bytes.Repeat([]byte("invalid database"), 32)} {
		t.Run(fmt.Sprint(len(data)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "broken")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			i, err := db.OpenImmutable(context.Background(), path)
			if i != nil || err == nil || errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("corrupt: %v %v", i, err)
			}
			after, err := os.ReadFile(filepath.Clean(path))
			if err != nil || !bytes.Equal(after, data) {
				t.Fatalf("changed bytes: %v", err)
			}
			afterInfo, err := os.Stat(path)
			if err != nil || afterInfo.Mode() != info.Mode() {
				t.Fatalf("changed mode: %v %v", afterInfo, err)
			}
		})
	}
}

// R-H7LV-ZWO8 R-H8TS-DOEX R-HCHH-IZN0
func TestImmutableReadTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	immutableFixture(t, path)
	i := immutableOpen(t, path)
	calls := 0
	if err := i.Read(context.Background(), func(tx *sql.Tx) error {
		calls++
		if tx == nil {
			t.Fatal("nil transaction")
		}
		return immutableValue(tx)
	}); err != nil || calls != 1 {
		t.Fatalf("read: calls=%d err=%v", calls, err)
	}
	sentinel := errors.New("callback failed")
	if err := i.Read(context.Background(), func(*sql.Tx) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("callback error: %v", err)
	}
	if err := i.Read(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO records VALUES(7)"); err == nil {
			t.Fatal("mutation succeeded")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// R-HA1O-RG5M
func TestImmutablePanicReleasesTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	immutableFixture(t, path)
	i := immutableOpen(t, path)
	sentinel := &struct{ n int }{42}
	for range db.Readers + 1 {
		func() {
			defer func() {
				if value := recover(); value != sentinel {
					t.Fatalf("panic: %v", value)
				}
			}()
			_ = i.Read(context.Background(), func(tx *sql.Tx) error {
				if err := immutableValue(tx); err != nil {
					t.Fatal(err)
				}
				panic(sentinel)
			})
		}()
	}
	if err := i.Read(context.Background(), immutableValue); err != nil {
		t.Fatal(err)
	}
}

// R-HB9L-57WB
func TestImmutableReadsThroughExclusiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	immutableFixture(t, path)
	i := immutableOpen(t, path)
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	conn, err := locker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") })
	calls := 0
	if err := i.Read(context.Background(), func(tx *sql.Tx) error {
		calls++
		return immutableValue(tx)
	}); err != nil || calls != 1 {
		t.Fatalf("locked read: calls=%d err=%v", calls, err)
	}
}

// R-HDPD-WRDP
func TestImmutableConcurrentReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	immutableFixture(t, path)
	i := immutableOpen(t, path)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		wg.Go(func() {
			<-start
			if err := i.Read(context.Background(), immutableValue); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
}

// R-HEXA-AJ4E R-HG56-OAV3 R-HHD3-22LS
func TestImmutableClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	immutableFixture(t, path)
	i := immutableOpen(t, path)
	for range 3 {
		if err := i.Close(); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	if err := i.Read(context.Background(), func(*sql.Tx) error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatalf("closed read: calls=%d err=%v", calls, err)
	}
}
