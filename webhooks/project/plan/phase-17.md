# Phase 17 — Per-hook verification schemes: `bearer` and `github-hmac`

*Realizes design Decision 17 (verification schemes). Depends on Phase 16.*

One new timestamped migration (`bin/create-migration webhooks <name>`) adds
`verification` (NOT NULL DEFAULT 'bearer') and nullable `secret` to the
`webhooks` table; the Store threads both. `create` accepts and surfaces the
optional `verification` scheme (`bearer` | `github-hmac`, unknown →
`validation`); `list` surfaces it; `rotate` re-mints for either scheme. The
ingress handler dispatches on the stored scheme: bearer byte-identical to
today; `github-hmac` reads the capped body first, verifies
`X-Hub-Signature-256` (HMAC-SHA256 over the raw bytes, constant-time
compare), preserves the uniform 404 on every failure, and records the event
with the two-key `headers` allowlist (`x-github-event`, `x-github-delivery`)
in the payload — bearer payloads unchanged.

**Done when:** R-G7RX-751P, R-G8ZT-KWSE, R-GA7P-YOJ3, R-GBFM-CG9S,
R-GCNI-Q80H, R-GDVF-3ZR6, and R-GF3B-HRHV are each covered by a
clearly-named test, and the suite is green per design Conventions
(`go build ./...`, `go vet ./...`, `go test ./...` clean from `webhooks/`).
