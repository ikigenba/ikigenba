# Stories — reading the chat of an agent

`agent-monitor chat <harness> <session-id> [<agent-id>]` prints a snapshot of
the chat of one agent of a session and exits; it does not watch or redraw.
The harness is `claude`, `codex`, or `grok`, spelled exactly so, and the
session is named by its full session id, found as `tree` finds it: any root
session of that harness in its default location under `$HOME`, live or
ended; a subagent is never a session, and `$HOME` is never guessed. With no
agent id the agent is the session's root; with one it is that subagent, at
any depth, named by the full id `tree` prints for it in brackets. The output
is one entry per thing said or done, in the order the agent's own transcript
records them, then the totals line. An entry is a header line, then its body
lines exactly as recorded with no indentation, then one empty line. The
header is `<time> <kind>`: the time the entry was recorded, in UTC,
`YYYY-MM-DDTHH:MM:SSZ`, converted and truncated as `list` prints LAST ACTIVE,
and the kind. The kinds are `user`, a prompt a person typed; `assistant`, the
agent's reply text; `reasoning`, the agent's reasoning, only when the
transcript holds it as readable text; `agent`, text passed between agents,
such as the task a parent gives a subagent or the notice that a subagent has
finished; `tool <name>`, a tool call, whose body is the tool's arguments as
the harness recorded them, printed as one line of JSON: a newline in an
argument appears as `\n`, and arguments longer than 200 characters are cut
to their first 200 characters followed by `…`, counting characters, not
bytes; and `result ok` or `result error`, whether a tool call succeeded or
failed. `result error` means the harness recorded the call as failed; a
command-running tool whose command exits non-zero is an error on every
harness, whether the harness marks the call failed, as Claude Code does, or
records the exit code in its output, as Codex does. A result is its own
entry, in its place in the transcript, and has no body: what the tool
returned is never printed. Everything else the
transcript holds is skipped: context the harness injects (the bodies of
`AGENTS.md` and `CLAUDE.md`, system reminders, environment blocks, skill
bodies, developer-role setup); reasoning or a message between agents whose
text the harness recorded only in encrypted form; and system and status
records (turn durations, hook summaries, mode changes, titles, file
snapshots, queue operations). In a body a newline and a tab are printed as
recorded and every other control character as `\xNN`, hex in lowercase; in
a tool name every control character, a newline and a tab included, is
printed as `\xNN`, so a header is always one line and an entry can never
forge one. After the last entry's empty line comes the totals line,
`tokens: in <n>  cache-write <n>  cache-read <n>  out <n>  reasoning <n>  calls <n>`,
every field always present, in that order, joined by two spaces: `in` is the
input tokens neither read from nor written to a cache, `cache-write` the
input tokens written to a cache, `cache-read` the input tokens served from
one, so the three add up to the whole prompt; `out` is every token generated,
reasoning included; `reasoning` is the part of `out` spent reasoning, already
counted in `out`; and `calls` is how many model calls were made. A value the
harness does not record is `-`. The totals are the agent's own usage: a
subagent's tokens are never added to its parent's, on any harness. There is
no cost and no colour. `chat` only reads: it takes no lock, changes nothing,
and writes no file.

## A developer reads a Claude Code session's chat

With no agent id, `chat` prints the root's chat, read from the session's
transcript, `$HOME/.claude/projects/<encoded-cwd>/<sessionId>.jsonl`. Claude
Code writes each part of a reply, a thinking block, its text, a tool call, as
a record of its own, and every record of one reply carries the same
`message.id` and the same `usage`: that reply is one model call, and its
usage is counted once. `input_tokens` is `in`, `cache_creation_input_tokens`
is `cache-write`, `cache_read_input_tokens` is `cache-read`, `output_tokens`
is `out`, and `output_tokens_details.thinking_tokens` is `reasoning`. The
thinking block here is empty, as Claude Code usually records it, so no
`reasoning` entry is printed, while its tokens are still counted. The first
record is injected context and the last a turn duration; neither is printed.

The records of
`/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93.jsonl`,
in order, with their other keys left out:

