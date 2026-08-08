# Phase 67 — The framing prompt tells the run about its clone

*Realizes design Decision 57 (framing prompt additions). Depends on Phase 66.*

`framingPrompt` becomes `framingPrompt(branch, sha string) string` and the
runner passes the run's real values. The new text states that the folder is a
git clone of this prompt's own repository at the named commit on the named
branch, that new branches belong under `ikigenba/`, that `main` cannot be pushed
or force-pushed, and that merging happens through the version-control service's
merge tool only when the prompt's instructions ask for it. The sandbox-tool
sentence, the file-share paragraph, the PDF sentence, and the verbatim "You have
NO network access from bash:" sentence are untouched, and no individual suite
service is named.

**Done when:**

- `R-S953-GPY3` — the assembled `System` string states the clone, the branch
  namespace, the `main` prohibition, and the conditional merge, contains the
  verbatim "You have NO network access from bash:" sentence, and names no
  individual suite service (no `repos`, no `ikigenba_`).
- `R-SACZ-UHOS` — the branch name and sha in the assembled `System` equal
  `git -C <sandbox> rev-parse --abbrev-ref HEAD` and `rev-parse HEAD` for the
  spawned run.
- The existing composing assertions stay green (`R-ZK3C-F6XI`, `R-6AUG-NHQY`,
  `R-6I5U-Y474`, `R-FEGC-LVD7`).
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
