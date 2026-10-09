// Package pages renders the prompt catalog and run records.
package pages

import (
	"html/template"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// ServiceName is the platform service name.
const ServiceName = "prompts"

// Description is the service's one-line description.
const Description = "Prompts run by an agent over the suite's models"

// MinuteLayout formats a table time.
const MinuteLayout = "2006-01-02 15:04"

// CardLayout formats a prompt's creation time.
const CardLayout = "2006-01-02 15:04:05"

// SecondLayout formats a run time.
const SecondLayout = "2006-01-02 15:04:05"

// Set is the immutable page template set.
type Set struct{ templates *template.Template }

// Load parses the embedded templates over the platform templates.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(prompts.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write sends one named page with the caller's status and headers.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = s.templates.ExecuteTemplate(w, name, data)
	}
}

// LandingData is the catalog page data.
type LandingData struct {
	Banner  page.Banner
	Prompts []PromptRow
}

// PromptRow is one catalog row.
type PromptRow struct {
	Name, URL, Model string
	LastRun          *RunRow
}

// RunRow is one run table row.
type RunRow struct {
	ID, URL, Status                 string
	ExitCode                        int
	Model, Cost, Started, StartedAt string
	Duration                        *Duration
}

// Duration holds whole elapsed minutes and remaining seconds.
type Duration struct{ Minutes, Seconds int64 }

// PromptData is one prompt's page data.
type PromptData struct {
	Banner        page.Banner
	Prompt        PromptCard
	Runs          []RunRow
	Subscriptions []string
}

// PromptCard is a prompt's stored fields and retention values.
type PromptCard struct {
	ID, Name, Model                          string
	Tools                                    []ToolGroup
	Text, System, Schema, Created, CreatedAt string
	RunsKept                                 int
	KeepNewest, KeepDays                     int64
}

// ToolGroup is one permitted group and its position.
type ToolGroup struct {
	Name        string
	First, Last bool
}

// RunData is one run's page data.
type RunData struct {
	Banner                page.Banner
	Prompt                PromptLink
	Run                   RunCard
	Input, Stdout, Stderr *FileText
	Transcript            *FileLink
	Files                 []FileRow
}

// PromptLink links back to the parent prompt.
type PromptLink struct{ Name, URL string }

// RunCard holds a run's summary and file state.
type RunCard struct {
	ID, URL, Status                                 string
	ExitCode                                        int
	Running                                         bool
	Model, Started, StartedAt, Finished, FinishedAt string
	Duration                                        *Duration
	Trigger, Event, User, Request                   string
	Usage                                           *Usage
	StdoutSize, StderrSize, TranscriptSize          Size
	Truncated, StdoutTruncated, StderrTruncated     bool
	Failure                                         *Failure
	FilesGone                                       bool
}

// Usage holds formatted usage counts and cost.
type Usage struct{ Calls, ToolCalls, InputTokens, CachedTokens, OutputTokens, ReasoningTokens, Cost string }

// Failure holds the recorded reason.
type Failure struct{ Reason string }

// FileText holds a record file's bytes and address.
type FileText struct {
	Size      Size
	Text, URL string
}

// FileLink links a transcript without rendering its bytes.
type FileLink struct {
	Size Size
	URL  string
}

// FileRow is one regular output file.
type FileRow struct {
	Path string
	Size Size
	URL  string
}

// Size holds a scaled byte count.
type Size struct{ Number, Unit string }

// AboutData is the about page data.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NoticeData is the data of a footer-only notice page.
type NoticeData struct{ Banner page.Banner }

// Config provides the read-only page dependencies.
type Config struct {
	Banner              func(u page.User) page.Banner
	Pages               *Set
	ServicesPath        string
	Store               *store.Store
	Runs                *runs.Core
	KeepDays, KeepCount int64
}
