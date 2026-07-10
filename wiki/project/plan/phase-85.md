# Phase 85 — The inline-link machinery: positional matcher + markdown-safe linkifier (`internal/wiki`)

*Realizes design Decision 58 (inline first-occurrence subject links). Depends on Phase 71 (D12 `Mentions`/`SubjectKeys`/`Path`, D25 aliases, `AliasStore.ListAll`, `Service`).*

Add the read-time inline-linking machinery to `internal/wiki`, reusing D12's exact match semantics (normalized whole-run, alias-aware, canonical resolution) but reporting raw byte offsets so a surface run can be wrapped in place:

- A **positional matcher** producing the `FirstMention` (byte span `[Start,End)` in the original body + the canonical `Subject`) for each subject's earliest **eligible** occurrence — a whole hyphen/edge-bounded run matching one of the subject's `Keys`, lying entirely outside a skip-region.
- The pure **`LinkFirstMentions(body string, others []SubjectKeys, base, excludeID string) string`** — wraps each subject's first eligible run as `[surface](base + Path(canonical))`, verbatim surface text; one link per canonical subject; **skip-regions** (inline code span, fenced code block, existing markdown link/autolink) are never wrapped and don't consume the first occurrence; overlaps resolve leftmost-start, longest-run on a tie; `excludeID`'s subject never linked. Pure/deterministic (no map-iteration dependence).
- The **`Service.LinkifyMentions(ctx, text, base, excludeID string) (string, error)`** loader — loads the current `SubjectKeys` (all subjects ∪ D25 alias keys, via `AliasStore.ListAll`, exactly as `MentionsIn`/`PageWithLinks` do) and calls `LinkFirstMentions`. Read-only, no write path.

No surface is wired here — this phase delivers the seam; Phases 86/88 consume it.

**Done when** the suite is green (`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`, `bin/check-migrations wiki`) and each id below is covered by a clearly-named test:

- R-82BY-EKDH — first occurrence only, one link per subject (later occurrences plain). *(pure, table-tested)*
- R-83JU-SC46 — link text is the verbatim surface run, not the canonical name (`"Vasari's fresco"` → `"[Vasari](…)'s fresco"`). *(pure)*
- R-84RR-63UV — an alias surface run links to the **canonical** subject URL, not the alias/folded path. *(pure)*
- R-877J-XNC9 — a match inside an inline code span / fenced code block / existing link is not wrapped and does not consume the first occurrence; a skip-region-only subject gets no inline link, surrounding markdown intact. *(pure)*
- R-88FG-BF2Y — overlapping runs of different subjects resolve leftmost-start / longest-on-tie, deterministically regardless of slice order. *(pure)*
- R-89NC-P6TN — `excludeID` suppresses that subject's own name while others still link; `""` links all. *(pure)*
- R-8AV9-2YKC — the positional matcher links only a whole edge-bounded run (`cat` within `category` yields no link). *(pure)*
- R-8C35-GQB1 — `LinkifyMentions` is alias-aware and composes the absolute base end to end over a **real temp SQLite** (folded short name → canonical absolute URL; no alias row → text unchanged). *(integration: real temp SQLite)*
