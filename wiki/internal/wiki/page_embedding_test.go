package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	appdb "appkit/db"
	"appkit/httpclient"
	"appkit/telemetry"
	"eventplane/correlation"

	wikidb "wiki/internal/db"
	"wiki/internal/extract"
	"wiki/internal/llm"
)

func TestEmbedAndStoreUsesDocumentRoleAndUpdatesStoreAndCache(t *testing.T) {
	// R-6XNX-FNXO
	// R-703Q-77F2
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-1")

	cache := &recordingVectorCache{}
	embedder := &recordingPageEmbedder{vectors: [][]float32{{0.25, 0.75}}}
	svc := NewService(
		conn,
		nil,
		nil,
		clockAt(time.Date(2026, 6, 25, 12, 30, 0, 0, time.UTC)),
		WithPageEmbedder("embed-model", embedder),
		WithVectorCacheUpdater(cache.Upsert),
	)
	page := Page{
		ID:        "subject-1",
		SubjectID: "subject-1",
		Title:     "Acme Robotics",
		Body:      "Acme Robotics opened a Tulsa lab.",
	}

	if err := svc.embedAndStore(ctx, llm.Attribution{}, "s1", page); err != nil {
		t.Fatalf("embedAndStore: %v", err)
	}
	if len(embedder.inputs) != 1 || !reflect.DeepEqual(embedder.inputs[0], []string{page.Body}) {
		t.Fatalf("embed inputs = %#v, want page body only", embedder.inputs)
	}
	if len(embedder.roles) != 1 || embedder.roles[0] != EmbedDocument {
		t.Fatalf("embed roles = %#v, want document role", embedder.roles)
	}

	embeddings, err := NewEmbeddingStore(conn).LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("embeddings len = %d, want 1", len(embeddings))
	}
	got := embeddings[0]
	if got.SubjectID != page.SubjectID || got.Model != "embed-model" || got.Dims != 2 ||
		got.ContentHash != pageFingerprint(page) || got.UpdatedAt != time.Date(2026, 6, 25, 12, 30, 0, 0, time.UTC).Unix() {
		t.Fatalf("embedding metadata = %+v, want current page fingerprint/model/dims/time", got)
	}
	if !reflect.DeepEqual(got.Vec, []float32{0.25, 0.75}) {
		t.Fatalf("embedding vec = %#v, want stored page vector", got.Vec)
	}

	if len(cache.entries) != 1 ||
		cache.entries[0].scope != "s1" ||
		cache.entries[0].subjectID != page.SubjectID ||
		cache.entries[0].title != page.Title ||
		!reflect.DeepEqual(cache.entries[0].vec, []float32{0.25, 0.75}) {
		t.Fatalf("cache entries = %+v, want upserted page vector", cache.entries)
	}
}

func TestEmbedAndStoreOverwritesExistingPageVector(t *testing.T) {
	// R-703Q-77F2
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	seedEmbeddingSubject(t, ctx, conn, "default", "subject-1")

	store := NewEmbeddingStore(conn)
	if err := store.Upsert(ctx, Embedding{
		SubjectID:   "subject-1",
		Model:       "old-model",
		Dims:        3,
		Vec:         []float32{9, 8, 7},
		ContentHash: "old-fingerprint",
		UpdatedAt:   time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC).Unix(),
	}); err != nil {
		t.Fatalf("seed old embedding: %v", err)
	}

	cache := &recordingVectorCache{}
	embedder := &recordingPageEmbedder{vectors: [][]float32{{0.5, 0.25}}}
	svc := NewService(
		conn,
		nil,
		nil,
		clockAt(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)),
		WithPageEmbedder("new-model", embedder),
		WithVectorCacheUpdater(cache.Upsert),
	)
	page := Page{
		ID:        "subject-1",
		SubjectID: "subject-1",
		Title:     "Acme Robotics",
		Body:      "Acme Robotics opened a refreshed Tulsa lab.",
	}

	if err := svc.embedAndStore(ctx, llm.Attribution{}, "s1", page); err != nil {
		t.Fatalf("embedAndStore: %v", err)
	}

	embeddings, err := store.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("embeddings len = %d, want one row for overwritten subject", len(embeddings))
	}
	got := embeddings[0]
	if got.SubjectID != page.SubjectID ||
		got.Model != "new-model" ||
		got.Dims != 2 ||
		got.ContentHash != pageFingerprint(page) ||
		got.UpdatedAt != time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC).Unix() ||
		!reflect.DeepEqual(got.Vec, []float32{0.5, 0.25}) {
		t.Fatalf("embedding = %+v, want overwritten current page vector", got)
	}
	if len(cache.entries) != 1 ||
		cache.entries[0].scope != "s1" ||
		cache.entries[0].subjectID != page.SubjectID ||
		cache.entries[0].title != page.Title ||
		!reflect.DeepEqual(cache.entries[0].vec, []float32{0.5, 0.25}) {
		t.Fatalf("cache entries = %+v, want updated current page vector", cache.entries)
	}
}

