> [!WARNING]
> This is unsupported AI slop.

# agent-repl

`agent-repl` is a CLI that runs an interactive agent session in the terminal.
It holds one [`agentkit`](../agentkit) conversation per session, with the
[`toolkit`](../toolkit) tools rooted at the working directory, and prints a
transcript a person can read.

## Installing it

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/agent-repl/install.sh | sh
```

This installs the newest stable release (Linux and macOS, amd64 and arm64) to
`~/.local/bin`. Set `AGENT_REPL_VERSION=vX.Y.Z` to pin a version, or `BINDIR`
to change the destination.

## Using it

```sh
agent-repl --help
```
