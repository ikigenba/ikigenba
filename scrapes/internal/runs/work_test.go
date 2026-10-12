package runs

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	scrapes "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

type workReader struct{}

func (workReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 1
	}
	return len(p), nil
}

type workScraper struct{ fail bool }

func (workScraper) Name() string { return "test" }
func (s workScraper) Scrape(context.Context, func([]source.Row) error) error {
	if s.fail {
		return errors.New("source failed")
	}
	return nil
}

// R-21PS-4DPL
func TestUnrecordedEndingReleasesWork(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish", true: "fail"}[fail], func(t *testing.T) {
			t.Setenv("IKIGENBA_SERVICES", "")
			ctx := context.Background()
			now := func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }
			d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "scrapes.db"), Migrations: scrapes.Migrations(), Now: now})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := d.Close(); err != nil {
					t.Error(err)
				}
			}()
			st := store.New(d, store.Config{Now: now, Rand: workReader{}})
			r, err := st.AddRun(ctx, store.Run{Scraper: "test", UserID: "test-user", RequestID: "test-request"})
			if err != nil {
				t.Fatal(err)
			}
			tc := new(telemetry.Capture)
			ec := new(events.Capture)
			w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: tc, Now: now, Rand: workReader{}})
			em := events.New(events.Config{Service: "scrapes", Sink: ec, Now: now, Rand: workReader{}, Emits: trail.Emits()})
			defer em.Shutdown(ctx)
			defer w.Shutdown(ctx, "test")
			e := New(Config{Store: st, Telemetry: w, Events: em})
			workctx, cancel := context.WithCancel(ctx)
			defer cancel()
			e.active[r.ID] = cancel
			e.contexts[r.ID] = workctx
			trail.Record(workctx, w, em, trail.Started, r)
			d.SetFailing(true)
			e.work(workctx, workScraper{fail: fail}, r)
			d.SetFailing(false)
			got, err := st.GetRun(ctx, r.ID)
			if err != nil || got != r {
				t.Fatal(got, err)
			}
			if err := w.Flush(ctx); err != nil {
				t.Fatal(err)
			}
			if err := em.Flush(ctx); err != nil {
				t.Fatal(err)
			}
			if len(tc.Events()) != 1 || len(ec.Events()) != 1 || tc.Events()[0].Name != trail.Started || ec.Events()[0].Name != trail.Started {
				t.Fatal(tc.Events(), ec.Events())
			}
			drained := make(chan struct{})
			go func() { e.Drain(ctx); close(drained) }()
			select {
			case <-drained:
			case <-time.After(time.Second):
				t.Fatal("unrecorded ending retained work")
			}
		})
	}
}
