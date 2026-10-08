package cli_test

import (
	"bytes"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/web"
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

// R-KQ4P-PD9A
func TestServeRefusesInvalidCatalogBeforeEffects(t *testing.T) {
	for _, kind := range []string{"invalid", "newline-parent"} {
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

// R-KRCM-34ZZ R-KSKI-GWQO
func TestServeNewerCatalogWarnsBeforeReadyAndPreservesData(t *testing.T) {
	f := newServeFixture(t)
	cfg := statusConfig(f.dir)
	cfg.Now = f.p.Now
	d, err := db.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	g, err := git.Find(filepath.Dir(f.gitPath), f.p.Environ)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), d, store.Config{Root: filepath.Join(f.dir, "state", "repos"), Git: g, Now: f.p.Now, Rand: f.random})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := s.Create(t.Context(), "owner", "owned")
	if err != nil {
		t.Fatal(err)
	}
	damaged, err := s.Create(t.Context(), "owner", "damaged")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), "other", "private"); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(9999,'future')")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.dir, "state", "repos", damaged.ID+".git")); err != nil {
		t.Fatal(err)
	}
	var before bytes.Buffer
	if err := db.Status(t.Context(), cfg, &before); err != nil {
		t.Fatal(err)
	}
	var warning recordedWrites
	cfg.Service, cfg.Stderr = web.ServiceName, &warning
	reference, err := db.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := reference.Close(); err != nil {
		t.Fatal(err)
	}
	if warning.calls != 1 || warning.Len() == 0 {
		t.Fatalf("warning fixture %q in %d calls", warning.String(), warning.calls)
	}
	f.notification(t)
	f.p.Stderr = warningBeforeReady{t: t, notify: f.notify, output: f.stderr}
	f.listen(t)
	underlying := f.listener
	checked := make(chan struct{})
	var once sync.Once
	f.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveListener{Listener: underlying, accept: func() (net.Conn, error) {
			once.Do(func() {
				// The warning must already be complete when READY is received.
				f.ready(t)
				if f.stderr.text() != warning.String() || len(f.stderr.lines()) != 1 {
					t.Errorf("warning before ready %q in %v", f.stderr.text(), f.stderr.lines())
				}
				f.flush(t)
				events := f.capture.Events()
				if len(events) != 2 || events[0].Name != "repo.unavailable" || events[0].Attrs["repo"] != damaged.ID || events[1].Name != "service.started" {
					t.Errorf("startup trail %+v", events)
				}
				close(checked)
			})
			return underlying.Accept()
		}}, nil
	}
	f.launch(t)
	select {
	case <-checked:
	case <-time.After(5 * time.Second):
		t.Fatal("start did not complete")
	}
	rows := f.tool(t, "list", "{}")["repos"].([]any)
	got := map[string]string{}
	for _, row := range rows {
		r := row.(map[string]any)
		got[r["id"].(string)] = r["name"].(string)
	}
	if len(rows) != 2 || !reflect.DeepEqual(got, map[string]string{owned.ID: owned.Name, damaged.ID: damaged.Name}) {
		t.Fatalf("newer catalog rows %v", rows)
	}
	if status, _, _ := f.request(t, "/", "newer-page"); status != 200 {
		t.Fatalf("page status %d", status)
	}
	f.stop(t, "newer catalog stop", cli.ExitSuccess)
	if f.stdout.text() != "" || f.stderr.text() != warning.String() || len(f.stderr.lines()) != 1 {
		t.Fatalf("streams stdout %q stderr %q", f.stdout.text(), f.stderr.text())
	}
	var after bytes.Buffer
	if err := db.Status(t.Context(), cfg, &after); err != nil || after.String() != before.String() {
		t.Fatalf("status after %q before %q err %v", after.String(), before.String(), err)
	}
}

// warningBeforeReady observes the datagram queue during the warning Write.
type warningBeforeReady struct {
	t      *testing.T
	notify *net.UnixConn
	output *serveOutput
}

func (w warningBeforeReady) Write(p []byte) (int, error) {
	noServeNotification(w.t, w.notify)
	return w.output.Write(p)
}
