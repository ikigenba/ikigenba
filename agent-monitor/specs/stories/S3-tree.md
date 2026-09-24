# Stories — drawing the subagent tree of a session

`agent-monitor tree <harness> <session-id>` prints a snapshot of one root
session and every subagent it started, at every depth, and exits; it does not
watch or redraw. The harness is `claude`, `codex`, or `grok`, spelled exactly
so, as for `list`, and the session is named by its full session id. Any root
session of that harness found in its default location under `$HOME` is
accepted, live or ended; a subagent is never a session, and `$HOME` is never
guessed. The output is a tree drawn as the Unix `tree` command draws one: the
root session's line comes first, and each subagent's line sits under the
session or subagent that started it, prefixed by `├── `, or by `└── ` when it
is the last of its siblings. Each level adds four columns: below a line drawn
with `├── `, the lines of its own subagents are continued by `│   `, and below
one drawn with `└── `, by four spaces. Siblings are in the order they were
started, oldest first, and siblings started at the same moment are ordered by
id, ascending byte by byte. Every line is `<label>  <status>`: the label, two
spaces, and the status word, with no column alignment and no other field. The
root's label is the session's title, or the session id when the session has no
title or its title cannot be read. The title is read from the same place
`list` reads it, except that a Claude Code root that is not live, or whose
registration has no `name` or an empty one, takes it from its transcript: the
`customTitle` of its latest `custom-title` record, else the `aiTitle` of its
latest `ai-title` record, where an empty one counts as none. A subagent's label
is the `description` of its Claude Code or Grok meta file, or the last segment
of its Codex `agent_path`; a subagent with no label, or an empty one, is
labelled by its own id: its Claude agent id, Codex thread id, or Grok subagent
id. A live root's status is its `list` STATUS, `working`, `idle`, or `unknown`,
by the same liveness and status rules as `list`; a root that is not live is
`ended`, even when its own details cannot be read. A subagent's status is
`working` while it runs, `done` when it has finished, `failed` when it ended in
failure, `killed` when it was stopped before it finished, and `unknown` when
none of these can be told from what the harness recorded. In a session that has
ended, a subagent that never had a result recorded is `unknown`, never
`working`, and so is a Claude Code subagent sent a message after its latest
notification. When some of the root's or a subagent's details cannot be read,
only the lines those details belong to fall back, and a subagent whose parent
cannot be told is drawn directly under the root. Labels are printed escaped as
`list` prints CWD and TITLE (a tab is `\t`, a newline `\n`, a backslash `\\`, a
right-to-left override `\u202e`), without quotes and with `'` printed as typed,
so every subagent is one line. `tree` only reads: it takes no lock and changes
nothing.

## A developer draws the subagent tree of a Claude Code session

Claude Code keeps every subagent of a session, at any depth, beside the
session's transcript in
`$HOME/.claude/projects/<encoded-cwd>/<sessionId>/subagents/`: a transcript
`agent-<agentId>.jsonl` and a meta file `agent-<agentId>.meta.json` whose
`description` is the label. A meta file with no `parentAgentId` is a subagent
the session started; one with a `parentAgentId` was started by that subagent.
When a subagent ends, Claude Code records a notification in the transcript of
whoever started it, the session's or the parent subagent's, naming the agent id
and a status of `completed`, `failed`, or `killed`; that is `done`, `failed`,
or `killed` (the latest one when there are several), unless a `SendMessage`
tool call addressed to its agent id is recorded in that transcript after its
latest notification; while the session is live, such a subagent is `working`
again, and once the session has ended it is `unknown`. A subagent its parent
waited on is `done` once its result is back in the parent's transcript. A
subagent with neither yet is `working`. The subagent's own transcript does not
decide its status. The same notification is recorded for background shell
commands; they have no meta file and are not drawn.

`/home/dev/.claude/sessions/41822.json`:

```
{"pid":41822,"sessionId":"7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93","cwd":"/home/dev/src/shop","name":"fix Bob's checkout","status":"busy"}
```

The meta files in
`/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/`,
with their other keys left out:

`agent-a1c4e7f09b2d38561.meta.json`:

