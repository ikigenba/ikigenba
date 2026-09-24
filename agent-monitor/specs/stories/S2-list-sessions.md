# Stories — listing live root sessions

`agent-monitor list <harness>` prints a snapshot of the live root sessions of
one harness on this machine and exits; it does not watch or redraw. The
harness is `claude` (Claude Code), `codex` (OpenAI Codex CLI), or `grok` (Grok
Build CLI), spelled exactly so. A session is live when the harness registers
it as running in its default location under `$HOME`, whichever client started
it, and the process that runs it is running; a registration whose process is
gone is dropped silently. A root is a session no other session started, so a
subagent is never a row. `CODEX_HOME` and similar variables are ignored: only
the default locations under `$HOME` are read, and `$HOME` is never guessed.
The output is a header row, `SESSION  STATUS  LAST ACTIVE  CWD  TITLE`, then
one row per live root session. SESSION is the harness's full session id.
STATUS is `working`, `idle`, or `unknown`. LAST ACTIVE is the time of the
latest timestamped record in the session's own log, in UTC,
`YYYY-MM-DDTHH:MM:SSZ`: the recorded time is converted to UTC and its
fractional seconds are dropped, never rounded, so
`2026-09-23T18:52:10.912-05:00` is `2026-09-23T23:52:10Z`; ordering compares
the full recorded time. Records without a timestamp are ignored, and the time
a file was last modified is never used. A session whose log has no timestamped
record yet has LAST ACTIVE `-`. A half-written last line in a log is ignored:
fields come from the last complete record. CWD is the directory the
session works in, and TITLE is the session's title, empty when it has none.
Columns are separated by two spaces and each is as wide as its widest entry,
header included, counted in characters of the text as printed; TITLE is last
and no line ends in spaces, so a row with no title ends after its CWD. Rows
are ordered newest LAST ACTIVE first, rows with `-` last, and rows that tie
ordered by session id, ascending byte by byte. CWD and TITLE are printed
escaped as agent-monitor echoes an argument in a diagnostic (a tab is `\t`, a
newline `\n`, a backslash `\\`, a right-to-left override `\u202e`), without
the quotes and with one difference: `'` is printed as typed. So every row is
one line. `list` only reads: it takes no lock and changes nothing.

## A developer lists the live Claude Code sessions

Claude Code registers each running session in a file of its own,
`$HOME/.claude/sessions/<pid>.json`, holding its `sessionId`, `cwd`, `name`,
`status`, and `pid`. It registers only root sessions, never subagents, so
every registered session whose process is running is a row, background jobs of
the Claude daemon included. A registration is dropped when no process with its
`pid` is running, or when the running process started after the session did, a
reused pid. A `status` of `busy` is `working`, `idle` is `idle`, and any other
value is `unknown`; the title is the `name`, empty when there is none, and an
apostrophe in it is printed as typed. The `*.key` files beside the
registrations hold secrets and are never read. A session's transcript is
`$HOME/.claude/projects/<encoded-cwd>/<sessionId>.jsonl`; LAST ACTIVE is the
latest `timestamp` in it, and records without one, such as title or mode
metadata, are ignored.

`/home/dev/.claude/sessions/41822.json`:

```
{"pid":41822,"sessionId":"7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93","cwd":"/home/dev/src/shop","name":"fix Bob's checkout","status":"busy"}
```

`/home/dev/.claude/sessions/39107.json`:

```
{"pid":39107,"sessionId":"b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14","cwd":"/home/dev/src/blog","status":"idle"}
```

`/home/dev/.claude/sessions/40550.json`:

```
{"pid":40550,"sessionId":"e5d93a07-2b6c-4f10-b8e4-7a1c0d9f3e62","cwd":"/home/dev/src/shop","status":"idle"}
```

Command:

```
$ agent-monitor list claude
```

Output:

