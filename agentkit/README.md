> [!WARNING]
> This is unsupported AI slop.

# agentkit

`agentkit` is a library that talks to LLM chat/completions APIs and runs an
agentic tool loop. It separates the wire format a vendor speaks, the endpoint
it lives behind, and the model string, so a new vendor or a day-one model
needs no library release. [`toolkit`](../toolkit) supplies ready-made tools
for it, and [`agent-repl`](../agent-repl) is a REPL built on it.

## Installing it

```sh
go get github.com/ikigenba/ikigenba/agentkit@latest
```

Releases are tagged `agentkit/vX.Y.Z`; to pin one, use `@vX.Y.Z` in place of
`@latest`.

## Using it

```sh
go doc -all github.com/ikigenba/ikigenba/agentkit
```