```
{"agentType":"general-purpose","description":"Find the checkout handler"}
```

`agent-a2d5f8e1c3b049672.meta.json`:

```
{"agentType":"general-purpose","description":"Review the payment tests"}
```

`agent-a3e6f9d2b4c150783.meta.json`:

```
{"agentType":"general-purpose","description":"Run the payment suite","parentAgentId":"a2d5f8e1c3b049672"}
```

`agent-a4f7e0c3d5a261894.meta.json`:

```
{"agentType":"general-purpose","description":"Profile the cart query"}
```

`agent-a5b8c1f4e6d372905.meta.json`:

```
{"agentType":"general-purpose","description":"Draft the refund fix"}
```

Command:

```
$ agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

Output:

```
fix Bob's checkout  working
├── Find the checkout handler  done
├── Review the payment tests  working
│   └── Run the payment suite  failed
├── Profile the cart query  killed
└── Draft the refund fix  working
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Process 41822 is running and started no later than session `7c2e9a41-…`.
- The session's transcript is
  `/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93.jsonl`.
- The five meta files above, each beside its transcript, are the only ones in
  the session's `subagents/` directory.
- The subagents were started in the order `a1c4e7f0…`, `a2d5f8e1…`,
  `a3e6f9d2…`, `a4f7e0c3…`, `a5b8c1f4…`.
- The session's transcript records a notification that `a1c4e7f0…` ended
  `completed` and one that `a4f7e0c3…` ended `killed`, no `SendMessage` tool
  call addressed to either after its notification, and neither a
  notification for `a2d5f8e1…` nor a result of it.
- The session's transcript records a notification that `a5b8c1f4…` ended
  `completed`, and after it a `SendMessage` tool call addressed to
  `a5b8c1f4e6d372905`, and no later notification for `a5b8c1f4…`.
- The transcript of `a2d5f8e1…`,
  `subagents/agent-a2d5f8e1c3b049672.jsonl`, records a notification that
  `a3e6f9d2…` ended `failed` and no later `SendMessage` tool call addressed
  to it.

Postconditions:

- Nothing has changed.

## A developer draws the subagent tree of a Codex session

A Codex subagent is a thread of its own, with its own rollout. The rollout of
the thread that started it records the start as a `SubAgentActivity` item of
kind `started`, naming the child's `agent_thread_id` and its `agent_path`,
such as `/root/forecast` for a subagent of the session and
`/root/forecast/radar` for a subagent of that subagent; the label is the last
segment of the path. A subagent's rollout also records its dealings with its
parent, but only `started` items name children. A subagent's status comes from
its own rollout, as a root's does in `list`: it is `working` when the last of
its `task_started`, `task_complete`, and `turn_aborted` records is
`task_started`, `done` when it is `task_complete`, and `killed` when it is
`turn_aborted`.

Command:

```
$ agent-monitor tree codex 01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4
```

Output:

```
weather check  working
├── forecast  done
│   └── radar  killed
└── alerts  working
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0d0ab-…` is loaded, a root, and `working`; its `thread_name`
  in `/home/dev/.codex/session_index.jsonl` is `weather check`.
- The rollout of `01a0d0ab-…` records two `started` items: first thread
  `01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07` at `/root/forecast`, then thread
  `01a0d8f1-0b6e-7d24-9c3a-5e7f1a2b4c68` at `/root/alerts`.
- The rollout of `01a0d7c3-…` records one `started` item, thread
  `01a0d9a2-4c7d-7e15-8f60-2b9d3e1c5a74` at `/root/forecast/radar`, and its
  last such record is `task_complete`.
- The last such record in the rollout of `01a0d9a2-…` is `turn_aborted`, and
  in the rollout of `01a0d8f1-…` it is `task_started`.
- Each rollout is under `/home/dev/.codex/sessions/YYYY/MM/DD/` and records
  no other `started` item.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer draws the subagent tree of a Grok session

