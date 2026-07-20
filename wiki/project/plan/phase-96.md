# Phase 96 — owner_email → owner_id conversion (the suite owner-id keying)

*Realizes design Decision 3 (jobs table + rebuild migration), 25 (aliases table + rebuild migration), 10 (ingest owner capture), 16 (jobs result owner pair), 27 (merge owner capture + merges result owner pair), and the seam/prose touch-ups in 4 (Ingest signature), 26 (alias carries job owner pair), 61 (jobs/merges output schemas + identity-gate validation site), and 62 (origin from the email snapshot). Depends on the shipped appkit chassis (D13: `server.Identity{OwnerID, OwnerEmail, OwnerName, OwnerPicture, ClientID}`, gate 401s on empty `X-Owner-Id`).*

The suite conversion (`docs/owner-id-design.md`) reaches wiki. wiki does **not**
isolate or scope any query by owner — owner is pure attribution — so this phase
carries **no** scoping rekey and **no** consumer-seam rekey (wiki has no
event-consumer seam; the agentkit seam the suite inventory named is retired).
What it does: rename the two owner-carrying columns to the suite-standard pair,
capture the owner **id** as the durable attribution key with the email as a
write-once snapshot, and expose both in the MCP results.

What gets built (observable end state):

- **One new timestamped migration** (`bin/create-migration wiki <name>`; the
  frozen migrations are untouched) rebuilds `jobs` and `aliases` empty with
  `owner_id TEXT NOT NULL` + `owner_email TEXT NOT NULL` in place of `jobs.owner`
  and `aliases.created_by`. `jobs` is recreated in its effective current shape
  (D26's `kind`/`merge_winner_id`/`merge_loser_id` carriers included); `aliases`
  keeps its `subject_id` FK (`ON DELETE RESTRICT`) and `aliases_subject` index.
  Rows are dropped (email cannot be mapped to an owner id; wiki holds no live
  data in the migration window). **`subjects.created_by` is NOT touched — it
  holds a job id, not an owner.** The legacy, unused `wiki_ingest`/`wiki_jobs`
  tables (frozen `002_wiki.sql`, agentkit-era, no live reader) are left as-is.
- `internal/wiki`: `Job` gains `OwnerID`/`OwnerEmail` (drop `Owner`);
  `Service.Ingest(ctx, ownerID, ownerEmail, text, title, tags)` stamps both;
  `MergeSubjects` reads `Identity.OwnerID`/`OwnerEmail`; `Alias` gains
  `OwnerID`/`OwnerEmail` (drop `CreatedBy`); the merge execution copies the
  job's pair into the alias row; `jobAttribution` sources `origin` `user:<…>`
  from `job.OwnerEmail` (grammar unchanged).
- `internal/mcp`: `ingest`/`merge`/`ask` identity gate keys on
  `Identity.OwnerID` (not `OwnerEmail`); the `jobs` and `merges` result items and
  their `outputSchema`s expose `owner_email` **and** `owner_id` (no `owner`/
  `created_by` field).
- Tests inject `X-Owner-Id` wherever they inject identity (plus `X-Owner-Email`
  where a snapshot/origin is asserted).

**Done when:**

- `cd wiki && go build ./... && go vet ./... && gofmt -l .` (no output) and
  `go test ./...` all pass with zero failures (design Conventions' green bar).
- Each new Verification id is covered by a genuine, clearly-named test:
  - R-1O8B-FNX4 — full migration set over a seeded pre-conversion `jobs` row
    leaves `jobs` with `owner_id`/`owner_email` NOT NULL, no `owner` column, zero
    rows (real temp SQLite; `PRAGMA table_info(jobs)` + row count).
  - R-1PG7-TFNT — same for `aliases`: `owner_id`/`owner_email` NOT NULL, no
    `created_by` column, zero rows.
  - R-1QO4-77EI — `ingest` writes `jobs.owner_id`/`owner_email` from the
    identity's `OwnerID`/`OwnerEmail`; an id-present/email-empty identity still
    writes the row (`owner_email == ""`) — the row keys on `X-Owner-Id`.
  - R-1RW0-KZ57 — `merge` stamps the job's `owner_id`/`owner_email` and the
    committed alias carries them (job → alias); id-present/email-empty still
    folds.
  - R-1T3W-YQVW — `jobs` result items carry `owner_email` **and** `owner_id`,
    no single `owner` field.
  - R-1VJP-QADA — `merges` result items carry `owner_email` **and** `owner_id`,
    no `created_by` field.
- The four revised ids keep their tags with updated assertions and pass:
  R-MZLQ-34IK (now `RequireIdentity` mount only), R-E198-AY8Z (now
  `RequireIdentity` mount only), R-E2H4-OPZO (owner field dropped from its item
  enumeration), R-183R-9YLK (origin from `owner_email`).
- No ids are retired, so the retired-id grep is vacuously empty. The identity
  gate no longer keys on the email:
  `grep -rn 'OwnerEmail == ""' wiki/internal/mcp/` returns **nothing**.
- The ikispec coverage check prints nothing (every current design id is realized
  by a tagged test or carried here):
  ```
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' wiki/project/design/*.md | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project wiki/) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' wiki/project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```
