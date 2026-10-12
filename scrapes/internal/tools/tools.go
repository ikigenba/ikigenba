// Package tools presents the shared scrape records through MCP.
package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// Config supplies the engine, registry and shared store.
type Config struct {
	Runs     *runs.Engine
	Scrapers *scrapers.Registry
	Store    *store.Store
}

// Entry names a tool and its description.
type Entry struct{ Name, Description string }

// PageSize is the number of entries returned in one page.
const PageSize = 25

// Tool descriptions and refusal sentences are stable copy.
const (
	DescribeScrapers  = "List scrapers and their current and running runs."
	DescribeScrape    = "Start a run of a scraper in the background."
	DescribeRuns      = "List a page of runs, newest first."
	DescribeRun       = "Read one run and its outcome."
	DescribeRows      = "List a page of a run's row envelopes."
	DescribeRow       = "Read one row with its original source record."
	RefuseNotFound    = "The requested record was not found."
	RefuseBusy        = "This scraper already has a run in progress."
	RefuseNoRun       = "This scraper has no completed run."
	RefuseStopping    = "The service is stopping; try again later."
	RefuseUnavailable = "The database is unavailable; try again later."
)

// Catalog returns fresh tool summaries in registration order.
func Catalog() []Entry {
	return []Entry{{"scrapers", DescribeScrapers}, {"scrape", DescribeScrape}, {"runs", DescribeRuns}, {"run", DescribeRun}, {"rows", DescribeRows}, {"row", DescribeRow}}
}

// NoArgs is the empty argument object.
type NoArgs struct{}

// ScrapeArgs names the scraper to start.
type ScrapeArgs struct {
	Scraper string `json:"scraper" mcp:"required"`
}

// RunsArgs selects a scraper and page of runs.
type RunsArgs struct {
	Scraper string `json:"scraper"`
	Page    uint32 `json:"page"`
}

// RunArgs names a run.
type RunArgs struct {
	Run string `json:"run" mcp:"required"`
}

// RowsArgs selects a run, name filter and page of rows.
type RowsArgs struct {
	Run     string `json:"run"`
	Scraper string `json:"scraper"`
	Name    string `json:"name"`
	Page    uint32 `json:"page"`
}

// RowArgs names one source record in a run.
type RowArgs struct {
	Run      string `json:"run" mcp:"required"`
	SourceID string `json:"source_id" mcp:"required"`
}

// Scraper reports the current and running runs of one scraper.
type Scraper struct {
	Name    string       `json:"name"`
	Current *runs.Record `json:"current,omitempty"`
	Running *runs.Record `json:"running,omitempty"`
}

// ScraperList is the scraper catalog result.
type ScraperList struct {
	Scrapers []Scraper `json:"scrapers"`
}

// RunList is a page of run records.
type RunList struct {
	Runs []runs.Record `json:"runs"`
	Next *int64        `json:"next,omitempty"`
}

// RowList is a page of row envelopes.
type RowList struct {
	Rows []source.Row `json:"rows"`
	Next *int64       `json:"next,omitempty"`
}

// MarshalJSON encodes an absent list as an empty array.
func (v ScraperList) MarshalJSON() ([]byte, error) {
	type wire ScraperList
	if v.Scrapers == nil {
		v.Scrapers = []Scraper{}
	}
	return json.Marshal(wire(v))
}

// MarshalJSON encodes an absent list as an empty array.
func (v RunList) MarshalJSON() ([]byte, error) {
	type wire RunList
	if v.Runs == nil {
		v.Runs = []runs.Record{}
	}
	return json.Marshal(wire(v))
}

// MarshalJSON encodes an absent list as an empty array.
func (v RowList) MarshalJSON() ([]byte, error) {
	type wire RowList
	if v.Rows == nil {
		v.Rows = []source.Row{}
	}
	return json.Marshal(wire(v))
}
func refuse(word string) error {
	sentence := RefuseUnavailable
	switch word {
	case runs.RefusalNotFound:
		sentence = RefuseNotFound
	case runs.RefusalBusy:
		sentence = RefuseBusy
	case runs.RefusalNoRun:
		sentence = RefuseNoRun
	case runs.RefusalStopping:
		sentence = RefuseStopping
	default:
		word = runs.RefusalUnavailable
	}
	return errors.New(word + ": " + sentence)
}
func storeError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return refuse(runs.RefusalNotFound)
	}
	return refuse(runs.RefusalUnavailable)
}
func answer(v any) (mcp.Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.Result{}, refuse(runs.RefusalUnavailable)
	}
	text, err := json.Marshal(string(b))
	if err != nil {
		return mcp.Result{}, refuse(runs.RefusalUnavailable)
	}
	wire := struct {
		Content    []map[string]json.RawMessage `json:"content"`
		Structured json.RawMessage              `json:"structuredContent"`
	}{[]map[string]json.RawMessage{{"type": json.RawMessage(`"text"`), "text": text}}, b}
	payload, err := json.Marshal(wire)
	if err != nil {
		return mcp.Result{}, refuse(runs.RefusalUnavailable)
	}
	var result mcp.Result
	if err = json.Unmarshal(payload, &result); err != nil {
		return mcp.Result{}, refuse(runs.RefusalUnavailable)
	}
	return result, nil
}

