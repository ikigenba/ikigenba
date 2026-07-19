# Phase 91 — Convert the chat seam to the prompts `/complete` client

*Realizes design Decision 5 (the prompts-client LLM seam) and Decision 18 (truncation detection over `/complete` usage).*

Rewrite `internal/llm` as the prompts HTTP client: `Client{baseURL}` built from a plain base URL, the D5 `Config` vocabulary, `CallSite`/`Attribution`, and `JSON[T]` posting `/complete` with full stateless replays and on-the-record corrective retries. `ExtractJSON` and `ErrTruncated` survive; `Converse` and the agentkit provider path are removed from the package. Extract (D6), compile (D7), and ask (D9) compile against the new seam (their `DefaultCallSite` values re-expressed in the Config vocabulary per D19's mapping; D19's resolver knobs re-target it). The composition root builds the client from `registry.BaseURL("prompts")`. Tests drive the real client against a `httptest` server playing prompts; the old provider mocks in the touched packages are replaced by canned `/complete` responses. (Package-external residue — the recorder stack, eval, agentkit's `go.mod` entry — is later phases'; this phase leaves the tree compiling and green.)

**Done when:** the suite is green (design Conventions) and these ids are covered by tagged tests:

- R-J8QP-BETB — `ExtractJSON` carves fenced/bare JSON.
- R-4BCC-0EHJ — `ExtractJSON` recovers decorated replies (regression guard).
- R-J9YL-P6K0 — `JSON` returns the validated `T` with `validate` applied.
- R-JCEE-GQ1E — `JSON` retries parse/validate failures up to `MaxParseRetries`, then errors.
- R-0X4N-U0XB — the `/complete` request JSON faithfully carries name/model/config/system and a final `user` turn.
- R-0ZKG-LKEP — a corrective retry is a new `/complete` call replaying user/assistant/corrective turns with `attempt` 2.
- R-10SC-ZC5E — a 400 fails immediately, consumes no retry, makes no further call.
- R-1209-D3W3 — a 502 or transport error propagates immediately with no wiki-side retry.
- R-MSKH-GPX5 — `max_tokens` reaches the request verbatim.
- R-MTSD-UHNU — output usage at the ceiling returns a distinct truncation error.
- R-MV0A-89EJ — truncation is terminal (exactly one HTTP call despite retries budget).
- R-MW86-M158 — extract/compile defaults carry `MaxTokens >= 16384` and the composition root builds from them.
