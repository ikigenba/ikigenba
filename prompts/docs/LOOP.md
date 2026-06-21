# prompts agentkit migration — Build Loop

Human/author-facing overview. This file is never read by the loop itself.

## Invocation

```
ralph prompts/docs/gather.md prompts/docs/build.md prompts/docs/verify.md
```

Run from the repository root. Ralph re-invokes each prompt file in sequence with
a fresh, isolated context each turn.

## Two-status contract

Every prompt ends its final message with exactly one JSON object:

```json
{"status": "NEXT" | "DONE", "message": "..."}
```

- **`NEXT`** — advance to the next prompt (wrapping `verify → gather`).
- **`DONE`** — the build is complete; stop and exit 0. **Only `gather` ever returns `DONE`** (when `STATUS.md` has no `⬜` phase). `build` and `verify` always return `NEXT`.

`CONTINUE` is not used by this loop.

## State machine

```
gather ──NEXT──► build ──NEXT──► verify ──NEXT──► gather ──► ...
   │                                                  │
  DONE                                          (loop back)
   │
  exit 0
```

### The brief lifecycle

`prompts/docs/brief.md` is the seam that keeps `build`'s context small:

1. **gather** creates it — a fresh, self-contained contract for exactly one phase.
2. **build** reads it (and only it) to do one increment of work.
3. **verify** reads it, runs the gate, and **always deletes it** as its final step — pass or gap.

The brief exists only between gather and verify's deletion. It is never committed
(gitignored at `prompts/.gitignore`).

### The `⬜`/`✅` marker

`prompts/docs/plan/STATUS.md` is the **only** home of status markers. The marker
is the sole completion signal:

- `⬜` — not started (or not yet verified green)
- `✅` — verified green by verify

**Only `verify` flips a marker**, and only from `⬜` to `✅`, and only when the
suite is fully green and every Verification id is genuinely covered.

### Why the loop is human-free and converges

- `verify` can neither halt nor advance a phase on a gap — an incomplete phase
  stays `⬜` and gets re-attacked next cycle.
- `build` does one bounded increment and always returns `NEXT`.
- The only exit is `gather` finding zero `⬜` phases, which requires every phase
  to have been verified green.
- Budget rails in `ralph` (`--max-iterations`, `--max-time`, `--max-spend`,
  `--max-tokens`) are the other stop condition — trip any rail and ralph exits non-zero.

## `prompts/docs/brief.md` schema

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - prompts/docs/design/D0n.md

## Ids to cover
R-XXXX-XXXX
R-YYYY-YYYY
# ...one bare id per line, OR the single line:
# (none — structural phase)

## Files to touch
- prompts/<path>
- prompts/<path>

## Dependency interfaces (copied from design — do not open design files)
```go
// package <dep>  (from D0k)
<copied type / func / const signatures>
```

## Done bar
- Every id under "Ids to cover" is covered by a genuinely-asserting test tagged
  with a `// R-XXXX-XXXX` comment (structural phase: green build + named smoke).
- The suite is green:
    cd prompts && go build ./...
    cd prompts && go vet ./...
    cd prompts && gofmt -l .          # prints nothing
    cd prompts && go test ./...
    bin/check-migrations prompts
- <any phase-specific check, copied verbatim from the phase body>
```

## Project conventions (inlined in build and verify)

- **Toolchain:** Go 1.26, `module prompts` rooted at `prompts/`.
- **"The suite is green":** `go build ./...`, `go vet ./...`, `gofmt -l .`
  (prints nothing), `go test ./...`, and `bin/check-migrations prompts` — all
  from `prompts/`, all zero failures. Race detection is implicit in CI.
- **Migrations:** `bin/new-migration prompts <name>` — never hand-author a version.
  Never edit a committed migration.
- **Coverage convention:** a Verification id (`R-XXXX-XXXX`) is covered only when
  it appears in a `// R-XXXX-XXXX` comment in a `*_test.go` file and the
  surrounding test genuinely asserts the described behavior. A structural phase
  (no ids) is proven by the green suite plus its named smoke check.
- **Test seams:** `validateConfig(c Config, getenv func(string) string) error` for
  environment injection; `Runner.buildProvider` and `Runner.discover` are
  injectable fields for runner tests. A `fakeProvider` (implements `Name`,
  `Pricing`, `RoundTrip`) returns a canned one-turn `FinishStop` response so no
  test requires a live API key. The suite is green offline.