```
{"type":"user","isMeta":true,"message":{"role":"user","content":"<system-reminder>\nContents of /home/dev/src/shop/CLAUDE.md:\n\nRun make test before committing.\n</system-reminder>"},"timestamp":"2026-09-24T19:58:03.101Z"}
{"type":"user","message":{"role":"user","content":"commit all files"},"timestamp":"2026-09-24T19:58:03.412Z"}
{"type":"assistant","message":{"id":"msg_01Qm4","role":"assistant","content":[{"type":"thinking","thinking":"","signature":"EqQBCkYIBxgCKkA"}],"usage":{"input_tokens":3,"cache_creation_input_tokens":5120,"cache_read_input_tokens":11840,"output_tokens":96,"output_tokens_details":{"thinking_tokens":30}}},"timestamp":"2026-09-24T19:58:05.030Z"}
{"type":"assistant","message":{"id":"msg_01Qm4","role":"assistant","content":[{"type":"text","text":"I'll check the tree first."}],"usage":{"input_tokens":3,"cache_creation_input_tokens":5120,"cache_read_input_tokens":11840,"output_tokens":96,"output_tokens_details":{"thinking_tokens":30}}},"timestamp":"2026-09-24T19:58:05.377Z"}
{"type":"assistant","message":{"id":"msg_01Qm4","role":"assistant","content":[{"type":"tool_use","id":"toolu_01Hx","name":"Bash","input":{"command":"git status --short","description":"Show working tree status"}}],"usage":{"input_tokens":3,"cache_creation_input_tokens":5120,"cache_read_input_tokens":11840,"output_tokens":96,"output_tokens_details":{"thinking_tokens":30}}},"timestamp":"2026-09-24T19:58:06.204Z"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01Hx","content":" M cart.go\n?? refund.go"}]},"timestamp":"2026-09-24T19:58:07.650Z"}
{"type":"assistant","message":{"id":"msg_01Rt7","role":"assistant","content":[{"type":"tool_use","id":"toolu_01Jk","name":"Bash","input":{"command":"git add -A && git commit -m \"Fix refund rounding\"","description":"Commit all changes"}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":180,"cache_read_input_tokens":16960,"output_tokens":64,"output_tokens_details":{"thinking_tokens":0}}},"timestamp":"2026-09-24T19:58:09.118Z"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01Jk","content":"error: gpg failed to sign the data","is_error":true}]},"timestamp":"2026-09-24T19:58:10.402Z"}
{"type":"assistant","message":{"id":"msg_01Sv2","role":"assistant","content":[{"type":"text","text":"The commit failed: gpg could not sign it.\nUnlock your key and I'll retry."}],"usage":{"input_tokens":1,"cache_creation_input_tokens":91,"cache_read_input_tokens":17331,"output_tokens":41,"output_tokens_details":{"thinking_tokens":0}}},"timestamp":"2026-09-24T19:58:12.870Z"}
{"type":"system","subtype":"turn_duration","durationMs":9458,"timestamp":"2026-09-24T19:58:12.871Z"}
```

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

Output:

