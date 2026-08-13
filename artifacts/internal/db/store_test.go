package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	appkitdb "appkit/db"
)

// R-3D7T-1KPU
func TestMigrationsCreateDeclaredSchemaAndVisibilityChecks(t *testing.T) {
	conn := migratedDB(t)
	for table, want := range map[string][]string{
		"artifacts": {"id", "owner_id", "owner_email", "filename", "description", "visibility", "size", "content_hash", "download_count", "created_at", "updated_at"},
		"uploads":   {"token", "owner_id", "owner_email", "filename", "description", "visibility", "created_at", "expires_at", "consumed_at", "artifact_id"},
	} {
		rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, kind string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				t.Fatal(err)
			}
			got = append(got, name)
		}
		_ = rows.Close()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s columns = %v, want %v", table, got, want)
		}
	}

	_, artifactErr := conn.Exec(`INSERT INTO artifacts
		(id, owner_id, owner_email, filename, visibility, size, content_hash, created_at, updated_at)
		VALUES ('a', 'owner', 'owner@example.com', 'a.txt', 'shared', 1, 'hash', 'now', 'now')`)
	if artifactErr == nil {
		t.Fatal("artifacts accepted invalid visibility")
	}
	_, uploadErr := conn.Exec(`INSERT INTO uploads
		(token, owner_id, owner_email, filename, visibility, created_at, expires_at)
		VALUES ('u', 'owner', 'owner@example.com', 'a.txt', 'shared', 'now', 'later')`)
	if uploadErr == nil {
		t.Fatal("uploads accepted invalid visibility")
	}
}

func TestCreateUploadRetriesOneInjectedTokenCollision(t *testing.T) {
	conn := migratedDB(t)
	tokens := []string{"collision", "collision", "success"}
	index := 0
	store := NewStore(conn, func() string {
		value := tokens[index]
		index++
		return value
	})
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	first, err := store.CreateUpload(context.Background(), uploadParams(now))
	if err != nil || first.Token != "collision" {
		t.Fatalf("first create = %#v, %v", first, err)
	}
	second, err := store.CreateUpload(context.Background(), uploadParams(now))
	if err != nil || second.Token != "success" {
		t.Fatalf("colliding create = %#v, %v; want retry token success", second, err)
	}
	if index != 3 {
		t.Fatalf("generator called %d times, want exactly one retry", index)
	}
}

// R-3I3E-KNOM
func TestConsumeUploadHasExactlyOneWinner(t *testing.T) {
	store := NewStore(migratedDB(t), sequenceTokens())
	now := time.Date(2026, 8, 10, 12, 0, 0, 123, time.UTC)
	upload, err := store.CreateUpload(context.Background(), uploadParams(now))
	if err != nil {
		t.Fatal(err)
	}
	wonFirst, err := store.ConsumeUpload(context.Background(), upload.Token, "artifact-first", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	wonSecond, err := store.ConsumeUpload(context.Background(), upload.Token, "artifact-second", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !wonFirst || wonSecond {
		t.Fatalf("consume winners = first:%v second:%v, want exactly first", wonFirst, wonSecond)
	}
	got, err := store.GetUpload(context.Background(), upload.Token)
	if err != nil || got.ConsumedAt == nil || !got.ConsumedAt.Equal(now.Add(time.Hour)) || got.ArtifactID == nil || *got.ArtifactID != "artifact-first" {
		t.Fatalf("consumed upload = %#v, %v", got, err)
	}
}

// R-3JBA-YFFB
func TestPurgeExpiredUploadsPreservesFutureAndConsumedRows(t *testing.T) {
	store := NewStore(migratedDB(t), sequenceTokens())
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	expired, err := store.CreateUpload(context.Background(), uploadParamsWithExpiry(now, now))
	if err != nil {
		t.Fatal(err)
	}
	future, err := store.CreateUpload(context.Background(), uploadParamsWithExpiry(now, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := store.CreateUpload(context.Background(), uploadParamsWithExpiry(now, now.Add(-time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if won, err := store.ConsumeUpload(context.Background(), consumed.Token, "finished", now); err != nil || !won {
		t.Fatalf("consume expired audit row = %v, %v", won, err)
	}
	count, err := store.PurgeExpiredUploads(context.Background(), now)
	if err != nil || count != 1 {
		t.Fatalf("purge count = %d, %v; want 1", count, err)
	}
	if _, err := store.GetUpload(context.Background(), expired.Token); err != sql.ErrNoRows {
		t.Fatalf("expired unconsumed row lookup error = %v, want sql.ErrNoRows", err)
	}
	for _, token := range []string{future.Token, consumed.Token} {
		if _, err := store.GetUpload(context.Background(), token); err != nil {
			t.Errorf("preserved upload %q missing: %v", token, err)
		}
	}
}

// R-3LR3-PYWP
func TestArtifactCountsUpdatesAndCRUDPreserveImmutableFields(t *testing.T) {
	store := NewStore(migratedDB(t), sequenceTokens())
	ctx := context.Background()
	createdAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	created, err := store.CreateArtifact(ctx, CreateArtifactParams{OwnerID: "owner-1", OwnerEmail: "owner@example.com", Filename: "old.txt", Description: "old", Visibility: "private", Size: 42, ContentHash: "abc", CreatedAt: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	for range 7 {
		if changed, err := store.IncrementDownloadCount(ctx, created.ID); err != nil || !changed {
			t.Fatalf("increment = %v, %v", changed, err)
		}
	}
	updatedAt := createdAt.Add(time.Hour)
	changed, err := store.UpdateArtifact(ctx, created.ID, UpdateArtifactParams{Filename: "new.txt", Description: "new", Visibility: "public", UpdatedAt: updatedAt})
	if err != nil || !changed {
		t.Fatalf("update = %v, %v", changed, err)
	}
	got, err := store.GetArtifact(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DownloadCount != 7 || !got.UpdatedAt.Equal(updatedAt) || !got.CreatedAt.Equal(createdAt) || got.OwnerID != created.OwnerID || got.OwnerEmail != created.OwnerEmail {
		t.Fatalf("updated artifact violated count/timestamp/owner contract: %#v", got)
	}
	listed, err := store.ListArtifacts(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list = %#v, %v", listed, err)
	}
	if deleted, err := store.DeleteArtifact(ctx, created.ID); err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	if _, err := store.GetArtifact(ctx, created.ID); err != sql.ErrNoRows {
		t.Fatalf("deleted artifact lookup error = %v, want sql.ErrNoRows", err)
	}
}

func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	return conn
}

func uploadParams(now time.Time) CreateUploadParams {
	return uploadParamsWithExpiry(now, now.Add(24*time.Hour))
}

func uploadParamsWithExpiry(now, expiry time.Time) CreateUploadParams {
	return CreateUploadParams{OwnerID: "owner", OwnerEmail: "owner@example.com", Filename: "file.txt", Description: "description", Visibility: "private", CreatedAt: now, ExpiresAt: expiry}
}

func sequenceTokens() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("token-%d", n)
	}
}
