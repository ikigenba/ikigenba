# bin — Product

**Authority: intent.** This doc owns *why* the repo-root `bin/` tooling exists,
*for whom*, what is in and out of scope, and the user-facing **promises** —
stated once, in outcome terms. It does **not** own mechanism, script flags,
file formats, version-string grammar, exit codes, or test assertions; those
belong to `bin/project/design/README.md` and its Decisions. Where the two could
overlap on behavior, this doc states the **promise** and design states the
**exact, checkable proof** of it.

The suite-wide contracts this tooling produces artifacts *for* — the version
identity, the release bundle's layout, the on-box install tree, the
env/manifest contract, the secrets parameter shape — are **not owned here**.
They belong to the umbrella project at the repo root (`project/`), and this
workspace uses them by value.

## Problem

The suite is a mono-repo of fifteen independently-versioned services that ship
to a box the operator does not develop on. Everything that has to happen
*before* an artifact reaches the box — advancing a version, building and
packaging a release, seeding the secrets a service will need there — is
workstation work, and without a home it becomes fifteen slightly-different
per-service rituals. The suite has already paid for that once: seven
per-service secret-seeding scripts, each re-implementing a read-modify-write
against a shared blob, each able to clobber the others.

The same gap exists in the other direction, for local development. Standing the
suite up on a workstation means building fifteen binaries, giving each its
port, its environment, and its secrets, and putting a front door in front of
them so the real path-routed auth chain is exercised. Done by hand it is not
reproducible, and done per-service it drifts from how the box actually runs —
which is exactly how a dev stack stops being evidence of anything.

And the tooling has a credibility problem of its own. These are shell scripts,
deliberately kept out of the test suite because they mostly orchestrate builds
and remote copies that no hermetic test can stand in for. But a subset of them
does not orchestrate at all: it *reads the same on-disk layout the box uses*.
That subset has already drifted silently once — the dev tooling and the box
disagreeing about where a service's manifest lives — and nothing caught it.

## Purpose

One home for the suite's **off-box operator tooling**: the small set of
workstation commands that advance a version, build and deliver a release,
stand the suite up and tear it down locally, add a schema migration, and seed a
service's deployed secrets. Uniform across every service, parameterized by
nothing but the service name, and — for the parts that read the box's on-disk
layout — proven rather than trusted.

## Users

The **suite maintainer**, working at their workstation. They are trying to:
cut and deliver a release without hand-assembling it; run the whole suite
locally to test a change against the real front door and the real auth chain;
add a schema migration without colliding with a branch they cannot see; and
seed a service's deployed secrets from what their local environment already
declares, without touching any other service's.

The on-box operator persona — deploying, rolling back, backing up — is *not* a
user of this tooling. That is `opsctl`'s job, on the box.

## Scope

In scope — the repo-root `bin/` tree:

- **Version advancement.** One command advances any service's committed version
  by the requested field, and refuses a version file that does not already
  conform to the suite's version contract rather than coping with it silently.
  It is also the release-time gate for the suite's changelog promise: a version
  cannot be minted unless the service's changelog already records what the new
  release changes.
- **Release delivery.** One command builds a service's release from the main
  line, stamps it with the exact identity it will be known by everywhere else,
  packages every shipped tier together as one versioned artifact, and puts that
  artifact on the target box ready to be staged.
- **The local stack.** One command builds every service, gives each its port and
  its environment, stages the runtime layout in the same shape the box uses,
  launches them all, and brings up a local front door so path-routed requests
  reach services exactly as they will in production. A companion command tears
  the stack back down, optionally discarding its local state.
- **Migration authoring.** One command creates a new, collision-proof migration
  file for a named service.
- **Secrets seeding.** One command seeds (or re-seeds) a service's deployed
  secrets from the declarations its own local development environment already
  carries, one service at a time or sweeping them all, with a preview mode that
  shows what would be pushed without pushing it.
- **Proof for the layout readers.** The parts of this tooling that read the
  suite's on-disk layout are exercised by automated tests that run the real
  scripts under the repo's ordinary green gate.

Out of scope — nothing else is promised here:

