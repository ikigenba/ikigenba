# Phase 128 — Scope on the MCP surface: the required parameter + the four management verbs

*Realizes design Decision 75. Depends on Phase 127.*

Rewires `internal/mcp`: required `scope` on the nine content verbs with the typed `scope_not_found` error; the new `scopes`/`scope_create`/`scope_delete`/`scope_set_visibility` verbs (`WithScopeService` Option, D10 schema rules); the D57 `Instructions` and embedded guide extended with the scope model. Updates the membership tests for the edited D10/D57 ids.

**Done when:** the suite is green and these ids are covered by tagged tests:
- R-H8HN-EXJE — nine verbs require `scope` in schema and at call time; id-addressed/meta verbs carry none.
- R-H9PJ-SPA3 — unknown scope → typed `scope_not_found` naming the `scopes` tool, nothing written.
- R-HAXG-6H0S — `scopes` lists name-ordered rows, `default` present.
- R-HC5C-K8RH — `scope_create` → private scope, immediately usable.
- R-HDD8-Y0I6 — invalid-name and duplicate-name creation errors.
- R-HEL5-BS8V — `scope_delete` semantics incl. `default` refusal.
- R-HFT1-PJZK — `scope_set_visibility` flips both ways, enum published, bad value refused.
- R-HH0Y-3BQ9 — `jobs`/`jobs_count` partition by scope.
- R-HI8U-H3GY — `merges` partitions by scope.
- R-HJGQ-UV7N — guide/instructions cover scopes; guide-pointer rule intact.
- R-MUQ4-K1JS *(edited in place, D10)* — eighteen-verb membership.
- R-YF06-03HO *(edited in place, D57)* — nineteen-tool `tools/list`.
