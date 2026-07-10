# Phase 82 — `ask` MCP citations become fully-qualified front-door URLs

*Realizes design Decision 56 (ask citation URLs). Depends on Phase 80 (the `appkit/mcp` tool table + `NewHandler`).*

The MCP `ask` tool result's citations carry a fully-qualified front-door **`url`** instead of a bare relative `path`. In `internal/mcp`: `NewHandler` derives the page base once from the runtime — `strings.TrimRight(rt.AuthServer(), "/") + wiki.Mount` — and captures it on the `Handler` used by `Tools(...)` (a new unexported field, no new public `Option`). `askToolResult` takes that base and emits `{url, title}` per citation, where `url = pageBase + citation.Path`; the `path` field is gone from the `ask` result and the `title` is unchanged. `handleAskCall` passes the captured base.

The change is contained to the MCP result builder: `internal/ask` (`ask.Answer`/`ask.Citation`, whose `Path` stays the bare relative `type/slug`) and `internal/web` (the human ask page, which keeps rendering relative hrefs) are **not** touched. The honest-empty / empty-citations result is byte-for-byte as before (URL composition runs only inside the per-citation loop).

**Done when** the following are covered by clearly-named tests and the suite is green (`go test ./...` from `wiki/`, per design Conventions):

- R-Y7OR-PH1I — the `ask` result's citations carry a `url` field and no `path` field; for a citation whose `Path` is `entity/tsr` the result is `{url, title}` with the title preserved and no `path` key on any citation.
- R-Y8WO-38S7 — prod-vs-local correctness: with `rt.AuthServer() == "https://acct.ikigenba.com"` a citation for `entity/tsr` yields `url == "https://acct.ikigenba.com/srv/wiki/entity/tsr"`; with `http://localhost:8080` it yields `http://localhost:8080/srv/wiki/entity/tsr` — exactly one `/` between origin, mount, and path.
- R-YA4K-H0IW — scope containment: `ask.Asker.Ask` still returns `ask.Answer` citations whose `Path` is the bare relative `type/slug` (no origin, no mount), and the `internal/web` ask page still renders relative hrefs — the absolute-URL composition lives only in the MCP result builder.
