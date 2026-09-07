# D5-agents-and-pass

An **agent** is one agentkit conversation with one turn: a fixed system
prompt for its role, one user message holding the prompt it was given, a
tool set for its role, its role's context limit, and an event log whose id is
its address. It ends when the turn ends. Its **report** is the text of the
last assistant message of that turn.

A **pass** is one root supervisor and everything it delegates. `RunPass`
runs it and returns the root's report and the pass's total usage.

```go
package agent

type Role string

const (
    RoleSupervisor Role = "supervisor"
    RoleWorker     Role = "worker"
)

// The fixed system prompts. Their text is tuned in place and is not contract.
const SupervisorPrompt = `...`
const WorkerPrompt = `...`

// Trace is what the pass tells the outside as it runs. The render package
// satisfies it (D6); tests use a recording fake.
type Trace interface {
    Event(address string, ev agentkit.Event)
    Error(address string, err error)
    Record(rec agentkit.LogRecord) string // the transcript text of a log record
}

type Config struct {
    Store      *store.Store
    Supervisor *model.Factory
    Worker     *model.Factory
    Root       string              // worker tool root
    Now        func() time.Time    // log clock
    Trace      Trace
}

type Result struct {
    Address string        // the root's address, the pass number as text
    Report  string        // the root's report; "" when the root produced no text
    Usage   agentkit.Usage // sum over every agent of the pass
    Cost    agentkit.Cost
}

func RunPass(ctx context.Context, cfg Config, prompt string) (Result, error)
```

**Addresses.** The root of pass *p* is `p`. A child's address is its parent's
address, a `.`, and the child's ordinal among that parent's children,
starting at 1: `3`, `3.1`, `3.2`, `3.2.1`. The ordinal is assigned when
`delegate` is called, so it is the order of delegation. The address is the
only identity an agent has, and it is the id on every log record the agent
writes and the address on every store entry written for it.

**Running one agent.** The harness stores a `prompt` entry at the address,
builds a `LogWriter` at the address with `Trace.Record` as its renderer,
wraps it in `agentkit.NewLog(writer, cfg.Now, address)`, asks the role's
factory for a conversation with the role's tools and that log, adds the
role's fixed prompt as the system message, and sends the prompt as one `Text`
block. Every event of the stream goes to `Trace.Event` with the address as it
arrives. When the stream ends:

- cleanly: the report is the `Text` blocks of the last `MessageDone` whose
  message has role assistant, joined by `\n`, or `""` if there was none; a
  `report` entry is stored at the address; the log is closed; the agent's
  usage is read from the writer's summary.
- with an error (`Stream.Err()` non-nil, including `agentkit.ErrLimitExceeded`
  and context cancellation): `Trace.Error` is called with the address, no
  `report` entry is stored, the log is closed, and the error is returned to
  whoever ran the agent — the parent's `delegate` tool, or `RunPass`.

**Supervisor tools.** Four, all built with `agentkit.NewTool`, each closed
over the agent's address:

| tool       | input                                                                 | result                                        |
|------------|-----------------------------------------------------------------------|-----------------------------------------------|
| `search`   | `query` string (required); `kind` string; `address` string; `page` integer ≥ 1 | the `store.Page` as JSON                |
| `fetch`    | `id` integer (required)                                               | the `store.Entry` as JSON                     |
| `remember` | `text` string (required, min length 1)                                | `ok`; stores a `note` at the agent's address  |
| `delegate` | `role` string enum `supervisor`,`worker` (required); `prompt` string (required, min length 1) | the child's report |

`kind` is a comma-separated list of kinds, each optionally prefixed with `-`
to exclude it: `note,report` keeps two kinds, `-transcript` drops one. An
unknown kind is a tool error. `address` is a prefix in the D4 sense.

**Delegate.** The tool runs the child to completion inside the call, so the
parent's turn blocks until the child reports. The child's address is the
parent's address plus the next ordinal. A clean child returns its report as
the tool's content. A child that ends with an error returns a tool error
whose text names the child's address and carries the error's text, so the
parent sees `child 3.2 terminated: agentkit: context limit exceeded ...` as
an `IsError` result and decides what to do. Nothing is retried. Depth and
count are not bounded by the tool; the role's context limit is the bound.

