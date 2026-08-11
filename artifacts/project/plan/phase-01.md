# Phase 1 — Module scaffold, composition root & chassis adoption

*Realizes design Decision 1 (skeleton/chassis) and 10 (test strategy).*

Bring the `artifacts` module to life as a bootable suite service: `go.mod`
(module `artifacts`, `replace` siblings `appkit`/`eventplane`/`registry`,
wired into the repo-root `go.work`), the `cmd/artifacts` composition root
(`appkit.Spec` with `Port: registry.MustPort("artifacts")`, temp-stub mux),
the committed `VERSION` (`v0.1.0`), the authored portable `etc/manifest.env`
carrying `ARTIFACTS_MAX_UPLOAD_BYTES=209715200`, the config loader for that
variable, and the `AGENTS.md` whose Tests section makes the D10
declaration. The observable end state: the binary builds, `serve` boots
against a temp `state/`, `/health` answers with service name and version,
and the D10 guards (AGENTS.md declaration check, skip-ban scan, no-literal
port scan, manifest portability + emit-agreement, no-`/opt` scan) are green.

**Done when:** the suite is green (design Conventions) and each of
R-39K3-W9HR (composed boot smoke), R-3AS0-A18G (port-by-name + literal
scan), R-3BZW-NSZ5 (VERSION + config parse/reject), R-8DF1-W89F (portable
manifest), R-8IAN-FB87 (emit agreement), R-VKB6-SHHV (no `/opt` literal),
R-4LKF-FB23 (install-tree boot via the composed smoke), R-O1AD-MRKW
(AGENTS.md Tests declaration), R-O2IA-0JBL (skip ban) is covered by a test
tagged with its id.
