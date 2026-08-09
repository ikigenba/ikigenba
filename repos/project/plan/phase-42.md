# Phase 42 — Client-side filter, sort, pagination, and copy (landing.js + goja)

*Realizes design Decision 25 (client controller). Depends on Phase 41.*

Build `share/www/static/landing.js` exactly as D25 shapes it: the pure
functions (`filterRepos`, `sortRows`, `paginate`, `nextSort`, `defaultState`,
`reduce`, `computeView`, `cloneURL`) exposed on `globalThis.ReposLanding`, and
the real, branchless `initController` that parses `#repos-data`, reveals the
hidden controls, wires the listeners (search input + Escape, Name-header sort,
Prev/Next, Clear, one delegated copy click on the table body), and transcribes
`computeView`'s view model onto the DOM — `textContent` only, SVG-namespaced
copy icons, the clipboard write with the `execCommand` fallback and the ~1.6s
`Copied` swap. `landing.html` gains the deferred
`<script src="static/landing.js">`. `github.com/dop251/goja` joins `go.mod`
test-only; the goja tests drive the repo-real shipped file.

**Done when:** the suite is green and these ids are covered by clearly-named
tagged tests:

- R-S7DH-V7ZG — subsequence match over the key, spanning the slash; substring
  implementations fail
- R-S8LE-8ZQ5 — case-insensitive matching
- R-S9TA-MRGU — empty query returns all rows in input order
- R-SB17-0J7J — survivors keep input order, never re-ranked
- R-SC93-EAY8 — `sortRows` asc by key; desc is the exact reverse
- R-SDGZ-S2OX — `nextSort` flips direction both ways
- R-SEOW-5UFM — `paginate` exact slice boundaries (34-row cases)
- R-SFWS-JM6B — `showPager` iff filtered count > 10
- R-SH4O-XDX0 — pageCount / label / range derivation and page clamping
- R-SICL-B5NP — empty vs no-match view-model kinds
- R-SJKH-OXEE — `defaultState` shape; `clear` is a total reset
- R-SKSE-2P53 — `setQuery`/`setSort` reset page; `setPage` does not
- R-SN86-U8MH — `cloneURL` joins origin + clonePath with exactly one slash
- R-SOG3-80D6 — `landing.js` shipped and referenced by the rendered page
