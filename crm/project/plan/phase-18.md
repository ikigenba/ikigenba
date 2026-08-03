# Phase 18 — Correlation-id adoption: fragment forwarding and context-carrying event appends

*Realizes design Decision 20 (correlation-id adoption).*

**Cross-workspace dependency.** This phase cannot land before the shared modules
are built: `appkit` must carry the correlation middleware (read-or-mint,
context accessor) and the telemetry recorder, and `eventplane` must carry the
`correlation_id` outbox column and the context-taking `Append`. crm builds
against them as replace-siblings, so `go build ./...` fails until both are in
place. Build order for the suite is registry/root → appkit and eventplane →
telemetry → dashboard → the remaining services, of which this is one.

**What gets built.**

- `crm/etc/nginx.conf` — the shipped fragment gains, on each of the three gated
  proxying locations (`= /srv/crm/`, `/srv/crm/static/`, `/srv/crm/`), the
  `auth_request_set $crm_correlation $upstream_http_x_correlation_id;` capture
  and the `proxy_set_header X-Correlation-Id $crm_correlation;` overwrite; and
  on the ungated PRM bootstrap location
  (`= /srv/crm/.well-known/oauth-protected-resource`) the
  `proxy_set_header X-Correlation-Id "";` strip. The non-proxying locations
  (`= /srv/crm/feed`, `@crm_authn_500`) are untouched.
- `crm/internal/crm/service.go` — the contact-event emit loop inside
  `Service.Save` calls `s.Outbox.Append(ctx, tx, ev)`. The surrounding logic,
  the four event kinds, the payload structs, and the reflection `Registry` are
  unchanged; no payload gains a correlation field.
- The existing fragment-content test package grows the D20 assertions; a
  package-level test drives `Service.Save` over a real temp SQLite and reads the
  outbox rows back.

Everything else crm gets from telemetry — MCP `request` records, plain-HTTP
request records, param capture, `lifecycle` start/stop — arrives from the
rebuilt chassis and is **not** re-proven here.

**Done when:**

- `R-X9B0-30E7` — a test reading `crm/etc/nginx.conf` from disk asserts each of
  the three gated locations carries both the
  `auth_request_set $crm_correlation $upstream_http_x_correlation_id;` capture
  and the `proxy_set_header X-Correlation-Id $crm_correlation;` overwrite.
- `R-XAIW-GS4W` — the same test asserts the ungated PRM location carries
  `proxy_set_header X-Correlation-Id "";` with the empty literal.
- `R-XCYP-8BMA` — the same test asserts the fragment's count of
  `proxy_set_header X-Correlation-Id` lines equals its count of
  `proxy_pass http://127.0.0.1` lines (4 == 4).
- `R-XE6L-M3CZ` — a test over a real temp migrated SQLite asserts a contact
  `Save` driven with a context carrying correlation id `X` writes outbox rows
  whose `correlation_id` column is exactly `X`, and that the same save under a
  bare context writes an empty `correlation_id`.
- The suite is green per design's *Conventions*: `cd crm && go build ./...`,
  `cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output), and
  `cd crm && go test ./...` all succeed with zero failures.
