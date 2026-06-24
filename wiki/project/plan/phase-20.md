# Phase 20 — Page links: read-time mention detection + markdown footer

*Realizes design Decision 12 (page links). Depends on Phase 19 (`Path`) and Phase 02 (the `internal/wiki` stores and `normalize`).*

D12 computes a page's links when it is read — no `page_links` table, no migration, no change to the ingest write path. This phase builds the read-time projection and the markdown footer renderer in `internal/wiki`.

**What gets built (the observable end state):**

- `internal/wiki`:
  - `Mentions(body string, others []Subject) []Subject` — pure, deterministic: returns each subject in `others` whose `NormName` occurs as a whole-word, exact-normalized match in `body` (normalize the body; match bounded by non-alphanumeric runes or string ends, so `cat` does not match `category`). The caller excludes the page's own subject from `others`.
  - `Ref struct { Path, Name string }` and `LinkedPage struct { Page; Mentions []Ref; MentionedBy []Ref }`.
  - `Service.PageWithLinks(ctx, subjectID string) (LinkedPage, error)` — reads the page for `subjectID`, then projects over the current corpus: outbound = `Mentions(thisBody, allOtherSubjects)`; inbound = every other subject with a page whose body mentions this subject. Targets deduped and ordered deterministically (by path); the page's own subject excluded from both directions.
  - `RenderFooter(body string, mentions, mentionedBy []Ref) string` — pure: appends the markdown footer (a `---` rule, then `**Mentions:**` then `**Mentioned by:**`, each a ` · `-joined list of `[Name](type/slug)`); outbound before inbound; an empty section omitted; the whole footer (including the rule) omitted when both are empty.
- The 12,000-char cap is untouched — nothing here writes to `pages`; the footer is render-time presentation only.

**Done when:**

- R-ZUDC-NJIP — a returned page's footer "Mentions" line links every *other* subject whose normalized name occurs (whole-word) in the body, each `[Name](type/slug)`, deduped and deterministically ordered.
- R-ZVL9-1B9E — the footer "Mentioned by" line links every subject whose own page body names this subject (inbound), by `type/slug` path.
- R-ZWT5-F303 — a page never links to its own subject, even when its own name occurs in its body (excluded from both sections).
- R-ZY11-SUQS — detection is exact normalized whole-word match only: a variant/partial name yields no link, and a substring inside a larger word (`cat` in `category`) is not a match.
- R-ZZ8Y-6MHH — with no outbound and no inbound links, no footer (and no `---`) is appended; the returned body equals the stored body.
- R-00GU-KE86 — "Mentions" renders before "Mentioned by"; when exactly one side is empty that section is omitted while the other still renders.
- The suite is green per the design Conventions.
