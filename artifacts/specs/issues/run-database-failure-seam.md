# Run database failure cannot be injected

Filed during the user-authorized build-spec run on branch `wip-artifacts`, starting at `628cb67a9d626dcd65894a005fedb624fa8f6606`. The runtime test implementer reported this blocker; a fresh independent verifier confirmed it.

Requirement: R-DVDT-EO0U.

## Friction

`specs/design/D03-serve.md` requires a serving `cli.Run` to write nothing to stdout or stderr, with “a failing database included,” after taking descriptor 3 and without refusing its start. `AGENTS.md` requires a database failure to be provoked with `SetFailing(true)` on the handle backing the store the test handed the server, never by corrupting the file, removing permissions, or closing the store. The `cli.Process` contract in R-FYGI-17O7 supplies neither a database/store injection nor a hook exposing the handle that `Run` opens.

## Evidence

- `specs/design/D03-serve.md:65` declares R-DVDT-EO0U, including the failing-database condition.
- `AGENTS.md:193-196` restricts database failure injection to `SetFailing(true)` on the server's handle.
- `specs/design/D01-layout-and-run-seam.md:49` declares `Process` without a database handle, store, constructor, or handle hook.
- `specs/design/D03-serve.md:41` specifies that `Run` opens its database through `db.Open` itself.
- The independently inspected published appkit dependency returns a fresh `*db.DB` from each `db.Open`; `SetFailing` affects that instance. Failing a test's seed handle cannot fail the separate handle `Run` opens.
- A pre-seeded database with its table dropped makes `Run` refuse startup under R-DLMM-CI3A, which is expressly outside R-DVDT-EO0U's condition.
- A failing-store `web.Handler` test and a healthy `Run` wiring test cannot distinguish an implementation that writes diagnostics only when its own database fails. They therefore do not prove the whole requirement by use.

## Why the build cannot resolve it

The spec skill requires every tagged test to prove its requirement by use and requires unreachable requirements to be filed as issues. Adding an undeclared run seam or weakening the requirement would change the read-only contract; forbidden filesystem failure techniques would violate the read-only test discipline. Partial coverage cannot close this id.

## Suggested resolution

Outside this build, declare a database injection or handle hook in the run seam, or replace this requirement with separately testable Run and handler contracts. Re-mint any changed requirement text through the design workflow, then remove this issue and start a new build-spec run.
