# oauth

`oauth` is built **spec-first**: the design documents under `specs/design/`
define the contract, and an automated build loop writes the code, tests it,
and proves it against the spec. Every behavior traces to a requirement id, and
every requirement id to a test.

So this sub-project is two things at once:

1. **A small, useful CLI:** clone the monorepo, run `make build`, get a
   working binary.
2. **A demonstration of spec-first construction:** a project fully specified
   up front, then generated from that spec. See
   [how the spec system works](../docs/spec-system.md).

> **Status:** the spec is written; the code is not. `cmd/` and `internal/` are
> absent until the build loop creates them.

## Installing it

Grab a released binary (linux/darwin, amd64/arm64) into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/oauth/install.sh | sh
```

Pin a version or change the destination with env vars:

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/oauth/install.sh | OAUTH_VERSION=v0.1.0 BINDIR=/usr/local/bin sh
```

## What oauth is (the end product)

Every tool that needs a user's OAuth credentials re-implements the same
authorization-code handshake: stand up a loopback listener, build an authorize
URL with PKCE, open a browser, catch the redirect, exchange the code. It is
fiddly, easy to get subtly wrong, and it gets rewritten per provider and per
program — so a bug fixed in one copy survives in the others.

`oauth` does that job once, as a standalone command. It runs the OAuth 2.0
authorization-code + PKCE flow against any protocol-compliant service and hands
the resulting token response back to whoever invoked it. A person runs it at a
terminal to log in and capture credentials to a file; a program shells out to
it to obtain a token response without carrying its own OAuth implementation.

It holds **no provider-specific knowledge** — no per-provider defaults,
endpoints, quirks, or branching. A login is described entirely by flags. (The
`--help` text names real services in worked examples; those are documentation,
not defaults.) It does nothing else: it does not store credentials, choose a
file format, refresh or renew tokens, or inspect what the token response
contains. Those belong to the caller.

```sh
oauth \
  --auth-url  https://auth.example.com/oauth/authorize \
  --token-url https://auth.example.com/oauth/token \
  --client-id your-client-id \
  --scope "openid profile offline_access" \
  > auth.json
```

The token endpoint's response goes to **stdout, verbatim** — nothing added,
removed, or reordered, and nothing else is ever written there, so the
redirection above is a faithful record of what the service said. Everything
meant for a human — the authorize URL, progress, errors — goes to **stderr**,
so it stays on the terminal and out of the file. A failed login writes zero
bytes to stdout and exits non-zero, so a file that exists but is empty is never
mistaken for credentials.

During a login the program serves its own loopback callback endpoint and opens
your browser to the authorize URL. If the browser cannot be opened, the URL is
still printed so you can visit it yourself.

The callback address must match what the provider registered for your client:
`--port` and `--callback-path` match it exactly, and `--callback-host` covers
registrations that use `127.0.0.1` rather than the default `localhost`. For
confidential clients, pass `--client-secret`, or supply an `Authorization`
header with `--token-header`. Providers demanding extra parameters are handled
by the repeatable `--auth-param` and `--token-param` escape hatches.

Run `oauth --help` for the full flag list and two copy-paste worked examples.

## Building it

Requires **Go 1.26+**. From this directory:

```sh
make build     # bin/oauth
make install   # go install ./cmd/oauth
make test      # go test -race ./...
```

The full verification gates (build, cross-platform vet, race tests,
`golangci-lint`, `llm-lint`) are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — the design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `specs/loops/` — the gather → build → verify prompts the build loop runs
  (via `ralph`, or any agent driving the same cycle).
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions
  the loop verifies against.

To change oauth, change the spec — `$open-spec`, then `$seal-spec`, then run
the loop — rather than editing the code directly.

## Releases

Releases are cut from the monorepo by pushing a tag `oauth/vMAJOR.MINOR.PATCH`
that matches the in-source version string; a GitHub workflow builds
linux/darwin × amd64/arm64 archives with checksums and publishes them on the
tag. Details in [`AGENTS.md`](AGENTS.md).
