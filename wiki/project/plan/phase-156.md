# Phase 156 — The honest web ask failure: styled error page, logged cause

*Realizes design Decision 89 (honest ask failure on the web).*

Replaces the web ask handler's bare `http.Error(w, "ask wiki", …)` exits in `internal/web` (plus the new `error` template state in `share/www`). End state: a genuinely failed ask renders HTTP 500 whose body is the full site shell — header with the question retained, footer, suite tokens — around a plain outcome-level failure message with no internal error text; the handler's other internal-failure exits share the same posture; one structured error-level log line carries the underlying error, scope, and question; a render failure of the error page itself falls back to `http.Error`.

**Done when:** the suite is green (design Conventions) and each of these ids is covered by a clearly-named test:

- R-RM58-3R9J — a failing asker yields the styled 500 error page on both tiers, question retained, no underlying error text in the body.
- R-RND4-HJ08 — the same failure emits exactly one error-level log record with the underlying error, scope, and question.
