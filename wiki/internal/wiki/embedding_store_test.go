package wiki

import (
	"context"
	"reflect"
	"testing"
)

func TestEncodeDecodeVecRoundTripsFloat32Values(t *testing.T) {
	// R-9OCK-FJK1
	vec := []float32{-1, 0, 0.25, 1.5, 3.4028235e+38}

	encoded := encodeVec(vec)
	if got, want := len(encoded), len(vec)*4; got != want {
		t.Fatalf("len(encodeVec) = %d, want %d", got, want)
	}
	decoded, err := decodeVec(encoded)
	if err != nil {
		t.Fatalf("decodeVec: %v", err)
	}
	if !reflect.DeepEqual(decoded, vec) {
		t.Fatalf("decodeVec(encodeVec(vec)) = %#v, want %#v", decoded, vec)
	}
}

func TestDecodeVecErrorsOnNonFloat32Length(t *testing.T) {
	// R-9PKG-TBAQ
	if _, err := decodeVec([]byte{0, 1, 2}); err == nil {
		t.Fatal("decodeVec length 3 err = nil, want error")
	}
}

func TestEmbeddingStoreUpsertReplacesSubjectEmbedding(t *testing.T) {
	// R-9QSD-731F
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-1")

	store := NewEmbeddingStore(conn)
	if err := store.Upsert(ctx, Embedding{
		SubjectID:   "subject-1",
		Model:       "model-a",
		Dims:        2,
		Vec:         []float32{0.25, 0.75},
		ContentHash: "hash-a",
		UpdatedAt:   101,
	}); err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}
	replacement := Embedding{
		SubjectID:   "subject-1",
		Model:       "model-b",
		Dims:        3,
		Vec:         []float32{0.1, 0.2, 0.3},
		ContentHash: "hash-b",
		UpdatedAt:   202,
	}
	if err := store.Upsert(ctx, replacement); err != nil {
		t.Fatalf("replacement Upsert: %v", err)
	}

	got, err := store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadAll len = %d, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], replacement) {
		t.Fatalf("LoadAll[0] = %#v, want %#v", got[0], replacement)
	}
}

func TestEmbeddingStoreLoadAllReturnsEveryEmbedding(t *testing.T) {
	// R-9S09-KUS4
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-a")
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-b")

	store := NewEmbeddingStore(conn)
	want := []Embedding{
		{SubjectID: "subject-a", Model: "model-a", Dims: 2, Vec: []float32{0.6, 0.8}, ContentHash: "hash-a", UpdatedAt: 1000},
		{SubjectID: "subject-b", Model: "model-b", Dims: 1, Vec: []float32{1}, ContentHash: "hash-b", UpdatedAt: 1001},
	}
	for _, embedding := range []Embedding{want[1], want[0]} {
		if err := store.Upsert(ctx, embedding); err != nil {
			t.Fatalf("Upsert %s: %v", embedding.SubjectID, err)
		}
	}

	got, err := store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadAll = %#v, want %#v", got, want)
	}
}

func TestEmbeddingStoreDeleteRemovesOnlyRequestedSubject(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-winner")
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-loser")

	store := NewEmbeddingStore(conn)
	remaining := Embedding{SubjectID: "subject-winner", Model: "model-a", Dims: 2, Vec: []float32{0.6, 0.8}, ContentHash: "hash-winner", UpdatedAt: 100}
	deleted := Embedding{SubjectID: "subject-loser", Model: "model-a", Dims: 2, Vec: []float32{0.8, 0.6}, ContentHash: "hash-loser", UpdatedAt: 101}
	for _, embedding := range []Embedding{remaining, deleted} {
		if err := store.Upsert(ctx, embedding); err != nil {
			t.Fatalf("Upsert %s: %v", embedding.SubjectID, err)
		}
	}

	if err := store.Delete(ctx, "subject-loser"); err != nil {
		t.Fatalf("Delete loser: %v", err)
	}

	got, err := store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if !reflect.DeepEqual(got, []Embedding{remaining}) {
		t.Fatalf("LoadAll after Delete = %#v, want only winner embedding", got)
	}
}

