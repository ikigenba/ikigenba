# Stories — token

A sandbox holds one bearer token: the credential an agent working in the worktree sends as `Authorization: Bearer <token>` to reach the sandbox's apps, `/mcp` included, through auth's check. A human signs in at the sandbox's auth app, creates the token there, and hands it over; the sandbox only keeps it. A token is one line beginning `ikp_` with no whitespace, such as `ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3`. It lives at `<data>/token`, mode 0600, in the sandbox's data directory; for the sandbox `wip` of the worktree `/home/me/src/ikigenba/wip` that is `/home/me/.local/state/ikigenba/sandbox/wip/token`. `token set` stores it, `token` prints it. Both act on this worktree's sandbox, found from the current directory as `up` finds it; the sandbox must be known to the registry, but it may be up or down. The token survives `down` and is deleted by `wipe`. `token set` takes the sandbox's lock: a second command that changes the same sandbox waits for the first to finish, then runs.

## An agent asks what `token` does

Both commands print the same help.

Command:

```
$ sandbox token --help
```

```
$ sandbox token -h
```

```
$ sandbox token set --help
```

Output:

```
Usage: sandbox token
       sandbox token set

Print the bearer token stored for this worktree's sandbox, or store one read
from stdin.

Subcommands:
  set  store the bearer token read from stdin, replacing any earlier one

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## An agent stores the token a human gave it

The token arrives on stdin so it never appears on a command line. A single trailing newline, `\n` or `\r\n`, is dropped, so a token echoed or pasted with its line ending stores the same as one written with `printf '%s'`. A token already stored is replaced.

Command:

```
$ printf '%s\n' 'ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3' | sandbox token set
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip` or a directory below it.
- The registry holds `wip`, and the sandbox is up.
- `/home/me/.local/state/ikigenba/sandbox/wip/token` may already hold an earlier token.

Postconditions:

- `/home/me/.local/state/ikigenba/sandbox/wip/token` holds exactly `ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3`, with no newline, and has mode 0600. Any earlier token is gone.
- The running sandbox is untouched; no unit was restarted.

## An agent stores a token while the sandbox is down

A sandbox that is down still has its data directory, so its token can be stored before it comes up.

Command:

```
$ printf '%s' 'ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3' | sandbox token set
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`, and the sandbox is down.

Postconditions:

- `/home/me/.local/state/ikigenba/sandbox/wip/token` holds exactly the token and has mode 0600.
- The sandbox is still down; nothing was started.

## An agent stores a token from empty stdin

Stdin that holds nothing once its trailing newline is dropped holds no token, so a lone newline is refused the same way as nothing at all.

Command:

```
$ sandbox token set < /dev/null
```

```
$ printf '\n' | sandbox token set
```

Output:

```
sandbox: no token on stdin
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`.

Postconditions:

- Nothing has changed. Any token stored earlier is still stored.

## An agent stores something that is not a token

What was read is never printed, since it may be a secret pasted by mistake. More than one line, whitespace inside the line, or a line not beginning `ikp_` are all refused this way.

Command:

```
$ printf 'hunter2\n' | sandbox token set
```

Output:

```
sandbox: stdin does not hold a bearer token

a bearer token is one line beginning 'ikp_'
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`.

Postconditions:

- Nothing has changed. Any token stored earlier is still stored.

## An agent stores a token for a sandbox that was never brought up

Command:

```
$ printf '%s' 'ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3' | sandbox token set
```

Output:

```
sandbox: no sandbox 'wip'

run 'sandbox up' to create it
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds no sandbox named `wip`.

Postconditions:

- Nothing has changed. No data directory was created.

## An agent stores a token outside any checkout

Without a worktree there is no sandbox to store the token in.

Command:

```
$ printf '%s' 'ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3' | sandbox token set
```

Output:

```
sandbox: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.

Postconditions:

- Nothing has changed. Nothing was stored.

## An agent puts the token on the command line

The token is read only from stdin; an argument is refused and is not echoed.

Command:

```
$ sandbox token set ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3
```

Output:

```
sandbox: token set takes no arguments

see 'sandbox token --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed. Nothing was stored.

## An agent reads the stored token

The agent needs the token to call an app. It is printed alone, followed by a newline, so `$(sandbox token)` gives exactly the token.

Command:

```
$ sandbox token
```

Output:

```
ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`, and `/home/me/.local/state/ikigenba/sandbox/wip/token` holds `ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3`.

Postconditions:

- Nothing has changed.

## An agent asks for a token none has stored

The agent cannot create a token itself; only a signed-in human can. The diagnostic tells the agent exactly what to ask for and how to store the answer, naming this sandbox's auth URL. The diagnostic and exit code are the same whether or not the sandbox's last `up` recorded `auth`: the URL is made from the sandbox's name and port.

Command:

```
$ sandbox token
```

Output:

```
sandbox: no token stored for sandbox 'wip'

Ask the human to sign in at http://auth.wip.localhost:7400, create a bearer
token, and store it with: printf '%s' '<token>' | sandbox token set
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip` with port `7400`; no token is stored for it.

Postconditions:

- Nothing has changed.

## An agent asks for the token of a sandbox that was never brought up

Command:

```
$ sandbox token
```

Output:

```
sandbox: no sandbox 'wip'

run 'sandbox up' to create it
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds no sandbox named `wip`.

Postconditions:

- Nothing has changed.

## An agent asks for the token outside any checkout

Command:

```
$ sandbox token
```

Output:

```
sandbox: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.

Postconditions:

- Nothing has changed.

## An agent uses the token in a worktree whose sandbox name belongs to another worktree

Two worktrees with the same basename would be the same sandbox. `token` and `token set` act on the sandbox only when the registry records this worktree as its own; otherwise they refuse as `up` does, whether or not the recorded worktree still exists, and neither reads nor writes that sandbox's token. Both forms print the same text.

Command:

```
$ sandbox token
```

```
$ printf '%s' 'ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3' | sandbox token set
```

Output:

```
sandbox: sandbox 'wip' belongs to another worktree: /home/me/old/wip

rename this worktree, or wipe that sandbox with 'sandbox wipe wip' once it is down
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip` with worktree `/home/me/old/wip`.

Postconditions:

- Nothing has changed. The token of the `wip` sandbox of `/home/me/old/wip`, if any, is neither printed nor replaced.

## An agent mistypes the token subcommand

Command:

```
$ sandbox token bogus
```

Output:

```
sandbox: unknown token subcommand 'bogus'

see 'sandbox token --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## An agent gives `token` an option it does not have

Command:

```
$ sandbox token --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox token --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## An agent reads its token after the sandbox was taken down

`down` keeps the sandbox's data, the token included, so the agent's credential is still there when the sandbox comes back up.

Command:

```
$ sandbox token
```

Output:

```
ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3` was stored for `wip`, and `sandbox down` has since run.

Postconditions:

- Nothing has changed.

## An agent asks for its token after the sandbox was wiped

`wipe` deletes the sandbox's data, the token included. Once the sandbox is brought up again it is known, but holds no token until a human creates a new one.

Command:

```
$ sandbox token
```

Output:

```
sandbox: no token stored for sandbox 'wip'

Ask the human to sign in at http://auth.wip.localhost:7400, create a bearer
token, and store it with: printf '%s' '<token>' | sandbox token set
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- A token was stored for `wip`; then `sandbox down`, `sandbox wipe` and `sandbox up` ran, and the new `up` took port `7400`.

Postconditions:

- Nothing has changed.
