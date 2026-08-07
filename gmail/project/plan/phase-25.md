# Phase 25 — Guard both committed deploy artifacts against registry port drift

*Realizes design Decision 12 (slice: `R-9SU9-BYHJ`).*

`R-9SU9-BYHJ` is gmail's one current Verification id with no tagged test. D12
describes it as a guard already carried by two existing tests, and those tests do
exist and are registry-derived — but they are **weaker than the id's claim**, so
this phase does not merely re-tag them. The id says *the committed
`etc/manifest.env` and `etc/nginx.conf` **ports** agree with `registry`* — a
universal claim over both artifacts. What is actually asserted today:

- `TestManifestLibraryByteEqualsCommittedFile` (`cmd/gmail/main_test.go`, tagged
  `R-8IAN-FB87`) byte-compares `manifest.Emit` over the compiled Spec against the
  committed `etc/manifest.env`. Because `Spec.Port` is `registry.MustPort("gmail")`
  (D11, pinned by `R-9QEG-KF05`), this does catch a manifest port drift — but it
  is the byte-agreement claim of a *different* id, and it catches the port only
  as a side effect of comparing the whole file.
- `cmd/gmail/nginx_test.go` builds three needles from `registry.BaseURL("gmail")`
  — the landing `proxy_pass …/;`, the static `proxy_pass …/static/;`, and the PRM
  well-known `proxy_pass …/.well-known/oauth-protected-resource;` — inside tests
  minted by `R-NGNX-7F1G`, `R-41ZW-HBBA`, `R-NGNX-9H3J` and `R-419Z-49R3`. Each is
  a **containment** check on one named location.

The gap that makes containment insufficient: `etc/nginx.conf` currently carries
**four** loopback-address occurrences, and one of them — the `proxy_pass
http://127.0.0.1:3202/;` inside the bearer-gated `/srv/gmail/` prefix location
(around line 102), plus the `127.0.0.1:3202/feed` reference in the direct-consumer
comment (around line 29) — is asserted against `registry` by **no** test. A
renumber that updated the three tested locations and missed the bearer location
would leave the whole suite green while shipping a fragment that proxies agent
traffic to a dead port. That is exactly the silent drift the id exists to
prevent, so the id gets its own exhaustive guard rather than a tag on a
containment test.

**What gets built.** One new test file `cmd/gmail/deploy_drift_test.go`
(`package main`) holding exactly one test, `TestCommittedDeployArtifactPortsAgreeWithRegistry`,
tagged `// R-9SU9-BYHJ`, that reads both committed artifacts from disk
(`../../etc/manifest.env`, `../../etc/nginx.conf`) and asserts:

- **manifest half.** The file contains exactly one line beginning `PORT=`, and its
  value parses to an int equal to `registry.MustPort("gmail")`. Assert the count
  is exactly one, not "at least one", so neither a missing nor a duplicated
  `PORT=` line can pass.
- **nginx half.** Every loopback-address occurrence anywhere in the fragment
  carries `registry.MustPort("gmail")` — scan the whole file, collect each
  `(line number, port)` pair, and fail naming every offending line. Assert the
  number of occurrences found is **at least 4**, so an over-narrow scan pattern
  that matched nothing (or only the one location) cannot pass vacuously. Both
  `proxy_pass` targets and the plain-comment reference count; the comment is
  deploy documentation that drifts the same way.

**The scan pattern must not trip the sibling guard.** `R-9RMC-Y6QU`'s
`TestGoSourceDoesNotHardcodeLoopbackRegistryPorts`
(`cmd/gmail/loopback_guard_test.go`) fails any `*.go` file under the module whose
bytes contain `127.0.0.1:3` followed by three digits, and it skips **only** its
own filename. So the new file must never contain that byte sequence: build the
scanner from an **escaped** regexp source, e.g.

```go
loopback := regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
```

whose literal bytes are `127\.0\.0\.1:(\d+)` — backslashes between the octets —
and therefore do not match the guard's `127.0.0.1:3` needle. Do not write the
expected address out as a plain unescaped string anywhere
in the file; derive it from `registry` and format it when a failure message needs
it. If the guard turns red, the fix is the pattern, never an exemption for the
new filename.

**Nothing else changes.** No production code, no schema, no migration, no edit to
`etc/manifest.env` or `etc/nginx.conf` themselves (their `3202` literals stay —
the point is that the test now polices them), and no change to any existing test:
`TestManifestLibraryByteEqualsCommittedFile` and every test in `nginx_test.go`
keep their current assertions and their current tags. `R-9SU9-BYHJ` is added to
exactly one test and appears in exactly one place in the tree.

**No stray tags to clear.** Each of the three adopted suite-contract ids
(`R-4LKF-FB23`, `R-8DF1-W89F`, `R-8IAN-FB87`) was checked and appears exactly
once in gmail's `*_test.go` files, all three in `cmd/gmail/main_test.go`, each on
the test that genuinely proves it. There is no duplicate-tag cleanup in this
phase.

## Done when

- `cmd/gmail/deploy_drift_test.go` exists and holds
  `TestCommittedDeployArtifactPortsAgreeWithRegistry` tagged `// R-9SU9-BYHJ`,
  asserting the behavior above: `R-9SU9-BYHJ` — the committed `etc/manifest.env`
  `PORT=` line and **every** loopback-address occurrence in the committed
  `etc/nginx.conf` carry `registry.MustPort("gmail")`.
- The suite is green per design's Conventions: from `gmail/`, `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures. In particular `TestGoSourceDoesNotHardcodeLoopbackRegistryPorts`
  still passes with the new file present.
- **Perturb and see it fail**, twice, each reverted before moving on:
  1. Change the bearer-location `proxy_pass` port in `etc/nginx.conf` (the
     occurrence around line 102 — the one no existing test covers) from `3202` to
     `3299`. `go test ./cmd/gmail/ -run TestCommittedDeployArtifactPortsAgreeWithRegistry`
     **fails**, naming that line. Confirm the pre-existing nginx tests still pass
     under this perturbation (`go test ./cmd/gmail/ -run TestNginx`) — that is
     the coverage gap this id closes. Revert; both pass again.
  2. Change `PORT=3202` to `PORT=3299` in `etc/manifest.env`. The same `-run`
     invocation **fails** on the manifest half. Revert; it passes again.
- The id appears exactly once as a tag in the tree — from `gmail/`:

  ```
  grep -rho 'R-9SU9-BYHJ' --include='*_test.go' --exclude-dir=project . | wc -l
  ```

  prints `1`. The `--exclude-dir=project` is what makes this non-vacuous: this
  phase file and `project/design/D12.md` both name the id, and neither is a test.
- The tree-local coverage difference is empty — from `gmail/`:

  ```
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```

  prints nothing.
