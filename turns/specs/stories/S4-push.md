# Stories — push

`turns push` reads the session files of Claude Code, Codex CLI and Grok CLI in the home it runs in, turns them into turns, asks the space which turns it already holds, and pushes the turns it lacks. A person runs it by hand; `turns.service`, started every five minutes by `turns.timer`, runs the same command, and its stdout and stderr go to the user journal. The top-level usage gains the line `  push      push new turns to the space` under `Commands:`. It reads `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl` (one session each) and `~/.claude/projects/<encoded-cwd>/<session-id>/subagents/agent-*.jsonl` (one subagent session each, whose parent is `<session-id>`); `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (one session each) and `~/.codex/archived_sessions/`; `~/.grok/sessions/<url-encoded-cwd>/<session-uuid>/` (one session per directory, usage from its `updates.jsonl`); and `~/.local/state/dispatch/jobs/<job>/session-id`. `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `GROK_HOME`, when set, stand in for `~/.claude`, `~/.codex` and `~/.grok`. A push reads only the session files modified within or after the second `$XDG_STATE_HOME/turns/last-push` holds: a Claude Code `<session-id>.jsonl`, and each subagent `agent-*.jsonl` on its own; a Codex CLI rollout file; a Grok CLI session directory, which has changed when its `updates.jsonl` or the directory itself was modified. A file or directory modified before that second is not read; one modified within it is read again. When `last-push` does not exist, as before the first push, every file is read, and `turns push --all` reads every file the same way. Every `~/.local/state/dispatch/jobs/<job>/session-id` is read on each run, and when a Claude Code session the run reads names a job directory, the job's Codex CLI or Grok CLI session is read too, changed or not. A turn carries its session, its parent session if any, the host (the machine's short host name, up to the first dot), the working directory, the tool (`claude`, `codex` or `grok`), the model, the moment the tool recorded, whose turn it was (the person, the agent, or a tool result), the token counts input, cached input, output and reasoning as the tool reports them, whether the session is interactive or headless, and, for a session in which the person ran a command or the agent called a skill, the first command's name and the first line of its argument. Nothing else leaves the machine: no text of any prompt, answer, thought, tool call or tool result, and not the person, whom the space knows from the token. Whose turn a line is: a Claude Code user line is the person's turn when its content is a string and it is not `isMeta`; a user line whose content is a `tool_result`, or whose `origin.kind` is `task-notification`, `peer` or `coordinator`, is a tool result; Claude Code assistant lines are one turn per `message.id`, later lines with the same id being the same response. Codex CLI usage is one turn per `response_id` from its `token_usage_record` lines, with the model from `turn_context`. Grok CLI usage comes from the `turn_completed` updates in `updates.jsonl`, with the model from the assistant chat line's `model_id`. The token counts map as follows: Claude Code `input_tokens` + `cache_creation_input_tokens` as input, `cache_read_input_tokens` as cached input, `output_tokens` as output, `thinking_tokens` as reasoning; Codex CLI `input_tokens`, `cached_input_tokens`, `output_tokens`, `reasoning_output_tokens`; Grok CLI `inputTokens`, `cachedReadTokens`, `outputTokens`, `reasoningTokens`. Every exchange with the space is an HTTP POST of one JSON-RPC `tools/call` to `https://mcp.<space>/mcp`, naming the service `sessions`, with `Authorization: Bearer <token>`; the question of which turns the space holds goes through the gateway's read call, and each push through its write call; there is no initialize and no session. The space and the token come from `$XDG_CONFIG_HOME/turns/` (default `~/.config/turns/`), or, on a host, the space from the host default. Push first asks the space which turns it already holds of the sessions it read, and a run that read none makes no request: for each session, up to 500 sessions at a time, the space answers the moment of the latest turn it holds, and it answers each push the same way for the sessions the push carried. Push then sends that latest turn again with the turns after it and nothing earlier; a turn pushed twice is one turn, and a re-sent turn replaces the fields the space held for it. The report counts only turns the space did not hold before, so a re-sent turn is not counted. A batch may hold hundreds of turns from many sessions. Push never changes, moves or removes a session file or a dispatch job file. The report on stdout is three lines, `claude`, `codex`, `grok` in that order, each counting the sessions that gained turns and the turns pushed, with `session` and `turn` singular when the count is 1. After a push that completes, push writes the moment that run started, in UTC as `YYYY-MM-DDTHH:MM:SSZ`, to `$XDG_STATE_HOME/turns/last-push` (default `~/.local/state/turns/last-push`), so it holds the start of the last successful push and a file written while a run is under way is read by the next; a push that fails leaves it unchanged, so the next run reads again every file changed since the last successful push started. `turns status` reads it too. A file or, for Grok CLI, a session directory that push cannot read as a session is skipped and reported on a line of its own under its tool's line, `<tool>: <n> sessions skipped`, singular when 1. When the space cannot be reached or refuses a request, the diagnostic is `turns: <what failed>: <reason>`; detail turns writes itself follows unprefixed after one empty line, and an answer from the gateway is quoted there with every line prefixed `> `.

