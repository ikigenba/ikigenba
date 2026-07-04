# github — Live verification (out of loop)

This doc owns the **one** acceptance check that the autonomous `ralph` build loop
cannot perform: proving the `@ikigenba` GitHub App credentials actually
authenticate against real GitHub. It is run by a **human operator**, after the
service is built and wired into the suite — never by `ralph`.

## Why this is out of loop

Design mints `R-DMUT-QF4A` (D2) as a **live-substrate** id: its correctness
depends on the real GitHub API accepting the app JWT and issuing an installation
token. A stub cannot falsify it (a stub accepts any JWT), and the loop's `verify`
runs only `GOWORK=off go test ./...` from `github/` with no network and no
credentials. A network-dependent test would also fail the loop's
"reproducible on identical repo state" bar. So this id is deliberately **not**
scheduled as loop-gating work by any plan phase (see `project/plan/README.md`);
design still owns it under D2. The offline suite proves request *construction*
(`R-DLMX-CNDL`, `R-DO2Q-46UZ`, `R-EL00-FZVQ`, …); this doc proves the request is
*accepted*.

## Preconditions

- Phases 1–6 built and green offline.
- Suite wiring done (out-of-scope suite work): the root `go.work` has
  `use ./github`, `bin/start` launches `github`, and the credentials are present
  in the environment via `github/.envrc` (`IKIGENBA_APP_ID`,
  `IKIGENBA_GITHUB_ORG`, `IKIGENBA_APP_PRIVATE_KEY`).
- The suite is up: `bin/start` from the repo root.

## `R-DMUT-QF4A` — the live auth proof

**Positive check.** With correct credentials, `health` reports healthy, which is
observable only if a real authenticated call to the `@ikigenba` installation
succeeded (resolve installation + mint installation token).

- Via the MCP tool (suite up): call the `ikigenba_github` `health` tool and
  confirm the envelope is **healthy**.
- Or directly over loopback: `curl -fsS http://127.0.0.1:3203/health` (or the
  chassis health path) returns a healthy envelope, exit 0.

Pass criterion: the envelope is healthy **and** its github-specific reporter
indicates the org installation was resolved / a token was minted (a real call ran,
not merely that the process started).

**Negative check (proves it is not a stub).** Temporarily point
`IKIGENBA_APP_PRIVATE_KEY` at a corrupted/wrong key and restart `github`; `health`
must report **unhealthy** and fail **loudly** (`ErrAppAuth`, no hang, no silent
OK). Restore the real key and restart afterward.

Pass criterion: healthy with the real key, unhealthy-and-loud with a bad key — the
difference proves the check exercises real GitHub authentication.

## Recording the result

This is a manual gate. Record the run (date, commit, positive + negative
observed) wherever the deploy/acceptance log for the box lives; it is not tracked
by `STATUS.md` (which is the autonomous loop's manifest only).
