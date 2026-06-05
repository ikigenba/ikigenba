# agentkit extraction — move-list + job-runner seam (Task 0.1)

> Status: **design note** for Task 0.1. Read-only analysis; **no code moved**.
> Authoritative on conflict: `wiki/GOALS.md` and `wiki/PLAN.md`. This note exists
> so the Task 1.1 / 1.2 subagents can perform the move + import rewrite
> mechanically without re-deciding the boundary.

## Scope & method

Classified every file under `ralph/internal/engine/**` plus the adjacent
`ralph/internal/{runner,session,sandbox}`. Verified the import graph:

- **No engine package imports any non-engine ralph package** (`grep` of
  `ralph/internal/engine/**` for `ralph/internal/{runner,session,sandbox,db,ids,…}`
  → zero hits). The whole `engine/` subtree is self-contained.
- Engine's internal graph is a clean DAG rooted at `wire` (the leaf): `wire` →
  imported by `tools/*` and `agent`; `provider` → imported by `agent`,
  `provider/anthropic`; `trace` → imported by `agent`, `provider/anthropic`;
  `model`, `schema` → imported by `agent`. **No cycles.**
- Only **four** non-engine ralph files import the engine:
  `runner/runner.go`, `session/service.go`, `session/store.go` (no — only
  `service.go`), and the runner tests. Concretely the production importers are
  `runner/runner.go` (imports `agent`, `model`, `provider`, `provider/anthropic`,
  `tools`, `wire`) and `session/service.go` (imports `model` only, for config
  validation). Plus test files.

This makes Decision #2 (physical move + mechanical import rewrite) safe: moving
`engine/*` → `agentkit/*` and rewriting `ralph/internal/engine/...` →
`agentkit/...` cannot create an import cycle, because nothing in engine points
back at ralph today.

---

## Move-list

Target module is **`agentkit`** (`module agentkit`, `go 1.26`), wired via
`replace agentkit => ../agentkit` in `ralph/go.mod` and `wiki/go.mod`, and
`./agentkit` added to the root `go.work`.

