# Phase 18 — Error-taxonomy enrichment: `too_large` and `source_unavailable` sentinels

*Realizes design Decision 20 (error-taxonomy enrichment). Depends on Phase 17.*

Add `script.ErrTooLarge` and `script.ErrSourceUnavailable` to
`internal/script/model.go`; reclassify `Service.Import`'s two coarse arms
(the `maxImportBytes` cap wraps `ErrTooLarge`; a `Fetcher.Fetch` failure wraps
`ErrSourceUnavailable`, message text otherwise preserved); extend Phase 17's
`structuredError` ladder in `internal/mcp` with the two matching `errors.Is`
arms mapping to `mcp.ErrTooLarge` / `mcp.ErrSourceUnavailable`. The UTF-8 and
`source_path is required` rejections stay `ErrValidation`; `conflict` stays
unused (upsert semantics). No tool, schema, or success-shape change.

**Done when:**

- R-CBF4-AYEU — a named test proves `import` of a mirror file over 1 MiB
  returns `isError: true` with `structuredContent.code == "too_large"` (not
  `"validation"`), message still naming the byte count and limit.
- R-CCN0-OQ5J — a named test proves `import` with an injected failing `Fetcher`
  returns `structuredContent.code == "source_unavailable"` (not `"internal"`).
- R-CDUX-2HW8 — a named test proves `import` of invalid-UTF-8 bytes still
  returns `structuredContent.code == "validation"`.
- The scripts suite is green per design Conventions.
