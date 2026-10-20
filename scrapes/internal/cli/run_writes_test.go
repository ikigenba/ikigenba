package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/pace"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

// R-3O6V-KQIB
func TestRunForwardsWriteRetryPauseAndDiagnostic(t *testing.T) {
	pageEntered, answerPage := make(chan struct{}), make(chan struct{})
	var first sync.Once
	src := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		first.Do(func() { close(pageEntered) })
		select {
		case <-answerPage:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return jsonResponse(r, []any{map[string]string{"dot_number": "write-fixture", "phy_state": "MN"}}), nil
	}}
	releaseLock, lockDone := make(chan struct{}), make(chan error, 1)
	paused := make(chan time.Duration, 2)
	var release sync.Once
	defer release.Do(func() { close(releaseLock) })
	f := startRun(t, runOptions{Source: src, Env: map[string]string{"FMCSA_STATES": "MN"}, Modify: func(p *cli.Process) {
		p.Sleep = func(_ context.Context, d time.Duration) {
			if d != runs.WriteBackoff {
				return
			}
			paused <- d
			release.Do(func() { close(releaseLock) })
			if err := <-lockDone; err != nil {
				t.Errorf("lock transaction: %v", err)
			}
		}
	}})
	id := runID(t, startScrape(t, f, fixtureCaller))
	select {
	case <-pageEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("source page was not requested")
	}
	_, d := openStore(t, f.Dir)
	locked := make(chan struct{})
	go func() {
		lockDone <- d.Write(context.Background(), func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(context.Background(), `UPDATE runs SET rows = rows WHERE id = ?`, id); err != nil {
				return err
			}
			close(locked)
			<-releaseLock
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-lockDone:
		t.Fatalf("could not hold write lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("write lock was not acquired")
	}
	close(answerPage)
	select {
	case duration := <-paused:
		if duration != runs.WriteBackoff {
			t.Fatalf("pause %v, want %v", duration, runs.WriteBackoff)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("write retry did not pause, stderr %q", f.Err.String())
	}
	if ended := f.awaitRun(id); ended["status"] != store.StatusDone || ended["rows"] != float64(1) {
		t.Fatalf("run ending %v", ended)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	select {
	case duration := <-paused:
		t.Fatalf("extra pause %v", duration)
	default:
	}
	prefix := "scrapes: " + id + " FinishRun: " + runs.Retrying + ": "
	if f.Out.String() != "" || f.Err.writes != 1 || !strings.HasPrefix(f.Err.String(), prefix) || strings.Count(f.Err.String(), "\n") != 1 {
		t.Fatalf("streams stdout %q stderr %q writes %d", f.Out.String(), f.Err.String(), f.Err.writes)
	}
}

// R-Q1HM-S9IY
func TestRunSourceFailureWritesNoDiagnostic(t *testing.T) {
	src := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture failure")), Request: r}, nil
	}}
	f := startRun(t, runOptions{Source: src, Modify: func(p *cli.Process) {
		p.Sleep = func(context.Context, time.Duration) {}
	}})
	id := runID(t, startScrape(t, f, fixtureCaller))
	if ended := f.awaitRun(id); ended["status"] != store.StatusFailed || ended["reason"] != store.ReasonSource {
		t.Fatalf("run ending %v", ended)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	if f.Out.String() != "" || f.Err.String() != "" {
		t.Fatalf("streams stdout %q stderr %q", f.Out.String(), f.Err.String())
	}
}

// R-PU68-HN2S
func TestRunForwardsWriteBudget(t *testing.T) {
	for _, budget := range []int64{1, 2, 1000} {
		t.Run(strconv.FormatInt(budget, 10), func(t *testing.T) {
			f := startRun(t, runOptions{Env: map[string]string{"SCRAPE_WRITE_BUDGET": strconv.FormatInt(budget, 10)}})
			id := runID(t, startScrape(t, f, fixtureCaller))
			if ended := f.awaitRun(id); ended["status"] != store.StatusDone {
				t.Fatal(ended)
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop failed")
			}
			found := map[string]bool{}
			positive := false
			for _, event := range f.Telemetry.Events() {
				if event.Name != trail.Walked && event.Name != trail.Loaded {
					continue
				}
				if event.Attrs["run"] != id {
					t.Fatalf("phase run %v", event)
				}
				bytes, bok := event.Attrs["bytes"].(int64)
				paused, pok := event.Attrs["paused_us"].(int64)
				if !bok || !pok || paused != bytes*(1000000/budget) {
					t.Fatalf("phase pacing %v", event)
				}
				positive = positive || bytes > 0
				found[event.Name] = true
			}
			if !found[trail.Walked] || !found[trail.Loaded] || !positive {
				t.Fatalf("phases %v positive %v", found, positive)
			}
		})
	}
}

// R-5Q72-TH9W R-43X0-ALDD
func TestRunForwardsPressureLimit(t *testing.T) {
	const pressureFile = cli.PressureFile
	if pressureFile != "/proc/pressure/io" {
		t.Fatal(pressureFile)
	}
	for _, limit := range []string{"", "40", "50", "60"} {
		t.Run("limit"+limit, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pressure")
			write := func(value string) {
				t.Helper()
				data := "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=" + value + " avg60=0.00 avg300=0.00 total=0\n"
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("50.00")
			var polls atomic.Int32
			var f *runFixture
			f = startRun(t, runOptions{Env: map[string]string{"SCRAPE_PRESSURE_LIMIT": limit}, Modify: func(p *cli.Process) {
				p.Pressure = path
				p.Sleep = func(_ context.Context, d time.Duration) {
					if d != pace.PressurePoll {
						return
					}
					n := polls.Add(1)
					st, _ := openStore(t, f.Dir)
					rs, err := st.ListRunsAfter(context.Background(), "fmcsa", "", "", 10)
					if err != nil || len(rs) != 1 || rs[0].Status != store.StatusRunning {
						t.Errorf("pressure running %v: %v", rs, err)
					}
					if n == 4 {
						write("0.00")
					}
				}
			}})
			id := runID(t, startScrape(t, f, fixtureCaller))
			if ended := f.awaitRun(id); ended["status"] != store.StatusDone {
				t.Fatal(ended)
			}
			want := int32(0)
			if limit == "" || limit == "40" {
				want = 4
			}
			if polls.Load() != want {
				t.Fatalf("polls %d, want %d", polls.Load(), want)
			}
		})
	}
}

// R-X13B-6JX7 R-Q1HM-S9IY
func TestRunMissingRowsKeepCatalogReadable(t *testing.T) {
	f := startRun(t, runOptions{})
	id := runID(t, startScrape(t, f, fixtureCaller))
	if done := f.awaitRun(id); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	if err := os.Remove(filepath.Join(f.Dir, "runs", id+".db")); err != nil {
		t.Fatal(err)
	}
	resp := f.request("GET", "/runs/"+id+"/rows", "", true)
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 503 || string(body) != store.Missing+"\n" {
		t.Fatalf("missing rows %d %q: %v", resp.StatusCode, body, err)
	}
	for _, path := range []string{"/runs/" + id + "/record", "/scrapers/fmcsa/current"} {
		resp = f.request("GET", path, "", true)
		var record map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&record); err != nil || resp.StatusCode != 200 || record["id"] != id {
			t.Fatalf("catalog %s %d %v: %v", path, resp.StatusCode, record, err)
		}
	}
	if f.stop() != cli.ExitSuccess || f.Out.String() != "" || f.Err.String() != "" {
		t.Fatalf("streams %q %q", f.Out.String(), f.Err.String())
	}
}