**Package-path rule:** drop the `internal/engine/` segment. `ralph/internal/engine/<pkg>`
→ `agentkit/<pkg>`. Keep packages **exported** (top-level under `agentkit/`, not
under an `internal/`) so a second module (`wiki`) can import them — `internal/`
would re-block cross-module use, the exact trap qmd hit (PLAN Decision #1).

### Files that MOVE to `agentkit/`

| source path | target `agentkit/` path | generic? | notes |
|---|---|---|---|
| `ralph/internal/engine/wire/` (all `*.go` incl. tests: `wire.go`, `decode.go`, `event.go`, `result.go`, `session.go`, `stdin_reader.go`, `text_block.go`, `thinking_block.go`, `tool_result_block.go`, `tool_use_block.go`, + all `*_test.go`) | `agentkit/wire/` | yes | stream-json NDJSON codec + `wire.Session` sink. Leaf package, zero ralph deps. Move whole dir verbatim. |
| `ralph/internal/engine/provider/provider.go`, `provider_test.go`, `clone_blocks_test.go`, `error_message_test.go` | `agentkit/provider/` | yes | provider-neutral `Client`/`Request`/`Event`/`Block`/`Error` abstraction. No ralph deps. |
| `ralph/internal/engine/provider/anthropic/` (`anthropic.go`, `anthropic_test.go`, `maxtokens_test.go`) | `agentkit/provider/anthropic/` | yes | Anthropic streaming client (pure net/http, CGO-free). Imports only `provider` + `trace`. |
| `ralph/internal/engine/trace/trace.go` | `agentkit/trace/` | yes | optional debug tracer + key redaction. Zero ralph deps. |
| `ralph/internal/engine/model/` (`model.go`, `registry.go`, `model_test.go`, `registry_test.go`) | `agentkit/model/` | yes | model alias/provider resolution + pricing/context registry. **See Risk 4** — the alias map is ralph-flavored but the package is generic; move as-is, tune data later. |
| `ralph/internal/engine/schema/` (`schema.go`, `schema_test.go`) | `agentkit/schema/` | yes | minimal JSON-Schema subset for structured-output validation. Zero ralph deps. wiki's ingest agent is freeform (`sch==nil`) so it won't use it, but it's generic and `agent` imports it. |
| `ralph/internal/engine/tools/confine.go`, `confine_test.go`, `dispatch_confine_test.go` | `agentkit/tools/` | yes | path-confinement helpers used by `dispatch`. **See Risk 2** (reconcile with `sandbox.confine`). |
| `ralph/internal/engine/tools/tools.go`, `tools_test.go` | `agentkit/tools/` | yes | tool registry (`All()`, `Select()`, `Descriptor`). **See Risk 3** — `All()` is a fixed set; wiki needs a per-consumer toolset. Move now, generalize in 1.2/4.1. |
| `ralph/internal/engine/tools/dispatch.go` | `agentkit/tools/` | yes | routes a `tool_use` block to its impl. Generic. |
| `ralph/internal/engine/tools/bash/` (`bash.go`, `bash_test.go`, `export_test.go`, `schema_test.go`) | `agentkit/tools/bash/` | yes | foreground `bash -c` tool. Pure-Go. |
| `ralph/internal/engine/tools/read/` (`read.go`, `read_test.go`, `schema_test.go`) | `agentkit/tools/read/` | yes | read tool. |
| `ralph/internal/engine/tools/write/` (`write.go`, `write_test.go`) | `agentkit/tools/write/` | yes | write tool. |
| `ralph/internal/engine/tools/edit/` (`edit.go`, `edit_test.go`) | `agentkit/tools/edit/` | yes | edit tool. |
| `ralph/internal/engine/tools/glob/` (`glob.go`, `glob_test.go`) | `agentkit/tools/glob/` | yes | glob tool. |
| `ralph/internal/engine/tools/grep/` (`grep.go`, `grep_test.go`) | `agentkit/tools/grep/` | yes | grep tool. |
| `ralph/internal/engine/agent/loop.go`, `loop_test.go` | `agentkit/agent/` | yes | the tool-use loop (`agent.Run`). The core generic machinery. Imports `model`, `provider`, `schema`, `tools`, `trace`, `wire` — all moving with it. |
| `ralph/internal/engine/agent/prompt.go` | `agentkit/agent/` | **PARTIAL** | `FramingPrompt` const is **ralph-specific** ("Ralph pattern", "leave deliverables as FILES", "no network"). **See Risk 5.** Move the *file* so `agent` compiles, but treat the constant as a default ralph keeps overriding; wiki supplies its own system prompt (the schema doc) at the composition root. No code change needed — `agent.Run` already takes the prompt via `provider.Request.SystemPrompt`, not from this const. |

### Things that STAY in ralph (do NOT move)

| source path | disposition | why |
|---|---|---|
| `ralph/internal/runner/` (`runner.go`, `runner_test.go`, `runner_maxtokens_test.go`) | **STAYS in ralph** | ralph-specific async lifecycle: it knows `session.Store`, `session.Run`, `sandbox.Manager`, run-log files, TTL classification, `ANTHROPIC_API_KEY` read. PLAN 1.2 builds a **fresh greenfield** `agentkit/job` *informed by* this; ralph migrates onto it only in Phase 7. After 1.1, `runner.go`'s imports are rewritten `ralph/internal/engine/...` → `agentkit/...` but the file does not move. |
| `ralph/internal/session/` (`model.go`, `service.go`, `store.go`, `*_test.go`) | **STAYS in ralph** | ralph's domain: sessions+runs state machine, single-flight gate, owner-scoping, SQLite store. wiki gets its **own** store (PLAN 3.1) behind agentkit's job-store interface. `service.go` keeps its one engine import (`model`), rewritten to `agentkit/model`. |
| `ralph/internal/sandbox/` (`sandbox.go`, `sandbox_test.go`) | **STAYS in ralph** | ralph's per-session folder manager (`data/sandboxes/<id>/`, ULID-keyed). wiki has a different on-disk model (owner+collection tree, PLAN 3.1). **But** its `confine`/`resolveLongestExisting` logic is a duplicate of `tools/confine.go` — **see Risk 2**: the *reusable confinement primitive* should live in agentkit; the *Manager lifecycle* stays per-consumer. |
| `ralph/internal/{db,ids,logging,mcp,server}`, `ralph/cmd/**` | **STAYS in ralph** | not in scope; ralph chassis. (wiki clones the equivalents from ledger in Phase 2.) |

---

## Job-runner store seam (the new `agentkit/job` package)

PLAN Decision #2: `agentkit/job` is **greenfield**, *informed by* `runner.go` +
`session/{service,store}.go`, **not** moved from them. The behaviors to
reproduce, distilled from the ralph reference:

- **spawn async** (`runner.Spawn` → goroutine + `context.WithTimeout(ttl)`),
- **single-flight gate** (`session.Service.Run`: load status, reject `ErrBusy` if
  already running, flip to running on the one serialized SQLite connection before
  spawning),
- **cancel** in-flight, distinguishing user-cancel from TTL (`runner.Cancel` +
  `userCancelled` map → cancelled vs failed classification),
- **terminal write** on a fresh background ctx (`runner.finish` →
  `store.UpdateRunTerminal` + flip owner state back to idle),
- **crash-recovery sweep** at boot (`runner.Recover` → `store.SweepRunning`:
  every `running` run → `failed` + owner back to idle, transactionally).

The seam: agentkit owns the **lifecycle**; the consumer owns **persistence** and
the **work**. Two interfaces — a `Store` (persistence the consumer implements
over its own DB) and a `Job` (the unit of work the consumer supplies). Status and
record types live in agentkit so both consumers agree on them.

```go
// Package job is agentkit's generic async agent-job lifecycle: spawn a run on a
// goroutine with context cancellation + TTL, gate single-flight per key, write
// terminal state, and sweep crash-orphaned runs at boot. Persistence and the
// actual work are supplied by the consumer via Store and Job.
package job

import (
	"context"
	"time"
)

// Status is a run's lifecycle state. Mirrors ralph's session.Run* values so a
// later ralph retrofit is a rename, not a remodel.
type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether s is an end state (anything but running).
func (s Status) Terminal() bool { return s != StatusRunning }

// Record is the persisted shape of one run. The consumer's Store maps it to its
// own table (ralph: runs; wiki: a job-record table from 002_wiki.sql). Generic
// fields only — no session/collection/owner columns here; the consumer carries
// those in its own row and joins on ID / FlightKey.
type Record struct {
	ID        string    // run id (consumer-minted; ULID in the suite)
	FlightKey string    // single-flight key: at most one running run per key
	Status    Status    // running | succeeded | failed | cancelled
	StartedAt time.Time
	EndedAt   time.Time // zero until terminal
	UsageJSON string    // opaque accounting blob captured from the result stream; "" if none
	Error     string    // terminal error message; "" on success
}

// Store is the persistence seam the consumer implements over its own DB. All
// methods are called by the runner; the consumer never calls them directly. The
// single-flight guarantee rests on Insert+SetRunning happening under one
// serialized writer (ralph relies on SQLite's single connection; wiki inherits
// the same db.Open).
type Store interface {
	// Insert persists a new record in StatusRunning. It MUST fail (return a
	// non-nil error, conventionally ErrFlightInUse) if another record with the
	// same FlightKey is already StatusRunning — this is the single-flight gate.
	// Returning that error makes Runner.Spawn report rejection without launching
	// a goroutine.
	Insert(ctx context.Context, rec Record) error

	// Load returns the record by id, or ErrNotFound if absent. Consumers layer
	// owner-scoping on top (a foreign-owned id reads as ErrNotFound), as ralph's
	// store already does.
	Load(ctx context.Context, id string) (Record, error)

	// UpdateTerminal writes the run's end state: status (one of the terminal
	// values), endedAt, the usage blob, and an error message. Called on a fresh
	// background context because the run's own ctx may be cancelled/expired by
	// the time we persist.
	UpdateTerminal(ctx context.Context, id string, status Status, endedAt time.Time, usageJSON, errMsg string) error

	// SweepRunning is boot-time crash recovery: every record still StatusRunning
	// (orphaned by a crash) is flipped to StatusFailed with endedAt + a
	// "interrupted by restart" error, transactionally, returning the count
	// swept. Mirrors session.Store.SweepRunning.
	SweepRunning(ctx context.Context) (int, error)
}

// Job is the unit of work the consumer supplies for one run. Run executes the
// agent (ralph: agent.Run over a session sandbox; wiki: the ingest integration
// pass), honoring ctx for cancellation/TTL. It returns the usage blob to persist
// and an error (nil = succeeded). The runner classifies the terminal Status from
// (ctx state, user-cancel, err).
type Job interface {
	Run(ctx context.Context) (usageJSON string, err error)
}

// Runner owns the lifecycle. It holds the Store, the per-run TTL, and the
// in-flight cancel registry; it is the only thing that writes terminal state.
type Runner struct {
	// store Store; ttl time.Duration; mu sync.Mutex;
	// cancels map[string]context.CancelFunc; userCancelled map[string]bool
}

// New builds a Runner over store with a per-run wall-clock ttl.
func New(store Store, ttl time.Duration) *Runner { /* ... */ return nil }

// Spawn gates single-flight (via Store.Insert on rec.FlightKey), and on success
// launches job on a goroutine bounded by ttl, returning the persisted Record.
// It returns ErrFlightInUse (without spawning) when FlightKey already has a
// running record. Spawn returns immediately; terminal state is written by the
// goroutine via Store.UpdateTerminal.
func (r *Runner) Spawn(rec Record, job Job) (Record, error) { /* ... */ return Record{}, nil }

// Cancel signals the in-flight run for id, marking it user-cancelled (so it is
// classified Cancelled, not Failed) and triggering context cancellation. Returns
// whether a run was in flight. Idempotent.
func (r *Runner) Cancel(id string) bool { /* ... */ return false }

// Recover is the boot-time crash sweep; delegates to Store.SweepRunning.
func (r *Runner) Recover(ctx context.Context) (int, error) { /* ... */ return 0, nil }

// Sentinel errors.
var (
	ErrNotFound    = /* errors.New("job: not found") */ error(nil)
	ErrFlightInUse = /* errors.New("job: a run is already in flight for this key") */ error(nil)
)
```

**Notes on the seam (so 1.2 doesn't re-litigate):**

- **`FlightKey` is the generalization of ralph's `session_id`.** ralph's
  single-flight is "one running run per *session*"; the gate keys on `session_id`.
  wiki's is "one running ingest per *raw doc* (sha256)" or per *collection* —
  whatever wiki chooses. Keeping it a free string keeps agentkit ignorant of the
  consumer's domain.
- **The single-flight invariant is enforced in `Store.Insert`, not in the
  runner.** That matches ralph, where the atomicity comes from SQLite's single
  serialized connection (`session.Service.Run` comment). agentkit must *document*
  that the consumer's `Insert` is responsible for the conflict check; the runner
  only reacts to `ErrFlightInUse`. (Alternative: pass a `CheckFlight` step — but
  folding it into `Insert` keeps it one atomic write, which is what makes ralph's
  gate race-free.)
- **`Job` is an interface, not a func, so the consumer can carry state** (wiki's
  ingest job carries the raw bytes + store + search reindex closure; ralph's
  carries the session + sandbox root + client factory). `agent.Run` is called
  *inside* the consumer's `Job.Run`, not by agentkit's runner — the runner is
  agnostic to whether the work even uses an LLM.
- **Owner-scoping stays in the consumer.** agentkit's `Record` has no owner field;
  ralph and wiki both layer "foreign-owned id reads as not-found" in their own
  `Store.Load` (as ralph already does). This keeps the trust model
  (`X-Owner-Email`) entirely consumer-side.
- **Usage capture** (`runner.captureUsage`, scanning the streamed result event
  for `usage`/`modelUsage`/`total_cost_usd`) is generic and belongs in agentkit
  as a helper the `Job` can call to produce its `usageJSON`; it depends only on
  the wire result-event shape, which is moving to `agentkit/wire`.

---

## Risk notes (things that do NOT lift cleanly)

### Risk 1 — runner↔session/store coupling is deep; do NOT try to move the runner
`runner.go` is welded to ralph: `session.Store` (typed, not an interface),
`sandbox.Manager`, run-log file paths, TTL→status classification,
`ANTHROPIC_API_KEY` env read, `model.Resolve`/`ModelContext` for max-tokens, and
`captureUsage` over the wire stream. **It is not a generic job-runner; it's
ralph's.** Mitigation: PLAN already says so — `agentkit/job` is **greenfield**
(1.2), the runner **stays** (1.1 only rewrites its engine imports), ralph
retrofits in Phase 7. The seam above is the distillation; do not attempt to lift
`runner.go` itself. Note `session.Service` depends on a `session.Runner`
*interface* (`Spawn`/`Cancel`) it defines locally — that interface is the
template for `agentkit/job`'s but is **not** itself moved (it names
`session.Session`/`session.Run`).

### Risk 2 — TWO copies of path-confinement; reconcile by extracting the primitive
There are two near-identical implementations of "resolve a path under a root,
reject escapes, catch symlink escapes on the longest-existing ancestor":

- `ralph/internal/engine/tools/confine.go` — `confinePath`, `effectiveSearchPath`,
  `resolveLongestExisting` (the **engine/tool-dispatch** copy; root may be `""` =
  unconfined).
- `ralph/internal/sandbox/sandbox.go` — `confine`, `resolveLongestExisting` (the
  **sandbox Manager** copy; root must exist, error prefixes say "sandbox:").

The algorithms are the same (join → `Clean` → `EvalSymlinks` longest-existing
ancestor → `filepath.Rel` containment check). **Recommended split:**

1. The **confinement primitive** moves to agentkit with `tools/confine.go` (it's
   already generic and CGO-free). Optionally promote it to a tiny exported helper
   (e.g. `agentkit/confine` with `Confine(root, p string) (string, error)` and
   `ResolveLongestExisting`) so both the tool dispatch *and* a consumer's
   filesystem manager can call **one** implementation.
2. `sandbox.Manager` **stays in ralph** (it's ralph's session-folder lifecycle),
   but in a *later* cleanup it should call agentkit's confine primitive instead of
   its private copy, deleting the duplicate. **For Task 1.1, do NOT change
   sandbox** — leaving the duplicate is green and in-scope-minimal; just be aware
   the canonical copy now lives in agentkit. wiki's own filesystem store (PLAN
   3.1) should call agentkit's confine primitive from day one rather than copy a
   third time. Flag for the 1.1 subagent: keeping `tools/confine.go`'s package as
   `package tools` is the zero-risk move; promoting to a standalone
   `agentkit/confine` package is a nice-to-have that can wait for 3.1.

