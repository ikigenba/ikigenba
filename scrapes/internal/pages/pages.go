// Package pages renders the service's read-only browser pages.
package pages

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/tools"
	"github.com/ikigenba/ikigenba/scrapes/internal/urls"
)

// ServiceName names the service in the banner and runtime.
const ServiceName = "scrapes"

// Description is the service description shared with its manifest.
const Description = "Public records about businesses in range, scraped per source as rows per run"

// MinuteLayout formats stored times on the landing page.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout formats stored times on record pages.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// ToolsLevel locates the tools page in the banner trail.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel locates the about screen in the banner trail.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Set holds the embedded page templates and shared chrome.
type Set struct{ templates *template.Template }

// Load parses the embedded templates independently of the working directory.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(app.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{t}, nil
}

// Write renders the named template and suppresses the body for HEAD.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var b bytes.Buffer
	if err := s.templates.ExecuteTemplate(&b, name, data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.Bytes())
	}
}

// Config supplies the banner, renderer, read-only store and registry.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	Scrapers     *scrapers.Registry
}

// LandingData is the landing template input.
type LandingData struct {
	Banner   page.Banner
	Scrapers []ScraperRow
	Runs     []RunRow
	Page     string
	Prev     string
	Next     string
}

// ScraperRow describes a registered scraper and its visible runs.
type ScraperRow struct {
	Name    string
	Current *CurrentRun
	Running *RunningRun
}

// CurrentRun summarizes the newest completed run.
type CurrentRun struct {
	ID, Status, Started, StartedText, Finished, FinishedText string
	Rows, New, Changed, Gone                                 int64
}

// RunningRun identifies the running run.
type RunningRun struct{ ID string }

// RunRow summarizes a stored run for the landing page.
type RunRow struct {
	ID, Scraper, Status, Reason, StartedBy, Trigger, Started, StartedText, Finished, FinishedText string
	Rows                                                                                          int64
}

// RunData is the run template input.
type RunData struct {
	Banner  page.Banner
	Run     RunView
	Rows    []RowLine
	Page    string
	Prev    string
	Next    string
	Missing bool
}

// RunView holds the complete visible run record.
type RunView struct {
	ID, Scraper, Status, Reason, StartedBy, Trigger, Started, StartedText, Finished, FinishedText string
	Rows, New, Changed, Unchanged, Gone                                                           int64
}

// RowLine summarizes a row in the run page table.
type RowLine struct{ SourceID, Name, City, State, County string }

// RowData is the row template input.
type RowData struct {
	Banner page.Banner
	Row    RowView
}

// RowView holds the visible envelope and formatted raw record.
type RowView struct {
	Scraper, Run, SourceID, Scraped, ScrapedText, Digest, Name string
	Names                                                      []string
	Street, City, State, Zip, County, Lat, Lon                 string
	People                                                     []PersonRow
	Facts                                                      []FactRow
	Raw                                                        string
}

// PersonRow describes a party named by a source.
type PersonRow struct{ Name, Role string }

// FactRow describes a source fact as compact JSON text.
type FactRow struct{ Key, Value string }

// ToolsData is the tools template input.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow describes a catalog entry.
type ToolRow struct{ Name, Description string }

// AboutData is the about template input.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData is the not-found template input.
type NotFoundData struct{ Banner page.Banner }

