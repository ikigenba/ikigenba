# Phase 80 — The completion queue's ownership lease and partition enforcement

*Realizes design Decision 29 (the completion queue) — the store slice: the lease, the reclaim rule, asymmetric terminal writes, consumer scoping, and the sweep.*

`internal/completion/store.go` stops treating liveness as a property of boot and makes it a property of the row.

A new timestamped migration (`bin/create-migration prompts add-completion-lease`) adds `owner`, `lease_expires_at`, `reclaims`, and `error_code` to `completions`, plus the `completions_lease` index. The existing `create-completions` migration is not touched — committed migrations are immutable.

`NewStore` takes the process's **owner token** alongside its existing clock, so both are injectable. `Claim` takes the oldest eligible item that is `queued` or `running`-with-an-expired-lease, stamps owner and expiry, and prefers a consumer with no items currently running before falling back to oldest-first. Reclaiming an expired lease increments `reclaims`; at 2 the item is failed with an error naming abandonment and a written `finished_at`. A new `Renew` is owner-guarded and reports lease loss as zero rows affected rather than as an error. A new `Release` is guarded on owner **and** `status='running'`, and does not touch `reclaims`.

`Complete` and `Fail` become asymmetric: `Complete` lands from any owner while the row is not `done`; `Fail` lands only from the current owner and never overwrites a `done`. A losing write and a vanished row are both no-ops the caller can distinguish from an error. `Get` and `Ack` match on `(id, consumer)`, so one service can no longer read or delete another's items. `RequeueRunning` is deleted, together with the test that asserted the retired boot-requeue behavior — the id that named it is no longer minted by design, so no test may still carry it.

**Done when:** every id below is covered by a clearly-named test in `internal/completion/*_test.go` running against real temp SQLite through the real migration runner with an injected clock and injected owner tokens; `go test ./...` from `prompts/` passes; `gofmt -l .` emits no output; and the design-only coverage difference is empty.

- R-ZJKL-8UQY — claiming an expired lease resumes the item under a new owner and increments `reclaims` by exactly 1
- R-ZM0E-0E8C — an item whose lease was renewed past now is not claimable, and its owner and reclaim count are unchanged
- R-ZKSH-MMHN — an item reaching 2 reclaims is failed rather than requeued, names abandonment, carries `finished_at`, and reaches the inbox
- R-ZN8A-E5Z1 — three claim/release cycles leave `reclaims` at 0
- R-06QO-IHU5 — release is refused for a foreign owner and for a non-`running` row, leaving status, owner, and lease untouched
- R-032Z-D6M2 — a `Complete` from a losing owner lands while the row is not `done`, and is a no-op against a row already `done`
- R-04AV-QYCR — a `Fail` from a losing owner does not land, and never overwrites a `done`
- R-ZPO3-5PGF — a terminal write against a row deleted mid-execution reports no error and resurrects nothing
- R-00N6-LN4O — Ack of another consumer's id returns not-found and leaves the row for its owner to ack
- R-01V2-ZEVD — Get of another consumer's id returns not-found while the owner's Get succeeds
- R-096H-A1BJ — claim prefers a consumer with nothing running, and a consumer with running items but nothing queued neither wins nor blocks
- R-JOT1-SJRO — the sweep deletes a terminal item past the TTL, retains a younger terminal item and any `queued` item regardless of age, and deletes a reclaim-failed item once it ages past the TTL
