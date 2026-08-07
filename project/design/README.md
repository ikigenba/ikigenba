# Suite contracts — Design

**Authority: shape and its proof.** This directory owns *how* the suite's shared
contracts are shaped — the on-box install tree, the versioned release bundle, the
version identity, the `state/`÷`cache/` boundary, the event epoch and its boot
obligation, per-service adoption, the env/manifest and verb-set contract, the
secrets parameter, the telemetry and correlation standard, the owner-identity key,
the event-plane wire, the content plane, and the MCP surface — and *how each of
their behaviors is proven*. The product (`project/product/README.md`) owns the *why* and
the promises; design uses the product's contractual constants **by value** but does
not own them. This is the **single, current** statement: when a contract changes,
its `DNN.md` is rewritten in place — Decisions are never stacked. Construction
history lives in git.

This project is the **umbrella**: it governs the suite's shared contracts and
**builds no code of its own**. Every Decision here is a convention that other
trees implement — `appkit`, `eventplane`, `opsctl`, `bin`, `nginx`, and each
deployable service. Those trees' specs **cite** these Decisions by path
(`project/design/DNN.md` at the repo root) and never restate them normatively; a
subproject conforms unless it carries its own Decision naming the departure and the
reason.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  contract requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in those Verification lists and **nowhere else** — there is
  no separate requirements document.
- Because this project owns no code, every id additionally carries a
  **proof-location marker** naming where its tagged test lives:
  - `[proof: <tree>]` — that one tree carries the tagged test; the behavior is
    proven once, in the implementation that owns it.
  - `[proof: per-service]` — every service adopting the contract carries its own
    tagged test for the id.
  A contract with no testable behavior of its own says so explicitly and mints no
  ids.
- Design's responsibility for ids ends at **minting** them (and marking where they
  are proven). How coverage is measured and when work is "done" are downstream
  concerns, deliberately not specified here.

## Conventions

Shared facts every Decision leans on:

- **No toolchain of its own.** This project builds, compiles, and tests nothing:
  there is no build command, no test command, and no test-file glob belonging to
  it. Each implementing tree's own design Conventions state those, and a contract's
  ids are proven under *that* tree's gate. A contract whose only artifacts are
  committed prose documents mints no ids at all.
- **The suite-wide gate, for reference.** Every Go tree in the suite is green under
  `go test ./...` and tags requirement ids in `*_test.go` files; `bin/` shell
  tooling is proven through the `bin/bintest` Go module or verified once manually,
  per that tree's spec. This is context for reading the markers, not a gate this
  project runs.
- **Coverage is checked per marker, not by a tree-local grep.** The ordinary
  tree-local coverage grep does not apply here, since this tree holds no tests. For
  each id, check the tree its marker names:

  ```
  # for an id marked [proof: <tree>]
  grep -rl 'R-XXXX-XXXX' --include='*_test.go' <tree>/

  # every marked id at once, listing any with no proof in its named tree
  grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4} \[proof: [a-z-]+\]' project/design/D*.md | sort -u
  ```

  An id marked `[proof: <tree>]` passes when it appears as a tag in that tree's
  test files. An id marked `[proof: per-service]` is checked in **each adopting
  service's** tree instead, and enters that service's own coverage denominator when
  its design cites the id.
- **Contracts are cited, never copied.** An implementing tree's spec references a
  contract by its path here and owns none of it. A local restatement is drift by
  construction.

## Layout

The design is **split for addressability** so a reader (or an amending phase) opens
only the one contract it concerns:

- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a sorted
  `R-id → Decision/file` reverse map. Regenerated whenever a Decision is added or
  its Verification ids change.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded
  `D01.md`, `D02.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/README.md` — this spine: cross-cutting facts only, no
  per-Decision detail.

**Decision numbers are permanent and are never reused.** The contracts are D01,
D02, D03, D05, D06, D08, D11, D12, D14, and D17–D20; the numbers **D04, D07, D09,
D10, D13, D15, and D16 are retired** — those Decisions owned code and moved to the
trees that build it (`bin/project/`, `nginx/project/`, `opsctl/project/`). A
retired number is never assigned to a new Decision, and survivors keep the numbers
they have.

**`docs/` is not normative.** Every binding suite-wide contract lives in a
Decision here; `docs/` retains only non-normative material (the positioning
pages). A contract absorbed from a former doc is cited by its `DNN.md` path, not
by the deleted filename.

Design is **rewritten in place**: a changed contract is rewritten in its `DNN.md`
and `INDEX.md` is regenerated; a new contract adds a `DNN.md` and an INDEX entry.

## Resolved design questions (rationale trail)

Questions settled during authoring, recorded so the *why* survives:

- **Manifest authored, not generated; data paths composed from `IKIGENBA_ROOT`**
  (D11). `manifest.env` is config the binary *reads*, so it is authored and shipped
  verbatim — the `manifest` verb and any on-box regeneration/stamp step are gone.
  Box data paths compose in-binary from one box-global `IKIGENBA_ROOT` (mirroring
  `IKIGENBA_DOMAIN`), keeping the committed manifest portable, with a fail-loud
  guard when production is half-configured.
- **No backup path inside a service binary** (D11 verb set, D05 boundary). A backup
  on the same disk as the data is not a backup, and a binary cannot stop its own
  unit; capture is off-box and opsctl-owned, so the binary keeps only the verbs
  nothing outside it can perform.
- **Served web content → uniform `state/www/{public,private}`** under `state/`
  (D01/D05). Backed up as plain `state/`, no special root, no `sites`-casing. nginx
  serves it via the `web` group + `0711` traverse-only `state/` (the DB stays
  private); `public/` is direct, `private/` is introspection-gated.
- **The epoch re-mints by exclusion, not by a delete step** (D06). The sidecar
  lives outside the backed-up region, so a restore cannot bring a stale epoch back;
  the price is the boot-reconstruction invariant every service must honor.
- **One SSM parameter per app, not a shared blob** (D12). Per-app parameters remove
  both the size wall and the clobber risk, and make "no secrets" an explicit seeded
  state rather than an absence.

No open contracts remain undecided.
