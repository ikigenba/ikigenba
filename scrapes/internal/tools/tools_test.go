package tools_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
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
	runsDir string
	t       *testing.T
	d       *db.DB
	st      *store.Store
	eng     *runs.Engine
	sc      *fake
	client  *mcp.Client
	caller  identity.Caller
	now     time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return setupRegistry(t, nil)
}

func setupRegistry(t *testing.T, reg *scrapers.Registry) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{t: t, sc: &fake{}, caller: identity.Caller{UserID: "caller-one", Email: "caller-one@example.invalid", RequestID: "request-one"}, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	var err error
	f.d, err = db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "test.db"), Migrations: scrapes.Migrations(), Now: func() time.Time { return f.now }, Service: "scrapes", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	f.runsDir = t.TempDir()
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: &random{}, Runs: f.runsDir})
	if reg == nil {
		reg = scrapers.New(f.sc)
	}
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Version: "fixture", Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: bytes.NewReader(make([]byte, 65536)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	engineNow := f.now
	f.eng = runs.New(runs.Config{Store: f.st, Scrapers: reg, Telemetry: writer, Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard, Now: func() time.Time { return engineNow }, Budget: 1 << 30, PressureLimit: 1 << 30})
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

	switch status {
	case store.StatusDone:
		r, err = f.st.FinishRun(f.t.Context(), r.ID, loadRows(f.t, f.st, r.ID, rows))
	case store.StatusFailed:
		r, err = f.st.FailRun(f.t.Context(), r.ID, store.ReasonSource)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// R-8OCA-EGXT R-8QS3-60F7 R-8RZZ-JS5W R-08WA-CLHF R-EELB-ES2D R-EKOT-BMRU R-ELWP-PEIJ
func TestCatalogAndRegistration(t *testing.T) {
	f := setup(t)
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
	fields := [][]string{{}, {"scraper"}, {"scraper", "page", "limit"}, {"run"}, {"run", "scraper", "name", "page", "limit"}, {"run", "source_id"}}
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
	dp, _ := json.Marshal(paging.PageDescription)
	dl, _ := json.Marshal(paging.LimitDescription)
	wantSchema := `{"type":"object","properties":{"scraper":{"type":"string"},"page":{"type":"string","description":` + string(dp) + `},"limit":{"type":"integer","description":` + string(dl) + `}},"additionalProperties":false}`
	if string(list[2].InputSchema) != wantSchema {
		t.Fatalf("runs schema got %s want %s", list[2].InputSchema, wantSchema)
	}

	wantRowsSchema := `{"type":"object","properties":{"run":{"type":"string"},"scraper":{"type":"string"},"name":{"type":"string"},"page":{"type":"string","description":` + string(dp) + `},"limit":{"type":"integer","description":` + string(dl) + `}},"additionalProperties":false}`
	if string(list[4].InputSchema) != wantRowsSchema {
		t.Fatalf("rows schema got %s want %s", list[4].InputSchema, wantRowsSchema)
	}

	_ = tools.NoArgs{}
	_ = tools.ScrapeArgs{"fmcsa"}
	_ = tools.RunsArgs{"", nil, nil}
	_ = tools.RunArgs{"invalid"}
	_ = tools.RowsArgs{"", "fmcsa", "", nil, nil}
	_ = tools.RowArgs{"invalid", "one"}
	for _, args := range []any{map[string]any{"page": -1}, map[string]any{"page": uint64(1) << 32}, map[string]any{"page": 1.5}} {
		if !f.call("rows", args).IsError() {
			t.Fatal(args)
		}
	}
	if !f.call("scrape", tools.NoArgs{}).IsError() {
		t.Fatal("missing required")
	}
}

// R-8UFS-BBNA R-8WVL-2V4O R-0BC3-44YT
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
	token := "chosen-token"
	r := runs.Record{ID: "example"}
	w := source.Row{SourceID: "one"}
	rb, _ := json.Marshal(r)
	wb, _ := json.Marshal(w)
	for _, tc := range []struct {
		v    any
		want string
	}{{tools.RunList{[]runs.Record{r}, &token}, `{"runs":[` + string(rb) + `],"next":"` + token + `"}`}, {tools.RowList{[]source.Row{w}, &token}, `{"rows":[` + string(wb) + `],"next":"` + token + `"}`}, {tools.Scraper{"fmcsa", &r, &r}, `{"name":"fmcsa","current":` + string(rb) + `,"running":` + string(rb) + `}`}} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%s %v", b, err)
		}
	}
}

