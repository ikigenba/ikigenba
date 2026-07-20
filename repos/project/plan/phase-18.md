# Phase 18 — owner-id keying: rebuild tables, rekey scoping, follow the payload

*Realizes design Decision 2 (data model & migrations), 3 (webhooks intake),
4 (repo lifecycle), 6 (runner-side github I/O), and 7 (MCP surface). Depends on
no pending phase (the appkit `X-Owner-Id` gate flip is already shipped).*

This is the repos slice of the suite-wide owner-id conversion
(`docs/owner-id-design.md`, `docs/owner-id-plan.md` phase 3). After appkit's
hard flip, `server.Identity` carries `OwnerID` (the stable key) alongside the
display headers, and the identity gate refuses on an empty `X-Owner-Id`. This
phase converts repos to key on that id.

What gets built (the observable end state):

- **Migration** (`internal/db/migrations/`): ONE new timestamped migration via
  `bin/create-migration repos <name>` that drops and recreates `repos` and
  `sessions` with `owner_id TEXT NOT NULL` (the sole scoping/provenance key) and
  a write-once `owner_email TEXT NOT NULL` display snapshot, indexes moved to
  `owner_id` (`idx_repos_owner`, `idx_sessions_owner`). Rows are dropped. The
  committed `create_repos`/`create_sessions` migrations are untouched; the
  outbox and `feed_offset` migrations are untouched.
- **Store** (`internal/repos/store.go`): `Repo`/`Session` carry `OwnerID` +
  `OwnerEmail`; `InsertRepo`/`InsertSession` persist both; `ListRepos`/
  `ListSessions` filter on `owner_id` only — no store method filters on
  `owner_email`.
- **Intake** (`internal/repos/intake.go`): decode the converted webhooks
  payload (`owner` replaced by `owner_id` + `owner_email`); attribute and key
  the enqueued session on `owner_id`, store `owner_email` as the snapshot; a
  payload with an empty/absent `owner_id` is not attributable → wrap
  `consumer.ErrSkip`.
- **Lifecycle** (`internal/repos/service.go`): `EnsureRepo`/`CloneRepo` record
  `owner_id` + `owner_email` (from the webhooks payload on the intake path, from
  `server.Identity` on the MCP path).
- **Runner/peer** (`internal/repos/ghpeer.go`, `protocol.go`): the loopback
  calls to the github service assert `X-Owner-Id` = the session's `owner_id`
  (the header github's appkit gate now requires) plus `X-Owner-Email` = the
  snapshot and `X-Client-Id`.
- **MCP** (`internal/mcp/`): handlers thread `id.OwnerID` for scoping and
  capture `id.OwnerEmail` as the snapshot on `clone`/`session_start`; owner is
  never read from args. Views expose no owner field (unchanged). Tests inject
  `X-Owner-Id` (plus `X-Owner-Email` where snapshots are asserted).

New Verification ids realized here (each covered by a clearly-named tagged
test in `*_test.go`):

- R-ICIJ-13TA — the full migration set applied over a seeded pre-conversion
  (email-only) `repos` and `sessions` leaves both tables with `owner_id` and
  `owner_email` NOT NULL, an `owner_id` index on each, and **zero rows**.
- R-IDQF-EVJZ — two owners sharing one `owner_email` but with distinct
  `owner_id`: `ListRepos(idA)`/`ListSessions(repo, idA)` return only idA's rows
  (email-filtering fails), and each row carries its `owner_email` snapshot
  verbatim.
- R-IEYB-SNAO — `clone`/`session_start` through the assembled MCP handler store
  `owner_id`/`owner_email` from the injected identity, not from tool args (a
  bogus owner arg is ignored); a different-`owner_id` caller gets `not_found`.
- R-IG68-6F1D — an `execute` delivery whose payload carries `owner_id`=X /
  `owner_email`=E enqueues a session keyed on X with snapshot E; the same email
  with a different `owner_id`=Y attributes to Y (email-attribution fails); an
  empty/absent `owner_id` creates nothing and returns `consumer.ErrSkip`.

Revised-in-place ids whose existing tagged tests this phase updates to the
`owner_id` world (behavior kept, keying/columns changed): R-EMGN-7X72 and
R-ENOJ-LOXR (D2: the `owner_id`/`owner_email` columns and round-trip),
R-ERC8-R05U (D3: provisioning/enqueue/nil, owner attribution moved to
R-IG68-6F1D), R-EYNN-1MM0 (D4: clone stamps `owner_id`/`owner_email`),
R-FDAF-MVIC (D6: assert `X-Owner-Id` to github), R-FN1M-P1FW and R-FPHF-GKXA
(D7: list scoped by `owner_id`). Tests inject `X-Owner-Id` throughout.

No ids are retired: every converted behavior keeps its stable id (rekeyed in
place); the genuinely new discriminating behaviors get the four fresh ids
above.

**Done when:**

- Each new id (R-ICIJ-13TA, R-IDQF-EVJZ, R-IEYB-SNAO, R-IG68-6F1D) appears
  verbatim as a tag on a test that asserts the behavior above, and the revised
  ids' tests assert the `owner_id` behavior.
- The store no longer scopes on the display column:
  `grep -nE 'owner_email *= *\?|WHERE[^;]*owner_email' internal/repos/store.go`
  prints nothing (scoping filters key on `owner_id`).
- From `repos/`: `go build ./...`, `go vet ./...`, and `go test ./...` all exit
  0 with no failures, and `gofmt -l .` prints nothing.
