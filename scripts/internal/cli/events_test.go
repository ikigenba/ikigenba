package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func deliveryBody(t *testing.T, h *runHarness, name, id string) []byte {
	t.Helper()
	ev := events.Event{ID: id, Time: h.now, Service: "repos", Name: name, RequestID: "producer-request", User: "producer-user", Attrs: events.Attrs{}, Seq: 1, Received: h.now}
	b, e := json.Marshal(ev)
	mustCLI(t, e)
	var fields map[string]json.RawMessage
	mustCLI(t, json.Unmarshal(b, &fields))
	fields["attempt"] = json.RawMessage("1")
	b, e = json.Marshal(fields)
	mustCLI(t, e)
	return b
}

func postDelivery(t *testing.T, h *runHarness, id string, body []byte, outcome string) {
	t.Helper()
	req, e := http.NewRequest(http.MethodPost, "http://"+h.listener.Addr().String()+events.EventsPath, bytes.NewReader(body))
	mustCLI(t, e)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", id)
	res, e := h.http.Do(req)
	mustCLI(t, e)
	b, e := io.ReadAll(res.Body)
	mustCLI(t, e)
	mustCLI(t, res.Body.Close())
	var got map[string]string
	mustCLI(t, json.Unmarshal(b, &got))
	if res.StatusCode != 200 || !reflect.DeepEqual(got, map[string]string{"outcome": outcome}) {
		t.Fatalf("delivery %d %s", res.StatusCode, b)
	}
}

