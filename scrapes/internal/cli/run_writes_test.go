package cli_test

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-LEN6-PKUY R-LH2Z-H4CC
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
	prefix := "scrapes: " + id + " PutRows: " + runs.Retrying + ": "
	if f.Out.String() != "" || f.Err.writes != 1 || !strings.HasPrefix(f.Err.String(), prefix) || strings.Count(f.Err.String(), "\n") != 1 {
		t.Fatalf("streams stdout %q stderr %q writes %d", f.Out.String(), f.Err.String(), f.Err.writes)
	}
}

// R-LH2Z-H4CC
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