Grok keeps every subagent of a session, at any depth, in the root session's
directory, `$HOME/.grok/sessions/<url-encoded-cwd>/<session-id>/subagents/<subagent-id>/`,
whose `meta.json` holds its `description`, the label, and its `status`:
`running` is `working` and `completed` is `done`. A meta file's
`parent_session_id` names the root even for a subagent started by another
subagent; which session started a subagent is taken from the spawn records
Grok writes to the sessions' `updates.jsonl`.

`meta.json` of the three subagents of session `01a0c4f2-…`, with their other
keys left out:

```
{"subagent_id":"01a0c51e-2a64-7f09-b3d8-6e1c9a4f0b25","description":"Trace the redirect loop","status":"completed"}
```

```
{"subagent_id":"01a0c533-8d0f-72c6-a4e7-1b5d8f2c6a90","description":"Check the cookie domain","status":"completed"}
```

```
{"subagent_id":"01a0c548-c3a1-7e5b-8f92-4d0b7e3a1c68","description":"Update the login tests","status":"running"}
```

Command:

```
$ agent-monitor tree grok 01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47
```

Output:

```
fix login redirect  working
├── Trace the redirect loop  done
│   └── Check the cookie domain  done
└── Update the login tests  working
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Session `01a0c4f2-…` is live in `/home/dev/src/site`, `working`, with
  `generated_title` `fix login redirect`.
- Its `subagents/` directory holds exactly the three subagents above.
- The session started `01a0c51e-…` and then `01a0c548-…`; `01a0c51e-…`
  started `01a0c533-…`; the sessions' `updates.jsonl` files record so.

Postconditions:

- Nothing has changed.

## A developer draws the tree of a session that started no subagents

A session with no subagents is a tree of one line, the root's. Here the
session has no title, neither in its registration nor in its transcript, so
its label is its id.

Command:

```
$ agent-monitor tree claude b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14
```

Output:

```
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- `/home/dev/.claude/sessions/39107.json` registers session `b41f0c77-…`
  with `status` `idle` and no `name`; process 39107 is running and started
  no later than the session.
- The session's transcript is
  `/home/dev/.claude/projects/-home-dev-src-blog/b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14.jsonl`,
  it records no `custom-title` or `ai-title` record, and there is no
  `subagents/` directory beside it.

Postconditions:

- Nothing has changed.

## A developer draws a tree whose subagent details are not yet readable

A tree is drawn even when some of what the harness recorded about a subagent
cannot be read yet: a file is not written, holds no complete record, or cannot
be read. A half-written last line is ignored and the last complete record is
used. Only that subagent's line falls back: its status is `unknown` when the
place its status comes from cannot be read, and its label is its id when the
place its label comes from cannot be read. A subagent is drawn only when the
place that records its existence says so: a Claude Code meta file, or a
transcript `agent-<agentId>.jsonl` whose meta file cannot be read, in the
session's `subagents/` directory; a `started` item in the Codex parent's
rollout; or an entry in the Grok session's `subagents/` directory. When that
place cannot be read, the subagents it would name are not drawn, and the line
of the session or subagent it belongs to is still drawn. Here the rollout of
`forecast` cannot be read, so its status is `unknown` and its own subagent is
not drawn; the second subagent's `started` item has no `agent_path`, so its
label is its thread id, and its rollout does not exist yet, so its status is
`unknown`.

Command:

```
$ agent-monitor tree codex 01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4
```

Output:

```
weather check  working
├── forecast  unknown
└── 01a0d8f1-0b6e-7d24-9c3a-5e7f1a2b4c68  unknown
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0d0ab-…` is loaded, a root, `working`, and named
  `weather check`.
- The rollout of `01a0d0ab-…` records two `started` items: first thread
  `01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07` at `/root/forecast`, then thread
  `01a0d8f1-0b6e-7d24-9c3a-5e7f1a2b4c68` with no `agent_path`.
- The rollout of `01a0d7c3-…` exists and is not readable by the developer;
  it records the start of a subagent of its own.
- The rollout of `01a0d8f1-…` does not exist yet.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer draws a Claude Code tree whose transcript and a meta file cannot be read

