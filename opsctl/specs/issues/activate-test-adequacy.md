# Activate and rollback test adequacy

Status: repaired in this phase; no unresolved implementation defect was found. This issue records the proof explicitly requested for this repair.

## Filing context

The independent mutation report at `30aa7d6b53a9b80b3adfb7ad80de411af61dd734` identified inadequate coverage for R-SSS9-CYXD, R-BONI-FRJS and R-SU05-QQO2. The canonical gap was empty in both directions (616 design ids and 616 test ids), so id presence did not expose these defects. Repairing the tests was possible within the existing contract; changing the read-only design was unnecessary.

## Proof

At the baseline, replacing the `release.LinkOpsctl` call in `linksStep` with a successful no-op passed `go test -count=1 ./internal/cli -run '^(TestActivateSuccessfulReleaseAndReactivation|TestRollbackTransitionPreservesLabelRemovesPrevious)$'` (exit 0). Omitting dropped-app environment directory deletion also passed that command (exit 0). The success fixture created dropped state but did not create the dropped units or environment directory, making the absence assertions vacuous.

The repaired tests exercise `cli.Run` with injected dependencies, a temporary root, explicit EUID, deterministic time and fake host processes. `TestActivateRepairsOpsctlLink` covers fresh activation, release transition and reactivation, each with a missing link, a plain file or a wrong symlink. `TestRollbackRepairsOpsctlLink` covers all three initial link states. Every case asserts the exact `/opt/ikigenba/current/opsctl/bin/opsctl` target. Repeating the skipped-link mutation fails all nine activate cases and all three rollback cases (exit 1), with missing-link, readlink or incorrect-target failures.

`TestActivateSuccessfulReleaseAndReactivation` now creates the dropped app's service unit, socket unit, environment file and retained state before activation. It asserts both units and the environment directory are absent afterward and reads the unchanged retained state. Repeating the environment-deletion omission fails at `activatecmd_test.go:330` with `etc/opt/ikigenba/dropped exists: <nil>` (exit 1). The seeded units also make their existing absence assertions non-vacuous.

Both production mutations were restored exactly. The focused repaired tests then passed, and an independent verifier reran all repaired tests successfully and checked the mutation output and fixture assertions. No production source change was needed.

## Resolution

Retain the strengthened behavioral tests. The adequacy defects are repaired; this file remains as the user-requested issue record, rather than an unresolved build blocker.
