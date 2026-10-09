package pages

import (
	"context"
	"fmt"
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
func status(u store.Run) (word, kind string) {
	switch u.Status {
	case store.StatusQueued:
		return WordQueued, KindInfo
	case store.StatusRunning:
		return WordRunning, KindInfo
	case store.StatusExited:
		kind = KindWarn
		if u.ExitCode == 0 {
			kind = KindOK
		}
		return WordExited + strconv.Itoa(u.ExitCode), kind
	case store.StatusTimedOut:
		return WordTimedOut, KindWarn
	case store.StatusKilled:
		return WordKilled, KindWarn
	default:
		return WordFailed, KindErr
	}
}
func duration(u store.Run) string {
	if u.Status == store.StatusQueued || u.Status == store.StatusRunning || u.Status == store.StatusFailed {
		return ""
	}
	d := int64(u.Finished.Sub(u.Started) / time.Second)
	if d < 60 {
		return strconv.FormatInt(d, 10) + UnitSecond
	}
	return strconv.FormatInt(d/60, 10) + UnitMinute + strconv.FormatInt(d%60, 10) + UnitSecond
}
func size(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10) + UnitByte
	}
	if n < 1000000 {
		return strconv.FormatInt(n/1000, 10) + "." + strconv.FormatInt(n/100%10, 10) + UnitKilobyte
	}
	return strconv.FormatInt(n/1000000, 10) + "." + strconv.FormatInt(n/100000%10, 10) + UnitMegabyte
}
func runRow(u store.Run, name string) RunRow {
	word, kind := status(u)
	sha := u.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	exit := ""
	if u.Status == store.StatusExited {
		exit = strconv.Itoa(u.ExitCode)
	}
	return RunRow{u.ID, "/" + name + "/runs/" + u.ID + "/", word, kind, sha, u.Started.UTC().Format(MinuteLayout), datetime(u.Started), duration(u), exit}
}
func failure(ctx context.Context, cfg Config, sc store.Script, u store.Run) *Failure {
	repo := sc.Repo
	if u.Reason == store.ReasonCommitMissing || u.Reason == store.ReasonGitFailed {
		if r := repository(ctx, cfg, sc); r.Name != "" {
			repo = r.Name
		}
	}
	switch u.Reason {
	case store.ReasonRepositoryMissing:
		return &Failure{TitleRepositoryMissing, fmt.Sprintf(TextRepositoryMissing, sc.Repo)}
	case store.ReasonCommitMissing:
		return &Failure{TitleCommitMissing, fmt.Sprintf(TextCommitMissing, u.Ref, repo)}
	case store.ReasonTooLarge:
		return &Failure{TitleTooLarge, fmt.Sprintf(TextTooLarge, cfg.TreeMaxBytes)}
	case store.ReasonGitFailed:
		return &Failure{TitleGitFailed, fmt.Sprintf(TextGitFailed, repo)}
	case store.ReasonQueueAbandoned:
		return &Failure{TitleQueueAbandoned, TextQueueAbandoned}
	case store.ReasonTimedOut:
		return &Failure{TitleTimedOut, fmt.Sprintf(TextTimedOut, cfg.OperationSeconds)}
	default:
		return &Failure{TitleStartFailed, TextStartFailed}
	}
}
func runData(ctx context.Context, cfg Config, sc store.Script, u store.Run, b page.Banner) RunData {
	word, kind := status(u)
	out, errout := cfg.Runs.Sizes(u)
	c := RunCard{ID: u.ID, URL: "/" + sc.Name + "/runs/" + u.ID + "/", Status: word, Kind: kind, Running: u.Status == store.StatusQueued || u.Status == store.StatusRunning, Commit: u.SHA, Ref: u.Ref, Started: u.Started.UTC().Format(SecondLayout), StartedAt: datetime(u.Started), Duration: duration(u), Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, StdoutSize: size(out), StderrSize: size(errout), Truncated: u.Truncated, FilesGone: cfg.Runs.Gone(u)}
	switch u.Status {
	case store.StatusQueued:
		c.Notice = NoticeQueued
	case store.StatusRunning:
		c.Notice = NoticeRunning
	}
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
