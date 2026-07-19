package calls

import (
	"context"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	appkitdb "appkit/db"

	promptsdb "prompts/internal/db"
)

func TestInsertAndGetRoundTripEveryField(t *testing.T) {
	// R-5J1W-8BCM
	store := testStore(t)
	request, response := `{"prompt":"hello"}`, `{"text":"world"}`
	row := Row{
		ID: "01CALLROUNDTRIP00000000000", Class: ClassCompletion,
		Origin: "user:Owner.Name@example.com", Name: "wiki.compile", GroupID: "job-42",
		Attempt: 3, OwnerEmail: "Owner.Name@example.com", Provider: "anthropic", Model: "claude-sonnet",
		InputTokens: 17, OutputTokens: 11, TotalTokens: 28,
		UsageJSON: `{"input_uncached":17,"output":11,"total":28}`, CostUSD: 0.0125,
		Error: "provider warning", RequestBody: &request, ResponseBody: &response,
		StartedAt: time.Date(2026, 7, 18, 10, 11, 12, 123000000, time.FixedZone("offset", -5*60*60)),
		EndedAt:   time.Date(2026, 7, 18, 15, 11, 14, 456000000, time.UTC),
	}
	if err := store.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := store.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	row.StartedAt = row.StartedAt.UTC()
	row.EndedAt = row.EndedAt.UTC()
	if !reflect.DeepEqual(got, row) {
		t.Fatalf("Get row mismatch\n got: %#v\nwant: %#v", got, row)
	}
}

func TestPruneBodiesPreservesMetrics(t *testing.T) {
	// R-5K9S-M33B
	store := testStore(t)
	ended := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	row := seededRow("old", "wiki.compile", ended)
	if err := store.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	before, err := store.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get before prune: %v", err)
	}
	count, err := store.PruneBodies(context.Background(), ended.Add(time.Hour))
	if err != nil {
		t.Fatalf("PruneBodies: %v", err)
	}
	if count != 1 {
		t.Fatalf("PruneBodies count = %d, want 1", count)
	}
	after, err := store.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get after prune: %v", err)
	}
	if after.RequestBody != nil || after.ResponseBody != nil {
		t.Fatalf("bodies after prune = (%v, %v), want nil", after.RequestBody, after.ResponseBody)
	}
	before.RequestBody, before.ResponseBody = nil, nil
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("prune changed metrics\n got: %#v\nwant: %#v", after, before)
	}
}

func TestPruneBodiesLeavesNewerRowsIntact(t *testing.T) {
	// R-5LHO-ZUU0
	store := testStore(t)
	ended := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	row := seededRow("new", "wiki.compile", ended)
	if err := store.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	count, err := store.PruneBodies(context.Background(), ended.Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneBodies: %v", err)
	}
	if count != 0 {
		t.Fatalf("PruneBodies count = %d, want 0", count)
	}
	got, err := store.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RequestBody == nil || *got.RequestBody != *row.RequestBody || got.ResponseBody == nil || *got.ResponseBody != *row.ResponseBody {
		t.Fatalf("newer row bodies = (%v, %v), want intact", got.RequestBody, got.ResponseBody)
	}
}

