# Phase 43 — Tag the sliding-window limiter proof with R-I98E-62GS

*Realizes design Decision 35 (authn rate limiter), slice R-I98E-62GS. No
dependencies.*

D35 now mints R-I98E-62GS for the limiter's discriminating behavior: N allowed
per key per trailing window, the (N+1)th rejected carrying `WindowCount == N`,
capacity released once the window slides. `internal/ratelimit/ratelimit_test.go`
`TestLimiter_WindowSlide` already exercises allow → reject → slide → allow with
an injected clock; extend its rejection assertion to also check
`WindowCount == 2` (the configured limit) and add the `// R-I98E-62GS` tag
comment there. The sibling tests (`TestLimiter_AllowThenReject`,
`TestLimiter_KeyIsolation`, `TestLimiter_Disabled`, `TestLimiter_EmptyKey`)
stay untagged; no other logic changes.

**Done when:**

- `grep -rn 'R-I98E-62GS' --include='*_test.go' .` from the dashboard root
  prints exactly one line, in `internal/ratelimit/ratelimit_test.go` inside
  `TestLimiter_WindowSlide` — N allowed, the next rejected with the window
  count, allowed again after the window elapses.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` all passing, from `dashboard/`).
