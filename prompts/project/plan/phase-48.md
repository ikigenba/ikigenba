# Phase 48 — Bump agentkit to v0.16.0 across every consuming package

*Realizes design Decision 1 (module dependency), 3 (validation), 4 (provider factory), 5 (sandbox tools), 6 (suite discovery), 7 (runner), 9 (MCP schema), 19 (eager suite tools), 29 (`/complete`), 30 (`/embed`).*

**This phase is deliberately larger than one package, and it cannot be split.** The bump breaks compilation in `internal/prompt`, `internal/provider`, `internal/suite`, `internal/runner`, `internal/mcp`, `internal/tools`, and `internal/inference` simultaneously. A phase's done bar is a green suite, and no intermediate state between v0.7.0 and v0.16.0 compiles, so every consuming package moves in one unit. Read D1 first for the scope of the bump and research §2, §3, §4 for the API facts; the per-package Decisions carry the rest.

**End state.**

`go.mod` requires `github.com/ikigenba/agentkit v0.16.0` with no local replace, and the import-guard test asserts that version.

*Catalog.* Every `catalog` call site uses the offering-structured API: `Entry.Offerings` in place of `Entry.Provider`/`Pricing`/`Reasoning`, `Resolve` returning one `Resolution` with a `Coverage`, `Offer` as validation's routing gate, `ListCurated` in place of `ListByProvider`, and `Check(model, provider, value)` for reasoning acceptance. Provider strings cross into `agentkit.ProviderID` at each boundary. Validation derives an omitted provider from `Offerings[0]` and checks reasoning against the configured provider's offering (D3). The runner sources `Conversation.Pricing` from `Resolution.Offering.Pricing` and fails a run loudly on `Coverage: Unrouted` (D7). `/complete` and `/embed` follow the same resolution shape (D29, D30). `describe` renders per-offering vocabularies (D9).

*Identity.* Nothing calls the retired `Provider.Name() string`; provider logging and comparison read `Identity()`, and no code compares a provider id against the literal `"zai"` (the id is now `z-ai`). The factory no longer rejects an empty credential at construction — the error arrives from the send (D4).

*Suite tools.* `suite.Discover` returns `[]agentkit.MCPServer` built from the box inventory, with no network I/O, no blurbs, and no tool wrapping (D6). The runner assigns it to `Conversation.MCPServers` and sets no `DeferredTools`; the framing prompt drops its `load_tools` paragraph (D19). `mcpclient.Initialize`, `qualify()`, the `RawTool` wrapping and its dispatch closure, and the within-service duplicate guard are deleted, along with the tests that pinned them — including the retired ids `R-K32H-6XAV`, `R-K4AD-KP1K`, `R-9JNO-RZM2`, `R-9KVL-5RCR`, `R-9M3H-JJ3G`, `R-9NBD-XAU5`, `R-9OJA-B2KU`, `R-9PR6-OUBJ`, and `R-A69O-ATWI`. `internal/mcpclient` is removed entirely if nothing outside the deleted code imports it.

*Sandbox tools.* `toolkit.Read`'s inherited refusal of non-text files is left in place and pinned (D5).

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/`, and `gofmt -l .` is silent.
- `grep -rn 'RawTool\|DeferredTool\|ListByProvider\|\.Name()' --include='*.go' .` returns no agentkit-related hit outside `project/`.
- These ids are covered by clearly-named tests:
  - R-ZAC5-D0ZY — a model served by two providers with different reasoning vocabularies validates per route, in both directions.
  - R-ZBK1-QSQN — a run with an unset provider key fails naming the missing credential, while `provider.Build` returns no error for that config.
  - R-ZCRY-4KHC — `Read` refuses binary content by detected type and still reads an extensionless UTF-8 file.
  - R-ZDZU-IC81 — `Discover` returns one `MCPServer` per non-self peer, `Name` prefixed `ikigenba_`, `URL` the registry loopback `/mcp`, no self entry.
  - R-EF0V-TP9R — every entry's `Headers` carry `X-Owner-Id`, `X-Owner-Email`, and `X-Client-Id`.
  - R-ZF7Q-W3YQ — against a real `httptest` MCP peer publishing the bare verb `health`, the tool reaching the provider is named `ikigenba_<svc>_health` and dispatches `health` with headers intact.
  - R-ZGFN-9VPF — a peer whose `tools/list` errors fails the run with an error naming it.
  - R-ZHNJ-NNG4 — a config resolving to `Coverage: Unrouted` fails the run naming provider and model, with no round-trip attempted.
  - R-ZIVG-1F6T — the first `Request` carries sandbox **and** peer tools and no `load_tools`; `DeferredTools` is empty.
  - R-ZK3C-F6XI — the `System` string contains no `load_tools` and no individual service name.
  - R-ZLB8-SYO7 — a scripted provider calls a suite tool natively on its first round-trip and the run succeeds.
