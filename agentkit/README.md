> [!WARNING]
> This is unsupported AI slop.

# agentkit

`agentkit` is built **spec-first**: the design documents under `specs/design/`
define the contract, and an agent-driven build run writes the code, tests it, and
proves it against the spec. Every behavior traces to a requirement id, and every
requirement id to a test.

So this sub-project is two things at once:

1. **A Go library:** talk to LLM chat/completions APIs and run an agentic tool
   loop, behind one small public surface.
2. **A demonstration of spec-first construction:** a library fully specified up
   front, then generated from that spec. See
   [how the spec system works](../docs/spec-system.md).

## What agentkit is (the end product)

Talking to a modern LLM means picking three things that vary independently: the
**wire format** a vendor's API speaks, the **endpoint** it lives behind (base URL,
auth, headers, error shape), and the **model** string. The old way fused these
into one "provider" and broke whenever a vendor moved a credential to a new host
or shipped a model on a Tuesday. agentkit keeps them orthogonal:

- **`WireFormat`** — the internal codec (Anthropic Messages, OpenAI Responses,
  OpenAI Chat Completions, Gemini generateContent), selected by a constructor.
- **`Endpoint`** — the transport: a base URL and an auth applier, nothing more.
- **`Model`** — a free-form string, never gated, passed verbatim. A model
  released today runs with no agentkit release; an unknown model is the vendor's
  400, not ours.

The catalog knows each provider: an `Offering` carries the wire, the default
base URL, the credential modes the provider accepts, and the wire model. The
consumer resolves an offering, hands it a root credential (`APIKey` or
`OAuth`), and assembles the conversation from those parts. The one thing a
consumer customizes is the URL passed to `NewEndpoint`; everything else comes
from the library. One verb drives a conversation — `conv.Send(ctx, agentkit.Text("hi"))` — returning a
stream of message-granular events, running any tool round-trips to completion.

Day-one endpoints: OpenAI, Anthropic, Google Gemini, xAI, OpenRouter.

## Building it

Requires **Go 1.26+**. From this directory:

```sh
make build     # go build ./...
make test      # go test -race ./...
```

The full verification gates (format, build, race tests, `golangci-lint`,
`llm-lint`) are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — the design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions the
  build run verifies against.

To change agentkit, change the spec — `draft-spec`, then `check-spec`, then
`build-spec` — rather than editing the code directly.