// R-SXL7-W9R6 R-TKPN-93VQ R-GVWQ-3EYJ
func TestDeliveryTrailOrigins(t *testing.T) {
	h := newHarness(t)
	h.repository("pass\n")
	h.p.Sink = &h.sink.capture
	h.start()
	h.create("subscriber")
	h.call("subscribe", map[string]any{"name": "subscriber", "event": "repo.pushed"})
	body := deliveryBody(t, h, "repo.pushed", "evt_0123456789abcdef")
	postDelivery(t, h, "delivery-origin", body, "ok")
	history := h.call("runs", map[string]any{"name": "subscriber"})["runs"].([]any)
	if len(history) != 1 {
		t.Fatal(history)
	}
	run := history[0].(map[string]any)["id"].(string)
	waitCredentialRun(t, h, run, "exited")
	postDelivery(t, h, "delivery-repeat", body, "ok")
	postDelivery(t, h, "delivery-irrelevant", deliveryBody(t, h, "repo.created", "evt_fedcba9876543210"), "skip")
	h.call("delete", map[string]any{"name": "subscriber"})
	h.stop()
	es := h.sink.capture.Events()
	window := trailWindow(t, es, "delivery-origin")
	start := trailEvent(t, window, "run.started", run)
	if start.RequestID != "delivery-origin" || start.User != "owner" {
		t.Fatal(start)
	}
	trailEvent(t, es, "run.finished", run)
	starts, ends := 0, 0
	for _, e := range es {
		if e.Name == "run.started" || e.Name == "run.finished" {
			if e.Attrs["run"] != run || e.RequestID != "delivery-origin" || e.User != "owner" {
				t.Fatalf("unrelated run event %#v", e)
			}
			if e.Name == "run.started" {
				starts++
			} else {
				ends++
			}
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("run events %d %d", starts, ends)
	}
	for _, id := range []string{"delivery-origin", "delivery-repeat", "delivery-irrelevant"} {
		trailWindow(t, es, id)
		for _, e := range es {
			if e.RequestID == id && (e.Name == "tool.called" || (id != "delivery-origin" && domain(e))) {
				t.Fatal(e)
			}
		}
	}

}

// R-TLXJ-MVMF R-GTGX-BVH5 R-GUOT-PN7U
func TestDeliveryTrailRetainsCatalogIdentityAcrossQueueAndEndings(t *testing.T) {
	for _, mode := range []string{"immediate", "queued", "cancel", "queued-cancel", "drain", "queued-drain", "missing"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			h.repository(waitingMain)
			h.set("RUN_MAX_ACTIVE", "1")
			h.set("DRAIN_SECONDS", "1")
			h.p.Sink = &h.sink.capture
			h.start()
			sc := h.create("subscriber")
			for _, pattern := range []string{"cron.*.fired", "cron.hourly.fired"} {
				h.call("subscribe", map[string]any{"name": "subscriber", "event": pattern})
			}
			var active map[string]any
			queued := mode == "queued" || mode == "queued-cancel" || mode == "queued-drain"
			if queued {
				active = h.call("run", map[string]any{"name": "subscriber"})
			}
			if mode == "missing" {
				h.call("update", map[string]any{"name": "subscriber", "ref": "missing"})
			}
			postDelivery(t, h, "delivery-catalog-origin", deliveryBody(t, h, "cron.hourly.fired", "evt_0123456789abcdef"), "ok")
			handle, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, e)
			defer func() { mustCLI(t, handle.Close()) }()
			st := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
			records, e := st.Runs(context.Background(), sc["id"].(string))
			mustCLI(t, e)
			var r store.Run
			eventRuns := map[string]bool{}
			for _, record := range records {
				if record.Event == "evt_0123456789abcdef" {
					r = record
					eventRuns[record.ID] = true
				}
			}
			if len(eventRuns) != 1 || r.ID == "" || r.RequestID != "delivery-catalog-origin" || r.User != "owner" || r.Trigger != store.TriggerEvent {
				t.Fatal(r)
			}
			if queued && r.Status != store.StatusQueued {
				t.Fatal(r)
			}
			switch mode {
			case "immediate":
				releaseQueued(t, h, sc["id"], r.ID)
				waitCredentialRun(t, h, r.ID, "exited")
			case "queued":
				releaseQueued(t, h, sc["id"], active["id"])
				deadline := time.NewTimer(10 * time.Second)
				for {
					next, e := st.RunByID(context.Background(), r.ID)
					mustCLI(t, e)
					if next.Status == store.StatusRunning {
						break
					}
					select {
					case <-deadline.C:
						t.Fatal("event run was not promoted")
					default:
					}
				}
				deadline.Stop()
				releaseQueued(t, h, sc["id"], r.ID)
				waitCredentialRun(t, h, r.ID, "exited")
			case "cancel", "queued-cancel":
				h.call("cancel", map[string]any{"run": r.ID})
				if queued {
					releaseQueued(t, h, sc["id"], active["id"])
					waitCredentialRun(t, h, active["id"].(string), "exited")
				}
			}
			h.stop()
			r, e = st.RunByID(context.Background(), r.ID)
			mustCLI(t, e)
			es := h.sink.capture.Events()
			for _, line := range h.stderr.snapshot() {
				if strings.HasPrefix(string(line), "scripts: undelivered event: ") {
					var ev struct {
						Name      string          `json:"event"`
						RequestID string          `json:"request_id"`
						User      string          `json:"user"`
						Attrs     telemetry.Attrs `json:"attrs"`
					}
					mustCLI(t, json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(string(line)), "scripts: undelivered event: ")), &ev))
					es = append(es, telemetry.Event{Name: ev.Name, RequestID: ev.RequestID, User: ev.User, Attrs: ev.Attrs})
				}
			}
			end := trailEvent(t, es, "run.finished", r.ID)
			if end.RequestID != r.RequestID || end.User != r.User {
				t.Fatal(end, r)
			}
			starts := 0
			for _, ev := range es {
				id, _ := ev.Attrs["run"].(string)
				if ev.Name == "run.started" && eventRuns[id] {
					starts++
					if ev.RequestID != r.RequestID || ev.User != r.User {
						t.Fatal(ev, r)
					}
					expectAttrs(t, ev, telemetry.Attrs{"run": r.ID, "script": r.Script, "sha": r.SHA, "trigger": "event"})
				}
			}
			wantStarts := 1
			if mode == "missing" || mode == "queued-cancel" || mode == "queued-drain" {
				wantStarts = 0
			}
			if starts != wantStarts {
				t.Fatal("start count", starts, wantStarts)
			}
			if mode == "missing" {
				failed := trailEvent(t, trailWindow(t, es, r.RequestID), "run.finished", r.ID)
				if r.Status != store.StatusFailed || r.Reason != store.ReasonCommitMissing || failed.Attrs["status"] != r.Status || failed.Attrs["reason"] != r.Reason {
					t.Fatal(failed, r)
				}
			}
			if wantStarts == 1 && !queued {
				trailEvent(t, trailWindow(t, es, r.RequestID), "run.started", r.ID)
			}
		})
	}
}