func stamp(t time.Time, layout string) (string, string) {
	if t.IsZero() {
		return "", ""
	}
	return t.UTC().Format(time.RFC3339), t.UTC().Format(layout)
}
func landingRow(r store.Run) RunRow {
	v := RunRow{ID: r.ID, Scraper: r.Scraper, Status: r.Status, Reason: r.Reason, Trigger: r.Trigger, Rows: r.Rows}
	if r.Trigger == "" {
		v.StartedBy = r.UserEmail
	}
	v.Started, v.StartedText = stamp(r.Started, MinuteLayout)
	v.Finished, v.FinishedText = stamp(r.Finished, MinuteLayout)
	return v
}
func currentView(r store.Run) CurrentRun {
	v := landingRow(r)
	return CurrentRun{v.ID, v.Status, v.Started, v.StartedText, v.Finished, v.FinishedText, r.Rows, r.New, r.Changed, r.Gone}
}
func runView(r store.Run) RunView {
	v := landingRow(r)
	x := RunView{ID: v.ID, Scraper: v.Scraper, Status: v.Status, Reason: v.Reason, StartedBy: v.StartedBy, Trigger: v.Trigger, Rows: r.Rows, New: r.New, Changed: r.Changed, Unchanged: r.Unchanged, Gone: r.Gone}
	x.Started, x.StartedText = stamp(r.Started, SecondLayout)
	x.Finished, x.FinishedText = stamp(r.Finished, SecondLayout)
	return x
}
func rowLine(w source.Row) RowLine {
	return RowLine{w.SourceID, w.Name, w.Address.City, w.Address.State, w.Address.County}
}
func rowView(w source.Row) RowView {
	v := RowView{Scraper: w.Scraper, Run: w.Run, SourceID: w.SourceID, Digest: w.Digest, Name: w.Name, Names: w.Names, Street: w.Address.Street, City: w.Address.City, State: w.Address.State, Zip: w.Address.Zip, County: w.Address.County, People: []PersonRow{}, Facts: []FactRow{}}
	v.Scraped, v.ScrapedText = stamp(w.Scraped, SecondLayout)
	if w.Lat != nil {
		v.Lat = strconv.FormatFloat(*w.Lat, 'f', -1, 64)
	}
	if w.Lon != nil {
		v.Lon = strconv.FormatFloat(*w.Lon, 'f', -1, 64)
	}
	for _, p := range w.People {
		v.People = append(v.People, PersonRow{p.Name, p.Role})
	}
	var facts map[string]json.RawMessage
	_ = json.Unmarshal(w.Facts, &facts)
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var b bytes.Buffer
		_ = json.Compact(&b, facts[k])
		v.Facts = append(v.Facts, FactRow{k, b.String()})
	}
	var raw bytes.Buffer
	_ = json.Indent(&raw, w.Raw, "", "  ")
	v.Raw = raw.String()
	return v
}
func paths(p string) (id, sourceID string, isRun, isRow bool) {
	if !strings.HasPrefix(p, "/runs/") {
		return
	}
	tail := strings.TrimPrefix(p, "/runs/")
	if store.ValidRunID(tail) {
		return tail, "", true, false
	}
	parts := strings.Split(tail, "/")
	if len(parts) != 3 || !store.ValidRunID(parts[0]) || parts[1] != "rows" || parts[2] == "" {
		return
	}
	s, err := url.PathUnescape(parts[2])
	if err != nil {
		return
	}
	return parts[0], s, false, true
}

