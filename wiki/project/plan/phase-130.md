# Phase 130 — The public tier's read-only states: browse, keyword search, no inference

*Realizes design Decision 78. Depends on Phase 129.*

Builds the public-tier page states in `internal/web`: the public home (search box + path-ordered subject browse list), the keyword-lane-only search results (with the plain no-matches state), and the public subject page — wired with no `Asker` seam at all — plus the private tier's re-rooted D43/D44 home/ask/orphans per scope.

**Done when:** the suite is green and these ids are covered by tagged tests:
- R-HUFU-ASVW — public home renders search form + the scope's subject links only.
- R-HVNQ-OKML — public `?q=` runs the keyword lane; no public route can invoke `Ask`.
- R-HWVN-2CDA — public no-matches state, form intact, no ask fallback.
- R-HY3J-G43Z — public subject page renders full markdown/links on the public tier.
- R-HZBF-TVUO — private tier keeps scoped ask + orphan index.