```
2026-09-24T19:58:03Z user
commit all files

2026-09-24T19:58:05Z assistant
I'll check the tree first.

2026-09-24T19:58:06Z tool Bash
{"command":"git status --short","description":"Show working tree status"}

2026-09-24T19:58:07Z result ok

2026-09-24T19:58:09Z tool Bash
{"command":"git add -A && git commit -m \"Fix refund rounding\"","description":"Commit all changes"}

2026-09-24T19:58:10Z result error

2026-09-24T19:58:12Z assistant
The commit failed: gpg could not sign it.
Unlock your key and I'll retry.

tokens: in 5  cache-write 5391  cache-read 46131  out 201  reasoning 30  calls 3
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- `/home/dev/.claude/sessions/41822.json` registers session
  `7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93` in `/home/dev/src/shop`.
- The session's transcript holds exactly the records above.

Postconditions:

- Nothing has changed.

## A developer reads a Claude Code subagent's chat

With an agent id, `chat` prints that subagent's chat, read from its own
transcript, `subagents/agent-<agentId>.jsonl` beside the session's; a
subagent started by another subagent is read the same way. The task its
parent gave it is the first record of that transcript: it was written by an
agent, not typed by a person, so it is an `agent` entry. The totals are the
subagent's own, and none of them is counted in the session's.

The records of
`/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a1c4e7f09b2d38561.jsonl`,
in order, with their other keys left out:

```
{"type":"user","isSidechain":true,"agentId":"a1c4e7f09b2d38561","message":{"role":"user","content":"Find where checkout requests are handled.\nReport the file and the function."},"timestamp":"2026-09-24T20:01:14.220Z"}
{"type":"assistant","isSidechain":true,"agentId":"a1c4e7f09b2d38561","message":{"id":"msg_02Ab9","role":"assistant","content":[{"type":"tool_use","id":"toolu_02Kp","name":"Grep","input":{"pattern":"func .*Checkout","path":"/home/dev/src/shop"}}],"usage":{"input_tokens":3,"cache_creation_input_tokens":4210,"cache_read_input_tokens":9870,"output_tokens":58,"output_tokens_details":{"thinking_tokens":0}}},"timestamp":"2026-09-24T20:01:16.045Z"}
{"type":"user","isSidechain":true,"agentId":"a1c4e7f09b2d38561","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_02Kp","content":"internal/cart/checkout.go:42:func HandleCheckout(w http.ResponseWriter, r *http.Request) {"}]},"timestamp":"2026-09-24T20:01:16.913Z"}
{"type":"assistant","isSidechain":true,"agentId":"a1c4e7f09b2d38561","message":{"id":"msg_02Bc4","role":"assistant","content":[{"type":"text","text":"The handler is HandleCheckout in internal/cart/checkout.go, line 42."}],"usage":{"input_tokens":1,"cache_creation_input_tokens":160,"cache_read_input_tokens":14080,"output_tokens":37,"output_tokens_details":{"thinking_tokens":0}}},"timestamp":"2026-09-24T20:01:19.588Z"}
```

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a1c4e7f09b2d38561
```

Output:

```
2026-09-24T20:01:14Z agent
Find where checkout requests are handled.
Report the file and the function.

2026-09-24T20:01:16Z tool Grep
{"pattern":"func .*Checkout","path":"/home/dev/src/shop"}

2026-09-24T20:01:16Z result ok

2026-09-24T20:01:19Z assistant
The handler is HandleCheckout in internal/cart/checkout.go, line 42.

tokens: in 4  cache-write 4370  cache-read 23950  out 95  reasoning 0  calls 2
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Session `7c2e9a41-…` is registered in `/home/dev/src/shop`, and
  `agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93` draws
  subagent `a1c4e7f09b2d38561`.
- The subagent's transcript holds exactly the records above.

Postconditions:

- Nothing has changed.

## A developer reads a Codex subagent's chat without the history it copied

A Codex subagent is a thread with its own rollout,
`$HOME/.codex/sessions/YYYY/MM/DD/rollout-*-<thread-id>.jsonl`. That rollout
begins with a copy of its parent's history; the copy is the parent's chat,
not the subagent's, so none of it is printed and none of its usage is
counted: the subagent's chat starts at its own first record. The task its
parent sent it is an `agent` entry, printed as recorded. Codex records its
reasoning only in encrypted form, so a Codex chat never has a `reasoning`
entry, while the reasoning tokens it records are counted.

Command:

```
$ agent-monitor chat codex 01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4 01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07
```

Output:

```
2026-09-24T20:14:31Z agent
Message Type: NEW_TASK
Task name: /root/forecast
Sender: /root
Payload:
Get tomorrow's forecast for Berlin.

2026-09-24T20:14:34Z tool exec_command
{"cmd":"curl -s 'wttr.in/Berlin?format=3'"}

2026-09-24T20:14:35Z result ok

2026-09-24T20:14:37Z tool spawn_agent
{"task_name":"radar","message":"Check the radar for rain over Berlin tonight."}

2026-09-24T20:14:38Z result ok

2026-09-24T20:14:41Z assistant
Berlin tomorrow: 14°C, light rain in the afternoon.