// Handler serves stored runs and rows without invoking a source.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.EscapedPath()
		id, sid, isRun, isRow := paths(p)
		known := p == "/" || p == "/tools" || p == "/about" || isRun || isRow
		if known && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		auth := urls.Auth(r, cfg.ServicesPath)
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		notfound := func() { cfg.Pages.Write(w, r, 404, "notfound", NotFoundData{b}) }
		refusal := func(line string) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(503)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, line+"\n")
			}
		}
		var pageValue, started, position string
		backward, lastPage := false, false
		if p == "/" || isRun {
			pageValue = r.URL.Query().Get("page")
			if pageValue != "" {
				after, before := "runs", "runs-before"
				keys := []any{&started, &position}
				if isRun {
					after, before = "rows", "rows-before"
					keys = []any{&position}
				}
				if (paging.Token{Tool: after}).Decode(pageValue, keys...) != nil {
					switch {
					case (paging.Token{Tool: before}).Decode(pageValue, keys...) == nil:
						backward = true
					case pageValue == "last":
						backward, lastPage = true, true
						started, position = "", ""
					default:
						notfound()
						return
					}
				}
			}
		}
		redirect := func(path string) { w.Header().Set("Location", path); w.WriteHeader(http.StatusSeeOther) }
		ctx := r.Context()
		switch {
		case p == "/":
			b.Trail = nil
			d := LandingData{b, []ScraperRow{}, []RunRow{}, pageValue, "", ""}
			for _, n := range cfg.Scrapers.Names() {
				x := ScraperRow{Name: n}
				current, err := cfg.Store.CurrentRun(ctx, n)
				if err == nil {
					v := currentView(current)
					x.Current = &v
				} else if !errors.Is(err, store.ErrNotFound) {
					refusal(store.Unreachable)
					return
				}
				running, err := cfg.Store.RunningRun(ctx, n)
				if err == nil {
					x.Running = &RunningRun{running.ID}
				} else if !errors.Is(err, store.ErrNotFound) {
					refusal(store.Unreachable)
					return
				}
				d.Scrapers = append(d.Scrapers, x)
			}
			var rs []store.Run
			var err error
			if backward {
				rs, err = cfg.Store.ListRunsBefore(ctx, "", started, position, paging.DefaultLimit+1)
			} else {
				rs, err = cfg.Store.ListRunsAfter(ctx, "", started, position, paging.DefaultLimit+1)
			}
			if err != nil {
				refusal(store.Unreachable)
				return
			}
			more := len(rs) > paging.DefaultLimit
			if backward && !more {
				redirect("/")
				return
			}
			if more {
				rs = rs[:paging.DefaultLimit]
			}
			if backward {
				slices.Reverse(rs)
			}
			if len(rs) > 0 {
				first, last := rs[0], rs[len(rs)-1]
				if pageValue != "" {
					d.Prev = paging.Token{Tool: "runs-before"}.Encode(first.Started.UTC().Format(time.RFC3339), first.ID)
				}
				if !lastPage && (more || backward) {
					d.Next = paging.Token{Tool: "runs"}.Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
				}
			}
			for _, x := range rs {
				d.Runs = append(d.Runs, landingRow(x))
			}
			cfg.Pages.Write(w, r, 200, "landing", d)
		case isRun:
			x, err := cfg.Store.GetRun(ctx, id)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				refusal(store.Unreachable)
				return
			}
			b.Trail = []page.Level{{Name: id, URL: "/runs/" + id}}
			d := RunData{Banner: b, Run: runView(x), Rows: []RowLine{}, Page: pageValue}
			readFailure := func(err error) {
				if errors.Is(err, store.ErrMissing) {
					d.Page, d.Prev, d.Next, d.Missing = "", "", "", true
					cfg.Pages.Write(w, r, 503, "run", d)
				} else {
					refusal(store.Unreachable)
				}
			}
			if x.Status == store.StatusDone {
				var rows []source.Row
				if backward {
					rows, err = cfg.Store.ListRowsBefore(ctx, id, "", position, paging.DefaultLimit+1)
					if err != nil {
						readFailure(err)
						return
					}
					if len(rows) <= paging.DefaultLimit {
						redirect("/runs/" + id)
						return
					}
					rows = rows[:paging.DefaultLimit]
					slices.Reverse(rows)
					if !lastPage {
						d.Next = paging.Token{Tool: "rows"}.Encode(rows[len(rows)-1].SourceID)
					}
				} else {
					result, readErr := tools.RowsPage(ctx, cfg.Store, id, "", position, paging.DefaultLimit)
					if readErr != nil {
						readFailure(readErr)
						return
					}
					rows = result.Rows
					if result.Next != nil {
						d.Next = *result.Next
					}
				}
				if pageValue != "" && len(rows) > 0 {
					d.Prev = paging.Token{Tool: "rows-before"}.Encode(rows[0].SourceID)
				}
				for _, row := range rows {
					d.Rows = append(d.Rows, rowLine(row))
				}
			}
			cfg.Pages.Write(w, r, 200, "run", d)
		case isRow:
			x, err := cfg.Store.GetRow(ctx, id, sid)
			if errors.Is(err, store.ErrMissing) {
				refusal(store.Missing)
				return
			}
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				refusal(store.Unreachable)
				return
			}
			b.Trail = []page.Level{{Name: id, URL: "/runs/" + id}, {Name: sid, URL: "/runs/" + id + "/rows/" + url.PathEscape(sid)}}
			cfg.Pages.Write(w, r, 200, "row", RowData{b, rowView(x)})
		case p == "/tools":
			b.Trail = []page.Level{ToolsLevel}
			d := ToolsData{b, []ToolRow{}}
			for _, e := range tools.Catalog() {
				d.Tools = append(d.Tools, ToolRow{e.Name, e.Description})
			}
			cfg.Pages.Write(w, r, 200, "tools", d)
		case p == "/about":
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, 200, "about", AboutData{b, Description})
		default:
			notfound()
		}
	})
}
