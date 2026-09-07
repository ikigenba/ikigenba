# D0-scope

Scope preamble for dory. This document carries no requirements and mints no
ids. It fixes the boundary the numbered design documents are authored inside
and records the decisions taken in discussion so the reasons survive.

## What dory is

A command that applies effort to a prompt by dispatching a tree of agents that
remember nothing. One invocation is one **pass**: a root **supervisor** reads
the prompt, orients itself by searching the session **store**, delegates to
children, and ends with a **report**. A supervisor can delegate to another
supervisor or to a **worker**. A worker changes the world with the six toolkit
tools and cannot delegate. Every agent starts with an empty context; the store
and the filesystem are the only things that persist.

Dory is driven by a person at a shell and by other agents alike. It reads its
prompt from stdin, streams a trace to stdout, and ends with the root's report
and a cost line. `--resume` names an earlier session so the next pass's fresh
root can search everything earlier passes wrote. Nothing loops inside dory: a
loop, if wanted, is the caller.

## Decisions taken in discussion

- The root is always a supervisor.
- Supervisors get `search`, `fetch`, `remember`, `delegate` and no toolkit
  tool. Workers get exactly the six toolkit tools and nothing else.
- Delegation is unbounded in count and depth. What is bounded is context, per
  role, through agentkit `Limits`. A round-trip cap is deferred until agentkit
  can count round-trips.
- `delegate` blocks; the child's final text is the tool result; any error that
  terminates the child (context limit included) is the tool's error result.
  Dispatch is sequential because agentkit dispatches sequentially; concurrent
  dispatch of one round-trip's calls is a later agentkit change and needs no
  change to dory's tool shape.
- A report is the agent's final assistant text. No status enum, no structured
  output.
- The store is one SQLite file per session at `~/.dory/sessions/<uuid>.db`,
  through the pure-Go `modernc.org/sqlite` driver with FTS5. Entry kinds are
  `note`, `prompt`, `report`, `transcript`. Transcript rows are agentkit's own
  event-log records. Row ids are SQLite row ids. Search is paginated with a
  fixed page size and filters by kind and address prefix; `fetch` returns one
  whole row by id.
- Addresses are the pass number followed by one ordinal per level: the root
  of pass 3 is `3`, its second child `3.2`, that child's first child `3.2.1`.
- One model per role, configured with `-c` pairs in agent-repl's grammar.
- The tool root is the working directory dory was launched from, recorded in
  the session at creation; a resumed pass from another directory fails.
- The trace prints everything, in agent-repl's decorated form, each line
  prefixed with the agent's address, through one writer.
- The cost line is the pass total only.
- Ctrl-C cancels the pass and exits; nothing is written on the way out beyond
  what the store already holds.
- The role prompts are fixed and built in; their text is tuned after the first
  runs and is deliberately not a requirement.
- Exit 0 when the root reports; non-zero only when dory itself fails.

## Deferred

Round-trip limits, concurrent dispatch, workers with `search`/`remember`, a
persistent root context, embeddings, a shell command for searching the store
(the `sqlite3` CLI is enough for now).
