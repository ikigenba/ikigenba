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

**Shape.** One file, `matrix_live_test.go`, holding `TestLiveMatrix` under
the build tag `live`, so the offline gates never touch the network. Following
the Live tests section of AGENTS.md, the matrix carries the requirement ids it
proves and the gap counts them like any other test's. A cell is a subtest, so
one host's outage fails one cell and the rest still report.

**Credentials.** AGENTS.md fixes the general rule: a live test reads its
credential from the environment and fails, never skips, when it is absent.
What is agentkit's own is which variable each cell reads: the API key from
the vendor's conventional variable for the offering's host, and the OAuth
token from the file `AGENTKIT_OPENAI_OAUTH_FILE` or `AGENTKIT_XAI_OAUTH_FILE`
names. `make live` sets the two file variables to the files under
`~/.agentkit`, where the `oauth` CLI writes them.

**Cells.** For each offering-id/authentication-mode pair present in
`Catalog()`, the matrix selects the lexicographically first catalog model that
carries the pair. Selection is derived at runtime, so requirements and
fixtures contain no release list. The ordinal in each subtest name keeps the
naming scheme stable if the representative count changes later. The catalog
today yields eleven cells: one `api_key` cell for each of the eight offering
ids, and an `oauth` cell for `openai-responses`, `xai-responses`, and
`xai-chat`.

**What a cell proves.** It builds one conversation exactly as a consumer
holding a catalog offering does: the `Offering` the derivation selected from
`Catalog()` (no `Lookup` round-trip), `Authenticator`, `NewEndpoint(auth)`,
`New`, with a `Log` writer so usage is observable and one advertised tool,
`echo`, taking one required string `text` that its handler returns. One `Send` asks the model to call `echo` and then answer
in text. The cell passes when the stream's `Err()` is nil, the tool call is
observed and executed, its result is carried back in a second round-trip, the
turn ends in a `MessageDone` with non-empty text, and every round-trip's
`usage` record (at least two) shows output tokens above zero and prompt
tokens, fresh plus cached, above zero; fresh input alone is not required to be
positive, since a vendor may serve the whole prompt from cache. That one turn covers
the silent-empty failure, the tool declaration, the tool-call decode, and the
tool-result replay on every wire, and nothing more: behavior, edge cases, and
error paths are the offline tests' job.

This representative matrix proves transport and authentication integration.
Targeted, dated observations outside the repository establish model-specific
availability and reasoning data before a row is written; the paid regression does
not sweep every catalog record, probe reasoning vocabularies, or exercise
system messages.

**When it runs.** It is a gate, but a conditional one, declared in
AGENTS.md: verify runs `make live` for a phase whose diff adds or changes a
`*_live_test.go` file, and does not run it otherwise. So the phase that
creates or extends the matrix must pass it against the real hosts, and later
phases do not pay for it. A human runs `make live` whenever fresh proof is
wanted. The one agentkit-specific part of `make live`, the two OAuth file
variables it sets, is proved offline.

## REQUIREMENTS

- R-FVBP-5TAH: `TestLiveMatrix`, in `matrix_live_test.go`, MUST run each cell derived under R-FU3S-S1JS as a subtest named `<offering-id>/<auth-mode>/<ordinal>`, where `ordinal` is the one-based position among representatives for that pair (and therefore `1` while R-FU3S-S1JS selects exactly one); a subtest name MUST NOT contain a model release.
- R-FU3S-S1JS: `TestLiveMatrix` MUST derive exactly one representative cell for every distinct `(Offering.ID, EndpointSpec.AuthMode)` pair present in `Catalog()`, selecting the lexicographically first `CatalogEntry.Model` among entries carrying the pair; the cell's `Offering` is that entry's matching `Offering` exactly as `Catalog()` returns it. This is one transport/auth representative per pair—not one paid call per catalog row—and contains no model literal.
- R-CJRE-3H0I: Every `TestLiveMatrix` cell MUST read its credential from the environment by its offering's `Host`: for an `api_key` cell the key in `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `XAI_API_KEY`, or `OPENROUTER_API_KEY` for `HostAnthropic`, `HostOpenAI`, `HostGemini`, `HostXAI`, or `HostOpenRouter` respectively, and for an `oauth` cell the token file named by `AGENTKIT_OPENAI_OAUTH_FILE` for `HostOpenAI` or `AGENTKIT_XAI_OAUTH_FILE` for `HostXAI`.
- R-FWJL-JL16: Every `TestLiveMatrix` cell MUST build one `Conversation` from the `Offering` selected under R-FU3S-S1JS, without calling `Lookup`, through `Offering.Authenticator` with `APIKeyRotator` for an `api_key` cell or `OAuthRotator(FileTokenStore(path))` for an `oauth` cell, `NewEndpoint(auth)` without `WithBaseURL`, and `New` with a `Log` and exactly one advertised tool, named `echo`, whose input schema is an object with one required string property `text` and whose handler returns its `text` argument; it MUST run one `Send` of `Call the echo tool with {"text":"pong"}, then answer with the single word: done` against the real vendor and require nil `Stream.Err()`, in order a `ToolCall` whose `Use.Name` is `echo`, a `ToolReturn` for that call, and a `MessageDone` carrying a non-empty `Text` block, and at least two `usage` log records, each whose `Usage` has positive `OutputTokens` and positive `InputTokens + CachedTokens`. No model-specific exception, system message, or reasoning probe may occur.
- R-CM76-V0HW: The module's `Makefile` `live` target MUST run its tests with `AGENTKIT_OPENAI_OAUTH_FILE` set to `$(HOME)/.agentkit/openai-auth.json` and `AGENTKIT_XAI_OAUTH_FILE` set to `$(HOME)/.agentkit/x-ai-auth.json`.
