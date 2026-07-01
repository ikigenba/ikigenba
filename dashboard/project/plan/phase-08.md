# Phase 8 — Reword the logged-out login sub-line to "Sign in to access your services."

*Realizes design Decision 7 (the reworded `R-DB18-KEEP` behavior). Touches only
`dashboard/ui/html/index.html` (the `{{else}}`/logged-out `.signin-wall` branch)
and the assertion in `dashboard/internal/server/index_test.go`. No
`internal/server` logic change, no route, no migration, no view-model, no schema,
no CSS. Independent of all other work — a pure copy edit.*

D7 originally kept the sub-line **"Sign in to manage access tokens, connected
agents, and the box's MCP services."** verbatim. That line enumerated three
specific capabilities (tokens, agents, MCP), but the box now also serves per-service
HTTP pages (wiki and sites have viewable pages, with more to come), so the
enumeration is both stale and too narrow. D7 has been updated in place to keep the
same control-plane framing with a shorter, generic sub-line.

In **`ui/html/index.html`**, in the `{{else}}` (logged-out) branch, replace the
sub-line paragraph text:

```html
<p>Sign in to access your services.</p>
```

The heading (`Your account's control plane`), the `Sign in with Google` CTA, and
the name-origin colophon below the CTA are all unchanged. Nothing else in the
template moves.

Update the existing assertion in
**`dashboard/internal/server/index_test.go`** that pins the old sub-line
(currently `<p>Sign in to manage access tokens, connected agents, and the box's
MCP services.</p>`) to assert the new line `<p>Sign in to access your
services.</p>`.

**Done when:** the suite is green — `cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...`, and
`bin/check-migrations dashboard` all succeed with zero failures (per design
*Conventions*) — and this id is covered:

- **R-DB18-KEEP** — the logged-out `GET /` still renders the control-plane framing
  verbatim: the heading `Your account's control plane`, the sub-line **`Sign in to
  access your services.`**, and the `Sign in with Google` CTA linking to `/login`.
  The old enumeration line (`manage access tokens, connected agents, and the box's
  MCP services`) must be **absent**. *(httptest via `testServer`/`do`, logged-out
  `GET /`)*
