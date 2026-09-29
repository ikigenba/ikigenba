# Stories — browsing the agents

`agent-monitor` run with no arguments, when both stdin and stdout are
terminals, opens a browser of the agents on this machine in the terminal's
alternate screen. (When stdin or stdout is not a terminal, the bare run
prints the help instead; that is the bootstrap group's story.) `list`, `tree`, `chat`, `-f`, `--help`, and
`--version` are unchanged. The browser has four levels, each a screen: the
harness menu, one harness's session menu, one session's agent menu, and one
agent's chat. Every screen has the same frame. Its first line is the
breadcrumb: `agent-monitor`, then ` › Claude Code`, ` › Codex`, or ` › Grok`
once a harness is open, then ` › <session-id>` once a session is open, then
` › <agent-id>` once an agent's chat is open, ids in full (`›` is U+203A).
Then one empty line, then the body. The terminal's last row is the key-hint
line: on the harness menu
`↑↓ move  → open  q quit`,
on the session and agent menus
`↑↓ move  → open  ← back  q quit`,
and on the chat
`↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]`,
where `[following]` reads `[paused]` while the chat is scrolled up. The vim keys, Enter, Esc, and
ctrl+c work but are not listed there. In a menu one row is highlighted in
reverse video; ↑ or `k` moves the highlight up one row and ↓ or `j` down one,
and neither moves it past the first or the last row. → or Enter opens the
highlighted item, the next level. ← goes back one level, with the highlight
on the item the developer came from; on the harness menu ← does nothing. `q`,
Esc, and ctrl+c quit from every screen. The highlight follows the item, a
session id or an agent id, not a row number: when rows are added or removed
above it, it moves with its item; when its item disappears, it moves to the
row that took the item's place, or to the new last row when the item was
last. A menu taller than the screen scrolls by the smallest amount that
keeps the highlight in view; only its rows that can be highlighted scroll,
while the session menu's header row stays at the top of the body and the
agent menu's key stays at its bottom. In a menu a row wider than the terminal
is cut at its edge; in the chat a long line wraps, cut into pieces exactly as
wide as the terminal, character by character, never at a word boundary.
Resizing the terminal redraws the screen to the new size. Every screen keeps
itself up to date exactly as following does: the menus as `list -f` and
`tree -f` keep their views, where a change is shown promptly after the harness
writes the file that records it, and a session's process exiting, which no
file records, is shown within a couple of seconds; and the chat as `chat -f`
follows, where each new entry is shown promptly after the agent's transcript
records it. Once the session menu, the agent menu, or the chat has been
drawn, a later failure to read what it shows leaves its last view in place,
the highlight unchanged, and the next refresh reads again, as following does.
The harness menu is the exception: a harness's count is `-` whenever its
latest read failed, even after the menu has been drawn. Only a first read that fails shows a
line saying why, as the whole of the body, in place of the table, the tree
and its key, or the chat and its totals line: `cannot read <path>: <reason>`
when data cannot be read, `no <harness> session '<id>'` when the session is
not found, and `no <harness> agent '<agent-id>' in session '<id>'` when the
agent is not found. These are the texts of the diagnostics `list`, `tree`,
and `chat` print, without the `agent-monitor: ` prefix; `<harness>` is
`claude`, `codex`, or `grok`, and paths and ids are escaped as there. The
screen keeps refreshing, and once a read succeeds the line is replaced by
what the screen shows. While the line is shown nothing can be highlighted, so
↑, ↓, and → do nothing; ← still goes back, and `q`, Esc, and ctrl+c still
quit. Quitting leaves the alternate screen and
restores the terminal, so nothing agent-monitor drew is left on the normal
screen, and agent-monitor exits 0 with nothing on stdout or stderr. A write
to the terminal that fails ends it with `agent-monitor: write error:
<reason>` on stderr and exit 1. The browser only reads, as `list`, `tree`,
and `chat` do: it changes nothing, takes no lock, writes no file, and never
reads a `*.key` file. In the Output blocks each screen is shown as the
developer sees it, with no escape bytes; a sentence under each block names
the highlighted row and, where it matters, the colours. Where a story shows
no screen, a line beside the `Output:` label says what there is to see.
`Keys:` stands where `Options:` would, listing the keys the story presses. The rows between the
body and the key-hint line are shown as one empty line, however many there
are; when the body fills the screen, the key-hint line follows it directly.
Unless a story says otherwise, `HOME` is `/home/dev`, stdin and stdout are
the same terminal of 120 columns by 40 rows, `TERM` is `xterm-256color`, and
`NO_COLOR` is not set.

## A developer opens agent-monitor in a terminal

The first screen is the harness menu: `Claude Code`, `Codex`, and `Grok`,
always all three and always in that order, each followed by how many live
root sessions it has, the rows `agent-monitor list <harness>` would print. The
names are padded to the widest and the count follows after two spaces. A
harness whose data cannot be read, where `list` would fail with `cannot
read`, shows `-` as its count, and keeps showing it for as long as its latest
read has failed. The highlight starts on the first row. The
counts are kept up to date. Here Claude Code has the two live sessions of the
story in which a developer lists the live Claude Code sessions, Codex has
never run on this machine, and Grok's index is not valid JSON.

Command:

```
$ agent-monitor
```

Keys:

- `q`: quit.

Output:

```
agent-monitor

Claude Code  2
Codex        0
Grok         -

↑↓ move  → open  q quit
```

The highlighted row is `Claude Code`. Exits 0 when the developer presses
`q`. Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- `HOME` is `/home/dev`; stdin and stdout are a terminal of 120 columns by
  40 rows.
- The Claude Code registrations, processes, and transcripts are exactly
  those of the story in which a developer lists the live Claude Code
  sessions.
- `/home/dev/.codex/thread-writer-locks` does not exist.
- `/home/dev/.grok/active_sessions.json` exists and is not valid JSON.

Postconditions:

- Nothing has changed.
- No lock was taken.
- No file was written.
- No `*.key` file was read.

## A developer moves the highlight through a menu

The highlight moves one row per key press, and stops at the ends: at the first
row ↑ does nothing, and at the last row ↓ does nothing. `k` is ↑ and `j` is
↓. Here the menu is the harness menu of the story in which a developer opens
agent-monitor in a terminal, and the developer presses ↓, ↓, ↓, then `k`,
`k`, `k`.

Command:

```
$ agent-monitor
```

Keys:

- ↓, ↓, ↓: move the highlight down one row each; on the last row, nothing.
- `k`, `k`, `k`: move the highlight up one row each; on the first row,
  nothing.
- `q`: quit.

Output: the screen does not change except for the highlight.

```
agent-monitor

Claude Code  2
Codex        0
Grok         -

↑↓ move  → open  q quit
```

The highlight is on `Codex` after the first ↓, on `Grok` after the second,
and still on `Grok` after the third. It is on `Codex` after the first `k`,
on `Claude Code` after the second, and still on `Claude Code` after the
third. Exits 0 when the developer presses `q`. Nothing is left on stdout;
stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The harnesses' data are exactly those of the story in which a developer
  opens agent-monitor in a terminal.

Postconditions:

- Nothing has changed.

## A developer opens a harness's live sessions

Opening a harness shows its session menu: exactly the harness's live root
sessions, as `list` defines them, each drawn as `list` draws its row, below
`list`'s header row, which cannot be highlighted. The columns are as wide as
`list` makes them for the rows shown. The rows are ordered by when each
session started, oldest first, not by LAST ACTIVE: the start is the moment the
harness recorded the session starting, the `startedAt` of a Claude Code
registration, the `timestamp` inside the `session_meta` record that begins a
Codex rollout, or the `opened_at` of a Grok index entry. A session whose start
is unknown, one whose registration or index entry has none, whose rollout is
not written yet, or whose `session_meta` record has no `timestamp` that can be
read as a time, comes after every session whose start is known, and sessions
that tie are ordered by session id, ascending byte by byte. The highlight
starts on the first session row. Here `list claude` would print session
`7c2e9a41-…` first, for its newer activity, but session `b41f0c77-…` started
earlier, so it comes first.

`/home/dev/.claude/sessions/41822.json`:

```
{"pid":41822,"sessionId":"7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93","cwd":"/home/dev/src/shop","name":"fix Bob's checkout","status":"busy","startedAt":1790201442000}
```

`/home/dev/.claude/sessions/39107.json`:

```
{"pid":39107,"sessionId":"b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14","cwd":"/home/dev/src/blog","status":"idle","startedAt":1790195467000}
```

Command:

```
$ agent-monitor
```

Keys:

- → on `Claude Code`: open the Claude Code session menu.
- `q`: quit.

Output:

```
agent-monitor › Claude Code

SESSION                               STATUS   LAST ACTIVE           CWD                 TITLE
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle     2026-09-23T21:04:55Z  /home/dev/src/blog
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93  working  2026-09-23T23:52:10Z  /home/dev/src/shop  fix Bob's checkout

↑↓ move  → open  ← back  q quit
```

The highlighted row is `b41f0c77-…`'s. Exits 0 when the developer presses
`q`. Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The Claude Code registrations, processes, and transcripts are those of the
  story in which a developer lists the live Claude Code sessions, except that
  the two live registrations are the ones above: session `7c2e9a41-…`
  started at 2026-09-23T22:10:42Z and session `b41f0c77-…` at
  2026-09-23T20:31:07Z.
- The developer opened agent-monitor, and the highlight is on `Claude Code`.

Postconditions:

- Nothing has changed.
- No `*.key` file was read.

## A developer opens a harness with no live sessions

A harness with no live root session, `0` on the harness menu, still opens: its
session menu is `list`'s header row, then the line `no live sessions`. Neither
line can be highlighted, so ↑, ↓, and → do nothing there; ← goes back. When a
session starts, it appears in place of the line.

Command:

```
$ agent-monitor
```

Keys:

- ↓, then → on `Codex`: open the Codex session menu.
- `q`: quit.

Output:

```
agent-monitor › Codex

SESSION  STATUS  LAST ACTIVE  CWD  TITLE
no live sessions

↑↓ move  → open  ← back  q quit
```

No row is highlighted. Exits 0 when the developer presses `q`. Nothing is
left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The harnesses' data are exactly those of the story in which a developer
  opens agent-monitor in a terminal: `/home/dev/.codex/thread-writer-locks`
  does not exist.

Postconditions:

- Nothing has changed.
- No directory was created.

## A developer opens a harness whose data cannot be read

A harness shown with `-` still opens. When the first read of its data fails,
in place of the table its session menu shows the line `cannot read <path>: <reason>`, naming the place that could not
be read as `list`'s diagnostic names it, the path escaped the same way, without
the `agent-monitor: ` prefix. agent-monitor does not exit: it keeps the screen
up to date, and once the place can be read again the sessions are shown in
place of the line. Here Grok's index is not valid JSON.

Command:

```
$ agent-monitor
```

Keys:

- ↓, ↓, then → on `Grok`: open the Grok session menu.
- `q`: quit.

Output:

```
agent-monitor › Grok

cannot read /home/dev/.grok/active_sessions.json: not valid JSON

↑↓ move  → open  ← back  q quit
```

No row is highlighted. Exits 0 when the developer presses `q`. Nothing is
left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The harnesses' data are exactly those of the story in which a developer
  opens agent-monitor in a terminal: `/home/dev/.grok/active_sessions.json`
  exists and is not valid JSON.

Postconditions:

- Nothing has changed.

## A developer opens a session's agent tree

Opening a session shows its agent menu: the session's tree drawn exactly as
`tree` draws it in a terminal, then an empty line and the key. Every line of
the tree is a row of the menu, the root's included, so the root's own chat can
be opened; the key cannot be highlighted. Each dot is coloured by its status as
`tree` colours it, and no status word follows the label. The highlight starts
on the root's line. Here the session menu is that of the story in which a
developer opens a harness's live sessions; the developer moves the highlight
to session `7c2e9a41-…` and opens it.

Command:

```
$ agent-monitor
```

Keys:

- ↓: move the highlight to `7c2e9a41-…`.
- →: open its agent menu.
- `q`: quit.

Output:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93

● [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout
├── ● [a1c4e7f09b2d38561] Find the checkout handler
├── ● [a2d5f8e1c3b049672] Review the payment tests
│   └── ● [a3e6f9d2b4c150783] Run the payment suite
├── ● [a4f7e0c3d5a261894] Profile the cart query
└── ● [a5b8c1f4e6d372905] Draft the refund fix

● working (3)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)

↑↓ move  → open  ← back  q quit
```

The highlighted row is the root's. The dots of the root, `a2d5f8e1…`, and
`a5b8c1f4…` are cyan, `a1c4e7f0…`'s green, `a3e6f9d2…`'s red, and
`a4f7e0c3…`'s yellow; the key's dots are, in order, cyan, blue, green, yellow,
red, magenta, and gray. Exits 0 when the developer presses `q`. Nothing is
left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The Claude Code sessions are those of the story in which a developer opens
  a harness's live sessions; the developer has opened that session menu, and
  the highlight is on `b41f0c77-…`.
- Session `7c2e9a41-…`'s transcript and subagents are exactly those of the
  story in which a developer draws the subagent tree of a Claude Code
  session.

Postconditions:

- Nothing has changed.

## A developer opens a session that started no subagents

A session with no subagents still opens to its agent menu: the root's line
alone, then the key. The root's line is the one row, so → opens the root's
chat. Here the session has no title, so its line has no label.

Command:

```
$ agent-monitor
```

Keys:

- → on `b41f0c77-…`: open its agent menu.
- `q`: quit.

Output:

```
agent-monitor › Claude Code › b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14

● [b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14]

● working (0)  ● idle (1)  ● done (0)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (0)

↑↓ move  → open  ← back  q quit
```

The highlighted row is the root's. Its dot is blue; the key's dots are
coloured as in the story in which a developer opens a session's agent tree.
Exits 0 when the developer presses `q`. Nothing is left on stdout; stderr is
empty.

Preconditions:

- `bin/agent-monitor` exists.
- The Claude Code sessions are those of the story in which a developer opens
  a harness's live sessions; the developer has opened that session menu, and
  the highlight is on `b41f0c77-…`.
- Session `b41f0c77-…`'s transcript is that of the story in which a
  developer draws the tree of a session that started no subagents.

Postconditions:

- Nothing has changed.

## A developer opens a session's own chat

Opening a row of the agent menu shows that agent's chat: its entries exactly
as `chat` prints them, then the totals line, which is the last line of the
body and stays in view however the chat is scrolled. The chat opens scrolled to
its end and follows it: while its end is in view, each new entry is shown as
it is recorded, and the totals line is kept up to date. A line longer than the
terminal is wide wraps: it is cut into pieces exactly as wide as the
terminal, character by character, never at a word boundary, each on a row of
its own. → and Enter do nothing on the chat.
Here the agent is the root of a Grok session, opened from its root line, so
the breadcrumb ends with the session id twice; the arguments of its `write`
call are wider than the terminal and wrap.

Command:

```
$ agent-monitor
```

Keys:

- ↓, ↓, → on `Grok`: open the Grok session menu, highlight on `01a0c4f2-…`.
- →: open the session's agent menu, highlight on the root.
- →: open the root's chat.
- `q`: quit.

Output:

```
agent-monitor › Grok › 01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47 › 01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47

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
{"file_path":"/home/dev/src/site/auth/cookie.go","content":"package auth\n\nimport \"net/http\"\n\n// sessionCookie is s
et for the parent domain so the redirect back from sign-in carries it.\nfunc ses…

2026-09-24T21:10:15Z result ok

2026-09-24T21:10:19Z assistant
The cookie was scoped to the login host only, so the site never saw it.
It is now set for the parent domain.

tokens: in 18420  cache-write 0  cache-read 36864  out 912  reasoning 388  calls 3

↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]
```

The `write` call's arguments are one line of the chat, wrapped after its
120th character. Exits 0 when the developer presses `q`. Nothing is left on
stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- Session `01a0c4f2-…` is Grok's only live session; it and its subagents
  are those of the story in which a developer draws the subagent tree of a
  Grok session, and its `updates.jsonl` is that of the story in which a
  developer reads a Grok session's chat.
- The Claude Code and Codex data are those of the story in which a developer
  opens agent-monitor in a terminal.

Postconditions:

- Nothing has changed.

## A developer opens a subagent's chat

Any line of the tree opens that agent's chat, a subagent at any depth
included; the breadcrumb ends with its agent id.

Command:

```
$ agent-monitor
```

Keys:

- ↓: on the agent menu, move the highlight from the root to `a1c4e7f0…`.
- →: open its chat.
- `q`: quit.

Output:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 › a1c4e7f09b2d38561

2026-09-24T20:01:14Z agent
Find where checkout requests are handled.
Report the file and the function.

2026-09-24T20:01:16Z tool Grep
{"pattern":"func .*Checkout","path":"/home/dev/src/shop"}

2026-09-24T20:01:16Z result ok

2026-09-24T20:01:19Z assistant
The handler is HandleCheckout in internal/cart/checkout.go, line 42.

tokens: in 4  cache-write 4370  cache-read 23950  out 95  reasoning 0  calls 2

↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]
```

Exits 0 when the developer presses `q`. Nothing is left on stdout; stderr is
empty.

Preconditions:

- `bin/agent-monitor` exists.
- The developer has opened session `7c2e9a41-…`'s agent menu as in the story
  in which a developer opens a session's agent tree, and the highlight is on
  the root.
- The transcript of subagent `a1c4e7f0…` is exactly that of the story in
  which a developer reads a Claude Code subagent's chat.

Postconditions:

- Nothing has changed.

## A developer scrolls back through a chat while the agent writes

A developer reads back through a chat while the agent keeps working. ↑ or `k`
scrolls up one line and ↓ or `j` down one; PgUp or ctrl+b scrolls up one page
and PgDn or ctrl+f down one; `g` goes to the top and `G` to the end. Scrolling
up pauses the chat: the view holds, the hint reads `[paused]`, and new entries
collect below it, out of view. `G`, or scrolling down until the end is in view,
resumes: the hint reads `[following]` again and new entries come into view as
they are recorded. The totals line is not part of what scrolls: while the chat
is paused it is still kept up to date, and only the entries hold still. Here the terminal has 9 rows, so the chat shows five lines
above its totals line. The agent is the Claude Code subagent that is still
writing, in the story in which a developer reads the chat of an agent that is
still writing; while the developer reads, it finishes its half-written line
and then records a tool call, as in the story in which a developer follows a
chat whose output is not a terminal.

Command:

```
$ agent-monitor
```

Keys:

- `g`: scroll to the top, pausing the chat.
- `G`: scroll to the end and follow it again.
- `q`: quit.

Output: when the chat opens, at its end:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 › a5b8c1f4e6d372905

Draft a fix for refunds that round down.

2026-09-24T20:05:42Z assistant
I'll start from the refund calculation.

tokens: in 3  cache-write 3980  cache-read 9870  out 22  reasoning 0  calls 1
↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]
```

After pressing `g`:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 › a5b8c1f4e6d372905

2026-09-24T20:05:40Z agent
Draft a fix for refunds that round down.

2026-09-24T20:05:42Z assistant
I'll start from the refund calculation.
tokens: in 3  cache-write 3980  cache-read 9870  out 22  reasoning 0  calls 1
↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [paused]
```

