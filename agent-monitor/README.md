> [!WARNING]
> This is unsupported AI slop.

# agent-monitor

`agent-monitor` is a CLI that shows what the coding agents on a developer's
Linux machine are doing. It reads the logs Claude Code, Codex, and Grok keep
under the developer's home directory and the process facts in `/proc`, and
never writes to either.

## Installing it

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/agent-monitor/install.sh | sh
```

This installs the newest stable release (Linux, amd64 and arm64) to
`~/.local/bin`. Set `AGENT_MONITOR_VERSION=vX.Y.Z` to pin a version, or
`BINDIR` to change the destination.

## Using it

```sh
agent-monitor --help
```