- **No on-box operation.** Deploy, rollback, prune, backup, restore, provision,
  and status are `opsctl`'s, and run on the box. Nothing here reaches into a
  running box's install tree.
- **No ownership of suite contracts.** The version grammar, the bundle layout,
  the install tree, the env/manifest contract, and the secrets parameter shape
  are the umbrella project's. This tooling conforms to them; it does not define
  them, and a change to any of them is a change there.
- **No test coverage for the orchestrating scripts.** Building, `scp`-ing,
  launching processes, and calling a cloud API are verified once, by hand, when
  written or changed — not simulated in the test suite. Only the layout readers
  and the version-advancement gate are covered automatically.
- **No production build path.** Local development runs in workspace mode; the
  release build deliberately does not, and the two are not unified.

## What we promise (user-facing behavior)

- **Advancing a version is one command and one commit.** The operator names a
  service and a field; the committed version file advances together with the
  service's changelog record of that release, and nothing else changes. A
  version file that does not already conform to the suite's version contract is
  rejected outright, with the operator told, rather than quietly normalized.
- **A release cannot outrun its changelog.** Advancing a version is refused,
  with the operator told what to add, unless the service's changelog already
  carries the record for the version being minted — so no release ships
  undocumented. The operator can always ask, without changing anything, what
  the next version would be, and the answer is available even while the
  changelog is not yet in order.
- **A release is one artifact, complete.** One command turns a service's current
  version into a single delivered artifact carrying the built binary and
  everything shipped alongside it, named with the exact same identity the binary
  reports when asked. Nothing that ships travels outside it, so nothing that
  ships can be forgotten.
- **The local stack looks like the box.** Bringing the suite up locally produces
  the same runtime layout, the same path routing, and the same front-door auth
  chain the box has — so a change tested locally has been tested against the
  real shape. Every service's log is where the operator expects it, and tearing
  the stack down leaves nothing running.
- **A new migration cannot collide.** Creating a migration for a service yields
  a file whose ordering is unique by construction, so two branches authored the
  same day still merge and apply in a defined order.
- **Secrets seeding is per-service and preview-able.** One command seeds a
  service's deployed secrets from exactly what its own local environment
  declares — no more, no fewer. It can be asked to show what it would push
  without pushing. A service with no secrets is seeded explicitly rather than
  left absent. Seeding one service can never touch, corrupt, or drop another's.
  A secret's value survives seeding unchanged, including a multi-line one.
- **Secret values never appear.** Preview and result output name keys and show
  values only in masked form; a full secret value never reaches the terminal, a
  command line, or a log.
- **The layout readers are proven, not trusted.** The tooling that reads the
  suite's on-disk layout is exercised automatically by the repo's ordinary test
  run, against the real scripts — so a drift between how dev stages a service
  and how the box reads it fails the build instead of failing in production.

## Success criteria (outcomes)

Each item is a result the maintainer can confirm end-to-end against the real
tooling:

- Advancing a service's version writes the expected next version together with
  that release's changelog record and touches nothing else; pointing the same
  command at a non-conforming version file fails and explains why, leaving the
  file unchanged.
- Advancing a version for a service whose changelog does not yet record the new
  release fails and explains what to add, changing nothing; asking what the
  next version would be succeeds and changes nothing even in that state.
- Building and delivering a service's release produces exactly one artifact on
  the target box, named with the identity the built binary reports when asked
  for its version, and containing every tier that service ships — including an
  explicitly empty one where the service ships nothing of that kind.
- The artifact so produced can be staged by the on-box tooling with no manual
  rearrangement of its contents.
- Bringing the suite up locally leaves every service answering on its own port
  behind one local front door, reachable through the same request path the box
  uses; tearing it down leaves no service process running.
- Creating two migrations for the same service on the same day yields two files
  with distinct, unambiguously ordered names.
- Previewing a service's secrets seeding lists exactly the keys that service's
  local environment declares, with no values shown; performing the seeding then
  leaves that service's deployed secrets carrying exactly those keys, every
  other service's untouched, and a multi-line value intact.
- The repo's ordinary test run exercises the layout-reading tooling by executing
  the real scripts, and fails if a reader stops agreeing with the layout the box
  uses.