func TestProcessNextEmbedsCommittedPageAfterIngest(t *testing.T) {
	// R-6YVT-TFOD
	// R-71BM-KZ5R
	// R-72JI-YQWG
	// R-73RF-CIN5
	ctx := context.Background()
	chainID := "01KZ6V08B73Q7W1G5GR3C2E5MK"
	conns, cleanup := migratedEmbeddingConns(t, ctx)
	defer cleanup()

	embedder := &recordingPageEmbedder{
		vectors: [][]float32{{1, 0}},
		onEmbed: func(_ context.Context, inputs []string, _ EmbedRole) error {
			page, err := NewPageStore(conns.Read).GetBySubject(ctx, "subject-1")
			if err != nil {
				return err
			}
			if len(inputs) != 1 || inputs[0] != page.Body {
				return errors.New("embed input did not match committed page body")
			}
			return nil
		},
	}
	svc := NewService(
		conns,
		&recordingExtractor{batches: [][]extract.ExtractedSubject{{
			{
				Type:   "entity",
				Kind:   "company",
				Name:   "Acme Robotics",
				Claims: []string{"Acme Robotics opened a committed Tulsa lab."},
			},
		}}},
		&recordingCompiler{},
		sequenceTimes(
			time.Date(2026, 6, 25, 13, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 25, 13, 0, 1, 0, time.UTC),
			time.Date(2026, 6, 25, 13, 0, 2, 0, time.UTC),
			time.Date(2026, 6, 25, 13, 0, 3, 0, time.UTC),
		),
		WithPageEmbedder("embed-model", embedder),
	)
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1")

	if _, err := svc.Ingest(correlation.WithContext(ctx, chainID), "default", "owner-id", "owner@example.com", "source", "Source", nil); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	processed, err := svc.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatal("ProcessNext processed = false, want true")
	}

	embeddings, err := NewEmbeddingStore(conns.Read).LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("embeddings len = %d, want 1", len(embeddings))
	}
	if embeddings[0].SubjectID != "subject-1" || !reflect.DeepEqual(embeddings[0].Vec, []float32{1, 0}) {
		t.Fatalf("embedding = %+v, want committed subject-1 vector", embeddings[0])
	}
	if embeddings[0].UpdatedAt != time.Date(2026, 6, 25, 13, 0, 3, 0, time.UTC).Unix() {
		t.Fatalf("updated_at = %d, want post-commit embedding time", embeddings[0].UpdatedAt)
	}
	if len(embedder.attrs) != 1 || embedder.attrs[0].GroupID != chainID {
		t.Fatalf("embed attribution = %+v, want stored chain %q (not job id %q)", embedder.attrs, chainID, "job-1")
	}
}

