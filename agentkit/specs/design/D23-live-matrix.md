# D23-live-matrix

Vendor transport behavior is an external dependency. The live matrix retains
one end-to-end regression for every distinct offering-id/authentication-mode
pair in the authoritative catalog, with each cell asserting on substantive
results rather than merely the absence of an error.

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

**Cells.** For each offering-id/authentication-mode pair present in
`Catalog()`, the matrix selects the lexicographically first catalog model that
carries the pair. Selection is derived at runtime, so requirements and
fixtures contain no release list. The ordinal in each subtest name keeps the
naming scheme stable if the representative count changes later.

**What a cell proves.** It builds the conversation exactly as a consumer
does, `Lookup`, `Authenticator`, `NewEndpoint(auth)`, `New`, with a `Log`
writer so usage is observable, and runs three sequences.

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
3. A system sequence, on a third fresh conversation: `AddSystem` with an
   instruction to end every reply with a fixed token, then a turn; the reply
   contains the token. Then `AddSystem` names a second token and a second turn
   must contain it, proving both the leading and interleaved rendering of D24.

This representative matrix proves transport and authentication integration.
Targeted, dated observations outside the repository establish model-specific
availability and reasoning data before `check-spec`; the paid regression does
not sweep every catalog record or probe reasoning vocabularies.

**When it runs.** It is a gate, but a conditional one, declared in
AGENTS.md: verify runs `make live` for a phase whose diff adds or changes a
`*_live_test.go` file, and does not run it otherwise. So the phase that
creates or extends the matrix must pass it against the real hosts, and later
phases do not pay for it. A human runs `make live` whenever fresh proof is
wanted.

## REQUIREMENTS

- R-B7OR-ZKRT: The module MUST contain `live_matrix_test.go`, starting `//go:build live`, with `TestLiveMatrix` iterating the cells derived under R-B8WO-DCII and naming every subtest `<offering-id>/<auth-mode>/<ordinal>`, where `ordinal` is the one-based position among representatives for that pair (and therefore `1` while R-B8WO-DCII selects exactly one). A subtest name MUST NOT contain a model release.
- R-L2HW-IKU6: Every `TestLiveMatrix` subtest MUST read its credential from the environment, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `XAI_API_KEY`, or `OPENROUTER_API_KEY` for `api_key` by the offering's `Host`, and the file named by `AGENTKIT_OPENAI_OAUTH_FILE` or `AGENTKIT_XAI_OAUTH_FILE` for `oauth` by the offering's `Host`, and MUST fail (never `t.Skip`) when the variable is unset or the file is unreadable.
- R-B8WO-DCII: `TestLiveMatrix` MUST derive exactly one representative cell for every distinct `(Offering.ID, EndpointSpec.AuthMode)` pair present in `Catalog()`, selecting the lexicographically first `CatalogEntry.Model` among matching offerings. This is one transport/auth representative per pair—not one paid call per catalog row—and contains no model literal.
- R-0GAK-O3DO: Every representative `TestLiveMatrix` cell MUST build three fresh conversations from its selected `Offering` through `Offering.Authenticator` with `APIKeyRotator` or `OAuthRotator(FileTokenStore(path))`, `NewEndpoint(auth)` without `WithBaseURL`, and `New` with a `Log`: the first MUST send `Reply with the single word: pong` and require nil `Stream.Err()`, a `MessageDone` with a non-empty `Text` block, and a `usage` log record with positive `InputTokens` and `OutputTokens`; the second MUST advertise one tool named `echo` whose handler returns its argument, send `Call the echo tool with {"text":"pong"}, then answer with the single word: done`, and require nil `Stream.Err()` plus, in order, a `ToolCall` whose `Use.Name` is `echo`, a `ToolReturn`, and a `MessageDone`; the third MUST call `AddSystem("Append " + firstToken + " to every reply.")`, send `Say hello.`, and require nil `Stream.Err()` and a `MessageDone` `Text` block containing `firstToken`, then call `AddSystem("Append " + secondToken + " to every reply.")` with a different `secondToken`, send `Say goodbye.`, and require nil `Stream.Err()` and a `MessageDone` `Text` block containing `secondToken`. No model-specific exception or reasoning probe may occur; reasoning observations belong only to the pre-`check-spec` evidence work.
- R-L65L-NW29: The module's `Makefile` MUST declare a `live` target that sets `AGENTKIT_OPENAI_OAUTH_FILE` to `$(HOME)/.agentkit/openai-auth.json` and `AGENTKIT_XAI_OAUTH_FILE` to `$(HOME)/.agentkit/x-ai-auth.json` and runs `go test -tags live -count=1 -run '^TestLive' ./...`, MUST NOT declare a `live-oauth` target, and no other target MUST pass `-tags live` or `-tags integration`.