The half-written line is then completed; its entry is not brought into view,
while the totals line changes to that of the next screen.
After pressing `G`:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 › a5b8c1f4e6d372905

I'll start from the refund calculation.

2026-09-24T20:05:45Z assistant
Rounding happens in refundAmount in internal/cart/refund.go.

tokens: in 4  cache-write 4112  cache-read 23720  out 61  reasoning 0  calls 2
↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]
```

After the tool call is recorded:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 › a5b8c1f4e6d372905

Rounding happens in refundAmount in internal/cart/refund.go.

2026-09-24T20:05:47Z tool Read
{"file_path":"/home/dev/src/shop/internal/cart/refund.go"}

tokens: in 5  cache-write 4200  cache-read 37702  out 106  reasoning 0  calls 3
↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]
```

Exits 0 when the developer presses `q`. Nothing is left on stdout; stderr is
empty.

Preconditions:

- `bin/agent-monitor` exists.
- stdin and stdout are a terminal of 120 columns by 9 rows.
- The session, the subagent, and its transcript start exactly as in the story
  in which a developer reads the chat of an agent that is still writing, and
  the developer has opened the chat of `a5b8c1f4…` from session
  `7c2e9a41-…`'s agent menu.
- After the developer presses `g`, the subagent completes the half-written
  line as the first record of the story in which a developer follows a chat
  whose output is not a terminal; after the developer presses `G`, it records
  the second.