func TestEmbeddingCatchUpUsesOneFreshRootPerDrainCycle(t *testing.T) {
	// R-KIH2-R4UC
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	pages := NewPageStore(conn)
	for _, p := range []Page{
		{ID: "page-a", SubjectID: "subject-a", Title: "A", Body: "A body"},
		{ID: "page-b", SubjectID: "subject-b", Title: "B", Body: "B body"},
		{ID: "page-c", SubjectID: "subject-c", Title: "C", Body: "C body"},
	} {
		if err := NewSubjectStore(conn).Save(ctx, Subject{ID: p.SubjectID, Name: p.Title, Type: "entity"}); err != nil {
			t.Fatalf("seed subject %s: %v", p.SubjectID, err)
		}
		if err := pages.Upsert(ctx, p); err != nil {
			t.Fatalf("seed page %s: %v", p.ID, err)
		}
	}
	type promptCall struct {
		Name    string `json:"name"`
		GroupID string `json:"group_id"`
		Header  string
	}
	var calls []promptCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call promptCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		call.Header = r.Header.Get(correlation.Header)
		calls = append(calls, call)
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": [][]float32{{float32(len(calls))}}})
	}))
	defer server.Close()
	hc := httpclient.New(httpclient.Options{Recorder: &telemetry.Recorder{}, Timeout: -1})
	embedder := sweepPromptsEmbedder{client: llm.New(server.URL, hc)}
	svc := NewService(conn, nil, nil, time.Now,
		WithTelemetryRecorder(&telemetry.Recorder{}),
		WithPageEmbedder("model-a", embedder),
	)
	if n, err := svc.DrainEmbeddingCatchUp(ctx); err != nil || n != 3 {
		t.Fatalf("first drain = %d, %v; want 3, nil", n, err)
	}
	first := calls[0].GroupID
	if !correlation.Valid(first) {
		t.Fatalf("first cycle group = %q, want valid root", first)
	}
	for _, call := range calls[:3] {
		if call.GroupID != first || call.Header != first || call.Name != "wiki.embed-page" {
			t.Fatalf("first cycle calls = %+v, want one payload/header root %q", calls[:3], first)
		}
	}
	svc.embedModel = "model-b"
	if n, err := svc.DrainEmbeddingCatchUp(ctx); err != nil || n != 3 {
		t.Fatalf("second drain = %d, %v; want 3, nil", n, err)
	}
	second := calls[3].GroupID
	if !correlation.Valid(second) || second == first {
		t.Fatalf("cycle roots = %q then %q, want distinct valid roots", first, second)
	}
	for _, call := range calls[3:] {
		if call.GroupID != second || call.Header != second || call.Name != "wiki.embed-page" {
			t.Fatalf("second cycle calls = %+v, want one payload/header root %q", calls[3:], second)
		}
	}
}

func TestEmbeddingCatchUpLabelsCacheEntryWithSubjectsScope(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	if _, err := NewScopeStore(conn).Create(ctx, "s1"); err != nil {
		t.Fatalf("Create(s1): %v", err)
	}
	if err := NewSubjectStore(conn).Save(ctx, "s1", Subject{ID: "subject-s1", Name: "S1", Type: "entity"}); err != nil {
		t.Fatalf("seed s1 subject: %v", err)
	}
	if err := NewPageStore(conn).Upsert(ctx, Page{ID: "subject-s1", SubjectID: "subject-s1", Title: "S1", Body: "Scoped body"}); err != nil {
		t.Fatalf("seed s1 page: %v", err)
	}
	cache := &recordingVectorCache{}
	svc := NewService(conn, nil, nil, time.Now,
		WithPageEmbedder("model-a", &recordingPageEmbedder{vectors: [][]float32{{1}}}),
		WithVectorCacheUpdater(cache.Upsert),
	)

	if n, err := svc.DrainEmbeddingCatchUp(ctx); err != nil || n != 1 {
		t.Fatalf("DrainEmbeddingCatchUp = %d, %v; want 1, nil", n, err)
	}
	if len(cache.entries) != 1 || cache.entries[0].scope != "s1" || cache.entries[0].subjectID != "subject-s1" {
		t.Fatalf("catch-up cache entries = %+v, want subject-s1 labeled s1", cache.entries)
	}
}

