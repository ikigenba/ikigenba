# Suite contracts — Product

**Authority: intent.** This doc owns *why* this project exists, *for whom*, what is
in and out of scope, and what it **promises** — stated once, in outcome terms. It
does **not** own mechanism, on-disk paths, permission bits, object-storage key
layouts, version-string formats, exit codes, or test assertions; those belong to
`project/design/` and its Decisions. Where the two could overlap on behavior, this
doc states the **promise** and design states the **exact, checkable proof** of it.
That boundary is load-bearing: it is what keeps product, design, and plan from
restating each other.

## Problem

The suite is one mono-repo of many independently deployable parts: fifteen service
applications, three shared libraries, the on-box CLI, the off-box build tooling,
and the front door. Most of what makes them a *suite* rather than fifteen unrelated
programs is agreement — where a service is installed, what a release artifact is
called and contains, what a version string looks like, which of a service's data
survives a restore, what environment a binary is handed, where its secrets come
from, what a forensic record is.

Those agreements have no home. Each is real, each binds several trees at once, and
none of them belongs to any single tree: the tree that *produces* a release bundle
and the tree that *consumes* it are different, and neither can be the authority
without the other silently drifting. Left unowned, an agreement gets restated in
two or three specs, the restatements diverge, and the drift is discovered on the
box — a fragment shipped but not applied, a manifest generated that should have
been authored, a version ordered lexicographically, a restore that resumes onto a
stale event epoch. Every one of those is a disagreement between trees that each
believed they were correct.

## Purpose

This project is the suite's **umbrella**: the single home for the contracts more
than one tree must agree on. It states each contract once, normatively, and builds
nothing. Every implementing tree — `appkit`, `eventplane`, `opsctl`, `bin`,
`nginx`, and each deployable service — **cites** a contract from here and owns none
of it, so there is exactly one statement of each agreement and no copy that can go
stale. Conformance is the default; a tree that must depart from a contract says so
in its own spec, visibly, with its reason.

## Users

The **spec authors and build loops of every tree in the suite**. When a service
spec needs to say where its database lives, what its binary's verbs are, or what
its release artifact is called, it cites a contract here instead of deciding again.
They are trying to build a tree that composes correctly with fourteen others
without reading fourteen other specs.

Behind them, the **box operator** — the person shipping, deploying, backing up, and
recovering the suite — is who the contracts are ultimately *for*: uniformity across
services is what lets one command work on any of them. But the operator uses the
tools, not this document; the tools' specs live in the trees that build them.

## Scope

This project owns the agreements that bind more than one tree:

- **The on-box install tree** — the single home a service is installed into, and
  what each tier within it means, who owns it, and how long it lives.
- **The release bundle** — what a release *is* as a unit of delivery: what it is
  named, what it carries, and how the live version is selected from what is
  installed, so a service's config and resources can never desync from its binary.
- **Version identity** — one version string, produced and consumed the same way
  everywhere, ordered by one rule.
- **The durable/disposable boundary** — the single line that decides what is backed
  up, what a restore wipes, and what a service must rebuild for itself on boot.
- **The event epoch and the boot obligation** — how a restored service comes back
  on a fresh event epoch rather than resuming a stale one, and what every service
  must therefore do at startup.
- **The env contract and the verb set** — what environment a service binary is
  handed and how it derives its paths from it, and which verbs every service binary
  exposes (and, as importantly, which it must not).
- **Per-service adoption** — what conforming to all of the above concretely
  obliges each service to carry, and the one-time migration of the live box onto
  it.
- **The secrets parameter** — where a service's deployed secrets live, what shape
  they take, and what the box does when they are missing.
- **The telemetry contract** — what a forensic record is, which moments produce
  one, how one correlation id per causal chain is minted and carried, and how bulk
  content is referenced rather than stored.
- **The testing language** — the shared vocabulary for how any tree proves its
  behavior: which layers of testing exist, what a test in each layer may touch,
  how a test is allowed to reach a real external service, and what every tree
  must declare about its own testing.
- **The version plane** — which service is the custodian of authored content's
  git history, how versioning stays ambient (no user ever manages it), what
  `main` means, the one git door and who may push what, how merges are gated,
  and what deleting a versioned artifact preserves.

Out of scope — nothing else is promised here:

- **No code.** This project builds, compiles, and tests nothing. It has no
  toolchain and no source tree. Every behavior a contract requires is implemented
  and proven in the tree that owns the implementation.
- **No tooling of its own.** The commands that produce a release, deploy or roll
  one back, back a service up, seed its secrets, or serve the front door are
  specified by the trees that build them — `bin/`, `opsctl/`, `nginx/` — not here.
- **No governance of a single tree's internals.** A contract states what several
  trees must agree on and stops there. How a service structures its packages, names
  its types, or tests its own domain is its own spec's business.
- **No history.** A contract is a current statement. What a contract used to say
  lives in git, never beside its replacement.

## Contractual constants

Fixed, promised values design must use verbatim and never re-derive:

- **Versions are SemVer 2.0, `v`-prefixed** (e.g. `v0.7.1`) everywhere a version
  appears.
- **Default backup retention: 30** most-recent snapshots per service
  (operator-configurable; 30 is the out-of-the-box default).
- **Default object-storage region: `us-east-2`** (operator-configurable).
- **The correlation header is `X-Correlation-Id`**, carrying a bare 26-character
  Crockford-base32 ULID, everywhere a correlation id travels.
- **The forensic store service is named `telemetry`** (its port is the registry's
  to own).

## What we promise (contract-facing behavior)

- **Every shared agreement has exactly one statement.** A fact more than one tree
  depends on is written here once. A tree that needs it cites it by path and copies
  nothing, so there is no second version to drift.
- **Citing a contract is enough to conform.** A spec author who cites a contract
  inherits its full content without restating it, and is not expected to re-derive
  the reasoning behind it.
- **A departure is always visible.** Silence means a tree conforms. A tree that
  must deviate carries its own Decision naming the contract it departs from and
  why, so no deviation is discoverable only by reading code.
- **Every contracted behavior is proven somewhere concrete.** Each behavior a
  contract requires names the tree whose tests prove it — one named tree where the
  behavior is implemented once, or every adopting service where the contract binds
  each of them individually. No behavior is contracted without a stated place its
  proof lives.
- **The suite composes.** Because producer and consumer of each agreement read the
  same statement, the parts fit: a bundle the build tooling produces is one the box
  tooling can stage, a path a binary composes is one the install tree provides, a
  restore leaves a service in a state its own boot path can complete.

## Success criteria (outcomes)

Each item is a result an author or reviewer can confirm against the real repo:

- Every contract in this project is stated in exactly one place, and no
  implementing tree's spec restates a contract's content normatively — each
  references it by path instead.
- Every behavior any contract requires names the tree that proves it, and that
  tree's tests carry the behavior's handle; for a contract binding every service
  individually, each adopting service's tests carry it.
- This project contains no build, compile, or test step of its own, and no source
  or build file outside its spec.
- A spec author for any tree can determine, from this project alone, where that
  tree's service is installed, what its release artifact is called, what version
  string it carries, which of its data survives a restore, what environment its
  binary receives, which verbs it exposes, where its secrets come from, and what it
  must record, and which testing layer any of its checks belongs to — without
  reading another tree's spec.
- Any tree that departs from a contract says so in its own spec, naming the
  contract and the reason; no departure exists that is visible only in code.
- The suite's parts compose end to end on the real box: a release produced by the
  build tooling stages, deploys, rolls back, backs up, and restores through the box
  tooling with no per-service special-casing, and every service comes back healthy
  afterward.