Postconditions:

- Nothing has changed.
- No file was written.

## A developer goes back up a level

← goes back one level and puts the highlight on the item the developer came
from: the agent whose chat was open, the session whose tree was open, the
harness whose sessions were open. On the harness menu ← does nothing. Here the
developer has opened subagent `a1c4e7f0…`'s chat as in the story in which a
developer opens a subagent's chat, and presses ← four times.

Command:

```
$ agent-monitor
```

Keys:

- ←: go back one level; on the harness menu, nothing.
- `q`: quit.

Output: after the first ←:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93

● [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout
├── ● [a1c4e7f09b2d38561] Find the checkout handler
├── ● [a2d5f8e1c3b049672] Review the payment tests
│   └── ● [a3e6f9d2b4c150783] Run the payment suite
├── ● [a4f7e0c3d5a261894] Profile the cart query
└── ● [a5b8c1f4e6d372905] Draft the refund fix

● working (3)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)

↑↓ move  → open  ← back  q quit
```

The highlighted row is `a1c4e7f0…`'s. After the second ←:

```
agent-monitor › Claude Code

SESSION                               STATUS   LAST ACTIVE           CWD                 TITLE
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle     2026-09-23T21:04:55Z  /home/dev/src/blog
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93  working  2026-09-23T23:52:10Z  /home/dev/src/shop  fix Bob's checkout

