package pages

import (
	"context"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func repository(ctx context.Context, cfg Config, sc store.Script) Repo {
	v := Repo{ID: sc.Repo}
	if n, ok := cfg.Source.Name(ctx, sc.Repo); ok {
		v.Name = n
	}
	return v
}
func datetime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func duration(u store.Run) *Duration {
	if u.Status == store.StatusQueued || u.Status == store.StatusRunning || u.Status == store.StatusFailed {
		return nil
	}
	d := int64(u.Finished.Sub(u.Started) / time.Second)
	return &Duration{Minutes: d / 60, Seconds: d % 60}
}
func size(n int64) Size {
	if n < 1000 {
		return Size{Number: strconv.FormatInt(n, 10), Unit: "byte"}
	}
	if n < 1000000 {
		return Size{Number: strconv.FormatInt(n/1000, 10) + "." + strconv.FormatInt(n/100%10, 10), Unit: "kilobyte"}
	}
	return Size{Number: strconv.FormatInt(n/1000000, 10) + "." + strconv.FormatInt(n/100000%10, 10), Unit: "megabyte"}
}
func runRow(u store.Run, name string) RunRow {
	sha := u.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return RunRow{u.ID, "/" + name + "/runs/" + u.ID + "/", u.Status, u.ExitCode, sha, u.Started.UTC().Format(MinuteLayout), datetime(u.Started), duration(u)}
}
func failure(ctx context.Context, cfg Config, sc store.Script, u store.Run) *Failure {
	return &Failure{Reason: u.Reason, Repo: repository(ctx, cfg, sc), Ref: u.Ref, TreeMaxBytes: cfg.TreeMaxBytes, OperationSeconds: cfg.OperationSeconds}
}
func runData(ctx context.Context, cfg Config, sc store.Script, u store.Run, b page.Banner) RunData {
	out, errout := cfg.Runs.Sizes(u)
	c := RunCard{ID: u.ID, URL: "/" + sc.Name + "/runs/" + u.ID + "/", Status: u.Status, ExitCode: u.ExitCode, Running: u.Status == store.StatusQueued || u.Status == store.StatusRunning, Commit: u.SHA, Ref: u.Ref, Started: u.Started.UTC().Format(SecondLayout), StartedAt: datetime(u.Started), Duration: duration(u), Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, StdoutSize: size(out), StderrSize: size(errout), Truncated: u.Truncated, FilesGone: cfg.Runs.Gone(u)}
	if !c.Running {
		c.Finished = u.Finished.UTC().Format(SecondLayout)
		c.FinishedAt = datetime(u.Finished)
	}
	if u.Status == store.StatusFailed {
		c.Failure = failure(ctx, cfg, sc, u)
	}
	d := RunData{Banner: b, Script: ScriptLink{sc.Name, "/" + sc.Name + "/"}, Run: c}
	if c.FilesGone {
		return d
	}
	folder, err := os.OpenRoot(cfg.Runs.Folder(u))
	if err != nil {
		return d
	}
	defer func() { _ = folder.Close() }()
	read := func(name string) *FileText {
		info, e := folder.Lstat(name)
		if e != nil || !info.Mode().IsRegular() {
			return nil
		}
		data, e := folder.ReadFile(name)
		if e != nil {
			return nil
		}
		return &FileText{size(int64(len(data))), string(data), c.URL + name}
	}
	d.Input = read(runs.InputFile)
	d.Stdout = read(runs.StdoutFile)
	d.Stderr = read(runs.StderrFile)
	_ = fs.WalkDir(folder.FS(), runs.OutDir, func(p string, entry fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, e := folder.Lstat(p)
		if e != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, e := filepath.Rel(runs.OutDir, p)
		if e != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		pieces := strings.Split(rel, "/")
		for i, v := range pieces {
			pieces[i] = url.PathEscape(v)
		}
		d.Files = append(d.Files, FileRow{rel, size(info.Size()), c.URL + "out/" + strings.Join(pieces, "/")})
		return nil
	})
	slices.SortFunc(d.Files, func(a, b FileRow) int { return strings.Compare(a.Path, b.Path) })
	return d
}
