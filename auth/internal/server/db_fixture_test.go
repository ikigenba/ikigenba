package server

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/auth"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

var serverStoreHandles sync.Map

func openServerStore(t *testing.T, path string, random io.Reader, now func() time.Time) *store.Store {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: auth.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(handle, random)
	serverStoreHandles.Store(st, handle)
	t.Cleanup(func() {
		serverStoreHandles.Delete(st)
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	})
	return st
}

func serverStoreDB(t *testing.T, st *store.Store) *db.DB {
	t.Helper()
	handle, ok := serverStoreHandles.Load(st)
	if !ok {
		t.Fatal("store has no test-owned database handle")
	}
	return handle.(*db.DB)
}

func failServerStore(t *testing.T, st *store.Store) {
	t.Helper()
	serverStoreDB(t, st).SetFailing(true)
}