↑↓ move  → open  ← back  q quit
```

The highlighted row is `7c2e9a41-…`'s. After the third ←, and again after the
fourth:

```
agent-monitor

Claude Code  2
Codex        0
Grok         -

↑↓ move  → open  q quit
```

The highlighted row is `Claude Code`. Exits 0 when the developer presses `q`.
Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The harnesses' data are those of the story in which a developer opens a
  subagent's chat, and the developer has opened `a1c4e7f0…`'s chat as that
  story does.
- The Codex and Grok data are those of the story in which a developer opens
  agent-monitor in a terminal.

Postconditions:

- Nothing has changed.

## A developer quits

`q`, Esc, and ctrl+c quit from every screen, the chat included. agent-monitor
leaves the alternate screen and restores the terminal as it found it: the
normal screen shows what it showed before agent-monitor started, and nothing
agent-monitor drew is left on it. Here the developer quits from the chat of
the story in which a developer opens a subagent's chat.

Command:

```
$ agent-monitor
```

Keys:

- `q`, Esc, or ctrl+c: quit.

Output: nothing agent-monitor drew; once it has exited, the terminal shows
what it showed before, and the prompt follows.

Exits 0 when the developer presses `q`, Esc, or ctrl+c. Nothing is left on
stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The developer has opened `a1c4e7f0…`'s chat as in the story in which a
  developer opens a subagent's chat.

Postconditions:

- Nothing has changed.
- No lock was taken.
- No file was written.
- No `*.key` file was read.
- The terminal is as it was before agent-monitor started.

## A developer watches a new session appear

The session menu is kept up to date, and its rows keep their order: a session
that appears is put at its place by start time, and rows never move because of
activity. A session that has just started comes after every other, since it
started last; a Codex thread that is loaded again keeps the start its rollout
recorded, so it can appear above others. The highlight stays on its session
wherever the session's row moves. Here the Codex session menu shows two
threads, the developer moves the highlight to `01a0d0ab-…`, and then thread
`01a09e55-…`, which started days before both, is loaded again; after that
thread `01a0c1f2-…` starts a turn.

Command:

```
$ agent-monitor
```

Keys:

- ↓, → on `Codex`: open the Codex session menu.
- ↓: move the highlight to `01a0d0ab-…`.
- `q`: quit.

Output: before thread `01a09e55-…` is loaded:

```
agent-monitor › Codex

