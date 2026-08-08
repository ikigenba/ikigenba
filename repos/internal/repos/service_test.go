package repos

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

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
