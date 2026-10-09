package pages_test

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

func templates(t *testing.T) *template.Template {
	t.Helper()
	set, err := page.Templates().ParseFS(prompts.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return set
}
func render(t *testing.T, set *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := set.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("execute %s: %v", name, err)
	}
	return b.String()
}
func load(t *testing.T) *pages.Set {
	t.Helper()
	s, err := pages.Load()
	if err != nil || s == nil {
		t.Fatalf("Load: %v %v", s, err)
	}
	return s
}
func statuses() []string {
	return []string{store.StatusQueued, store.StatusRunning, store.StatusExited, store.StatusTimedOut, store.StatusKilled, store.StatusFailed}
}

// R-B3WN-LG66 R-B54J-Z7WV R-A5MM-0F6O R-B7KC-QRE9 R-BZM1-JHG8 R-YFSK-4K6C
func TestTemplateSet(t *testing.T) {
	if pages.ServiceName != "prompts" {
		t.Fatal("service name")
	}
	if pages.Description == "" || strings.ContainsAny(pages.Description, "\n\r\"\\") {
		t.Fatal("description")
	}
	for _, layout := range []string{pages.MinuteLayout, pages.CardLayout, pages.SecondLayout} {
		if layout == "" || time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC).Format(layout) == "" {
			t.Fatal("time layout")
		}
	}
	t.Chdir(t.TempDir())
	s := load(t)
	set := templates(t)
	for _, name := range []string{"landing", "prompt", "run", "about", "tools", "notfound", "unavailable", "run-status", "run-duration", "file-size"} {
		if set.Lookup(name) == nil {
			t.Fatal(name)
		}
	}
	w := httptest.NewRecorder()
	s.Write(w, httptest.NewRequest("GET", "/", nil), 200, "about", pages.AboutData{})
	if w.Body.String() != render(t, set, "about", pages.AboutData{}) {
		t.Fatal("loaded set differs")
	}
}

// pageRecorder accepts the bytes Set writes even for statuses that HTTP transports suppress.
type pageRecorder struct{ *httptest.ResponseRecorder }

func (w pageRecorder) Write(b []byte) (int, error) { return w.Body.Write(b) }

// R-B8S9-4J4Y R-YI8C-W3NQ R-YJG9-9VEF R-YKO5-NN54
func TestWrite(t *testing.T) {
	set, s := templates(t), load(t)
	cases := []struct {
		name string
		data any
	}{{"landing", pages.LandingData{}}, {"prompt", pages.PromptData{}}, {"run", pages.RunData{}}, {"about", pages.AboutData{}}, {"tools", pages.ToolsData{}}, {"notfound", pages.NoticeData{}}, {"unavailable", pages.NoticeData{}}}
	for _, c := range cases {
		want := render(t, set, c.name, c.data)
		for code := 200; code <= 599; code++ {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				w := pageRecorder{httptest.NewRecorder()}
				w.Header()["X-Test"] = []string{"first", "second"}
				w.Header()["Content-Type"] = []string{"old", "older"}
				s.Write(w, httptest.NewRequest(method, "/", nil), code, c.name, c.data)
				if w.Code != code {
					t.Fatal("status", code, w.Code)
				}
				headers := http.Header{"X-Test": []string{"first", "second"}, "Content-Type": []string{"text/html; charset=utf-8"}}
				if !reflect.DeepEqual(w.Header(), headers) {
					t.Fatalf("headers %v", w.Header())
				}
				body := want
				if method == "HEAD" {
					body = ""
				}
				if w.Body.String() != body {
					t.Fatalf("body %s %s code %d lengths %d/%d", method, c.name, code, w.Body.Len(), len(body))
				}
			}
		}
	}
}

// R-C21U-B0XM R-BDNU-NM3Q R-BTIJ-MMQR R-BCFY-9UD1 R-BKZ8-Y8JW
func TestPartials(t *testing.T) {
	set := templates(t)
	for _, s := range statuses() {
		for _, code := range []int{0, 1} {
			row := pages.RunRow{Status: s, ExitCode: code}
			card := pages.RunCard{Status: s, ExitCode: code}
			if render(t, set, "run-status", row) != render(t, set, "run-status", card) {
				t.Fatal("status partial differs")
			}
		}
	}
	for _, m := range []int64{0, 1} {
		render(t, set, "run-duration", pages.Duration{Minutes: m, Seconds: 41})
	}
	for _, unit := range []string{"byte", "kilobyte", "megabyte"} {
		render(t, set, "file-size", pages.Size{Number: "1.2", Unit: unit})
	}
}

