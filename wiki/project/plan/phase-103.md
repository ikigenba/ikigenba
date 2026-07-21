# Phase 103 — Runner widening: per-case progress, the full provider set, subscription auth

*Realizes design Decision 66 (eval runner), slice R-ETV6-57CQ, R-EWAY-WQU4, R-EXIV-AIKT.*

`cmd/eval-extract` stops being silent and provider-narrow: `run` emits one per-extraction progress line to stderr (stdout and the `-out` scorecard stay clean); the chat provider set widens to agentkit's five (`anthropic`, `google`, `openai`, `openrouter`, `zai`) with key auth via each provider's env var, failing loudly by name; and `eval.auth`/`eval.auth_file` (new optional config fields, parsed by the `internal/eval` loader with `key` and `~/.agentrepl/auth.json` defaults) select openai-only subscription auth over `agentkit/openai/subscription`, failing loudly for a non-openai provider or a bad auth file before any call. Embedding stays pinned openai key auth throughout.

**Done when:**
- R-ETV6-57CQ — per-case×repeat stderr progress lines; none of it on stdout or in the scorecard — covered by a tagged test.
- R-EWAY-WQU4 — all five providers construct with their env key; unknown provider and missing key each fail naming the offender — covered by a tagged test.
- R-EXIV-AIKT — `auth=sub` + openai + valid token-response fixture constructs the subscription-backed provider; non-openai `auth=sub` and a missing/malformed auth file fail naming provider/path — covered by a tagged test.
- The suite is green per design Conventions.
