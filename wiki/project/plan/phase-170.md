# Phase 170 — The live `/embed` smoke discovers prompts through the registry

*Realizes design Decision 34 (embedding call site) — the R-15NY-IF46 slice —
and conforms `internal/llm` and `AGENTS.md` to D73's live-credentials facts.
Depends on no pending phase.*

The live `/embed` smoke `TestEmbedAgainstLivePrompts`
(`internal/llm/embed_live_test.go`, tagged `//go:build live`) resolves the
prompts base URL from `registry.BaseURL("prompts")` — the same discovery the
composition root (`cmd/wiki/main.go`) already uses — instead of reading
`WIKI_LIVE_PROMPTS_URL` from the environment. The test keeps its `R-15NY-IF46`
tag and its assertions unchanged: an `Embed` call against the real prompts
`/embed` returns one vector of length 512 per input. `registry` is already a
module dependency (`go.mod` carries `registry v0.0.0` with a `replace` to
`../registry`), so no new dependency and no out-of-tree change is needed.

After the change `WIKI_LIVE_PROMPTS_URL` appears nowhere in the wiki tree
outside `project/`: the test no longer reads it, and `AGENTS.md`'s Tests
section drops it from the live-credentials line — leaving `OPENAI_API_KEY` as
the sole live credential and stating that the `/embed` smoke needs a **running**
prompts on loopback, discovered through the registry, which holds the provider
key.

**Done when:**

- **R-15NY-IF46** — `internal/llm/embed_live_test.go` obtains the prompts base
  URL from `registry.BaseURL("prompts")` and reads no environment variable for
  it; the `/embed` assertion (one length-512 vector per input against the real
  endpoint) is unchanged and still tagged `R-15NY-IF46`. This is a **live** id,
  exercised only under `go test -tags live ./...` with a running prompts; the
  build-loop bar for it is that the tagged live file compiles under the tag
  (`go vet -tags live ./...` succeeds).
- From `wiki/`, `grep -rn 'WIKI_LIVE_PROMPTS_URL' . --exclude-dir=project
  --exclude-dir=.git` returns **no** matches — the variable is gone from both
  the test and `AGENTS.md`.
- The default gate is green: `go build ./...`, `go vet ./...`, `gofmt -l .`
  (no output), and `go test ./...` all succeed with zero failures.