// R-6Z8T-HIWW
func TestEventFinishingBodyDuringDrainIsRefused(t *testing.T) {
	h := newHarness(t)
	h.set("DRAIN_SECONDS", "30")
	h.repository(waitingMain)
	h.start()
	sc := h.create("subscriber")
	h.call("subscribe", map[string]any{"name": "subscriber", "event": "repo.pushed"})
	run := h.call("run", map[string]any{"name": "subscriber"})
	h.event("run.started")
	before := h.call("runs", map[string]any{"name": "subscriber"})["runs"]
	folder := filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string))
	entries, e := os.ReadDir(folder)
	mustCLI(t, e)
	body := deliveryBody(t, h, "repo.pushed", "evt_0123456789abcdef")
	c, e := net.Dial("tcp", h.listener.Addr().String())
	mustCLI(t, e)
	defer func() { _ = c.Close() }()
	mustCLI(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	_, e = fmt.Fprintf(c, "POST /events HTTP/1.1\r\nHost: backend\r\nX-Request-Id: delayed-event\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(body))
	mustCLI(t, e)
	for started := h.event("request.started"); started.RequestID != "delayed-event"; started = h.event("request.started") {
		t.Logf("Drained earlier request notification: %s", started.RequestID)
	}
	h.cancel(errors.New("delayed event"))
	connectionDeadline(t, h.listener.Addr().String())
	_, e = c.Write(body)
	mustCLI(t, e)
	res, e := http.ReadResponse(bufio.NewReader(c), nil)
	mustCLI(t, e)
	b, e := io.ReadAll(res.Body)
	mustCLI(t, e)
	mustCLI(t, res.Body.Close())
	var got map[string]string
	mustCLI(t, json.Unmarshal(b, &got))
	if res.StatusCode != 500 || !reflect.DeepEqual(got, map[string]string{"outcome": "error", "error": "scripts is stopping; try again later"}) {
		t.Fatalf("drain delivery %d %s", res.StatusCode, b)
	}
	handle, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	records, e := st.Runs(context.Background(), sc["id"].(string))
	mustCLI(t, e)
	mustCLI(t, handle.Close())
	if len(records) != len(before.([]any)) || len(records) != 1 || records[0].ID != run["id"] {
		t.Fatalf("late delivery records %v", records)
	}
	after, e := os.ReadDir(folder)
	mustCLI(t, e)
	if len(after) != len(entries) || len(after) != 1 || after[0].Name() != entries[0].Name() {
		t.Fatalf("late delivery folders %v", after)
	}
	mustCLI(t, os.WriteFile(filepath.Join(folder, run["id"].(string), "out", "release"), nil, 0600))
	if code := h.finish(); code != cli.ExitSuccess {
		t.Fatalf("stop %d %s", code, h.stderr.String())
	}
}

// R-GVWQ-3EYJ
func TestRefusedDeliveryHasNoDomainTrail(t *testing.T) {
	h := newHarness(t)
	h.repository("pass\n")
	h.p.Cgroup = ""
	h.p.Sink = &h.sink.capture
	h.start()
	h.create("subscriber")
	h.call("subscribe", map[string]any{"name": "subscriber", "event": "cron.*.fired"})
	req, err := http.NewRequest(http.MethodPost, "http://"+h.listener.Addr().String()+events.EventsPath, bytes.NewReader(deliveryBody(t, h, "cron.hourly.fired", "evt_0123456789abcdef")))
	mustCLI(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "delivery-refused")
	res, err := h.http.Do(req)
	mustCLI(t, err)
	body, err := io.ReadAll(res.Body)
	mustCLI(t, err)
	mustCLI(t, res.Body.Close())
	var got map[string]string
	mustCLI(t, json.Unmarshal(body, &got))
	if res.StatusCode != 500 || got["outcome"] != "error" || !strings.HasPrefix(got["error"], "runs are unavailable: ") {
		t.Fatal(res.StatusCode, got)
	}
	if history := h.call("runs", map[string]any{"name": "subscriber"})["runs"].([]any); len(history) != 0 {
		t.Fatal(history)
	}
	h.stop()
	es := h.sink.capture.Events()
	trailWindow(t, es, "delivery-refused")
	for _, e := range es {
		if e.RequestID == "delivery-refused" && (e.Name == "tool.called" || domain(e)) {
			t.Fatal(e)
		}
	}
}
