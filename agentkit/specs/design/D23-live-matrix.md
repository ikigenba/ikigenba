# D23-live-matrix

Every fact agentkit holds about a vendor is an external dependency: which
host honors which credential, which body fields a protocol demands, where
usage sits in a stream. None of it can be assumed. Each such fact is proven
by a real request before it is written into a requirement, and the proof is
kept as a test so the builder has something to meet and a later change that
breaks the wire is caught. This design is that test: a **live matrix** with
one cell per catalog offering and credential kind, each cell driving a real
conversation end to end and asserting on substance, never on "no error".

The matrix exists because its absence let three defects ship. No wire sent
`"stream":true`, so every API-key call returned unary JSON that the SSE
reader silently turned into an empty transcript with zero usage and exit 0.
The Anthropic wire omitted the mandatory `max_tokens`. And the OpenAI OAuth
path posted to a host that has never honored a ChatGPT token. Each would have
failed the first cell that exercised it.

**Shape.** One file, `live_matrix_test.go`, under the build tag `live`, so the
offline gates never touch the network. It is not in the gap file set
(AGENTS.md excludes `*_live_test.go`), and it carries no requirement id; an
offline architecture test pins its existence, tag, and cell table, the way
D22's live rotation tests are pinned. A cell is a subtest, so one host's
outage fails one cell and the rest still report.

**Credentials fail, never skip.** A missing credential is a missing proof.
Each cell reads its credential from the environment, API keys from the
vendor's conventional variable and OAuth tokens from the file
`AGENTKIT_OPENAI_OAUTH_FILE` or `AGENTKIT_XAI_OAUTH_FILE` names, and fails
the cell when it is absent. `make live` sets the two file variables to the
files under `~/.agentkit`, where the `oauth` CLI writes them.

**Cells.** One per offering id and per `AuthMode` its endpoint specs list,
each on one fixed cheap model. The Codex cell uses a model Codex serves,
since the Codex backend rejects the platform's nano model.

| Offering | Auth | Model |
|---|---|---|
| `anthropic-messages` | api_key | `claude-haiku-4-5` |
| `openai-responses` | api_key | `gpt-5.4-nano` |
| `openai-responses` | oauth | `gpt-5.4-mini` |
| `openai-chat` | api_key | `gpt-5.4-nano` |
| `gemini-generate-content` | api_key | `gemini-3.1-flash-lite` |
| `xai-responses` | api_key, oauth | `grok-4.3` |
| `xai-chat` | api_key, oauth | `grok-4.3` |
| `openrouter-chat` | api_key | `gpt-5.4-nano` |
| `openrouter-responses` | api_key | `gpt-5.4-nano` |

**What a cell proves.** It builds the conversation exactly as a consumer
does, `Lookup`, `Authenticator`, `NewEndpoint(auth)`, `New`, with a `Log`
writer so usage is observable, and runs two turns.

1. A text turn: send "Reply with the single word: pong". The stream's
   `Err()` is nil, a `MessageDone` carries a non-empty `Text` block, and the
   turn's `usage` record has input and output tokens above zero. This is the
   cell that catches the silent-empty failure.
2. A tool turn, on a fresh conversation advertising one tool, `echo`, whose
   handler returns its argument: send an instruction to call `echo` with
   "pong" and then answer "done". `Err()` is nil, a `ToolCall` naming `echo`
   and a `ToolReturn` are observed, and a `MessageDone` follows them. This
   proves the tool declaration, the tool-call decode, and the tool-result
   replay on every wire.

**When it runs.** It is a gate, but a conditional one, declared in
AGENTS.md: verify runs `make live` for a phase whose diff adds or changes a
`*_live_test.go` file, and does not run it otherwise. So the phase that
creates or extends the matrix must pass it against the real hosts, and later
phases do not pay for it. A human runs `make live` whenever fresh proof is
wanted.

## REQUIREMENTS

- R-L1A0-4T3H: The module MUST contain the file `live_matrix_test.go`, beginning with the build constraint `//go:build live`, containing a test named `TestLiveMatrix` that runs one subtest per row of R-L3PS-WCKV named `<offering id>/<auth mode>`.
- R-L2HW-IKU6: Every `TestLiveMatrix` subtest MUST read its credential from the environment, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `XAI_API_KEY`, or `OPENROUTER_API_KEY` for `api_key` by the offering's `Host`, and the file named by `AGENTKIT_OPENAI_OAUTH_FILE` or `AGENTKIT_XAI_OAUTH_FILE` for `oauth` by the offering's `Host`, and MUST fail (never `t.Skip`) when the variable is unset or the file is unreadable.
- R-L3PS-WCKV: `TestLiveMatrix` MUST run exactly these cells, each resolved with `Lookup(model, host, wire)`: `anthropic-messages`/`api_key` on `claude-haiku-4-5`; `openai-responses`/`api_key` on `gpt-5.4-nano`; `openai-responses`/`oauth` on `gpt-5.4-mini`; `openai-chat`/`api_key` on `gpt-5.4-nano`; `gemini-generate-content`/`api_key` on `gemini-3.1-flash-lite`; `xai-responses`/`api_key` and `/oauth` on `grok-4.3`; `xai-chat`/`api_key` and `/oauth` on `grok-4.3`; `openrouter-chat`/`api_key` and `openrouter-responses`/`api_key` on `gpt-5.4-nano`.
- R-L4XP-A4BK: Every `TestLiveMatrix` subtest MUST build its conversations from `Offering.Authenticator` with `APIKeyRotator` or `OAuthRotator(FileTokenStore(path))`, `NewEndpoint(auth)` with no `WithBaseURL`, and `New` with a `Log`; MUST run a text turn asserting `Stream.Err()` is nil, a `MessageDone` holds a non-empty `Text` block, and a `usage` log record has `InputTokens` and `OutputTokens` greater than zero; and MUST run a tool turn on a second conversation advertising one tool named `echo`, asserting `Stream.Err()` is nil and the events include, in order, a `ToolCall` whose `Use.Name` is `echo`, a `ToolReturn`, and a `MessageDone`.
- R-L65L-NW29: The module's `Makefile` MUST declare a `live` target that sets `AGENTKIT_OPENAI_OAUTH_FILE` to `$(HOME)/.agentkit/openai-auth.json` and `AGENTKIT_XAI_OAUTH_FILE` to `$(HOME)/.agentkit/x-ai-auth.json` and runs `go test -tags live -count=1 -run '^TestLive' ./...`, MUST NOT declare a `live-oauth` target, and no other target MUST pass `-tags live` or `-tags integration`.
