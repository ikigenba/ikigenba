> [!WARNING]
> This is unsupported AI slop.

# sandbox

`sandbox` is a CLI that stands up the whole Ikigenba suite, exactly as it is
in the current git worktree, on a developer's own Linux machine, as that
developer, with no root. Each app runs as a pair of user-level systemd units
behind a per-sandbox nginx on `127.0.0.1`, and answers at
`http://<app>.<name>.localhost:<port>`. Many worktrees run sandboxes at once
without interfering. It is a local development tool and is never deployed to
a host.

## Installing it

From this directory:

```sh
make install
```

This builds `sandbox` from the checkout and installs it with `go install`.

## Using it

```sh
sandbox --help
```
