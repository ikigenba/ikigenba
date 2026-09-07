# D27-prompt-caching

A savepoint (D26) says something a wire can use: *everything before this mark is
the stable part, and I intend to send it again.* That is exactly the condition
every vendor's prompt cache is built for, and without it a consumer like
`llm-lint` re-sends its whole corpus at full price once per rule. With it, the
corpus is paid for once and read back at a tenth of the cost or less.

**A savepoint is a request-prefix boundary, not a history boundary.** Vendors
key their caches on the whole rendered request prefix — tools first, then system
content, then messages — so a mark that promised only a stable *message* prefix
would promise nothing: one tool loaded mid-loop shifts every byte after it and
misses the cache entirely. D26 is what makes the promise real, by freezing the
tool array and refusing `AddSystem` for as long as the savepoint lives. Because
the whole prefix is frozen, a single breakpoint at the mark is enough; there is
nothing upstream of it left that can move. On Anthropic the mark lands wherever
the last pre-mark message was rendered — inside `messages`, or on the last
`system` block when only system messages precede it (D24 hoists leading system
messages out of `messages`); the vendor honours a breakpoint in either place.
The Gemini cache names its model as a resource, `models/<name>`, where every
`generateContent` request already puts the same name in its path; the bare
name in the cache body is an "Invalid model name". `toolConfig` rides along
in the Gemini cache with the tools: `Settings` is fixed
for the conversation's life (D18), so tool choice is part of the frozen prefix
too, and a cache that carried the tools but not the choice would silently
change what the model may call.

**Three vendor shapes, one consumer story.** The consumer takes a savepoint and
nothing else. What each wire does with it differs completely:

- **Anthropic** marks a block in a request it was sending anyway. The prefix
  bytes still travel on every request; the vendor hashes them and bills the
  matched span at the cache-read rate. One `cache_control` object, placed on the
  last content block before the mark.
- **OpenAI, xAI, OpenRouter** need nothing. Their prefix caching is automatic
  above a token floor, so the savepoint's contribution is the stable prefix
  itself. These wires render exactly the same bytes whether or not a savepoint is
  live. Rendering a cache boundary to *nothing* is a correct rendering here, not
  a silent downgrade — D8's fail-loud rule (R-3S2D-29LY) governs reasoning
  shapes a wire cannot express, and has no bearing on a boundary a wire honours
  by construction.
- **Gemini** is the odd one. It offers both implicit caching and an explicit
  `cachedContents` resource, and we take the explicit path: implicit caching is
  best-effort, unobservable in advance, and gives no control over lifetime,
  which is a weak contract for a library that fails loud everywhere else.
  Explicit caching means the prefix is uploaded once as its own resource and
  later requests reference it *by name and omit those bytes*. So on Gemini a
  savepoint changes the request's shape, not just one field, and adds a
  preparatory call before the first request that uses it.

**Gemini's asymmetry, which the design must respect.** Sending the cached
`contents` again alongside `cachedContent` is silently additive — no error, the
turns simply stack on top of the cached prefix and are billed as fresh input. But
sending `systemInstruction`, `tools`, or `toolConfig` alongside a cache that
carries them is a hard rejection, even when byte-identical. So the wire must omit
all four, and the silent case is the dangerous one: a bug that forgets to drop
the cached messages produces no error, just a doubled context at double the
price.

**Lifetime.** The cache is created lazily, on the first round-trip after the
savepoint — inside a method that already makes HTTP calls, rather than making
`Savepoint()` secretly a network operation. `Restore` never deletes it; that is
the whole point, since the consumer restores to the same mark on every
iteration. `Release` and `Close` delete it. Deletion matters in a way it does not
on the other hosts: Gemini bills explicit cache storage per token-hour for as
long as the resource lives, so an undeleted cache is rent paid for nothing.

**Two failure modes worth naming.** Below the host's minimum cacheable size
Gemini refuses to create the cache at all — HTTP 400, `INVALID_ARGUMENT`,
"Cached content is too small. total_token_count=16, min_total_token_count=1024"
— where Anthropic simply ignores the marker; in both cases the turn must proceed
normally and uncached, because "your prefix was small" is not a consumer error.
And a cache that has expired or been deleted comes back as HTTP 403,
`PERMISSION_DENIED`, "CachedContent not found (or permission denied)" — not a
404 — which the built-in classification would otherwise read as a credential
failure (D4, R-OHYJ-3C6O). So a stale cache must be rebuilt rather than surfaced
as an auth error. The two are told apart by context, not by status: only a
request that referenced a cache is retried, and only once. A real credential
failure fails the creating `POST` too, and that one surfaces as auth like any
other. The cache's default lifetime when `ttl` is omitted is one hour.

