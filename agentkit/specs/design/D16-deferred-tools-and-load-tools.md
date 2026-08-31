# D16-deferred-tools-and-load-tools

Some consumers register many tools but want only a handful advertised to the
model up front — the rest revealed on demand, so the tool list the model reasons
over stays small and its prompt prefix stays cacheable. agentkit serves this with
**deferred tools** and one synthesized meta-tool, `load_tools`. Both are **day
one**, not a sibling: loading rewrites the live tool array *between the
round-trips of a single turn*, inside the orchestration loop (D12), so it cannot
live in a package layered above `Send`.

A **deferred tool** is an ordinary `Tool` (D9) — constructed, schema-validated,
and callable exactly like an eager one — that the consumer registers as *not
surfaced up front*. Registration groups deferred tools under a named group with a
one-line blurb; a group is the unit the model loads and the unit the catalog
lists. The registration surface hangs off the conversation config, beside the
eager `Tools`:

```go
package agentkit

// DeferredGroup is a named, on-demand bundle of tools. Its Blurb is one line the
// load_tools catalog shows to explain what the group is for; its Tools are
// ordinary Tool values (D9), validated at Send like eager tools but withheld from
// the advertised tool array until the model loads the group or a member by name.
type DeferredGroup struct {
	Name  string // stable group id the model passes to load_tools
	Blurb string // one-line description shown in the load_tools catalog
	Tools []Tool
}

// Deferred registers on-demand tool groups on the conversation. Registering any
// group makes the orchestrator synthesize the built-in load_tools meta-tool.
func (c *Conversation) Deferred(groups ...DeferredGroup)
```

When at least one deferred group exists, the orchestrator **synthesizes one
built-in `load_tools` meta-tool** and adds it to the advertised toolset. Its
*description* carries a generated **catalog**: per-group blurb plus the bare tool
**names** in that group — never the tools' own descriptions and never their JSON
schemas. Withholding schemas is the whole point; a catalog that inlined them would
cost as much prompt as advertising the tools eagerly. `load_tools` takes one
argument, a list of names, each either a group name or a single tool name, and the
model may batch several in one call:

```go
// load_tools input schema (synthesized): {"names": ["<group-or-tool>", ...]}.
// Calling it loads every named group and tool; from the NEXT round-trip those
// tools are ordinary members of the live toolset, advertised with full schemas.
```

Loading is **monotonic and conversation-scoped**: a loaded tool stays loaded for
the life of the conversation, and there is no unload. Unloading would strand the
`tool_use` / `tool_result` history references (D2) a later round-trip still
carries, so it is simply not offered. A `load_tools` call that names an
already-loaded or unknown name is not an error; unknown names are reported back in
the tool's own result text so the model can correct itself, and the turn
continues.

A model may also **call a deferred-but-unloaded tool directly**, guessing its
arguments before loading it. The orchestrator does **not** execute that guessed
input. Instead it feeds back a `ToolResult` with `IsError` set, whose content
names `load_tools` and the tool, **and it loads the tool as a side effect** — so
the model recovers in a single iteration: the next round-trip both explains the
misstep and exposes the tool's real schema, and the model can retry with correct
arguments. This mirrors the in-band tool-error contract (D12): a wrong call is
recoverable feedback, never turn-ending.

Tool **ordering is cache-prefix-stable** (D-C), because a reordered tool array
invalidates a wire's prompt cache and silently inflates cost. The orchestrator
emits a fixed order: first a **name-sorted base** — the eager `Tools` plus
`load_tools` — then the **loaded tools appended in load order**. A load only ever
**extends the tail**; it never reorders or reinserts. The Anthropic adapter
**preserves the orchestrator's order verbatim** (no re-sort) so the cached prefix
survives from one round-trip to the next; a wire that re-sorts its tools (Gemini,
whose caching is implicit) makes the point moot and is free to do so. The result
is that turn N+1's advertised tools are turn N's array with zero or more entries
appended — a stable prefix by construction.

All tools pass **one validation gate at `Send`** (D11): the union of the eager
`Tools`, every deferred group's tools, and the synthesized `load_tools` is checked
for name uniqueness and canonical-subset schema conformance (D9) before any
provider call. A deferred tool with a bad schema fails the whole `Send` with
`ErrInvalidConfig` (D4), not lazily at load time — the consumer learns of the
defect before the turn starts, and a load can never fail for a schema reason.

## REQUIREMENTS

- R-5PKM-V6VJ: Deferred tools MUST be registered on the `Conversation` as named groups and MUST be ordinary `Tool` values, validated at `Send` identically to eager `Tools` but withheld from the advertised toolset until loaded.
- R-5QSJ-8YM8: When at least one deferred group is registered, the orchestrator MUST synthesize exactly one built-in `load_tools` meta-tool and advertise it alongside the eager tools.
- R-5S0F-MQCX: The synthesized `load_tools` description MUST list, per group, the group blurb and the bare tool names, and MUST NOT include any tool's own description or JSON schema.
- R-5T8C-0I3M: A `load_tools` call MUST accept a batched list of group-or-tool names, and from the next round-trip every named tool MUST be an ordinary member of the advertised toolset with its full schema; unknown names MUST be reported in the tool result without ending the turn.
- R-5UG8-E9UB: Loading MUST be monotonic and conversation-scoped — a loaded tool MUST remain loaded for the conversation's life, and no unload operation may exist.
- R-5VO4-S1L0: A direct call to a deferred-but-unloaded tool MUST NOT execute the model-supplied arguments; the orchestrator MUST return an `IsError` `ToolResult` naming `load_tools` and MUST load the tool as a side effect so recovery completes in one iteration.
- R-5WW1-5TBP: The advertised tool array MUST be a name-sorted base (eager `Tools` ∪ `load_tools`) followed by loaded tools in load order, a load MUST only append to the tail, and the Anthropic adapter MUST transmit that order without re-sorting so the cache prefix is stable across round-trips.
- R-5Y3X-JL2E: `Send` MUST run one validation gate over the union of eager tools, all deferred groups, and `load_tools` (name uniqueness and canonical-subset schema, D11) before any provider call, failing with `ErrInvalidConfig` rather than deferring a schema failure to load time.