// R-RNMF-EJBU R-EI90-K3AG
func TestScrapers(t *testing.T) {
	f := setupRegistry(t, scrapers.Default(scrapers.Config{Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }}))
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{Name: "fmcsa"}, {Name: "mn_parcels"}, {Name: "ppp"}}})
	done := f.add(store.StatusDone, 1)
	running := f.add(store.StatusRunning, 0)
	currentRecord, runningRecord := runs.RecordOf(done), runs.RecordOf(running)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{"fmcsa", &currentRecord, &runningRecord}, {Name: "mn_parcels"}, {Name: "ppp"}}})
	pppDone, err := f.st.AddRun(t.Context(), store.Run{Scraper: "ppp", UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	pppDone, err = f.st.FinishRun(t.Context(), pppDone.ID, loadRows(t, f.st, pppDone.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	pppRunning, err := f.st.AddRun(t.Context(), store.Run{Scraper: "ppp", UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	pppCurrentRecord, pppRunningRecord := runs.RecordOf(pppDone), runs.RecordOf(pppRunning)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{"fmcsa", &currentRecord, &runningRecord}, {Name: "mn_parcels"}, {"ppp", &pppCurrentRecord, &pppRunningRecord}}})
	parcelDone, err := f.st.AddRun(t.Context(), store.Run{Scraper: "mn_parcels", UserID: "parcel-owner"})
	if err != nil {
		t.Fatal(err)
	}
	parcelDone, err = f.st.FinishRun(t.Context(), parcelDone.ID, loadRows(t, f.st, parcelDone.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	parcelRunning, err := f.st.AddRun(t.Context(), store.Run{Scraper: "mn_parcels", UserID: "parcel-owner"})
	if err != nil {
		t.Fatal(err)
	}
	parcelCurrentRecord, parcelRunningRecord := runs.RecordOf(parcelDone), runs.RecordOf(parcelRunning)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{"fmcsa", &currentRecord, &runningRecord}, {"mn_parcels", &parcelCurrentRecord, &parcelRunningRecord}, {"ppp", &pppCurrentRecord, &pppRunningRecord}}})
}

// R-0HFL-0ZOA
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
	next := paging.Token{Tool: "runs"}.Encode(records[24].Started, records[24].ID)
	f.success("runs", tools.NoArgs{}, tools.RunList{records[:25], &next})
	f.success("runs", tools.RunsArgs{Page: &next}, tools.RunList{Runs: records[25:]})
	f.success("runs", tools.RunsArgs{Scraper: "unknown"}, tools.RunList{})
}

