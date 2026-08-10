# Phase 76 — Patch-semantics update and the no-empty-prompt guard

*Realizes design Decision 61 (patch-semantics `update`), with the in-place
reshaped D02 (whole-value config) and D53 (merge-then-commit update, sent-name
rename) it threads through.*

What gets built: `prompt.UpdateInput` moves to pointer fields (`*string` /
`*Config`; nil = omitted) and `Service.Update` becomes merge-then-validate-
then-write — the plane's current definition plus the row's name as the base,
sent fields overlaid per D61's per-field rules (omitted keeps; empty
`user_prompt` errors; empty `system_prompt` clears; empty `name` clears to the
ULID-derived key via the D53 rename path; a sent config replaces whole and
revalidates). The non-empty `user_prompt` guard lands at all three writers
(`Create`, `Update`, `import`). The MCP `update` handler in
`prompts/internal/mcp/tools.go` parses into the pointer shape and its
description states the merge rule; the `create` description notes `user_prompt`
must be non-empty. No schema migration; the input/output schemas are unchanged.
Existing tests constructing `UpdateInput` are updated mechanically to the
pointer shape; the behaviors behind existing ids (R-JUJ6-IJ40, R-ROES-YMCA)
are preserved as restated in D02/D53.

**Done when:**

- Each of these ids is covered by a clearly-named test tagged verbatim in a
  `*_test.go` file:
  - R-A8EU-5VUL — config-only update leaves name/name-key unchanged, zero
    `Rename`, commit batch exactly `["config.json"]`; user_prompt-only update
    commits exactly `["prompt.md"]` and leaves the stored config unchanged.
  - R-A9MQ-JNLA — update sending empty/whitespace `user_prompt` returns a
    `ValidationError`, zero plane calls, definition unchanged.
  - R-AAUM-XFBZ — update sending `name: ""` renames to the ULID-derived key,
    clears the row's name, and commits nothing.
  - R-AC2J-B72O — create with empty/whitespace `user_prompt` returns a
    `ValidationError`; no row, zero plane calls.
  - R-ADAF-OYTD — import of an empty/whitespace mirror file errors with zero
    plane calls; a prior import for another path stays intact.
  - R-AEIC-2QK2 — the `update` descriptor's description contains `omitted` and
    `unchanged`; the `create` descriptor still requires `user_prompt` and
    `config`.
- `go test ./...` from `prompts/` is green and `gofmt -l .` emits nothing.
