## Filing context

Filed during the user-invoked opsctl build-spec run begun at `77faae5ce58ee8957bc8fc6e21c74bf84ca01f78`. The workflow implementer and a fresh independent verifier reproduced this blocker. Design and matched tests are read-only during this run.

## Requirements involved

Protected matched requirements: R-M6Y0-I4V2 and R-VLO7-YADY. Gap replacement: R-V3TR-FMU7 → R-YTKN-V54P. Interacting requirements: R-YG5R-NNZ2, R-YHDO-1FPR, R-VU7I-MOKT, and R-78QR-PFQ3.

## Friction

`TestUninstallCommandComposesLifecycleRoutingAndReplication` in `internal/cli/uninstallcmd_test.go` asserts an exact external command sequence allowing only one tasks socket enablement query. Current design requires nginx and services regeneration to each query that state. The remaining installed tasks app has no icon; the revised services contract lists it anyway, requiring the additional query.

## Why unresolvable in-role

The test carries matched R-M6Y0-I4V2 and R-VLO7-YADY, so build-spec cannot rewrite its body or assertions. Skipping services listing or regeneration, or suppressing either required Disabled query, violates current contracts. Removing tasks from the fixture conflicts with the protected test's surviving binary and nginx route assertions. Comment retagging cannot make the gate pass.

## Evidence

Both reports reproduced this command from `opsctl/` against the uncommitted services/workflow implementation:

```text
go test ./internal/cli -run '^TestUninstallCommandComposesLifecycleRoutingAndReplication$' -count=1
--- FAIL: TestUninstallCommandComposesLifecycleRoutingAndReplication (0.00s)
    uninstallcmd_test.go:80: commands = ... want ...
FAIL github.com/ikigenba/ikigenba/opsctl/internal/cli
```

Exit 1. Actual has 14 commands; expected has 13. The additional command is `systemctl show --property=LoadState --property=UnitFileState ikigenba-tasks.socket`, between nginx reload and account checks.

R-YG5R-NNZ2 says "whether it has an icon MUST NOT affect whether it is listed." R-YHDO-1FPR derives enabled from `apps.Disabled`; R-VU7I-MOKT requires the exact systemctl query through `env.Execute` on each invocation. R-78QR-PFQ3 requires nginx to call Disabled, and R-YTKN-V54P requires `services.Write` after nginx application. The protected test's inline execution closure logs every command, so changing fixture responses cannot remove the second query. Removing the tasks binary or manifest contradicts the protected retained-binary and route assertions.

## Suggested resolution

Resolve through draft-design by re-minting the implicated matched requirement(s) R-M6Y0-I4V2 and R-VLO7-YADY, preserving intended synchronization and retained-state behavior while aligning the resulting build work with current services behavior. This build must not edit the protected test. Delete this issue after design resolution; a later user-invoked build recomputes the gap.