tokens: in 1204  cache-write 0  cache-read 44160  out 261  reasoning 128  calls 3
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0d0ab-…` is a root with a rollout under
  `/home/dev/.codex/sessions/`, and it started thread
  `01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07` at `/root/forecast`.
- The rollout of `01a0d7c3-…` begins with a copy of the history of
  `01a0d0ab-…`: its developer-role setup, the prompt `what's the weather in
  berlin`, the parent's replies and tool calls, and their token counts.
- After the copy, the rollout of `01a0d7c3-…` records, in order: the task
  message from `/root` shown above; an `exec_command` call with the arguments
  shown and its successful output; a `spawn_agent` call with the arguments
  shown and its successful output; and the reply shown, each at the time
  shown; along with encrypted reasoning, and token counts for three model
  calls that together used 1204 fresh input tokens, wrote 0 to the cache,
  read 44160 from it, and generated 261 tokens, 128 of them reasoning.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer reads a Grok session's chat

A Grok session's chat, and the time of each entry, is read from the
session's `updates.jsonl`, in its directory
`$HOME/.grok/sessions/<url-encoded-cwd>/<session-id>/`. Grok records each
prompt inside a `<user_query>` wrapper, with the context it injects around
it; `chat` prints only the text the developer typed. Grok also records a
readable summary of its reasoning, printed as a `reasoning` entry. A tool
call's arguments are the ones Grok recorded for it; here the `write` call's
arguments run past 200 characters, so they are cut. Grok records every field
of the totals line.

Command:

```
$ agent-monitor chat grok 01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47
```

Output:

```
2026-09-24T21:10:02Z user
the login page redirects forever after sign-in

2026-09-24T21:10:05Z reasoning
The loop likely comes from the session cookie being set for the wrong domain.
I should find the redirects in the login handler first.

2026-09-24T21:10:06Z assistant
I'll look at the login handler's redirects.

2026-09-24T21:10:06Z tool run_terminal_command
{"command":"grep -n Redirect auth/login.go","description":"Find the redirects in the login handler"}

2026-09-24T21:10:07Z result ok

