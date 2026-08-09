package repos

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	appdb "appkit/db"
	servicedb "repos/internal/db"
)

func TestStoreRoundTripsMetadataUpsertsStatusesAndSweepsTokens(t *testing.T) {
	// R-J02N-Y67K
	ctx := context.Background()
	conn, store := newTestStore(t)
	created := time.Date(2026, 8, 8, 12, 13, 14, 123, time.UTC)
	archivedAt, archivedPath := created.Add(time.Hour), "code/plane.git"
	repository := Repository{Kind: "code", Name: "plane", OwnerID: "owner-1", OwnerEmail: "owner@example.com", DefaultBranch: "main", ArchivedAt: &archivedAt, ArchivedPath: &archivedPath, CreatedAt: created}
	inTx(t, conn, func(tx *sql.Tx) error { return store.InsertRepository(ctx, tx, repository) })
	gotRepository, err := store.GetRepository(ctx, repository.Kind, repository.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotRepository, repository) {
		t.Errorf("repository round trip = %#v, want %#v", gotRepository, repository)
	}

	detail1, detail2 := "queued", "https://checks.example/result"
	status := Status{Kind: "code", Name: "plane", SHA: "0123456789012345678901234567890123456789", CheckName: "test", State: "pending", Detail: &detail1, Actor: "worker-1", UpdatedAt: created.Add(time.Minute)}
	inTx(t, conn, func(tx *sql.Tx) error { return store.SetStatus(ctx, tx, status) })
	status.State, status.Detail, status.Actor, status.UpdatedAt = "success", &detail2, "worker-2", created.Add(2*time.Minute)
	inTx(t, conn, func(tx *sql.Tx) error { return store.SetStatus(ctx, tx, status) })
	statuses, err := store.ListStatuses(ctx, status.Kind, status.Name, status.SHA)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(statuses, []Status{status}) {
		t.Errorf("statuses after upsert = %#v, want only %#v", statuses, status)
	}
	empty, err := store.ListStatuses(ctx, status.Kind, status.Name, "ffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("unseen sha statuses = %#v, want non-nil empty slice", empty)
	}

	expired := RunToken{TokenSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "code", Name: "plane", ExpiresAt: created.Add(time.Hour), CreatedAt: created}
	live := RunToken{TokenSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "code", Name: "plane", ExpiresAt: created.Add(3 * time.Hour), CreatedAt: created.Add(time.Second)}
	inTx(t, conn, func(tx *sql.Tx) error {
		if err := store.InsertRunToken(ctx, tx, expired); err != nil {
			return err
		}
		return store.InsertRunToken(ctx, tx, live)
	})
	gotToken, err := store.LookupRunToken(ctx, live.TokenSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotToken, live) {
		t.Errorf("run token round trip = %#v, want %#v", gotToken, live)
	}
	inTx(t, conn, func(tx *sql.Tx) error {
		deleted, err := store.SweepExpiredTokens(ctx, tx, created.Add(2*time.Hour))
		if err == nil && deleted != 1 {
			t.Errorf("deleted tokens = %d, want 1", deleted)
		}
		return err
	})
	if _, err := store.LookupRunToken(ctx, expired.TokenSHA256); err != sql.ErrNoRows {
		t.Errorf("expired token lookup error = %v, want sql.ErrNoRows", err)
	}
	if _, err := store.LookupRunToken(ctx, live.TokenSHA256); err != nil {
		t.Errorf("live token was swept: %v", err)
	}
}

func TestArchiveAndOutboxAppendRollBackAtomically(t *testing.T) {
	// R-J1AK-BXY9
	ctx := context.Background()
	conn, store := newTestStore(t)
	repository := Repository{Kind: "sites", Name: "docs", OwnerID: "owner", OwnerEmail: "owner@example.com", DefaultBranch: "main", CreatedAt: time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)}
	inTx(t, conn, func(tx *sql.Tx) error { return store.InsertRepository(ctx, tx, repository) })
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveRepository(ctx, tx, "sites", "docs", repository.CreatedAt.Add(time.Hour), "sites/docs.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox (event_id, kind, subject, payload, created_at, correlation_id)
		VALUES ('event-1', 'repository.archived', 'docs', '{"name":"docs"}', '2026-08-08T02:00:00Z', 'correlation-1')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetRepository(ctx, "sites", "docs")
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != nil || got.ArchivedPath != nil {
		t.Errorf("repository remained archived after rollback: %#v", got)
	}
	var outboxRows int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if outboxRows != 0 {
		t.Errorf("outbox rows after rollback = %d, want zero", outboxRows)
	}
}

