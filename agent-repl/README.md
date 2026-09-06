# agent-repl

`agent-repl` is built **spec-first**: the design documents under
`specs/design/` define the contract, and an automated build loop writes the
code, tests it, and proves it against the spec. Every behavior traces to a
requirement id, and every requirement id to a test. See
[how the spec system works](../docs/spec-system.md).

## Installing it

Grab a released binary (linux/darwin, amd64/arm64) into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/agent-repl/install.sh | sh
```

Pin a version or change the destination with env vars:

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/agent-repl/install.sh | AGENT_REPL_VERSION=v0.8.0 BINDIR=/usr/local/bin sh
```

## What agent-repl is

A thin interactive REPL over [`agentkit`](../agentkit): one conversation per
session, the six [`toolkit`](../toolkit) file tools (`Bash`, `Read`, `Write`,
`Edit`, `Glob`, `Grep`) rooted at the working directory, and a transcript a
person can read.

```
$ agent-repl -c provider=openrouter -c model=deepseek-v4-pro
you › Hi, I'm Mike.

assistant › Hi Mike! Nice to meet you. How can I help?

you › 
summary
· tokens  in=626 cache(r=0 w=0) out=17 reasoning=0 total=643
· cost     $0.002830 session
```

Everything is a `-c key=value` string. Six keys pick the conversation —
`provider`, `model`, `wire`, `auth`, `auth_file`, `base_url` — and every other
key is handed to agentkit as a request option, unchecked, so the library and
the provider report what they will not accept. `agent-repl --help` prints the
catalog: every provider, its credential source, and every model with its
reasoning vocabulary, generated from agentkit at build time.

Credentials come from the environment (`<PROVIDER>_API_KEY`, upper-cased) or,
for providers that offer it, from an OAuth token file at
`~/.agent-repl/<provider>-auth.json` written by the sibling
[`oauth`](../oauth) CLI. Each session's event log lands in
`~/.agent-repl/logs/`; `-raw` streams those same records to stdout instead of
the transcript.

## Building it

Requires **Go 1.26+**. From this directory:

```sh
make build     # bin/agent-repl
make install   # go install ./cmd/agent-repl
make test      # go test -race ./...
```

The full verification gates are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — six design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves.
- `specs/loops/` — the gather → build → verify prompts the build loop runs.
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions.

To change agent-repl, change the spec — `$open-spec`, then `$seal-spec`, then
run the loop — rather than editing the code directly.

## Releases

Push a tag `agent-repl/vMAJOR.MINOR.PATCH` matching the in-source version
string; a GitHub workflow builds and publishes the archives. Details in
[`AGENTS.md`](AGENTS.md).