## A person asks what `push` can do

Command:

```
$ turns push --help
```

```
$ turns push -h
```

Output:

```
Usage: turns push [--dry-run] [--all]

Read this home's Claude Code, Codex CLI and Grok CLI session files, ask the
space which turns it already holds, and push the turns it lacks. Session
files are never changed, and no text leaves the machine except a first
command's name and the first line of its argument.

Options:
  --dry-run    report what would be pushed and push nothing
  --all        read every session file, not just changed ones
  -h, --help   print this help
```

Exits 0. The text is on stdout; stderr is empty. It prints whether or not turns is configured; nothing is read and the space is not asked.

Preconditions:

- `turns` is on the PATH.

Postconditions:

- Nothing has changed. No session file was read and no request was made.

## A person pushes their sessions for the first time

A person has just configured turns on a laptop where the three tools have been used for weeks. `last-push` does not exist yet, so every session file is read, and the space holds nothing of theirs yet, so every turn in every session file is sent. A Claude Code session file is one JSON object per line; the lines push reads look like this, shown with invented ids and with every text field elided, since push never sends one:

```
{"type":"user","sessionId":"5d0c1a2e-0000-4000-8000-000000000001","cwd":"/home/pat/work/shop","entrypoint":"cli","timestamp":"2026-10-20T09:00:00.000Z","message":{"role":"user","content":"<text>"}}
{"type":"assistant","sessionId":"5d0c1a2e-0000-4000-8000-000000000001","cwd":"/home/pat/work/shop","timestamp":"2026-10-20T09:00:07.000Z","message":{"id":"msg_0001","model":"claude-opus-5-5","usage":{"input_tokens":12,"cache_creation_input_tokens":3000,"cache_read_input_tokens":9000,"output_tokens":420}}}
{"type":"user","sessionId":"5d0c1a2e-0000-4000-8000-000000000001","cwd":"/home/pat/work/shop","timestamp":"2026-10-20T09:00:09.000Z","message":{"role":"user","content":[{"type":"tool_result","content":"<text>"}]}}
```

The first line is the person's turn, the second the agent's (input 3012, cached input 9000, output 420, reasoning 0), the third a tool result. Lines of the assistant's that share one `message.id` are one turn.

Command:

```
$ turns push
```

Output:

```
claude: 214 sessions, 18402 turns pushed
codex: 31 sessions, 2210 turns pushed
grok: 6 sessions, 388 turns pushed
```

Exits 0. The text is on stdout; stderr is empty. The counts are those of the files present; subagent sessions count in their tool's line.

Preconditions:

- `~/.config/turns/config` holds `space=us.ikigenba.dev` and `~/.config/turns/token` holds a token the space accepts.
- `~/.claude/projects/`, `~/.codex/sessions/` and `~/.grok/sessions/` hold the sessions counted above.
- The space holds no turn pushed with this person's token. `~/.local/state/turns/last-push` does not exist.

Postconditions:

- The space's `sessions` service holds every turn of every session found, each once, attributed to the person the token belongs to, with the fields the opening paragraph lists and no text.
- `~/.local/state/turns/last-push` holds the moment the push started.
- No session file and no dispatch job file has changed.

