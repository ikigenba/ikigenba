# appkit — Live verification (out of loop)

This doc owns the acceptance checks the autonomous `ralph` build loop **cannot**
perform: appkit's **live-box** Verification ids, whose correctness depends on
the deployed suite on the real box — the live dashboard deriving its OAuth-AS
resource set on the real `/opt` tree, and a real strict MCP client driving the
deployed services. These are run by a **human operator** against the live box
(`int.ikigenba.com`), never by `ralph`.

## Why these are out of loop

The loop's `verify` runs only `go build ./...`, `go vet ./...`, `gofmt -l .`,
and `go test ./...` from `appkit/` — offline, no box access, and the unattended
loop is forbidden to `ssh int` / `opsctl` / mutate the box. Each id below has a
pass-predicate only the deployed suite can express: a filesystem end-state on
the live `/opt` tree observed through the live dashboard, or an external strict
schema-validating client accepting the deployed `tools/list`. An id whose only
pass-predicate is a live-box command could never clear a pending `⬜` line and
would make the loop non-convergent. So design mints them as live-substrate ids,
**no plan phase schedules them as loop-gating work**, and they are deliberately
**absent from `STATUS.md`**; design still owns each under its Decision. The
offline suite proves construction; this doc proves the real thing runs.

These two ids are exactly the appkit design ids that do **not** appear as tags
in any `*_test.go` — the out-of-loop set. The offline `comm -23` coverage
ratchet (`project/loops/verify.md`) reads its live-tracked set from this doc's
`### D<n> — `R-id`` check headers; these ids are its expected, documented
remainder.

## Preconditions

- The box is reachable: `ssh int true`.
- The suite is deployed at the version under test (`deploy.md`); checks here
  observe the deployed suite, so run them **only on explicit instruction to
  deploy/verify**, never as unattended work.

## Checks

### D4 — `R-YU3O-6CQP` — box end-state stands on `current` alone

**Positive.** With the `current`-reader dashboard deployed, no
`/opt/<app>/etc/manifest.env` sibling exists and every service still resolves
through `etc/current/manifest.env` alone:

```sh
ssh int 'ls /opt/*/etc/manifest.env'   # expect: no matches
curl -s https://int.ikigenba.com/services   # expect: every service listed
```

Pass: the sibling enumeration is empty, `/services` lists the full service set
(crm included), and a `/srv/<svc>/mcp` request is token-mintable (the service
is in the AS resource set). If a service drops from `/services`, a sibling was
still load-bearing — recovery is recreating its symlink to
`current/manifest.env`.

**Falsifiability.** The offline suite can prove the reader prefers
`etc/current`, but only the live box proves nothing else on the deployed tree
still needs the hand-placed sibling: the proof is the live dashboard deriving
the complete resource set with no sibling present.

*(One-time cleanup completed 2026-08-07: the mid-investigation bridge symlinks
and stale sibling copies — 11 paths — were removed from int as part of the
first run of this check.)*

### D8 — `R-ELE5-W5ML` — a strict MCP client accepts the deployed tool list

**Positive.** A strict schema-validating MCP client (Claude Code, or MCP
Inspector's strict mode) fetches the full `tools/list` from each deployed
`/srv/<svc>/mcp` without a schema-validation rejection, and each server reports
`connected` with its tools listed — **not** `connected · tools fetch failed`.
Then each service's `health` tool answers `status: ok` with the authenticated
caller identity.

Pass: every MCP-serving service is accepted with tools listed and healthy. A
`curl` 200 is **not** sufficient — curl does no schema validation.

**Falsifiability.** The construction guard (R-EIYD-4M57) and the open-object
`reflection` schema (R-EK69-IDVW) *model* the strict-client rule; only a real
strict client against the deployed suite proves the emitted `tools/list` is
actually accepted. Because all services share the appkit chassis, the check
ranges over every deployed service, not one.

## Recording the result

These are manual gates. Record each run (date, the `main` commit the deployed
suite/box state was verified against, the positive observation); they are
**not** tracked by `project/plan/STATUS.md` (the autonomous loop's manifest
only). A lightweight running record follows.

| id | last verified | commit | observed |
|---|---|---|---|
| `R-YU3O-6CQP` (D4) | 2026-08-07 | 075afac1 | Removed all 11 `/opt/<app>/etc/manifest.env` siblings on int (9 stale copies + crm/telemetry bridge symlinks), restarted dashboard: `/services` still lists all 14 services, apex 200, `/srv/crm/mcp` 401 anonymous, authenticated crm MCP call minted — end state stands on `current` alone. |
| `R-ELE5-W5ML` (D8) | 2026-08-07 | 075afac1 | Claude Code (strict schema-validating client) fetched and validated `tools/list` from all 13 MCP-serving services at `v*+823310ac`; none in `tools fetch failed`; every `health` returned `status: ok` with caller identity. |
