# Phase 15 — Decode github's created-PR `html_url` and fail loud on a blank PR address

*Realizes design Decision 6 (issue protocol / runner-side GitHub I/O), slice
R-APSC-24AL. Depends on Phase 03 and Phase 04.*

The github peer's `pr_create` decode stops assuming a bare `url` field. github
answers the created PR's web address in **`html_url`** (its D9 mirrors GitHub's
REST field names), so `PR` (`internal/repos/ghpeer.go`) decodes
`URL string ``json:"html_url"``` — a bare object as before, but the tag now
matches github's real shape, so `pr.URL` is the actual PR URL instead of empty.
`Success` (`internal/repos/protocol.go`) gains a boundary guard: after
`PRCreate`, an empty `pr.URL` is a corrupt/unknown state, so it returns an error
(the session ends `failed` with a clear reason) rather than posting an empty
PR-link comment and recording a blank `pr_url`. The `githubRecorder` test stub
(`internal/runner/runner_test.go`) is corrected to answer `pr_create` with
github's real created-PR shape `{"number":N,"html_url":"…"}`, so success-path
assertions drive the real contract instead of the client's old `url` assumption.

The observable end state: a session that opens a PR records that PR's real URL in
`sessions.pr_url`, posts it as the issue's PR-link comment, and carries it on the
outcome event's `PRURL` — the whole downstream chain (which already threads
`pr.URL`) lights up. No schema change, no new migration; no github change (its
`html_url` contract is correct).

**Done when:**

- R-APSC-24AL is covered by a clearly-named test: `Success` against the
  corrected recording stub — answering `pr_create` with github's real
  `{"number":N,"html_url":"https://…"}` shape — sets the session's `pr_url` to
  that `html_url`, sends exactly one `issue_comment` whose body **is** that
  non-empty URL, and ends the session `succeeded`. A variant answering
  `{"number":N,"url":"…"}` (the pre-fix assumption) leaves `pr.URL` empty, so the
  assertion turns on the `html_url` tag. A variant whose `pr_create` returns an
  empty `html_url` drives `Success` to fail loud: it returns an error, sends no
  empty-body `issue_comment`, and the session ends `failed` — never `succeeded`
  with a blank `pr_url`. The existing D6 tests (R-FDAF-MVIC, R-FEIC-0N91,
  R-FFQ8-EEZQ, R-FGY4-S6QF, R-FI61-5YH4, R-FKLT-XHYI, R-FLTQ-B9P7, R-2V8C-1FO6,
  R-894D-CUA2) remain green under the corrected stub — in particular R-FEIC-0N91's
  "`pr_url` set" now asserts a non-empty github URL.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `go test ./...` exit 0, `gofmt -l .` prints nothing, all from `repos/`).
- Live acceptance (e2e proof, not a minted id): with the fix deployed to
  `int.ikigenba.com`, re-triggering the `execute` label on `ikigenba/agentrepl`
  issue #1 drives a session to a real PR whose `html_url` lands both as a comment
  on the issue and in the session's `pr_url` (a non-empty
  `https://github.com/ikigenba/agentrepl/pull/<n>`), instead of the empty comment
  and blank `pr_url` this pass was opened to remove.
