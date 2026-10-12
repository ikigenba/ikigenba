package tools_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/tools"
)

type random struct{ n uint64 }

func (r *random) Read(p []byte) (int, error) {
	r.n++
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], r.n)
	for i := range p {
		p[i] = b[i%8]
	}
	return len(p), nil
}

type fake struct{ calls atomic.Int32 }

func (*fake) Name() string { return "fmcsa" }
func (f *fake) Scrape(ctx context.Context, _ func([]source.Row) error) error {
	f.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

type fixture struct {
	t      *testing.T
	d      *db.DB
	st     *store.Store
	eng    *runs.Engine
	sc     *fake
	client *mcp.Client
	caller identity.Caller
	now    time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{t: t, sc: &fake{}, caller: identity.Caller{UserID: "caller-one", Email: "caller-one@example.invalid", RequestID: "request-one"}, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	var err error
	f.d, err = db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "test.db"), Migrations: scrapes.Migrations(), Now: func() time.Time { return f.now }, Service: "scrapes", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: &random{}})
	reg := scrapers.New(f.sc)
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Version: "fixture", Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: bytes.NewReader(make([]byte, 65536)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	f.eng = runs.New(runs.Config{Store: f.st, Scrapers: reg, Telemetry: writer})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "scrapes", Version: "fixture", Telemetry: writer})
	tools.Register(srv, tools.Config{f.eng, reg, f.st})
	server := httptest.NewServer(identity.Require(srv))
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client(), Name: "test", Version: "fixture"})
	t.Cleanup(func() {
		f.eng.Drain(context.Background())
		server.Close()
		writer.Shutdown(context.Background(), "fixture")
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

type wire struct {
	Content    []struct{ Type, Text string }
	Structured json.RawMessage `json:"structuredContent"`
}

func (f *fixture) call(name string, args any) mcp.Result {
	f.t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	result, err := f.client.CallTool(f.t.Context(), f.caller, name, b)
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}
func decode(t *testing.T, r mcp.Result) wire {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var w wire
	if err = json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Content) != 1 || w.Content[0].Type != "text" {
		t.Fatalf("content: %s", b)
	}
	return w
}
func (f *fixture) success(name string, args, expected any) {
	f.t.Helper()
	r := f.call(name, args)
	if r.IsError() {
		f.t.Fatalf("unexpected refusal: %+v", decode(f.t, r))
	}
	w := decode(f.t, r)
	b, err := json.Marshal(expected)
	if err != nil {
		f.t.Fatal(err)
	}
	if w.Content[0].Text != string(b) {
		f.t.Fatalf("%s text got %s want %s", name, w.Content[0].Text, b)
	}
	var got, want any
	if err = json.Unmarshal(w.Structured, &got); err != nil {
		f.t.Fatal(err)
	}
	if err = json.Unmarshal(b, &want); err != nil {
		f.t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		f.t.Fatalf("structured mismatch %s", w.Structured)
	}
}
func (f *fixture) refused(name string, args any, word, sentence string) {
	f.t.Helper()
	r := f.call(name, args)
	w := decode(f.t, r)
	if !r.IsError() || w.Structured != nil || w.Content[0].Text != word+": "+sentence {
		f.t.Fatalf("refusal: %+v", w)
	}
}
func (f *fixture) add(status string, n int) store.Run {
	f.t.Helper()
	f.now = f.now.Add(time.Second)
	r, err := f.st.AddRun(f.t.Context(), store.Run{Scraper: "fmcsa", UserID: "owner", UserEmail: "owner@example.invalid"})
	if err != nil {
		f.t.Fatal(err)
	}
	rows := make([]source.Row, n)
	for i := range rows {
		rows[i] = source.Row{SourceID: fmt.Sprintf("%03d", i), Collected: f.now, Name: "chosen-business", Names: []string{"Chosen Business", "Alternate"}, Digest: fmt.Sprint(i), Facts: json.RawMessage(`{"chosen":true}`), Raw: json.RawMessage(fmt.Sprintf(`{"number":%d}`, i))}
	}
	if err = f.st.PutRows(f.t.Context(), r.ID, rows); err != nil {
		f.t.Fatal(err)
	}
	switch status {
	case store.StatusDone:
		r, err = f.st.FinishRun(f.t.Context(), r.ID)
	case store.StatusFailed:
		r, err = f.st.FailRun(f.t.Context(), r.ID, store.ReasonSource)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// R-E9PP-VP3L R-EAXM-9GUA R-EELB-ES2D R-EKOT-BMRU R-ELWP-PEIJ
func TestCatalogAndRegistration(t *testing.T) {
	f := setup(t)
	if tools.PageSize != 25 {
		t.Fatal(tools.PageSize)
	}
	catalog := tools.Catalog()
	want := []tools.Entry{{"scrapers", tools.DescribeScrapers}, {"scrape", tools.DescribeScrape}, {"runs", tools.DescribeRuns}, {"run", tools.DescribeRun}, {"rows", tools.DescribeRows}, {"row", tools.DescribeRow}}
	if !reflect.DeepEqual(catalog, want) {
		t.Fatal(catalog)
	}
	catalog[0].Name = "changed"
	if !reflect.DeepEqual(tools.Catalog(), want) {
		t.Fatal("shared catalog")
	}
	list, err := f.client.ListTools(t.Context(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 6 {
		t.Fatal(list)
	}
	fields := [][]string{{}, {"scraper"}, {"scraper", "page"}, {"run"}, {"run", "scraper", "name", "page"}, {"run", "source_id"}}
	required := [][]string{{}, {"scraper"}, {}, {"run"}, {}, {"run", "source_id"}}
	seen := map[string]bool{}
	for i, tool := range list {
		entry := want[i]
		if tool.Name != entry.Name || tool.Description != entry.Description || len(tool.OutputSchema) != 0 {
			t.Fatal(tool)
		}
		effect := mcp.Read
		if i == 1 {
			effect = mcp.Additive
		}
		if tool.Effect() != effect {
			t.Fatal(tool.Annotations)
		}
		d := entry.Description
		if seen[d] || len(d) > 120 || !strings.HasSuffix(d, ".") || strings.Contains(d, "\n") || strings.Contains(d, ". ") || strings.TrimSpace(d) != d {
			t.Fatal(d)
		}
		seen[d] = true
		var schema struct {
			Properties map[string]json.RawMessage
			Required   []string
		}
		if err = json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if len(schema.Properties) != len(fields[i]) {
			t.Fatal(string(tool.InputSchema))
		}
		last := -1
		for _, field := range fields[i] {
			property := strings.Index(string(tool.InputSchema), `"`+field+`":`)
			if property <= last {
				t.Fatal("property order", string(tool.InputSchema))
			}
			last = property
			if _, ok := schema.Properties[field]; !ok {
				t.Fatal(field)
			}
		}
		if len(schema.Required) != len(required[i]) {
			t.Fatal(schema.Required)
		}
		for j, field := range required[i] {
			if schema.Required[j] != field {
				t.Fatal(schema.Required)
			}
		}
	}
	_ = tools.NoArgs{}
	_ = tools.ScrapeArgs{"fmcsa"}
	_ = tools.RunsArgs{"", 0}
	_ = tools.RunArgs{"invalid"}
	_ = tools.RowsArgs{"", "fmcsa", "", 0}
	_ = tools.RowArgs{"invalid", "one"}
	for _, args := range []any{map[string]any{"page": -1}, map[string]any{"page": uint64(1) << 32}, map[string]any{"page": 1.5}} {
		if !f.call("runs", args).IsError() {
			t.Fatal(args)
		}
	}
	if !f.call("scrape", tools.NoArgs{}).IsError() {
		t.Fatal("missing required")
	}
}

// R-EDDF-10BO
func TestListEncoding(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{{tools.ScraperList{}, `{"scrapers":[]}`}, {tools.RunList{}, `{"runs":[]}`}, {tools.RowList{}, `{"rows":[]}`}, {tools.Scraper{"fmcsa", nil, nil}, `{"name":"fmcsa"}`}} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%s %v", b, err)
		}
	}
	n := int64(1)
	r := runs.Record{ID: "example"}
	w := source.Row{SourceID: "one"}
	rb, _ := json.Marshal(r)
	wb, _ := json.Marshal(w)
	for _, tc := range []struct {
		v    any
		want string
	}{{tools.RunList{[]runs.Record{r}, &n}, `{"runs":[` + string(rb) + `],"next":1}`}, {tools.RowList{[]source.Row{w}, &n}, `{"rows":[` + string(wb) + `],"next":1}`}, {tools.Scraper{"fmcsa", &r, &r}, `{"name":"fmcsa","current":` + string(rb) + `,"running":` + string(rb) + `}`}} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%s %v", b, err)
		}
	}
}

// R-EOCI-GXZX R-EI90-K3AG
func TestScrapers(t *testing.T) {
	f := setup(t)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{Name: "fmcsa"}}})
	done := f.add(store.StatusDone, 1)
	running := f.add(store.StatusRunning, 0)
	currentRecord, runningRecord := runs.RecordOf(done), runs.RecordOf(running)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{"fmcsa", &currentRecord, &runningRecord}}})
}

