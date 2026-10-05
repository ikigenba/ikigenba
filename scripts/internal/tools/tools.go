package tools

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// Config supplies the catalog, source, run core and event writer.
type Config struct {
	Store     *store.Store
	Source    *source.Source
	Runs      *runs.Core
	Telemetry *telemetry.Writer
}

func missingScript(n string) error { return errors.New("no script named '" + n + "'") }
func missingRun(n string) error    { return errors.New("no run '" + n + "'") }
func invalidRef(n string) error    { return errors.New("invalid ref '" + n + "'") }
func catalogError() error          { return errors.New(store.Unreachable) }
func findScript(ctx context.Context, cfg Config, u identity.Caller, n string) (store.Script, error) {
	if !store.ValidName(n) {
		return store.Script{}, missingScript(n)
	}
	s, e := cfg.Store.Find(ctx, u.UserID, n)
	if errors.Is(e, store.ErrNotFound) {
		e = missingScript(n)
	} else if e != nil {
		e = catalogError()
	}
	return s, e
}
func findRun(ctx context.Context, cfg Config, u identity.Caller, n string) (store.Run, error) {
	if !store.ValidRunID(n) {
		return store.Run{}, missingRun(n)
	}
	r, e := cfg.Store.FindRun(ctx, u.UserID, n)
	if errors.Is(e, store.ErrNotFound) {
		e = missingRun(n)
	} else if e != nil {
		e = catalogError()
	}
	return r, e
}
func cutoff(ctx context.Context, e error) {
	if errors.Is(e, limits.ErrHalted) || context.Cause(ctx) != nil && errors.Is(e, context.Cause(ctx)) {
		runtime.Goexit()
	}
}
func timeText(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
func lastRun(r *store.Run) *LastRun {
	if r == nil {
		return nil
	}
	v := &LastRun{ID: r.ID, Status: r.Status, Started: timeText(r.Started)}
	if r.Status == store.StatusExited {
		n := r.ExitCode
		v.ExitCode = &n
	}
	return v
}
func script(s store.Script) Script {
	return Script{s.ID, s.Name, s.Repo, s.Ref, timeText(s.Created), lastRun(s.Last)}
}
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func runEntry(r store.Run) RunEntry {
	v := RunEntry{ID: r.ID, SHA: optional(r.SHA), Ref: r.Ref, Trigger: r.Trigger, Status: r.Status, Started: timeText(r.Started), Truncated: r.Truncated}
	if r.Status == store.StatusExited {
		v.ExitCode = &r.ExitCode
	}
	if r.Status != store.StatusRunning {
		s := timeText(r.Finished)
		v.Finished = &s
	}
	if r.Status == store.StatusFailed {
		v.Reason = &r.Reason
	}
	return v
}
func stream(folder, name string) string {
	p := filepath.Join(folder, name)
	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0444 == 0 {
		return ""
	}
	root, e := os.OpenRoot(folder)
	if e != nil {
		return ""
	}
	defer func() { _ = root.Close() }()
	b, e := root.ReadFile(name)
	if e != nil {
		return ""
	}
	return string(b)
}
func files(folder string) []File {
	out := []File{}
	root := filepath.Join(folder, runs.OutDir)
	st, e := os.Lstat(root)
	if e != nil || !st.IsDir() {
		return out
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		st, e := os.Lstat(p)
		if e != nil {
			return nil
		}
		if d.IsDir() && st.Mode().Perm()&0444 == 0 {
			return filepath.SkipDir
		}
		if !st.Mode().IsRegular() {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e == nil {
			out = append(out, File{filepath.ToSlash(rel), st.Size()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func result(cfg Config, r store.Run) RunResult {
	entry := runEntry(r)
	o, e := cfg.Runs.Sizes(r)
	v := RunResult{ID: r.ID, Script: r.Script, SHA: entry.SHA, Ref: r.Ref, User: r.User, RequestID: r.RequestID, Trigger: r.Trigger, Status: r.Status, ExitCode: entry.ExitCode, Started: entry.Started, Finished: entry.Finished, StdoutBytes: o, StderrBytes: e, Truncated: r.Truncated, Reason: entry.Reason}
	if cfg.Runs.Gone(r) {
		b := true
		v.FilesGone = &b
	} else {
		folder := cfg.Runs.Folder(r)
		o, e := stream(folder, runs.StdoutFile), stream(folder, runs.StderrFile)
		ff := files(folder)
		v.Stdout = &o
		v.Stderr = &e
		v.Files = &ff
	}
	return v
}
func listHandler(cfg Config) func(context.Context, identity.Caller, ListArgs) (ScriptList, error) {
	return func(ctx context.Context, u identity.Caller, _ ListArgs) (ScriptList, error) {
		ss, e := cfg.Store.List(ctx, u.UserID)
		if e != nil {
			return ScriptList{}, catalogError()
		}
		v := ScriptList{Scripts: []ListedScript{}}
		for _, s := range ss {
			v.Scripts = append(v.Scripts, ListedScript{s.ID, s.Name, s.Repo, s.Ref, lastRun(s.Last)})
		}
		return v, nil
	}
}
func showHandler(cfg Config) func(context.Context, identity.Caller, ShowArgs) (Script, error) {
	return func(ctx context.Context, u identity.Caller, a ShowArgs) (Script, error) {
		s, e := findScript(ctx, cfg, u, a.Name)
		return script(s), e
	}
}
func createHandler(cfg Config) func(context.Context, identity.Caller, CreateArgs) (Script, error) {
	return func(ctx context.Context, u identity.Caller, a CreateArgs) (Script, error) {
		if !store.ValidName(a.Name) {
			return Script{}, errors.New("invalid name '" + a.Name + "'")
		}
		taken, e := cfg.Store.Taken(ctx, a.Name)
		if e != nil {
			if a.Ref != nil && !source.ValidRef(*a.Ref) {
				return Script{}, invalidRef(*a.Ref)
			}
			return Script{}, catalogError()
		}
		if taken {
			return Script{}, errors.New("a script named '" + a.Name + "' already exists")
		}
		owner := ""
		if source.ValidRepo(a.Repo) {
			owner, e = cfg.Source.Owner(ctx, a.Repo)
			cutoff(ctx, e)
		}
		if e != nil || owner != u.UserID {
			return Script{}, errors.New("no repository '" + a.Repo + "'")
		}
		ref := "main"
		if a.Ref != nil {
			ref = *a.Ref
			if !source.ValidRef(ref) {
				return Script{}, invalidRef(ref)
			}
		}
		s, e := cfg.Store.Create(ctx, store.Draft{Owner: u.UserID, Name: a.Name, Repo: a.Repo, Ref: ref})
		if errors.Is(e, store.ErrNameTaken) {
			return Script{}, errors.New("a script named '" + a.Name + "' already exists")
		}
		if e != nil {
			return Script{}, catalogError()
		}
		cfg.Telemetry.Emit(ctx, "script.created", telemetry.Attrs{"script": s.ID})
		return script(s), nil
	}
}
func updateHandler(cfg Config) func(context.Context, identity.Caller, UpdateArgs) (Script, error) {
	return func(ctx context.Context, u identity.Caller, a UpdateArgs) (Script, error) {
		s, e := findScript(ctx, cfg, u, a.Name)
		if e != nil {
			if e.Error() == store.Unreachable && !source.ValidRef(a.Ref) {
				return Script{}, invalidRef(a.Ref)
			}
			return Script{}, e
		}
		if !source.ValidRef(a.Ref) {
			return Script{}, invalidRef(a.Ref)
		}
		s, changed, e := cfg.Store.SetRef(ctx, s.ID, a.Ref)
		if errors.Is(e, store.ErrNotFound) {
			return Script{}, missingScript(a.Name)
		}
		if e != nil {
			return Script{}, catalogError()
		}
		if changed {
			cfg.Telemetry.Emit(ctx, "script.updated", telemetry.Attrs{"script": s.ID})
		}
		return script(s), nil
	}
}
func deleteHandler(cfg Config) func(context.Context, identity.Caller, DeleteArgs) (Deleted, error) {
	return func(ctx context.Context, u identity.Caller, a DeleteArgs) (Deleted, error) {
		s, e := findScript(ctx, cfg, u, a.Name)
		if e != nil {
			return Deleted{}, e
		}
		if e = cfg.Runs.Delete(ctx, s.ID); e != nil {
			if errors.Is(e, store.ErrNotFound) {
				return Deleted{}, missingScript(a.Name)
			}
			return Deleted{}, catalogError()
		}
		cfg.Telemetry.Emit(ctx, "script.deleted", telemetry.Attrs{"script": s.ID})
		return Deleted{true, s.ID}, nil
	}
}
func runHandler(cfg Config) func(context.Context, identity.Caller, RunArgs) (Started, error) {
	return func(ctx context.Context, u identity.Caller, a RunArgs) (Started, error) {
		s, e := findScript(ctx, cfg, u, a.Name)
		if e != nil {
			return Started{}, e
		}
		ref := ""
		if a.Ref != nil {
			ref = *a.Ref
		}
		r, e := cfg.Runs.Run(ctx, s, runs.Request{Ref: ref, Input: a.Input, Caller: u})
		if e != nil {
			cutoff(ctx, e)
			if errors.Is(e, runs.ErrDraining) {
				return Started{}, errors.New("scripts is stopping; try again later")
			}
			if errors.Is(e, store.ErrNotFound) {
				return Started{}, missingScript(a.Name)
			}
			return Started{}, catalogError()
		}
		v := runEntry(r)
		return Started{r.ID, r.Status, v.SHA, v.Reason}, nil
	}
}
func runsHandler(cfg Config) func(context.Context, identity.Caller, RunsArgs) (RunList, error) {
	return func(ctx context.Context, u identity.Caller, a RunsArgs) (RunList, error) {
		s, e := findScript(ctx, cfg, u, a.Name)
		if e != nil {
			return RunList{}, e
		}
		rr, e := cfg.Store.Runs(ctx, s.ID)
		if e != nil {
			return RunList{}, catalogError()
		}
		v := RunList{Runs: []RunEntry{}}
		for _, r := range rr {
			v.Runs = append(v.Runs, runEntry(r))
		}
		return v, nil
	}
}
func resultHandler(cfg Config) func(context.Context, identity.Caller, ResultArgs) (RunResult, error) {
	return func(ctx context.Context, u identity.Caller, a ResultArgs) (RunResult, error) {
		r, e := findRun(ctx, cfg, u, a.Run)
		if e != nil {
			return RunResult{}, e
		}
		return result(cfg, r), nil
	}
}
func cancelHandler(cfg Config) func(context.Context, identity.Caller, CancelArgs) (RunEntry, error) {
	return func(ctx context.Context, u identity.Caller, a CancelArgs) (RunEntry, error) {
		r, e := findRun(ctx, cfg, u, a.Run)
		if e != nil {
			return RunEntry{}, e
		}
		r, e = cfg.Runs.Cancel(ctx, r.ID)
		if errors.Is(e, store.ErrNotFound) {
			return RunEntry{}, missingRun(a.Run)
		}
		if errors.Is(e, store.ErrEnded) {
			return RunEntry{}, errors.New("run '" + a.Run + "' has already ended")
		}
		if e != nil {
			return RunEntry{}, catalogError()
		}
		return runEntry(r), nil
	}
}

// Register adds the nine typed tools in their advertised order.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[ListArgs, ScriptList]{Name: "list", Description: "The scripts you own, by name.\n\nTakes no arguments. Each script has its id, name, repo (the id of the repos repository it runs from), ref (the branch, tag, or commit a run resolves), and last_run (its newest run's id, status, exit_code when it exited, and started; absent when it has never run). Use show for one script's created time, and runs for all of its runs.", Effect: mcp.Read, Handler: listHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[ShowArgs, Script]{Name: "show", Description: "One of your scripts, with its repository, its ref and its last run.\n\nPass name, the script's name. The result has its id, name, repo (the id of the repos repository it runs from), ref (the branch, tag, or commit a run resolves), created, and last_run (its newest run's id, status, exit_code when it exited, and started); last_run is absent when it has never run.", Effect: mcp.Read, Handler: showHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[CreateArgs, Script]{Name: "create", Description: "Create a script from one of your repositories and a ref.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, is neither about nor mcp, and must not already name a script in the space: names are shared by every user, because a script's page is at its name. repo is the id of one of your repositories in repos. ref is the branch, tag, or commit a run uses, main unless given; it is not resolved until a run, so it may name nothing yet. A run unpacks that commit and runs " + runner.Interpreter + " main.py from the repository's root. The script does not run until you call run. The result is what show returns.", Effect: mcp.Additive, Handler: createHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[UpdateArgs, Script]{Name: "update", Description: "Change the ref one of your scripts runs from.\n\nPass name and ref, the branch, tag, or commit every later run resolves, under the rules of create. A run already started keeps the commit it resolved. A script's name and repository never change. The result is what show returns.", Effect: mcp.Additive, Handler: updateHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[DeleteArgs, Deleted]{Name: "delete", Description: "Delete one of your scripts and every run it has.\n\nPass name. Every run of the script goes with it, its output and files included, and a run still running is killed. Its name is free for anyone to take. The repository and its history stay in repos. The result is the id of the deleted script.", Effect: mcp.Destructive, Handler: deleteHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[RunArgs, Started]{Name: "run", Description: "Start a run of one of your scripts and return its id, status and commit.\n\nPass name, and ref to run a branch, tag, or commit other than the one the script runs from; a ref given here is used for this run only and does not change the script's ref. Pass input, a JSON object, for the script to read from the file its IKIGENBA_INPUT variable names; {} unless given. The ref is resolved and its commit unpacked before the answer; the script then runs on its own, and run never waits for it. The result is the run's id, its status, running, and sha, the commit it runs. A run that could not start is still a run: its status is failed, with reason (repository_missing, commit_missing, too_large, git_failed, timed_out, or start_failed), and sha when the ref resolved. Follow a run with result until its status is final; end it early with cancel.", Effect: mcp.Additive, Handler: runHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[RunsArgs, RunList]{Name: "runs", Description: "The runs of one of your scripts, newest first.\n\nPass name, the script's name. Each run has its id, sha (the commit it ran, absent when the ref never resolved), ref, trigger, status (running, exited, killed, timed_out, or failed), exit_code (when it exited), started, finished (absent while running), truncated (whether output was cut), and reason (why it never started, when it failed). Use result for one run's output and files.", Effect: mcp.Read, Handler: runsHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[ResultArgs, RunResult]{Name: "result", Description: "One run whole: its details, its output so far, and the files it wrote.\n\nPass run, the run's id. The result has its id, script (the script's id), sha, ref, user, request_id, trigger, status, exit_code (when it exited), started, finished (absent while running), stdout_bytes and stderr_bytes (how much of each is kept), truncated, and reason (when it failed), then stdout and stderr, the output kept so far, and files, each file the script wrote under its out folder, with its path and size. While status is running, call result again until it is final. When the run's files are gone, stdout, stderr, and files are absent and files_gone is true.", Effect: mcp.Read, Handler: resultHandler(cfg)})
	mcp.AddTool(srv, mcp.Tool[CancelArgs, RunEntry]{Name: "cancel", Description: "End one of your runs that is still running.\n\nPass run, the run's id. The script's process group is killed whole, and the run is recorded killed; what it wrote so far is kept. A run that has already ended is refused. The result is the run as runs lists it, with its final status and finished.", Effect: mcp.Destructive, Handler: cancelHandler(cfg)})
}
