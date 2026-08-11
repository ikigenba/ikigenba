# Phase 7 — Event production wired into every domain mutation

*Realizes design Decision 7 (events). Depends on Phase 6.*

The eventplane producer: outbox rows written in the same transaction as
each domain change — `created` on upload-commit and import, `updated` on
`update`/`set_visibility`, `deleted` on `delete` — with subject `/<id>`,
D7's payload shapes (owner pair; the content reference on `created`), and
the reflection families declaration. End state: every mutation the service
performs is a durable fact on the plane, and a refused operation leaves no
trace.

**Done when:** the suite is green and each of R-4NJZ-KUW8, R-4PZS-CEDM,
R-4R7O-Q64B, R-4SFL-3XV0 is covered by a test tagged with its id.