**What this design does not price.** Gemini's storage rent is a charge per
token-*hour*, and `Usage` carries no duration for `Pricing` to multiply. Rather
than bend the cost model around a charge worth a few percent of what caching
saves, `Cost` continues to price tokens only, and the rent is knowingly
unaccounted. Making that explicit is better than a cost figure that is quietly
wrong.

**Proof against the real hosts.** Caching is exactly the kind of behavior that
can be implemented plausibly and still not work — a breakpoint in the wrong
place, an invalidation nobody predicted, a cache that is created and never read.
Golden fixtures pin what we emit; only a live call proves the vendor honoured it.
So every wire family carries a live test that takes a savepoint and sends two
different suffixes across a `Restore`.

Four of the five then assert the second round-trip actually read from cache.
xAI is the exception, and deliberately: probing it against the real host showed
cache reads arriving intermittently — hits and misses alternating on an
identical prefix seconds apart, at every prefix size tried, and no better with a
larger one. Worse, nearly every xAI response reports a small constant
`cached_tokens` floor unrelated to the prefix we sent, so a naive "greater than
zero" assertion would pass without proving anything. A gate that fails at random,
or passes vacuously, is worse than no gate: it teaches everyone to re-run the
suite until it goes green. So the xAI subtest proves what is actually
reproducible — that a live savepoint does not break the turn — and asserts
nothing about caching. If xAI's behavior becomes dependable, that is a
requirement to add then, on evidence.

The prefix floor is a single number for every subtest rather than a per-host
minimum, chosen to clear the largest of them with margin: the observed minimums
are 4096 tokens on `claude-haiku-4-5` and 1024 on `gemini-3.5-flash-lite`, and
OpenAI proved unreliable near 2000 tokens but perfectly reliable past 5000.

The Anthropic subtest builds its prefix with `AddSystem` alone, so what it
proves is the breakpoint on a `system` block — the shape a consumer that loads
its corpus as system content depends on. A probe against the real host showed
that placement written (`cache_creation_input_tokens: 6702`) and read back
exactly on the next two requests, with no message preceding the `system`
array and an uncached `tools` array ahead of it. The raw request and response
bodies of that probe, and of the Gemini probes behind the error shapes above,
are kept in `docs/probes/prompt-caching.md`.

## REQUIREMENTS

