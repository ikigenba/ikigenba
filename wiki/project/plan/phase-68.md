# Phase 68 — `Service.Orphans`: the read-time orphan index

*Realizes design Decision 46 (the orphan index: subjects with zero inbound
mentions, read-time single-pass). Service layer only — `internal/wiki`. No web,
no migration, no LLM. Depends on Phase 49 (D12 `Mentions`/`SubjectKeys`,
`AliasStore.ListAll`) and the existing `SubjectStore`/`AliasStore`/`PageStore`.*

The home page (Phase 69) needs a list of **orphan** subjects — those nothing
links to — so every subject is reachable. A subject `X` is an orphan iff no
*other* subject's page body names `X` by its `norm_name` or by any D25 alias key
that resolves to `X` (the exact D12 match). This is "`X` appears in no page's
`MentionedBy`."

Add to `internal/wiki` a service method:

```go
func (s *Service) Orphans(ctx context.Context) ([]Subject, error)
```

computed **read-time in one inverted pass**, not stored (no `page_links` table,
no migration — D12's standing stance): build a `key → canonical subject` index
over all subjects ∪ all D25 alias keys (the same data D12 loads); scan each page
body **once**, collecting the canonical subjects it mentions (excluding the
body's **own** subject — a self-mention never rescues); return
`allSubjects − referenced`, ordered by public `type/slug` path (D11). Cost is
linear in the corpus, not the O(N²) of calling `PageWithLinks` per subject. A
match via an alias key counts toward the **canonical** subject, so a page naming
`X` only by a folded short name makes `X` non-orphan.

**Done when:** the suite is green (per design *Conventions* — `cd wiki && go
build ./... && go vet ./... && gofmt -l .` empty `&& go test ./...` and
`bin/check-migrations wiki`) and these ids are covered by clearly-named tests
against a **real temp SQLite** (migrated; composing `SubjectStore`/`AliasStore`/
`PageStore` as the service does):

- **R-QSR2-AFAD** — zero-inbound membership: with `A`'s page naming `B` and no
  page naming `C`, `Orphans` returns `C` and **not** `B`.
- **R-QTYY-O712** — self-mention does not rescue: a subject whose **only**
  occurrence (by its `norm_name`) is inside its **own** page body is returned as
  an orphan; add a second subject whose page names it and it is removed.
- **R-QV6V-1YRR** — alias mentions count as inbound (canonical): with an alias
  `vasari → W` and another subject `F` whose body names only bare "Vasari"
  (W's `norm_name` absent from `F`), `W` is **not** in `Orphans`; remove the
  alias row and, with no page naming `W`'s canonical name, `W` becomes an orphan.
- **R-QWER-FQIG** — deterministic order: `Orphans` returns its subjects ordered
  by public `type/slug` path, stable across calls on identical DB state.
