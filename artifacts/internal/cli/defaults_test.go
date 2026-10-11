package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/artifacts/internal/cli"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
)

// R-XO1A-6QM3: nil Unsetenv, empty Dir and injected clock use.
func TestRunDefaultDirectoryAndInjectedClock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	r := newRun(t, "")
	r.p.Unsetenv = nil
	initial := r.p.Now()
	r.start()
	status, body := r.request("PUT", "/files/clock.bin", "owner", strings.NewReader("first"), map[string]string{files.ExpiresHeader: initial.Add(time.Hour).Format(time.RFC3339)})
	expectStatus(t, status, 201)
	var first files.Metadata
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatal(err)
	}
	if first.Created != initial.Format(time.RFC3339) || first.Modified != initial.Format(time.RFC3339) {
		t.Fatalf("initial clock %#v", first)
	}
	later := initial.Add(time.Minute)
	r.now.Store(later.Unix())
	status, body = r.request("PUT", "/files/clock.bin", "owner", strings.NewReader("replacement"), nil)
	expectStatus(t, status, 200)
	var second files.Metadata
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatal(err)
	}
	if second.Created != first.Created || second.Modified != later.Format(time.RFC3339) {
		t.Fatalf("replacement clock %#v", second)
	}
	status, body = r.request("PUT", "/files/refused.bin", "owner", strings.NewReader("refused"), map[string]string{files.ExpiresHeader: later.Format(time.RFC3339)})
	expectStatus(t, status, 400)
	if string(body) != files.PastExpiry+"\n" {
		t.Fatalf("expiry comparison %q", body)
	}
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	if !reflect.DeepEqual(r.env, map[string]string{"LISTEN_PID": "321", "LISTEN_FDS": "1", "NOTIFY_SOCKET": r.notify.LocalAddr().String()}) || len(r.unset) != 0 {
		t.Fatalf("nil Unsetenv changed environment %v %v", r.env, r.unset)
	}
	info, err := os.Stat("state/artifacts.db")
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("cwd database %v %v", info, err)
	}
	for _, event := range r.trail.Events() {
		want := later
		if event.Name == "service.started" || event.Name == "artifact.created" {
			want = initial
		}
		// The first request's wrapper events also use the initial clock.
		if event.Time != initial && event.Time != later {
			t.Errorf("telemetry clock %#v", event)
		}
		if event.Name == "artifact.created" || event.Name == "artifact.replaced" || event.Name == "service.stopping" {
			if !event.Time.Equal(want) {
				t.Errorf("event stamp %#v want %v", event, want)
			}
		}
	}
	bus := r.bus.Events()
	if len(bus) != 2 || !bus[0].Time.Equal(initial) || !bus[1].Time.Equal(later) {
		t.Fatalf("bus clocks %#v", bus)
	}
}

// R-XO1A-6QM3: import expiry and metadata also use the supplied clock.
func TestRunImportInjectedClock(t *testing.T) {
	path, url := serveSibling(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("imported body")) }))
	r := newRun(t, t.TempDir())
	r.env["IKIGENBA_SERVICES"] = path
	r.start()
	now := r.p.Now()
	result := r.call("import", map[string]any{"path": "expired.bin", "url": "https://scripts.space.test/output", "expires": now.Format(time.RFC3339)})
	if !result.IsError() || resultText(t, result) != tools.PastExpiry(now.Format(time.RFC3339)) {
		t.Fatalf("expiry equal to injected clock accepted: %v", result)
	}
	result = r.call("import", map[string]any{"path": "future.bin", "url": url, "expires": now.Add(time.Minute).Format(time.RFC3339)})
	if result.IsError() {
		t.Fatalf("future expiry refused %s", resultText(t, result))
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Metadata files.Metadata `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Metadata.Created != now.Format(time.RFC3339) || answer.Metadata.Modified != now.Format(time.RFC3339) {
		t.Fatalf("import clock %#v", answer.Metadata)
	}
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	for _, event := range r.trail.Events() {
		if !event.Time.Equal(now) {
			t.Errorf("import telemetry clock %#v", event)
		}
	}
	bus := r.bus.Events()
	if len(bus) != 1 || !bus[0].Time.Equal(now) {
		t.Fatalf("import bus clock %#v", bus)
	}
}

// R-XO1A-6QM3: startup and subsequent sweep comparisons use Now,
// independently of the timestamp delivered by the injected timer.
func TestRunInjectedSweepClock(t *testing.T) {
	dir := t.TempDir()
	r := newRun(t, dir)
	initial := r.p.Now()
	seedRun(t, dir, initial.Add(-time.Hour), func(_ *db.DB, st *store.Store) {
		for _, expiry := range []struct {
			path string
			at   time.Time
		}{{"expired.bin", initial}, {"future.bin", initial.Add(time.Minute)}} {
			_, _, err := st.Put(context.Background(), store.Draft{Path: expiry.path, OwnerID: "owner", OwnerEmail: "owner@example.test", Expires: expiry.at, Max: 99}, strings.NewReader(expiry.path))
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	r.start()
	status, _ := r.request("GET", "/files/expired.bin", "owner", nil, nil)
	expectStatus(t, status, 404)
	status, _ = r.request("GET", "/files/future.bin", "owner", nil, nil)
	expectStatus(t, status, 200)
	wait := readWait(t, r.waits)
	later := initial.Add(time.Minute)
	r.now.Store(later.Unix())
	wait.fire <- initial.Add(-time.Hour)
	_ = readWait(t, r.waits)
	status, _ = r.request("GET", "/files/future.bin", "owner", nil, nil)
	expectStatus(t, status, 404)
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	for _, event := range r.trail.Events() {
		want := later
		if event.Name == "service.started" || event.Name == "artifact.deleted" && event.Attrs["path"] == "expired.bin" {
			want = initial
		}
		if event.Name == "service.started" || event.Name == "artifact.deleted" || event.Name == "service.stopping" {
			if !event.Time.Equal(want) {
				t.Errorf("sweep telemetry clock %#v want %v", event, want)
			}
		}
	}
	bus := r.bus.Events()
	if len(bus) != 2 || !bus[0].Time.Equal(initial) || !bus[1].Time.Equal(later) {
		t.Fatalf("sweep bus clocks %#v", bus)
	}
}