2026-09-24T21:10:15Z tool write
{"file_path":"/home/dev/src/site/auth/cookie.go","content":"package auth\n\nimport \"net/http\"\n\n// sessionCookie is set for the parent domain so the redirect back from sign-in carries it.\nfunc ses…

2026-09-24T21:10:15Z result ok

2026-09-24T21:10:19Z assistant
The cookie was scoped to the login host only, so the site never saw it.
It is now set for the parent domain.

tokens: in 18420  cache-write 0  cache-read 36864  out 912  reasoning 388  calls 3
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Session `01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47` is a root, with its files in
  `/home/dev/.grok/sessions/%2Fhome%2Fdev%2Fsrc%2Fsite/01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47/`.
- The session's `updates.jsonl` records, in order and at the times shown:
  the prompt shown, inside a `<user_query>` wrapper with injected context
  around it; the reasoning summary shown; the reply shown; a
  `run_terminal_command` call with the arguments
  `{"command":"grep -n Redirect auth/login.go","description":"Find the redirects in the login handler"}`
  that completed; a `write` call with the arguments
  `{"file_path":"/home/dev/src/site/auth/cookie.go","content":"package auth\n\nimport \"net/http\"\n\n// sessionCookie is set for the parent domain so the redirect back from sign-in carries it.\nfunc sessionCookie(value string) *http.Cookie {\n\treturn &http.Cookie{Name: \"session\", Value: value, Domain: \".example.com\", Path: \"/\"}\n}\n"}`
  that completed; and the reply shown.
- Grok recorded three model calls for the session that together used 18420
  fresh input tokens, wrote 0 to the cache, read 36864 from it, and
  generated 912 tokens, 388 of them reasoning.

Postconditions:

- Nothing has changed.

## A developer reads the chat of an agent that has made no model calls yet

An agent that has made no model call yet has used no tokens, so every field
of the totals line that the harness records is `0`; a field the harness never
records is still `-`. Its chat holds whatever has been recorded so far. An
agent with no entries at all prints only the totals line, and so does an
agent whose transcript has not been written yet: it is read as a transcript
with no records, and `chat` exits 0. Here a Codex subagent has just been
given its task and has not yet called the model.

Command:

```
$ agent-monitor chat codex 01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4 01a0d8f1-0b6e-7d24-9c3a-5e7f1a2b4c68
```

Output:

```
2026-09-24T20:14:32Z agent
Message Type: NEW_TASK
Task name: /root/alerts
Sender: /root
Payload:
List the weather alerts for Berlin.

tokens: in 0  cache-write 0  cache-read 0  out 0  reasoning 0  calls 0
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Thread `01a0d0ab-…` is a root with a rollout under
  `/home/dev/.codex/sessions/`, and it started thread
  `01a0d8f1-0b6e-7d24-9c3a-5e7f1a2b4c68` at `/root/alerts`.
- The rollout of `01a0d8f1-…` begins with a copy of the history of
  `01a0d0ab-…`, token counts included, and after the copy records only its
  turn's start and the task message from `/root` shown above, at the time
  shown.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer reads the chat of an agent that is still writing

An agent that is running may be part way through writing a record when
`chat` reads its transcript. A half-written last line is not shown and its
usage is not counted: the chat ends at the last complete record. A line in
the middle of a transcript that cannot be read as a record is skipped, and
the records after it are shown as usual. Here a Claude Code subagent is
running and the last line of its transcript is half-written.

The records of
`/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a5b8c1f4e6d372905.jsonl`,
in order, with their other keys left out; the last line ends where shown,
with no newline:

```
{"type":"user","isSidechain":true,"agentId":"a5b8c1f4e6d372905","message":{"role":"user","content":"Draft a fix for refunds that round down."},"timestamp":"2026-09-24T20:05:40.118Z"}
{"type":"assistant","isSidechain":true,"agentId":"a5b8c1f4e6d372905","message":{"id":"msg_03Cd1","role":"assistant","content":[{"type":"text","text":"I'll start from the refund calculation."}],"usage":{"input_tokens":3,"cache_creation_input_tokens":3980,"cache_read_input_tokens":9870,"output_tokens":22,"output_tokens_details":{"thinking_tokens":0}}},"timestamp":"2026-09-24T20:05:42.731Z"}
{"type":"assistant","isSidechain":true,"agentId":"a5b8c1f4e6d372905","message":{"id":"msg_03De8","role":"assistant","content":[{"type":"text","text":"Rounding happens in
```

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a5b8c1f4e6d372905
```

Output:

```
2026-09-24T20:05:40Z agent
Draft a fix for refunds that round down.

2026-09-24T20:05:42Z assistant
I'll start from the refund calculation.

tokens: in 3  cache-write 3980  cache-read 9870  out 22  reasoning 0  calls 1
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Session `7c2e9a41-…` is registered in `/home/dev/src/shop`, and
  `agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93` draws
  subagent `a5b8c1f4e6d372905`.
- The subagent's transcript holds exactly the lines above.

Postconditions:

- Nothing has changed.

## A developer passes the session id as the agent id

`tree` prints the root's session id in brackets like any subagent's id, so a
developer can pass it as the agent id: it names the root, and the output is
the root's chat, exactly as with no agent id.

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

Output:

```
2026-09-24T19:58:03Z user
commit all files

2026-09-24T19:58:05Z assistant
I'll check the tree first.

2026-09-24T19:58:06Z tool Bash
{"command":"git status --short","description":"Show working tree status"}

2026-09-24T19:58:07Z result ok

2026-09-24T19:58:09Z tool Bash
{"command":"git add -A && git commit -m \"Fix refund rounding\"","description":"Commit all changes"}

2026-09-24T19:58:10Z result error

2026-09-24T19:58:12Z assistant
The commit failed: gpg could not sign it.
Unlock your key and I'll retry.

tokens: in 5  cache-write 5391  cache-read 46131  out 201  reasoning 30  calls 3
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- The session and its transcript are exactly those of the story in which a
  developer reads a Claude Code session's chat.

Postconditions:

- Nothing has changed.

## A developer asks what chat can do

`--help` or `-h` anywhere after `chat` prints the help of `chat` and wins
over everything else after `chat`, a missing, unknown, or extra argument or
an unknown option included. It needs no session data, so it works with
`HOME` unset. The exit codes are those of the top-level help and are not
repeated here.

Command:

```
$ agent-monitor chat --help
```

```
$ agent-monitor chat -h
```

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 --help
```

```
$ agent-monitor chat bogus extra more more --bogus -h
```

Options:

- `--help`, `-h`: print the help of `chat` and exit.

Output:

```
Usage: agent-monitor chat <harness> <session-id> [<agent-id>]

Print one agent's chat and its token totals.

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

## A developer gives chat no harness

Command:

```
$ agent-monitor chat
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

## A developer gives chat no session id

Command:

```
$ agent-monitor chat claude
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

## A developer names an unknown harness to chat

The harness must be one of `claude`, `codex`, or `grok` exactly, so `Claude`
and the empty argument are unknown too. The harness is echoed exactly as an
unknown command is. The arguments after `chat` are read left to right and the
first error wins, so `agent-monitor chat claud` fails here, not as a missing
session id, and `agent-monitor chat claud --bogus` fails here, not as an
unknown option.

Command:

```
$ agent-monitor chat claud 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

```
$ agent-monitor chat claud
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

## A developer gives chat an extra argument

`chat` takes one harness, one session id, and at most one agent id. The
extra argument is echoed exactly as an unknown command is. The arguments are
all checked before any session is looked for, so an extra argument fails here
even when the session or the agent does not exist.

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a1c4e7f09b2d38561 extra
```

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a1c4e7f09b2d38561 extra more
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

## A developer gives chat an unknown option

The only option `chat` takes is `--help` or `-h`; any other option after
`chat` is unknown, the top-level `--version` and `-V` included. `chat` never
prints colour, so the `--no-color` that `tree` takes is unknown to `chat`
too: `agent-monitor chat --no-color claude
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93` fails with `agent-monitor: unknown
option '--no-color'`. The option is named and echoed exactly as a top-level
unknown option is. Read left to right, an unknown option before the harness
fails here, and so does one after a valid harness, session id, or agent id:
`agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 --bogus`
fails with `agent-monitor: unknown option '--bogus'`.

Command:

```
$ agent-monitor chat --bogus
```

```
$ agent-monitor chat --bogus claud
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

## A developer names a session that does not exist to chat

When no root session of the harness has the id, `chat` prints nothing and
fails, as `tree` does, whether or not an agent id follows. The id must be
given in full, and a subagent's id names no session, so it fails this way
too. The id is echoed exactly as an unknown command is.

Command:

```
$ agent-monitor chat claude 0b9e4d12-7a3c-4f58-9e21-6c8d0a5b3f47
```

```
$ agent-monitor chat claude 0b9e4d12-7a3c-4f58-9e21-6c8d0a5b3f47 a1c4e7f09b2d38561
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

## A developer names an agent that is not in the session

When the session is found but no agent in it has the agent id, `chat` prints
nothing and fails. The agent id must be given in full, as `tree` prints it: a
prefix of an agent's id names no agent, and neither does the id of a subagent
of another session. Both ids are echoed exactly as an unknown command is.

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a0b1c2d3e4f506172
```

Output:

```
agent-monitor: no claude agent 'a0b1c2d3e4f506172' in session '7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Session `7c2e9a41-…` is registered in `/home/dev/src/shop`, and
  `agent-monitor tree claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93` draws no
  line with id `a0b1c2d3e4f506172`.

Postconditions:

- Nothing has changed.

## A developer names an agent whose transcript cannot be read

When the agent is found but its transcript cannot be read, `chat` prints
nothing and fails, naming the path it tried, escaped as `tree` prints a
label. `<reason>` is the system's description of the failure and varies, so
here it fails with `agent-monitor: cannot read
/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c3b049672.jsonl:
permission denied`. When the place `chat` looks for the session exists but
cannot be read, it fails the same way, naming that place, as `tree` does.

Command:

```
$ agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a2d5f8e1c3b049672
```

Output:

```
agent-monitor: cannot read /home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c3b049672.jsonl: <reason>
```

Exits 3. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`.
- Session `7c2e9a41-…` is registered in `/home/dev/src/shop`, and its
  `subagents/` directory holds `agent-a2d5f8e1c3b049672.meta.json`, which is
  readable, beside `agent-a2d5f8e1c3b049672.jsonl`, which exists and is not
  readable by the developer.

Postconditions:

- Nothing has changed.

## A developer runs chat with HOME not set

Every location `chat` reads is under `$HOME`. When `HOME` is unset or empty
`chat` fails; it never looks the home directory up elsewhere. The arguments
are checked first, so a usage error is reported as such without `HOME`:
`env -u HOME agent-monitor chat claude` fails with
`agent-monitor: missing session id` and exits 2, and
`env -u HOME agent-monitor chat --help` prints the help of `chat`.

Command:

```
$ env -u HOME agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93
```

```
$ HOME= agent-monitor chat claude 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 a1c4e7f09b2d38561
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
