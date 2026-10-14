// Package tools presents the shared scrape records through MCP.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
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
	RefuseMissing     = "The run rows are missing; scrape again."
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
	Scraper string  `json:"scraper"`
	Page    *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit   *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// RunArgs names a run.
type RunArgs struct {
	Run string `json:"run" mcp:"required"`
}

// RowsArgs selects a run, name filter and page of rows.
type RowsArgs struct {
	Run     string  `json:"run"`
	Scraper string  `json:"scraper"`
	Name    string  `json:"name"`
	Page    *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit   *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
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
	Next *string       `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
}

// RowList is a page of row envelopes.
type RowList struct {
	Rows []source.Row `json:"rows"`
	Next *string      `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
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
	case runs.RefusalMissing:
		sentence = RefuseMissing
	case runs.RefusalStopping:
		sentence = RefuseStopping
	default:
		word = runs.RefusalUnavailable
	}
	return errors.New(word + ": " + sentence)
}
func storeError(err error) error {
	if errors.Is(err, store.ErrMissing) {
		return refuse(runs.RefusalMissing)
	}
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

// RowsPage is shared by the rows tool and run page.
func RowsPage(ctx context.Context, st *store.Store, runID, name, after string, limit int) (RowList, error) {
	list, err := st.ListRowsAfter(ctx, runID, name, after, limit+1)
	if err != nil {
		return RowList{}, err
	}
	result := RowList{Rows: list}
	if result.Rows == nil {
		result.Rows = []source.Row{}
	}
	if len(list) > limit {
		next := (paging.Token{Tool: "rows"}).Encode(list[limit-1].SourceID)
		result.Next = &next
		result.Rows = list[:limit]
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
		size, err := paging.Window(a.Limit)
		if err != nil {
			return mcp.Result{}, err
		}
		token := paging.Token{Tool: "runs"}
		var started, id string
		if a.Page != nil && *a.Page != "" {
			if err = token.Decode(*a.Page, &started, &id); err != nil {
				return mcp.Result{}, errors.New(paging.BadPage)
			}
		}
		list, err := cfg.Store.ListRunsAfter(ctx, a.Scraper, started, id, size+1)
		if err != nil {
			return mcp.Result{}, storeError(err)
		}
		result := RunList{Runs: make([]runs.Record, 0, size)}
		if len(list) > size {
			list = list[:size]
			last := list[len(list)-1]
			next := token.Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			result.Next = &next
		}
		for _, r := range list {
			result.Runs = append(result.Runs, runs.RecordOf(r))
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
		size, err := paging.Window(a.Limit)
		if err != nil {
			return mcp.Result{}, err
		}
		var after string
		if a.Page != nil && *a.Page != "" {
			if err = (paging.Token{Tool: "rows"}).Decode(*a.Page, &after); err != nil {
				return mcp.Result{}, errors.New(paging.BadPage)
			}
		}
		id, err := resolveRows(ctx, cfg, a)
		if err != nil {
			return mcp.Result{}, err
		}
		result, err := RowsPage(ctx, cfg.Store, id, a.Name, after, size)
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
