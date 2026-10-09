package pages_test

import (
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// R-B8S9-4J4Y R-B7KC-QRE9 R-BA05-IAVN R-BB81-W2MC R-BCFY-9UD1 R-BDNU-NM3Q
// R-BEVR-1DUF R-BG3N-F5L4 R-BHBJ-SXBT R-BIJG-6P2I R-BJRC-KGT7 R-BKZ8-Y8JW
// R-BNF1-PS1A R-BOMY-3JRZ R-BPUU-HBIO R-BR2Q-V39D R-BSAN-8V02 R-BTIJ-MMQR
// R-BUQG-0EHG R-BVYC-E685 R-YDCR-D0OY R-YEKN-QSFN
// Converting each contract shape checks field names, types, and order by use.
func TestDataShapes(t *testing.T) {
	_ = pages.LandingData(struct {
		Banner  page.Banner
		Prompts []pages.PromptRow
	}{})
	_ = pages.PromptRow(struct {
		Name, URL, Model string
		LastRun          *pages.RunRow
	}{})
	_ = pages.RunRow(struct {
		ID, URL, Status                 string
		ExitCode                        int
		Model, Cost, Started, StartedAt string
		Duration                        *pages.Duration
	}{})
	_ = pages.Duration(struct{ Minutes, Seconds int64 }{})
	_ = pages.PromptData(struct {
		Banner        page.Banner
		Prompt        pages.PromptCard
		Runs          []pages.RunRow
		Subscriptions []string
	}{})
	_ = pages.PromptCard(struct {
		ID, Name, Model                          string
		Tools                                    []pages.ToolGroup
		Text, System, Schema, Created, CreatedAt string
		RunsKept                                 int
		KeepNewest, KeepDays                     int64
	}{})
	_ = pages.ToolGroup(struct {
		Name        string
		First, Last bool
	}{})
	_ = pages.RunData(struct {
		Banner                page.Banner
		Prompt                pages.PromptLink
		Run                   pages.RunCard
		Input, Stdout, Stderr *pages.FileText
		Transcript            *pages.FileLink
		Files                 []pages.FileRow
	}{})
	_ = pages.PromptLink(struct{ Name, URL string }{})
	_ = pages.RunCard(struct {
		ID, URL, Status                                 string
		ExitCode                                        int
		Running                                         bool
		Model, Started, StartedAt, Finished, FinishedAt string
		Duration                                        *pages.Duration
		Trigger, Event, User, Request                   string
		Usage                                           *pages.Usage
		StdoutSize, StderrSize, TranscriptSize          pages.Size
		Truncated, StdoutTruncated, StderrTruncated     bool
		Failure                                         *pages.Failure
		FilesGone                                       bool
	}{})
	_ = pages.Usage(struct{ Calls, ToolCalls, InputTokens, CachedTokens, OutputTokens, ReasoningTokens, Cost string }{})
	_ = pages.Failure(struct{ Reason string }{})
	_ = pages.FileText(struct {
		Size      pages.Size
		Text, URL string
	}{})
	_ = pages.FileLink(struct {
		Size pages.Size
		URL  string
	}{})
	_ = pages.FileRow(struct {
		Path string
		Size pages.Size
		URL  string
	}{})
	_ = pages.Size(struct{ Number, Unit string }{})
	_ = pages.AboutData(struct {
		Banner      page.Banner
		Description string
	}{})
	// R-YRZJ-Y9LA R-YT7G-C1BZ
	_ = pages.ToolsData(struct {
		Banner page.Banner
		Tools  []pages.Tool
	}{})
	_ = pages.Tool(struct{ Name, Description string }{})
	_ = pages.NoticeData(struct{ Banner page.Banner }{})
	_ = pages.Config(struct {
		Banner              func(u page.User) page.Banner
		Pages               *pages.Set
		ServicesPath        string
		Store               *store.Store
		Runs                *runs.Core
		MCP                 *mcp.Server
		KeepDays, KeepCount int64
	}{})
	func(makeSet func() (*pages.Set, error), handler func(pages.Config) http.Handler, write func(*pages.Set, http.ResponseWriter, *http.Request, int, string, any)) {
		if makeSet == nil || handler == nil || write == nil {
			t.Fatal("missing API")
		}
	}(pages.Load, pages.Handler, (*pages.Set).Write)
}
