# Phase 19 — Subject addressing: the `type/slug` public path

*Realizes design Decision 11 (subject addressing). Depends on Phase 02 (the `internal/wiki` data model, `normalize`, and the domain stores).*

D11 introduces the public subject identifier — a `type/slug` path derived purely from `type` + `norm_name`, with no new column, migration, or write-path change. This phase builds the addressing seam in `internal/wiki` that D12 (links), D9 (ask citations), and D10 (MCP I/O) all consume.

**What gets built (the observable end state):**

- `internal/wiki`:
  - `slug(normName string) string` — pure, deterministic: each run of spaces or `/` becomes a single `-`, leading/trailing `-` trimmed; all other runes (including non-Latin letters) preserved. `norm_name` is already NFKC + casefold + trimmed + collapsed + diacritic-stripped (Phase 02 `normalize`), so `slug` only makes it path-safe.
  - `Path(s Subject) string` → `s.Type + "/" + slug(s.NormName)`.
  - `SubjectStore.GetByPath(ctx, path string) (Subject, error)` — splits `path` into `(type, slug)` and resolves by forward-compare (the subject whose `Type == type` AND `slug(NormName) == slug`); exact only, no fuzzy/alias matching. Zero matches → `ErrSubjectNotFound`; exactly one → the subject; two or more → `ErrAmbiguousPath`.
  - Exported sentinels `ErrSubjectNotFound` and `ErrAmbiguousPath`.
- Nothing is persisted and no migration is added — the path is a pure projection of existing columns.

**Done when:**

- R-ZO9U-QOT8 — a test asserts `Path(s) == s.Type + "/" + slug(s.NormName)`, that `slug` replaces runs of spaces/`/` with a single `-` and trims, preserving other runes, is deterministic, and that distinct non-colliding subjects yield distinct paths (table-tested directly).
- R-ZQPN-I8AM — round-trip: `GetByPath(ctx, Path(s))` returns exactly subject `s` by exact `type`+`slug` match; a variant/partial path does not resolve to `s` (against a real temp SQLite).
- R-ZRXJ-W01B — a path matching no subject returns `ErrSubjectNotFound` (clean miss, no panic, no wrong subject).
- R-ZT5G-9RS0 — two distinct same-type subjects whose `norm_name`s slug to the same path cause `GetByPath` to return `ErrAmbiguousPath`, never a silent pick.
- The suite is green per the design Conventions (`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`, `bin/check-migrations wiki`).
