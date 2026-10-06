package cli_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

// R-UVGS-T2CG
func TestServeAppliesEmbeddedMigrationsBeforeReady(t *testing.T) {
	for _, kind := range []string{"absent", "root-only", "zero", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			f := newServeFixture(t)
			cfg := statusConfig(f.dir)
			cfg.Now = f.p.Now
			if kind != "absent" {
				if err := os.MkdirAll(filepath.Join(f.dir, "state", "repos"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "zero" {
				if err := os.WriteFile(cfg.Path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "legacy" {
				// Create a catalog and repository, then restore the predecessor's migration-free stored form.
				f.start(t)
				f.tool(t, "create", `{"name":"legacy"}`)
				f.stop(t, "prepare legacy", cli.ExitSuccess)
				d, err := db.Open(t.Context(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				err = d.Write(t.Context(), func(tx *sql.Tx) error {
					_, err := tx.Exec("DROP TABLE schema_migrations; PRAGMA user_version=0")
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
				f = newServeFixture(t, f.gitPath)
				f.dir = filepath.Dir(filepath.Dir(cfg.Path))
				f.p.Dir = f.dir
			}
			expectedCfg := statusConfig(t.TempDir())
			expectedCfg.Now = f.p.Now
			statusFixture(t, expectedCfg, "")
			var expected bytes.Buffer
			if err := db.Status(t.Context(), expectedCfg, &expected); err != nil {
				t.Fatal(err)
			}
			f.start(t)
			var got bytes.Buffer
			if err := db.Status(t.Context(), statusConfig(f.dir), &got); err != nil {
				t.Fatal(err)
			}
			if got.String() != expected.String() {
				t.Fatalf("migration status at ready %q want %q", got.String(), expected.String())
			}
			if kind == "legacy" {
				rows := f.tool(t, "list", `{}`)["repos"].([]any)
				if len(rows) != 1 || rows[0].(map[string]any)["name"] != "legacy" {
					t.Fatalf("legacy rows %v", rows)
				}
			}
			f.stop(t, "migration stop", cli.ExitSuccess)
		})
	}
}

// R-YB4H-4PUF
func TestServeRefusesInvalidAndNewerCatalogBeforeEffects(t *testing.T) {
	for _, kind := range []string{"invalid", "unknown", "newline-parent"} {
		t.Run(kind, func(t *testing.T) {
			f := newServeFixture(t)
			if kind == "newline-parent" {
				f.dir = filepath.Join(f.dir, "line\nbreak")
				f.p.Dir = f.dir
				if err := os.Mkdir(f.dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := statusConfig(f.dir)
			cfg.Now = f.p.Now
			switch kind {
			case "unknown":
				statusFixture(t, cfg, "INSERT INTO schema_migrations(version,applied_at) VALUES(9999,'future')")
			case "invalid":
				if err := os.Mkdir(filepath.Dir(cfg.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Path, []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "newline-parent":
				if err := os.WriteFile(filepath.Join(f.dir, "state"), []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, expectedErr := db.Open(t.Context(), cfg)
			if expectedErr == nil {
				t.Fatal("fixture did not fail")
			}
			f.notification(t)
			f.listen(t)
			if code := cli.Run(t.Context(), f.p); code != cli.ExitServerFailed {
				t.Fatalf("exit %d", code)
			}
			want := "repos: cannot open database state/repos.db: " + strings.ReplaceAll(expectedErr.Error(), "\n", " ") + "\n"
			if f.stdout.text() != "" || f.stderr.text() != want || len(f.stderr.lines()) != 1 || f.mcpCalls.Load() != 0 || f.bannerCalls.Load() != 0 || len(f.capture.Events()) != 0 {
				t.Fatalf("failure effects stdout=%q stderr=%q", f.stdout.text(), f.stderr.text())
			}
			noServeNotification(t, f.notify)
			if kind == "invalid" {
				b, err := os.ReadFile(cfg.Path)
				if err != nil || string(b) != "not a database" {
					t.Fatalf("fixture changed %q %v", b, err)
				}
			}
		})
	}
}
