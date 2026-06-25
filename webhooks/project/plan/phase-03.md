# Phase 3 — Event production (durable-before-ack)

*Realizes design Decision 5 (Event production). Depends on Phase 1 and Phase 2.*

Make the service an event-plane producer. Add `internal/webhooks/events.go`: the
single registered event type `webhook.received`, the `webhookReceivedPayload`
struct (`name`, `owner`, `received_at` RFC3339Nano from the injected clock,
`content_type`, and `body` as standard-encoding base64 of the raw request bytes),
and the exported `Events outbox.Registry` carrying that one type with a filled
sample. Wire the `Outbox` into the `Service` (the field introduced in Phase 2,
populated here via the producer seam).

Add `Service.Record(ctx, wh, contentType, body)`: in **one** transaction, append
the `webhook.received` event through `s.Outbox.Append(tx, …)` and
`Store.TouchLastTriggered(tx, wh.Name, now)`, then commit (rollback on any error);
`s.Outbox.Ring()` fires best-effort **after** commit. The owner stamped in the
payload is the **stored** `wh.OwnerEmail`. `Record` returns nil only once the row
is committed — the durable-before-ack guarantee the ingress handler (Phase 4)
relies on.

End state: `cd webhooks && go build ./... && go vet ./... && go test ./...` green,
with `Record` tests driving the real `eventplane/outbox` over real temp-file
SQLite.

**Done when:** design D5's Verification ids are each covered by a genuine
real-DB test and the suite is green —
- R-GTUZ-AIGW — one `Record` yields exactly one `webhook.received` row whose
  payload carries the name/owner/content_type and a `body` that base64-decodes to
  the exact submitted bytes (verified with a non-UTF8/binary body);
- R-GV2V-OA7L — the payload `owner` equals the stored `owner_email` even when the
  triggering call carried a different or absent owner;
- R-GWAS-21YA — after `Record` returns, the committed row is readable through a
  **freshly opened** connection to the same file (durability across reopen);
- R-GXIO-FTOZ — a single `Record` produces exactly one row and that webhook's
  `last_triggered_at` equals the payload's `received_at` (append + touch in one tx);
- R-GYQK-TLFO — the `Events` registry contains `webhook.received`, and an `Append`
  of an unregistered type errors with no row written.
