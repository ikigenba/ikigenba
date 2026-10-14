package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/fmcsa"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-YZYT-P0Z3 R-48SL-TOC5 R-DDS2-65CJ
func TestRunRejectsBeforeInheriting(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{{"settings", map[string]string{"SCRAPE_KEEP_RUNS": "0"}, "scrapes: SCRAPE_KEEP_RUNS is '0', not " + settings.WantRuns + "\n"}, {"counties", map[string]string{"MN_PARCELS_COUNTIES": "Chisago;"}, "scrapes: MN_PARCELS_COUNTIES is 'Chisago;', not " + settings.WantCounties + "\n"}, {"absent", nil, "scrapes: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"}, {"many", map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "02"}, "scrapes: 02 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"}, {"wrongpid", map[string]string{"LISTEN_PID": "124", "LISTEN_FDS": "1"}, "scrapes: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			out, errw := &outputBuffer{}, &outputBuffer{}
			dir := t.TempDir()
			p := cli.Process{Stdout: out, Stderr: errw, Pid: 123, Dir: dir, Version: "test", LookupEnv: func(k string) (string, bool) {
				if (tc.name == "settings" || tc.name == "counties") && (k == "LISTEN_PID" || k == "LISTEN_FDS" || k == services.Variable) {
					t.Fatalf("lookup after settings %s", k)
				}
				v, ok := tc.env[k]
				return v, ok
			}, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherited"); return nil, nil }, Unsetenv: func(string) error { t.Fatal("unset"); return nil }, HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("source called"); return nil, nil })}}
			if got := cli.Run(context.Background(), p); got != cli.ExitUsage {
				t.Fatalf("exit %d", got)
			}
			if out.String() != "" || errw.String() != tc.want || errw.writes != 1 {
				t.Fatalf("out %q err %q writes %d", out.String(), errw.String(), errw.writes)
			}
			entries, e := os.ReadDir(dir)
			if e != nil || len(entries) != 0 {
				t.Fatalf("state created %v %v", entries, e)
			}
		})
	}
}

