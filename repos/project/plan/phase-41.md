# Phase 41 — The landing page lists the live repositories (server render + layout)

*Realizes design Decision 24 (landing listing: server render + layout).*

Build the server side of the repository-listing landing page. The store gains
`ListAllRepositories` (all owners' live rows, kind-then-name order). A landing
handler — constructed at the composition root with the Store, the Custody, and
the version — replaces the static template render at `GET /{$}`: it resolves
each live repository's `main` tip through `Custody.Refs` and renders
`share/www/landing.html` with the D24 view model (`Key` = `kind/name`, `Sha` =
first 7 hex or empty, `ClonePath` = `/srv/repos/git/<kind>/<name>.git`).

`share/www/landing.html` is rewritten to the D24 layout: Home link, eyebrow
`Git custodian`, single 28px heading line carrying name + version, the new
lead paragraph, filter bar above the table, the Name/Sha/copy table with the
`data-sort-key="key"` hook and `sr-only` copy header, the hidden no-match
message, the pager below, the `#repos-data` JSON island, hidden-until-JS
controls with the `[hidden] { display: none !important; }` guarantee, the
`aria-sort` caret/pointer CSS, Carbon `.input`/`.btn` control styling, and the
replicated `.copy-btn`/`.copy-label`/`.is-copied` width-stable copy control —
all composed from the existing `tokens.css` custom properties, no new token
values. The old canonical card markup, its dead CSS, and the stale v1 text
fields go.

Design deleted the old landing's canonical byte-parity behavior and its
minted id (prefix `R-G2WB`); this phase removes that id's tagged test
alongside the rewrite.

**Done when:** the suite is green (design Conventions) and these ids are
covered by clearly-named tagged tests:

- R-RP30-4NV1 — `ListAllRepositories`: all live rows across owners, ordered,
  archived excluded
- R-RQAW-IFLQ — single heading line; no Service/Version/API card; no
  `POST /mcp` substring
- R-RRIS-W7CF — exact eyebrow and lead texts; stale v1 texts absent
- R-RSQP-9Z34 — document order lead < controls < table < pager; no-match
  element; Name sortable, Sha not; `sr-only` copy header
- R-RTYL-NQTT — controls hidden in server render; `[hidden]` rule with
  `!important`
- R-RV6I-1IKI — `aria-sort` caret CSS + pointer affordance; no server-stamped
  `aria-sort`
- R-RWEE-FAB7 — every interactive control's classes resolve to rules
- R-RXMA-T21W — per-row copy button: hidden, correct `ClonePath` data
  attribute byte-identical to the island's, styled
- R-RYU7-6TSL — rows show `kind/name` and exactly the first 7 chars of the
  real `main` sha (real git substrate)
- R-S023-KLJA — a no-commit repository renders `—` and island `sha: ""`
- R-S19Z-YD9Z — `#repos-data` island shape; `[]` when empty
- R-S2HW-C50O — an archived repository appears nowhere in page or island
- R-S4XP-3OI2 — home link `href="/"`
- R-S65L-HG8R — empty listing renders safely with name and version

and `grep -rn 'R-G2WB' --include='*_test.go' .` (from `repos/`) returns
nothing — the retired byte-parity id's tag is gone from the tests.