SESSION                               STATUS   LAST ACTIVE           CWD                  TITLE
01a0c1f2-7710-7a3e-8e02-5b1d2c9a0f13  idle     2026-09-23T19:15:30Z  /home/dev/src/infra
01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4  working  2026-09-23T23:40:02Z  /home/dev/src/api    weather check

↑↓ move  → open  ← back  q quit
```

The highlighted row is `01a0d0ab-…`'s. After thread `01a09e55-…` is loaded
and thread `01a0c1f2-…` starts a turn:

```
agent-monitor › Codex

SESSION                               STATUS   LAST ACTIVE           CWD                  TITLE
01a09e55-8b3d-7c61-9f04-2e7a5c1b8d96  idle     2026-09-23T23:44:18Z  /home/dev/src/db     migrate the schema
01a0c1f2-7710-7a3e-8e02-5b1d2c9a0f13  working  2026-09-23T23:45:06Z  /home/dev/src/infra
01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4  working  2026-09-23T23:40:02Z  /home/dev/src/api    weather check

↑↓ move  → open  ← back  q quit
```

The highlighted row is still `01a0d0ab-…`'s, now the third. Exits 0 when the
developer presses `q`. Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- At the start, the Codex threads, locks, and rollouts are exactly those of
  the story in which a developer lists the live Codex sessions.
- The `session_meta` record that begins each rollout records the thread's
  start: `2026-09-14T05:13:21Z` for `01a09e55-…`, `2026-09-21T03:11:28Z` for
  `01a0c1f2-…`, and `2026-09-23T21:02:37Z` for `01a0d0ab-…`.
- While the menu is shown, a running Codex process takes the lock on
  `/home/dev/.codex/thread-writer-locks/01a09e55-8b3d-7c61-9f04-2e7a5c1b8d96.lock`;
  thread `01a09e55-…` is a root with `cwd` `/home/dev/src/db`, its
  `thread_name` in `/home/dev/.codex/session_index.jsonl` is
  `migrate the schema`, its last such record is `task_complete`, and the
  latest `timestamp` in its rollout is 2026-09-23T23:44:18Z. Then the
  rollout of `01a0c1f2-…` records `task_started` at 2026-09-23T23:45:06Z.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer's highlighted session ends

A session that ends drops out of the session menu at the next refresh, as it
drops out of `list -f`'s view: within a couple of seconds of its process
exiting. When the highlighted session drops
out, the highlight moves to the row that took its place, or, when it was the
last row, to the new last row. When the last session drops out, the menu is
the header and `no live sessions`. Here the Claude Code session menu shows
three sessions, the highlight is on the middle one, `7c2e9a41-…`, and its
process exits; then the process of `d2a8f613-…` exits.

`/home/dev/.claude/sessions/42310.json`:

```
{"pid":42310,"sessionId":"d2a8f613-5c07-4e9b-a1d4-8f3e6b0c7a52","cwd":"/home/dev/src/docs","status":"idle","startedAt":1790207702000}
```

Command:

```
$ agent-monitor
```

Keys:

- → on `Claude Code`: open the Claude Code session menu.
- ↓: move the highlight to `7c2e9a41-…`.
- `q`: quit.

Output: at the start:

```
agent-monitor › Claude Code

