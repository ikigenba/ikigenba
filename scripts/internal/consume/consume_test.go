package consume_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appkitdatabase "appkit/db"
	"eventplane/consumer"
	"eventplane/correlation"

	"scripts/internal/consume"
	"scripts/internal/db"
	"scripts/internal/script"
)

type call struct{ source, kind, subject, key, eventID string }

func TestHandlerUsesCanonicalKeyAndForwardsRoutingFields(t *testing.T) {
	// R-7ZUN-NNNL
	var mu sync.Mutex
	var lookedUp string
	var fired []call
	done := make(chan struct{})
	lookup := func(_ context.Context, source, key string) ([]string, error) {
		mu.Lock()
		lookedUp = source + "|" + key
		mu.Unlock()
		return []string{"script-1"}, nil
	}
	fire := func(_ context.Context, _ string, source, kind, subject, eventID string, _ []byte) error {
		mu.Lock()
		fired = append(fired, call{source: source, kind: kind, subject: subject, eventID: eventID})
		mu.Unlock()
		close(done)
		return nil
	}
	ev := consumer.Event{Source: "dropbox", Kind: "create", Subject: "/bills/a.pdf", ID: "evt-1", Payload: json.RawMessage(`{}`)}
	h := consume.Handler(fire, lookup, "dropbox", slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err := h(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fire did not run")
	}
	mu.Lock()
	defer mu.Unlock()
	if lookedUp != "dropbox|dropbox:create/bills/a.pdf" {
		t.Fatalf("lookup = %q", lookedUp)
	}
	if len(fired) != 1 || fired[0] != (call{source: "dropbox", kind: "create", subject: "/bills/a.pdf", eventID: "evt-1"}) {
		t.Fatalf("fire = %+v", fired)
	}
	if err := consume.Handler(fire, lookup, "dropbox", nil)(context.Background(), consumer.Event{ID: "evt"}); !errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("empty kind = %v", err)
	}
}

func TestSubscriptionsUseDoubleStar(t *testing.T) {
	// R-7ZUN-NNNL
	for _, sub := range consume.Subscriptions([]string{"cron", "crm", "ledger", "dropbox", "prompts"}) {
		if sub.Filter != "**" {
			t.Fatalf("%s filter = %q", sub.Source, sub.Filter)
		}
	}
}

type recordingRunner struct {
	spawned chan script.Run
}

func (r *recordingRunner) Spawn(run script.Run, _ []byte) { r.spawned <- run }
func (r *recordingRunner) Cancel(string) bool             { return false }

func newRealService(t *testing.T) (*script.Service, *sql.DB, *recordingRunner) {
	t.Helper()
	conn, err := appkitdatabase.Open(filepath.Join(t.TempDir(), "scripts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	migrations, err := appkitdatabase.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdatabase.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{spawned: make(chan script.Run, 8)}
	return script.NewService(script.NewStore(conn), t.TempDir(), runner), conn, runner
}

func TestHandlerCarriesContextCorrelationAcrossDetachedFanout(t *testing.T) {
	// R-4XFG-EFU8
	svc, conn, runner := newRealService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		sc, err := svc.Create(ctx, "owner@example.com", script.CreateInput{Name: fmt.Sprintf("script-%d", i), Body: "print(1)"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetTrigger(ctx, "owner@example.com", sc.ID, "crm:contact.*"); err != nil {
			t.Fatal(err)
		}
	}
	handler := consume.Handler(svc.RunForEvent, svc.ScriptsForEvent, "crm", slog.New(slog.NewJSONHandler(io.Discard, nil)))

	assertDelivery := func(eventID, contextID, wireID string) {
		t.Helper()
		deliveryCtx := correlation.WithContext(context.Background(), contextID)
		ev := consumer.Event{ID: eventID, Source: "crm", Kind: "contact.created", CorrelationID: wireID, Payload: json.RawMessage(`{}`)}
		if err := handler(deliveryCtx, ev); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			select {
			case <-runner.spawned:
			case <-time.After(2 * time.Second):
				t.Fatalf("delivery %s spawned only %d runs", eventID, i)
			}
		}
		rows, err := conn.Query(`SELECT correlation_id FROM runs WHERE trigger_event_id = ?`, eventID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var got string
			if err := rows.Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != contextID {
				t.Fatalf("delivery %s stored correlation = %q, want context id %q (wire id %q)", eventID, got, contextID, wireID)
			}
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("delivery %s stored %d runs, want 3", eventID, count)
		}
	}

	assertDelivery("event-with-wire-chain", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	assertDelivery("event-engine-rooted", "01BX5ZZKBKACTAV9WEVGEMMVRZ", "")
}
