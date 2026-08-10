# Phase 141 — The public tier serves ask; one Ask label on both tiers

*Realizes design Decisions 78 (both tiers serve ask) and 79 (the Ask label), with the D80/D43/D44 truings they carry.*

The web surface (`internal/web` + `share/www` templates) serves the full
D43/D44 experience on the public tier: the public route registrations share the
private tier's handler wiring (`Asker`, `Mentioner`, `Linkifier` injected once,
reachable from either tier), `GET /public/{scope}/?q=…` runs `Ask` and renders
`answer.tmpl`, and the keyword-only public search results page is gone. The
handler forwards `X-Owner-Email` verbatim as `owner` — empty on the ungated
public tier — and an empty owner asks normally. The question form's submit
button reads `Ask` with `data-busy-label="Asking…"` and `aria-label`
`Ask this wiki` on both tiers; the `Search`/`Searching…` labels retire.
Tests tagged with the retired ids R-HVNQ-OKML, R-HWVN-2CDA, R-HPXQ-UTP7, and
R-VCIV-8Y97 are deleted with the behaviors; existing D80/D44 shape tests that
exercised the public search-results state are updated in place to the public
ask-result state under their unchanged id tags.

**Done when:**

- R-4Q8B-N9SX — public-tier `?q=` invokes `Ask` exactly once with the path
  scope and decoded question and renders the answer page; private tier
  likewise — covered by a tagged test.
- R-4RG8-11JM — `owner` passes through: the header value on the private tier,
  the empty string (an ordinary 200 answer page) on the public tier — covered
  by a tagged test.
- R-4TW0-SL10 — the public-tier question form says `Ask`/`Asking…`/`Ask this
  wiki` — covered by a tagged test.
- No test file carries the tags R-HVNQ-OKML, R-HWVN-2CDA, R-HPXQ-UTP7, or
  R-VCIV-8Y97 (`grep -rn 'R-HVNQ-OKML\|R-HWVN-2CDA\|R-HPXQ-UTP7\|R-VCIV-8Y97'
  --include='*_test.go' .` returns nothing).
- The suite is green per design's Conventions (`go test ./...` from `wiki/`).