func TestEmbeddingCatchUpReapsOrphansAndPreservesLiveEntries(t *testing.T) {
	// R-R9Y8-A1UL
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	seedEmbeddingSubject(t, ctx, conn, "default", "live-subject")
	page := Page{ID: "live-subject", SubjectID: "live-subject", Title: "Live", Body: "Live body"}
	if err := NewPageStore(conn).Upsert(ctx, page); err != nil {
		t.Fatalf("Upsert live page: %v", err)
	}
	live := Embedding{
		SubjectID: "live-subject", Model: "model-a", Dims: 2, Vec: []float32{0.25, 0.75},
		ContentHash: pageFingerprint(page), UpdatedAt: 10,
	}
	if err := NewEmbeddingStore(conn).Upsert(ctx, live); err != nil {
		t.Fatalf("Upsert live embedding: %v", err)
	}
	seedOrphanEmbedding(t, ctx, conn, "orphan-subject")

	svc := NewService(conn, nil, nil, time.Now,
		WithPageEmbedder("model-a", &recordingPageEmbedder{vectors: [][]float32{{9, 9}}}),
	)
	if n, err := svc.DrainEmbeddingCatchUp(ctx); err != nil || n != 0 {
		t.Fatalf("DrainEmbeddingCatchUp = %d, %v; want 0, nil", n, err)
	}
	embeddings, err := NewEmbeddingStore(conn).LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll after sweep: %v", err)
	}
	if !reflect.DeepEqual(embeddings, []Embedding{live}) {
		t.Fatalf("embeddings after sweep = %#v, want untouched live embedding", embeddings)
	}
	entries, err := LoadVectorCacheEntries(ctx, conn)
	if err != nil {
		t.Fatalf("LoadVectorCacheEntries after sweep: %v", err)
	}
	want := []VectorCacheEntry{{Scope: "default", SubjectID: "live-subject", Title: "Live", Vec: []float32{0.25, 0.75}}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("hydrated entries = %#v, want %#v", entries, want)
	}
}