// R-YH0G-IBX1 R-BA05-IAVN R-BB81-W2MC R-BEVR-1DUF R-BG3N-F5L4 R-BHBJ-SXBT
// R-BIJG-6P2I R-BJRC-KGT7 R-BNF1-PS1A R-BOMY-3JRZ R-BPUU-HBIO R-BR2Q-V39D
// R-BSAN-8V02 R-BUQG-0EHG R-BVYC-E685
func TestAllTemplateStates(t *testing.T) {
	t.Setenv(services.Variable, "")
	set := templates(t)
	banners := []page.Banner{{}, page.New(pages.ServiceName, "display-fixture").Banner(page.User{Email: "reader@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})}
	for _, banner := range banners {
		for variant := 0; variant < 4; variant++ {
			d := pages.LandingData{Banner: banner}
			if variant > 0 {
				row := pages.PromptRow{Name: "fixture", URL: "/fixture/", Model: "fixture-model"}
				if variant > 1 {
					row.LastRun = &pages.RunRow{Status: store.StatusRunning}
					if variant == 3 {
						row.LastRun.Cost = "0.000001"
					}
				}
				d.Prompts = []pages.PromptRow{row}
			}
			render(t, set, "landing", d)
		}
		for _, n := range []int{0, 1, 3} {
			for mask := 0; mask < 128; mask++ {
				d := pages.PromptData{Banner: banner, Prompt: pages.PromptCard{ID: "prompt-fixture", Name: "fixture", Model: "fixture-model", Text: "fixture-text", Created: "fixture-created", CreatedAt: "fixture-datetime", RunsKept: 1, KeepNewest: 2, KeepDays: 3}}
				for i := 0; i < n; i++ {
					d.Prompt.Tools = append(d.Prompt.Tools, pages.ToolGroup{Name: "fixture-group", First: i == 0, Last: i == n-1})
				}
				if mask&1 != 0 {
					d.Prompt.System = "fixture-system"
				}
				if mask&2 != 0 {
					d.Prompt.Schema = `{"fixture":true}`
				}
				if mask&4 != 0 {
					row := pages.RunRow{ID: "run-fixture", URL: "/fixture/runs/run-fixture/", Status: store.StatusExited, ExitCode: 1, Model: "fixture-model", Started: "fixture-start", StartedAt: "fixture-datetime"}
					if mask&16 != 0 {
						row.Duration = &pages.Duration{Minutes: 1, Seconds: 2}
					}
					if mask&32 != 0 {
						row.Cost = "0.123456"
					}
					d.Runs = []pages.RunRow{row}
				}
				if mask&8 != 0 {
					d.Subscriptions = []string{"fixture.*"}
				}
				render(t, set, "prompt", d)
			}
		}
		for _, status := range statuses() {
			for trunc := 0; trunc < 4; trunc++ {
				for mask := 0; mask < 32; mask++ {
					d := pages.RunData{Banner: banner, Prompt: pages.PromptLink{Name: "fixture", URL: "/fixture/"}, Run: pages.RunCard{ID: "run-fixture", URL: "/fixture/runs/run-fixture/", Status: status, Running: status == store.StatusQueued || status == store.StatusRunning, Model: "fixture-model", Started: "fixture-start", StartedAt: "fixture-start-at", Trigger: store.TriggerManual, User: "fixture-user", Request: "fixture-request", StdoutSize: pages.Size{Number: "0", Unit: "byte"}, StderrSize: pages.Size{Number: "0", Unit: "byte"}, TranscriptSize: pages.Size{Number: "0", Unit: "byte"}, Truncated: trunc != 0, StdoutTruncated: trunc&1 != 0, StderrTruncated: trunc&2 != 0, FilesGone: mask&16 != 0}}
					if mask&1 != 0 {
						d.Run.Usage = &pages.Usage{Calls: "1", ToolCalls: "0", InputTokens: "1,000", CachedTokens: "2", OutputTokens: "3", ReasoningTokens: "0", Cost: "0.000000"}
						d.Run.Finished = "fixture-finish"
						d.Run.FinishedAt = "fixture-finish-at"
					}
					if mask&2 != 0 {
						d.Run.Event = "fixture-event"
						d.Run.Trigger = store.TriggerEvent
						d.Run.Duration = &pages.Duration{Minutes: 0, Seconds: 41}
					}
					if mask%3 == 1 {
						d.Run.Failure = &pages.Failure{Reason: store.ReasonStartFailed}
					}
					if mask%3 == 2 {
						d.Run.Failure = &pages.Failure{Reason: store.ReasonQueueAbandoned}
					}
					text := pages.FileText{Size: pages.Size{Number: "1", Unit: "byte"}, Text: "fixture-file", URL: "/fixture/file"}
					if mask&4 != 0 {
						text.Text = ""
					}
					if mask&1 != 0 {
						d.Input = &text
					}
					if mask&2 != 0 {
						d.Stdout = &text
					}
					if mask&4 != 0 {
						d.Stderr = &text
					}
					if mask&8 != 0 {
						d.Transcript = &pages.FileLink{Size: text.Size, URL: text.URL}
						d.Files = []pages.FileRow{{Path: "fixture-path", Size: text.Size, URL: text.URL}}
					}
					render(t, set, "run", d)
				}
			}
		}
		render(t, set, "about", pages.AboutData{Banner: banner, Description: pages.Description})
		render(t, set, "tools", pages.ToolsData{Banner: banner})
		render(t, set, "tools", pages.ToolsData{Banner: banner, Tools: []pages.Tool{{Name: "fixture-tool", Description: "fixture-description"}}})
		for _, name := range []string{"notfound", "unavailable"} {
			render(t, set, name, pages.NoticeData{Banner: banner})
		}
	}
}
