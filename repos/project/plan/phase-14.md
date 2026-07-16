# Phase 14 — Decode github's real issue_comments list envelope

*Realizes design Decision 6 (issue protocol / runner-side GitHub I/O), slice
R-894D-CUA2. Depends on Phase 03 and Phase 04.*

The github peer's list read stops assuming a bare JSON array. github answers
`issue_comments` as MCP structured content, which is always a root object, so it
wraps the list as `{"items": [...]}`. `GitHubPeer.IssueComments`
(`internal/repos/ghpeer.go`) unmarshals into an `{ Items []Comment }` wrapper
and returns `Items`; the single-object verbs (`issue_get`, `pr_create`) are
untouched. The `githubRecorder` test stub (`internal/runner/runner_test.go`) is
corrected to emit github's real MCP structured-content shape — `{"items": [...]}`
for `issue_comments` — carrying a non-empty comment thread, so read-path
assertions drive the real contract instead of the client's old assumption; this
also gives `R-F76X-Q0SV` a non-vacuous comment set. The observable end state: a
session on an issue with comments fetches the full thread, pins it into
`instructions.md`, and proceeds past `FetchIssue` instead of dying with
`cannot unmarshal object into Go value of type []repos.Comment`. No schema
change, no new migration; no github change (its `{"items":[...]}` contract is
correct).

**Done when:**

- R-894D-CUA2 is covered by a clearly-named test: `FetchIssue` against the
  corrected recording stub — answering `issue_comments` with github's real
  `{"items": [...]}` envelope carrying ≥2 comments — returns the issue title,
  body, and every comment in order, and the pinned `instructions.md` contains
  all of them. A variant returning a bare array (the pre-fix stub) fails to
  decode, so the unwrap is what the assertion turns on. The existing D6 tests
  (R-FEIC-0N91, R-FFQ8-EEZQ, R-FGY4-S6QF, R-FI61-5YH4, R-FKLT-XHYI, R-FLTQ-B9P7,
  R-2V8C-1FO6) and R-F76X-Q0SV remain green under the corrected stub.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `go test ./...` exit 0, `gofmt -l .` prints nothing, all from `repos/`).
- Live acceptance (e2e proof, not a minted id): with the fix deployed to
  `int.ikigenba.com`, re-triggering the `execute` label on `ikigenba/agentrepl`
  issue #1 drives a session that fetches the real multi-comment thread from the
  live github service, passes `FetchIssue`, writes `instructions.md`, and runs
  the agent — the wall this pass was opened to remove.