## A person pushes again with nothing new

The machine has been quiet for five minutes: nothing has been written to any session file since the last push started. Push reads no session file, so it asks the space nothing and sends nothing, and a timer run on a quiet machine finishes in seconds.

Command:

```
$ turns push
```

Output:

```
claude: 0 sessions, 0 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured as in the first push, and that push completed.
- No session file has changed since that push started.

Postconditions:

- The space holds exactly the turns it held before.
- No session file was read and no request was made.
- `~/.local/state/turns/last-push` holds the moment this run started.
- No session file has changed.

## A person pushes a session that is still running, twice

A Claude Code session is open in another terminal. The first run sends the turns written so far; the person goes on working, and the second run sends the latest turn the space holds again, with the turns the session gained after it, and counts only those it gained. At the second run that file is the only one modified since the first run started, so it is the only file read and the only session the space is asked about. A trailing line the tool has not finished writing is not an error: it is left for a later run. An agent turn whose lines were still being written when it was first pushed is that latest held turn, so its fields are replaced with the complete ones.

Command:

```
$ turns push
```

Output:

```
claude: 1 session, 9 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Then, after the session has gained six turns:

Command:

```
$ turns push
```

Output:

```
claude: 1 session, 6 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured. Every other session is already held by the space, and no other session file changes between the two runs.
- `~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000002.jsonl` is being appended to by a running Claude Code session; it held nine turns at the first run and fifteen at the second.

Postconditions:

- The space holds all fifteen turns of the session, each once, the ninth with the fields it has in the file at the second run.
- The session file was only read; Claude Code went on appending to it undisturbed.

## The timer catches up after the machine was off

A laptop was shut overnight. `turns.timer` is persistent, so at the first boot after the missed runs it starts `turns.service` once, not once per missed run, and that run reads every file changed since the last successful push started and sends everything written to them since, whatever its age. The service runs `turns push`; its output goes to the user journal.

Command:

```
$ turns push
```

Output:

```
claude: 3 sessions, 512 turns pushed
codex: 1 session, 40 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout, which the user journal records; stderr is empty.

Preconditions:

- `turns install` has run, so `~/.config/systemd/user/turns.timer` is enabled.
- turns is configured. Sessions written before the machine was shut have not been pushed.

Postconditions:

- The space holds every turn of those sessions.
- No session file has changed.

## A person pushes sessions that ran subagents

Each tool records a subagent's work as a session of its own, and push sends it with its parent, so the space can roll it up into the session that ran it. A Claude Code subagent is a file under its parent's `subagents/` directory; its session id is `<parent session id>/<file stem>`, never the bare stem, and its parent is the enclosing session id, and a subagent that a subagent started has the same parent, the top session. A Codex CLI subagent thread is a rollout file of its own whose `session_meta` carries `thread_source` `subagent`; its parent is the `parent_thread_id` it names. A Grok CLI session whose directory names a `parent_session_id` has that session as parent.

```
~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000003.jsonl
~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000003/subagents/agent-a1.jsonl
~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000003/subagents/agent-a2.jsonl
~/.codex/sessions/2026/10/21/rollout-2026-10-21T10-00-00-0199c0de-0000-7000-8000-000000000010.jsonl
~/.codex/sessions/2026/10/21/rollout-2026-10-21T10-04-00-0199c0de-0000-7000-8000-000000000011.jsonl
```

`agent-a2.jsonl` is a subagent `agent-a1` started (its `parentAgentId` is `a1`). The second rollout's `session_meta` has `thread_source` `subagent` and `parent_thread_id` `0199c0de-0000-7000-8000-000000000010`.

Command:

```
$ turns push
```

Output:

