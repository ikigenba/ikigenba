package consume_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"eventplane/consumer"

	"scripts/internal/consume"
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
