> [!WARNING]
> This is unsupported AI slop.

# toolkit

`toolkit` is a library that provides ready-made tools for
[`agentkit`](../agentkit): `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`,
`WebSearch`, and `WebFetch`. `WebSearch` queries the Brave Search API and
`WebFetch` fetches a public URL as text. Each local tool is built against an
explicit root directory, and every tool behaves like the Claude Code harness
tool of the same name, so a model already knows how to use it.

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