```
claude: 3 sessions, 140 turns pushed
codex: 2 sessions, 58 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured; the space holds none of these sessions.
- The files above exist.

Postconditions:

- The space holds `5d0c1a2e-0000-4000-8000-000000000003/agent-a1` and `5d0c1a2e-0000-4000-8000-000000000003/agent-a2` as sessions whose parent is `5d0c1a2e-0000-4000-8000-000000000003`, and the Codex thread `0199c0de-0000-7000-8000-000000000011` as a session whose parent is `0199c0de-0000-7000-8000-000000000010`. The top sessions have no parent.
- No session file has changed.

## A person pushes a Codex job that a Claude Code session dispatched

A Claude Code session handed a task to Codex CLI through the dispatch skill. The `codex exec` session it started carries no parent of its own, but the dispatch job directory records its thread id, and the Claude session's tool results name that directory. Push reads every `~/.local/state/dispatch/jobs/<job>/session-id` on each run, finds the Claude session in whose tool results the job directory's path appears (a match on the path alone; nothing else of the tool result is read for this, and none of it is sent), and pushes the Codex session with that Claude session as its parent. When a Claude session the run reads names a job directory, the job's Codex session is read too, changed or not, so its parent is sent. A dispatched Grok CLI job is treated the same way. When more than one Claude session names the job directory, the earliest to name it is the parent. A parent found only after the child was pushed is set by the later run that reads the Claude session naming the job, which sends the child's latest held turn again, now carrying its parent. A job's session read in a later run while its dispatching Claude session is unchanged is pushed without a parent, and the space keeps the parent it already holds.

```
~/.local/state/dispatch/jobs/2026-10-22-1400-codex-demo/session-id    0199c0de-0000-7000-8000-000000000020
~/.codex/sessions/2026/10/22/rollout-2026-10-22T14-00-05-0199c0de-0000-7000-8000-000000000020.jsonl
~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000004.jsonl
```

One tool result in the Claude session contains `/home/pat/.local/state/dispatch/jobs/2026-10-22-1400-codex-demo/`.

Command:

```
$ turns push
```

Output:

```
claude: 1 session, 22 turns pushed
codex: 1 session, 17 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured; the space holds neither session.
- The files above exist. The rollout's `session_meta` has `originator` `codex_exec` and no parent.

Postconditions:

- The space holds the Codex session `0199c0de-0000-7000-8000-000000000020`, headless, whose parent is the Claude session `5d0c1a2e-0000-4000-8000-000000000004`.
- A job directory no Claude session names leaves its Codex or Grok session without a parent.
- No session file and no dispatch job file has changed.

## A person pushes a session that began with a command

A session started with `/deliver` and a document path is the one place push sends anything the person typed: the command's name and its argument, so the space can tie the session to what it delivered. The first command is the earlier in the session of two things: the first `<command-name>` found anywhere in a person's turn's content, not only at its start, skipping `clear` and `exit`, and the agent's first call of a skill, an assistant line whose `message.content` holds an item with `type` `tool_use` and `name` `Skill`. Of a `<command-name>`, its name is kept, and of what follows it, its `<command-args>`, only the first line, at most 256 bytes of it, cut back to a character boundary, and nothing else of that turn or any other. Of a skill call, the command is `/` followed by its `input.skill`, and its argument is the first line of its `input.args`, at most 256 bytes of it, cut back to a character boundary; a call whose `input.args` is absent or empty gives no argument, and nothing else of the tool use is kept. Typed prose after a command is prompt text, and prompt text never leaves the machine. Only Claude Code sessions carry a first command. The first person's turn of this session has, as its content:

```
<command-name>/clear</command-name>
```

and the second:

```
<command-message>deliver</command-message>
<command-name>/deliver</command-name>
<command-args>~/Documents/acme-billing.md</command-args>
```

Command:

```
$ turns push
```

Output:

```
claude: 1 session, 64 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured; the space does not hold this session.
- `~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000005.jsonl` begins with the two turns above.

Postconditions:

- The space holds the session with first command `/deliver` and argument `~/Documents/acme-billing.md`. A command with no argument is held with its name alone; a session with no command has none.
- No other text of the session is held. No session file has changed.

## A person pushes a session whose command follows other text

Claude Code can write other text into the person's turn before the command, such as a caveat about local commands, and the person can type more than one line after it. The first command is still found, and of its argument only the first line is kept. The first person's turn of this session has, as its content, with the caveat's text elided:

```
<local-command-caveat><text></local-command-caveat>
<command-message>deliver</command-message>
<command-name>/deliver</command-name>
<command-args>~/Documents/acme-billing.md
then run the sandbox check and tell me what failed</command-args>
```

Command:

```
$ turns push
```

Output:

```
claude: 1 session, 12 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured; the space does not hold this session.
- `~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000007.jsonl` begins with the turn above.