**Worker tools.** Exactly the six toolkit tools rooted at `cfg.Root`, with
`.git` skipped for `Glob` and `Grep`, as agent-repl registers them. No
`search`, no `remember`, no `delegate`.

**The pass.** `RunPass` takes `store.NextPass()` as the pass number, runs the
root supervisor at that address with `prompt`, and returns its report. A root
that ends with an error returns that error, with the usage accumulated so
far in `Result`. `Result.Usage` and `Result.Cost` are the field-wise sums of
every agent's summary in the pass, root and descendants, including agents
that ended with an error.

**The pass from the CLI** (`internal/cli`, after D2's parse, validate, and
prompt read), in this order so a failure leaves nothing behind:

1. Open both role factories (D3). A failure is exit 1 with no session file
   created.
2. Without `-resume`: create `<Home>/.dory/sessions/` with mode `0700` if
   absent and `store.Create` the file `<SessionID>.db` in it with
   `Deps.Root`. With `-resume`: `store.Open` `<Resume>.db` there; a missing
   file is exit 1; a `Root()` that differs from `Deps.Root` is exit 1 with an
   error naming both directories.
3. `RunPass` with a context that `Deps.Interrupts` cancels. A receive
   cancels the pass; every agent's stream ends with the context error, every
   bash process group dies with it (toolkit's guarantee), and the exit is 1.
4. Render the summary (D6) with the pass's usage and the session id, close
   the store, and exit 0 if the root reported, else 1 with the error rendered.

## REQUIREMENTS

- R-IXX2-8C9Q: Package `internal/agent` MUST export `type Role string` with exactly the constants `RoleSupervisor = "supervisor"` and `RoleWorker = "worker"`, and the string constants `SupervisorPrompt` and `WorkerPrompt`, each non-empty and not consisting only of white space.
- R-IZ4Y-M40F: Package `internal/agent` MUST export a `Trace` interface whose method set is exactly `Event(address string, ev agentkit.Event)`, `Error(address string, err error)`, and `Record(rec agentkit.LogRecord) string`.
- R-J0CU-ZVR4: Package `internal/agent` MUST export a `Config` struct whose fields are exactly `Store *store.Store`, `Supervisor *model.Factory`, `Worker *model.Factory`, `Root string`, `Now func() time.Time`, and `Trace Trace`; a `Result` struct whose fields are exactly `Address string`, `Report string`, `Usage agentkit.Usage`, and `Cost agentkit.Cost`; and `RunPass(ctx context.Context, cfg Config, prompt string) (Result, error)`.
- R-J2SN-RF8I: `RunPass` MUST use `cfg.Store.NextPass()` as the pass number, run the root as a supervisor at the address that number formats to in decimal, and set `Result.Address` to it, verified by a second `RunPass` on the same store using the next number.
- R-J40K-56Z7: Before an agent's first provider request, the harness MUST store a `KindPrompt` entry at the agent's address whose `Text` is the prompt the agent was given, verified for a root (the pass prompt) and for a child (the `delegate` prompt).
- R-J58G-IYPW: Every agent's conversation MUST be built by its role's factory with an `agentkit.Log` whose id is the agent's address and whose writer is a `store.LogWriter` at that address using `cfg.Trace.Record`, verified by every transcript entry at the address carrying that address in its `Raw` record's `id` field.
- R-J6GC-WQGL: Every agent's first provider request MUST carry a system entry whose text is `SupervisorPrompt` for a supervisor and `WorkerPrompt` for a worker, followed by a user message whose text is the prompt the agent was given, verified through an `httptest` provider.
- R-J7O9-AI7A: A supervisor's conversation MUST advertise exactly the tools named `search`, `fetch`, `remember`, and `delegate`, and a worker's conversation MUST advertise exactly the six toolkit tools `Bash`, `Read`, `Write`, `Edit`, `Glob`, and `Grep` rooted at `cfg.Root` with `Glob` and `Grep` skipping `.git`, verified by the tool declarations an `httptest` provider receives and by a worker `Read` of a file under `cfg.Root`.
- R-J8W5-O9XZ: The `search` tool's schema MUST declare exactly the properties `query` (string, required), `kind` (string), `address` (string), and `page` (integer, minimum 1); `fetch` exactly `id` (integer, required); `remember` exactly `text` (string, required, minLength 1); and `delegate` exactly `role` (string, required, enum `supervisor` and `worker`) and `prompt` (string, required, minLength 1).
- R-JA42-21OO: The `search` tool MUST call `Store.Search` with `query` and a `Filter` whose `Kinds` are the comma-separated `kind` values without a `-` prefix, whose `Exclude` are those with it, whose `Address` is `address`, and whose `Page` is `page`, and MUST return the resulting `store.Page` encoded as JSON; a `kind` value naming an unknown kind MUST be a tool error naming it.
- R-JBBY-FTFD: The `fetch` tool MUST return `Store.Fetch(id)` encoded as JSON, and MUST return a tool error whose text contains the id when the store returns `ErrNotFound`.
- R-JCJU-TL62: The `remember` tool MUST store a `KindNote` entry at the calling agent's address whose `Text` is `text`, and return `ok`.
- R-JDRR-7CWR: The `delegate` tool MUST run a child of the given `role` at the address formed from the caller's address, a `.`, and the child's ordinal among the caller's children counted from 1 in call order, MUST block until the child's turn ends, and MUST return the child's report as its content, verified by a parent delegating twice and receiving addresses `1.1` and `1.2`.
- R-JEZN-L4NG: When a child's stream ends with a non-nil `Stream.Err()`, the `delegate` tool MUST return a tool error whose text contains the child's address and the error's text, MUST store no `KindReport` entry for the child, and the parent's turn MUST continue, verified with a child whose provider answers with an error and with a child whose role has `Limits.MaxContextTokens` set low enough to trip.
- R-JG7J-YWE5: The harness MUST call `cfg.Trace.Event` with the agent's address for every event of the agent's stream in order as it arrives, and `cfg.Trace.Error` with the agent's address when the stream's terminal error is non-nil.
- R-JHFG-CO4U: When an agent's stream ends cleanly, the harness MUST set its report to the `Text` blocks of the last assistant `MessageDone` joined by `\n` (or `""` when there was none), MUST store a `KindReport` entry at the address whose `Text` is that report, and MUST close the log, verified by a `summary` transcript entry existing at the address.
- R-JINC-QFVJ: `Result.Usage` and `Result.Cost` MUST equal the field-wise sums of the summary `Usage` and `Cost` of every agent run in the pass, including agents whose stream ended with an error, and `RunPass` MUST return them alongside a non-nil error when the root's stream ends with an error.
- R-JJV9-47M8: `RunPass` MUST return a non-nil error when the root's stream ends with a non-nil `Stream.Err()`, and `Result.Report` otherwise, verified by a root whose provider answers with an error and one that answers with text.
- R-JL35-HZCX: `Run` MUST open both role factories before creating or opening the session file, and a factory failure MUST exit 1 with the error on stderr and no file created under `<Deps.Home>/.dory`.
- R-JNIY-9IUB: Without `-resume`, `Run` MUST create `<Deps.Home>/.dory/sessions/` with mode `0700` when absent and create the store at `<Deps.Home>/.dory/sessions/<Deps.SessionID>.db` with `Deps.Root` as its root; with `-resume`, `Run` MUST open `<Deps.Home>/.dory/sessions/<Options.Resume>.db`, exit 1 with an error naming the path when it does not exist, and exit 1 with an error naming both directories when its `Root()` differs from `Deps.Root`.
- R-JOQU-NAL0: A receive on `Deps.Interrupts` while a pass runs MUST cancel the pass's context so that every in-flight stream ends with an error wrapping `context.Canceled`, and `Run` MUST then exit 1, verified with an `httptest` provider that blocks until the test releases it.
- R-JPYR-12BP: `Run` MUST exit 0 when `RunPass` returns a nil error and 1 when it returns a non-nil error, and in both cases MUST render the summary (D6) with the returned `Result` and the session id and close the store before returning.