func TestListFiltersAndPaginatesDeterministically(t *testing.T) {
	// R-5MPL-DMKP
	store := testStore(t)
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	rows := []Row{
		seededRow("a", "wiki.compile", base),
		seededRow("b", "wiki.compile", base.Add(time.Hour)),
		seededRow("c", "wiki.compile", base.Add(2*time.Hour)),
		seededRow("d", "crm.summarize", base.Add(3*time.Hour)),
		seededRow("e", "wiki.compile", base.Add(4*time.Hour)),
	}
	for i := range rows {
		rows[i].StartedAt = rows[i].EndedAt
		rows[i].GroupID = "target"
		rows[i].Origin = "service:wiki"
		rows[i].Class = ClassCompletion
	}
	rows[1].Error = "failed"
	rows[2].Error = "failed"
	rows[4].Error = "failed"
	rows[3].GroupID = "other"
	rows[3].Origin = "trigger:cron"
	rows[4].Class = ClassEmbedding
	for _, row := range rows {
		if err := store.Insert(context.Background(), row); err != nil {
			t.Fatalf("Insert %s: %v", row.ID, err)
		}
	}

	checks := []struct {
		name string
		f    Filter
		want int
	}{
		{"class", Filter{Class: ClassEmbedding}, 1},
		{"origin", Filter{Origin: "trigger:cron"}, 1},
		{"name", Filter{Name: "crm.summarize"}, 1},
		{"group", Filter{GroupID: "other"}, 1},
		{"errors", Filter{ErrorsOnly: true}, 3},
		{"window", Filter{Since: base.Add(time.Hour), Until: base.Add(3 * time.Hour)}, 3},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			got, err := store.List(context.Background(), check.f)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != check.want {
				t.Fatalf("List returned %d rows, want %d: %#v", len(got), check.want, got)
			}
		})
	}

	match := Filter{Origin: "service:wiki", Name: "wiki.compile", GroupID: "target", ErrorsOnly: true}
	got, err := store.List(context.Background(), match)
	if err != nil {
		t.Fatalf("combined List: %v", err)
	}
	if ids := rowIDs(got); !reflect.DeepEqual(ids, []string{"e", "c", "b"}) {
		t.Fatalf("combined List ids = %v, want [e c b]", ids)
	}
	first, err := store.List(context.Background(), Filter{Origin: match.Origin, Name: match.Name, GroupID: match.GroupID, ErrorsOnly: true, Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	second, err := store.List(context.Background(), Filter{Origin: match.Origin, Name: match.Name, GroupID: match.GroupID, ErrorsOnly: true, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if ids := append(rowIDs(first), rowIDs(second)...); !reflect.DeepEqual(ids, []string{"e", "c", "b"}) {
		t.Fatalf("paginated ids = %v, want [e c b]", ids)
	}
}

func TestAggregateByNameSumsUsageCostAndAppliesWindow(t *testing.T) {
	// R-5NXH-REBE
	store := testStore(t)
	base := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		seededRow("a", "wiki.compile", base),
		seededRow("b", "wiki.compile", base.Add(time.Hour)),
		seededRow("c", "crm.summarize", base.Add(2*time.Hour)),
		seededRow("d", "wiki.compile", base.Add(48*time.Hour)),
	}
	for i := range rows {
		rows[i].StartedAt = rows[i].EndedAt
		rows[i].InputTokens = int64(i + 1)
		rows[i].OutputTokens = int64((i + 1) * 10)
		rows[i].TotalTokens = rows[i].InputTokens + rows[i].OutputTokens
		rows[i].CostUSD = float64(i+1) * 0.25
	}
	rows[1].Error = "timeout"
	for _, row := range rows {
		if err := store.Insert(context.Background(), row); err != nil {
			t.Fatalf("Insert %s: %v", row.ID, err)
		}
	}
	got, err := store.Aggregate(context.Background(), GroupByName, Filter{})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	byName := bucketMap(got)
	assertBucket(t, byName["wiki.compile"], Bucket{Key: "wiki.compile", Calls: 3, InputTokens: 7, OutputTokens: 70, TotalTokens: 77, CostUSD: 1.75, Errors: 1})
	assertBucket(t, byName["crm.summarize"], Bucket{Key: "crm.summarize", Calls: 1, InputTokens: 3, OutputTokens: 30, TotalTokens: 33, CostUSD: 0.75})

	windowed, err := store.Aggregate(context.Background(), GroupByName, Filter{Since: base, Until: base.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("windowed Aggregate: %v", err)
	}
	byName = bucketMap(windowed)
	assertBucket(t, byName["wiki.compile"], Bucket{Key: "wiki.compile", Calls: 2, InputTokens: 3, OutputTokens: 30, TotalTokens: 33, CostUSD: 0.75, Errors: 1})
	if _, exists := byName["crm.summarize"]; !exists {
		t.Fatal("window unexpectedly excluded in-range crm.summarize bucket")
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "calls.db"))
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}
	return NewStore(conn)
}

func seededRow(id, name string, ended time.Time) Row {
	request, response := "request-"+id, "response-"+id
	return Row{
		ID: id, Class: ClassCompletion, Origin: "service:wiki", Name: name,
		GroupID: "group-1", Attempt: 1, Provider: "anthropic", Model: "claude-sonnet",
		InputTokens: 10, OutputTokens: 5, TotalTokens: 15, UsageJSON: `{}`, CostUSD: 0.5,
		RequestBody: &request, ResponseBody: &response, StartedAt: ended.Add(-time.Minute), EndedAt: ended,
	}
}

func rowIDs(rows []Row) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

func bucketMap(buckets []Bucket) map[string]Bucket {
	result := make(map[string]Bucket, len(buckets))
	for _, bucket := range buckets {
		result[bucket.Key] = bucket
	}
	return result
}

func assertBucket(t *testing.T, got, want Bucket) {
	t.Helper()
	if got.Key != want.Key || got.Calls != want.Calls || got.InputTokens != want.InputTokens ||
		got.OutputTokens != want.OutputTokens || got.TotalTokens != want.TotalTokens || got.Errors != want.Errors ||
		math.Abs(got.CostUSD-want.CostUSD) > 1e-9 {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
}