// R-42P3-WTMO R-YCSQ-FDVW R-YHOB-YGUO R-454W-OD42
// R-48SL-TOC5 R-312V-7Q7Q R-YMJX-HJTG R-YNRT-VBK5
// R-47KP-FWLG R-DEZY-JX38 R-DJVK-3020 R-DNJ9-8BA3  R-ELOF-XW1J
func TestRunAssembly(t *testing.T) {
	var mu sync.Mutex
	looked := []string{}
	unset := []string{}
	inherited := 0
	f := startRun(t, runOptions{CheckReadyBeforeAccept: true, Rand: fixedRandom(0x5a), Modify: func(p *cli.Process) {
		lookup := p.LookupEnv
		p.LookupEnv = func(k string) (string, bool) { mu.Lock(); looked = append(looked, k); mu.Unlock(); return lookup(k) }
		p.Unsetenv = func(k string) error { unset = append(unset, k); return errors.New("ignored") }
		inherit := p.Inherit
		p.Inherit = func(fd uintptr) (net.Listener, error) { inherited++; return inherit(fd) }
	}})
	checkStateTree(t, f.Dir, "")
	v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))
	id, _ := v["id"].(string)
	if id != "run_5a5a5a5a5a5a5a5a" {
		t.Fatalf("run %v", v)
	}
	v = f.awaitRun(id)
	if v["status"] != store.StatusDone || v["rows"] != float64(2) {
		t.Fatalf("ended %v", v)
	}
	checkStateTree(t, f.Dir, id)
	resp := f.request("GET", "/runs/"+id+"/rows", "", true)
	body, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("rows %d %s", resp.StatusCode, body)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines %q", body)
	}
	for _, line := range lines {
		var row map[string]any
		if e = json.Unmarshal([]byte(line), &row); e != nil || row["run"] != id || row["raw"] == nil || row["collected"] != fixtureNow().Format(time.RFC3339) {
			t.Fatalf("row %v %v", row, e)
		}
	}
	resp = f.request("GET", "/scrapers/fmcsa/current", "", true)
	var current map[string]any
	if e = json.NewDecoder(resp.Body).Decode(&current); e != nil || resp.StatusCode != 200 || current["id"] != id {
		t.Fatalf("current %v %v", current, e)
	}
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	checkStateTree(t, f.Dir, id)
	if pendingNotify(t, f.Notify) {
		t.Fatal("extra readiness datagram")
	}
	st, _ := openStore(t, f.Dir)
	r, e := st.GetRun(context.Background(), id)
	if e != nil || r.ID != id || !r.Started.Equal(fixtureNow()) || !r.Finished.Equal(fixtureNow()) {
		t.Fatalf("persisted %+v %v", r, e)
	}
	count := 0
	if err := st.EachRow(context.Background(), id, func(row source.Row) error {
		count++
		if row.Run != id {
			t.Errorf("persisted row run %q", row.Run)
		}
		return nil
	}); err != nil || count != 2 {
		t.Fatalf("persisted rows %d: %v", count, err)
	}
	if inherited != 1 || !reflect.DeepEqual(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatalf("inherit %d unset %v", inherited, unset)
	}
	allowed := map[string]bool{}
	for _, k := range []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "SCRAPE_WRITE_BUDGET", "SCRAPE_PRESSURE_LIMIT", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS", "PPP_FILES", "PPP_STATES", "MN_PARCELS_COUNTIES", services.Variable, "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET"} {
		allowed[k] = true
	}
	for _, k := range looked {
		if !allowed[k] {
			t.Errorf("unexpected lookup %s", k)
		}
	}
	requests := f.Source.Requests()
	if len(requests) == 0 {
		t.Fatal("source transport unused")
	}
	for _, req := range requests {
		if req.Header.Get("User-Agent") != pages.ServiceName+" ("+pages.Description+")" {
			t.Errorf("user agent %q", req.Header.Get("User-Agent"))
		}
	}
	evs := f.Telemetry.Events()
	if len(evs) < 2 || evs[0].Name != "service.started" || evs[len(evs)-1].Name != "service.stopping" {
		t.Fatalf("events %v", evs)
	}
	if evs[0].Attrs["version"] != f.Process.Version || evs[len(evs)-1].Attrs["reason"] != "fixture stop" {
		t.Fatalf("lifecycle %v", evs)
	}
	stopping := 0
	for _, event := range evs {
		if event.Name == "service.stopping" {
			stopping++
			if event.RequestID != "" || event.User != "" || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"reason": "fixture stop"}) {
				t.Fatalf("stopping envelope %+v", event)
			}
		}
	}
	if stopping != 1 {
		t.Fatalf("stopping count %d", stopping)
	}
	bus := f.Events.Events()
	if len(bus) != 2 || bus[0].Name != "scrape.started" || bus[1].Name != "scrape.finished" {
		t.Fatalf("bus %v", bus)
	}
	if f.Out.String() != "" || f.Err.String() != "" {
		t.Fatalf("streams %q %q", f.Out.String(), f.Err.String())
	}
}
func checkStateTree(t *testing.T, dir, id string) {
	t.Helper()
	entries, e := os.ReadDir(dir)
	wantEntries := 1
	if id != "" {
		wantEntries = 2
	}
	if e != nil || len(entries) != wantEntries {
		t.Fatalf("dir entries %v %v", entries, e)
	}
	for _, entry := range entries {
		if !entry.IsDir() || (entry.Name() != "state" && (id == "" || entry.Name() != "runs")) {
			t.Fatalf("unexpected entry %v", entry)
		}
	}
	if id != "" {
		runFiles, err := os.ReadDir(filepath.Join(dir, "runs"))
		if err != nil || len(runFiles) != 1 || runFiles[0].Name() != id+".db" || !runFiles[0].Type().IsRegular() {
			t.Fatalf("run files %v: %v", runFiles, err)
		}
	}
	entries, e = os.ReadDir(filepath.Join(dir, "state"))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "scrapes.db") || entry.IsDir() {
			t.Fatalf("state entry %v", entry)
		}
		if entry.Name() == "scrapes.db" {
			found = true
		}
	}
	if !found {
		t.Fatal("database absent")
	}
}

// R-Z16Q-2SPS
func TestRunScopeSettings(t *testing.T) {
	for _, tc := range []struct {
		env    map[string]string
		states []string
		status string
		min    int64
	}{{nil, []string{"MN", "WI"}, "A", 5}, {map[string]string{"FMCSA_STATES": "IA,IL", "FMCSA_STATUS": "I", "FMCSA_MIN_POWER_UNITS": "0"}, []string{"IA", "IL"}, "I", 0}} {
		t.Run(strings.Join(tc.states, ""), func(t *testing.T) {
			f := startRun(t, runOptions{Env: tc.env})
			v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))
			if done := f.awaitRun(v["id"].(string)); done["status"] != store.StatusDone {
				t.Fatal(done)
			}
			seen := map[string]bool{}
			for _, r := range f.Source.Requests() {
				where := r.URL.Query().Get("$where")
				ok := false
				for _, state := range tc.states {
					if where == fmcsa.Where(state, tc.status, tc.min) {
						seen[state] = true
						ok = true
					}
				}
				if !ok {
					t.Errorf("scope %q", where)
				}
			}
			if len(seen) != len(tc.states) {
				t.Fatalf("states %v", seen)
			}
		})
	}
}

