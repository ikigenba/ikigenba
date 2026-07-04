# cleanup-research — status & how to read these reports

This folder holds a one-per-top-level-folder research pass that located
**non-current information** across the suite, ahead of a cleanup. Two migrations
were the priority targets: the **`registry/` service-naming** migration and the
**tar.gz deploy-format** migration. Each `<folder>.md` lists `filename:line`
pointers, migrations flagged high-priority. `plan/` directories were excluded
(old plan phases are allowed to hold non-current info).

> **Read this first.** The per-folder reports are a **point-in-time snapshot**
> from before the registry work landed. Several of them describe the registry as
> "not yet built" / "not yet landed" or treat the deploy format as in-flight.
> **That framing is itself now outdated** — see the status below. Where a report
> and this README disagree about whether registry/deploy is done, **this README
> wins.**

## Status: both priority migrations are DONE

### registry — built, adopted by the first four consumers, and deployed

- **`registry/` is built.** It is a real, standalone, zero-dependency Go module
  at the repo root (`registry/go.mod`, `module registry`) with the authoritative
  `name → port` table (D02) and the resolution API (D03:
  `Port`, `MustPort`, `BaseURL`). Wired into the root `go.work`.
- **Four consumers have adopted it:** `prompts`, `scripts`, `notify`, `sites`.
  Their hardcoded loopback port literals (own port **and** peer feed maps) are
  gone, replaced by `registry.MustPort(...)` / `registry.BaseURL(...) + "/<path>"`
  resolved once at startup. Each carries a committed
  `replace registry => ../registry`, so `GOWORK=off` production builds are clean.
- **Deployed and active on `int.ikigenba.com`:** prompts `v0.14.0`,
  scripts `v0.8.0`, notify `v0.12.0`, sites `v0.9.0` (all `+39fc855`).

**Consequence for the findings:** every high-priority "registry" pointer of the
form `(When the service registry lands, <svc>'s port becomes NNNN; update the
literal below then.)` in the `etc/nginx.conf` files (crm, cron, dropbox, gmail,
notify, prompts, wiki) is now **stale-by-completion**. The registry has landed
and did **not** renumber anyone (it pins current ports), so those notes are not
merely premature, they are wrong: several predict a renumber that never happened
(e.g. prompts predicted 3101 = now ledger's; wiki predicted 3100 = now crm's).
These should be removed/corrected in the cleanup pass.

### deploy — the tar.gz format is current and confirmed live

The `bump → ship → opsctl stage → opsctl deploy` flow (versioned release slots,
three-symlink atomic swap, S3 pre-deploy backup, nginx reload) is the live
mechanism, exercised end to end for the four deploys above. Root `deploy.md` is
the reference. Remaining stale references to the OLD flat-bin model (e.g. the
`Makefile` "production deploy spine (setup/deploy/...)" boilerplate and the
`bin/teardown` flat-`/opt/<app>/{bin,etc,data}` scripts in several services) are
**still stale** and are part of the not-yet-applied cleanup below.

## Applied so far (2026-07-03)

Cleanup has been applied in batches; resolved findings are marked `✅ **DONE**`
(or `⏸️ DEFERRED` / `✅ REVIEWED — no change`) inline in the per-folder reports.

### Batch 1 — registry/deploy loose ends + greenlit fixes

- **All 7 stale registry parentheticals** removed from the `etc/nginx.conf`
  fragments (crm, cron, dropbox, gmail, notify, prompts, wiki).
- **`notes/` scrubbed:** the `notes/` row dropped from 13 `project/README.md`
  tables, and the dead `notes/PLAN.md` / `ARCHITECTURE.md` pointers repointed to
  `project/design/design.md` (or, in preambles, reduced to the `CLAUDE.md`
  owner) across crm, ledger, dropbox, scripts, and a dropbox nginx.conf comment.
- **Superseded verb sets fixed** (`backup`/`restore` → `schema`) in cron, gmail,
  notify, dropbox, sites, scripts, webhooks, and wiki docs plus
  `docs/positioning-onepage.md`; the wiki doc's wrong `appkit.go:244-260` cite
  corrected to `215-224`.
- **`bin/build` bug fixed** in all five `bin/start` scripts that called the
  deleted wrapper (crm, notify, dropbox, prompts, scripts → `make build`); crm's
  invocation also aligned to the canonical `serve --port` form.
- **`docs/README.md` pointer removed** from root `AGENTS.md`/`CLAUDE.md` (the
  file stays intentionally absent; the "In short:" convention summary was kept).

### Batch 2 — seven reports fully cleared

Commits `095e87f` → `ed01841`. This batch closed out **design, appkit, project,
opsctl, eventplane, nginx, and bin** (registry and agentkit had no findings):

- **`project/README.md` scaffold sweep (suite-wide).** Per operator directive to
  treat every service as fully built, removed the `Status: scaffold` blockquote
  from 11 READMEs and stripped the `docs/` referral + not-yet-built framing
  (`(once it exists)`, `generated once a design and plan exist`) from all 12.
  `opsctl` kept its accurate `Status: active` block, minus the `docs/` pointer.
- **Dead `design-bible.html` source-of-truth pointer** repointed: 11 service
  `tokens.css` copies → `design/tokens.css`; dead clauses dropped from
  `design/carbon.md` and `design/tokens.css` (that folder is the source).
- **appkit manifest on-box path** corrected to `/opt/<app>/etc/current/manifest.env`
  (the retired sibling path); committed source-tree `etc/manifest.env` refs left.
- **project registry non-goal deleted** — `registry/` is built/adopted/deployed,
  so the "no designed-but-unbuilt service registry" bullet is overtaken.
- **opsctl rollback usage strings** → `<app> [-N]` (explicit version is rejected
  by the code; recovery is `-N` snapshot recency).
- **eventplane** dead `ikigai/` codename → mono-repo/root `go.work`; the "binary
  `restore` verb"/`bin/restore` wording corrected to the real trigger
  (`opsctl restore`/`opsctl rollback` clearing the `*.generation` sidecar).
- **nginx** dead `~/projects/nginx` path, obsolete "Phase 2b" phrasing, and the
  incomplete Map (added `/_session-authn`) fixed; `run`'s hardcoded service list
  deferred as registry-adoption work.
- **bin** findings confirmed **moot** (already resolved by the `docs/archive`
  deletion and the migration-script purge) — report-only update, no code change.

## What is NOT done

- **Many per-folder findings are still open** across `docs`, `crm`, `ledger`,
  `dropbox`, `prompts`, `scripts`, `notify`, `webhooks`, and `dashboard`: stale
  `internal/server`/`internal/logging` package layouts, `CLAUDE.md` Consumes
  drift, stale Makefile "deploy spine" comments, the `bin/teardown` flat-layout
  scripts, remaining scripts `notes/` refs in `product.md`/`D05.md`/`loops/`/
  `main.go`, and the `docs/service-registry-design.md` "table lives in appkit"
  supersession. Still valid work.
- **registry adoption is only partial.** Only prompts/scripts/notify/sites
  import registry so far. Other services still hardcode their own port literal;
  broader adoption is separate future work, not stale info to scrub.

## Scope reminder for whoever applies the cleanup

Non-current info is allowed to remain in `**/project/plan/**` (old phases) and in
`docs/archive/**` (intentionally historical). Everything else is fair game.