Postconditions:

- The space holds the session with first command `/deliver` and argument `~/Documents/acme-billing.md`; the second line of the argument and the caveat are not held.
- An argument whose first line is longer than 256 bytes, such as a pasted paragraph of 300 bytes, is held as its first 256 bytes, cut back to a character boundary, and the rest is not held.
- No session file has changed.

## A person pushes a session whose agent called a skill

A person can name a skill in the prose of a prompt rather than typing it as a command, so the person's turn carries no `<command-name>`, and the agent then calls the skill itself. That call is the session's first command. The first person's turn of this session has, as its content:

```
please run /deliver on ~/Documents/x.md and then tell me what failed
```

and the agent's next line, an assistant line, holds in its `message.content` the item:

```
{"type":"tool_use","id":"toolu_01","name":"Skill","input":{"skill":"deliver","args":"~/Documents/x.md"}}
```

Command:

```
$ turns push
```

Output:

```
claude: 1 session, 9 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured; the space does not hold this session.
- `~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000008.jsonl` begins with the turn and the tool use above, and no person's turn in it carries a `<command-name>` other than `clear` or `exit`.

Postconditions:

- The space holds the session with first command `/deliver` and argument `~/Documents/x.md`.
- The person's prose, including the inline `/deliver`, is not held, nor anything else of the tool use.
- Had a person's turn carried a `<command-name>` earlier in the session, that command would be the first command and the later skill call would not.
- No session file has changed.

## A person pushes headless and interactive sessions

Whether a session is interactive decides whether the space gives it attention, so push sends it with every turn. A Claude Code session whose `entrypoint` is `sdk-cli` is headless and one whose `entrypoint` is `cli` or `claude-desktop` is interactive; a Codex CLI session whose `originator` is `codex_exec` is headless and `codex-tui` or `Codex Desktop` interactive; a Grok CLI session whose `session_kind` is `headless` or `subagent` is headless. A value push does not know is sent as interactive. A Claude Code subagent session is headless.

Command:

```
$ turns push
```

Output:

```
claude: 2 sessions, 51 turns pushed
codex: 2 sessions, 33 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured; the space holds none of these sessions.
- One new Claude Code session with `entrypoint` `cli` and one with `sdk-cli`; one new Codex rollout with `originator` `codex-tui` and one with `codex_exec`.

Postconditions:

- The space holds the `cli` and `codex-tui` sessions as interactive and the `sdk-cli` and `codex_exec` sessions as headless.
- No session file has changed.

## A person asks what a push would send

Before a first push, or to see what the timer will do next, the person asks for the report without sending anything. A dry run reads the files a push would read now, those changed since the last successful push started, or every file with `--all`, and the space is still asked which turns it already holds of the sessions read, so the counts are exactly what a push would send now; `turns push --dry-run --all` reports what `turns push --all` would send. A dry run that read no file asks the space nothing.

Command:

```
$ turns push --dry-run
```

Options:

- `--dry-run` reads the files and asks the space which turns it already holds, reports, and pushes nothing. It writes nothing, `last-push` included.
- `--all`, with `--dry-run`, reads every session file, as `turns push --all` does.

Output:

```
claude: 2 sessions, 31 turns to push
codex: 0 sessions, 0 turns to push
grok: 1 session, 4 turns to push
```

Exits 0. The text is on stdout; stderr is empty. A dry run that cannot reach the space, or whose token is refused, fails as a push does, with the same diagnostic and exit 1, but prints no report lines.

Preconditions:

- turns is configured; the space lacks the turns counted above.

Postconditions:

- The space holds exactly what it held before.
- `~/.local/state/turns/last-push` is unchanged.
- No session file has changed.

## A person pushes every session file again