The root's line falls back as a `list` row does: a live root's status is
`unknown` only when the place its status comes from cannot be read, while a
root that is not live is `ended` all the same, its label is its session
id when the place its title comes from cannot be read, and the subagents are
still drawn from whatever can be read. A subagent whose parent cannot be told
is drawn directly under the root: a Claude Code subagent whose
`agent-<agentId>.jsonl` exists but whose meta file cannot be read, or a Grok
subagent whose entry exists in `subagents/` but whose spawn records cannot be
read. Its status follows the usual rules when they can be applied and is
`unknown` when they cannot, and its label is its id when its label cannot be
read. Here the session's registration has no `name` and its transcript cannot
be read, so its label is its id, while its status is read from its
registration as usual. Its first subagent's status would come from that
transcript, so it is `unknown`. The meta file of the last subagent cannot be
read, so it is labelled by its agent id and drawn under the root, although the
first subagent started it; its status would come from the transcript of
whoever started it, which cannot be told, so it is `unknown`.

`/home/dev/.claude/sessions/40533.json`:

```
{"pid":40533,"sessionId":"e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54","cwd":"/home/dev/src/docs","status":"idle"}
```

The readable meta files in
`/home/dev/.claude/projects/-home-dev-src-docs/e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54/subagents/`,
with their other keys left out:

`agent-a8e1f4a7b9d2c5306.meta.json`:

```
{"agentType":"general-purpose","description":"Map the docs routes"}
```

`agent-a9f2a5b8c0e3d6417.meta.json`:

```
{"agentType":"general-purpose","description":"List the page templates","parentAgentId":"a8e1f4a7b9d2c5306"}
```

Command:

```
$ agent-monitor tree claude e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54
```

Output:

```
e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54  idle
├── Map the docs routes  unknown
│   └── List the page templates  done
└── a0a3b6c9d1f4e7528  unknown
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Process 40533 is running and started no later than session `e82a5c90-…`.
- The session's transcript,
  `/home/dev/.claude/projects/-home-dev-src-docs/e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54.jsonl`,
  exists and is not readable by the developer.
- The session's `subagents/` directory holds the two meta files above, each
  beside its transcript, and `agent-a0a3b6c9d1f4e7528.jsonl` beside
  `agent-a0a3b6c9d1f4e7528.meta.json`, which exists and is not readable by
  the developer; it holds no other file.
- The subagents were started in the order `a8e1f4a7…`, `a9f2a5b8…`,
  `a0a3b6c9…`; `a8e1f4a7…` started both of the others.
- The transcript of `a8e1f4a7…`, `subagents/agent-a8e1f4a7b9d2c5306.jsonl`,
  records a notification that `a9f2a5b8…` ended `completed` and no later
  `SendMessage` tool call addressed to it.

Postconditions:

- Nothing has changed.

## A developer draws the tree of a session that has ended

A session that is no longer live is still found on disk and drawn; its root
is `ended`, even when its own details cannot be read. A subagent whose result
was recorded, and that was not sent a message after it, keeps it; a subagent
that never had a result recorded, or a Claude Code subagent sent a message
after its latest notification, is `unknown`: it is not running, and whether it
finished cannot be told.
Here thread `01a09e55-…` is no longer loaded; its subagent `seed` last
recorded `task_started`.

Command:

```
$ agent-monitor tree codex 01a09e55-8b3d-7c61-9f04-2e7a5c1b8d96
```

Output:

```
migrate the schema  ended
├── schema  done
└── seed  unknown
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a09e55-…` has a rollout under `/home/dev/.codex/sessions/`, is a
  root, and is not loaded: no process holds the lock on
  `/home/dev/.codex/thread-writer-locks/01a09e55-8b3d-7c61-9f04-2e7a5c1b8d96.lock`.
- Its `thread_name` in `/home/dev/.codex/session_index.jsonl` is
  `migrate the schema`.
- Its rollout records two `started` items: first thread
  `01a09f10-6d2a-7b35-8e41-9c0f3a7d2b56` at `/root/schema`, then thread
  `01a09f84-2b7e-7c09-a5d3-1e6b8f4c0a37` at `/root/seed`.
- The last such record in the rollout of `01a09f10-…` is `task_complete`,
  and in the rollout of `01a09f84-…` it is `task_started`.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer draws the tree of a Claude Code session that has ended

A Claude Code session that has ended has no live registration to take a `name`
from, so its root takes its title from its transcript: the `customTitle` of the
latest `custom-title` record, the name the developer gave the session, or, when
there is none, the `aiTitle` of the latest `ai-title` record, the title Claude
Code generated. An empty `customTitle` or `aiTitle` counts as none. With
neither, or when the transcript cannot be read, the label is the session id. A
live session whose registration has no `name`, or an empty one, takes its title
from its transcript the same way. Here the transcript records a name and,
later, a generated title, and the name wins; the second subagent has no
recorded result, so it is `unknown`.

The title records in the session's transcript, in the order recorded, with
their other keys left out:

```
{"type":"custom-title","customTitle":"invoice export","sessionId":"5d3b8e17-2c49-4a06-b8f1-7e0a9c4d2f65"}
{"type":"ai-title","aiTitle":"Fix the flaky invoice export","sessionId":"5d3b8e17-2c49-4a06-b8f1-7e0a9c4d2f65"}
```

The meta files in
`/home/dev/.claude/projects/-home-dev-src-billing/5d3b8e17-2c49-4a06-b8f1-7e0a9c4d2f65/subagents/`,
with their other keys left out:

`agent-a6c9d2e5f7b083146.meta.json`:

```
{"agentType":"general-purpose","description":"Reproduce the export failure"}
```

`agent-a7d0e3f6a8c194257.meta.json`:

```
{"agentType":"general-purpose","description":"Patch the CSV writer"}
```

Command:

```
$ agent-monitor tree claude 5d3b8e17-2c49-4a06-b8f1-7e0a9c4d2f65
```

Output:

```
invoice export  ended
├── Reproduce the export failure  done
└── Patch the CSV writer  unknown
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- No file in `/home/dev/.claude/sessions/` registers session
  `5d3b8e17-…`.
