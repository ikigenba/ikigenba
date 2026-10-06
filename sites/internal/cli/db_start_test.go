package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
)

// R-WG3Q-IOBR R-XKCF-53SO R-XU3M-79Q8
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
						if event.Name == "service.started" {
							started <- struct{}{}
						}
						return f.capture.Deliver(ctx, event)
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

// R-XJ4I-RC1Z
func TestRunUnknownCatalogVersionRefusal(t *testing.T) {
	f := newStartFixture(t)
	cfg := statusConfig(f.p.Dir)
	cfg.Now = f.p.Now
	statusFixture(t, cfg, "INSERT INTO schema_migrations(version, applied_at) VALUES(9999, 'future')")
	handle, openingErr := db.Open(context.Background(), cfg)
	if handle != nil {
		_ = handle.Close()
	}
	if openingErr == nil || !strings.Contains(openingErr.Error(), "9999") {
		t.Fatal("unknown fixture did not fail", openingErr)
	}
	check := startGuardRefusal(t, f)
	defer check()
	code := cli.Run(context.Background(), f.p)
	expected := "sites: cannot open database state/sites.db: " + strings.ReplaceAll(openingErr.Error(), "\n", " ") + "\n"
	if code != cli.ExitServerFailed || f.err.text() != expected || f.err.calls != 1 || f.out.text() != "" || len(f.capture.Events()) != 0 {
		t.Fatal(code, f.err.text())
	}
	if _, err := os.Stat(filepath.Join(f.p.Dir, "cache")); !os.IsNotExist(err) {
		t.Fatal("cache created", err)
	}
}