A person wants push to look again at every session file, not only those changed since the last push started, for example to try again a file that was skipped and has not changed since. With `--all` push reads every file, whatever `last-push` holds, as the first push does; the space is asked about every session read, and push sends each session's latest held turn again with the turns after it, so only turns the space lacks are counted.

Command:

```
$ turns push --all
```

Options:

- `--all` reads every session file, not only those modified within or after the second `last-push` holds.

Output:

```
claude: 0 sessions, 0 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured and an earlier push completed; the space holds every turn of every session file.

Postconditions:

- Every session file was read and the space was asked about every session.
- The space holds exactly the turns it held before.
- `~/.local/state/turns/last-push` holds the moment this run started.
- No session file has changed.

## A person pushes when the space cannot be reached

The laptop is offline, or the space's name does not resolve. Push stops before it sends anything; the report still prints, all zeros, because that is what was pushed.

Command:

```
$ turns push
```

Output:

```
claude: 0 sessions, 0 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

```
turns: push failed: cannot reach https://mcp.us.ikigenba.dev/mcp

<the network error>
```

Exits 1. The report is on stdout; the diagnostic is on stderr, its detail the network error as push met it, unprefixed because turns writes it.

Preconditions:

- turns is configured with `space=us.ikigenba.dev`; no connection to `mcp.us.ikigenba.dev` can be made.
- A session file has changed since the last push started.

Postconditions:

- The space is unchanged. `~/.local/state/turns/last-push` is unchanged.
- No session file has changed.

## A person pushes with a token the space refuses

The token was revoked, or mistyped at configure. The gateway refuses the first request, so nothing is sent.

Command:

```
$ turns push
```

Output:

```
claude: 0 sessions, 0 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

```
turns: push failed: the space refused the token

> <the gateway's answer, each line as it came, with the token, where it appears, shown as `<token>`>
```

Exits 1. The report is on stdout; the diagnostic is on stderr, the gateway's answer quoted after the empty line. The token itself is never printed.

Preconditions:

- turns is configured with a token the gateway answers with HTTP 401.
- A session file has changed since the last push started.

Postconditions:

- The space is unchanged. `~/.local/state/turns/last-push` is unchanged.
- No session file has changed.

## A person's push is refused part way through

Push sends turns in batches. The space accepted the first batches and then answered one with a tool error. Push stops at that batch; what the space accepted stays, and the report counts it.

Command:

```
$ turns push
```

Output:

```
claude: 40 sessions, 3100 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

```
turns: push failed: sessions refused a batch

> <the tool's error text, each line as it came, with the token, where it appears, shown as `<token>`>
```

Exits 1. The report is on stdout; the diagnostic is on stderr.

Preconditions:

- turns is configured; the space accepts the first batches and answers the next with an error result.

Postconditions:

- The space holds the turns of the accepted batches and nothing of the refused one or any after it.
- `~/.local/state/turns/last-push` is unchanged.
- No session file has changed.

## A person pushes again after a failed push

The failed run left `last-push` unchanged, so the next run, by hand or from the timer, reads again every file changed since the last successful push started, those the failed run read included, asks the space again, and sends the latest turn the space holds for each session and every turn after it, the refused batch included.

Command:

```
$ turns push
```

Output:

```
claude: 174 sessions, 15302 turns pushed
codex: 31 sessions, 2210 turns pushed
grok: 6 sessions, 388 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The previous run failed after the space accepted part of what was new; the cause has since cleared.

Postconditions:

- The space holds every turn of every session found, each once; no turn the earlier run sent is held twice.
- `~/.local/state/turns/last-push` holds the moment this run started.
- No session file has changed.

## A person pushes when one session file cannot be read as a session

A tool changed its file format, or a file under a tool's directory is not one of its sessions. Push skips that file, pushes everything else, and says how many it skipped on a line under that tool's line, in the wording the opening paragraph gives. A file whose last line is incomplete is not skipped.

Command:

```
$ turns push
```

Output:

```
claude: 2 sessions, 47 turns pushed
claude: 1 session skipped
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured.
- `~/.claude/projects/-home-pat-work-shop/5d0c1a2e-0000-4000-8000-000000000006.jsonl` holds a complete line that is not a JSON object.