func TestListRepositoriesScopesOnlyByOwnerIDAndExcludesArchived(t *testing.T) {
	// R-J2IG-PPOY
	ctx := context.Background()
	conn, store := newTestStore(t)
	created := time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC)
	repositories := []Repository{
		{Kind: "code", Name: "alpha", OwnerID: "owner-a", OwnerEmail: "shared@example.com", DefaultBranch: "main", CreatedAt: created},
		{Kind: "scripts", Name: "beta", OwnerID: "owner-a", OwnerEmail: "shared@example.com", DefaultBranch: "main", CreatedAt: created.Add(time.Second)},
		{Kind: "code", Name: "other", OwnerID: "owner-b", OwnerEmail: "shared@example.com", DefaultBranch: "main", CreatedAt: created.Add(2 * time.Second)},
		{Kind: "code", Name: "archived", OwnerID: "owner-a", OwnerEmail: "snapshot@example.com", DefaultBranch: "main", CreatedAt: created.Add(3 * time.Second)},
	}
	inTx(t, conn, func(tx *sql.Tx) error {
		for _, repository := range repositories {
			if err := store.InsertRepository(ctx, tx, repository); err != nil {
				return err
			}
		}
		return store.ArchiveRepository(ctx, tx, "code", "archived", created.Add(time.Hour), "code/archived.git")
	})
	gotA, err := store.ListRepositories(ctx, "owner-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotA, repositories[:2]) {
		t.Errorf("owner-a repositories = %#v, want %#v", gotA, repositories[:2])
	}
	gotB, err := store.ListRepositories(ctx, "owner-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotB, []Repository{repositories[2]}) {
		t.Errorf("owner-b repositories = %#v, want %#v", gotB, []Repository{repositories[2]})
	}
	kind := "code"
	filtered, err := store.ListRepositories(ctx, "owner-a", &kind)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(filtered, []Repository{repositories[0]}) {
		t.Errorf("owner-a code repositories = %#v, want alpha only", filtered)
	}
}

func TestListAllRepositoriesReturnsEveryOwnersLiveRowsInKeyOrder(t *testing.T) {
	// R-RP30-4NV1
	ctx := context.Background()
	conn, store := newTestStore(t)
	created := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	repositories := []Repository{
		{Kind: "sites", Name: "beta", OwnerID: "owner-a", OwnerEmail: "a@example.test", DefaultBranch: "main", CreatedAt: created},
		{Kind: "code", Name: "zed", OwnerID: "owner-a", OwnerEmail: "a@example.test", DefaultBranch: "main", CreatedAt: created.Add(time.Second)},
		{Kind: "code", Name: "alpha", OwnerID: "owner-b", OwnerEmail: "b@example.test", DefaultBranch: "main", CreatedAt: created.Add(2 * time.Second)},
		{Kind: "prompts", Name: "archived", OwnerID: "owner-b", OwnerEmail: "b@example.test", DefaultBranch: "main", CreatedAt: created.Add(3 * time.Second)},
	}
	inTx(t, conn, func(tx *sql.Tx) error {
		for _, repository := range repositories {
			if err := store.InsertRepository(ctx, tx, repository); err != nil {
				return err
			}
		}
		return store.ArchiveRepository(ctx, tx, "prompts", "archived", created.Add(time.Hour), "prompts/archived.git")
	})

	got, err := store.ListAllRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Repository{repositories[2], repositories[1], repositories[0]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("all live repositories = %#v, want kind/name ordered rows from both owners %#v", got, want)
	}
}

func newTestStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	migrations, err := servicedb.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	return conn, NewStore(conn)
}

func inTx(t *testing.T, conn *sql.DB, fn func(*sql.Tx) error) {
	t.Helper()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
