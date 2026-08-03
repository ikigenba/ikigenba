# Phase 49 — Move the run directory into durable state, unified per run

*Realizes design Decision 39 (durable unified run directory). Depends on Phase 48.*

**End state.**

`cmd/prompts/main.go` derives `runsDir` from the **state** tree — `filepath.Join(stateDir, "runs")`, beside `prompts.db` — instead of from the generation cache. `recreateRunsDir` is deleted; nothing removes run directories at startup or on any schedule.

A run's artifacts live in one directory: `<stateDir>/runs/<run_id>/` holding `input/` (`config.json`, `user_prompt.txt`, `system_prompt.txt`, `event.json`), `output.jsonl`, and `sandbox/`. `sandbox.New` is rooted at `<stateDir>/runs` and resolves a run's working directory as `<run_id>/sandbox`; the separate `<stateDir>/sandboxes/` tree is gone. Sandbox confinement is otherwise unchanged — the same symlink-aware containment against the same kind of per-run root.

`log_path` for new runs points inside the run directory. No migration of existing rows or directories is written (D39: there are none).

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/`, and `gofmt -l .` is silent.
- `grep -rn 'recreateRunsDir' --include='*.go' .` returns nothing.
- `grep -rn 'generationPath' --include='*.go' cmd/` shows no use of it in deriving a runs or sandbox path.
- These ids are covered by clearly-named tests:
  - R-ZMJ5-6QEW — after a run, `<stateDir>/runs/<run_id>/` holds `input/config.json`, `input/user_prompt.txt`, `input/system_prompt.txt`, `output.jsonl`, and `sandbox/`; the path is under the database's directory and no run artifact exists under the generation-cache directory.
  - R-ZNR1-KI5L — a completed run's directory survives a second startup against the same state directory byte-identically, and `run_output` still returns its content.
