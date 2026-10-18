package cli_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Each read has a distinct base-96 suffix, and every output byte is >= 0xa0.
type secrecyBytes struct{ n uint64 }

func (r *secrecyBytes) Read(p []byte) (int, error) {
	r.n++
	n := r.n
	for i := len(p) - 1; i >= 0; i-- {
		p[i] = 0xa0 + byte(n%96)
		n /= 96
	}
	return len(p), nil
}

type secrecyFake struct {
	mu         sync.Mutex
	rows       []map[string]any
	mode, echo string
	hold       <-chan struct{}
	arrived    chan struct{}
}

func (f *secrecyFake) set(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *secrecyFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	mode, echo, rows, hold, arrived := f.mode, f.echo, f.rows, f.hold, f.arrived
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if mode == "record_failure" && strings.HasSuffix(r.URL.Path, "/record") || mode == "rows_failure" && strings.HasSuffix(r.URL.Path, "/rows") {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, echo)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/rows") {
		if arrived != nil {
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if mode == "malformed" {
			_, _ = fmt.Fprintln(w, echo)
			return
		}
		for _, row := range rows {
			_ = json.NewEncoder(w).Encode(row)
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_fixture", "scraper": "fmcsa", "status": "done", "started": "2026-01-01T00:00:00Z", "finished": "2026-01-01T01:00:00Z", "rows": len(rows), "new": len(rows), "changed": 0, "unchanged": 0, "gone": 0})
}

// R-0BIN-NRQ6: the fixtures and the comparison implement the marker vocabulary.
func secrecyMarkers(t *testing.T) ([]string, string) {
	t.Helper()

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	markers := make([]string, 10)
	for i := range markers {
		seed := sha256.Sum256([]byte(fmt.Sprintf("marker-fixture-%d", i)))
		var b [24]byte
		for j := range b {
			b[j] = alphabet[int(seed[j])%len(alphabet)]
		}
		// Spacing the forced letters eight apart protects every sliding window.
		for j := 0; j < len(b); j += 8 {
			b[j] = 'G' + seed[j+1]%20
		}
		markers[i] = string(b[:])
	}
	phone := "8"
	phoneSeed := sha256.Sum256([]byte("phone-fixture"))
	for i := 1; i < 10; i++ {
		phone += string('0' + phoneSeed[i]%10)
	}
	for i, m := range markers {
		if len(m) != 24 {
			t.Fatal("marker width")
		}
		for _, b := range m {
			if !strings.ContainsRune(alphabet, b) {
				t.Fatal("marker alphabet")
			}
		}
		for j := 0; j <= len(m)-8; j++ {
			if !strings.ContainsAny(m[j:j+8], "GHIJKLMNOPQRSTUVWXYZ") {
				t.Fatal("marker window")
			}
		}
		for k, n := range markers {
			if i != k && strings.Contains(m, n) {
				t.Fatal("overlapping markers")
			}
		}
	}
	return markers, phone
}
func secrecyAbsent(t *testing.T, where string, value any, markers []string) {
	t.Helper()
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
	}

	checkText := func(text string) {
		lower := strings.ToLower(text)
		for i, m := range markers {
			if strings.Contains(lower, strings.ToLower(m)) {
				t.Fatalf("%s leaked marker %d", where, i)
			}
		}
	}
	// Search the serialized event and every decoded field, including escaped JSON strings.
	checkText(string(raw))
	var fields any
	if json.Unmarshal(raw, &fields) == nil {
		var checkField func(any)
		checkField = func(v any) {
			switch f := v.(type) {
			case string:
				checkText(f)
			case []any:
				for _, x := range f {
					checkField(x)
				}
			case map[string]any:
				for k, x := range f {
					checkText(k)
					checkField(x)
				}
			}
		}
		checkField(fields)
	}
}

// R-0DBY-N0TE R-0FRR-EKAS R-0I7K-63S6 R-0JFG-JVIV R-0LV9-BF09 R-VXPS-X5UC
func TestRunSecrecyAcrossSuccessAndFailures(t *testing.T) {
	markers, phone := secrecyMarkers(t)
	forbidden := append(append([]string{}, markers...), phone)
	row := runRow()
	row["name"] = markers[0]
	row["names"] = []string{markers[1], markers[2]}
	row["address"] = map[string]any{"street": markers[3], "city": markers[4], "state": "MN", "zip": "55072", "county": "115"}
	row["people"] = []map[string]string{{"name": markers[5], "role": markers[6]}}
	facts := row["facts"].(map[string]any)
	facts["email"] = markers[7] + "@" + markers[8] + ".example"
	facts["phone"] = phone
	facts["power_units"] = "100"
	facts["drivers"] = "100"
	facts["status"] = "A"
	facts["class"] = "AUTHORIZED FOR HIRE"
	facts["mcs150_date"] = "20250101"
	row["raw"] = map[string]any{"private": markers[9]}
	secondBytes, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var second map[string]any
	if err = json.Unmarshal(secondBytes, &second); err != nil {
		t.Fatal(err)
	}
	second["source_id"], second["digest"] = "102", "second-digest"
	second["name"], second["names"] = markers[1], []string{markers[1]}
	secondFacts := second["facts"].(map[string]any)
	secondPhone := phone[:9] + string('0'+(phone[9]-'0'+1)%10)
	secondFacts["phone"] = secondPhone
	secondFacts["email"] = markers[8] + "@" + markers[7] + ".example"
	forbidden = append(forbidden, secondPhone)
	f := &secrecyFake{rows: []map[string]any{row, second}, echo: strings.Join(forbidden, " ")}
	pf := &promptFake{echo: strings.Join(forbidden, " ")}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f), "prompts": runSocket(t, pf)}))
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/error") {
			return nil, fmt.Errorf("fixture fetch failure")
		}
		status := 200
		if strings.HasSuffix(r.URL.Path, "/missing") {
			status = 404
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<title>" + markers[3] + "</title><p>" + markers[4] + "</p>"))}, nil
	})}
	h.p.After = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		if d == website.Interval || d == passes.PollInterval || d == passes.QueueWait {
			ch <- time.Time{}
		}
		return ch
	}
	h.p.Rand = &secrecyBytes{}
	h.start()
	caller := identity.Caller{UserID: "reader", Email: "reader@example.test", RequestID: "secrecy-request"}
	refusals := []mcp.Result{}
	expectedPasses := map[string]string{}
	call := func(name string, args any, wantError bool) map[string]json.RawMessage {
		t.Helper()
		r, v := h.call(caller, name, args)
		if r.IsError() != wantError {
			t.Fatalf("%s error=%v want=%v", name, r.IsError(), wantError)
		}
		if r.IsError() {
			refusals = append(refusals, r)
		}
		return v
	}
	rebuild := func(wantReason string) string {
		t.Helper()
		v := call("rebuild", map[string]any{}, false)
		id := runText(v, "id")
		expectedPasses[id] = caller.RequestID
		p := h.waitPass(caller, id)
		if wantReason == "" {
			if runText(p, "status") != store.StatusDone {
				t.Fatal("pass did not finish done")
			}
		} else if runText(p, "status") != store.StatusFailed || runText(p, "reason") != wantReason {
			t.Fatalf("pass failure %s/%s", runText(p, "status"), runText(p, "reason"))
		}
		return id
	}
	doneID := rebuild("")
	call("pass", map[string]any{"pass": doneID}, false)
	call("cohorts", map[string]any{}, false)
	list := call("candidates", map[string]any{"name": markers[0], "phone": phone}, false)
	var candidates []map[string]json.RawMessage
	if err := json.Unmarshal(list["candidates"], &candidates); err != nil || len(candidates) != 1 {
		t.Fatal("marker candidate missing")
	}
	id := runText(candidates[0], "id")
	detail := call("candidate", map[string]any{"candidate": id}, false)
	b, _ := json.Marshal(detail)
	if !strings.Contains(strings.ToLower(string(b)), strings.ToLower(markers[0])) {
		t.Fatal("successful answer omitted supplied marker")
	}
	websitePass := call("enrich", map[string]any{"kind": "website_check"}, false)
	websiteID := runText(websitePass, "id")
	expectedPasses[websiteID] = caller.RequestID
	h.waitPass(caller, websiteID)
	siteURL := "https://" + strings.ToLower(markers[8]) + ".example"
	kept := call("add_page", map[string]any{"candidate": id, "url": siteURL + "/kept"}, false)
	pagePath := "/candidates/" + id + "/pages/" + string(kept["number"])
	for _, suffix := range []string{"/missing", "/error"} {
		rr, _ := h.call(caller, "add_page", map[string]any{"candidate": id, "url": siteURL + suffix})
		if !rr.IsError() {
			t.Fatal(rr)
		}
		secrecyAbsent(t, "add_page refusal", rr, append(append([]string{}, markers[:8]...), markers[9], phone))
	}
	call("enrich", map[string]any{"kind": "absent"}, true)
	call("add_page", map[string]any{"candidate": "absent", "url": "https://absent.test/"}, true)
	call("passes", map[string]any{}, false)
	all := call("candidates", map[string]any{}, false)
	var active []map[string]json.RawMessage
	if err := json.Unmarshal(all["candidates"], &active); err != nil || len(active) < 2 {
		t.Fatal("distinct candidates missing")
	}
	other := runText(active[0], "id")
	if other == id {
		other = runText(active[1], "id")
	}
	call("decide", map[string]any{"kind": store.KindNotSame, "candidate": id, "other": other, "why": markers[9]}, false)
	call("exclude", map[string]any{"candidate": id, "why": markers[9]}, false)
	call("include", map[string]any{"candidate": id, "why": markers[9]}, false)
	co := call("create_cohort", map[string]any{"name": markers[0]}, false)
	cohort := co
	cohortID := runText(cohort, "id")
	call("edit_cohort", map[string]any{"cohort": cohortID, "name": markers[1]}, false)
	call("create_cohort", map[string]any{"name": markers[2]}, false)
	call("reject", map[string]any{"cohort": cohortID, "candidate": id, "why": markers[9]}, false)
	call("unreject", map[string]any{"cohort": cohortID, "candidate": id, "why": markers[9]}, false)
	call("reject", map[string]any{"cohort": cohortID, "candidate": id, "why": markers[9]}, false)
	cohortRead := call("cohort", map[string]any{"cohort": cohortID}, false)
	var members, rejects []map[string]json.RawMessage
	if err := json.Unmarshal(cohortRead["members"], &members); err != nil || len(members) == 0 {
		t.Fatal("marker cohort has no members", err)
	}
	if err := json.Unmarshal(cohortRead["rejects"], &rejects); err != nil || len(rejects) != 1 || runText(rejects[0], "why") != markers[9] {
		t.Fatal("marker cohort standing reject missing", err)
	}
	var readCohort map[string]json.RawMessage
	if err := json.Unmarshal(cohortRead["cohort"], &readCohort); err != nil || runText(readCohort, "name") != markers[1] {
		t.Fatal("marker cohort name missing", err)
	}
	for _, tc := range []struct {
		name string
		args any
	}{
		{"cohorts", map[string]any{"page": markers[0]}}, {"candidates", map[string]any{"name": markers[0], "phone": phone, "page": markers[0]}},
		{"candidate", map[string]any{"candidate": "absent"}}, {"passes", map[string]any{"page": markers[0]}}, {"pass", map[string]any{"pass": "absent"}},
		{"cohort", map[string]any{"cohort": "absent"}},
		{"create_cohort", map[string]any{"name": markers[4], "size": 0}}, {"edit_cohort", map[string]any{"cohort": "absent", "name": markers[1]}},
		{"delete_cohort", map[string]any{"cohort": "absent"}},
		{"exclude", map[string]any{"candidate": "absent", "why": markers[9]}}, {"include", map[string]any{"candidate": "absent", "why": markers[9]}},
		{"reject", map[string]any{"cohort": cohortID, "candidate": "absent", "why": markers[9]}}, {"unreject", map[string]any{"cohort": cohortID, "candidate": "absent", "why": markers[9]}},
		{"rebuild", []any{}}, {"decide", map[string]any{"kind": "invalid", "candidate": id, "why": markers[9]}},
	} {
		call(tc.name, tc.args, true)
	}
	for _, path := range []string{"/", "/cohorts/" + cohortID, "/candidates/" + id, "/passes/" + doneID, "/tools", "/about", "/absent", "/candidates/absent", "/passes/absent"} {
		status, body := h.request("GET", path, "page-request", nil)
		if path == "/cohorts/"+cohortID {
			if status != 200 || !strings.Contains(strings.ToLower(string(body)), strings.ToLower(markers[1])) || !strings.Contains(strings.ToLower(string(body)), strings.ToLower(markers[9])) {
				t.Fatal("signed-in cohort page omitted marker cohort or standing reject")
			}
		}
		if path == "/candidates/"+id {
			if status != 200 || !strings.Contains(strings.ToLower(string(body)), strings.ToLower(markers[0])) {
				t.Fatal("signed-in candidate page omitted marker")
			}
		}
	}
	for _, mode := range []string{"success", "failed", "refuse-result", "http-show", "http-run", "http-result", "refuse-show"} {
		pf.mu.Lock()
		pf.mode = mode
		pf.mu.Unlock()
		if mode == "refuse-show" || mode == "http-show" {
			call("enrich", map[string]any{"kind": enrichers.KindSiteText, "cohort": cohortID, "force": true}, true)
			continue
		}
		v := call("enrich", map[string]any{"kind": enrichers.KindSiteText, "cohort": cohortID, "force": true}, false)
		pid := runText(v, "id")
		expectedPasses[pid] = caller.RequestID
		ended := h.waitPass(caller, pid)
		if mode == "refuse-result" || strings.HasPrefix(mode, "http-") {
			if runText(ended, "reason") != store.ReasonPrompts {
				t.Fatal(ended)
			}
		} else if runText(ended, "status") != store.StatusDone {
			t.Fatal(ended)
		}
	}
	// Brief output is record text too: provoke decoding, failed-run storage,
	// facts projection and rendering with every marker in each text field.
	memberIDs := []string{}
	for _, member := range members {
		var candidate map[string]json.RawMessage
		if err := json.Unmarshal(member["candidate"], &candidate); err != nil {
			t.Fatal(err)
		}
		mid := runText(candidate, "id")
		memberIDs = append(memberIDs, mid)
		url := runText(member, "website_url")
		call("add_page", map[string]any{"candidate": mid, "url": url + "/brief-input"}, false)
	}
	marked := strings.Join(forbidden, " ")
	output, err := json.Marshal(map[string]any{
		"summary":    marked,
		"categories": []map[string]string{{"name": sources.Categories()[0], "why": marked}, {"name": marked, "why": marked}},
		"labels":     []string{marked},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"decoded", "nonzero", "malformed"} {
		pf.mu.Lock()
		pf.mode, pf.stdout, pf.exitCode = "success", string(output), 0
		if mode == "nonzero" {
			pf.exitCode = 1
		}
		if mode == "malformed" {
			pf.stdout = marked
		}
		pf.mu.Unlock()
		v := call("enrich", map[string]any{"kind": enrichers.KindBrief, "cohort": cohortID, "force": true}, false)
		pid := runText(v, "id")
		expectedPasses[pid] = caller.RequestID
		ended := h.waitPass(caller, pid)
		if runText(ended, "status") != store.StatusDone {
			t.Fatal(ended)
		}
		db, records := runStore(t, h.p.Dir, h.p.Now)
		for _, mid := range memberIDs {
			es, err := records.Enrichments(t.Context(), mid)
			if err != nil {
				t.Fatal(err)
			}
			var newest *enrichers.BriefFound
			for _, result := range es {
				if result.Kind != enrichers.KindBrief {
					continue
				}
				var found enrichers.BriefFound
				if err := json.Unmarshal(result.Found, &found); err != nil {
					t.Fatal(err)
				}
				newest = &found
				break
			}
			if newest == nil {
				t.Fatal("brief result missing")
			}
			if mode == "malformed" {
				if newest.Summary != "" || len(newest.Categories) != 0 || len(newest.Labels) != 0 {
					t.Fatal(newest)
				}
			} else if newest.Summary != marked || len(newest.Categories) != 1 || newest.Categories[0].Why != marked || len(newest.Labels) != 2 || newest.Labels[0] != marked || newest.Labels[1] != marked || newest.Failed() != (mode == "nonzero") {
				t.Fatal("stored brief output mismatch", newest)
			}
			facts, err := records.GetFacts(t.Context(), mid)
			if err != nil || facts.Brief != marked {
				t.Fatal("facts omitted brief", facts, err)
			}
			detail := call("candidate", map[string]any{"candidate": mid}, false)
			data, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), marked) {
				t.Fatal("brief readback omitted markers")
			}
			status, body := h.request("GET", "/candidates/"+mid, "brief-candidate-page", nil)
			if status != 200 || !strings.Contains(string(body), marked) {
				t.Fatal("brief candidate page omitted markers")
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		call("cohort", map[string]any{"cohort": cohortID}, false)
		if status, _ := h.request("GET", "/cohorts/"+cohortID, "brief-cohort-page", nil); status != 200 {
			t.Fatal(status)
		}
	}
	status, body := h.request("GET", pagePath, "kept-page-request", nil)
	if status != 200 || !strings.Contains(strings.ToLower(string(body)), strings.ToLower(markers[4])) {
		t.Fatal("kept text page missing", status, pagePath)
	}
	call("delete_cohort", map[string]any{"cohort": cohortID}, false)
	call("create_cohort", map[string]any{"name": markers[3]}, false)
	answers := [][]byte{}
	delivery := func(eventID, runID, status string) {
		t.Helper()
		e := events.Event{ID: eventID, Time: h.p.Now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": "fmcsa", "run": runID, "status": status}, Seq: 1, Received: h.p.Now()}
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var env map[string]any
		_ = json.Unmarshal(b, &env)
		env["attempt"] = 1
		b, _ = json.Marshal(env)
		_, answer := h.request("POST", "/events", "delivery-request", b)
		answers = append(answers, answer)
	}
	delivery("evt_abababababababab", "run_fixture", "failed")
	f.set("record_failure")
	delivery("evt_acacacacacacacac", "run_failure", "done")
	// The failed record read creates a failed pass synchronously.
	ps := call("passes", map[string]any{}, false)
	var passList []map[string]json.RawMessage
	_ = json.Unmarshal(ps["passes"], &passList)
	found := false
	for _, p := range passList {
		if runText(p, "trigger") == "run_failure" {
			found = true
			expectedPasses[runText(p, "id")] = "delivery-request"
			if runText(p, "reason") != store.ReasonScrapes {
				t.Fatal("record failure did not fail as scrapes")
			}
		}
	}
	if !found {
		t.Fatal("record failure pass missing")
	}
	// Each list has two entries here, and every returned paging handle is searched.
	for _, name := range []string{"cohorts", "candidates", "passes"} {
		first := call(name, map[string]any{"limit": 1}, false)
		token := runText(first, "next")
		if token == "" {
			t.Fatalf("%s missing paging handle", name)
		}
		forbidden = append(forbidden, token)
		if name == "cohorts" {
			status, _ := h.request("GET", "/?cohorts_page="+url.QueryEscape(token), "paged-landing", nil)
			if status != 200 {
				t.Fatal("paged landing failed")
			}
		}
		second := call(name, map[string]any{"limit": 1, "page": token}, false)
		if token = runText(second, "next"); token != "" {
			forbidden = append(forbidden, token)
		}
		call(name, map[string]any{"limit": 1, "page": markers[0]}, true)
	}
	f.set("rows_failure")
	rebuild(store.ReasonScrapes)
	f.set("malformed")
	rebuild(store.ReasonScrapes)
	f.set("")
	// A successful delivery's row read is distinct from the failed record delivery.
	delivery("evt_adadadadadadadad", "run_fixture", "done")
	ps = call("passes", map[string]any{}, false)
	_ = json.Unmarshal(ps["passes"], &passList)
	for _, p := range passList {
		if runText(p, "trigger") == "run_fixture" {
			h.waitPass(caller, runText(p, "id"))
			expectedPasses[runText(p, "id")] = "delivery-request"
		}
	}
	hold := make(chan struct{})
	arrived := make(chan struct{}, 1)
	f.mu.Lock()
	f.hold = hold
	f.arrived = arrived
	f.mu.Unlock()
	caller.RequestID = "interrupt-request"
	working := call("rebuild", map[string]any{}, false)
	expectedPasses[runText(working, "id")] = caller.RequestID
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted rows read never started")
	}

	for _, method := range []string{"GET", "POST"} {
		_, answer := h.request(method, "/declarations", "declarations-request", nil)
		answers = append(answers, answer)
	}
	h.stop()
	close(hold)
	_, st := runStore(t, h.p.Dir, h.p.Now)
	ended, err := st.GetPass(context.Background(), runText(working, "id"))
	if err != nil || ended.Status != store.StatusFailed || ended.Reason != store.ReasonInterrupted {
		t.Fatal("held pass did not end interrupted")
	}
	allPasses, err := st.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range allPasses {
		if p.Origin == store.OriginPass {
			parent, err := st.GetPass(t.Context(), p.Trigger)
			if err != nil || p.RequestID != parent.RequestID {
				t.Fatal(p, parent, err)
			}
			expectedPasses[p.ID] = p.RequestID
		}
	}
	stamp := h.p.Now().UTC().Truncate(time.Microsecond)
	trailPasses, busPasses := map[string]int{}, map[string]int{}
	interruptRequests, interruptReads := map[string]int{}, map[string]int{}
	for _, e := range h.telemetry.Events() {
		secrecyAbsent(t, "trail event", e, forbidden)
		if !e.Time.Equal(stamp) {
			t.Fatal("trail capture clock mismatch")
		}
		if e.Name == "pass.started" || e.Name == "pass.finished" {
			pid, _ := e.Attrs["pass"].(string)
			q, known := expectedPasses[pid]
			if !known || e.RequestID != q {
				t.Fatal("unexpected captured trail pass")
			}
			trailPasses[e.Name+"|"+pid]++
		}
		if e.RequestID == "interrupt-request" {
			interruptRequests[e.Name]++
			if e.Name == "sibling.called" {
				path, _ := e.Attrs["path"].(string)
				interruptReads[path]++
			}
		}
	}
	for _, e := range h.bus.Events() {
		secrecyAbsent(t, "bus event", e, forbidden)
		if !e.Time.Equal(stamp) {
			t.Fatal("bus capture clock mismatch")
		}
		pid, _ := e.Attrs["pass"].(string)
		q, known := expectedPasses[pid]
		if !known || e.RequestID != q {
			t.Fatal("unexpected captured bus pass")
		}
		busPasses[e.Name+"|"+pid]++
	}
	for pid := range expectedPasses {
		for _, name := range []string{"pass.started", "pass.finished"} {
			key := name + "|" + pid
			if trailPasses[key] != 1 || busPasses[key] != 1 {
				t.Fatalf("pass capture missing %s: trail=%d bus=%d", key, trailPasses[key], busPasses[key])
			}
		}
	}
	for _, name := range []string{"request.started", "request.finished", "tool.called"} {
		if interruptRequests[name] != 1 {
			t.Fatalf("interrupted pass request capture %s=%d", name, interruptRequests[name])
		}
	}
	for _, path := range []string{"/scrapers/fmcsa/current", "/runs/run_fixture/rows"} {
		if interruptReads[path] != 1 {
			t.Fatalf("interrupted pass read capture %s=%d", path, interruptReads[path])
		}
	}

	secrecyAbsent(t, "stdout", h.out.String(), forbidden)
	secrecyAbsent(t, "stderr", h.err.String(), forbidden)
	for _, r := range refusals {
		secrecyAbsent(t, "refused tool result", r, forbidden)
	}
	for _, answer := range answers {
		secrecyAbsent(t, "bus answer", answer, forbidden)
	}
}
