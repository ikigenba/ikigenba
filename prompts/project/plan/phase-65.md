# Phase 65 — The run workspace is a clone pinned to a sha

*Realizes design Decision 55 (run pinning and the clone workspace) and the
rewritten Decisions 39 (run directory) and 40 (the two loaders). Depends on
Phase 64.*

Spawn resolves `main`'s head, records it as `runs.definition_sha`, and
materializes `<stateDir>/runs/<run_id>/sandbox` as a real `git clone` checked
out on branch `ikigenba/run-<run_id>` at that sha, with `user.name`/`user.email`
set locally. `materializeInput` stops writing definition copies and writes only
`event.json`, for triggered runs only. `LoadFromRun` gains the sha parameter and
reads the definition out of the clone's pinned commit (`git show <sha>:<path>`),
never the working tree; the runner reads its config, user prompt, and system
prompt the same way. `run_fs_list`/`run_fs_read` exclude `.git`.

The clone URL comes from the injected credential; in this phase the tests supply
a `file://` URL for a real bare repository in a temp directory. The authenticated
HTTP door is Phase 66.

Two consequences to carry out in this phase:

- **Delete `R-ZMJ5-6QEW` and its test.** The behavior it pinned (the `input/`
  definition copies) is gone from the design; the id goes with it.
- **Update `AGENTS.md`'s Tests section** to declare the `git` binary as the one
  environmental precondition beyond the Go toolchain, and update the assertion
  behind `R-O1AD-MRKW` to match (D50).

**Done when:**

- `R-RWY3-N0J5` — a run against a head of `A` persists `definition_sha == A`; a
  second run after the head moves to `B` persists `B` and the first row still
  reads `A`.
- `R-RY60-0S9U` — `sandbox/.git` exists, `rev-parse HEAD` equals the run's
  `definition_sha`, `rev-parse --abbrev-ref HEAD` equals
  `ikigenba/run-<run_id>`, and the working tree holds that commit's files.
- `R-S0LS-SBR8` — overwriting `prompt.md` and `config.json` in the working tree
  leaves `RunExecuted` returning the committed text and config, while
  `LoadFromPrompt` returns the new `main` content.
- `R-S1TP-63HX` — a triggered run's `input/` holds exactly `event.json`; a
  manual run's holds nothing; both still read back through `RunExecuted`.
- `R-S31L-JV8M` — `run_fs_list` shows no `.git` entry and `run_fs_read` of
  `.git/config` returns not-found while `prompt.md` reads normally.
- `R-ZNR1-KI5L` — a restart leaves `output.jsonl` and the whole `sandbox/`
  clone byte-identical and `rev-parse HEAD` still equal to `definition_sha`.
- `R-ZOYX-Y9WA` — a new commit on the prompt's `main` leaves `RunExecuted`
  unchanged while `LoadFromPrompt` reflects it.
- `R-ZREQ-PTDO` — after the prompt is deleted and its repository archived,
  `RunList` still returns the run and `RunExecuted` still answers **with the
  version-plane client failing every call**.
- `R-O1AD-MRKW` — the `AGENTS.md` declaration test passes against the updated
  declaration naming `git`.
- `grep -rn 'R-ZMJ5-6QEW' --include='*_test.go' .` returns nothing.
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