SESSION                               STATUS   LAST ACTIVE           CWD                 TITLE
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle     2026-09-23T21:04:55Z  /home/dev/src/blog
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93  working  2026-09-23T23:52:10Z  /home/dev/src/shop  fix Bob's checkout
d2a8f613-5c07-4e9b-a1d4-8f3e6b0c7a52  idle     -                     /home/dev/src/docs

↑↓ move  → open  ← back  q quit
```

The highlighted row is `7c2e9a41-…`'s. After process 41822 exits:

```
agent-monitor › Claude Code

SESSION                               STATUS  LAST ACTIVE           CWD                 TITLE
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle    2026-09-23T21:04:55Z  /home/dev/src/blog
d2a8f613-5c07-4e9b-a1d4-8f3e6b0c7a52  idle    -                     /home/dev/src/docs

↑↓ move  → open  ← back  q quit
```

The highlighted row is `d2a8f613-…`'s, the row that took its place. After
process 42310 exits:

```
agent-monitor › Claude Code

SESSION                               STATUS  LAST ACTIVE           CWD                 TITLE
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle    2026-09-23T21:04:55Z  /home/dev/src/blog

↑↓ move  → open  ← back  q quit
```

The highlighted row is `b41f0c77-…`'s, the new last row. Exits 0 when the
developer presses `q`. Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The Claude Code registrations are those of the story in which a developer
  opens a harness's live sessions, and the registration above; process 42310
  is running and started no later than its session, and session
  `d2a8f613-…`'s transcript does not exist.
- While the menu is shown, process 41822 exits; then process 42310 exits.

Postconditions:

- Nothing has changed.
- No `*.key` file was read.

## A developer watches a new subagent appear in the tree

The agent menu is kept up to date as `tree -f` keeps its view. A subagent
that starts appears under the agent that started it, at its place among its
siblings, not at the bottom of the screen; a line whose status changes stays
where it is, and only its dot changes. The highlight stays on its agent
wherever its line moves. Here the developer has moved the highlight to
`a4f7e0c3…`; then subagent `a2d5f8e1…` starts a subagent of its own.

`agent-a9c3d6e9f1a4b7250.meta.json`, with its other keys left out:

```
{"agentType":"general-purpose","description":"Rerun the failing payment test","parentAgentId":"a2d5f8e1c3b049672"}
```

Command:

```
$ agent-monitor
```

Keys:

- ↓, ↓, ↓, ↓: on the agent menu, move the highlight from the root to
  `a4f7e0c3…`.
- `q`: quit.

Output: after the subagent starts:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93

● [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout
├── ● [a1c4e7f09b2d38561] Find the checkout handler
├── ● [a2d5f8e1c3b049672] Review the payment tests
│   ├── ● [a3e6f9d2b4c150783] Run the payment suite
│   └── ● [a9c3d6e9f1a4b7250] Rerun the failing payment test
├── ● [a4f7e0c3d5a261894] Profile the cart query
└── ● [a5b8c1f4e6d372905] Draft the refund fix

● working (4)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)

↑↓ move  → open  ← back  q quit
```

The highlighted row is still `a4f7e0c3…`'s, now the sixth. The new line's dot
is cyan; the others are coloured as in the story in which a developer opens a
session's agent tree. Exits 0 when the developer presses `q`. Nothing is left
on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- At the start, the session and its subagents are exactly those of the story
  in which a developer opens a session's agent tree, and the developer has
  opened its agent menu as that story does.