// R-ES42-OOZT R-ETBZ-2GQI
func TestRunPages(t *testing.T) {
	f := setup(t)
	records := []runs.Record{}
	for i := 0; i < 30; i++ {
		r := f.add(store.StatusDone, 0)
		records = append([]runs.Record{runs.RecordOf(r)}, records...)
		if i == 24 {
			f.success("runs", tools.NoArgs{}, tools.RunList{Runs: records})
		}
	}
	next := int64(1)
	f.success("runs", tools.NoArgs{}, tools.RunList{records[:25], &next})
	f.success("runs", tools.RunsArgs{Page: 1}, tools.RunList{Runs: records[25:]})
	for _, args := range []tools.RunsArgs{{Scraper: "unknown"}, {Page: 171798692}} {
		f.success("runs", args, tools.RunList{})
	}
}

// R-ET84-00YP R-EUJV-G8H7 R-EWVT-5C6S R-ES07-M980
func TestRowsAndLookup(t *testing.T) {
	f := setup(t)
	f.refused("rows", tools.RowsArgs{Scraper: "fmcsa"}, runs.RefusalNoRun, tools.RefuseNoRun)
	for _, a := range []tools.RowsArgs{{}, {Scraper: "unknown"}, {Run: "invalid"}, {Run: "run_ffffffffffffffff"}} {
		f.refused("rows", a, runs.RefusalNotFound, tools.RefuseNotFound)
	}
	done := f.add(store.StatusDone, 30)
	all, err := f.st.ListRows(t.Context(), done.ID, "", 0, 31)
	if err != nil {
		t.Fatal(err)
	}
	next := int64(1)
	first := tools.RowList{all[:25], &next}
	f.success("rows", tools.RowsArgs{Run: done.ID}, first)
	f.success("rows", tools.RowsArgs{Scraper: "fmcsa"}, first)
	f.success("rows", tools.RowsArgs{Run: done.ID, Scraper: "fmcsa", Name: "bUsInEsS"}, first)
	f.success("rows", tools.RowsArgs{Run: done.ID, Page: 1}, tools.RowList{Rows: all[25:]})
	f.success("rows", tools.RowsArgs{Run: done.ID, Page: 171798692}, tools.RowList{})
	f.success("rows", tools.RowsArgs{Run: done.ID, Name: "absent"}, tools.RowList{})
	f.refused("rows", tools.RowsArgs{Run: done.ID, Scraper: "other"}, runs.RefusalNotFound, tools.RefuseNotFound)
	f.success("run", tools.RunArgs{Run: done.ID}, runs.RecordOf(done))
	raw, err := f.st.GetRow(t.Context(), done.ID, all[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	f.success("row", tools.RowArgs{done.ID, raw.SourceID}, raw)
	for _, id := range []string{"invalid", "run_ffffffffffffffff"} {
		f.refused("run", tools.RunArgs{id}, runs.RefusalNotFound, tools.RefuseNotFound)
		f.refused("row", tools.RowArgs{id, "missing"}, runs.RefusalNotFound, tools.RefuseNotFound)
	}
	f.refused("row", tools.RowArgs{done.ID, "missing"}, runs.RefusalNotFound, tools.RefuseNotFound)
	for _, status := range []string{store.StatusFailed, store.StatusRunning} {
		r := f.add(status, 1)
		f.success("rows", tools.RowsArgs{Run: r.ID}, tools.RowList{})
		f.refused("row", tools.RowArgs{r.ID, "000"}, runs.RefusalNotFound, tools.RefuseNotFound)
		if status == store.StatusRunning {
			if _, err = f.st.FailRun(t.Context(), r.ID, store.ReasonInterrupted); err != nil {
				t.Fatal(err)
			}
		}
	}
	exact := f.add(store.StatusDone, 25)
	rows, err := f.st.ListRows(t.Context(), exact.ID, "", 0, 26)
	if err != nil {
		t.Fatal(err)
	}
	f.success("rows", tools.RowsArgs{Run: exact.ID}, tools.RowList{Rows: rows})
	if _, err = f.st.PruneRuns(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	f.refused("run", tools.RunArgs{done.ID}, runs.RefusalNotFound, tools.RefuseNotFound)
	f.refused("row", tools.RowArgs{done.ID, "000"}, runs.RefusalNotFound, tools.RefuseNotFound)
}

// R-EFT7-SJT2 R-EH14-6BJR R-EJGW-XV15 R-EPKE-UPQM
func TestStartRefusals(t *testing.T) {
	f := setup(t)
	seen := map[string]bool{}
	for _, s := range []string{tools.RefuseNotFound, tools.RefuseBusy, tools.RefuseNoRun, tools.RefuseStopping, tools.RefuseUnavailable} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal(s)
		}
		seen[s] = true
	}
	f.refused("scrape", tools.ScrapeArgs{"unknown"}, runs.RefusalNotFound, tools.RefuseNotFound)
	result := f.call("scrape", tools.ScrapeArgs{"fmcsa"})
	if result.IsError() {
		t.Fatal(decode(t, result))
	}
	w := decode(t, result)
	var r runs.Record
	if err := json.Unmarshal(w.Structured, &r); err != nil {
		t.Fatal(err)
	}
	stored, err := f.st.GetRun(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != store.StatusRunning || stored.UserID != f.caller.UserID || stored.UserEmail != f.caller.Email || stored.RequestID != f.caller.RequestID {
		t.Fatal(stored)
	}
	b, err := json.Marshal(runs.RecordOf(stored))
	if err != nil || w.Content[0].Text != string(b) {
		t.Fatalf("%s %v", b, err)
	}
	f.refused("scrape", tools.ScrapeArgs{"fmcsa"}, runs.RefusalBusy, tools.RefuseBusy)
	f.eng.Drain(t.Context())
	f.refused("scrape", tools.ScrapeArgs{"fmcsa"}, runs.RefusalStopping, tools.RefuseStopping)
}

// R-EY3P-J3XH
func TestStoreOutage(t *testing.T) {
	f := setup(t)
	r := f.add(store.StatusDone, 1)
	f.d.SetFailing(true)
	defer f.d.SetFailing(false)
	for _, tc := range []struct {
		name string
		args any
	}{{"scrapers", tools.NoArgs{}}, {"scrape", tools.ScrapeArgs{"fmcsa"}}, {"runs", tools.RunsArgs{}}, {"run", tools.RunArgs{r.ID}}, {"rows", tools.RowsArgs{Run: r.ID}}, {"rows", tools.RowsArgs{Scraper: "fmcsa"}}, {"row", tools.RowArgs{r.ID, "000"}}} {
		f.refused(tc.name, tc.args, runs.RefusalUnavailable, tools.RefuseUnavailable)
	}
	for _, tc := range []struct {
		name string
		args any
	}{{"run", tools.RunArgs{"invalid"}}, {"rows", tools.RowsArgs{Run: "invalid"}}, {"rows", tools.RowsArgs{Scraper: "unknown"}}, {"rows", tools.RowsArgs{}}, {"row", tools.RowArgs{"invalid", "000"}}} {
		f.refused(tc.name, tc.args, runs.RefusalNotFound, tools.RefuseNotFound)
	}
}

// R-EZBL-WVO6 R-F0JI-ANEV
func TestReadToolsShareAndPreserveRecords(t *testing.T) {
	f := setup(t)
	done := f.add(store.StatusDone, 2)
	running := f.add(store.StatusRunning, 1)
	beforeRuns, err := f.st.ListRuns(t.Context(), "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows, err := f.st.ListRows(t.Context(), done.ID, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	beforeRow, err := f.st.GetRow(t.Context(), done.ID, "000")
	if err != nil {
		t.Fatal(err)
	}
	beforeRun, err := f.st.GetRun(t.Context(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	caller := f.caller
	for _, tc := range []struct {
		name string
		args any
	}{{"scrapers", tools.NoArgs{}}, {"runs", tools.RunsArgs{}}, {"run", tools.RunArgs{done.ID}}, {"rows", tools.RowsArgs{Run: done.ID}}, {"row", tools.RowArgs{done.ID, "000"}}, {"run", tools.RunArgs{"invalid"}}, {"rows", tools.RowsArgs{}}, {"row", tools.RowArgs{done.ID, "absent"}}} {
		f.caller = caller
		a, err := json.Marshal(f.call(tc.name, tc.args))
		if err != nil {
			t.Fatal(err)
		}
		f.caller = identity.Caller{UserID: "second", Email: "second@example.invalid", RequestID: "second-request"}
		b, err := json.Marshal(f.call(tc.name, tc.args))
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("caller changed answer %s", tc.name)
		}
	}
	afterRuns, err := f.st.ListRuns(t.Context(), "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	afterRows, err := f.st.ListRows(t.Context(), done.ID, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	afterRow, err := f.st.GetRow(t.Context(), done.ID, "000")
	if err != nil {
		t.Fatal(err)
	}
	afterRun, err := f.st.GetRun(t.Context(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeRuns, afterRuns) || !reflect.DeepEqual(beforeRows, afterRows) || !reflect.DeepEqual(beforeRow, afterRow) || beforeRun != afterRun || f.sc.calls.Load() != 0 {
		t.Fatal("read changed records or scraped")
	}
}

// R-EZBL-WVO6
func TestReadToolsShareRunStartedByCaller(t *testing.T) {
	f := setup(t)
	started := f.call("scrape", tools.ScrapeArgs{Scraper: "fmcsa"})
	if started.IsError() {
		t.Fatal(decode(t, started))
	}
	var record runs.Record
	if err := json.Unmarshal(decode(t, started).Structured, &record); err != nil {
		t.Fatal(err)
	}
	row := source.Row{SourceID: "chosen-id", Collected: f.now, Name: "chosen-name", Names: []string{"chosen-name"}, Facts: json.RawMessage(`{}`), Raw: json.RawMessage(`{"chosen":true}`)}
	if err := f.st.PutRows(t.Context(), record.ID, []source.Row{row}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.FinishRun(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	first := f.caller
	for _, tc := range []struct {
		name string
		args any
	}{{"scrapers", tools.NoArgs{}}, {"runs", tools.RunsArgs{}}, {"run", tools.RunArgs{Run: record.ID}}, {"rows", tools.RowsArgs{Run: record.ID}}, {"row", tools.RowArgs{Run: record.ID, SourceID: row.SourceID}}} {
		f.caller = first
		a, err := json.Marshal(f.call(tc.name, tc.args))
		if err != nil {
			t.Fatal(err)
		}
		f.caller = identity.Caller{UserID: "another-caller", Email: "another@example.invalid", RequestID: "another-request"}
		b, err := json.Marshal(f.call(tc.name, tc.args))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("caller changed %s", tc.name)
		}
	}
}
