# Phase 126 — The scope registry: schema migration, name validation, and the store

*Realizes design Decision 74 (scope model), slice: the registry and schema.*

Builds the D74 foundation in `internal/wiki` + `internal/db`: the timestamped migration creating `scopes` (with `default` seeded private) and rebuilding `subjects`/`jobs`/`aliases` empty with `NOT NULL scope` columns and `UNIQUE(scope, norm_name)` subject identity; `ValidateScopeName`; and `ScopeStore` (`Create`/`Get`/`List`/`SetVisibility`/`Delete` with the typed errors `ErrScopeNotFound`/`ErrScopeExists`/`ErrScopeIsDefault`), including delete's one-transaction removal of a scope's content. No caller is rewired yet — existing service seams may compile against `"default"` internally this phase if needed to stay green.

**Done when:** the suite is green (design Conventions) and these ids are covered by tagged tests:
- R-GSMY-FWWD — migration yields `scopes` with the seeded private `default` and the visibility CHECK.
- R-GTUU-TON2 — `ValidateScopeName` accepts/rejects the stated table.
- R-GV2R-7GDR — `UNIQUE(scope, norm_name)`: same name coexists across scopes, duplicates within one rejected.
- R-GWAN-L84G — the rebuild migration leaves scoped, empty content tables (no scope column on `claims`/`pages`).
- R-H3M1-VUKM — `Delete` refuses `default` and removes a scope with all its content, others untouched.
- R-H4TY-9MBB — scoped operations on an unknown scope return `ErrScopeNotFound`, creating nothing.
