# Phase 71 — The subject page (`GET /subject/{type}/{slug}`)

*Realizes design Decision 45 (the subject page) and Decision 42's subject-route
slice (`R-WC29-XALJ`, `R-WGXV-GDKB`). Touches `internal/web` (the subject route +
`SubjectView` rendering) and the `cmd/wiki/main.go` PageFinder adapter. No
migration, no LLM. Depends on Phase 69 (the handler skeleton + seams), Phase 50
(D29 `Resolver.ResolveByPath`), and Phase 49 (D12 `Service.PageWithLinks`).
Completes the `internal/web` package begun in Phase 69.*

Following any link lands on a subject page: the route rejoins `{type}/{slug}`
into the public path (D11), resolves it **alias-aware** (D29), and renders the
compiled prose with outbound + inbound link footers.

In **`internal/web`**: register `GET /subject/{type}/{slug}`; the handler rejoins
the segments to `"<type>/<slug>"`, calls `PageFinder.PageByPath`, and renders
`subject.tmpl` — an "ask another question" control (href `.`) at top, the title
(H1), the **HTML-escaped** prose body, and a footer built from the structured
`SubjectView` Refs: a **Mentions** (outbound) section before a **Mentioned by**
(inbound) section, each a list of subject links, each omitted when empty and the
whole footer omitted when both are empty. A `web.ErrNotFound` from the seam
renders a **styled Carbon 404 page** (HTTP 404, the control intact), never a
plaintext `http.Error`.

In **`cmd/wiki/main.go`**: add the `PageFinder` adapter — `Resolver.ResolveByPath`
→ on hit `svc.PageWithLinks(subject.ID)` → `web.SubjectView{Title, Body:
page.Body (stored prose, **no** `RenderFooter`), Outbound: refs(page.Mentions),
Inbound: refs(page.MentionedBy)}` (each `wiki.Ref{Path,Name}` →
`web.Ref{Href:"subject/"+Path, Name}`); `ErrSubjectNotFound` → `web.ErrNotFound`.
Pass `web.WithPageFinder(...)`.

**Done when:** the suite is green (per design *Conventions*) and these ids are
covered by clearly-named tests:

- **R-WC29-XALJ** — `GET /subject/entity/acme-corp` is dispatched to the subject
  handler (the `PageFinder` is reached — R-WGXV), while `GET /subject/onlyoneseg`
  is **not** matched (404 from the mux) and the `/subject/` route serves nothing
  for `/mcp`/`/health`/`/feed`/`/static/x`. *(httptest, stub)*
- **R-WGXV-GDKB** — `GET /subject/entity/acme-corp` invokes
  `PageFinder.PageByPath` exactly once with `"entity/acme-corp"`. *(httptest,
  stub)*
- **R-PH2F-47LB** — a stub returning `SubjectView{Title:"Acme Corp",
  Body:"Acme makes widgets."}` yields a 200 page containing the title and body.
- **R-PIAB-HZC0** — with `Outbound:[Beta]` and `Inbound:[Deal Q3]`, the page
  renders a "Mentions" section linking `subject/entity/beta` **before** a
  "Mentioned by" section linking `subject/event/deal-q3`.
- **R-PJI7-VR2P** — footer omission is symmetric: outbound-empty renders only
  "Mentioned by" (and vice-versa); both-empty renders prose with **no** footer.
- **R-PKQ4-9ITE** — every subject page (found and not-found) has an "ask another
  question" control whose href resolves to the base.
- **R-PLY0-NAK3** — a path the stub reports `ErrNotFound` yields HTTP **404**,
  `text/html`, the Carbon shell — not plaintext, not 200/500.
- **R-PN5X-12AS** — a `SubjectView.Body` containing `<script>x</script>` renders
  with the brackets escaped (`&lt;script&gt;`).
- **R-PODT-EU1H** — driving the web handler wired to the **real**
  `Resolver.ResolveByPath` + `Service.PageWithLinks` over a **real temp SQLite**:
  with subject `W` (`entity/giorgio-vasari`, has a page), a subject `F` whose page
  prose names "Vasari", and an alias `vasari → W`, `GET /subject/entity/vasari`
  (the folded path) returns 200 rendering `W`'s title/prose and lists `F` under
  "Mentioned by" — byte-equivalent to requesting `W`'s own path. *(integration:
  real temp SQLite; composes with D29 R-AL5R-PL1P, D12 R-1Z52-453N)*