- R-NU2E-M5K2: While a savepoint (D26) is live, `AnthropicMessagesWire()` MUST place exactly one `cache_control` object on the last content block it renders from the `History` messages before the savepoint's mark — the last block of the last `messages` entry rendered from them, or, when every message before the mark is a `RoleSystem` message rendered into the top-level `system` array (D24), the last block of that array — and MUST place no `cache_control` anywhere else in the request; when no `History` message precedes the mark it MUST place none at all; pinned by golden request fixtures covering both placements.
- R-KV60-Z3HN: With no live savepoint, `AnthropicMessagesWire()` MUST emit no `cache_control` key anywhere in the request; pinned by a golden request fixture.
- R-KWDX-CV8C: A `cache_control` object `AnthropicMessagesWire()` emits MUST be exactly `{"type":"ephemeral"}` with no `ttl` member, so every cache write takes the vendor's five-minute default.
- R-KXLT-QMZ1: `ChatWire()`, `OpenAIChatWire()`, `XAIChatWire()`, `ResponsesWire()`, `OpenAIResponsesWire()`, and `XAIResponsesWire()` MUST each render a byte-identical request body whether or not a savepoint is live, and a live savepoint MUST NOT cause any of them to fail at `Send`; pinned by golden request fixtures.
- R-LC8M-BVVD: With no live savepoint, `GeminiGenerateContentWire()` MUST create no cache resource, MUST emit no `cachedContent` member, and MUST render the request exactly as it does today; pinned by a golden request fixture.
- R-Z0V6-102E: On the first provider round-trip after a savepoint is taken, `GeminiGenerateContentWire()` MUST create one `cachedContents` resource by `POST` whose `model` is the conversation's model in the host's resource form, `models/` followed by the model string — the bare string is rejected as an invalid model name — whose `contents` are the `History` messages up to the savepoint's mark, whose `systemInstruction` is that history's system rendering, whose `tools` are the advertised tools, and whose `toolConfig` is the rendering of `Settings.ToolChoice` (D8) that the request would otherwise carry — each member omitted exactly when the uncached request would omit it — and MUST omit `ttl`.
- R-L01M-I6GF: While a `cachedContents` resource is live for the conversation, every `generateContent` request `GeminiGenerateContentWire()` builds MUST set `cachedContent` to that resource's name, MUST omit from `contents` every `History` message at or before the savepoint's mark, and MUST omit the `systemInstruction`, `tools`, and `toolConfig` members entirely; pinned by a golden request fixture.
- R-L2HF-9PXT: A `Restore` MUST NOT delete the conversation's `cachedContents` resource, and the first `generateContent` request after a `Restore` MUST reference the same resource name as the request before it.
- R-L3PB-NHOI: `Release` and `Close` (D26) MUST each delete the conversation's `cachedContents` resource by `DELETE` on its name, and a conversation that has been released or closed MUST leave no `cachedContents` resource it created undeleted.
- R-NWI7-DP1G: When creating a `cachedContents` resource is rejected with HTTP 400 whose `error.status` is `INVALID_ARGUMENT` and whose `error.message` begins `Cached content is too small` — the host's below-minimum-size rejection — the turn MUST proceed with no cache — rendering the request exactly as R-LC8M-BVVD requires — MUST complete normally, and MUST surface no error on `Stream.Err()`.
- R-NXQ3-RGS5: When a `generateContent` request carrying `cachedContent` is rejected with HTTP 403 whose `error.status` is `PERMISSION_DENIED` — the host's shape for a `cachedContents` resource that no longer exists — the wire MUST create the resource once more and retry that request exactly once, and MUST NOT surface that first rejection as an `*Error` whose `Category` is `CategoryAuth`; a 403 on the retry, or on the creating `POST`, MUST surface through the ordinary classification (D4).
- R-L7D0-SSWL: The `Cost` reported for a turn MUST be derived from `Usage` alone; the storage rent a `cachedContents` resource accrues MUST NOT be added to any `usage`, `turn_end`, or `summary` record's `Cost`.
- R-1ETZ-7OQ0: The module MUST contain the file `cache_live_test.go`, beginning with the build constraint `//go:build live`, containing a test named `TestLiveCache` that runs one subtest per built-in wire family — `anthropic-messages`, `openai-responses`, `openai-chat`, `gemini-generate-content`, and `xai-responses` — reading its credential from the environment exactly as R-L2HW-IKU6 requires and failing, never skipping, when it is absent.
- R-6PME-Y8QW: Every `TestLiveCache` subtest MUST build a conversation whose `History` before the savepoint exceeds 8192 tokens — the `anthropic-messages` subtest by `AddSystem` alone with no `Send` before the savepoint, so the breakpoint it proves is one on a `system` block, and every other subtest by at least one `Send` — take a `Savepoint`, run one `Send`, `Restore`, then run a second `Send` carrying different text, and MUST assert `Stream.Err()` is nil for both; the `anthropic-messages`, `openai-responses`, `openai-chat`, and `gemini-generate-content` subtests MUST additionally assert that the `usage` log record of the second `Send`'s round-trip has `CachedTokens` greater than zero, and the `xai-responses` subtest MUST NOT assert anything about `CachedTokens`.

## Canonical usage

Unchanged from D26 — a consumer asks for caching by taking a savepoint, and
never names a cache:

```go
sp, err := conv.Savepoint()          // the wire caches the prefix from here
if err != nil {
	return err
}
for _, rule := range rules {
	stream := conv.Send(ctx, agentkit.Text{Text: rule.Prompt})
	for event := range stream.Events() {
		record(event)
	}
	if err := stream.Err(); err != nil {
		return err
	}
	if err := conv.Restore(sp); err != nil {
		return err
	}
}
if err := conv.Release(sp); err != nil {   // deletes any provider-side cache
	return err
}
```

The saving is observable where every other cost is, in the log's `usage`
records — `CachedTokens` carries the cache read on every wire:

```go
for _, record := range decodeLog(buf) {
	if record.Type == agentkit.RecordUsage {
		fmt.Println(record.Usage.InputTokens, record.Usage.CachedTokens, *record.Cost)
	}
}
```
