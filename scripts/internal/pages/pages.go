// Package pages renders scripts' embedded templates and read-only pages.
package pages

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// ServiceName names this service.
const ServiceName = "scripts"

// Description describes this service in its manifest.
const Description string = "Python scripts run from the suite's repositories"

// Set holds the parsed embedded page templates.
type Set struct{ templates *template.Template }

// Load parses scripts' assets into appkit's template set.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(scripts.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{t}, nil
}

// Write sends a named page, preserving its caller's headers.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var b bytes.Buffer
	if r.Method != http.MethodHead {
		_ = s.templates.ExecuteTemplate(&b, name, data)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.Bytes())
	}
}

// LandingData is the caller's catalog page.
type LandingData struct {
	Banner  page.Banner
	Scripts []ScriptRow
}

// ScriptRow is one catalog entry.
type ScriptRow struct {
	Name, URL string
	Repo      Repo
	Ref       string
	LastRun   *RunRow
}

// Repo holds the repository id and its current name.
type Repo struct{ ID, Name string }

// RunRow is a formatted run in a table.
type RunRow struct {
	ID, URL, Status            string
	ExitCode                   int
	Commit, Started, StartedAt string
	Duration                   *Duration
}

// Duration carries whole minutes and the remaining seconds.
type Duration struct{ Minutes, Seconds int64 }

// Size carries a scaled byte count and its unit.
type Size struct{ Number, Unit string }

// ScriptData is a script and its retained runs.
type ScriptData struct {
	Banner        page.Banner
	Script        ScriptCard
	Runs          []RunRow
	Subscriptions []string
}

// ScriptCard is the formatted script record and retention rule.
type ScriptCard struct {
	ID, Name                string
	Repo                    Repo
	Ref, Created, CreatedAt string
	RunsKept                int
	KeepNewest, KeepDays    int64
}

// RunData is a run record and its kept files.
type RunData struct {
	Banner                page.Banner
	Script                ScriptLink
	Run                   RunCard
	Input, Stdout, Stderr *FileText
	Files                 []FileRow
}

// ScriptLink identifies the run's parent script.
type ScriptLink struct{ Name, URL string }

// RunCard is the formatted headline and details of a run.
type RunCard struct {
	ID, URL, Status                                       string
	ExitCode                                              int
	Running                                               bool
	Commit, Ref, Started, StartedAt, Finished, FinishedAt string
	Duration                                              *Duration
	Trigger, Event, User, Request                         string
	StdoutSize, StderrSize                                Size
	Truncated                                             bool
	Failure                                               *Failure
	FilesGone                                             bool
}

// Failure carries the recorded reason and the values its template may show.
type Failure struct {
	Reason                         string
	Repo                           Repo
	Ref                            string
	TreeMaxBytes, OperationSeconds int64
}

// FileText carries a kept text file and its download address.
type FileText struct {
	Size      Size
	Text, URL string
}

// FileRow carries one regular output file.
type FileRow struct {
	Path string
	Size Size
	URL  string
}

// AboutData describes this service.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// ToolsData lists the server's registered tools.
type ToolsData struct {
	Banner page.Banner
	Tools  []Tool
}

// Tool carries a registered tool's name and description.
type Tool struct{ Name, Description string }

// NoticeData carries only the notice footer.
type NoticeData struct{ Banner page.Banner }

// Config supplies the page handler's read-only dependencies.
type Config struct {
	Banner                                              func(u page.User) page.Banner
	Pages                                               *Set
	ServicesPath                                        string
	Store                                               *store.Store
	Source                                              *source.Source
	Runs                                                *runs.Core
	MCP                                                 *mcp.Server
	KeepDays, KeepCount, TreeMaxBytes, OperationSeconds int64
}
