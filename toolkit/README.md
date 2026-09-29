> [!WARNING]
> This is unsupported AI slop.

# toolkit

`toolkit` is a library that provides ready-made local tools for
[`agentkit`](../agentkit): `Bash`, `Read`, `Write`, `Edit`, `Glob`, and
`Grep`. Each tool is built against an explicit root directory and behaves like
the Claude Code harness tool of the same name, so a model already knows how to
use it.

## Installing it

```sh
go get github.com/ikigenba/ikigenba/toolkit@latest
```

Releases are tagged `toolkit/vX.Y.Z`; to pin one, use `@vX.Y.Z` in place of
`@latest`.

## Using it

```sh
go doc -all github.com/ikigenba/ikigenba/toolkit
```