// RunsPage projects one page of stored runs into public records.
func RunsPage(ctx context.Context, st *store.Store, scraper string, page uint32) (RunList, error) {
	list, err := st.ListRuns(ctx, scraper, int64(PageSize)*int64(page), PageSize+1)
	if err != nil {
		return RunList{}, err
	}
	result := RunList{Runs: make([]runs.Record, 0, PageSize)}
	if len(list) > PageSize {
		next := int64(page) + 1
		result.Next = &next
		list = list[:PageSize]
	}
	for _, r := range list {
		result.Runs = append(result.Runs, runs.RecordOf(r))
	}
	return result, nil
}

// RowsPage is shared by the rows tool and run page.
func RowsPage(ctx context.Context, st *store.Store, runID, name string, page uint32) (RowList, error) {
	list, err := st.ListRows(ctx, runID, name, int64(PageSize)*int64(page), PageSize+1)
	if err != nil {
		return RowList{}, err
	}
	result := RowList{Rows: list}
	if result.Rows == nil {
		result.Rows = []source.Row{}
	}
	if len(list) > PageSize {
		next := int64(page) + 1
		result.Next = &next
		result.Rows = list[:PageSize]
	}
	return result, nil
}
func resolveRows(ctx context.Context, cfg Config, a RowsArgs) (string, error) {
	if a.Run != "" {
		if !store.ValidRunID(a.Run) {
			return "", refuse(runs.RefusalNotFound)
		}
		r, err := cfg.Store.GetRun(ctx, a.Run)
		if err != nil {
			return "", storeError(err)
		}
		if a.Scraper != "" && a.Scraper != r.Scraper {
			return "", refuse(runs.RefusalNotFound)
		}
		return r.ID, nil
	}
	if a.Scraper == "" {
		return "", refuse(runs.RefusalNotFound)
	}
	if _, ok := cfg.Scrapers.Get(a.Scraper); !ok {
		return "", refuse(runs.RefusalNotFound)
	}
	r, err := cfg.Store.CurrentRun(ctx, a.Scraper)
	if errors.Is(err, store.ErrNotFound) {
		return "", refuse(runs.RefusalNoRun)
	}
	if err != nil {
		return "", storeError(err)
	}
	return r.ID, nil
}

// Register adds the six scrape tools to the MCP server.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddRawTool(srv, mcp.RawTool[NoArgs]{Name: "scrapers", Description: DescribeScrapers, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, _ NoArgs) (mcp.Result, error) {
		result := ScraperList{Scrapers: []Scraper{}}
		for _, name := range cfg.Scrapers.Names() {
			entry := Scraper{Name: name}
			r, err := cfg.Store.CurrentRun(ctx, name)
			if err == nil {
				record := runs.RecordOf(r)
				entry.Current = &record
			} else if !errors.Is(err, store.ErrNotFound) {
				return mcp.Result{}, storeError(err)
			}
			r, err = cfg.Store.RunningRun(ctx, name)
			if err == nil {
				record := runs.RecordOf(r)
				entry.Running = &record
			} else if !errors.Is(err, store.ErrNotFound) {
				return mcp.Result{}, storeError(err)
			}
			result.Scrapers = append(result.Scrapers, entry)
		}
		return answer(result)
	}})
	mcp.AddRawTool(srv, mcp.RawTool[ScrapeArgs]{Name: "scrape", Description: DescribeScrape, Effect: mcp.Additive, Handler: func(ctx context.Context, c identity.Caller, a ScrapeArgs) (mcp.Result, error) {
		r, err := cfg.Runs.Start(ctx, c, a.Scraper)
		if err != nil {
			var refusal *runs.Refusal
			if errors.As(err, &refusal) {
				return mcp.Result{}, refuse(refusal.Word)
			}
			return mcp.Result{}, refuse(runs.RefusalUnavailable)
		}
		return answer(runs.RecordOf(r))
	}})
	mcp.AddRawTool(srv, mcp.RawTool[RunsArgs]{Name: "runs", Description: DescribeRuns, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, a RunsArgs) (mcp.Result, error) {
		result, err := RunsPage(ctx, cfg.Store, a.Scraper, a.Page)
		if err != nil {
			return mcp.Result{}, storeError(err)
		}
		return answer(result)
	}})
	mcp.AddRawTool(srv, mcp.RawTool[RunArgs]{Name: "run", Description: DescribeRun, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, a RunArgs) (mcp.Result, error) {
		if !store.ValidRunID(a.Run) {
			return mcp.Result{}, refuse(runs.RefusalNotFound)
		}
		r, err := cfg.Store.GetRun(ctx, a.Run)
		if err != nil {
			return mcp.Result{}, storeError(err)
		}
		return answer(runs.RecordOf(r))
	}})
	mcp.AddRawTool(srv, mcp.RawTool[RowsArgs]{Name: "rows", Description: DescribeRows, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, a RowsArgs) (mcp.Result, error) {
		id, err := resolveRows(ctx, cfg, a)
		if err != nil {
			return mcp.Result{}, err
		}
		result, err := RowsPage(ctx, cfg.Store, id, a.Name, a.Page)
		if err != nil {
			return mcp.Result{}, storeError(err)
		}
		return answer(result)
	}})
	mcp.AddRawTool(srv, mcp.RawTool[RowArgs]{Name: "row", Description: DescribeRow, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, a RowArgs) (mcp.Result, error) {
		if !store.ValidRunID(a.Run) {
			return mcp.Result{}, refuse(runs.RefusalNotFound)
		}
		r, err := cfg.Store.GetRow(ctx, a.Run, a.SourceID)
		if err != nil {
			return mcp.Result{}, storeError(err)
		}
		return answer(r)
	}})
}