### Risk 3 — `tools.All()` is a fixed registry; wiki needs a per-consumer toolset
`tools.All()` returns a hardcoded six-tool set, and `runner.buildRequest`
advertises all of them. wiki's ingest agent is **write-enabled, sandbox-confined,
no bash-with-network**, and `wiki_ask` is **read-only**. A fixed `All()` doesn't
fit. The package has `Select(csv string)` already, which is a start.
**Recommended split:** move `tools` as-is in 1.1 (ralph keeps using `All()`
unchanged — no behavior change, acceptance gate stays green). In 1.2/4.1,
generalize toolset selection at the **consumer/composition-root** level: the
consumer builds its `[]provider.Tool` from `tools.Select(...)` (or a new
explicit-list constructor) and `Dispatch` already dispatches by name, so a
consumer that never advertises `bash` simply never receives a `bash` tool_use.
**No agentkit change is required for wiki to restrict its toolset** — it just
advertises fewer descriptors. Confirm in 4.1; nothing to redesign in 1.1.

### Risk 4 — "generic" packages carry ralph-flavored data/assumptions
- `model/registry.go` + `model.go`: alias map (`opus`/`sonnet`/`haiku`),
  pricing/context tables, and `DefaultEffort`. The *code* is generic; the *data*
  is ralph's current pin. Move as-is; wiki reads model + cost ceiling from config
  (PLAN Decision #3 / Task 4.1), so wiki overrides at the edge, not by forking the
  registry. Low risk — just don't treat the alias list as frozen API.
- `schema` package: built for ralph's `--json-schema` structured-output path.
  wiki ingest is freeform (`agent.Run(..., sch=nil, ...)`), so wiki won't exercise
  it, but `agent` imports it unconditionally, so it **must** move with `agent`.
  Generic enough; no split needed.
- `wire` package + `trace` package doc comments mention "ralph-loops",
  "ikigai-cli", stdin/stdout framing. Purely cosmetic (comments + the
  `stdin_reader`); the code is generic NDJSON. Move verbatim; optionally retitle
  doc comments later. The `stdin_reader.go` (reads events *from* an upstream
  ralph-loops process) is unused by wiki but harmless and part of the wire
  package's surface — move it with the package.

### Risk 5 — `agent.FramingPrompt` is ralph-specific (the "Ralph pattern")
`agent/prompt.go`'s `FramingPrompt` const hardcodes ralph's worldview ("autonomous
agent in a single persistent folder", "leave deliverables as FILES", "no network
from bash", "the Ralph pattern", "final message recorded as the result"). This is
**not** generic. **But it lifts cleanly anyway** because `agent.Run` does **not**
read this const — `runner.buildRequest` injects it via
`provider.Request.SystemPrompt`. So:

- Move `prompt.go` with the `agent` package (keeps ralph compiling unchanged —
  ralph's `runner` still references `agent.FramingPrompt`).
- Treat `FramingPrompt` as **ralph's default**, not agentkit's contract. wiki
  never references it; wiki's composition root sets `Request.SystemPrompt` to its
  **schema doc + integration-pass instructions** (PLAN 4.1). No code change in
  1.1; just don't let a later subagent assume `FramingPrompt` is "the agentkit
  system prompt". (Optional later cleanup: rename to `RalphFramingPrompt` or move
  it back into ralph's `runner` package, since nothing else in `agent` uses it —
  but that's a behavior-neutral nicety, not required for the move.)

### Risk 6 (minor) — no import cycles, but watch the test files
All `*_test.go` move with their packages. Two runner tests
(`runner_test.go`, `runner_maxtokens_test.go`) import engine packages **and**
ralph domain packages (`db`, `ids`, `sandbox`, `session`) — those tests **stay
with the runner in ralph** and just get their `ralph/internal/engine/...` imports
rewritten to `agentkit/...`. They are the regression gate for Task 1.1
(acceptance: ralph's suite passes unchanged). No cycle is introduced because
ralph depending on agentkit is one-directional and agentkit never imports ralph.

---

## Mechanical checklist for Task 1.1 (so the move is decision-free)

1. `mkdir agentkit/`; `agentkit/go.mod` = `module agentkit` + `go 1.26`.
2. `git mv` each engine subtree per the move-list table (drop `internal/engine/`):
   `wire`, `provider` (+ `provider/anthropic`), `trace`, `model`, `schema`,
   `tools` (+ `bash`/`read`/`write`/`edit`/`glob`/`grep`), `agent`.
3. Global import rewrite **inside the moved tree and inside ralph**:
   `ralph/internal/engine/` → `agentkit/` (sed/`gofmt -r` over `*.go`).
4. `ralph/go.mod`: add `require agentkit v0.0.0` + `replace agentkit => ../agentkit`.
   Add `./agentkit` to root `go.work`. (wiki gets the same `replace` in Phase 2,
   even before it imports anything.)
5. **Do not touch** `runner/`, `session/`, `sandbox/` except for the import-path
   rewrite in step 3 (they only reference engine via those paths).
6. Gate: `go build ./...` clean across the workspace; `go test ./...` in both
   `agentkit` and `ralph` green (ralph's behavior unchanged).
</content>
</invoke>
