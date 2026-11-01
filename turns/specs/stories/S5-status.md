# Stories — status

Asking turns where it stands: which space it pushes to, whether it has a token, when it last pushed, and, per tool, how many sessions and turns are on this machine against how many the space holds. The report is product and goes to stdout whole, bad news included: six lines in a fixed order, `space:`, `token:`, `last push:`, then one line each for `claude`, `codex` and `grok`, always all three. `space:` names the space turns would push to, the one in `$XDG_CONFIG_HOME/turns/config` or else the host's default, or `none`; `token:` says `set` or `none` and never shows the token. `last push:` is the moment, in UTC as `YYYY-MM-DDTHH:MM:SSZ`, held in `$XDG_STATE_HOME/turns/last-push` (`~/.local/state/turns/last-push` by default), which each successful `turns push` writes with the moment that run started, and from which the next push decides which session files have changed; status only reads it. The local counts come from reading every session file, those `turns push --all` reads, whatever `last-push` holds, and status never changes them; the space's counts come from asking the space, through the gateway at `https://mcp.<space>/mcp` with the stored token, which turns it already holds, the same question `turns push` and `turns push --dry-run` ask. They count the sessions on this machine that the space holds, and of each such session the turns up to the latest one the space holds. A count of one says `session` or `turn`. status pushes nothing. The top-level usage gains the line `  status    show what is here and what the space holds` under `Commands:`.

## A person asks what `status` can do

Command:

```
$ turns status --help
```

```
$ turns status -h
```

Output:

```
Usage: turns status

Print the space and token turns uses, when it last pushed, and per tool how
many sessions and turns are on this machine and how many the space holds.
Pushes nothing.

Options:
  -h, --help   print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person checks how far turns has got

A person whose timer has been running wants to see that pushes are landing and how much is still to go. The difference between the two counts on a line is what `turns push --all` would send, such as a session still running that has grown since the last push; a plain `turns push` sends only the part of it in files changed since the last push started.

Command:

```
$ turns status
```

Output:

```
space: us.ikigenba.dev
token: set
last push: 2026-11-01T14:05:00Z
claude: 827 sessions, 51234 turns here; 820 sessions, 51000 turns on the space
codex: 475 sessions, 9120 turns here; 475 sessions, 9120 turns on the space
grok: 1 session, 12 turns here; 1 session, 12 turns on the space
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `~/.config/turns/config` names `us.ikigenba.dev` and `~/.config/turns/token` holds a token the space accepts.
- `~/.local/state/turns/last-push` holds `2026-11-01T14:05:00Z`.
- The machine's session files hold the counts shown as `here`, and of those sessions the space holds the counts shown as `on the space`.

Postconditions:

- Nothing has changed. No session file was changed and nothing was pushed.

## A person checks status before the first push

A person who has just configured turns, before the timer's first run, sees that nothing has been pushed yet.

Command:

```
$ turns status
```

Output:

```
space: us.ikigenba.dev
token: set
last push: never
claude: 827 sessions, 51234 turns here; 0 sessions, 0 turns on the space
codex: 475 sessions, 9120 turns here; 0 sessions, 0 turns on the space
grok: 0 sessions, 0 turns here; 0 sessions, 0 turns on the space
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- turns is configured as in the previous story.
- `~/.local/state/turns/last-push` does not exist.
- The space holds none of this machine's sessions; `~/.grok/sessions/` does not exist.

Postconditions:

- Nothing has changed. `last-push` was not created.

## A person checks status before configuring turns

With no space or no token there is nothing to ask, so the space's half of each line is left off and the report still succeeds: what is on the machine is worth knowing on its own. A host account whose space comes from the host but which has stored no token shows its space and `token: none`.

Command:

```
$ turns status
```

Output:

```
space: none
token: none
last push: never
claude: 827 sessions, 51234 turns here
codex: 475 sessions, 9120 turns here
grok: 0 sessions, 0 turns here
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `~/.config/turns/` does not exist, and `/etc/xdg/turns/config` does not exist.
- `~/.local/state/turns/last-push` does not exist.

Postconditions:

- Nothing has changed. No request was made to any space.

## A person checks status when the space cannot be reached

The gateway is unreachable, answers with an HTTP error, rejects the token, or the read itself fails. The local half of each line is still known, so the report prints whole with the space's half `unknown`, and stderr says why.

Command:

```
$ turns status
```

Output:

```
space: us.ikigenba.dev
token: set
last push: 2026-11-01T14:05:00Z
claude: 827 sessions, 51234 turns here; unknown on the space
codex: 475 sessions, 9120 turns here; unknown on the space
grok: 1 session, 12 turns here; unknown on the space
```

```
turns: could not read the space: the space refused the token

> <the gateway's answer, each line as it came, with the token, where it appears, shown as `<token>`>
```

Exits 1. The report is on stdout; the diagnostic is on stderr, the gateway's answer quoted after an empty line. The diagnostic has the form `turns push` uses: `turns: <what failed>: <reason>`, then after one empty line the detail, unprefixed when turns writes it and quoted with `> ` when it is the gateway's HTTP answer. A gateway that cannot be reached at all says `turns: could not read the space: cannot reach https://mcp.us.ikigenba.dev/mcp`, and its detail is `<the network error>`, unprefixed.

Preconditions:

- turns is configured with `us.ikigenba.dev` and a token, and `last-push` holds `2026-11-01T14:05:00Z`.
- The gateway at `https://mcp.us.ikigenba.dev/mcp` refuses the token with 401.

Postconditions:

- Nothing has changed.
