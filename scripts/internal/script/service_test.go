package script

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"eventplane/correlation"
)

// fakeRunner records Spawn/Cancel calls instead of execing python.
type fakeRunner struct {
	mu       sync.Mutex
	spawns   []spawnCall
	cancels  []string
	cancelOK bool // value Cancel returns
}

type spawnCall struct {
	run   Run
	input []byte
}

type planeCall struct {
	verb, key, other, message, clientID string
	files                               map[string]string
}

type fakePlane struct {
	calls           []planeCall
	files           map[string]string
	createErr       error
	commitErr       error
	createErrForKey map[string]error
	commitErrForKey map[string]error
	renameErr       error
	deleteErr       error
	readErr         error
	headErr         error
	headSHA         string
}

func newFakePlane() *fakePlane { return &fakePlane{files: make(map[string]string), headSHA: "sha"} }

func (f *fakePlane) Create(_ context.Context, key string, _ Owner, clientID string) error {
	f.calls = append(f.calls, planeCall{verb: "create", key: key, clientID: clientID})
	if err := f.createErrForKey[key]; err != nil {
		return err
	}
	return f.createErr
}

func (f *fakePlane) Commit(_ context.Context, key string, files map[string]string, message, clientID string) (string, error) {
	copyFiles := make(map[string]string, len(files))
	for name, body := range files {
		copyFiles[name] = body
		f.files[key+":"+name] = body
	}
	f.calls = append(f.calls, planeCall{verb: "commit", key: key, files: copyFiles, message: message, clientID: clientID})
	if err := f.commitErrForKey[key]; err != nil {
		return "", err
	}
	return "sha", f.commitErr
}

func (f *fakePlane) Head(context.Context, string, string) (string, error) {
	return f.headSHA, f.headErr
}

func (f *fakePlane) ReadFile(_ context.Context, key, ref, path string) ([]byte, error) {
	f.calls = append(f.calls, planeCall{verb: "read", key: key, other: ref + ":" + path})
	return []byte(f.files[key+":"+path]), f.readErr
}

func (f *fakePlane) Rename(_ context.Context, oldKey, newKey string, _ Owner, clientID string) error {
	f.calls = append(f.calls, planeCall{verb: "rename", key: oldKey, other: newKey, clientID: clientID})
	for suffix, body := range f.files {
		if strings.HasPrefix(suffix, oldKey+":") {
			delete(f.files, suffix)
			f.files[newKey+strings.TrimPrefix(suffix, oldKey)] = body
		}
	}
	return f.renameErr
}

func (f *fakePlane) Delete(_ context.Context, key string, _ Owner, clientID string) error {
	f.calls = append(f.calls, planeCall{verb: "delete", key: key, clientID: clientID})
	return f.deleteErr
}

func (f *fakePlane) RunToken(context.Context, string, time.Duration) (string, string, error) {
	return "token", "clone", nil
}

func (f *fakePlane) resetCalls() { f.calls = nil }

func (f *fakeRunner) Spawn(run Run, input []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawns = append(f.spawns, spawnCall{run: run, input: append([]byte(nil), input...)})
}

func (f *fakeRunner) Cancel(runID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, runID)
	return f.cancelOK
}

func (f *fakeRunner) lastSpawn(t *testing.T) spawnCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.spawns) == 0 {
		t.Fatalf("expected a Spawn, got none")
	}
	return f.spawns[len(f.spawns)-1]
}

// newTestService builds a Service over a real temp-db store and a fake runner.
// runsDir is a temp dir that stands in for <dataDir>/runs.
func newTestService(t *testing.T) (*Service, *Store, *fakeRunner, string) {
	t.Helper()
	store := newTestStore(t)
	fr := &fakeRunner{}
	runsDir := t.TempDir()
	svc := NewService(store, runsDir, fr)
	svc.Plane = newFakePlane()
	return svc, store, fr, runsDir
}

