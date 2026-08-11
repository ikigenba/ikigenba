# Phase 154 — Ask retrieval honesty: drop stale hits, pin inside the scope wall

*Realizes design Decisions 86 (stale-hit drop-and-continue) and 87 (scoped exact-name pin).*

Fixes the ask-side retrieval behaviors in `internal/ask` and `internal/retrieve`. End state: `gatherPages` treats `ErrSubjectNotFound` from the subject lookup exactly like a missing page row — skip the hit, continue; every other error still propagates; zero surviving pages still yields the honest-empty answer. The hybrid retriever's pin path resolves candidates with the ask's scope (`ResolveByName(ctx, scope, candidate)` — no more one-argument `default` fallback) and builds its pinned `Hit` with the subject id, never the page row id.

**Done when:** the suite is green (design Conventions) and each of these ids is covered by a clearly-named test:

- R-RDLX-FD2O — an ask over mixed live/stale hits succeeds, grounded in and citing only the live pages; no error reaches either surface.
- R-RETT-T4TD — an ask whose hits are all stale returns the honest-empty answer, never an error.
- R-RG1Q-6WK2 — a candidate naming a subject in the asked (non-`default`) scope pins it first.
- R-RH9M-KOAR — a candidate whose subject exists only in another scope (including `default`) pins nothing there — the wall holds both directions.
- R-RIHI-YG1G — a pinned hit carries the subject id and survives to the answer: the pinned subject's page is gathered and cited end to end.