- The session's transcript is
  `/home/dev/.claude/projects/-home-dev-src-billing/5d3b8e17-2c49-4a06-b8f1-7e0a9c4d2f65.jsonl`;
  the two records above are its only `custom-title` and `ai-title` records.
- The two meta files above, each beside its transcript, are the only ones in
  the session's `subagents/` directory.
- The subagents were started in the order `a6c9d2e5…`, `a7d0e3f6…`.
- The session's transcript records a notification that `a6c9d2e5…` ended
  `completed` and no `SendMessage` tool call addressed to it, and holds
  neither a notification for `a7d0e3f6…` nor a result of it.

Postconditions:

- Nothing has changed.

## A developer asks what tree can do

`--help` or `-h` anywhere after `tree` prints the help of `tree` and wins
over everything else after `tree`, a missing, unknown, or extra argument or
an unknown option included. It needs no session data, so it works with
`HOME` unset. The exit codes are those of the top-level help and are not
repeated here.

Command:

```
$ agent-monitor tree --help
```

```
$ agent-monitor tree -h
```

```
$ agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 --help
```

```
$ agent-monitor tree bogus extra more --bogus -h
```

Options:

- `--help`, `-h`: print the help of `tree` and exit.

Output:

```
Usage: agent-monitor tree <harness> <session-id>

Draw the subagent tree of one session.

Harnesses:
  claude  Claude Code
  codex   OpenAI Codex CLI
  grok    Grok Build CLI

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer gives tree no harness

Command:

```
$ agent-monitor tree
```

Output:

```
agent-monitor: missing harness

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer gives tree no session id

Command:

```
$ agent-monitor tree claude
```

Output:

```
agent-monitor: missing session id

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer names an unknown harness to tree

The harness must be one of `claude`, `codex`, or `grok` exactly, so `Claude`
and the empty argument are unknown too. The harness is echoed exactly as an
unknown command is. The arguments after `tree` are read left to right and the
first error wins, so `agent-monitor tree claud` fails here, not as a missing
session id, and `agent-monitor tree claud --bogus` fails here, not as an
unknown option.

Command:

```
$ agent-monitor tree claud 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

```
$ agent-monitor tree claud
```

Output:

