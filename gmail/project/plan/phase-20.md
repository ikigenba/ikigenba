# Phase 20 — Swap the attachment endpoint to the shared loopback guard

*Realizes design Decision 20 (structured MCP adoption — guard-swap slice:
R-8Q5R-R9T8). Depends on Phase 14 and Phase 17 (D16 — the `GET /attachment`
holder endpoint and its mount).*

This phase moves gmail's attachment endpoint off its hand-copied two-header
predicate and onto the shared chassis loopback guard, per D20 §4. It touches
`internal/gmail/attachment.go` (+ its test) and the mount in `cmd/gmail/main.go`.

Observable end state:

- `AttachmentHandler` carries **no** header guard: its opening
  `if r.Header.Get("X-Owner-Email") != "" || r.Header.Get("X-Forwarded-Proto")
  != ""` block is deleted; the handler is pure attachment logic (param read,
  MIME walk, byte fetch, 404/502 mapping — all unchanged).
- The route is mounted with `rt.HandleLoopback("GET /attachment",
  gm.AttachmentHandler(client))` (was `rt.Handle`), so `server.LoopbackOnly`
  fronts it and keys on `X-Forwarded-Proto` only. A bare loopback caller that
  asserts `X-Owner-Email` for its own identity is now served; a front-door
  request (bearing `X-Forwarded-Proto`) still gets a bare 404.
- The existing D16 handler tests (R-3G57/R-3HD3/R-3IKZ) continue to drive
  `AttachmentHandler` directly (no guard) and stay green unchanged.

**Done when:**

- `cd gmail && go build ./... && go vet ./... && gofmt -l . && go test ./...`
  all succeed with zero failures and no `gofmt` output (design Conventions:
  "the suite is green").
- R-8Q5R-R9T8 is covered by a clearly-named test, green: driving the route as
  mounted (`server.LoopbackOnly(gm.AttachmentHandler(fake))` over `httptest`), a
  `GET /attachment?message_id=…&part_id=…` with `X-Forwarded-Proto: https` gets
  a bare 404 and the fake `AttachmentSource` records zero calls, while the
  byte-identical request without `X-Forwarded-Proto` but with
  `X-Owner-Email: a@b.c` is served 200 with the attachment bytes.
- Structural:
  `cd gmail && grep -n 'X-Owner-Email' internal/gmail/attachment.go` returns
  empty output (the hand-copied predicate is gone), and
  `cd gmail && grep -n 'HandleLoopback' cmd/gmail/main.go` shows the attachment
  mount.