- While the menu is shown, the meta file above is written in the session's
  `subagents/` directory beside the subagent's transcript, `a2d5f8e1…` having
  started `a9c3d6e9…` after `a3e6f9d2…`; the transcript of `a2d5f8e1…`
  records no notification or result for `a9c3d6e9…`.

Postconditions:

- Nothing has changed.

## A developer's session ends while its tree is open

When the session whose agent menu is open ends, the screen stays: its root's
status becomes `ended`, and its subagents take the statuses `tree` gives the
subagents of a session that has ended. Nothing moves; only the dots and the
key change. Here the process of session `7c2e9a41-…` exits while its tree is
open; `a2d5f8e1…`, with no result recorded, and `a5b8c1f4…`, sent a message
after its latest notification, become `unknown`. The root takes its title
from its transcript, as `tree` takes that of a session that has ended.

Command:

```
$ agent-monitor
```

Keys:

- `q`: quit.

Output: after the process exits:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93

● [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout
├── ● [a1c4e7f09b2d38561] Find the checkout handler
├── ● [a2d5f8e1c3b049672] Review the payment tests
│   └── ● [a3e6f9d2b4c150783] Run the payment suite
├── ● [a4f7e0c3d5a261894] Profile the cart query
└── ● [a5b8c1f4e6d372905] Draft the refund fix

● working (0)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (1)  ● unknown (2)

↑↓ move  → open  ← back  q quit
```

The highlighted row is the root's. The root's dot is now magenta, and those
of `a2d5f8e1…` and `a5b8c1f4…` gray; the others are unchanged. Exits 0 when
the developer presses `q`. Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- At the start, the session and its subagents are exactly those of the story
  in which a developer opens a session's agent tree, and the developer has
  opened its agent menu as that story does.
- The session's transcript records a `custom-title` record whose
  `customTitle` is `fix Bob's checkout`.
- While the menu is shown, process 41822 exits. No notification, result, or
  `SendMessage` tool call is recorded.

Postconditions:

- Nothing has changed.

## A developer views a tree without colour

With `NO_COLOR` set to a non-empty value, or `TERM` exactly `dumb`, the agent
menu has no colour: every dot is plain and each line of the tree ends with two
spaces and its status word, as `tree` prints it without colour. Nothing else
changes: the browser opens on a `TERM` of `dumb` all the same, and the
highlight is still reverse video. Here the
session is the Codex session drawn by `tree`; in the Codex session menu it is
the second row, since thread `01a0c1f2-…` started first.

Command:

```
$ NO_COLOR=1 agent-monitor
```

```
$ TERM=dumb agent-monitor
```

Keys:

- ↓, → on `Codex`: open the Codex session menu.
- ↓, →: open thread `01a0d0ab-…`'s agent menu.
- `q`: quit.

Output:

```
agent-monitor › Codex › 01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4

● [01a0d0ab-ed24-72a0-9cca-1c9f54a9dec4] weather check  working
├── ● [01a0d7c3-5e19-7f42-b0a8-4c6e1d9f2a07] forecast  done
│   └── ● [01a0d9a2-4c7d-7e15-8f60-2b9d3e1c5a74] radar  killed
└── ● [01a0d8f1-0b6e-7d24-9c3a-5e7f1a2b4c68] alerts  working

● working (2)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (0)  ● ended (0)  ● unknown (0)

↑↓ move  → open  ← back  q quit
```

The highlighted row is the root's. Exits 0 when the developer presses `q`.
Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- In the first form `NO_COLOR` is `1` and `TERM` is `xterm-256color`; in the
  second `NO_COLOR` is not set and `TERM` is `dumb`.
- The Codex threads are those of the story in which a developer watches a new
  session appear, before thread `01a09e55-…` is loaded, and thread
  `01a0d0ab-…` and its subagents are those of the story in which a developer
  draws the subagent tree of a Codex session.

Postconditions:

- Nothing has changed.
- No lock was taken.

## A developer browses a menu taller than the terminal

When a menu has more rows than the screen has room for, its rows scroll by
the smallest amount that keeps the highlight in view: moving within the rows
in view scrolls nothing, and moving past the last row in view scrolls by one
row. Only the rows that can be highlighted scroll: the session menu's header
row stays fixed at the top of the body, and the agent menu's empty line and
key stay fixed at its bottom, so a tall session list or tree scrolls between
them. Here the terminal has 5 rows,
so the harness menu shows two of its three rows; the developer presses ↓
twice.

Command:

```
$ agent-monitor
```

Keys:

- ↓, ↓: move the highlight down two rows.
- `q`: quit.

Output: at the start:

```
agent-monitor

Claude Code  2
Codex        0
↑↓ move  → open  q quit
```

The highlighted row is `Claude Code`. After the first ↓ the screen is the
same, with the highlight on `Codex`. After the second:

```
agent-monitor

Codex        0
Grok         -
↑↓ move  → open  q quit
```

The highlighted row is `Grok`. Exits 0 when the developer presses `q`.
Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- stdin and stdout are a terminal of 120 columns by 5 rows.
- The harnesses' data are exactly those of the story in which a developer
  opens agent-monitor in a terminal.

Postconditions:

- Nothing has changed.

## A developer's session data becomes unreadable while it is open

A screen that has been drawn stays as it was when what it shows can no longer
be read: the session menu, the agent menu, and the chat alike keep their last
view and their highlight, and every refresh reads again, as following does.
Once the read succeeds, the screen is up to date again. The harness menu shows
`-` for the harness while its latest read fails. Here the Claude Code session
menu is open when the registry directory stops being readable.

Command:

```
$ agent-monitor
```

Keys:

- ←: go back to the harness menu.
- `q`: quit.

Output: after `/home/dev/.claude/sessions` stops being readable:

```
agent-monitor › Claude Code

SESSION                               STATUS   LAST ACTIVE           CWD                 TITLE
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle     2026-09-23T21:04:55Z  /home/dev/src/blog
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93  working  2026-09-23T23:52:10Z  /home/dev/src/shop  fix Bob's checkout

↑↓ move  → open  ← back  q quit
```

The screen is the one drawn before the failure, and the highlighted row is
still `7c2e9a41-…`'s. After ←, the harness menu shows `Claude Code  -`, with
the highlight on `Claude Code`. Exits 0 when the developer presses `q`.
Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The Claude Code sessions are those of the story in which a developer opens
  a harness's live sessions; the developer has opened that session menu and
  moved the highlight to `7c2e9a41-…`.
- While the menu is shown, `/home/dev/.claude/sessions` stops being readable
  by the developer, and stays so.

Postconditions:

- Nothing has changed.

## A developer opens an agent whose transcript cannot be read

When the first read for a chat fails, the chat's body is one line saying why,
in place of the entries and the totals line, and the screen keeps refreshing:
once the transcript can be read, the chat is shown in its place. The line is
a line of the chat, so it wraps as any other. The agent menu fails the same
way when the session's data cannot be read or the session is no longer found.
Here the transcript of subagent `a2d5f8e1…` is not readable by the developer.

Command:

```
$ agent-monitor
```

Keys:

- ↓, ↓: on the agent menu, move the highlight from the root to `a2d5f8e1…`.
- →: open its chat.
- `q`: quit.

Output:

```
agent-monitor › Claude Code › 7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93 › a2d5f8e1c3b049672

cannot read /home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c
3b049672.jsonl: permission denied

↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]
```

Exits 0 when the developer presses `q`. Nothing is left on stdout; stderr is
empty.

Preconditions:

- `bin/agent-monitor` exists.
- Session `7c2e9a41-…` and its subagents are those of the story in which a
  developer opens a session's agent tree, and the developer has opened its
  agent menu as that story does.
- `agent-a2d5f8e1c3b049672.jsonl` in the session's `subagents/` directory
  exists and is not readable by the developer, as in the story in which a
  developer names an agent whose transcript cannot be read; its meta file is
  readable.

Postconditions:

- Nothing has changed.

## A developer resizes the terminal

When the terminal changes size, the screen is redrawn for the new size at
once. In a menu a row wider than the terminal is cut at its edge; in the chat
the lines are wrapped again to the new width. Here the developer narrows the
terminal to 80 columns while the Claude Code session menu is shown.

Command:

```
$ agent-monitor
```

Keys:

- `q`: quit.

Output: after the terminal is narrowed:

```
agent-monitor › Claude Code

SESSION                               STATUS   LAST ACTIVE           CWD
b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle     2026-09-23T21:04:55Z  /home/dev/s
7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93  working  2026-09-23T23:52:10Z  /home/dev/s

↑↓ move  → open  ← back  q quit
```

The highlighted row is `b41f0c77-…`'s, as before. Exits 0 when the developer
presses `q`. Nothing is left on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.
- The Claude Code sessions are those of the story in which a developer opens
  a harness's live sessions, and the developer has opened that session menu.
- While the menu is shown, the terminal is resized from 120 columns by 40
  rows to 80 columns by 40 rows.

Postconditions:

- Nothing has changed.

## A developer runs agent-monitor with HOME not set

Every place the browser reads is under `$HOME`. When `HOME` is unset or empty,
agent-monitor fails as `list` does, before the screen opens: the alternate
screen is never entered.

Command:

```
$ env -u HOME agent-monitor
```

```
$ HOME= agent-monitor
```

Output:

```
agent-monitor: cannot find the home directory: HOME is not set
```

Exits 3. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- stdin and stdout are a terminal.

Postconditions:

- Nothing has changed.
- No session data was read.

## A developer's terminal cannot be written

When a write to the terminal fails, agent-monitor stops and fails, whichever
screen it was showing. `<reason>` is the system's description of the failure
and varies. Here stderr goes to a file, and the terminal stops taking writes
while the harness menu is shown.

Command:

```
$ agent-monitor 2> err.log
```

Output, in `err.log`:

```
agent-monitor: write error: <reason>
```

Exits 1. The line is on stderr; stdout carries only the screens drawn before
the failure.

Preconditions:

- `bin/agent-monitor` exists.
- stdin and stdout are a terminal; stderr is the file `err.log`.
- While agent-monitor runs, the terminal refuses writes.

Postconditions:

- Nothing has changed, besides `err.log`, which the shell wrote.