func TestEmbeddingCatchUpDeleteBetweenSelectionAndStoreLeavesNoOrphan(t *testing.T) {
	// R-RB64-NTLA
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if _, err := NewScopeStore(conn).Create(ctx, "doomed"); err != nil {
		t.Fatalf("Create doomed scope: %v", err)
	}
	seedEmbeddingSubject(t, ctx, conn, "doomed", "doomed-subject")
	page := Page{ID: "doomed-subject", SubjectID: "doomed-subject", Title: "Doomed", Body: "Doomed body"}
	if err := NewPageStore(conn).Upsert(ctx, page); err != nil {
		t.Fatalf("Upsert doomed page: %v", err)
	}

	embedder := &recordingPageEmbedder{
		vectors: [][]float32{{1, 0}},
		onEmbed: func(context.Context, []string, EmbedRole) error {
			return NewScopeStore(conn).Delete(ctx, "doomed")
		},
	}
	svc := NewService(conn, nil, nil, time.Now, WithPageEmbedder("model-a", embedder))
	if n, err := svc.DrainEmbeddingCatchUp(ctx); err != nil || n != 1 {
		t.Fatalf("DrainEmbeddingCatchUp = %d, %v; want 1, nil", n, err)
	}
	var rows int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM page_embeddings WHERE subject_id = ?`, page.SubjectID).Scan(&rows); err != nil {
		t.Fatalf("count raced embedding rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("raced embedding rows = %d, want 0", rows)
	}
	entries, err := LoadVectorCacheEntries(ctx, conn)
	if err != nil {
		t.Fatalf("LoadVectorCacheEntries after race: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("LoadVectorCacheEntries after race = %#v, want empty", entries)
	}
}

type sweepPromptsEmbedder struct{ client *llm.Client }

func (e sweepPromptsEmbedder) Embed(ctx context.Context, attr llm.Attribution, inputs []string, _ EmbedRole) (*EmbedResult, error) {
	vectors, err := e.client.Embed(ctx, llm.EmbedSite{Name: "wiki.embed-page", Model: "embed", Dims: 1}, attr, "document", inputs)
	if err != nil {
		return nil, err
	}
	return &EmbedResult{Vectors: vectors}, nil
}

func TestProcessNextKeepsCommittedPageDoneWhenAfterCommitEmbedFails(t *testing.T) {
	// R-6XNX-FNXO
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	embedder := &recordingPageEmbedder{
		onEmbed: func(context.Context, []string, EmbedRole) error {
			return errors.New("embed transport down")
		},
	}
	svc := NewService(
		conn,
		&recordingExtractor{batches: [][]extract.ExtractedSubject{{
			{
				Type:   "entity",
				Kind:   "company",
				Name:   "Acme Robotics",
				Claims: []string{"Acme Robotics opened a committed Tulsa lab."},
			},
		}}},
		&recordingCompiler{},
		sequenceTimes(
			time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 25, 14, 0, 1, 0, time.UTC),
			time.Date(2026, 6, 25, 14, 0, 2, 0, time.UTC),
		),
		WithPageEmbedder("embed-model", embedder),
	)
	svc.newID = sequenceIDs("job-1", "subject-1", "claim-1")

	if _, err := svc.Ingest(ctx, "default", "owner-id", "owner@example.com", "source", "Source", nil); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	processed, err := svc.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !processed {
		t.Fatal("ProcessNext processed = false, want true")
	}

	status, err := svc.JobStatus(ctx, "job-1")
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.Status != JobDone || status.Error != "" {
		t.Fatalf("status = %+v, want done with no job error after post-commit embed failure", status)
	}
	if len(status.Subjects) != 1 || status.Subjects[0] != "subject-1" {
		t.Fatalf("subjects = %#v, want committed subject-1", status.Subjects)
	}
	claims, err := svc.ClaimsBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("ClaimsBySubject: %v", err)
	}
	if len(claims) != 1 || claims[0].Body != "Acme Robotics opened a committed Tulsa lab." {
		t.Fatalf("claims = %+v, want committed ingest claim", claims)
	}
	page, err := svc.PageBySubject(ctx, "subject-1")
	if err != nil {
		t.Fatalf("PageBySubject: %v", err)
	}
	if page.Title != "Acme Robotics" || page.Body != "Acme Robotics opened a committed Tulsa lab." {
		t.Fatalf("page = %+v, want committed compiled page", page)
	}
	embeddings, err := NewEmbeddingStore(conn).LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(embeddings) != 0 {
		t.Fatalf("embeddings = %+v, want no stored vector so catch-up can select the page", embeddings)
	}
}

type recordingPageEmbedder struct {
	vectors [][]float32
	inputs  [][]string
	roles   []EmbedRole
	attrs   []llm.Attribution
	onEmbed func(context.Context, []string, EmbedRole) error
}

func (e *recordingPageEmbedder) Embed(ctx context.Context, attr llm.Attribution, inputs []string, role EmbedRole) (*EmbedResult, error) {
	e.attrs = append(e.attrs, attr)
	e.inputs = append(e.inputs, append([]string(nil), inputs...))
	e.roles = append(e.roles, role)
	if e.onEmbed != nil {
		if err := e.onEmbed(ctx, inputs, role); err != nil {
			return nil, err
		}
	}
	vec := []float32(nil)
	if len(e.vectors) > 0 {
		vec = append([]float32(nil), e.vectors[0]...)
		e.vectors = e.vectors[1:]
	}
	return &EmbedResult{Vectors: [][]float32{vec}}, nil
}

func migratedEmbeddingConns(t *testing.T, ctx context.Context) (Conns, func()) {
	t.Helper()

	path := t.TempDir() + "/wiki.db"
	write, err := appdb.Open(path)
	if err != nil {
		t.Fatalf("Open writer: %v", err)
	}
	migs, err := appdb.LoadMigrations(wikidb.FS, "migrations")
	if err != nil {
		write.Close()
		t.Fatalf("LoadMigrations: %v", err)
	}
	if err := appdb.Migrate(ctx, write, migs); err != nil {
		write.Close()
		t.Fatalf("Migrate: %v", err)
	}
	read, err := wikidb.OpenRead(path)
	if err != nil {
		write.Close()
		t.Fatalf("OpenRead: %v", err)
	}
	return Conns{Read: read, Write: write}, func() {
		read.Close()
		write.Close()
	}
}

type recordingVectorCache struct {
	entries []recordingVectorEntry
}

type recordingVectorEntry struct {
	scope     string
	subjectID string
	title     string
	vec       []float32
}

func (c *recordingVectorCache) Upsert(scope, subjectID, title string, vec []float32) {
	c.entries = append(c.entries, recordingVectorEntry{
		scope:     scope,
		subjectID: subjectID,
		title:     title,
		vec:       append([]float32(nil), vec...),
	})
}

var _ PageEmbedder = (*recordingPageEmbedder)(nil)