// R-2W7Z-MXQR
func TestCreateStoresSeedMetadataWithoutStoredBody(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	wantBody := "print('café')\x00\n"
	sc, err := svc.Create(context.Background(), ownerA, CreateInput{Name: "First Job", Body: wantBody})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetScript(context.Background(), ownerA, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.NameKey == "" || stored.RepoSeededAt == "" || stored.Body != "" {
		t.Fatalf("stored script = %+v, want seed metadata and no stored body", stored)
	}
	plane := svc.Plane.(*fakePlane)
	if got := plane.files[stored.NameKey+":main.py"]; got != wantBody {
		t.Fatalf("plane body = %q, want %q", got, wantBody)
	}
}

// R-2XFW-0PHG
func TestCreatePlaneFailureLeavesNoUnseededRow(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	plane := svc.Plane.(*fakePlane)
	plane.commitErrForKey = map[string]error{"retry-repo": errors.New("repos unavailable")}
	if _, err := svc.Create(context.Background(), ownerA, CreateInput{Name: "Retry Repo", Body: "print('retry')"}); err == nil {
		t.Fatal("Create succeeded despite failed plane commit")
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM scripts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("scripts rows = %d, want compensation to leave none", count)
	}
}

func TestServiceCreate(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()

	// Validation: empty name / body.
	if _, err := svc.Create(ctx, ownerA, CreateInput{Name: "", Body: "print(1)"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: want ErrValidation, got %v", err)
	}
	if _, err := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "  "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty body: want ErrValidation, got %v", err)
	}

	// Success: id + timestamps set, config defaulted to python3.
	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "nightly", Body: "print('hi')"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.ID == "" || sc.CreatedAt == "" || sc.UpdatedAt == "" {
		t.Fatalf("Create: missing id/timestamps: %+v", sc)
	}
	if sc.Config.Interpreter != "python3" {
		t.Fatalf("Create: want default interpreter python3, got %q", sc.Config.Interpreter)
	}
	if sc.OwnerEmail != ownerA {
		t.Fatalf("Create: owner = %q", sc.OwnerEmail)
	}
}

// R-2BHP-4U4Y
func TestCreateSeedsVersionPlaneAndStampsRow(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	plane := svc.Plane.(*fakePlane)

	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "Nightly Export", Body: "print('hi')"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(plane.calls) != 2 || plane.calls[0].verb != "create" || plane.calls[1].verb != "commit" {
		t.Fatalf("plane calls = %+v, want Create then Commit", plane.calls)
	}
	wantKey := "nightly-export"
	wantClient := "scripts:" + sc.ID
	if plane.calls[0].key != wantKey || plane.calls[1].key != wantKey {
		t.Fatalf("plane keys = %q, %q; want %q", plane.calls[0].key, plane.calls[1].key, wantKey)
	}
	commit := plane.calls[1]
	if len(commit.files) != 1 || commit.files["main.py"] != "print('hi')" || commit.message != "create Nightly Export" || commit.clientID != wantClient || plane.calls[0].clientID != wantClient {
		t.Fatalf("create attribution/commit = %+v, want exact main.py body and %q", plane.calls, wantClient)
	}
	stored, err := store.GetScript(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if stored.NameKey != "nightly-export" || stored.RepoSeededAt == "" {
		t.Fatalf("stored version metadata = name_key %q seeded_at %q", stored.NameKey, stored.RepoSeededAt)
	}
}

// R-2CPL-ILVN
func TestCreatePlaneFailureCompensatesInsertedRow(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	plane := svc.Plane.(*fakePlane)
	plane.createErr = ErrSourceUnavailable

	_, err := svc.Create(context.Background(), ownerA, CreateInput{Name: "orphan", Body: "print(1)"})
	if !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("Create error = %v, want ErrSourceUnavailable", err)
	}
	var rows int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM scripts`).Scan(&rows); err != nil {
		t.Fatalf("count scripts: %v", err)
	}
	if rows != 0 {
		t.Fatalf("scripts rows = %d, want 0 after compensating delete", rows)
	}
}

func TestServiceUpdatePartial(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "orig", Body: "print(1)"})

	newName := "renamed"
	got, err := svc.Update(ctx, ownerA, sc.ID, UpdateInput{Name: &newName})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name != "renamed" {
		t.Fatalf("Update name = %q", got.Name)
	}
	if got.Body != "print(1)" {
		t.Fatalf("Update should leave body unchanged, got %q", got.Body)
	}
	if got.UpdatedAt == sc.UpdatedAt {
		// Both are RFC3339Nano; with the same monotonic clock they can collide
		// only if no time passed. Tolerate equality but require non-empty.
		if got.UpdatedAt == "" {
			t.Fatalf("Update: empty UpdatedAt")
		}
	}

	// Owner mismatch → ErrNotFound.
	if _, err := svc.Update(ctx, ownerB, sc.ID, UpdateInput{Name: &newName}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update: want ErrNotFound, got %v", err)
	}
}

// R-2DXH-WDMC
func TestUpdateCommitsBodyAndRenamesOnlyWhenRequired(t *testing.T) {
	ctx := context.Background()

	t.Run("body only", func(t *testing.T) {
		svc, _, _, _ := newTestService(t)
		sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "Nightly Export", Body: "old"})
		if err != nil {
			t.Fatal(err)
		}
		plane := svc.Plane.(*fakePlane)
		plane.resetCalls()
		body := "new"
		if _, err := svc.Update(ctx, ownerA, sc.ID, UpdateInput{Body: &body}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if len(plane.calls) != 1 || plane.calls[0].verb != "commit" || plane.calls[0].files["main.py"] != body {
			t.Fatalf("plane calls = %+v, want one body Commit", plane.calls)
		}
	})

	t.Run("changed slug", func(t *testing.T) {
		svc, store, _, _ := newTestService(t)
		sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "Nightly Export", Body: "old"})
		if err != nil {
			t.Fatal(err)
		}
		plane := svc.Plane.(*fakePlane)
		plane.resetCalls()
		name := "Weekly Export"
		if _, err := svc.Update(ctx, ownerA, sc.ID, UpdateInput{Name: &name}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if len(plane.calls) != 2 || plane.calls[0].verb != "read" || plane.calls[1].verb != "rename" || plane.calls[1].key != "nightly-export" || plane.calls[1].other != "weekly-export" {
			t.Fatalf("plane calls = %+v, want body Read then Rename", plane.calls)
		}
		stored, _ := store.GetScript(ctx, ownerA, sc.ID)
		if stored.NameKey != "weekly-export" {
			t.Fatalf("stored name_key = %q", stored.NameKey)
		}
	})

	t.Run("same slug", func(t *testing.T) {
		svc, _, _, _ := newTestService(t)
		sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "Nightly Export", Body: "old"})
		if err != nil {
			t.Fatal(err)
		}
		plane := svc.Plane.(*fakePlane)
		plane.resetCalls()
		name := "nightly export"
		if _, err := svc.Update(ctx, ownerA, sc.ID, UpdateInput{Name: &name}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if len(plane.calls) != 1 || plane.calls[0].verb != "read" {
			t.Fatalf("plane calls = %+v, want one body Read", plane.calls)
		}
	})
}

func TestServiceDeleteTombstone(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})
	// A run exists; delete must not remove it (tombstone).
	seedRun(t, store, sc.ID, RunSucceeded)

	if err := svc.Delete(ctx, ownerA, sc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, ownerA, sc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete Get: want ErrNotFound, got %v", err)
	}
	// Runs survive as history.
	runs, err := store.ListRuns(ctx, ownerA, "", "", "")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	// ListRuns joins scripts; with the script tombstoned the run dangles, so the
	// owner-scoped ListRuns no longer sees it — assert the run row itself still
	// exists via a direct count.
	_ = runs
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE script_id = ?`, sc.ID).Scan(&n); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if n != 1 {
		t.Fatalf("tombstone: run should survive, count = %d", n)
	}

	// Foreign / missing delete → ErrNotFound.
	if err := svc.Delete(ctx, ownerA, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
}

// R-2GDA-NX3Q
func TestDeleteArchivesRepositoryWithoutLettingPlaneFailurePinRow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deleteErr error
	}{
		{name: "archive succeeds"},
		{name: "archive unavailable", deleteErr: ErrSourceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _, _ := newTestService(t)
			ctx := context.Background()
			sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "Disposable", Body: "print(1)"})
			if err != nil {
				t.Fatal(err)
			}
			plane := svc.Plane.(*fakePlane)
			plane.resetCalls()
			plane.deleteErr = tc.deleteErr
			if err := svc.Delete(ctx, ownerA, sc.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if len(plane.calls) != 1 || plane.calls[0].verb != "delete" || plane.calls[0].key != "disposable" {
				t.Fatalf("plane calls = %+v, want one Delete", plane.calls)
			}
			if _, err := store.GetScript(ctx, ownerA, sc.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("row after Delete = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestServiceListGetDerived(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})
	seedRun(t, store, sc.ID, RunRunning)
	seedRun(t, store, sc.ID, RunSucceeded)

	detail, err := svc.Get(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.RunningCount != 1 {
		t.Fatalf("RunningCount = %d, want 1", detail.RunningCount)
	}
	if detail.LastRun == nil {
		t.Fatalf("LastRun nil")
	}

	list, err := svc.List(ctx, ownerA)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
	if list[0].RunningCount != 1 || list[0].LastRun == nil {
		t.Fatalf("List detail not attached: %+v", list[0])
	}

	// Owner B sees nothing / ErrNotFound.
	if _, err := svc.Get(ctx, ownerB, sc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign Get: want ErrNotFound, got %v", err)
	}
	if l, _ := svc.List(ctx, ownerB); len(l) != 0 {
		t.Fatalf("foreign List len = %d, want 0", len(l))
	}
}

// R-2HL7-1OUF
func TestGetReadsMainTipFromPlaneInsteadOfStoredBody(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "Plane Read", Body: "stored body"})
	if err != nil {
		t.Fatal(err)
	}
	plane := svc.Plane.(*fakePlane)
	plane.files[sc.NameKey+":main.py"] = "plane body"
	plane.resetCalls()

	detail, err := svc.Get(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Body != "plane body" {
		t.Fatalf("Get body = %q, want plane body", detail.Body)
	}
	stored, _ := store.GetScript(ctx, ownerA, sc.ID)
	if stored.Body != "" {
		t.Fatalf("stored-row body = %q, want empty after body retirement", stored.Body)
	}
	if len(plane.calls) != 1 || plane.calls[0].verb != "read" || plane.calls[0].key != sc.NameKey || plane.calls[0].other != "main:main.py" {
		t.Fatalf("plane calls = %+v, want ReadFile(key, main, main.py)", plane.calls)
	}
}

// R-2IT3-FGL4
func TestListDoesNotReadPlaneAndOmitsBodies(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()
	for _, name := range []string{"One", "Two", "Three"} {
		if _, err := svc.CreateForOwner(ctx, ownerA, "owner@example.test", CreateInput{Name: name, Body: "print(1)"}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	plane := svc.Plane.(*fakePlane)
	plane.resetCalls()

	list, err := svc.List(ctx, ownerA)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(plane.calls) != 0 {
		t.Fatalf("List made plane calls: %+v", plane.calls)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	for _, detail := range list {
		if detail.Body != "" {
			t.Errorf("%s Body = %q, want empty", detail.ID, detail.Body)
		}
		if detail.ID == "" || detail.Name == "" || detail.OwnerID != ownerA || detail.OwnerEmail != "owner@example.test" || detail.CreatedAt == "" || detail.UpdatedAt == "" {
			t.Errorf("List omitted persisted fields: %+v", detail.Script)
		}
	}
}

func TestServiceRunManual(t *testing.T) {
	svc, store, fr, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})

	run, err := svc.Run(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != RunRunning {
		t.Fatalf("Run status = %q", run.Status)
	}
	if run.TriggerSource != "" {
		t.Fatalf("manual run should have empty trigger, got %q", run.TriggerSource)
	}
	// A running row was inserted.
	got, err := store.GetRun(ctx, ownerA, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != RunRunning {
		t.Fatalf("inserted run status = %q", got.Status)
	}
	if run.RepoSha != "sha" || got.RepoSha != "sha" {
		t.Fatalf("spawned/persisted repo_sha = %q/%q, want resolved sha", run.RepoSha, got.RepoSha)
	}
	// Spawn called once with "{}".
	if len(fr.spawns) != 1 {
		t.Fatalf("Spawn called %d times, want 1", len(fr.spawns))
	}
	sp := fr.lastSpawn(t)
	if string(sp.input) != "{}" {
		t.Fatalf("manual Spawn input = %q, want {}", sp.input)
	}

	// Owner mismatch → ErrNotFound, no spawn.
	if _, err := svc.Run(ctx, ownerB, sc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign Run: want ErrNotFound, got %v", err)
	}
	if len(fr.spawns) != 1 {
		t.Fatalf("foreign Run should not Spawn; count = %d", len(fr.spawns))
	}
}

func TestHeadFailureCreatesNoManualOrEventRun(t *testing.T) {
	// R-2SKA-HMIO
	svc, store, fr, _ := newTestService(t)
	ctx := context.Background()
	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "head-failure", Body: "print(1)"})
	if err != nil {
		t.Fatal(err)
	}
	plane := svc.Plane.(*fakePlane)
	plane.headErr = ErrSourceUnavailable
	if _, err := svc.Run(ctx, ownerA, sc.ID); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("manual Run error = %v, want ErrSourceUnavailable", err)
	}
	if err := svc.RunForEvent(ctx, sc.ID, "crm", "contact.created", "/x", "evt", []byte(`{}`)); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("RunForEvent error = %v, want ErrSourceUnavailable", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE script_id = ?`, sc.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || len(fr.spawns) != 0 {
		t.Fatalf("head failures created %d rows and %d spawns, want none", count, len(fr.spawns))
	}
}

func TestServiceRunForEvent(t *testing.T) {
	svc, store, fr, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})

	payload := []byte(`{"id":"evt1"}`)
	if err := svc.RunForEvent(ctx, sc.ID, "crm", "contact.created", "/a", "evt1", payload); err != nil {
		t.Fatalf("RunForEvent: %v", err)
	}
	sp := fr.lastSpawn(t)
	if string(sp.input) != string(payload) {
		t.Fatalf("RunForEvent Spawn input = %q, want %q", sp.input, payload)
	}
	if sp.run.TriggerSource != "crm" || sp.run.TriggerKind != "contact.created" || sp.run.TriggerSubject != "/a" || sp.run.TriggerEventID != "evt1" {
		t.Fatalf("RunForEvent trigger fields = %+v", sp.run)
	}
	// Row persisted with trigger fields.
	got, err := store.GetRun(ctx, ownerA, sp.run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.TriggerSource != "crm" {
		t.Fatalf("persisted trigger source = %q", got.TriggerSource)
	}

	// Missing script → no-op (nil), no spawn.
	before := len(fr.spawns)
	if err := svc.RunForEvent(ctx, "does-not-exist", "crm", "contact.created", "/a", "evt2", payload); err != nil {
		t.Fatalf("RunForEvent missing script: want nil, got %v", err)
	}
	if len(fr.spawns) != before {
		t.Fatalf("RunForEvent missing script should not Spawn")
	}
}

func TestRunUsesItsIDAsRootCorrelation(t *testing.T) {
	// R-4Q42-3TE2
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "root", Body: "print(1)"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT correlation_id FROM runs WHERE id = ?`, run.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" || stored != run.ID || run.CorrelationID != run.ID {
		t.Fatalf("run id=%q returned correlation=%q stored correlation=%q, want all equal and non-empty", run.ID, run.CorrelationID, stored)
	}
}

func TestRunInheritsContextCorrelation(t *testing.T) {
	// R-4RBY-HL4R
	svc, store, _, _ := newTestService(t)
	const inbound = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ctx := correlation.WithContext(context.Background(), inbound)
	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "continued", Body: "print(1)"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT correlation_id FROM runs WHERE id = ?`, run.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != inbound || run.CorrelationID != inbound || run.ID == inbound {
		t.Fatalf("run id=%q returned correlation=%q stored correlation=%q, want inherited %q distinct from run id", run.ID, run.CorrelationID, stored, inbound)
	}
}

func TestRootStarterRunsOnlyForNewRunChain(t *testing.T) {
	// R-4ZV9-5ZBM
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	sc, err := svc.Create(ctx, ownerA, CreateInput{Name: "rooted", Body: "print(1)"})
	if err != nil {
		t.Fatal(err)
	}
	type rootCall struct{ rootID, op string }
	var calls []rootCall
	svc.RootStarter = func(ctx context.Context, rootID, op string) context.Context {
		calls = append(calls, rootCall{rootID: rootID, op: op})
		return correlation.WithContext(ctx, rootID)
	}

	rooted, err := svc.Run(ctx, ownerA, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].rootID != rooted.ID || calls[0].op != "run:"+rooted.ID {
		t.Fatalf("root calls = %+v, want one call for run %s", calls, rooted.ID)
	}
	var rootedCorrelation string
	if err := store.db.QueryRow(`SELECT correlation_id FROM runs WHERE id = ?`, rooted.ID).Scan(&rootedCorrelation); err != nil {
		t.Fatal(err)
	}
	if rootedCorrelation != rooted.ID {
		t.Fatalf("rooted correlation = %q, want run id %q", rootedCorrelation, rooted.ID)
	}

	const inheritedID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	inheritedCtx := correlation.WithContext(ctx, inheritedID)
	inherited, err := svc.Run(inheritedCtx, ownerA, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("inherited run added a root call: %+v", calls)
	}
	var inheritedCorrelation string
	if err := store.db.QueryRow(`SELECT correlation_id FROM runs WHERE id = ?`, inherited.ID).Scan(&inheritedCorrelation); err != nil {
		t.Fatal(err)
	}
	if inheritedCorrelation != inheritedID {
		t.Fatalf("inherited correlation = %q, want %q", inheritedCorrelation, inheritedID)
	}
}

func TestServiceRunGetElapsed(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})
	r := Run{
		ID:         "run-elapsed",
		ScriptID:   sc.ID,
		Status:     RunSucceeded,
		StartedAt:  "2020-01-01T00:00:00Z",
		EndedAt:    "2020-01-01T00:00:05Z",
		StdoutPath: "runs/x/stdout.log",
		StderrPath: "runs/x/stderr.log",
	}
	if err := store.InsertRun(ctx, r); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	got, err := svc.RunGet(ctx, ownerA, r.ID)
	if err != nil {
		t.Fatalf("RunGet: %v", err)
	}
	if got.ElapsedSecs != 5 {
		t.Fatalf("ElapsedSecs = %d, want 5", got.ElapsedSecs)
	}

	if _, err := svc.RunGet(ctx, ownerB, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RunGet: want ErrNotFound, got %v", err)
	}
}

func TestServiceRunCancel(t *testing.T) {
	svc, store, fr, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})
	run := seedRun(t, store, sc.ID, RunRunning)

	// In flight: Cancel returns true.
	fr.cancelOK = true
	if err := svc.RunCancel(ctx, ownerA, run.ID); err != nil {
		t.Fatalf("RunCancel: %v", err)
	}
	if len(fr.cancels) != 1 || fr.cancels[0] != run.ID {
		t.Fatalf("runner.Cancel not called with run id: %v", fr.cancels)
	}

	// Not in flight: ErrValidation.
	fr.cancelOK = false
	run2 := seedRun(t, store, sc.ID, RunRunning)
	if err := svc.RunCancel(ctx, ownerA, run2.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("RunCancel not-in-flight: want ErrValidation, got %v", err)
	}

	// Already terminal: ErrValidation, no Cancel call.
	term := seedRun(t, store, sc.ID, RunSucceeded)
	before := len(fr.cancels)
	if err := svc.RunCancel(ctx, ownerA, term.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("RunCancel terminal: want ErrValidation, got %v", err)
	}
	if len(fr.cancels) != before {
		t.Fatalf("RunCancel terminal should not call runner.Cancel")
	}

	// Foreign owner → ErrNotFound.
	if err := svc.RunCancel(ctx, ownerB, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RunCancel: want ErrNotFound, got %v", err)
	}
}

func TestServiceTriggers(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})

	// Unknown source → ErrValidation.
	if _, err := svc.SetTrigger(ctx, ownerA, sc.ID, "bogus:x.*"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown source: want ErrValidation, got %v", err)
	}
	// Unsatisfiable filter → ErrValidation.
	if _, err := svc.SetTrigger(ctx, ownerA, sc.ID, "crm:transaction.recorded"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad filter: want ErrValidation, got %v", err)
	}

	// Success.
	tr, err := svc.SetTrigger(ctx, ownerA, sc.ID, "crm:contact.*")
	if err != nil {
		t.Fatalf("SetTrigger: %v", err)
	}
	if tr.ScriptID != sc.ID || tr.Source != "crm" || tr.Filter != "crm:contact.*" || tr.CreatedAt == "" {
		t.Fatalf("SetTrigger returned %+v", tr)
	}

	// ScriptsForEvent finds it.
	ids, err := svc.ScriptsForEvent(ctx, "crm", "crm:contact.created")
	if err != nil {
		t.Fatalf("ScriptsForEvent: %v", err)
	}
	if len(ids) != 1 || ids[0] != sc.ID {
		t.Fatalf("ScriptsForEvent = %v, want [%s]", ids, sc.ID)
	}

	// Foreign SetTrigger → ErrNotFound.
	if _, err := svc.SetTrigger(ctx, ownerB, sc.ID, "crm:contact.*"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign SetTrigger: want ErrNotFound, got %v", err)
	}

	// ClearTrigger.
	if err := svc.ClearTrigger(ctx, ownerA, sc.ID, "crm:contact.*"); err != nil {
		t.Fatalf("ClearTrigger: %v", err)
	}
	ids2, _ := svc.ScriptsForEvent(ctx, "crm", "crm:contact.created")
	if len(ids2) != 0 {
		t.Fatalf("after ClearTrigger ScriptsForEvent = %v, want empty", ids2)
	}

	// TriggerSources is the static set.
	if len(svc.TriggerSources()) == 0 {
		t.Fatalf("TriggerSources empty")
	}
}

func TestServiceRunOutputAndFs(t *testing.T) {
	svc, store, _, runsDir := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})
	run := seedRun(t, store, sc.ID, RunSucceeded)

	// Hand-create the persisted run dir under runsDir/<run_id>/.
	dir := filepath.Join(runsDir, run.ID)
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o700); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.log"), []byte("line1\nline2\nline3\n"), 0o600); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr.log"), []byte("err1\n"), 0o600); err != nil {
		t.Fatalf("write stderr: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out", "result.txt"), []byte("a\nb\n"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	// RunOutput stdout line-slice [offset=2, limit=1) → line2.
	out, err := svc.RunOutput(ctx, ownerA, run.ID, "stdout", 2, 1)
	if err != nil {
		t.Fatalf("RunOutput: %v", err)
	}
	if out != "line2\n" {
		t.Fatalf("RunOutput slice = %q, want line2", out)
	}

	// RunOutput unknown stream → ErrValidation.
	if _, err := svc.RunOutput(ctx, ownerA, run.ID, "bogus", 0, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("RunOutput bad stream: want ErrValidation, got %v", err)
	}

	// RunFsList root: sees stdout.log, stderr.log, out/.
	entries, err := svc.RunFsList(ctx, ownerA, run.ID, "")
	if err != nil {
		t.Fatalf("RunFsList: %v", err)
	}
	names := map[string]FileEntry{}
	for _, e := range entries {
		names[e.Path] = e
	}
	if _, ok := names["stdout.log"]; !ok {
		t.Fatalf("RunFsList missing stdout.log: %+v", entries)
	}
	if d, ok := names["out"]; !ok || !d.IsDir {
		t.Fatalf("RunFsList missing out dir: %+v", entries)
	}

	// RunFsList subdir.
	sub, err := svc.RunFsList(ctx, ownerA, run.ID, "out")
	if err != nil {
		t.Fatalf("RunFsList subdir: %v", err)
	}
	if len(sub) != 1 || sub[0].Path != filepath.Join("out", "result.txt") {
		t.Fatalf("RunFsList subdir = %+v", sub)
	}

	// RunFsRead a file.
	body, err := svc.RunFsRead(ctx, ownerA, run.ID, "out/result.txt", 0, 0)
	if err != nil {
		t.Fatalf("RunFsRead: %v", err)
	}
	if body != "a\nb\n" {
		t.Fatalf("RunFsRead = %q", body)
	}

	// Path traversal rejected.
	if _, err := svc.RunFsList(ctx, ownerA, run.ID, "../.."); !errors.Is(err, ErrValidation) {
		t.Fatalf("RunFsList traversal: want ErrValidation, got %v", err)
	}
	if _, err := svc.RunFsRead(ctx, ownerA, run.ID, "../../etc/passwd", 0, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("RunFsRead traversal: want ErrValidation, got %v", err)
	}

	// Foreign owner → ErrNotFound.
	if _, err := svc.RunOutput(ctx, ownerB, run.ID, "stdout", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RunOutput: want ErrNotFound, got %v", err)
	}
	if _, err := svc.RunFsList(ctx, ownerB, run.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RunFsList: want ErrNotFound, got %v", err)
	}
}

func TestServiceReadsRunFilesFromRebuildableRunsDir(t *testing.T) {
	svc, store, _, runsDir := newTestService(t)
	ctx := context.Background()
	sc, _ := svc.Create(ctx, ownerA, CreateInput{Name: "x", Body: "print(1)"})
	run := seedRun(t, store, sc.ID, RunSucceeded)

	runDir := filepath.Join(runsDir, run.ID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "stdout.log"), []byte("fresh boot output\n"), 0o600); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "artifact.txt"), []byte("rebuilt\n"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	out, err := svc.RunOutput(ctx, ownerA, run.ID, "stdout", 0, 0)
	if err != nil {
		t.Fatalf("RunOutput: %v", err)
	}
	if out != "fresh boot output\n" {
		t.Fatalf("RunOutput = %q, want rebuildable run output", out)
	}
	body, err := svc.RunFsRead(ctx, ownerA, run.ID, "artifact.txt", 0, 0)
	if err != nil {
		t.Fatalf("RunFsRead: %v", err)
	}
	if body != "rebuilt\n" {
		t.Fatalf("RunFsRead = %q, want artifact from runs dir", body)
	}
}

// --- Import (Dropbox → scripts) ---

// stubFetcher is a ContentFetcher returning canned bytes/err, so Import is
// exercised with no live dropbox or network.
type stubFetcher struct {
	data []byte
	err  error
}

func (f stubFetcher) Fetch(ctx context.Context, path string) ([]byte, error) {
	return f.data, f.err
}

// TestImportHappyAndIdempotent: a first import writes a row with the body and a
// basename-derived name; a second import of the SAME path updates that same row
// (same script_id, new body) with no duplicate.
// R-2F5E-A5D1
func TestImportHappyAndIdempotent(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	ctx := context.Background()
	plane := svc.Plane.(*fakePlane)
	svc.Fetcher = stubFetcher{data: []byte("print('v1')\n")}

	sc, err := svc.Import(ctx, ownerA, "/scripts/nightly.py", "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if sc.Name != "nightly.py" {
		t.Fatalf("name not derived from basename: %q", sc.Name)
	}
	if sc.Body != "print('v1')\n" {
		t.Fatalf("body: %q", sc.Body)
	}
	if sc.SourcePath != "/scripts/nightly.py" {
		t.Fatalf("source_path: %q", sc.SourcePath)
	}
	if sc.Config.Interpreter != "python3" {
		t.Fatalf("config not defaulted: %+v", sc.Config)
	}
	if len(plane.calls) != 2 || plane.calls[0].verb != "create" || plane.calls[1].verb != "commit" || plane.calls[1].files["main.py"] != "print('v1')\n" {
		t.Fatalf("first import plane calls = %+v, want Create then fetched-byte Commit", plane.calls)
	}

	// Re-import the same path with new bytes → same id, updated body, no dup.
	plane.resetCalls()
	svc.Fetcher = stubFetcher{data: []byte("print('v2')\n")}
	sc2, err := svc.Import(ctx, ownerA, "/scripts/nightly.py", "")
	if err != nil {
		t.Fatalf("re-Import: %v", err)
	}
	if sc2.ID != sc.ID {
		t.Fatalf("re-import created a new row: %q != %q", sc2.ID, sc.ID)
	}
	if sc2.Body != "print('v2')\n" {
		t.Fatalf("re-import did not update body: %q", sc2.Body)
	}
	if len(plane.calls) != 1 || plane.calls[0].verb != "commit" || plane.calls[0].key != sc.NameKey || plane.calls[0].files["main.py"] != "print('v2')\n" {
		t.Fatalf("re-import plane calls = %+v, want one Commit on existing key", plane.calls)
	}
	list, err := store.ListScripts(ctx, ownerA)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected exactly one row after re-import: len=%d err=%v", len(list), err)
	}
}

// TestImportRejectsNonUTF8 asserts a binary blob is rejected as ErrValidation.
func TestImportRejectsNonUTF8(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	svc.Fetcher = stubFetcher{data: []byte{0xff, 0xfe, 0x00}}
	if _, err := svc.Import(context.Background(), ownerA, "/scripts/blob.bin", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-UTF-8: want ErrValidation, got %v", err)
	}
}

// TestImportRejectsTooLarge asserts a body over 1 MiB is rejected as ErrTooLarge.
func TestImportRejectsTooLarge(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	big := make([]byte, (1<<20)+1)
	for i := range big {
		big[i] = 'a'
	}
	svc.Fetcher = stubFetcher{data: big}
	if _, err := svc.Import(context.Background(), ownerA, "/scripts/huge.py", ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too-large: want ErrTooLarge, got %v", err)
	}
}

// TestImportRejectsEmptySourcePath asserts a missing source_path is ErrValidation
// (before any fetch).
func TestImportRejectsEmptySourcePath(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	svc.Fetcher = stubFetcher{data: []byte("x")}
	if _, err := svc.Import(context.Background(), ownerA, "  ", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty source_path: want ErrValidation, got %v", err)
	}
}
