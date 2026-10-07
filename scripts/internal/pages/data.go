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
func status(u store.Run) (word, kind string) {
	switch u.Status {
	case store.StatusQueued:
		return "queued", "info"
	case store.StatusRunning:
		return "running", "info"
	case store.StatusExited:
		kind = "warn"
		if u.ExitCode == 0 {
			kind = "ok"
		}
		return "exited " + strconv.Itoa(u.ExitCode), kind
	case store.StatusTimedOut:
		return "timed out", "warn"
	case store.StatusKilled:
		return "killed", "warn"
	default:
		return "failed", "err"
	}
}
func duration(u store.Run) string {
	if u.Status == store.StatusQueued || u.Status == store.StatusRunning || u.Status == store.StatusFailed {
		return ""
	}
	d := int64(u.Finished.Sub(u.Started) / time.Second)
	if d < 60 {
		return strconv.FormatInt(d, 10) + "s"
	}
	return strconv.FormatInt(d/60, 10) + "m " + strconv.FormatInt(d%60, 10) + "s"
}
func size(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10) + " B"
	}
	if n < 1000000 {
		return strconv.FormatInt(n/1000, 10) + "." + strconv.FormatInt(n/100%10, 10) + " kB"
	}
	return strconv.FormatInt(n/1000000, 10) + "." + strconv.FormatInt(n/100000%10, 10) + " MB"
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
	return RunRow{u.ID, "/" + name + "/runs/" + u.ID + "/", word, kind, sha, u.Started.UTC().Format("2006-01-02 15:04"), datetime(u.Started), duration(u), exit}
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
		return &Failure{"The repository is unavailable", "The repository " + sc.Repo + " is not there. The script was not started."}
	case store.ReasonCommitMissing:
		return &Failure{"The ref did not resolve", "'" + u.Ref + "' names no commit in the repository " + repo + ". The script was not started."}
	case store.ReasonTooLarge:
		return &Failure{"The tree is too large", "The commit's files add up to more than " + strconv.FormatInt(cfg.TreeMaxBytes, 10) + " bytes. The script was not started."}
	case store.ReasonGitFailed:
		return &Failure{"git failed", "git could not read the repository " + repo + ". The script was not started."}
	case store.ReasonQueueAbandoned:
		return &Failure{"The run never left the queue", "The run was waiting for a slot when scripts stopped. The script was not started."}
	case store.ReasonTimedOut:
		return &Failure{"git took too long", "git took longer than " + strconv.FormatInt(cfg.OperationSeconds, 10) + " seconds. The script was not started."}
	default:
		return &Failure{"The script could not start", "The script's process could not be launched. The script was not started."}
	}
}
func runData(ctx context.Context, cfg Config, sc store.Script, u store.Run, b page.Banner) RunData {
	word, kind := status(u)
	out, errout := cfg.Runs.Sizes(u)
	c := RunCard{ID: u.ID, URL: "/" + sc.Name + "/runs/" + u.ID + "/", Status: word, Kind: kind, Running: u.Status == store.StatusQueued || u.Status == store.StatusRunning, Commit: u.SHA, Ref: u.Ref, Started: u.Started.UTC().Format("2006-01-02 15:04:05 UTC"), StartedAt: datetime(u.Started), Duration: duration(u), Trigger: u.Trigger, Event: u.Event, User: u.User, Request: u.RequestID, StdoutSize: size(out), StderrSize: size(errout), Truncated: u.Truncated, FilesGone: cfg.Runs.Gone(u)}
	switch u.Status {
	case store.StatusQueued:
		c.Notice = "Queued"
	case store.StatusRunning:
		c.Notice = "Running"
	}
	if !c.Running {
		c.Finished = u.Finished.UTC().Format("2006-01-02 15:04:05 UTC")
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
