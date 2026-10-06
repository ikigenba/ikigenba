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
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
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

// R-SXL7-W9R6 R-SV5F-4Q9S
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
	for _, id := range []string{"delivery-repeat", "delivery-irrelevant"} {
		for _, e := range trailWindow(t, es, id) {
			if e.RequestID == id && domain(e) {
				t.Fatal(e)
			}
		}
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
