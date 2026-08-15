package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"eventplane/outbox"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type commitGraph struct {
	main, fastForward, divergent string
}

func TestApplyRefUpdateProtectsMainButAllowsOtherBranchRewrites(t *testing.T) {
	// R-JB1R-E3VT
	ctx := context.Background()
	conn, store := newTestStore(t)
	custody, _ := newTestCustody(t)
	graph := makeCommitGraph(t, custody)
	service := serviceWithOutbox(t, conn, store, custody)

	inTx(t, conn, func(tx *sql.Tx) error {
		return service.ApplyRefUpdate(ctx, tx, RefUpdate{Kind: "code", Name: "demo", Ref: "refs/heads/main", OldSHA: graph.main, NewSHA: graph.fastForward, Actor: "client-1"})
	})
	assertRef(t, custody, "refs/heads/main", graph.fastForward)

	for _, update := range []RefUpdate{
		{Kind: "code", Name: "demo", Ref: "refs/heads/main", OldSHA: graph.fastForward, NewSHA: "", Actor: "client-1"},
		{Kind: "code", Name: "demo", Ref: "refs/heads/main", OldSHA: graph.fastForward, NewSHA: graph.divergent, Actor: "client-1"},
	} {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.ApplyRefUpdate(ctx, tx, update); !errors.Is(err, ErrForcePush) {
			t.Fatalf("main rejected update error = %v, want ErrForcePush", err)
		}
		_ = tx.Rollback()
		assertRef(t, custody, "refs/heads/main", graph.fastForward)
	}

	inTx(t, conn, func(tx *sql.Tx) error {
		return service.ApplyRefUpdate(ctx, tx, RefUpdate{Kind: "code", Name: "demo", Ref: "refs/heads/ikigenba/work", NewSHA: graph.fastForward, Actor: "runner"})
	})
	inTx(t, conn, func(tx *sql.Tx) error {
		return service.ApplyRefUpdate(ctx, tx, RefUpdate{Kind: "code", Name: "demo", Ref: "refs/heads/ikigenba/work", OldSHA: graph.fastForward, NewSHA: graph.divergent, Actor: "runner"})
	})
	assertRef(t, custody, "refs/heads/ikigenba/work", graph.divergent)
}

func TestApplyRefUpdateAppendsOnePushOnlyAfterAcceptedMutation(t *testing.T) {
	// R-JC9N-RVMI
	ctx := context.Background()
	conn, store := newTestStore(t)
	custody, _ := newTestCustody(t)
	graph := makeCommitGraph(t, custody)
	service := serviceWithOutbox(t, conn, store, custody)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted := RefUpdate{Kind: "code", Name: "demo", Ref: "refs/heads/main", OldSHA: graph.main, NewSHA: graph.fastForward, Actor: "client-live"}
	if err := service.ApplyRefUpdate(ctx, tx, accepted); err != nil {
		t.Fatal(err)
	}
	var kind, subject, raw string
	if err := tx.QueryRowContext(ctx, `SELECT kind, subject, payload FROM outbox`).Scan(&kind, &subject, &raw); err != nil {
		t.Fatal(err)
	}
	var payload PushPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	want := PushPayload{Kind: "code", Name: "demo", Branch: "main", SHA: graph.fastForward, OldSHA: graph.main, Actor: "client-live"}
	if kind != "push" || subject != "/code/demo" || payload != want {
		t.Fatalf("outbox kind=%q subject=%q payload=%#v, want push /code/demo %#v", kind, subject, payload, want)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("accepted outbox count=%d err=%v, want 1", count, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM outbox`); err != nil {
		t.Fatal(err)
	}
	rejectedTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = service.ApplyRefUpdate(ctx, rejectedTx, RefUpdate{Kind: "code", Name: "demo", Ref: "refs/heads/main", OldSHA: graph.fastForward, NewSHA: graph.divergent, Actor: "client-live"})
	if !errors.Is(err, ErrForcePush) {
		t.Fatalf("rejected error = %v, want ErrForcePush", err)
	}
	if err := rejectedTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected outbox count=%d err=%v, want 0", count, err)
	}
	_ = rejectedTx.Rollback()
	assertRef(t, custody, "refs/heads/main", graph.fastForward)
}

func makeCommitGraph(t *testing.T, custody *Custody) commitGraph {
	t.Helper()
	ctx := context.Background()
	if err := custody.Init(ctx, "code", "demo"); err != nil {
		t.Fatal(err)
	}
	bare, _ := custody.Path("code", "demo")
	main := seedCommits(t, bare, 2)
	work := filepath.Join(t.TempDir(), "graph")
	runGit(t, "", "clone", bare, work)
	if err := os.WriteFile(filepath.Join(work, "fast.txt"), []byte("fast"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "fast.txt")
	runGit(t, work, "commit", "-m", "fast forward")
	fast := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	runGit(t, work, "push", "origin", "HEAD:refs/heads/candidate")
	runGit(t, work, "checkout", "-b", "divergent", "HEAD~1")
	if err := os.WriteFile(filepath.Join(work, "divergent.txt"), []byte("divergent"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "divergent.txt")
	runGit(t, work, "commit", "-m", "divergent")
	divergent := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	runGit(t, work, "push", "origin", "HEAD:refs/heads/divergent-seed")
	return commitGraph{main: main, fastForward: fast, divergent: divergent}
}

func serviceWithOutbox(t *testing.T, conn *sql.DB, store *Store, custody *Custody) *Service {
	t.Helper()
	producer, err := outbox.New(conn, outbox.Options{Source: "repos", Registry: Events, Now: func() time.Time { return time.Unix(1700000100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	service.SetCustody(custody)
	service.SetProducer(producer)
	return service
}

func assertRef(t *testing.T, custody *Custody, ref, want string) {
	t.Helper()
	path, _ := custody.Path("code", "demo")
	if got := strings.TrimSpace(runGit(t, path, "rev-parse", ref)); got != want {
		t.Fatalf("%s = %s, want %s", ref, got, want)
	}
}