func TestEmbeddingStoreUpsertRequiresLiveSubjectAtWriteTime(t *testing.T) {
	// R-R7IF-IID7
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if _, err := NewScopeStore(conn).Create(ctx, "doomed"); err != nil {
		t.Fatalf("Create doomed scope: %v", err)
	}
	seedEmbeddingSubject(t, ctx, conn, "doomed", "deleted-subject")
	seedEmbeddingSubject(t, ctx, conn, "default", "live-subject")
	if err := NewScopeStore(conn).Delete(ctx, "doomed"); err != nil {
		t.Fatalf("Delete doomed scope: %v", err)
	}

	store := NewEmbeddingStore(conn)
	for _, subjectID := range []string{"deleted-subject", "live-subject"} {
		if err := store.Upsert(ctx, Embedding{
			SubjectID: subjectID, Model: "model", Dims: 1, Vec: []float32{1}, ContentHash: "hash", UpdatedAt: 1,
		}); err != nil {
			t.Fatalf("Upsert %s: %v", subjectID, err)
		}
	}

	var deletedRows, liveRows int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM page_embeddings WHERE subject_id = ?`, "deleted-subject").Scan(&deletedRows); err != nil {
		t.Fatalf("count deleted subject embeddings: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM page_embeddings WHERE subject_id = ?`, "live-subject").Scan(&liveRows); err != nil {
		t.Fatalf("count live subject embeddings: %v", err)
	}
	if deletedRows != 0 || liveRows != 1 {
		t.Fatalf("embedding rows: deleted=%d live=%d, want deleted=0 live=1", deletedRows, liveRows)
	}
}

func TestLoadVectorCacheEntriesOmitsOrphanAndKeepsLiveScopes(t *testing.T) {
	// R-R8QB-WA3W
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if _, err := NewScopeStore(conn).Create(ctx, "team"); err != nil {
		t.Fatalf("Create team scope: %v", err)
	}
	seedEmbeddingSubject(t, ctx, conn, "default", "live-default")
	seedEmbeddingSubject(t, ctx, conn, "team", "live-team")
	pages := NewPageStore(conn)
	for _, page := range []Page{
		{ID: "live-default", SubjectID: "live-default", Title: "Default Page", Body: "Default body"},
		{ID: "live-team", SubjectID: "live-team", Title: "Team Page", Body: "Team body"},
	} {
		if err := pages.Upsert(ctx, page); err != nil {
			t.Fatalf("Upsert page %s: %v", page.SubjectID, err)
		}
	}
	store := NewEmbeddingStore(conn)
	for _, embedding := range []Embedding{
		{SubjectID: "live-default", Model: "model", Dims: 2, Vec: []float32{1, 0}, ContentHash: "default-hash", UpdatedAt: 1},
		{SubjectID: "live-team", Model: "model", Dims: 2, Vec: []float32{0, 1}, ContentHash: "team-hash", UpdatedAt: 2},
	} {
		if err := store.Upsert(ctx, embedding); err != nil {
			t.Fatalf("Upsert embedding %s: %v", embedding.SubjectID, err)
		}
	}
	seedOrphanEmbedding(t, ctx, conn, "orphan")

	got, err := LoadVectorCacheEntries(ctx, conn)
	if err != nil {
		t.Fatalf("LoadVectorCacheEntries: %v", err)
	}
	want := []VectorCacheEntry{
		{Scope: "default", SubjectID: "live-default", Title: "Default Page", Vec: []float32{1, 0}},
		{Scope: "team", SubjectID: "live-team", Title: "Team Page", Vec: []float32{0, 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadVectorCacheEntries = %#v, want %#v", got, want)
	}
}

func seedEmbeddingSubject(t *testing.T, ctx context.Context, conn sqlStore, scope, subjectID string) {
	t.Helper()
	if err := NewSubjectStore(conn).Save(ctx, scope, Subject{ID: subjectID, Name: subjectID, Type: "entity"}); err != nil {
		t.Fatalf("Save subject %s: %v", subjectID, err)
	}
}

func seedOrphanEmbedding(t *testing.T, ctx context.Context, conn sqlStore, subjectID string) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO page_embeddings (subject_id, model, dims, vec, content_hash, updated_at)
		VALUES (?, 'model', 1, ?, 'orphan-hash', 1)`, subjectID, encodeVec([]float32{1})); err != nil {
		t.Fatalf("seed orphan embedding %s: %v", subjectID, err)
	}
}
