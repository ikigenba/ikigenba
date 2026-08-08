package repos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRepositoryLifecycleIsOwnerScopedAndPreservesGitHistory(t *testing.T) {
	// R-KTX8-LS92
	// R-KV54-ZJZR
	// R-KWD1-DBQG
	// R-KXKX-R3H5
	// R-KYSU-4V7U
	ctx := context.Background()
	conn, store := newTestStore(t)
	custody, root := newTestCustody(t)
	service := serviceWithOutbox(t, conn, store, custody)

	demo, err := service.CreateRepository(ctx, Repository{Kind: "code", Name: "demo", OwnerID: "owner-a", OwnerEmail: "shared@example.test"})
	if err != nil || demo.DefaultBranch != "main" || !custody.Exists("code", "demo") {
		t.Fatalf("default create = %#v, err=%v, custody exists=%v", demo, err, custody.Exists("code", "demo"))
	}
	empty, err := service.GetRepository(ctx, "owner-a", "code", "demo")
	if err != nil || empty.Head != "" || len(empty.Branches) != 0 {
		t.Fatalf("empty repository detail = %#v, err=%v", empty, err)
	}
	door := httptest.NewServer(GitDoorHandler(service))
	t.Cleanup(door.Close)
	clone := filepath.Join(t.TempDir(), "empty-clone")
	runGit(t, "", "-c", "http.extraHeader=X-Forwarded-Proto: https", "-c", "http.extraHeader=X-Owner-Id: owner-a", "clone", door.URL+"/git/code/demo.git", clone)
	if _, err := service.CreateRepository(ctx, Repository{Kind: "code", Name: "demo", OwnerID: "owner-a"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create error = %v, want conflict", err)
	}
	if _, err := service.CreateRepository(ctx, Repository{Kind: "unknown", Name: "bad", OwnerID: "owner-a"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown kind create error = %v, want validation", err)
	}
	if _, err := service.CreateRepository(ctx, Repository{Kind: "sites", Name: "home", OwnerID: "owner-a", OwnerEmail: "shared@example.test"}); err != nil {
		t.Fatalf("sites create: %v", err)
	}
	if _, err := service.CreateRepository(ctx, Repository{Kind: "code", Name: "foreign", OwnerID: "owner-b", OwnerEmail: "shared@example.test"}); err != nil {
		t.Fatalf("foreign create: %v", err)
	}
	if _, err := service.GetRepository(ctx, "owner-b", "code", "demo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("different owner get error = %v, want not_found", err)
	}
	ownerB, err := service.ListRepositories(ctx, "owner-b", nil)
	if err != nil || len(ownerB) != 1 || ownerB[0].Name != "foreign" {
		t.Fatalf("owner-b list = %#v, err=%v", ownerB, err)
	}
	sites := "sites"
	filtered, err := service.ListRepositories(ctx, "owner-a", &sites)
	if err != nil || len(filtered) != 1 || filtered[0].Name != "home" {
		t.Fatalf("kind-filtered list = %#v, err=%v", filtered, err)
	}

	demoPath, _ := custody.Path("code", "demo")
	head := seedCommits(t, demoPath, 3)
	runGit(t, demoPath, "branch", "feature", "main")
	detail, err := service.GetRepository(ctx, "owner-a", "code", "demo")
	if err != nil || detail.Head != head || !reflect.DeepEqual(detail.Branches, []string{"feature", "main"}) {
		t.Fatalf("git-backed detail = %#v, err=%v, want head %s and real branches", detail, err, head)
	}
	if got := "/srv/repos/git/" + detail.Repository.Kind + "/" + detail.Repository.Name + ".git"; !strings.HasSuffix(got, "/srv/repos/git/code/demo.git") {
		t.Fatalf("clone URL = %q", got)
	}
	if _, err := service.CreateRepository(ctx, Repository{Kind: "code", Name: "taken", OwnerID: "owner-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenameRepository(ctx, "owner-a", "code", "demo", "taken"); !errors.Is(err, ErrConflict) || !custody.Exists("code", "demo") {
		t.Fatalf("conflicting rename error=%v source exists=%v", err, custody.Exists("code", "demo"))
	}
	renamed, err := service.RenameRepository(ctx, "owner-a", "code", "demo", "renamed")
	if err != nil || renamed.Name != "renamed" {
		t.Fatalf("rename = %#v, err=%v", renamed, err)
	}
	if _, err := service.GetRepository(ctx, "owner-a", "code", "demo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old key get error = %v, want not_found", err)
	}
	renamedPath, _ := custody.Path("code", "renamed")
	if got := strings.TrimSpace(runGit(t, renamedPath, "rev-parse", "main")); got != head {
		t.Fatalf("renamed head = %s, want %s", got, head)
	}
	if got := strings.TrimSpace(runGit(t, renamedPath, "rev-list", "--count", "main")); got != "3" {
		t.Fatalf("renamed commit count = %s, want 3", got)
	}

	archived, err := service.DeleteRepository(ctx, "owner-a", "code", "renamed")
	if err != nil || archived.ArchivedPath == nil {
		t.Fatalf("delete result = %#v, err=%v", archived, err)
	}
	live, err := service.ListRepositories(ctx, "owner-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range live {
		if repository.Name == "renamed" {
			t.Fatalf("archived repository remained in list: %#v", live)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/git/code/renamed.git/info/refs?service=git-upload-pack", nil)
	request.Header.Set("X-Owner-Id", "owner-a")
	response := httptest.NewRecorder()
	GitDoorHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("archived git door status = %d, want 404", response.Code)
	}
	assertArchivedHistory(t, filepath.Join(root, filepath.FromSlash(*archived.ArchivedPath)), head, "3")
	again, err := service.DeleteRepository(ctx, "owner-a", "code", "renamed")
	if err != nil || !reflect.DeepEqual(again, archived) {
		t.Fatalf("idempotent delete = %#v, err=%v, want unchanged %#v", again, err, archived)
	}
	var archivedEvents int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE kind = 'archived'`).Scan(&archivedEvents); err != nil || archivedEvents != 1 {
		t.Fatalf("archived event count=%d err=%v, want 1", archivedEvents, err)
	}
	if _, err := service.CreateRepository(ctx, Repository{Kind: "code", Name: "renamed", OwnerID: "owner-a"}); err != nil {
		t.Fatalf("recreate archived key: %v", err)
	}

	files, err := filepath.Glob(filepath.Join("*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if (strings.Contains(text, "os.Remove(") || strings.Contains(text, "os.RemoveAll(")) && (strings.Contains(text, `"git"`) || strings.Contains(text, `"archive"`)) {
			t.Errorf("%s contains a destroy path targeting custody roots", file)
		}
	}
}

func TestSetStatusReplacesVerdictAndRejectsInvalidStateOrCommit(t *testing.T) {
	// R-KFAG-0JCQ
	ctx := context.Background()
	_, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC), step: time.Minute}
	custody := testCustodyWithClock(t, clock)
	if err := custody.Init(ctx, "code", "demo"); err != nil {
		t.Fatal(err)
	}
	path, err := custody.Path("code", "demo")
	if err != nil {
		t.Fatal(err)
	}
	sha := seedCommits(t, path, 1)
	service := NewService(store)
	service.SetCustody(custody)
	detail := "check is running"
	want := Status{Kind: "code", Name: "demo", SHA: sha, CheckName: "tests", State: "pending", Detail: &detail, Actor: "worker-1", UpdatedAt: clock.now}

	got, err := service.SetStatus(ctx, Status{Kind: want.Kind, Name: want.Name, SHA: sha, CheckName: want.CheckName, State: want.State, Detail: want.Detail, Actor: want.Actor})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetStatus result = %#v, want %#v", got, want)
	}
	stored, err := service.ListStatuses(ctx, "code", "demo", sha)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, []Status{want}) {
		t.Fatalf("stored statuses = %#v, want %#v", stored, []Status{want})
	}

	want.State = "success"
	want.Detail = nil
	want.Actor = "worker-2"
	want.UpdatedAt = clock.now
	got, err = service.SetStatus(ctx, Status{Kind: want.Kind, Name: want.Name, SHA: sha, CheckName: want.CheckName, State: want.State, Actor: want.Actor})
	if err != nil {
		t.Fatal(err)
	}
	stored, err = service.ListStatuses(ctx, "code", "demo", sha)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(stored, []Status{want}) {
		t.Fatalf("replacement result=%#v stored=%#v, want exactly %#v", got, stored, []Status{want})
	}

	invalid := Status{Kind: "code", Name: "demo", SHA: sha, CheckName: "invalid", State: "unknown", Actor: "worker-3"}
	if _, err := service.SetStatus(ctx, invalid); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid state error = %v, want ErrValidation", err)
	}
	missingSHA := strings.Repeat("f", 40)
	missing := Status{Kind: "code", Name: "demo", SHA: missingSHA, CheckName: "tests", State: "failure", Actor: "worker-3"}
	if _, err := service.SetStatus(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing commit error = %v, want ErrNotFound", err)
	}
	stored, err = service.ListStatuses(ctx, "code", "demo", sha)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, []Status{want}) {
		t.Fatalf("rejected status changed valid rows: %#v", stored)
	}
	missingRows, err := service.ListStatuses(ctx, "code", "demo", missingSHA)
	if err != nil {
		t.Fatal(err)
	}
	if missingRows == nil || len(missingRows) != 0 {
		t.Fatalf("missing commit statuses = %#v, want non-nil empty slice", missingRows)
	}
}

func TestListStatusesReturnsAllChecksScopedToRepository(t *testing.T) {
	// R-KHQ8-S2U4
	ctx := context.Background()
	_, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 18, 0, 0, 0, time.UTC), step: time.Second}
	custody := testCustodyWithClock(t, clock)
	for _, name := range []string{"first", "second"} {
		if err := custody.Init(ctx, "code", name); err != nil {
			t.Fatal(err)
		}
	}
	firstPath, err := custody.Path("code", "first")
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := custody.Path("code", "second")
	if err != nil {
		t.Fatal(err)
	}
	sha := seedCommits(t, firstPath, 1)
	runGit(t, firstPath, "push", secondPath, "main:main")
	service := NewService(store)
	service.SetCustody(custody)

	for _, status := range []Status{
		{Kind: "code", Name: "first", SHA: sha, CheckName: "lint", State: "success", Actor: "lint-worker"},
		{Kind: "code", Name: "first", SHA: sha, CheckName: "tests", State: "pending", Actor: "test-worker"},
		{Kind: "code", Name: "second", SHA: sha, CheckName: "foreign", State: "failure", Actor: "other-worker"},
	} {
		if _, err := service.SetStatus(ctx, status); err != nil {
			t.Fatal(err)
		}
	}
	got, err := service.ListStatuses(ctx, "code", "first", sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].CheckName != "lint" || got[0].State != "success" || got[1].CheckName != "tests" || got[1].State != "pending" {
		t.Fatalf("first repository statuses = %#v, want its lint and tests checks only", got)
	}
	for _, status := range got {
		if status.Name != "first" || status.CheckName == "foreign" {
			t.Fatalf("cross-repository status leaked into listing: %#v", got)
		}
	}
	empty, err := service.ListStatuses(ctx, "code", "first", strings.Repeat("0", 40))
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("unseen sha statuses = %#v, want non-nil empty slice", empty)
	}
}
