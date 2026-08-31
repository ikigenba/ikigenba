# agentkit

`agentkit` is built **spec-first**: the design documents under `specs/design/`
define the contract, and an automated build loop writes the code, tests it, and
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
- **`Endpoint`** — a public, option-built transport: base URL, auth applier,
  headers, framing, error classifier, request-mutation hook, replay encoding.
- **`Model`** — a free-form string, never gated, passed verbatim. A model
  released today runs with no agentkit release; an unknown model is the vendor's
  400, not ours.

Vendor packages (`anthropic.New(...)`, `openai.New(...)`, …) bake a
`(WireFormat, Endpoint)` pair with their own typed credentials; the generic wire
path is public too, so a new vendor speaking a known wire needs no code here. One
verb drives a conversation — `conv.Send(ctx, agentkit.Text("hi"))` — returning a
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
- `specs/loops/` — the gather → build → verify prompts the build loop runs
  (via `ralph`, or any agent driving the same cycle).
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions the
  loop verifies against.

To change agentkit, change the spec — `$open-spec`, then `$seal-spec`, then run
the loop — rather than editing the code directly.