// R-8ZBD-UEM2 R-95EV-R9BJ R-9F62-TF93 R-EWVT-5C6S R-ES07-M980
func TestRowsAndLookup(t *testing.T) {
	f := setup(t)
	f.refused("rows", tools.RowsArgs{Scraper: "fmcsa"}, runs.RefusalNoRun, tools.RefuseNoRun)
	for _, a := range []tools.RowsArgs{{}, {Scraper: "unknown"}, {Run: "invalid"}, {Run: "run_ffffffffffffffff"}} {
		f.refused("rows", a, runs.RefusalNotFound, tools.RefuseNotFound)
	}
	done := f.add(store.StatusDone, 30)
	all, err := f.st.ListRowsAfter(t.Context(), done.ID, "", "", 31)
	if err != nil {
		t.Fatal(err)
	}
	next := (paging.Token{Tool: "rows"}).Encode(all[24].SourceID)
	first := tools.RowList{all[:25], &next}
	f.success("rows", tools.RowsArgs{Run: done.ID}, first)
	f.success("rows", tools.RowsArgs{Scraper: "fmcsa"}, first)
	f.success("rows", tools.RowsArgs{Run: done.ID, Scraper: "fmcsa", Name: "bUsInEsS"}, first)
	f.success("rows", tools.RowsArgs{Run: done.ID, Page: &next}, tools.RowList{Rows: all[25:]})
	f.success("rows", tools.RowsArgs{Run: done.ID, Page: new((paging.Token{Tool: "rows"}).Encode("zzz"))}, tools.RowList{})
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
	rows, err := f.st.ListRowsAfter(t.Context(), exact.ID, "", "", 26)
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

// R-UKN1-6JN6 R-ULUX-KBDV R-UN2T-Y34K R-EPKE-UPQM
func TestStartRefusals(t *testing.T) {
	f := setup(t)
	seen := map[string]bool{}
	for _, s := range []string{tools.RefuseNotFound, tools.RefuseBusy, tools.RefuseNoRun, tools.RefuseStopping, tools.RefuseUnavailable, tools.RefuseMissing} {
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

// R-97UO-ISSX
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

// R-EZBL-WVO6 R-9HLV-KYQH
func TestReadToolsShareAndPreserveRecords(t *testing.T) {
	f := setup(t)
	done := f.add(store.StatusDone, 2)
	running := f.add(store.StatusRunning, 1)
	beforeRuns, err := f.st.ListRunsAfter(t.Context(), "", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows, err := f.st.ListRowsAfter(t.Context(), done.ID, "", "", 100)
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
	}{{"scrapers", tools.NoArgs{}}, {"runs", tools.RunsArgs{}}, {"run", tools.RunArgs{done.ID}}, {"rows", tools.RowsArgs{Run: done.ID}}, {"row", tools.RowArgs{done.ID, "000"}}, {"run", tools.RunArgs{"invalid"}}, {"rows", tools.RowsArgs{}}, {"rows", tools.RowsArgs{Run: done.ID, Page: new("bogus")}}, {"rows", tools.RowsArgs{Run: done.ID, Limit: new(int(0))}}, {"row", tools.RowArgs{done.ID, "absent"}}} {
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
	afterRuns, err := f.st.ListRunsAfter(t.Context(), "", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	afterRows, err := f.st.ListRowsAfter(t.Context(), done.ID, "", "", 100)
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
	counts := loadRows(t, f.st, record.ID, []source.Row{row})
	if _, err := f.st.FinishRun(t.Context(), record.ID, counts); err != nil {
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

// R-0DRV-VOG7 R-0EZS-9G6W R-0G7O-N7XL R-UN2T-Y34K
func TestRunsPagingRefusals(t *testing.T) {
	f := setup(t)
	token := paging.Token{Tool: "runs"}
	pages := []string{"bogus", token.Encode("2026-10-05T09:00:07Z"), token.Encode(int64(1759654807), "run_5a5a5a5a5a5a5a5a"), (paging.Token{Tool: "run"}).Encode("2026-10-05T09:00:07Z", "run_5a5a5a5a5a5a5a5a")}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
			_, err := paging.Window(&limit)
			for _, page := range append([]string{""}, pages...) {
				f.refusedText("runs", tools.RunsArgs{Scraper: "unknown", Page: &page, Limit: &limit}, err.Error())
			}
		}
		for _, page := range pages {
			for _, limit := range []*int{nil, new(int(1)), new(int(paging.MaxLimit))} {
				f.refusedText("runs", tools.RunsArgs{Page: &page, Limit: limit}, paging.BadPage)
			}
		}
	}
	f.d.SetFailing(false)
	f.success("runs", map[string]any{"page": nil, "limit": nil}, tools.RunList{})
	for _, args := range []any{map[string]any{"page": 0}, map[string]any{"limit": "1"}, map[string]any{"limit": 1.5}, json.RawMessage(`{"limit":9223372036854775808}`)} {
		result := f.call("runs", args)
		if !result.IsError() {
			t.Fatal(args)
		}
	}
}

func (f *fixture) refusedText(name string, args any, text string) {
	f.t.Helper()
	result := f.call(name, args)
	w := decode(f.t, result)
	if !result.IsError() || w.Structured != nil || w.Content[0].Text != text {
		f.t.Fatalf("text refusal: %+v", w)
	}
}

// R-0HFL-0ZOA
func TestRunsWindowsAcrossEqualStartedTimes(t *testing.T) {
	f := setup(t)
	records := []runs.Record{}
	for i := 0; i < 30; i++ {
		if i%6 != 0 {
			f.now = f.now.Add(-time.Second)
		}
		records = append(records, runs.RecordOf(f.add(store.StatusDone, 0)))
	}
	// A second scraper's newest run participates only in the unfiltered list.
	f.now = f.now.Add(time.Second)
	other, err := f.st.AddRun(t.Context(), store.Run{Scraper: "other", UserID: "other-owner"})
	if err != nil {
		t.Fatal(err)
	}
	all := append(append([]runs.Record{}, records...), runs.RecordOf(other))
	sort.Slice(all, func(i, j int) bool {
		if all[i].Started != all[j].Started {
			return all[i].Started > all[j].Started
		}
		return all[i].ID > all[j].ID
	})
	for _, scraper := range []string{"", "fmcsa", "other", "unknown"} {
		expected := []runs.Record{}
		for _, r := range all {
			if scraper == "" || r.Scraper == scraper {
				expected = append(expected, r)
			}
		}
		for _, limit := range []int{1, 2, 7, 25, paging.MaxLimit} {
			var page *string
			for offset := 0; ; offset += limit {
				end := min(offset+limit, len(expected))
				want := tools.RunList{Runs: expected[offset:end]}
				if end < len(expected) {
					last := expected[end-1]
					next := (paging.Token{Tool: "runs"}).Encode(last.Started, last.ID)
					want.Next = &next
				}
				f.success("runs", tools.RunsArgs{Scraper: scraper, Page: page, Limit: &limit}, want)
				if want.Next == nil {
					break
				}
				page = want.Next
			}
		}
	}
	empty := ""
	next := (paging.Token{Tool: "runs"}).Encode(all[paging.DefaultLimit-1].Started, all[paging.DefaultLimit-1].ID)
	want := tools.RunList{all[:paging.DefaultLimit], &next}
	for _, args := range []any{tools.NoArgs{}, map[string]any{"page": nil, "limit": nil}, tools.RunsArgs{Page: &empty}} {
		f.success("runs", args, want)
	}
}

// R-9AAH-ACAB R-9CQA-1VRP R-UN2T-Y34K
func TestRowsPagingRefusals(t *testing.T) {
	f := setup(t)
	r := f.add(store.StatusDone, 1)
	token := paging.Token{Tool: "rows"}
	pages := []string{"bogus", token.Encode("one", "two"), token.Encode(int64(25)), (paging.Token{Tool: "row"}).Encode("one"), (paging.Token{Tool: "runs"}).Encode("2026-10-05T09:00:07Z", r.ID)}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, run := range []string{"", "bogus", r.ID} {
			for _, scraper := range []string{"", "unknown", "fmcsa"} {
				for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
					_, err := paging.Window(&limit)
					for _, page := range append([]string{""}, pages...) {
						f.refusedText("rows", tools.RowsArgs{Run: run, Scraper: scraper, Name: "chosen", Page: &page, Limit: &limit}, err.Error())
					}
				}
				for _, page := range pages {
					for _, limit := range []*int{nil, new(int(1)), new(int(paging.MaxLimit))} {
						f.refusedText("rows", tools.RowsArgs{Run: run, Scraper: scraper, Page: &page, Limit: limit}, paging.BadPage)
					}
				}
			}
		}
	}
	f.d.SetFailing(false)
	for _, args := range []any{map[string]any{"page": 0}, map[string]any{"limit": "1"}, map[string]any{"limit": 1.5}, json.RawMessage(`{"limit":9223372036854775808}`)} {
		if !f.call("rows", args).IsError() {
			t.Fatal(args)
		}
	}
}

// R-8ZBD-UEM2 R-9F62-TF93
func TestRowsWindows(t *testing.T) {
	f := setup(t)
	r := f.add(store.StatusDone, 30)
	all, err := f.st.ListRowsAfter(t.Context(), r.ID, "", "", 31)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "bUsInEsS", "alternate", "absent"} {
		expected := all
		if name == "absent" {
			expected = []source.Row{}
		}
		for _, limit := range []int{1, 2, 7, paging.DefaultLimit, paging.MaxLimit} {
			var page *string
			for offset := 0; ; offset += limit {
				end := min(offset+limit, len(expected))
				want := tools.RowList{Rows: expected[offset:end]}
				if end < len(expected) {
					next := (paging.Token{Tool: "rows"}).Encode(expected[end-1].SourceID)
					want.Next = &next
				}
				f.success("rows", tools.RowsArgs{Run: r.ID, Name: name, Page: page, Limit: &limit}, want)
				if want.Next == nil {
					break
				}
				page = want.Next
			}
		}
	}
	next := (paging.Token{Tool: "rows"}).Encode(all[paging.DefaultLimit-1].SourceID)
	want := tools.RowList{all[:paging.DefaultLimit], &next}
	for _, args := range []any{map[string]any{"run": r.ID, "page": nil, "limit": nil}, tools.RowsArgs{Run: r.ID, Page: new("")}} {
		f.success("rows", args, want)
	}
}

type namedScraper struct {
	name string
	fake
}

func (s *namedScraper) Name() string { return s.name }

// R-RNMF-EJBU
func TestScrapersInjectedRegistry(t *testing.T) {
	reg := scrapers.New(&namedScraper{name: "zeta"}, &namedScraper{name: "alpha"}, &namedScraper{name: "middle"})
	f := setupRegistry(t, reg)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{{Name: "alpha"}, {Name: "middle"}, {Name: "zeta"}}})
	add := func(name string, done bool) runs.Record {
		t.Helper()
		f.now = f.now.Add(time.Second)
		r, err := f.st.AddRun(t.Context(), store.Run{Scraper: name, UserID: "registry-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if done {
			r, err = f.st.FinishRun(t.Context(), r.ID, loadRows(t, f.st, r.ID, nil))
			if err != nil {
				t.Fatal(err)
			}
		}
		return runs.RecordOf(r)
	}
	alphaCurrent := add("alpha", true)
	middleRunning := add("middle", false)
	zetaCurrent := add("zeta", true)
	zetaRunning := add("zeta", false)
	add("outside-registry", true)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{Scrapers: []tools.Scraper{
		{Name: "alpha", Current: &alphaCurrent},
		{Name: "middle", Running: &middleRunning},
		{Name: "zeta", Current: &zetaCurrent, Running: &zetaRunning},
	}})
}

