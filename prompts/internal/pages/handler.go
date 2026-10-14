package pages

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/urls"
)

// Handler renders read-only catalog pages and canonical redirects.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		notice := func(status int, name string) {
			cfg.Pages.Write(w, r, status, name, NoticeData{Banner: cfg.Banner(page.User{})})
		}
		failed := func(err error) {
			if errors.Is(err, store.ErrNotFound) {
				notice(http.StatusNotFound, "notfound")
			} else {
				notice(http.StatusServiceUnavailable, "unavailable")
			}
		}
		banner := func(trail ...page.Level) page.Banner {
			b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, cfg.ServicesPath), LogoutURL: urls.AuthLogout(r, cfg.ServicesPath)})
			b.Trail = trail
			return b
		}
		if r.URL.Path == "/about" {
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: banner(page.Level{Name: "about", URL: "/about"}), Description: Description})
			return
		}
		if r.URL.Path == "/tools" {
			data := ToolsData{Banner: banner(page.Level{Name: "tools", URL: "/tools"})}
			for _, tool := range cfg.MCP.Tools() {
				first, _, _ := strings.Cut(tool.Description, "\n")
				data.Tools = append(data.Tools, Tool{Name: tool.Name, Description: first})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", data)
			return
		}
		caller, _ := identity.FromContext(r.Context())
		if r.URL.Path == "/" {
			value := r.URL.Query().Get("page")
			mode, keys, ok := decodePage(value, "list", 1)
			if !ok {
				notice(http.StatusNotFound, "notfound")
				return
			}
			var ps []store.Prompt
			var err error
			if mode == beforePage || mode == lastPage {
				ps, err = cfg.Store.ListBefore(r.Context(), caller.UserID, keys[0], paging.DefaultLimit+1)
			} else {
				ps, err = cfg.Store.List(r.Context(), caller.UserID, keys[0], paging.DefaultLimit+1)
			}
			if err != nil {
				failed(err)
				return
			}
			if (mode == beforePage || mode == lastPage) && len(ps) <= paging.DefaultLimit {
				w.Header().Set("Location", "/")
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			more := len(ps) > paging.DefaultLimit
			if more {
				ps = ps[:paging.DefaultLimit]
			}
			if mode == beforePage || mode == lastPage {
				slices.Reverse(ps)
			}
			data := LandingData{Banner: banner(), Page: value}
			if len(ps) > 0 {
				if value != "" {
					data.Prev = paging.Token{Tool: "list-before"}.Encode(ps[0].Name)
				}
				if mode == beforePage || more && mode != lastPage {
					data.Next = paging.Token{Tool: "list"}.Encode(ps[len(ps)-1].Name)
				}
			}
			for _, p := range ps {
				row := PromptRow{Name: p.Name, URL: "/" + p.Name + "/", Model: p.Model}
				if p.Last != nil {
					last := runRow(*p.Last, p.Name)
					row.LastRun = &last
				}
				data.Prompts = append(data.Prompts, row)
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
		for i, s := range parts {
			if decoded, err := url.PathUnescape(s); err == nil {
				parts[i] = decoded
			}
		}
		var value string
		mode, keys := firstPage, []string{"", ""}
		if len(parts) == 2 && parts[1] == "" {
			value = r.URL.Query().Get("page")
			var ok bool
			mode, keys, ok = decodePage(value, "runs", 2)
			if !ok {
				notice(http.StatusNotFound, "notfound")
				return
			}
		}
		p, err := cfg.Store.Find(r.Context(), caller.UserID, parts[0])
		if err != nil {
			failed(err)
			return
		}
		base := "/" + p.Name + "/"
		redirect := func(path string) {
			if r.URL.RawQuery != "" {
				path += "?" + r.URL.RawQuery
			}
			w.Header().Set("Location", path)
			w.WriteHeader(http.StatusMovedPermanently)
		}
		if len(parts) == 1 {
			redirect(base)
			return
		}
		if len(parts) == 2 && parts[1] == "" {
			var rs []store.Run
			var e error
			if mode == beforePage || mode == lastPage {
				rs, e = cfg.Store.RunsBefore(r.Context(), p.ID, keys[0], keys[1], paging.DefaultLimit+1)
			} else {
				rs, e = cfg.Store.Runs(r.Context(), p.ID, keys[0], keys[1], paging.DefaultLimit+1)
			}
			if e != nil {
				failed(e)
				return
			}
			if (mode == beforePage || mode == lastPage) && len(rs) <= paging.DefaultLimit {
				w.Header().Set("Location", base)
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			more := len(rs) > paging.DefaultLimit
			if more {
				rs = rs[:paging.DefaultLimit]
			}
			if mode == beforePage || mode == lastPage {
				slices.Reverse(rs)
			}
			count, e := cfg.Store.RunCount(r.Context(), p.ID)
			if e != nil {
				failed(e)
				return
			}
			data := promptData(p, rs, count, cfg, banner(page.Level{Name: p.Name, URL: base}))
			data.Page = value
			if len(rs) > 0 {
				if value != "" {
					data.Prev = runToken("runs-before", rs[0])
				}
				if mode == beforePage || more && mode != lastPage {
					data.Next = runToken("runs", rs[len(rs)-1])
				}
			}
			cfg.Pages.Write(w, r, http.StatusOK, "prompt", data)
			return
		}
		if (len(parts) == 3 || len(parts) == 4 && parts[3] == "") && parts[1] == "runs" && parts[2] != "" {
			u, e := cfg.Store.FindRun(r.Context(), caller.UserID, parts[2])
			if e != nil {
				failed(e)
				return
			}
			if u.Prompt != p.ID {
				notice(http.StatusNotFound, "notfound")
				return
			}
			if len(parts) == 3 {
				redirect(base + "runs/" + u.ID + "/")
				return
			}
			cfg.Pages.Write(w, r, http.StatusOK, "run", runData(p, u, cfg, banner(page.Level{Name: p.Name, URL: base}, page.Level{Name: u.ID, URL: base + "runs/" + u.ID + "/"})))
			return
		}
		notice(http.StatusNotFound, "notfound")
	})
}

func promptData(p store.Prompt, rs []store.Run, count int, cfg Config, b page.Banner) PromptData {
	d := PromptData{Banner: b, Prompt: PromptCard{ID: p.ID, Name: p.Name, Model: p.Model, Text: p.Prompt, System: p.System, Schema: string(p.Schema), Created: p.Created.UTC().Format(CardLayout), CreatedAt: datetime(p.Created), RunsKept: count, KeepNewest: cfg.KeepCount, KeepDays: cfg.KeepDays}}
	for i, s := range p.Tools {
		d.Prompt.Tools = append(d.Prompt.Tools, ToolGroup{Name: s, First: i == 0, Last: i == len(p.Tools)-1})
	}
	for _, u := range rs {
		d.Runs = append(d.Runs, runRow(u, p.Name))
	}
	for _, s := range p.Subscriptions {
		d.Subscriptions = append(d.Subscriptions, s.Event)
	}
	return d
}

func runData(p store.Prompt, u store.Run, cfg Config, b page.Banner) RunData {
	c := RunCard{ID: u.ID, URL: "/" + p.Name + "/runs/" + u.ID + "/", Status: u.Status, ExitCode: u.ExitCode, Running: active(u), Model: u.Model, Started: u.Started.UTC().Format(SecondLayout), StartedAt: datetime(u.Started), Duration: duration(u), Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, Usage: usage(u), StdoutSize: size(0), StderrSize: size(0), TranscriptSize: size(0), Truncated: u.Truncated(), StdoutTruncated: u.StdoutTruncated, StderrTruncated: u.StderrTruncated, FilesGone: cfg.Runs.Gone(u)}
	if !c.Running {
		c.Finished = u.Finished.UTC().Format(SecondLayout)
		c.FinishedAt = datetime(u.Finished)
	}
	if u.Status == store.StatusFailed {
		c.Failure = &Failure{Reason: u.Reason}
	}
	d := RunData{Banner: b, Prompt: PromptLink{Name: p.Name, URL: "/" + p.Name + "/"}, Run: c}
	if c.FilesGone {
		return d
	}
	folder, err := os.OpenRoot(cfg.Runs.Folder(u))
	if err != nil {
		return d
	}
	defer func() { _ = folder.Close() }()
	stdout, stderr := cfg.Runs.Sizes(u)
	if regular(folder, runs.StdoutFile) {
		d.Run.StdoutSize = size(stdout)
	}
	if regular(folder, runs.StderrFile) {
		d.Run.StderrSize = size(stderr)
	}
	if fi, err := folder.Lstat(runs.TranscriptFile); err == nil && fi.Mode().IsRegular() {
		d.Run.TranscriptSize = size(fi.Size())
		d.Transcript = &FileLink{Size: d.Run.TranscriptSize, URL: c.URL + runs.TranscriptFile}
	}
	text := func(name string) *FileText {
		if !regular(folder, name) {
			return nil
		}
		bytes, err := folder.ReadFile(name)
		if err != nil {
			return nil
		}
		return &FileText{Size: size(int64(len(bytes))), Text: string(bytes), URL: c.URL + name}
	}
	d.Input = text(runs.InputFile)
	d.Stdout = text(runs.StdoutFile)
	d.Stderr = text(runs.StderrFile)
	info, err := folder.Lstat(runs.WorkDir)
	if err != nil || !info.IsDir() {
		return d
	}
	work, err := folder.OpenRoot(runs.WorkDir)
	if err != nil {
		return d
	}
	defer func() { _ = work.Close() }()
	_ = fs.WalkDir(work.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		info, e := entry.Info()
		if e != nil {
			return nil
		}
		names := strings.Split(path, "/")
		for i := range names {
			names[i] = url.PathEscape(names[i])
		}
		d.Files = append(d.Files, FileRow{Path: path, Size: size(info.Size()), URL: c.URL + runs.WorkDir + "/" + strings.Join(names, "/")})
		return nil
	})
	slices.SortFunc(d.Files, func(a, b FileRow) int { return strings.Compare(a.Path, b.Path) })
	return d
}

func regular(root *os.Root, path string) bool {
	fi, err := root.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// pageMode identifies the direction in the store's ordering.
type pageMode uint8

const (
	firstPage pageMode = iota
	afterPage
	beforePage
	lastPage
)

func decodePage(value, tool string, n int) (pageMode, []string, bool) {
	keys := make([]string, n)
	if value == "" {
		return firstPage, keys, true
	}
	targets := make([]any, n)
	for i := range keys {
		targets[i] = &keys[i]
	}
	if (paging.Token{Tool: tool}).Decode(value, targets...) == nil {
		return afterPage, keys, true
	}
	if (paging.Token{Tool: tool + "-before"}).Decode(value, targets...) == nil {
		return beforePage, keys, true
	}
	if value == "last" {
		return lastPage, make([]string, n), true
	}
	return firstPage, keys, false
}

func runToken(tool string, run store.Run) string {
	return paging.Token{Tool: tool}.Encode(run.Started.UTC().Format(time.RFC3339), run.ID)
}
