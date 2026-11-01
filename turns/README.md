> [!WARNING]
> This is unsupported AI slop.

# turns

`turns` is a CLI that reads this account's Claude Code, Codex CLI and Grok CLI session files and pushes their turns to the `sessions` service of an Ikigenba space. A turn is content-free: its session, host, working directory, tool, model, time, whose turn it was and token counts. No prompt or answer text leaves the machine except the first slash command's name and argument, and session files are never modified. It never runs as root.

## Installing it

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/turns/install.sh | sh
```

This installs the newest stable release (Linux and macOS, amd64 and arm64) where `go install` would put it: `$GOBIN`, else `$GOPATH/bin`, else `~/go/bin`, so it and `make install` from the checkout never leave two copies on your PATH. Set `TURNS_VERSION=vX.Y.Z` to pin a version, or `BINDIR` to change the destination. On a host turns is already at `/usr/local/bin/turns`.

## Configuring it

```sh
turns configure us.ikigenba.dev < token.txt
```

The token is a personal access token minted at the space's auth app, read as one line from stdin; `--token <token>` gives it as an argument instead. Both are stored under `~/.config/turns/`, the token mode `0600`, and running it again replaces them. On a host the space may be omitted (`turns configure < token.txt`); the host's own space is used.

## Pushing on a timer

```sh
turns install
```

This writes `turns.service` and `turns.timer` under `~/.config/systemd/user/` and enables the timer, which runs `turns push` every five minutes and catches up at the next start after the machine was off. On a host the units are already in `/etc/systemd/user/`, so it only enables the timer. To stop it, `systemctl --user disable --now turns.timer`.

## Pushing by hand

```sh
turns push
turns push --dry-run
turns status
```

`push` sends the turns the space lacks and reports, per tool, the sessions that gained turns and the turns pushed. `--dry-run` asks the space the same question and pushes nothing. `status` shows the space, whether a token is set, the last push, and per tool what is on this machine against what the space holds.

## Exit codes

`0` success, `1` the operation failed, `2` usage error, `3` refused to run as root, `4` not configured. A timer run before `turns configure` exits 4, which the service counts as success. `turns --help` has the rest.
