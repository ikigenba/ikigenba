# Phase 42 — Give the `AGENTS.md` claim a real test home, and clear the orphan rate-limiter tag

*Realizes design Decision 6 (`AGENTS.md` states the current page truth, and a test holds it there).*

Two small, independent edits under `dashboard/`. Neither changes production
behavior; both close a coverage-hygiene gap the tree carries today.

**A. `cmd/dashboard/docs_test.go` — new file, `package main`.**

`R-DB16-DOCS` is a current design id with no possible tag site: its proof was a
by-hand text search, so no `*_test.go` could carry it and the tree read as
permanently one id short. D6 now names the substrate. Create
`cmd/dashboard/docs_test.go` holding one test, tagged `// R-DB16-DOCS`, that
reads `../../AGENTS.md` from disk (the same relative-path idiom the committed
manifest tests in `cmd/dashboard/main_test.go` already use) and asserts, with a
separate reported failure per condition:

- **Absent, case-insensitively:** `single hybrid page`, `IAM console`,
  `telemetry`.
- **Present:** the four-page statement (`four pages`), and each of `login`,
  `landing`, `profile`, `metrics`.
- **Present:** the statement that personal-access-token and OAuth-grant
  management live on the **profile** page and not the landing.
- **Present:** `metrics` inside the `internal/` package list.

The assertions are fixed substrings, not a golden-file comparison — unrelated
prose in `AGENTS.md` must stay free to change. `package main` already provides a
`contains` helper in `main_test.go`; reuse it or use `strings` directly rather
than adding a second copy. `AGENTS.md` satisfies every condition today, so the
test is expected to pass on first run; that is the point — it pins the state,
and the perturbation check below is what proves it is not vacuous.

Do **not** edit `AGENTS.md` in this phase (and never `CLAUDE.md`, which is a
symlink to it). If a condition fails, that is a real finding: report it rather
than rewriting the doc to fit the test.

**B. `internal/ratelimit/ratelimit_test.go` — delete two stale id references.**

`R-K5RY-83NL` appears at `ratelimit_test.go:8` and `:73` but belongs to no
current design Decision — it was minted by an earlier generation of this spec
that has since been rewritten in place, and no successor id replaced it. The
limiter itself is live, correct, exercised code: **the tests stay exactly as they
are.** Remove only the id token from the two comments, keeping the surrounding
prose that explains what each test covers:

- line 8: `// R-K5RY-83NL: sliding-window limiter keyed by oauth_tokens.id.` →
  the same sentence without the id prefix.
- line 73: the `(R-K5RY-83NL)` parenthetical inside the `TestLimiter_EmptyKey`
  comment block → drop the parenthetical, keep the explanation.

No test body, name, or assertion changes. Afterwards `R-K5RY-83NL` must not
appear anywhere in the tree.

**Done when:**

- `R-DB16-DOCS` — `cmd/dashboard/docs_test.go` exists and its test asserts every
  condition listed in section A above; the id appears verbatim as a
  `// R-DB16-DOCS` tag on that test.
- The docs test is **genuine**, verified by perturbation: temporarily insert the
  string `single hybrid page` into `AGENTS.md` and confirm the test fails;
  temporarily delete the `metrics` entry from the `internal/` package list and
  confirm the test fails; restore `AGENTS.md` to its committed content
  (`git diff --exit-code dashboard/AGENTS.md` exits 0) and confirm the test
  passes. No `t.Skip`, no assertion that passes against an empty or unreadable
  file — a read error must fail the test, not skip it.
- `R-K5RY-83NL` is gone from the tree. From `dashboard/`, this prints exactly
  `0`:

  ```
  grep -rc 'R-K5RY-83NL' . | grep -v ':0$' | wc -l
  ```

- The rate-limiter tests are otherwise untouched: from the repo root,
  `git diff -- dashboard/internal/ratelimit/ratelimit_test.go` shows only comment
  lines changed, and `go test ./internal/ratelimit/...` passes.
- The suite is green per design's *Conventions*, from `dashboard/`:
  `go build ./...` exits 0, `go vet ./...` is clean, `gofmt -l .` prints nothing,
  and `go test ./...` passes with no failures and no `SKIP`.
- From `dashboard/`, the four ids this tree's coverage turns on are each tagged in
  **test** files (not merely quoted in the spec) — this command prints exactly
  `4`:

  ```
  grep -rhoE 'R-DB16-DOCS|R-4LKF-FB23|R-8DF1-W89F|R-8IAN-FB87' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l
  ```

- From `dashboard/`, the tree-local coverage check prints nothing:

  ```
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```
