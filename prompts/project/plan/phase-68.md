# Phase 68 — `repos` joins the trigger sources

*Realizes design Decision 58 (repos as a trigger source) and the rewritten
Verification line of Decision 24 (`R-6KLN-PNOI`). Depends on Phase 67.*

`knownFamilies` gains `"repos": {"push"}`, `sources` in `cmd/prompts/main.go`
gains `repos`, `etc/manifest.env`'s `CONSUMES` gains `repos`, and the feed URL
resolves from `PROMPTS_REPOS_FEED_URL` defaulting through
`registry.BaseURL("repos")`. No consume-side code changes: the subscription is
the ordinary broad `**` in-edge.

**Done when:**

- `R-SBKW-89FH` — `repos:push/prompts/daily-digest` and `repos:push/**` are
  accepted and store `source = "repos"`; `repos:nosuchkind/**` is rejected;
  `github:push/**` is rejected with an error naming the known set including
  `repos`.
- `R-SCSS-M166` — `consume.Subscriptions` yields a `repos` subscription with
  `Filter` exactly `**`; the committed `etc/manifest.env` `CONSUMES` contains
  `repos`; the resolved feed URL honours `PROMPTS_REPOS_FEED_URL` and otherwise
  equals the registry-derived address, with no port literal in source.
- `R-6KLN-PNOI` — the well-formedness discrimination still holds with the
  widened source set, and `repos:push/**` passes well-formedness.
- The D49 manifest/Spec drift oracle (`R-M51H-QWOL`, `R-M69E-4OFA`) stays green.
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