```
SESSION                               STATUS   LAST ACTIVE           CWD                 TITLE
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93  working  2026-09-23T23:52:10Z  /home/dev/src/shop  fix Bob's checkout
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle     2026-09-23T21:04:55Z  /home/dev/src/blog
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Processes 41822 and 39107 are running and started no later than their
  sessions; no process 40550 is running.
- The three registrations above are the only files in
  `/home/dev/.claude/sessions/` besides their `*.key` files.
- The latest `timestamp` in session `7c2e9a41-…`'s transcript,
  `/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93.jsonl`,
  is `2026-09-23T23:52:10.912Z`, shown truncated to the second; in session
  `b41f0c77-…`'s it is `2026-09-23T21:04:55.087Z`.

Postconditions:

- Nothing has changed.
- No `*.key` file was read.

## A developer lists the live Codex sessions

A Codex thread that is loaded holds an exclusive lock on
`$HOME/.codex/thread-writer-locks/<thread-id>.lock`, and `/proc/locks` shows
the lock with the process that holds it; that held lock is what makes a thread
live. A lock file whose lock no process holds, as a crashed Codex leaves
behind, is not a live thread. The thread's rollout is
`$HOME/.codex/sessions/YYYY/MM/DD/rollout-*-<thread-id>.jsonl`; its first
record, `session_meta`, carries the thread's `cwd` and `parent_thread_id`. A
thread whose `parent_thread_id` is set is a subagent and is not a row. The
thread is `working` when the last of its `task_started`, `task_complete`, and
`turn_aborted` records is `task_started`, and `idle` when it is
`task_complete` or `turn_aborted`; a thread with no turn started yet is
`idle`. LAST ACTIVE is the latest `timestamp` in the rollout, so a thread with
no turn yet takes it from `session_meta`. The title is the thread's
`thread_name` in `$HOME/.codex/session_index.jsonl`, where only named threads
appear; an unnamed thread has an empty TITLE. Threads of any client count, the
ChatGPT desktop app's Codex threads included.

`/home/dev/.codex/session_index.jsonl`:

```
{"id":"01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4","thread_name":"weather check"}
```

Command:

```
$ agent-monitor list codex
```

Output:

```
SESSION                               STATUS   LAST ACTIVE           CWD                  TITLE
01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4  working  2026-09-23T23:40:02Z  /home/dev/src/api    weather check
01a0c1f2-7710-7a3e-8e02-5b1d2c9a0f13  idle     2026-09-23T19:15:30Z  /home/dev/src/infra
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0d0ab-…` is loaded with `cwd` `/home/dev/src/api`, no
  `parent_thread_id`, and its last such record `task_started`; the latest
  `timestamp` in its rollout is 2026-09-23T23:40:02Z.
- Thread `01a0c1f2-…` is loaded with `cwd` `/home/dev/src/infra`, no
  `parent_thread_id`, its last such record `task_complete`, and no entry in
  `session_index.jsonl`; the latest `timestamp` in its rollout is
  2026-09-23T19:15:30Z.
- Thread `01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07` is loaded, with
  `parent_thread_id` `01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4`.
- `thread-writer-locks/` also holds
  `01a09e55-8b3d-7c61-9f04-2e7a5c1b8d96.lock`, whose lock no process holds.
- No other thread is loaded.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer lists the live Grok sessions

Grok registers its running sessions in `$HOME/.grok/active_sessions.json`, a
list of entries with `session_id`, `pid`, `cwd`, and `opened_at`. It registers
only root sessions. An entry can outlive its session: an entry with no running
process for its `pid`, or whose running process started after the session was
opened (a reused pid), is stale and is not listed. A session's files are in
`$HOME/.grok/sessions/<url-encoded-cwd>/<session-id>/`: the title is
`generated_title` in `summary.json`, and the session is `idle` when the last
record of `events.jsonl` is `turn_ended` or no turn has started yet, and
`working` otherwise, a session waiting on the developer included. LAST ACTIVE
is the latest `ts` in `events.jsonl`.

`/home/dev/.grok/active_sessions.json`, with `opened_at` left out:

```
[{"session_id":"3f6d8b20-51ac-4e97-b2d4-0e9c7a1f6b58","pid":5120,"cwd":"/home/dev/src/site"},
 {"session_id":"9a1c47e5-0d83-4b2f-8e6a-c5b7d2f01e39","pid":5388,"cwd":"/home/dev/notes"},
 {"session_id":"c07e2b19-6f4a-4d85-a3e1-8b9d0f5c7a26","pid":4471,"cwd":"/home/dev/src/site"}]
```

Command:

```
$ agent-monitor list grok
```

Output:

```
SESSION                               STATUS   LAST ACTIVE           CWD                 TITLE
3f6d8b20-51ac-4e97-b2d4-0e9c7a1f6b58  working  2026-09-23T23:58:03Z  /home/dev/src/site  fix login redirect
9a1c47e5-0d83-4b2f-8e6a-c5b7d2f01e39  idle     2026-09-23T20:12:44Z  /home/dev/notes
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Processes 5120 and 5388 are running and started no later than their
  sessions were opened; no process 4471 is running.
- Session `3f6d8b20-…` has `generated_title` `fix login redirect`, the last
  record of its `events.jsonl` is a turn in progress, and the latest `ts` in
  it is 2026-09-23T23:58:03Z.
- Session `9a1c47e5-…` has no `generated_title`, the last record of its
  `events.jsonl` is `turn_ended`, and the latest `ts` in it is
  2026-09-23T20:12:44Z.

Postconditions:

- Nothing has changed.

## A developer lists a harness with no live sessions

When the harness has no live root session, only the header is printed, its
columns as wide as their names. That includes a harness never used on this
machine, whose directory under `$HOME` does not exist.

Command:

```
$ agent-monitor list claude
```

```
$ agent-monitor list codex
```

```
$ agent-monitor list grok
```

Output:

