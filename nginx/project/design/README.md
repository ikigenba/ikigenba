# nginx — Design

**Authority: shape and its proof.** This directory owns *how* the local
development front door and the committed parked `default_server` artifacts are
built, and *how each behavior is proven*. The product
(`project/product/README.md`) owns the *why* and the user-facing promises.
Design uses the product's contractual constants — the `:8080` local port, the
parked message, the success status — **by value** and does not own them. It
likewise **cites** the suite contracts it depends on by path and restates none of
them. This is the **single, current** statement of the architecture: when a
decision changes, its `DNN.md` is rewritten in place — decisions are never
stacked. Construction history lives in git.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior. Ids are minted, never
  hand-written, never renumbered.
- The ids live inline in those Verification lists and **nowhere else** — there is
  no separate requirements document.
- Design's responsibility for ids ends at **minting** them here. How coverage is
  measured, what counts as covered, and when the work is "done" are downstream
  phases' concern and are deliberately not specified in this directory.

**This tree currently mints no ids**, and each Decision says so with its reason.
That is a property of what lives here, not a relaxation of the rule: `nginx/`
holds nginx configuration, two static files, and one shell script — no module
owns it, so the suite's green gate has no faithful assertion it could make about
it, and the behaviors that matter hinge on a real nginx, a real certificate
authority, and real DNS. A future Decision that does introduce a testable seam
mints ids normally.

## Conventions

Shared facts every Decision leans on:

- **What this tree is.** nginx configuration (`nginx.conf`, the generated
  `locations/*.conf`), two static committed files (`parked/nginx.conf`,
  `parked/index.html`), and one Bash script (`run`). No Go, no module, no
  `go.mod`; the repo-root `go.work` does not and must not name it.
- **There is no test-file glob and no id tags.** Nothing here runs under
  `go test ./...`. The repo-root green gate is unaffected by changes in this
  tree, and a passing gate is never evidence about it.
- **The build/typecheck command** is nginx's own configuration test, run against
  the dev prefix from the repo root:

      mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t

  `mkdir -p nginx/tmp` is part of the command, not an aside: the config declares
  its scratch paths under `tmp/` and nginx refuses to create that parent itself.
  It exits 0 and prints `configuration file … test is successful` when the config
  is valid. The parked config is a fragment of a server context and is
  syntax-checked the same way only when installed on the box, where `nginx -t`
  covers it as part of the runbook (`deploy.md`).
- **The shell check** is `bash -n nginx/run`, exiting 0.
- **"The tree is green"** concretely means, from the repo root: `bash -n
  nginx/run` exits 0; `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t`
  exits 0; and the structural checks a phase names (exact committed files, exact
  greps) all hold. There is no test suite to run and no id coverage to compute.
- **Testing language (suite contract).** This tree uses the suite's testing
  vocabulary and rules from `root project/design/D23.md`, cited **by path only**
  and never restated; D4 records the adoption. In this tree's terms: the layers
  present are **manual only** — no hermetic, no composed, no live — and the
  contract's own no-test-suite clause makes conformance here structural, so the
  contract's `[proof: per-service]` ids are deliberately **not** cited (no file
  in this tree could carry an id tag). The `nginx -t` and `bash -n` checks below
  are configuration and syntax checks, not tests, and are not a layer.
- **Verification that needs a real substrate happens outside any gate.** The
  claims that matter — a request actually being refused at the boundary, a real
  certificate authority issuing for names that really resolve to the live box, a
  real nginx selecting a real `default_server` — are checked once, by hand,
  against the running stack (`bin/start`, then the local front door) or against
  the live box via the runbook's verification checklist. They are never asserted
  by a stub, because a stub would accept anything and prove nothing.
- **Ports and routes are never restated here.** Each service's loopback port
  lives in `registry/` and reaches this tree only through the fragment that
  service ships; the suite's path-routing and identity-header contract is the
  umbrella's, cited by path, never copied.

## Layout

The design is **split for addressability** so a build phase reads only the one
Decision it realizes:

- `project/design/INDEX.md` — the manifest: each Decision → its file, plus the
  id → Decision/file reverse map (empty while this tree mints no ids).
  Regenerated whenever a Decision is added or its Verification ids change.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded
  `D01.md`, `D02.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/README.md` — this spine: cross-cutting facts only, no
  per-Decision detail.

Design is **rewritten in place**: a changed Decision is rewritten in its `DNN.md`
and `INDEX.md` is regenerated; a new Decision adds a `DNN.md` and an INDEX entry.
