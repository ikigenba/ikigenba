package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

func trailCall(t *testing.T, f *serveFixture, id, name string, args any) mcp.Result {
	t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := f.client.CallTool(ctx, identity.Caller{UserID: "fixture-user", Email: "private@example.test", RequestID: id}, name, b)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func trailCreate(t *testing.T, f *serveFixture, id, name string) tools.Prompt {
	t.Helper()
	return decodeServe[tools.Prompt](t, trailCall(t, f, id, "create", map[string]any{"name": name, "model": serveChatModel(t), "prompt": "private prompt payload"}))
}
func trailRun(t *testing.T, f *serveFixture, id, name string) tools.Started {
	t.Helper()
	return decodeServe[tools.Started](t, trailCall(t, f, id, "run", map[string]any{"name": name, "input": map[string]any{"private": "input"}}))
}
func trailWait(t *testing.T, f *serveFixture, id string) {
	t.Helper()
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	for {
		for _, e := range f.capture.Events() {
			if e.Name == "run.finished" && e.Attrs["prompt_run"] == id {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("missing finish %s: %#v", id, f.capture.Events())
		default:
			runtime.Gosched()
		}
	}
}
func trailIndex(t *testing.T, es []telemetry.Event, name, id string) int {
	t.Helper()
	found := -1
	for i, e := range es {
		if e.Name == name && (e.Attrs["prompt_run"] == id || e.RequestID == id) {
			if found >= 0 {
				t.Fatalf("duplicate %s %s", name, id)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("missing %s %s: %#v", name, id, es)
	}
	return found
}
func trailWindow(t *testing.T, es []telemetry.Event, id string) (int, int) {
	t.Helper()
	a, b := trailIndex(t, es, "request.started", id), trailIndex(t, es, "request.finished", id)
	if b <= a {
		t.Fatal("reversed request window")
	}
	return a, b
}
func trailDomain(e telemetry.Event) bool {
	return strings.HasPrefix(e.Name, "prompt.") || strings.HasPrefix(e.Name, "run.")
}
func trailNoDomain(t *testing.T, es []telemetry.Event, id string) {
	t.Helper()
	trailWindow(t, es, id)
	for _, e := range es {
		if e.RequestID == id && trailDomain(e) {
			t.Fatalf("unexpected domain event for %s: %#v", id, e)
		}
	}
}
func trailPromptEvent(t *testing.T, es []telemetry.Event, id, name, prompt string) {
	t.Helper()
	a, b := trailWindow(t, es, id)
	count := 0
	for i, e := range es {
		if e.RequestID == id && trailDomain(e) {
			count++
			if e.Name != name || i <= a || i >= b || i >= trailIndex(t, es, "tool.called", id) || e.User != "fixture-user" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"prompt": prompt}) {
				t.Fatal(e)
			}
		}
	}
	if count != 1 {
		t.Fatal("domain count", count)
	}
}

// R-KYY6-UI0E R-L063-89R3 R-L1DZ-M1HS R-5VO1-KI12 R-MGLR-OEMY
func trailGeneral(t *testing.T, es []telemetry.Event) {
	t.Helper()
	allowed := map[string]bool{"service.started": true, "service.stopping": true, "request.started": true, "request.finished": true, "tool.called": true, "prompt.created": true, "prompt.updated": true, "prompt.deleted": true, "run.started": true, "run.finished": true}
	for _, e := range es {
		if !allowed[e.Name] {
			t.Fatal(e)
		}
		if trailDomain(e) && (e.Service != "prompts" || !e.Time.Equal(serveTime.UTC().Truncate(time.Microsecond))) {
			t.Fatal(e)
		}
		if v, ok := e.Attrs["prompt"]; ok {
			s, ok := v.(string)
			if !ok || !store.ValidPromptID(s) {
				t.Fatal(e)
			}
		}
		if v, ok := e.Attrs["prompt_run"]; ok {
			s, ok := v.(string)
			if !ok || !store.ValidRunID(s) {
				t.Fatal(e)
			}
		}
		if v, ok := e.Attrs["model"]; ok {
			s, ok := v.(string)
			if !ok || s == "" {
				t.Fatal(e)
			}
		}
		if strings.HasPrefix(e.Name, "prompt.") && len(e.Attrs) != 1 {
			t.Fatal(e)
		}
		if e.Name == "run.started" {
			if len(e.Attrs) != 4 {
				t.Fatal(e)
			}
		}
		if e.Name == "run.finished" {
			allowedKeys := map[string]bool{"prompt_run": true, "status": true, "duration_us": true, "truncated": true, "calls": true, "tool_calls": true, "input_tokens": true, "output_tokens": true, "cost_nanos": true, "exit_code": true, "reason": true}
			for k := range e.Attrs {
				if !allowedKeys[k] {
					t.Fatal(e)
				}
			}
		}
		if e.Name == "tool.called" {
			if len(e.Attrs) != 4 {
				t.Fatal(e)
			}
			for _, k := range []string{"tool", "kind", "outcome"} {
				if _, ok := e.Attrs[k].(string); !ok {
					t.Fatal(e)
				}
			}
			if d, ok := e.Attrs["duration_us"].(int64); !ok || d < 0 {
				t.Fatal(e)
			}
		}
	}
}

// R-LQZV-N82D R-LS7S-0ZT2 R-LUNK-SJAG R-LVVH-6B15 R-LX3D-K2RU R-LIGK-YTVI R-LJOH-CLM7 R-LPRZ-9GBO R-M4ER-UP80
func trailRuns(t *testing.T, f *serveFixture, es []telemetry.Event, requests map[string]string) {
	t.Helper()
	d, st := serveCatalog(t, f.p.Dir)
	defer func() { _ = d.Close() }()
	started, ended := map[string]bool{}, map[string]bool{}
	stop := -1
	for i, e := range es {
		if e.Name == "service.stopping" {
			stop = i
		}
		if !strings.HasPrefix(e.Name, "run.") {
			continue
		}
		id := e.Attrs["prompt_run"].(string)
		request, ok := requests[id]
		if !ok {
			t.Fatalf("unknown run event %#v", e)
		}
		if e.RequestID != request || e.User != "fixture-user" {
			t.Fatal(e)
		}
		if ended[id] {
			t.Fatal("event after run.finished", e)
		}
		if e.Name == "run.started" {
			if started[id] {
				t.Fatal("duplicate start", id)
			}
			started[id] = true
		}
		if e.Name == "run.finished" {
			ended[id] = true
			r, err := st.RunByID(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(e.Attrs, runs.FinishedAttrs(r, 0)) {
				t.Fatalf("finish %#v record %#v", e, r)
			}
		}
	}
	if stop < 0 {
		t.Fatal("missing stop")
	}
	for id := range started {
		if !ended[id] || trailIndex(t, es, "run.finished", id) >= stop {
			t.Fatal("unfinished run", id)
		}
	}
}

// R-KWIE-2YJ0 R-L2LV-ZT8H R-L3TS-DKZ6 R-5S0C-F6SZ R-5T88-SYJO R-L7HH-IW79 R-LB56-O7FC R-LCD3-1Z61 R-ME5Y-WV5K
func TestTrailCatalogRequests(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	p := trailCreate(t, f, "created", "catalog-probe")
	changes := []map[string]any{{"prompt": "changed payload"}, {"system": "changed system"}, {"tools": []string{"files", "bash"}}, {"tools": []string{"bash", "files"}}, {"schema": json.RawMessage(`{"type":"object","properties":{}}`)}}
	for i, args := range changes {
		args["name"] = p.Name
		decodeServe[tools.Prompt](t, trailCall(t, f, fmt.Sprintf("changed-%d", i), "update", args))
	}
	for i, args := range []map[string]any{{"name": p.Name}, {"name": p.Name, "tools": []string{"bash", "files"}}, {"name": p.Name, "schema": json.RawMessage(`{"type":"object","properties":{}}`)}, {"name": p.Name, "prompt": "changed payload", "system": "changed system"}} {
		trailCall(t, f, fmt.Sprintf("unchanged-%d", i), "update", args)
	}
	for _, name := range []string{"list", "show", "runs"} {
		trailCall(t, f, "read-"+name, name, map[string]any{"name": p.Name})
	}
	if !trailCall(t, f, "refused", "show", map[string]any{"name": "absent"}).IsError() {
		t.Fatal("missing prompt accepted")
	}
	for _, name := range []string{"subscribe", "unsubscribe"} {
		if trailCall(t, f, name, name, map[string]any{"name": p.Name, "event": "repo.pushed"}).IsError() {
			t.Fatal(name)
		}
		if !trailCall(t, f, name+"-refused", name, map[string]any{"name": "absent", "event": "repo.pushed"}).IsError() {
			t.Fatal(name)
		}
	}
	for i, path := range []string{"/", "/about", "/absent", "/_appkit/absent"} {
		req, e := http.NewRequest("GET", "http://backend"+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("X-User-Id", "fixture-user")
		req.Header.Set("X-Request-Id", fmt.Sprintf("page-%d", i))
		response, e := f.http.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		_, e = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	decodeServe[tools.Deleted](t, trailCall(t, f, "deleted", "delete", map[string]any{"name": p.Name}))
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	trailPromptEvent(t, es, "created", "prompt.created", p.ID)
	for i := range changes {
		trailPromptEvent(t, es, fmt.Sprintf("changed-%d", i), "prompt.updated", p.ID)
	}
	trailPromptEvent(t, es, "deleted", "prompt.deleted", p.ID)
	for i := 0; i < 4; i++ {
		trailNoDomain(t, es, fmt.Sprintf("unchanged-%d", i))
	}
	for _, id := range []string{"read-list", "read-show", "read-runs", "refused", "subscribe", "unsubscribe", "subscribe-refused", "unsubscribe-refused"} {
		trailNoDomain(t, es, id)
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("page-%d", i)
		trailWindow(t, es, id)
		count := 0
		for _, e := range es {
			if e.RequestID == id {
				count++
				if e.Name != "request.started" && e.Name != "request.finished" {
					t.Fatal(e)
				}
			}
		}
		if count != 2 {
			t.Fatal(count)
		}
	}
	for _, e := range es {
		if strings.HasPrefix(e.Name, "prompt.") {
			id := e.RequestID
			if id != "created" && id != "deleted" && !strings.HasPrefix(id, "changed-") {
				t.Fatal(e)
			}
		}
	}
	trailGeneral(t, es)
}

// R-LESV-TINF R-LG0S-7AE4 R-M82H-00G3 R-M9AD-DS6S
func TestTrailManualQueueAndExit(t *testing.T) {
	f := newServeFixture(t)
	f.env["RUN_MAX_ACTIVE"] = "1"
	entered, release := serveProvider(t, f)
	f.start(t)
	p := trailCreate(t, f, "create-queue", "queue-probe")
	shown := decodeServe[tools.Prompt](t, trailCall(t, f, "show-first", "show", map[string]any{"name": p.Name}))
	a := trailRun(t, f, "run-first", p.Name)
	serveWait(t, entered)
	shown2 := decodeServe[tools.Prompt](t, trailCall(t, f, "show-second", "show", map[string]any{"name": p.Name}))
	b := trailRun(t, f, "run-second", p.Name)
	if a.Status != store.StatusRunning || b.Status != store.StatusQueued {
		t.Fatal(a, b)
	}
	other := agentkit.Catalog()[0].Model
	if other == p.Model {
		other = agentkit.Catalog()[1].Model
	}
	decodeServe[tools.Prompt](t, trailCall(t, f, "update-queued", "update", map[string]any{"name": p.Name, "model": other}))
	release()
	trailWait(t, f, b.ID)
	trailCall(t, f, "result-ended", "result", map[string]any{"run": a.ID})
	if !trailCall(t, f, "cancel-ended", "cancel", map[string]any{"run": a.ID}).IsError() {
		t.Fatal("ended cancel accepted")
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	for _, pair := range []struct {
		r   tools.Started
		p   tools.Prompt
		req string
	}{{a, shown, "run-first"}, {b, shown2, "run-second"}} {
		i := trailIndex(t, es, "run.started", pair.r.ID)
		want := telemetry.Attrs{"prompt_run": pair.r.ID, "prompt": pair.p.ID, "model": pair.p.Model, "trigger": store.TriggerManual}
		if !reflect.DeepEqual(es[i].Attrs, want) {
			t.Fatal(es[i])
		}
		end := trailIndex(t, es, "run.finished", pair.r.ID)
		if end <= i || es[end].Attrs["status"] != store.StatusExited || es[end].Attrs["exit_code"] != int64(0) {
			t.Fatal(es[end])
		}
	}
	firstStart := trailIndex(t, es, "run.started", a.ID)
	ws, we := trailWindow(t, es, "run-first")
	if firstStart <= ws || firstStart >= we || firstStart >= trailIndex(t, es, "tool.called", "run-first") {
		t.Fatal("immediate start order")
	}
	secondStart := trailIndex(t, es, "run.started", b.ID)
	if secondStart <= trailIndex(t, es, "tool.called", "run-second") || secondStart <= trailIndex(t, es, "run.finished", a.ID) {
		t.Fatal("queued start order")
	}
	for _, e := range es[:trailIndex(t, es, "tool.called", "run-second")] {
		if e.Attrs["prompt_run"] == b.ID {
			t.Fatal(e)
		}
	}
	trailNoDomain(t, es, "result-ended")
	trailNoDomain(t, es, "cancel-ended")
	trailGeneral(t, es)
	trailRuns(t, f, es, map[string]string{a.ID: "run-first", b.ID: "run-second"})
}

// R-L8PD-WNXY R-L9XA-AFON R-LYB9-XUIJ R-M0R2-PDZX
func TestTrailCancelAndDelete(t *testing.T) {
	for _, action := range []string{"cancel", "delete"} {
		t.Run(action, func(t *testing.T) {
			f := newServeFixture(t)
			f.env["RUN_MAX_ACTIVE"] = "1"
			entered, _ := serveProvider(t, f)
			f.start(t)
			p := trailCreate(t, f, "create-kill", "kill-probe")
			a := trailRun(t, f, "running-origin", p.Name)
			serveWait(t, entered)
			b := trailRun(t, f, "queued-origin", p.Name)
			if b.Status != store.StatusQueued {
				t.Fatal(b)
			}
			if action == "delete" {
				decodeServe[tools.Deleted](t, trailCall(t, f, "delete-kill", "delete", map[string]any{"name": p.Name}))
			} else {
				decodeServe[tools.RunEntry](t, trailCall(t, f, "cancel-queued", "cancel", map[string]any{"run": b.ID}))
				decodeServe[tools.RunEntry](t, trailCall(t, f, "cancel-running", "cancel", map[string]any{"run": a.ID}))
			}
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			es := f.capture.Events()
			for _, pair := range []struct{ id, request, window string }{{a.ID, "running-origin", "cancel-running"}, {b.ID, "queued-origin", "cancel-queued"}} {
				i := trailIndex(t, es, "run.finished", pair.id)
				e := es[i]
				if e.Attrs["status"] != store.StatusKilled || e.RequestID != pair.request || e.User != "fixture-user" {
					t.Fatal(e)
				}
				if _, ok := e.Attrs["exit_code"]; ok {
					t.Fatal(e)
				}
				window, before := pair.window, "tool.called"
				if action == "delete" {
					window, before = "delete-kill", "prompt.deleted"
				}
				x, y := trailWindow(t, es, window)
				if i <= x || i >= y || i >= trailIndex(t, es, before, window) {
					t.Fatal("kill outside action window", e)
				}
				if pair.id == b.ID {
					for _, k := range []string{"calls", "tool_calls", "input_tokens", "output_tokens", "cost_nanos"} {
						if e.Attrs[k] != int64(0) {
							t.Fatal(e)
						}
					}
					for _, v := range es {
						if v.Name == "run.started" && v.Attrs["prompt_run"] == b.ID {
							t.Fatal(v)
						}
					}
				}
			}
			if action == "delete" {
				trailPromptEvent(t, es, "delete-kill", "prompt.deleted", p.ID)
				if trailIndex(t, es, "run.finished", a.ID) >= trailIndex(t, es, "prompt.deleted", "delete-kill") {
					t.Fatal("delete before kill")
				}
			} else {
				trailRuns(t, f, es, map[string]string{a.ID: "running-origin", b.ID: "queued-origin"})
			}
			trailGeneral(t, es)
		})
	}
}

// R-LH8O-L24T
func TestTrailFailedStart(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	p := trailCreate(t, f, "create-fail", "failed-probe")
	trailBreakCgroup(t, f)
	r := trailRun(t, f, "failed-origin", p.Name)
	if r.Status != store.StatusFailed || r.Reason == nil {
		t.Fatal(r)
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	i := trailIndex(t, es, "run.finished", r.ID)
	a, b := trailWindow(t, es, "failed-origin")
	e := es[i]
	if i <= a || i >= b || i >= trailIndex(t, es, "tool.called", "failed-origin") || e.Attrs["status"] != store.StatusFailed || e.Attrs["reason"] != *r.Reason {
		t.Fatal(e)
	}
	for _, k := range []string{"calls", "tool_calls", "input_tokens", "output_tokens", "cost_nanos"} {
		if e.Attrs[k] != int64(0) {
			t.Fatal(e)
		}
	}
	for _, e := range es {
		if e.Name == "run.started" && e.Attrs["prompt_run"] == r.ID {
			t.Fatal(e)
		}
	}
	trailRuns(t, f, es, map[string]string{r.ID: "failed-origin"})
	trailGeneral(t, es)
}

// R-M1YZ-35QM R-M36V-GXHB
func TestTrailRecovery(t *testing.T) {
	for _, offset := range []time.Duration{-time.Hour, time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			f := newServeFixture(t)
			d, st := serveCatalog(t, f.p.Dir)
			p, e := st.Create(context.Background(), store.Draft{Owner: "saved-owner", Name: "recovery-probe", Model: serveChatModel(t), Prompt: "private"})
			if e != nil {
				t.Fatal(e)
			}
			originals := []store.Run{}
			for i, status := range []string{store.StatusRunning, store.StatusQueued} {
				r, e := st.AddRun(context.Background(), store.Run{ID: fmt.Sprintf("prr_%016x", i+1), Prompt: p.ID, Model: p.Model, User: p.Owner, RequestID: fmt.Sprintf("saved-%d", i), Trigger: store.TriggerManual, Status: status, Started: serveTime.Truncate(time.Second).Add(offset)})
				if e != nil {
					t.Fatal(e)
				}
				originals = append(originals, r)
			}
			_ = d.Close()
			f.start(t)
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			es := f.capture.Events()
			start := -1
			for i, e := range es {
				if e.Name == "service.started" {
					start = i
				}
			}
			for _, r := range originals {
				i := trailIndex(t, es, "run.finished", r.ID)
				v := es[i]
				duration := serveTime.Sub(r.Started).Microseconds()
				if duration < 0 {
					duration = 0
				}
				if i >= start || v.User != r.User || v.RequestID != r.RequestID || v.Attrs["duration_us"] != duration || v.Attrs["truncated"] != false {
					t.Fatal(v)
				}
				if _, ok := v.Attrs["exit_code"]; ok {
					t.Fatal(v)
				}
				if r.Status == store.StatusRunning {
					if v.Attrs["status"] != store.StatusKilled {
						t.Fatal(v)
					}
				} else {
					if v.Attrs["status"] != store.StatusFailed || v.Attrs["reason"] != store.ReasonQueueAbandoned {
						t.Fatal(v)
					}
					for _, k := range []string{"calls", "tool_calls", "input_tokens", "output_tokens", "cost_nanos"} {
						if v.Attrs[k] != int64(0) {
							t.Fatal(v)
						}
					}
				}
				for _, v := range es {
					if v.Name == "run.started" && v.Attrs["prompt_run"] == r.ID {
						t.Fatal(v)
					}
				}
			}
		})
	}
}

// R-M6UK-M8PE
func TestTrailDrainQueue(t *testing.T) {
	f := newServeFixture(t)
	f.env["RUN_MAX_ACTIVE"] = "1"
	f.env["DRAIN_SECONDS"] = "5"
	entered, release := serveProvider(t, f)
	f.start(t)
	p := trailCreate(t, f, "create-drain", "drain-probe")
	a := trailRun(t, f, "drain-active", p.Name)
	serveWait(t, entered)
	b := trailRun(t, f, "drain-queued", p.Name)
	if b.Status != store.StatusQueued {
		t.Fatal(b)
	}
	f.cancel(fmt.Errorf("fixture drain"))
	trailWait(t, f, b.ID)
	release()
	if code := serveWait(t, f.result); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	i := trailIndex(t, es, "run.finished", b.ID)
	e := es[i]
	if e.Attrs["status"] != store.StatusFailed || e.Attrs["reason"] != store.ReasonQueueAbandoned || e.RequestID != "drain-queued" || e.User != "fixture-user" {
		t.Fatal(e)
	}
	if _, ok := e.Attrs["exit_code"]; ok {
		t.Fatal(e)
	}
	for _, e := range es {
		if e.Name == "run.started" && e.Attrs["prompt_run"] == b.ID {
			t.Fatal(e)
		}
	}
	if i >= trailIndex(t, es, "run.finished", a.ID) {
		t.Fatal("queued abandonment waited for active run")
	}
	trailRuns(t, f, es, map[string]string{a.ID: "drain-active", b.ID: "drain-queued"})
	trailGeneral(t, es)
}

// R-LTFO-ERJR
func TestTrailMeasuredDuration(t *testing.T) {
	for _, delta := range []time.Duration{1500 * time.Microsecond, -1500 * time.Microsecond} {
		t.Run(delta.String(), func(t *testing.T) {
			f := newServeFixture(t)
			var switched atomic.Bool
			f.p.Now = func() time.Time {
				if switched.Load() {
					return serveTime.Add(delta)
				}
				return serveTime
			}
			entered, release := serveProvider(t, f)
			f.start(t)
			p := trailCreate(t, f, "create-duration", "duration-probe")
			r := trailRun(t, f, "duration-origin", p.Name)
			if r.Status != store.StatusRunning {
				t.Fatal(r)
			}
			serveWait(t, entered)
			switched.Store(true)
			release()
			trailWait(t, f, r.ID)
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			es := f.capture.Events()
			v := es[trailIndex(t, es, "run.finished", r.ID)]
			us := delta.Microseconds()
			if us < 0 {
				us = 0
			}
			if v.Attrs["duration_us"] != us {
				t.Fatal(v)
			}
		})
	}
}

// R-MAI9-RJXH
func TestTrailTimeout(t *testing.T) {
	f := newServeFixture(t)
	timer := make(chan time.Time, 1)
	f.p.ScriptAfter = func(time.Duration) <-chan time.Time { return timer }
	entered, _ := serveProvider(t, f)
	f.start(t)
	p := trailCreate(t, f, "create-timeout", "timeout-probe")
	r := trailRun(t, f, "timeout-origin", p.Name)
	serveWait(t, entered)
	timer <- serveTime
	trailWait(t, f, r.ID)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	i := trailIndex(t, es, "run.finished", r.ID)
	if i <= trailIndex(t, es, "run.started", r.ID) || es[i].Attrs["status"] != store.StatusTimedOut {
		t.Fatal(es[i])
	}
	if _, ok := es[i].Attrs["exit_code"]; ok {
		t.Fatal(es[i])
	}
	trailRuns(t, f, es, map[string]string{r.ID: "timeout-origin"})
	trailGeneral(t, es)
}

// R-MBQ6-5BO6 R-M9AD-DS6S
func TestTrailProviderOutputAndError(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			f := newServeFixture(t)
			f.env["OUTPUT_MAX_BYTES"] = "4"
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if failure {
					w.WriteHeader(400)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"private provider failure"}}`)
				} else {
					serveAnswer(w)
				}
			}))
			t.Cleanup(provider.Close)
			f.p.BaseURL = provider.URL
			off, e := agent.Offering(serveChatModel(t))
			if e != nil {
				t.Fatal(e)
			}
			f.env[agent.KeyVariable(off.Host)] = "private-key"
			f.start(t)
			p := trailCreate(t, f, "create-output", "output-probe")
			r := trailRun(t, f, "output-origin", p.Name)
			trailWait(t, f, r.ID)
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			es := f.capture.Events()
			v := es[trailIndex(t, es, "run.finished", r.ID)]
			code := int64(0)
			if failure {
				code = 1
			}
			if v.Attrs["status"] != store.StatusExited || v.Attrs["exit_code"] != code || v.Attrs["truncated"] != true {
				t.Fatal(v)
			}
			trailRuns(t, f, es, map[string]string{r.ID: "output-origin"})
			trailGeneral(t, es)
		})
	}
}

func trailDelivery(t *testing.T, f *serveFixture, id, eventID, name string) {
	t.Helper()
	event := events.Event{ID: eventID, Time: serveTime, Service: "repos", Name: name, RequestID: "foreign-event-request", User: "foreign-event-user", Attrs: events.Attrs{"private": "event payload"}, Seq: 1, Received: serveTime}
	b, e := json.Marshal(event)
	if e != nil {
		t.Fatal(e)
	}
	var envelope map[string]json.RawMessage
	if e = json.Unmarshal(b, &envelope); e != nil {
		t.Fatal(e)
	}
	envelope["attempt"] = json.RawMessage("1")
	b, e = json.Marshal(envelope)
	if e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequest("POST", "http://backend/events", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", id)
	response, e := f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = response.Body.Close() }()
	var result struct{ Outcome string }
	if e = json.NewDecoder(response.Body).Decode(&result); e != nil {
		t.Fatal(e)
	}
	wantOutcome := "ok"
	if name == "other.event" {
		wantOutcome = "skip"
	}
	if response.StatusCode != 200 || result.Outcome != wantOutcome {
		t.Fatal(response.StatusCode, result)
	}
}

// R-KXQA-GQ9P R-LKWD-QDCW R-LM4A-453L R-LNC6-HWUA R-LOK2-VOKZ
func TestTrailDeliveries(t *testing.T) {
	for _, mode := range []string{"immediate", "queued", "failed"} {
		t.Run(mode, func(t *testing.T) {
			f := newServeFixture(t)
			f.env["RUN_MAX_ACTIVE"] = "1"
			var entered <-chan struct{}
			release := func() {}
			if mode != "failed" {
				entered, release = serveProvider(t, f)
			}
			f.start(t)
			p := trailCreate(t, f, "create-event", "event-probe")
			trailCall(t, f, "subscribe-event", "subscribe", map[string]any{"name": p.Name, "event": "repo.pushed"})
			requests := map[string]string{}
			var active tools.Started
			if mode == "queued" {
				active = trailRun(t, f, "event-blocker", p.Name)
				requests[active.ID] = "event-blocker"
				serveWait(t, entered)
			}
			if mode == "failed" {
				trailBreakCgroup(t, f)
			}
			eventID := "evt_0123456789abcdef"
			trailDelivery(t, f, "delivery-origin", eventID, "repo.pushed")
			trailDelivery(t, f, "duplicate-delivery", eventID, "repo.pushed")
			trailDelivery(t, f, "unmatched-delivery", "evt_fedcba9876543210", "other.event")
			d, st := serveCatalog(t, f.p.Dir)
			rs, e := st.Runs(context.Background(), p.ID)
			if e != nil {
				t.Fatal(e)
			}
			var r store.Run
			for _, v := range rs {
				if v.Event == eventID {
					if r.ID != "" {
						t.Fatal("duplicate event runs")
					}
					r = v
				}
			}
			_ = d.Close()
			if r.ID == "" {
				t.Fatal("missing delivered run")
			}
			requests[r.ID] = "delivery-origin"
			if r.User != "fixture-user" || r.RequestID != "delivery-origin" || r.Trigger != store.TriggerEvent {
				t.Fatal(r)
			}
			release()
			trailWait(t, f, r.ID)
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			es := f.capture.Events()
			a, b := trailWindow(t, es, "delivery-origin")
			if mode == "failed" {
				i := trailIndex(t, es, "run.finished", r.ID)
				v := es[i]
				if i <= a || i >= b || v.Attrs["status"] != store.StatusFailed || v.Attrs["reason"] != store.ReasonStartFailed {
					t.Fatal(v)
				}
				for _, k := range []string{"calls", "tool_calls", "input_tokens", "output_tokens", "cost_nanos"} {
					if v.Attrs[k] != int64(0) {
						t.Fatal(v)
					}
				}
				for _, v := range es {
					if v.Name == "run.started" && v.Attrs["prompt_run"] == r.ID {
						t.Fatal(v)
					}
				}
			} else {
				i := trailIndex(t, es, "run.started", r.ID)
				want := telemetry.Attrs{"prompt_run": r.ID, "prompt": p.ID, "model": p.Model, "trigger": store.TriggerEvent}
				if !reflect.DeepEqual(es[i].Attrs, want) {
					t.Fatal(es[i])
				}
				if mode == "immediate" && (i <= a || i >= b) {
					t.Fatal("delivery start outside window")
				}
				if mode == "queued" && i <= b {
					t.Fatal("queue start inside delivery")
				}
			}
			for _, id := range []string{"delivery-origin", "duplicate-delivery", "unmatched-delivery"} {
				trailWindow(t, es, id)
				for _, v := range es {
					if v.RequestID == id && v.Name == "tool.called" {
						t.Fatal(v)
					}
				}
			}
			trailNoDomain(t, es, "duplicate-delivery")
			trailNoDomain(t, es, "unmatched-delivery")
			trailRuns(t, f, es, requests)
			trailGeneral(t, es)
		})
	}
}

type trailDeadlineSink struct {
	blocked atomic.Bool
	capture telemetry.Capture
}

func (s *trailDeadlineSink) Deliver(ctx context.Context, e telemetry.Event) error {
	if s.blocked.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.capture.Deliver(ctx, e)
}

// R-M5MO-8GYP
func TestTrailDrainDeadlineDiagnostics(t *testing.T) {
	f := newServeFixture(t)
	f.env["DRAIN_SECONDS"] = "1"
	sink := &trailDeadlineSink{}
	f.p.Sink = sink
	entered, _ := serveProvider(t, f)
	f.start(t)
	p := trailCreate(t, f, "create-deadline", "deadline-probe")
	r := trailRun(t, f, "deadline-origin", p.Name)
	serveWait(t, entered)
	sink.blocked.Store(true)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	finish, stop := -1, -1
	for i, line := range strings.Split(f.err.String(), "\n") {
		prefix := "prompts: undelivered event: "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		var v struct {
			Name      string         `json:"event"`
			RequestID string         `json:"request_id"`
			User      string         `json:"user"`
			Attrs     map[string]any `json:"attrs"`
		}
		if e := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &v); e != nil {
			t.Fatal(e)
		}
		if v.Name == "service.stopping" {
			stop = i
		}
		if v.Name == "run.finished" && v.Attrs["prompt_run"] == r.ID {
			if finish >= 0 || v.Attrs["status"] != store.StatusKilled || v.RequestID != "deadline-origin" || v.User != "fixture-user" {
				t.Fatal(line)
			}
			if _, ok := v.Attrs["exit_code"]; ok {
				t.Fatal(line)
			}
			finish = i
		}
	}
	if finish < 0 || stop <= finish {
		t.Fatal(f.err.String())
	}
}

// R-MCY2-J3EV
func TestTrailSuiteGatewayStaysSeparate(t *testing.T) {
	f := newServeFixture(t)
	short, e := os.MkdirTemp("", "prompts-gateway-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	socket := filepath.Join(short, "gateway.sock")
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	var gatewayCalls atomic.Int32
	gateway := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
			return
		}
		var result any
		switch request.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "gateway_probe", "description": "supplied tool description", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			gatewayCalls.Add(1)
			if r.Header.Get("X-Request-Id") != "suite-origin" || r.Header.Get("X-User-Id") != "fixture-user" {
				t.Error("gateway identity", r.Header)
			}
			result = mcp.TextResult("supplied tool result")
		default:
			t.Error(request.Method)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	})}
	go func() { _ = gateway.Serve(ln) }()
	t.Cleanup(func() { _ = gateway.Close() })
	servicesFile := filepath.Join(f.p.Dir, "services.json")
	b, e := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "mcp", "url": "http://gateway", "description": "supplied gateway", "socket": socket, "enabled": true, "mcp": true}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(servicesFile, b, 0600); e != nil {
		t.Fatal(e)
	}
	f.env["IKIGENBA_SERVICES"] = servicesFile
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, v := range []any{map[string]any{"type": "message_start", "message": map[string]any{"id": "tool-message", "type": "message", "role": "assistant", "content": []any{}, "usage": map[string]any{"input_tokens": 1}}}, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "tool-call", "name": "gateway_probe", "input": map[string]any{}}}, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": "{}"}}, map[string]any{"type": "content_block_stop", "index": 0}, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 1}}, map[string]any{"type": "message_stop"}} {
				b, _ := json.Marshal(v)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			}
		} else {
			serveAnswer(w)
		}
	}))
	t.Cleanup(provider.Close)
	f.p.BaseURL = provider.URL
	off, e := agent.Offering(serveChatModel(t))
	if e != nil {
		t.Fatal(e)
	}
	f.env[agent.KeyVariable(off.Host)] = "suite-key"
	f.start(t)
	p := decodeServe[tools.Prompt](t, trailCall(t, f, "create-suite", "create", map[string]any{"name": "suite-probe", "model": serveChatModel(t), "prompt": "supplied prompt", "tools": []string{agent.GroupSuite}}))
	r := trailRun(t, f, "suite-origin", p.Name)
	trailWait(t, f, r.ID)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if gatewayCalls.Load() != 1 || calls.Load() != 2 {
		raw, err := os.ReadFile(filepath.Join(f.p.Dir, "state", "runs", p.ID, r.ID, runs.StderrFile))
		t.Fatal(gatewayCalls.Load(), calls.Load(), string(raw), err)
	}
	es := f.capture.Events()
	count := 0
	for _, v := range es {
		if v.RequestID == "suite-origin" {
			if v.Name == "tool.called" {
				count++
			}
			if v.Name != "request.started" && v.Name != "request.finished" && v.Name != "tool.called" && v.Name != "run.started" && v.Name != "run.finished" {
				t.Fatal(v)
			}
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
	v := es[trailIndex(t, es, "run.finished", r.ID)]
	if v.Attrs["status"] != store.StatusExited || v.Attrs["exit_code"] != int64(0) {
		t.Fatal(v)
	}
	trailGeneral(t, es)
	trailRuns(t, f, es, map[string]string{r.ID: "suite-origin"})
}

func trailBreakCgroup(t *testing.T, f *serveFixture) {
	t.Helper()
	path := filepath.Join(f.p.Cgroup, "runs")
	if e := os.Rename(path, path+"-saved"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("start failure fixture"), 0600); e != nil {
		t.Fatal(e)
	}
}

// R-5S0C-F6SZ R-5T88-SYJO
func TestTrailConcurrentUpdates(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	p := trailCreate(t, f, "create-concurrent", "concurrent-probe")
	model := ""
	for _, entry := range agentkit.Catalog() {
		if entry.Model != p.Model {
			model = entry.Model
			break
		}
	}
	if model == "" {
		t.Fatal("catalog needs two distinct models")
	}
	ready := make(chan struct{})
	done := make(chan struct{}, 2)
	for _, id := range []string{"model-change-a", "model-change-b"} {
		go func() {
			<-ready
			decodeServe[tools.Prompt](t, trailCall(t, f, id, "update", map[string]any{"name": p.Name, "model": model}))
			done <- struct{}{}
		}()
	}
	close(ready)
	serveWait(t, done)
	serveWait(t, done)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	changed := 0
	for _, id := range []string{"model-change-a", "model-change-b"} {
		found := false
		for _, e := range es {
			if e.RequestID == id && e.Name == "prompt.updated" {
				found = true
			}
		}
		if found {
			changed++
			trailPromptEvent(t, es, id, "prompt.updated", p.ID)
		} else {
			trailNoDomain(t, es, id)
		}
	}
	if changed != 1 {
		t.Fatal("concurrent changes", changed)
	}
	trailGeneral(t, es)
}

// R-LUNK-SJAG R-LVVH-6B15 R-LX3D-K2RU
func TestTrailCancelRacesExit(t *testing.T) {
	f := newServeFixture(t)
	entered, release := serveProvider(t, f)
	f.start(t)
	p := trailCreate(t, f, "create-race", "race-probe")
	r := trailRun(t, f, "race-origin", p.Name)
	serveWait(t, entered)
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() { <-ready; trailCall(t, f, "race-cancel", "cancel", map[string]any{"run": r.ID}); close(done) }()
	close(ready)
	release()
	serveWait(t, done)
	trailWait(t, f, r.ID)
	if !trailCall(t, f, "race-ended-cancel", "cancel", map[string]any{"run": r.ID}).IsError() {
		t.Fatal("cancel ended run accepted")
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	trailNoDomain(t, es, "race-ended-cancel")
	trailRuns(t, f, es, map[string]string{r.ID: "race-origin"})
	trailGeneral(t, es)
}

// R-LPRZ-9GBO R-ME5Y-WV5K
func TestTrailPruningEndedDeletionRefusedRunAndFile(t *testing.T) {
	f := newServeFixture(t)
	f.env["RUN_KEEP_DAYS"] = "1"
	f.env["RUN_KEEP_COUNT"] = "1"
	d, st := serveCatalog(t, f.p.Dir)
	oldPrompt, e := st.Create(context.Background(), store.Draft{Owner: "fixture-user", Name: "pruned-probe", Model: serveChatModel(t), Prompt: "supplied old prompt"})
	if e != nil {
		t.Fatal(e)
	}
	old, e := st.AddRun(context.Background(), store.Run{ID: "prr_fedcba9876543210", Prompt: oldPrompt.ID, Model: oldPrompt.Model, User: oldPrompt.Owner, RequestID: "pruned-origin", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: serveTime.Add(-48 * time.Hour).Truncate(time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	old, e = st.FinishRun(context.Background(), old.ID, store.Ending{Status: store.StatusExited, Finished: old.Started.Add(time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	kept, e := st.AddRun(context.Background(), store.Run{ID: "prr_abcdef0123456789", Prompt: oldPrompt.ID, Model: oldPrompt.Model, User: oldPrompt.Owner, RequestID: "retained-origin", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: old.Started.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = st.FinishRun(context.Background(), kept.ID, store.Ending{Status: store.StatusExited, Finished: kept.Started.Add(time.Second)}); e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	oldFolder := filepath.Join(f.p.Dir, "state", "runs", old.Prompt, old.ID)
	if e = os.MkdirAll(filepath.Join(oldFolder, runs.WorkDir), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(oldFolder, runs.StdoutFile), []byte("supplied old answer"), 0600); e != nil {
		t.Fatal(e)
	}
	entered, release := serveProvider(t, f)
	f.start(t)
	d, st = serveCatalog(t, f.p.Dir)
	_, e = st.RunByID(context.Background(), old.ID)
	_ = d.Close()
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("old run was not pruned: %v", e)
	}
	if _, e = os.Stat(oldFolder); !os.IsNotExist(e) {
		t.Fatalf("old run folder remains: %v", e)
	}
	if !trailCall(t, f, "refused-run", "run", map[string]any{"name": "absent-prompt"}).IsError() {
		t.Fatal("absent prompt run accepted")
	}
	p := trailCreate(t, f, "create-file", "file-probe")
	r := trailRun(t, f, "file-origin", p.Name)
	if r.Status != store.StatusRunning {
		t.Fatal(r)
	}
	serveWait(t, entered)
	release()
	trailWait(t, f, r.ID)
	root, e := os.OpenRoot(filepath.Join(f.p.Dir, "state", "runs", p.ID, r.ID))
	if e != nil {
		t.Fatal(e)
	}
	want, e := root.ReadFile(runs.StdoutFile)
	_ = root.Close()
	if e != nil {
		t.Fatal(e)
	}
	if len(want) == 0 {
		t.Fatal("child produced no stdout")
	}
	request, e := http.NewRequest("GET", "http://backend/"+p.Name+"/runs/"+r.ID+"/"+runs.StdoutFile, nil)
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set("X-User-Id", "fixture-user")
	request.Header.Set("X-Request-Id", "actual-run-file")
	response, e := f.http.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(got, want) {
		t.Fatalf("run file status %d body %q want %q", response.StatusCode, got, want)
	}
	decodeServe[tools.Deleted](t, trailCall(t, f, "delete-ended", "delete", map[string]any{"name": p.Name}))
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	trailNoDomain(t, es, "refused-run")
	trailPromptEvent(t, es, "delete-ended", "prompt.deleted", p.ID)
	starts, finishes := 0, 0
	for _, event := range es {
		if !strings.HasPrefix(event.Name, "run.") {
			continue
		}
		if event.Attrs["prompt_run"] != r.ID || event.RequestID != "file-origin" || event.User != "fixture-user" {
			t.Fatalf("unanswered run or extra lifecycle event: %#v", event)
		}
		switch event.Name {
		case "run.started":
			starts++
		case "run.finished":
			finishes++
		default:
			t.Fatal(event)
		}
	}
	if starts != 1 || finishes != 1 {
		t.Fatalf("run lifecycle counts: started %d finished %d", starts, finishes)
	}
	start, finish := trailIndex(t, es, "run.started", r.ID), trailIndex(t, es, "run.finished", r.ID)
	if start >= finish || finish >= trailIndex(t, es, "request.started", "delete-ended") {
		t.Fatal("ended run lifecycle ordering")
	}
	a, b := trailWindow(t, es, "actual-run-file")
	if a >= b {
		t.Fatal("file request ordering")
	}
	count := 0
	for _, event := range es {
		if event.RequestID == "actual-run-file" {
			count++
			if event.Name != "request.started" && event.Name != "request.finished" {
				t.Fatal(event)
			}
		}
	}
	if count != 2 {
		t.Fatalf("file request event count %d", count)
	}
	trailGeneral(t, es)
}

// R-5S0C-F6SZ R-5T88-SYJO R-5VO1-KI12
func TestTrailSchemaClear(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	p := trailCreate(t, f, "created-schema-clear", "clear-probe")
	decodeServe[tools.Prompt](t, trailCall(t, f, "schema-set", "update", map[string]any{"name": p.Name, "schema": json.RawMessage(`{"type":"object"}`)}))
	decodeServe[tools.Prompt](t, trailCall(t, f, "schema-absent", "update", map[string]any{"name": p.Name}))
	decodeServe[tools.Prompt](t, trailCall(t, f, "schema-clear", "update", map[string]any{"name": p.Name, "schema": nil}))
	decodeServe[tools.Prompt](t, trailCall(t, f, "schema-clear-empty", "update", map[string]any{"name": p.Name, "schema": nil}))
	decodeServe[tools.Prompt](t, trailCall(t, f, "schema-restore", "update", map[string]any{"name": p.Name, "schema": json.RawMessage(`{"type":"object"}`)}))
	ready := make(chan struct{})
	done := make(chan struct{}, 2)
	for _, id := range []string{"schema-concurrent-a", "schema-concurrent-b"} {
		go func() {
			<-ready
			decodeServe[tools.Prompt](t, trailCall(t, f, id, "update", map[string]any{"name": p.Name, "schema": nil}))
			done <- struct{}{}
		}()
	}
	close(ready)
	serveWait(t, done)
	serveWait(t, done)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	for _, id := range []string{"schema-set", "schema-clear", "schema-restore"} {
		trailPromptEvent(t, es, id, "prompt.updated", p.ID)
	}
	for _, id := range []string{"schema-absent", "schema-clear-empty"} {
		trailNoDomain(t, es, id)
	}
	changed := 0
	for _, id := range []string{"schema-concurrent-a", "schema-concurrent-b"} {
		found := false
		for _, e := range es {
			if e.RequestID == id && e.Name == "prompt.updated" {
				found = true
			}
		}
		if found {
			changed++
			trailPromptEvent(t, es, id, "prompt.updated", p.ID)
		} else {
			trailNoDomain(t, es, id)
		}
	}
	if changed != 1 {
		t.Fatal("concurrent clears", changed)
	}
	trailGeneral(t, es)
}
