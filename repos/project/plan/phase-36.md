# Phase 36 — The MCP tool surface

*Realizes design Decision 22 (the MCP tool surface). Depends on Phase 35.*

`internal/mcp/` gains the eight domain tools (`create`, `list`, `get`,
`rename`, `delete`, `merge`, `status_set`, `status_list`) plus `guide`, each
with a declared `outputSchema`, structured results mirrored as text, and the
closed error-code mapping; plus the Tier 0 `initialize` instructions and the
Tier 2 guide document. `delete` is archive and is idempotent on an
already-archived repository, and no destroy path exists.

This is also the surface **owning services** call: sites, prompts, and scripts
create, rename, and archive their artifacts' repositories by speaking MCP to
this endpoint over loopback with asserted identity headers, so the phase proves
that path explicitly and keeps every `inputSchema` inside agentkit's canonical
subset (prompts attaches repos as an MCP peer).

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-KSPC-80ID — `tools/list` advertises exactly the nine domain entries plus the
  chassis pair; every tool but `guide` declares a schema; the instructions carry
  the routing vocabulary and the guide pointer.
- R-KTX8-LS92 — `create` defaults to kind `code`, produces a clonable
  repository, conflicts on a duplicate, and rejects an unknown kind.
- R-KV54-ZJZR — `list` returns only the caller's live repositories and honors
  the kind filter.
- R-KWD1-DBQG — `get` reports the real head sha, the real branch list, and the
  door `clone_url`; unknown is `not_found`; empty is a success with empty
  fields.
- R-KXKX-R3H5 — `rename` moves the repository with its history and conflicts on
  a taken key.
- R-KYSU-4V7U — `delete` archives recoverably, frees the key, is idempotent on
  a second call, and a source scan finds no removal call against the git or
  archive roots.
- R-L00Q-IMYJ — every success result's `structuredContent` validates against the
  tool's advertised schema and matches its mirrored text block.
- R-L2GJ-A6FX — every error result carries a code from the closed set, exercised
  across validation, not-found, conflict, and a status-blocked merge.
- R-JFAI-W00R — a loopback caller asserting its own identity headers reaches the
  tool table and its writes are keyed on the asserted `owner_id` (arguments
  ignored); an empty `X-Owner-Id` is refused; another owner gets `not_found`.
- R-JGIF-9RRG — no declared `inputSchema` contains `additionalProperties` at any
  depth.