// R-RNMF-EJBU
func TestScrapersEmptyRegistry(t *testing.T) {
	f := setupRegistry(t, scrapers.New())
	f.add(store.StatusDone, 0)
	f.success("scrapers", tools.NoArgs{}, tools.ScraperList{})
}

func loadRows(t *testing.T, st *store.Store, id string, rows []source.Row) store.Counts {
	t.Helper()
	step := func(context.Context, int64, time.Duration) error { return nil }
	c, err := st.Collect(t.Context(), id, step)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Add(t.Context(), rows); err != nil {
		t.Fatal(err)
	}
	if err = c.Seal(t.Context()); err != nil {
		t.Fatal(err)
	}
	counts, err := c.Load(t.Context(), step)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// R-SULF-SCH1 R-UPIM-PMLY R-ULUX-KBDV R-UN2T-Y34K
func TestRunFileToolRefusals(t *testing.T) {
	f := setup(t)
	done := f.add(store.StatusDone, paging.DefaultLimit+1)
	unchanged := map[string]mcp.Result{}
	for _, name := range []string{"runs", "scrapers"} {
		unchanged[name] = f.call(name, tools.NoArgs{})
	}
	record := runs.RecordOf(done)
	file := filepath.Join(f.runsDir, done.ID+".db")
	for _, missing := range []bool{true, false} {
		if missing {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(file, []byte("broken fixture"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		word, sentence := runs.RefusalUnavailable, tools.RefuseUnavailable
		if missing {
			word, sentence = runs.RefusalMissing, tools.RefuseMissing
		}
		token := paging.Token{Tool: "rows"}.Encode("000")
		for _, a := range []tools.RowsArgs{{Run: done.ID}, {Scraper: "fmcsa"}, {Run: done.ID, Name: "chosen", Page: &token}} {
			f.refused("rows", a, word, sentence)
		}
		for _, sid := range []string{"000", "never-held"} {
			f.refused("row", tools.RowArgs{Run: done.ID, SourceID: sid}, word, sentence)
		}
		f.success("run", tools.RunArgs{Run: done.ID}, record)
		for name, want := range unchanged {
			if !reflect.DeepEqual(f.call(name, tools.NoArgs{}), want) {
				t.Fatal("catalog changed", name)
			}
		}
		zero := 0
		_, limitError := paging.Window(&zero)
		r := f.call("rows", tools.RowsArgs{Run: done.ID, Limit: &zero})
		if !r.IsError() || decode(t, r).Content[0].Text != limitError.Error() {
			t.Fatal("limit precedence")
		}
		bogus := "bogus"
		r = f.call("rows", tools.RowsArgs{Run: done.ID, Page: &bogus})
		if !r.IsError() || decode(t, r).Content[0].Text != paging.BadPage {
			t.Fatal("page precedence")
		}
	}
}