// R-YF8J-6XDA
func TestRunDefaultBoundaryOptions(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	f := startRun(t, runOptions{DefaultSinks: true, Modify: func(p *cli.Process) { p.Dir = ""; p.Unsetenv = nil }})
	v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))
	id := v["id"].(string)
	if f.awaitRun(id)["status"] != store.StatusDone {
		t.Fatal("source failed")
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	st, _ := openStore(t, dir)
	r, e := st.GetRun(context.Background(), id)
	if e != nil || !r.Started.Equal(fixtureNow()) || !r.Finished.Equal(fixtureNow()) {
		t.Fatalf("run %+v %v", r, e)
	}
	if len(f.Events.Events()) != 2 || len(f.Telemetry.Events()) == 0 {
		t.Fatal("socket sinks unused")
	}
	for _, event := range f.Telemetry.Events() {
		if !event.Time.Equal(fixtureNow()) {
			t.Fatalf("telemetry clock %+v", event)
		}
	}
	for _, event := range f.Events.Events() {
		if !event.Time.Equal(fixtureNow()) {
			t.Fatalf("bus clock %+v", event)
		}
	}
}

// R-DG7U-XOTX R-DHFR-BGKM
func TestRunDatabaseRefusal(t *testing.T) {
	t.Setenv(services.Variable, "")
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "state"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	_, openErr := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scrapes.db"), Migrations: app.Migrations(), Now: fixtureNow})
	if openErr == nil {
		t.Fatal("fixture opens")
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ln.Close() }()
	notify := notificationSocket(t)
	accepted := make(chan struct{})
	tracked := &acceptSignal{Listener: ln, ready: accepted}
	out, errw := &outputBuffer{}, &outputBuffer{}
	sink, bus := &telemetry.Capture{}, &events.Capture{}
	p := cli.Process{Pid: 7, Dir: dir, Stdout: out, Stderr: errw, Now: fixtureNow, Rand: &runRandom{}, Sink: sink, EventSink: bus, LookupEnv: func(k string) (string, bool) {
		switch k {
		case "NOTIFY_SOCKET":
			return notify.LocalAddr().String(), true
		case "LISTEN_PID":
			return "7", true
		case "LISTEN_FDS":
			return "1", true
		}
		return "", false
	}, Inherit: func(uintptr) (net.Listener, error) { return tracked, nil }}
	if code := cli.Run(context.Background(), p); code != cli.ExitServerFailed {
		t.Fatalf("code %d", code)
	}
	want := "scrapes: cannot open database state/scrapes.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
	if errw.String() != want || errw.writes != 1 || out.String() != "" || len(sink.Events()) != 0 || len(bus.Events()) != 0 {
		t.Fatalf("streams %q %q events %v", out.String(), errw.String(), sink.Events())
	}
	if pendingNotify(t, notify) {
		t.Fatal("refusal sent readiness")
	}
	select {
	case <-accepted:
		t.Fatal("refusal accepted a connection")
	default:
	}
}

// R-UCGC-VLLI
func TestRunWithoutServicesFile(t *testing.T) {
	for _, value := range []string{"", filepath.Join(t.TempDir(), "missing"), "malformed"} {
		t.Run(value, func(t *testing.T) {
			path := value
			if value == "malformed" {
				path = filepath.Join(t.TempDir(), "services")
				if e := os.WriteFile(path, []byte("broken"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			f := startRun(t, runOptions{Env: map[string]string{services.Variable: path}})
			if r := f.call(fixtureCaller, "scrapers", map[string]any{}); r.IsError() {
				t.Fatal(r)
			}
		})
	}
}

// R-DJVK-3020
func TestRunWithNotificationUnset(t *testing.T) {
	f := startRun(t, runOptions{WithoutNotify: true})
	if r := f.call(fixtureCaller, "scrapers", map[string]any{}); r.IsError() {
		t.Fatal(r)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop")
	}
	if pendingNotify(t, f.Notify) {
		t.Fatal("notified with unset socket")
	}
}
