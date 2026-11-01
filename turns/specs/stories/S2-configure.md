# Stories — configure

Telling turns which space to push to and with which token. turns' configuration is the directory `$XDG_CONFIG_HOME/turns/`, `~/.config/turns/` when `XDG_CONFIG_HOME` is unset, holding two files: `config`, one line `space=<space>`, and `token`, the person's personal token, mode `0600`. `turns configure` is their one writer. A space is a bare host name such as `us.ikigenba.dev`; turns reaches it at `https://mcp.<space>/mcp`. A space named in `config` overrides a host's default space. The token is never printed, never written anywhere but `token`, and never checked against the gateway at configure time. On a host the default space is read from `/etc/xdg/turns/config`, mode `0644`, one line `space=<the host's space>`, which opsctl writes at init and activate; turns never writes it. turns reads `$XDG_CONFIG_HOME/turns/config` first and `/etc/xdg/turns/config` only when the first names no space. The top-level usage gains the line `  configure store the space and token turns pushes with` under `Commands:`.

## A person asks what `configure` can do

Command:

```
$ turns configure --help
```

```
$ turns configure -h
```

Output:

```
Usage: turns configure [<space>] [--token <token>]

Store the space turns pushes to and the personal token it pushes with, in
$XDG_CONFIG_HOME/turns/ (~/.config/turns/ by default). The token is read as
one line from stdin unless --token gives it. Running it again replaces both,
or, with the space omitted, only the token. On a host the space may be
omitted; the host's own space is used.

Options:
  --token <token>   take the token from the argument instead of stdin
  -h, --help        print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person stores a space and a token from stdin

A person on a laptop has a personal token from the space's auth app and stores it with the space it belongs to. The token is read from stdin so it never appears on a command line or in shell history: one line, its trailing newline stripped. Nothing is printed; the answer to "did it work" is the exit code.

Command:

```
$ turns configure us.ikigenba.dev < token.txt
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `XDG_CONFIG_HOME` is unset and `/home/me/.config/turns/` does not exist.
- `./token.txt` holds a personal token followed by a newline.

Postconditions:

- `/home/me/.config/turns/config` holds the one line `space=us.ikigenba.dev`.
- `/home/me/.config/turns/token` has mode `0600` and holds the token without the newline read from stdin.
- No request was made to the space; the token was accepted, not checked.

## A person stores a token given as an option

Where stdin is not convenient, such as a provisioning script, `--token` gives the token on the command line instead; stdin is not read.

Command:

```
$ turns configure us.ikigenba.dev --token <token>
```

Options:

- `--token <token>` is the token to store; stdin is not read.

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- `config` holds `space=us.ikigenba.dev` and `token` (mode `0600`) holds `<token>`, as in the previous story.
- No request was made to the space.

## A person configures turns again

A person whose token was revoked, or who moves to another space, runs `configure` again. Both files are replaced whole; nothing of the earlier configuration is kept. With the space omitted, as on a host, only `token` is replaced and `config` is left as it is.

Command:

```
$ turns configure sbx.ikigenba.dev < new-token
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `config` holds `space=us.ikigenba.dev` and `token` holds an older token.
- `new-token` holds a token for `sbx.ikigenba.dev`.

Postconditions:

- `config` holds `space=sbx.ikigenba.dev` and `token` (mode `0600`) holds the new token.
- No session already pushed to `us.ikigenba.dev` was changed or removed there.

## A person configures turns without naming a space

Off a host there is no default space, so the space is required.

Command:

```
$ turns configure < token
```

Output:

```
turns: no space given

see 'turns configure --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `/etc/xdg/turns/config` does not exist.

Postconditions:

- Nothing has changed. stdin was not read.

## A person names something that is not a space

A space is a bare host name: lowercase labels of letters, digits and hyphens joined by dots, with at least one dot. A URL, or a host with a port or a path, is refused; turns does not ask the network whether the space exists.

Command:

```
$ turns configure https://us.ikigenba.dev < token
```

Output:

```
turns: 'https://us.ikigenba.dev' is not a space name
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed. stdin was not read.

## A person gives configure no token

An empty stdin, a stdin whose one line is empty, or `--token ''` gives no token, and turns refuses it rather than store an empty file.

Command:

```
$ turns configure us.ikigenba.dev < /dev/null
```

```
$ turns configure us.ikigenba.dev --token ''
```

Output:

```
turns: no token given
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed. An existing `config` and `token` are as they were.

## A person gives the token option no value

Command:

```
$ turns configure us.ikigenba.dev --token
```

Output:

```
turns: option '--token' needs a value

see 'turns configure --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person on a host stores only a token

On a host the space is the host's own, named in `/etc/xdg/turns/config`, so a person whose account `opsctl user add` created stores only their token, and the timer that has been inert since then starts pushing at its next run.

```
space=us.ikigenba.dev
```

Command:

```
$ turns configure < token
```

```
$ turns configure --token <token>
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `/etc/xdg/turns/config` holds the line above.
- `~/.config/turns/config` does not exist.

Postconditions:

- `~/.config/turns/token` has mode `0600` and holds the token.
- `~/.config/turns/config` was not created, so the host's space is used; an existing `config` naming a space is left as it is and keeps overriding the host's.
