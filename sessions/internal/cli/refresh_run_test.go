package cli_test

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/settings"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
	"github.com/ikigenba/ikigenba/sessions/internal/web"
)

type refreshAnswers struct {
	lists  []tools.SessionList
	totals tools.Totals
	page   string
}

func readRefreshAnswers(t *testing.T, r *runRig, roots int) refreshAnswers {
	t.Helper()
	var answer refreshAnswers
	limit := 127
	args := tools.ListSessionsArgs{Limit: &limit}
	seen := make(map[string]bool)
	for len(answer.lists) <= roots/limit {
		list := runDecode[tools.SessionList](t, r.ok(t, tools.ToolListSessions, args))
		answer.lists = append(answer.lists, list)
		for _, session := range list.Sessions {
			if seen[session.ID] || session.Kind != "operate" {
				t.Fatalf("duplicate or stale session: %+v", session)
			}
			seen[session.ID] = true
		}
		if list.Next == nil {
			break
		}
		args.Page = list.Next
	}
	if len(seen) != roots || answer.lists[len(answer.lists)-1].Next != nil {
		t.Fatalf("list walk returned %d roots, want %d", len(seen), roots)
	}
	answer.totals = runDecode[tools.Totals](t, r.ok(t, tools.ToolTotalSessions, tools.TotalSessionsArgs{Period: "week", From: runInstant.Format("2006-01-02"), Group: "kind"}))
	if len(answer.totals.Periods) != 1 || len(answer.totals.Periods[0].Groups) != 1 || answer.totals.Periods[0].Groups[0].Group != "operate" || answer.totals.Periods[0].Groups[0].Sessions != roots {
		t.Fatalf("stale totals: %+v", answer.totals)
	}
	status, body, _ := r.request(t, "/", true)
	if status != http.StatusOK {
		t.Fatalf("landing status %d", status)
	}
	answer.page = body
	return answer
}

// R-RO2J-JNL5
func TestRunFinishesRefreshBeforeFirstRead(t *testing.T) {
	r := newRunRig(t)
	path := filepath.Join(r.p.Dir, "state", "sessions.db")
	open := func() (*db.DB, *store.Store) {
		d, err := db.Open(t.Context(), db.Config{Path: path, Migrations: sessions.Migrations(), Now: r.p.Now, Service: pages.ServiceName, Stderr: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		})
		return d, store.New(d, store.Config{Now: r.p.Now})
	}
	d, st := open()
	roots := store.BatchSize + 1
	batch := make([]store.PushSession, 0, min(store.MaxPushSessions, store.MaxPushTurns/3))
	first := runInstant.Add(-time.Hour)
	for i := range roots {
		batch = append(batch, store.PushSession{Tool: "codex", ID: "refresh-root-" + decimal(i), Host: "refresh-host", Directory: "/work/refresh", Turns: []store.PushTurn{
			{Moment: first.Format(time.RFC3339Nano), Whose: "person"},
			{Moment: first.Add(time.Minute).Format(time.RFC3339Nano), Whose: "agent", Model: "refresh-model", Input: int64(i + 1)},
			{Moment: first.Add(3 * time.Minute).Format(time.RFC3339Nano), Whose: "person"},
		}})
		if len(batch) == cap(batch) || i == roots-1 {
			if _, err := st.Push(t.Context(), "owner", "owner@example.test", batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
	}
	before, err := st.Sessions(t.Context(), store.Filter{}, store.Position{}, 1)
	if err != nil || len(before) != 1 || string(before[0].Summary.Kind) != "untagged" {
		t.Fatalf("initial fixture: %+v, %v", before, err)
	}
	// The rule is committed while its summaries retain the old classification.
	// The empty refresh pair is the store's permitted unfinished-refresh fixture.
	if err := d.Write(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO rules (host,directory,command,kind) VALUES (?,?,?,?)", "refresh-host", "/work/refresh", "", "operate"); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), "UPDATE settings SET refresh_after_tool='',refresh_after_id='' WHERE id=1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	r.start(t)
	firstAnswers := readRefreshAnswers(t, r, roots)
	laterAnswers := readRefreshAnswers(t, r, roots)
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal("Run failed to stop")
	}

	// Complete the exact same database independently, then serve the completed
	// store directly to establish the answers the run was required to give.
	_, completed := open()
	if _, err := completed.FinishRefresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: r.p.Version, Sink: new(telemetry.Capture), Stderr: io.Discard, Now: r.p.Now, Sleep: func(context.Context, time.Duration) {}, Rand: repeatByte(0x5a)})
	t.Cleanup(func() { w.Shutdown(context.Background(), "reference complete") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: r.p.Version, Telemetry: w})
	h := httptest.NewServer(web.Handler(web.Config{Banner: r.p.Banner, MCP: srv, Store: completed, Telemetry: w, Settings: settings.Defaults()}))
	t.Cleanup(h.Close)
	reference := &runRig{ln: h.Listener, http: h.Client(), client: mcp.NewClient(mcp.ClientConfig{Endpoint: h.URL + "/mcp", HTTPClient: h.Client(), Name: "refresh-reference", Version: "test"})}
	want := readRefreshAnswers(t, reference, roots)
	for i, got := range []refreshAnswers{firstAnswers, laterAnswers} {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("read round %d differs from the fully refreshed store: lists equal=%t, totals equal=%t, page equal=%t", i, reflect.DeepEqual(got.lists, want.lists), reflect.DeepEqual(got.totals, want.totals), got.page == want.page)
		}
	}
}
