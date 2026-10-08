package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

// R-WG3Q-IOBR R-XKCF-53SO
func TestRunReadyCatalogAndMigrationClock(t *testing.T) {
	for _, emptyDirs := range []bool{false, true} {
		for _, notify := range []bool{false, true} {
			t.Run(map[bool]string{false: "absent", true: "empty"}[emptyDirs]+map[bool]string{false: "-no-notify", true: "-notify"}[notify], func(t *testing.T) {
				f := newStartFixture(t)
				if emptyDirs {
					for _, name := range []string{"state", "cache"} {
						if err := os.Mkdir(filepath.Join(f.p.Dir, name), 0700); err != nil {
							t.Fatal(err)
						}
					}
				}
				reference := statusConfig(t.TempDir())
				reference.Now = f.p.Now
				statusFixture(t, reference, "")
				var expected bytes.Buffer
				if err := db.Status(context.Background(), reference, &expected); err != nil {
					t.Fatal(err)
				}
				if notify {
					f.start(t)
				} else {
					delete(f.env, "NOTIFY_SOCKET")
					started := make(chan struct{}, 1)
					f.p.Sink = lifeSink(func(ctx context.Context, event telemetry.Event) error {
						err := f.capture.Deliver(ctx, event)
						if event.Name == "service.started" {
							started <- struct{}{}
						}
						return err
					})
					ctx, cancel := context.WithCancelCause(context.Background())
					f.cancel = cancel
					t.Cleanup(func() { cancel(context.Canceled) })
					go func() { f.done <- cli.Run(ctx, f.p) }()
					select {
					case <-started:
					case <-time.After(3 * time.Second):
						t.Fatal("service.started did not arrive")
					}
				}
				for _, path := range []string{"state", "cache/sites"} {
					info, err := os.Stat(filepath.Join(f.p.Dir, path))
					if err != nil || !info.IsDir() {
						t.Fatal(path, info, err)
					}
				}
				info, err := os.Stat(filepath.Join(f.p.Dir, "state/sites.db"))
				if err != nil || !info.Mode().IsRegular() {
					t.Fatal(info, err)
				}
				entries, err := os.ReadDir(filepath.Join(f.p.Dir, "cache/sites"))
				if err != nil || len(entries) != 0 {
					t.Fatal(entries, err)
				}
				var actual bytes.Buffer
				if err := db.Status(context.Background(), statusConfig(f.p.Dir), &actual); err != nil {
					t.Fatal(err)
				}
				if actual.String() != expected.String() {
					t.Fatalf("status=%q want=%q", actual.String(), expected.String())
				}
				result := f.call(t, "list", "{}")
				if records, ok := result["sites"].([]any); !ok || len(records) != 0 {
					t.Fatal(result)
				}
				if code := f.stop(t); code != cli.ExitSuccess || f.out.text() != "" || f.err.text() != "" {
					t.Fatal(code, f.out.text(), f.err.text())
				}
			})
		}
	}
}

// R-WJBB-CXDT R-WKJ7-QP4I
func TestRunUnknownCatalogVersionWarnsAndServes(t *testing.T) {
	for _, notify := range []bool{true, false} {
		t.Run(map[bool]string{true: "notify", false: "no-notify"}[notify], func(t *testing.T) {
			f := newStartFixture(t)
			cfg := statusConfig(f.p.Dir)
			cfg.Now = f.p.Now
			h, err := db.Open(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			catalog := store.New(h, store.Config{Now: f.p.Now, Rand: f.p.Rand})
			site, err := catalog.Create(context.Background(), store.Draft{Owner: "owner", Name: "restored", Repo: "rep_0123456789abcdef", Ref: "main", Visibility: store.Public, Listed: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = catalog.SetApex(context.Background(), site.ID); err != nil {
				t.Fatal(err)
			}
			if err = h.Write(context.Background(), func(tx *sql.Tx) error {
				_, e := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(9999, 'future')")
				return e
			}); err != nil {
				t.Fatal(err)
			}
			if err = h.Close(); err != nil {
				t.Fatal(err)
			}
			var before, warning bytes.Buffer
			if err = db.Status(context.Background(), cfg, &before); err != nil {
				t.Fatal(err)
			}
			cfg.Service = pages.ServiceName
			cfg.Stderr = &warning
			h, err = db.Open(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = h.Close(); err != nil {
				t.Fatal(err)
			}
			if warning.Len() == 0 {
				t.Fatal("missing reference warning")
			}
			f.p.Stderr = startupWarningWriter{write: func(data []byte) (int, error) {
				startNoNotification(t, f)
				if len(f.capture.Events()) != 0 {
					t.Error("warning followed a lifecycle event")
				}
				return f.err.Write(data)
			}}
			started := make(chan struct{}, 1)
			f.p.Sink = lifeSink(func(ctx context.Context, event telemetry.Event) error {
				if event.Name == "service.started" {
					if f.err.text() != warning.String() {
						t.Error("started preceded warning")
					}
				}
				err := f.capture.Deliver(ctx, event)
				if event.Name == "service.started" {
					started <- struct{}{}
				}
				return err
			})
			if !notify {
				delete(f.env, "NOTIFY_SOCKET")
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			f.cancel = cancel
			t.Cleanup(func() { cancel(context.Canceled) })
			go func() { f.done <- cli.Run(ctx, f.p) }()

			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("missing started event")
			}
			if notify {
				if data, err := startDatagram(f.notify); err != nil || data != "READY=1" {
					t.Fatal("readiness", data, err)
				}
				startNoNotification(t, f)
			}

			if events := f.capture.Events(); len(events) != 1 || events[0].Name != "service.started" {
				t.Fatal(events)
			}
			result, err := f.mcp.CallTool(context.Background(), identity.Caller{UserID: "owner"}, "apex", nil)
			if err != nil || result.IsError() {
				t.Fatal(err, result)
			}
			var content map[string]any
			if err = json.Unmarshal(startStructured(t, result), &content); err != nil {
				t.Fatal(err)
			}
			if content["apex"].(map[string]any)["id"] != site.ID {
				t.Fatal(content)
			}
			if code := f.stop(t); code != cli.ExitSuccess || f.err.text() != warning.String() || f.err.calls != 1 || f.out.text() != "" {
				t.Fatal(code, f.err.text())
			}
			var after bytes.Buffer
			if err = db.Status(context.Background(), cfg, &after); err != nil || after.String() != before.String() {
				t.Fatal(err, after.String(), before.String())
			}
		})
	}
}

type startupWarningWriter struct{ write func([]byte) (int, error) }

func (w startupWarningWriter) Write(data []byte) (int, error) { return w.write(data) }
