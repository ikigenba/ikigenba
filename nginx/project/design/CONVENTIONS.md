# nginx — Design Conventions

Shared facts every Decision leans on:

- **What this tree is.** nginx configuration (`nginx.conf`, the generated
  `locations/*.conf`), three static committed files (`parked/nginx.conf`,
  `parked/index.html`, `michaelgreenly.dev/nginx.conf`), and one Bash script
  (`run`). No Go, no module, no `go.mod`; the repo-root `go.work` does not and
  must not name it.
- **There is no test-file glob and no id tags.** Nothing here runs under
  `go test ./...`. The repo-root green gate is unaffected by changes in this
  tree, and a passing gate is never evidence about it.
- **The build/typecheck command** is nginx's own configuration test, run against
  the dev prefix from the service root (`nginx/`):

      mkdir -p tmp && nginx -p . -c nginx.conf -t

  `mkdir -p tmp` is part of the command, not an aside: the config declares
  its scratch paths under `tmp/` and nginx refuses to create that parent itself.
  It exits 0 and prints `configuration file … test is successful` when the config
  is valid. The parked config and the `michaelgreenly.dev` vhost config are
  fragments of a server context and are syntax-checked the same way only when
  installed on the box, where `nginx -t` covers them as part of the runbook
  (`deploy.md`).
- **The shell check** is `bash -n run`, exiting 0.
- **"The tree is green"** concretely means, from the service root: `bash -n
  run` exits 0; `mkdir -p tmp && nginx -p . -c nginx.conf -t`
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
- **Ports and routes are never restated here**, with one recorded exception.
  Each service's loopback port lives in `registry/` and reaches this tree
  through the fragment that service ships; the suite's path-routing and
  identity-header contract is the umbrella's, cited by path, never copied. The
  exception is a committed vhost that pins one service's fixed registry port as
  a literal (D5 pins sites' port the way the dashboard's apex block pins its
  own `3000`); the pinning Decision names the port and the registry row it
  mirrors.
