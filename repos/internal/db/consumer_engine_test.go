package db

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	appdb "appkit/db"
	"eventplane/consumer"
)

func TestConsumerEngineBootPersistsWebhooksOffset(t *testing.T) {
	// R-TZAN-U7IJ
	handshake := make(chan *http.Request, 1)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case handshake <- r.Clone(context.Background()):
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ": subscribed\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(feed.Close)

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatalf("open temp database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply full embedded migration set: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(ctx, consumer.Config{
			FeedURL:    feed.URL,
			From:       "tail",
			DB:         conn,
			Source:     "webhooks",
			ConsumerID: "repos",
			Logger:     slog.New(slog.DiscardHandler),
			HTTPClient: feed.Client(),
			Now: func() time.Time {
				return time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
			},
		}, func(context.Context, consumer.Event) error { return nil })
	}()

	select {
	case request := <-handshake:
		if got := request.Header.Get("X-Consumer-Id"); got != "repos" {
			t.Errorf("X-Consumer-Id = %q, want repos", got)
		}
		if got := request.URL.Query().Get("from"); got != "tail" {
			t.Errorf("from = %q, want tail", got)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("consumer did not complete feed subscription handshake")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		var subscribed int
		err = conn.QueryRow(`SELECT subscribed FROM feed_offset WHERE source = ?`, "webhooks").Scan(&subscribed)
		if err == nil {
			if subscribed != 1 {
				t.Errorf("feed_offset subscribed = %d, want 1", subscribed)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("feed_offset row was not persisted: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("consumer.Run after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("consumer.Run did not stop after cancellation")
	}
}
