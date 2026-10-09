package tools

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

func timeText(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
func ptr[T any](v T) *T           { return &v }
func promptObject(p store.Prompt) Prompt {
	out := Prompt{ID: p.ID, Name: p.Name, Model: p.Model, Prompt: p.Prompt, System: p.System, Tools: append([]string{}, p.Tools...), Schema: p.Schema, Created: timeText(p.Created), Subscriptions: make([]Subscription, 0, len(p.Subscriptions))}
	for _, s := range p.Subscriptions {
		out.Subscriptions = append(out.Subscriptions, Subscription{Event: s.Event, Created: timeText(s.Created)})
	}
	if p.Last != nil {
		r := p.Last
		out.LastRun = &LastRun{ID: r.ID, Status: r.Status, Started: timeText(r.Started)}
		if r.Status == store.StatusExited {
			out.LastRun.ExitCode = ptr(r.ExitCode)
		}
	}
	return out
}
func runEntry(r store.Run) RunEntry {
	out := RunEntry{ID: r.ID, Model: r.Model, Trigger: r.Trigger, Status: r.Status, Started: timeText(r.Started), Truncated: r.Truncated()}
	if r.Trigger == store.TriggerEvent {
		out.Event = ptr(r.Event)
	}
	if r.Status == store.StatusExited {
		out.ExitCode = ptr(r.ExitCode)
	}
	if r.Status == store.StatusFailed {
		out.Reason = ptr(r.Reason)
	}
	if r.Status != store.StatusRunning && r.Status != store.StatusQueued {
		out.Finished = ptr(timeText(r.Finished))
		out.Calls = ptr(r.Usage.Calls)
		out.ToolCalls = ptr(r.Usage.ToolCalls)
		out.InputTokens = ptr(r.Usage.InputTokens)
		out.CachedTokens = ptr(r.Usage.CachedTokens)
		out.OutputTokens = ptr(r.Usage.OutputTokens)
		out.ReasoningTokens = ptr(r.Usage.ReasoningTokens)
		out.CostNanos = ptr(r.Usage.CostNanos)
	}
	return out
}
func regular(path string) (fs.FileInfo, bool) {
	info, err := os.Lstat(path)
	return info, err == nil && info.Mode().IsRegular()
}
func stream(path string) string {
	if _, ok := regular(path); !ok {
		return ""
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return ""
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return ""
	}
	return string(b)
}
func size(path string) int64 {
	if info, ok := regular(path); ok {
		return info.Size()
	}
	return 0
}
func files(root string) []File {
	out := []File{}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return out
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			rel, err := filepath.Rel(root, path)
			if err == nil {
				out = append(out, File{Path: filepath.ToSlash(rel), Size: info.Size()})
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i].Path, out[j].Path) < 0 })
	return out
}
func (h handlers) runResult(r store.Run) RunResult {
	e := runEntry(r)
	o, s := h.Runs.Sizes(r)
	out := RunResult{ID: r.ID, Prompt: r.Prompt, Model: r.Model, User: r.User, RequestID: r.RequestID, Trigger: r.Trigger, Event: e.Event, Status: r.Status, ExitCode: e.ExitCode, Started: e.Started, Finished: e.Finished, StdoutBytes: o, StderrBytes: s, Truncated: e.Truncated, Reason: e.Reason, Calls: e.Calls, ToolCalls: e.ToolCalls, InputTokens: e.InputTokens, CachedTokens: e.CachedTokens, OutputTokens: e.OutputTokens, ReasoningTokens: e.ReasoningTokens, CostNanos: e.CostNanos}
	if h.Runs.Gone(r) {
		out.FilesGone = ptr(true)
		return out
	}
	folder := h.Runs.Folder(r)
	out.Stdout = ptr(stream(filepath.Join(folder, runs.StdoutFile)))
	out.Stderr = ptr(stream(filepath.Join(folder, runs.StderrFile)))
	out.TranscriptBytes = ptr(size(filepath.Join(folder, runs.TranscriptFile)))
	out.Files = ptr(files(filepath.Join(folder, runs.WorkDir)))
	return out
}