```
SESSION  STATUS  LAST ACTIVE  CWD  TITLE
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is set.
- The harness has no live root session: nothing is registered, every
  registration is stale, or its directory under `$HOME` does not exist.

Postconditions:

- Nothing has changed.
- No directory was created.

## A developer lists a session whose details are not yet readable

A live root session is listed even when some of its details cannot be read
yet: a file they come from is not written, holds no complete timestamped
record, or cannot be read. A half-written last line alone is no such cause; it
is ignored and the last complete record is used. Each field falls back on its
own, and only when the place it comes from cannot be read: STATUS is
`unknown`, LAST ACTIVE is `-`, CWD is the working directory of the process
that runs the session, and TITLE is empty. A field whose place can be read is
shown as usual. So a Claude Code session whose transcript cannot be read keeps
the STATUS, CWD, and TITLE of its registration and shows LAST ACTIVE `-`. Here
a Codex thread has just been loaded and its rollout does not exist yet, so its
STATUS, LAST ACTIVE, and CWD fall back, while its TITLE is read from
`session_index.jsonl` as usual and is empty because the thread is unnamed.

Command:

```
$ agent-monitor list codex
```

Output:

```
SESSION                               STATUS   LAST ACTIVE           CWD                  TITLE
01a0c1f2-7710-7a3e-8e02-5b1d2c9a0f13  idle     2026-09-23T19:15:30Z  /home/dev/src/infra
01a0d4e9-2c51-7b08-a1f6-93d0e8c7b245  unknown  -                     /home/dev/src/tools
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0c1f2-…` is loaded, a root, `idle`, unnamed, in
  `/home/dev/src/infra`, and the latest `timestamp` in its rollout is
  2026-09-23T19:15:30Z.
- Thread `01a0d4e9-…` is loaded and has no entry in `session_index.jsonl`;
  its rollout does not exist yet; the process holding its lock works in
  `/home/dev/src/tools`.
- No other thread is loaded.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer asks what list can do

`--help` or `-h` anywhere after `list` prints the help of `list` and wins
over everything else after `list`, a missing, unknown, or extra argument or
an unknown option included. It needs no session data, so it works with
`HOME` unset. The exit codes are those of the top-level help and are not
repeated here.

Command:

```
$ agent-monitor list --help
```

```
$ agent-monitor list -h
```

```
$ agent-monitor list claude --help
```

```
$ agent-monitor list bogus extra --bogus -h
```

Options:

- `--help`, `-h`: print the help of `list` and exit.

Output:

```
Usage: agent-monitor list <harness>

List the live root sessions of one harness, newest activity first.

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

## A developer names no harness

Command:

```
$ agent-monitor list
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

## A developer names an unknown harness

The harness must be one of `claude`, `codex`, or `grok` exactly, so `Claude`
and the empty argument are unknown too: `agent-monitor list ''` fails with
`agent-monitor: unknown harness ''`. The harness is echoed exactly as an
unknown command is. The arguments after `list` are read left to right and
the first error wins, so `agent-monitor list claud extra` fails here, and
`agent-monitor list claud --bogus` fails here, not as an unknown option.

Command:

```
$ agent-monitor list claud
```

```
$ agent-monitor list claud extra
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

## A developer gives list an extra argument

`list` takes exactly one harness. The extra argument is echoed exactly as an
unknown command is.

Command:

```
$ agent-monitor list claude extra
```

```
$ agent-monitor list claude extra more
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

## A developer gives list an unknown option

The only option `list` takes is `--help` or `-h`; any other option after
`list` is unknown, the top-level `--version` and `-V` included. The option is
named and echoed exactly as a top-level unknown option is. Read left to
right, an unknown option before the harness fails here, and an unknown
option after a valid harness fails here too: `agent-monitor list claude
--bogus` fails with `agent-monitor: unknown option '--bogus'`.

Command:

```
$ agent-monitor list --bogus
```

```
$ agent-monitor list --bogus claud
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

## A developer's harness data cannot be read

When the place the harness registers its live sessions exists but cannot be
read, `list` prints nothing and fails, naming the path it tried. That place
is the directory `$HOME/.claude/sessions` for `claude`; the directory
`$HOME/.codex/thread-writer-locks` or `/proc/locks` for `codex`; and
`$HOME/.grok/active_sessions.json` for `grok`, which also cannot be read
when it is not valid JSON. A directory is named without a trailing `/`, and
the path is escaped as CWD is in a row, without quotes and with `'` as typed.
`<reason>` is the system's description of the failure and varies, so an
unreadable Claude registry fails with `agent-monitor: cannot read
/home/dev/.claude/sessions: permission denied`; for an index that is not
valid JSON it is `not valid JSON`, as in `agent-monitor: cannot read
/home/dev/.grok/active_sessions.json: not valid JSON`. A place that does not
exist is not a failure: it means no live sessions.

Command:

```
$ agent-monitor list grok
```

Output:

```
agent-monitor: cannot read /home/dev/.grok/active_sessions.json: <reason>
```

Exits 3. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- `/home/dev/.grok/active_sessions.json` exists and is not readable by the
  developer, or is not valid JSON.

Postconditions:

- Nothing has changed.

## A developer's HOME is not set

Every location `list` reads is under `$HOME`. When `HOME` is unset or empty
`list` fails; it never looks the home directory up elsewhere. The arguments
are checked first, so a usage error is reported as such without `HOME`:
`env -u HOME agent-monitor list bogus` fails with `agent-monitor: unknown
harness 'bogus'` and exits 2, and `env -u HOME agent-monitor list --help`
prints the help of `list`.

Command:

```
$ env -u HOME agent-monitor list claude
```

```
$ HOME= agent-monitor list claude
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
