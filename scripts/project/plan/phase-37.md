# Phase 37 — Runs pin a commit and execute a real clone

*Realizes design Decision 38 (pinned clone + run token). Depends on Phase 36.*

`Service.newRun` resolves `main`'s head through the plane before inserting the
run row, stores it on `runs.repo_sha`, and returns a mapped error (creating no
run) when resolution fails. `internal/runner` stops reading the body: a new
`internal/runner/git.go` mints a run token, clones the repository into the run
dir, checks out the pinned sha detached, persists an env-reading credential
helper plus `user.name`/`user.email`, and appends the injected filenames to
`.git/info/exclude`; `suite.py` and `config.json` are written beside the
checkout as before, and `SUITE_REPO_KEY`/`SUITE_REPO_SHA`/`SUITE_GIT_TOKEN`
join the child environment. `git` becomes a hard test precondition (a
`t.Fatalf` helper, like `requirePython`), declared in `AGENTS.md` beside
`python3` so the existing `R-O1AD-MRKW` declaration test keeps passing.

**Done when:** the suite is green and each of these ids is covered by a genuine
test driving real `git` against a real bare repository over a `file://` remote:

- R-2K0Z-T8BT — the run row records the resolved sha and `run_get`/`run_list`
  expose `repo_sha`.
- R-2L8W-702I — the run dir is a real detached working tree at exactly that sha
  whose supporting module the running script imports successfully.
- R-2MGS-KRT7 — a commit landing on `main` after resolution does not change what
  the run executes.
- R-2NOO-YJJW — `suite.py` and `config.json` are injected, cwd is the run dir,
  and `git status --porcelain` lists none of the injected files.
- R-2OWL-CBAL — the git env facts reach the child, the token appears in no file
  under the run dir, and the persisted credential helper names the env var.
- R-2RCE-3URZ — a run's `git push` of a branch reaches the real remote while the
  remote's `main` is unchanged (no ambient merge).
- R-2SKA-HMIO — a failed head resolution yields `source_unavailable` and creates
  no run row, on both the manual and the event path.