```
agent-monitor: unknown harness 'claud'

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer gives tree an extra argument

`tree` takes exactly one harness and one session id. The extra argument is
echoed exactly as an unknown command is. The arguments are all checked before
any session is looked for, so an extra argument fails here even when the
session does not exist.

Command:

```
$ agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 extra
```

```
$ agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 extra more
```

Output:

```
agent-monitor: unexpected argument 'extra'

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer gives tree an unknown option

The only option `tree` takes is `--help` or `-h`; any other option after
`tree` is unknown, the top-level `--version` and `-V` included. The option is
named and echoed exactly as a top-level unknown option is. Read left to
right, an unknown option before the harness fails here, and so does one after
a valid harness or session id: `agent-monitor tree claude --bogus` and
`agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 --bogus` both
fail with `agent-monitor: unknown option '--bogus'`.

Command:

```
$ agent-monitor tree --bogus
```

```
$ agent-monitor tree --bogus claud
```

Output:

```
agent-monitor: unknown option '--bogus'

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer names a session that does not exist

When no root session of the harness has the id, `tree` prints nothing and
fails. The id must be given in full: a prefix of a session's id names no
session. The id is echoed exactly as an unknown command is, so
`agent-monitor tree claude bogus` fails with
`agent-monitor: no claude session 'bogus'`. A harness never used on this
machine, whose directory under `$HOME` does not exist, has no sessions, so
every id fails this way.

Command:

```
$ agent-monitor tree claude 0b9e4d12-7a3c-4f58-9e21-6c8d0a5b3f47
```

Output:

```
agent-monitor: no claude session '0b9e4d12-7a3c-4f58-9e21-6c8d0a5b3f47'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- No Claude Code session with id `0b9e4d12-…` is registered or has a
  transcript under `/home/dev/.claude/projects/`, or that directory does
  not exist.

Postconditions:

- Nothing has changed.
- No directory was created.

## A developer names a subagent instead of a session

A subagent is not a session, so its id fails exactly as an id that names
nothing does, even though the harness keeps a record of it. Here the id is a
Codex subagent's thread: its rollout exists, and its `session_meta` names a
`parent_thread_id`.

Command:

```
$ agent-monitor tree codex 01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07
```

Output:

```
agent-monitor: no codex session '01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0d7c3-…` has a rollout under `/home/dev/.codex/sessions/`
  whose `session_meta` has `parent_thread_id`
  `01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4`.

Postconditions:

- Nothing has changed.

## A developer's harness data cannot be read by tree

When the place `tree` looks for the session exists but cannot be read, `tree`
prints nothing and fails, naming the path it tried. That place is the
directory `$HOME/.claude/projects` for `claude`, `$HOME/.codex/sessions` for
`codex`, and `$HOME/.grok/sessions` for `grok`. The directory is named
without a trailing `/`, escaped as a label is, and `<reason>` is the system's
description of the failure and varies, as for `list`: an unreadable Codex
sessions directory fails with `agent-monitor: cannot read
/home/dev/.codex/sessions: permission denied`. A place that does not exist is
not this failure; it means the session does not exist. Data about the root
session or a subagent that is missing, half-written, or cannot be read is not
this failure either: the tree is still drawn, and only the lines that data
belongs to fall back, as in a tree whose details are not yet readable.

Command:

```
$ agent-monitor tree codex 01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4
```

Output:

```
agent-monitor: cannot read /home/dev/.codex/sessions: <reason>
```

Exits 3. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- `/home/dev/.codex/sessions` exists and is not readable by the developer.

Postconditions:

- Nothing has changed.

## A developer runs tree with HOME not set

Every location `tree` reads is under `$HOME`. When `HOME` is unset or empty
`tree` fails; it never looks the home directory up elsewhere. The arguments
are checked first, so a usage error is reported as such without `HOME`:
`env -u HOME agent-monitor tree claude` fails with
`agent-monitor: missing session id` and exits 2, and
`env -u HOME agent-monitor tree --help` prints the help of `tree`.

Command:

```
$ env -u HOME agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

```
$ HOME= agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

Output:

```
agent-monitor: cannot find the home directory: HOME is not set
```

Exits 3. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.
- No session data was read.