Postconditions:

- The space holds the turns of the other sessions and nothing of the skipped file.
- The skipped file is untouched; a later run tries it again once the file changes, and `turns push --all` tries it again.

## A person who never installed one of the tools pushes

Grok CLI was never installed, so `~/.grok` does not exist. That is not an error: its line reports zeros, and nothing is said about the missing directory. The same holds for `~/.claude` or `~/.codex`, or for a directory `CLAUDE_CONFIG_DIR`, `CODEX_HOME` or `GROK_HOME` names that does not exist.

Command:

```
$ turns push
```

Output:

```
claude: 3 sessions, 120 turns pushed
codex: 1 session, 12 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured. `~/.grok` does not exist and `GROK_HOME` is unset.

Postconditions:

- The space holds the new Claude Code and Codex CLI turns.
- `~/.grok` still does not exist; push created nothing there.

## A person pushes before configuring turns

The space or the token is missing, so there is nowhere to push and nothing to push as. Push refuses before it reads a session file or makes a request. A token without a space, or a space without a token, is refused the same way. On a host with a default space the hint is `run 'turns configure'`, since only the token is missing; elsewhere it is `run 'turns configure <space>'`.

Command:

```
$ turns push
```

```
$ turns push --dry-run
```

Output:

```
turns: not configured

run 'turns configure <space>'
```

Exits 4. The text is on stderr; stdout is empty.

Preconditions:

- `~/.config/turns/config` or `~/.config/turns/token` is absent, and no host default names a space.

Postconditions:

- Nothing has changed. No session file was read and no request was made.

## The timer fires on a host for a person who has stored no token

On a host the timer runs for every account `opsctl user add` created, whether or not its person has stored a token. Until they do, push is inert: it refuses as above, the journal records the line, and the unit does not count it as a failure, so nothing alarms every five minutes. No token is minted for the person.

Command:

```
$ turns push
```

Output:

```
turns: not configured

run 'turns configure'
```

Exits 4, which `turns.service` counts as success. The text is on stderr, which the user journal records; stdout is empty.

Preconditions:

- The host's default names the space `us.ikigenba.dev`; `~/.config/turns/token` does not exist.
- The timer is enabled in the person's user manager.

Postconditions:

- Nothing has changed. No session file was read and no request was made.

## The timer fires while a person's push is still running

A first push of a large home can take longer than five minutes, or the person runs push by hand just as the timer fires. Two runs never push at once: the later one waits for the earlier to finish, then reads the files changed since the last successful push started, the earlier run's start when it succeeded, so a file written while the earlier run was under way is read, asks the space which turns it already holds of those sessions and sends the latest turn the space holds for each and every turn after it, usually nothing new. The wait is on the lock file `$XDG_STATE_HOME/turns/lock` (`~/.local/state/turns/lock` by default), and it has no time limit.

Command:

```
$ turns push
```

Output:

```
claude: 0 sessions, 0 turns pushed
codex: 0 sessions, 0 turns pushed
grok: 0 sessions, 0 turns pushed
```

Exits 0, once the earlier run has finished. The text is on stdout, which the user journal records; stderr is empty. The hand run's output is the same as if the timer had not fired.

Preconditions:

- A hand run of `turns push` is in progress when `turns.service` starts, and it succeeds.
- No session file changes after the hand run started.

Postconditions:

- The space holds each turn once.
- `~/.local/state/turns/last-push` holds the moment the later run started.
- No session file has changed.

## A person mistypes a push option

Command:

```
$ turns push --bogus
```

Output:

```
turns: unknown option '--bogus'

see 'turns push --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `turns` is on the PATH.

Postconditions:

- Nothing has changed. No session file was read and no request was made.

## A person gives push an argument

push takes no arguments; the only thing it accepts is its options.

Command:

```
$ turns push extra
```

Output:

```
turns: push takes no arguments

see 'turns push --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `turns` is on the PATH.

Postconditions:

- Nothing has changed. No session file was read and no request was made.
