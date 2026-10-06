# D07-teardown

This document owns what `down`, `wipe` and `ls` do once D02 has parsed a well-formed command line and D03 has found the sandbox. Help texts, argument parsing, usage errors, exit codes and the shared diagnostic shapes are D02's; finding the sandbox, its name, the layout, the registry, the locks, the unit names, the slice names, the unit state run and the up/down rule are D03's. This document cites them and restates none.

`down` stops a sandbox and removes what `up` made of it, keeping its data. The work it does once it holds the sandbox lock is called the teardown, and `wipe` performs the same teardown before it deletes the rest. The teardown has six steps in a fixed order, every step walking the sandbox's units in one order: nginx first, so no new request arrives, then each recorded app in name order, its socket before its service, so a queued connection cannot start the service again once it has stopped, and last the sandbox slice (D03), which holds the core and apps slices beneath it. The slice has no file and is never one of the unit names, so it is stopped only when systemd says it is not `inactive`, as it is while anything ran in it since it was started: a slice stays `active` after its services stop, until it is stopped itself, and stopping it stops both slices beneath it. Nothing of the slice is removed, since it has no file. First it asks `systemctl --user show` for each unit's active state. Second, it stops, one `systemctl --user stop` at a time, each unit whose path still holds a regular file, directly or through a symbolic link, or whose state is anything but `inactive`. A unit whose path holds a valid unit file, as every file `up` writes is, is loaded and stopping it succeeds; a unit whose file is gone can still be running if something deleted the file and reloaded the manager, and the manager still holds it, so a stop succeeds; but a unit that reads `inactive` and whose path holds nothing, a directory or a dangling symbolic link is not loaded, and stopping it fails, so it is skipped. Third, once every stop has succeeded, it removes the unit files; whatever sits at a unit file's path, even a directory, is that unit's file and is removed whole, since nothing else may own a name sandbox gave a unit. Fourth, it runs one `systemctl --user daemon-reload`, always, even when no file was removed, so a run that follows a teardown which failed after removing the files still makes the manager forget them. Fifth, it asks each unit's state again and, directly after each answer of `failed`, resets that unit's failed state before asking the next unit: systemd never unloads a failed unit until its failed state is reset, so without this step the manager would still know a unit that crashed, and `status` would report it on a down sandbox. Sixth, it removes the generated entries under the data directory, a staging directory left by a killed `up` among them; the kept entries, the data directory itself and the registry entry stay.

Each step happens only after every earlier step has succeeded, and the first failing program run or removal ends the command with D02's diagnostic. So a failed `down` leaves exactly what it had not yet removed: a failed state query or stop leaves every unit file and generated entry in place, a failed daemon-reload leaves the generated entries, and so on. Every step is safe to repeat (stopping a loaded inactive unit succeeds, a missing file is skipped), so running `down` again once the fault is gone finishes the job.

`wipe` refuses a sandbox that is up, by D03's rule, since data is deleted only once nothing runs against it. A sandbox that reads down may still hold unit files and generated entries, after a reboot for example, and may even have an app's units running when nginx failed to start, so `wipe` runs the whole teardown first, then deletes the data directory, and only then removes the registry entry, which frees the port. A `wipe` that fails part-way keeps the registry entry, so it can be run again. The worktree is never touched.

`ls` reads the registry and asks systemd for each sandbox's state in name order, stopping at the first failure, and prints a table only when every answer came back. It takes no lock and needs no checkout.

## REQUIREMENTS

### Program runs

- R-OB3F-0G6F: A stop run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `stop` and the unit's name, and whose `Dir` is `/`.

- R-ODJ7-RZNT: A daemon-reload run MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user` and `daemon-reload`, and whose `Dir` is `/`.

- R-OFZ0-JJ57: A reset-failed run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `reset-failed` and the unit's name, and whose `Dir` is `/`.

### The teardown

- R-JH81-EDDD: `down` MUST begin its program runs, once it holds the sandbox lock, with one unit state run for each of the sandbox's unit names (D03 R-LTAU-0RJC), in the order R-JB4J-HINW gives the stop runs, and then one unit state run of its sandbox slice (D03 R-FZIE-PWVJ), verified for sandbox `wip` recording `auth` and `dummy` by the unit state runs of `sandbox-wip-nginx.service`, `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket`, `sandbox-wip-dummy.service` and `sandbox-wip.slice`, in that order, and for sandbox `wip-cgroups` by its last unit state run being of `sandbox-wip\x2dcgroups.slice`.

- R-GRK3-IMXI: `down` MUST run a stop run for exactly those of the sandbox's unit names whose unit file path in the unit directory, looked up following symbolic links once `down` holds the sandbox lock, is a regular file, or whose first unit state run returned a `Stdout` that, with a trailing newline removed, is not `inactive`, one stop run per such unit, and no stop run for any other of the sandbox's unit names, where a directory, a dangling symbolic link or any other entry at a unit file's path that is not, following symbolic links, a regular file does not count as a unit file for this choice, verified at least by a unit with a regular file whose state is `inactive` (stopped), a unit whose path holds a symbolic link to a regular file and whose state is `inactive` (stopped), a unit whose path holds a dangling symbolic link and whose state is `inactive` (not stopped, its path removed, `down` returning 0), a unit whose path holds a directory and whose state is `inactive` (not stopped, its path removed, `down` returning 0), a unit with no file whose state is `active` (stopped), a unit with no file whose state is `failed` (stopped), and a unit with no file whose state is `inactive` (not stopped).

- R-JB4J-HINW: `down` MUST make its stop runs of the sandbox's unit names in this order: the nginx unit, then each recorded app in ascending byte order of its name, the app's socket unit immediately before its service unit, verified with both apps' and nginx's unit files present by a recorded order of `sandbox-wip-nginx.service`, `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket`, `sandbox-wip-dummy.service`.

- R-JKVQ-JOLG: `down` MUST make a stop run of its sandbox slice (D03 R-FZIE-PWVJ) exactly when the slice's first unit state run returned a `Stdout` that, with a trailing newline removed, is not `inactive`, and that stop run MUST be made after every stop run of the sandbox's unit names; and MUST make no stop run of any unit that is neither one of the sandbox's unit names nor its sandbox slice, so its core and apps slices are never stopped by name; verified at least for sandbox `wip` by a slice reading `active` with every unit file present (its stop run of `sandbox-wip.slice` directly after that of `sandbox-wip-dummy.service`), a slice reading `failed` (stopped), a slice reading `inactive` while every unit file is present and every unit reads `active` (not stopped, though every unit is), a regular file at `<units>/sandbox-wip.slice` while the slice reads `inactive` (not stopped, and the file left unchanged), and for sandbox `wip-cgroups` by a slice reading `active` (a stop run of `sandbox-wip\x2dcgroups.slice`).

- R-JG05-0LMO: `down` MUST NOT remove any unit file before its last stop run has returned with `ExitCode` 0: at each of its unit state runs before its daemon-reload run and at each of its stop runs, every unit file of the sandbox present once `down` held the sandbox lock MUST still be present.

- R-JCCF-VAEL: After its stop runs have all succeeded, `down` MUST remove each of the sandbox's unit files present in the unit directory, one at a time in the order R-JB4J-HINW gives the stop runs, where an entry of any kind at a unit file's path, a directory included, counts as that unit file and is removed with everything under it, verified by a directory holding a file at `<units>/sandbox-wip-dummy.service` and a dangling symbolic link at `<units>/sandbox-wip-nginx.service` each being removed by a `down` that succeeds, and by making `<units>/sandbox-wip-auth.socket` a directory holding a read-only, non-empty subdirectory: `down` MUST fail with `sandbox-wip-nginx.service` gone and the files of `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service` still present.

- R-DL6C-4HKA: `down` MUST make exactly one daemon-reload run, also when no unit file of the sandbox existed.

- R-DNM4-W11O: At the time of `down`'s daemon-reload run, no file named by one of the sandbox's unit names MUST remain in the unit directory.

- R-JIFX-S542: After its daemon-reload run has succeeded, `down` MUST make one unit state run for each of the sandbox's unit names, whether or not its unit file existed, in the order R-JB4J-HINW gives the stop runs, and then one unit state run of its sandbox slice (D03 R-FZIE-PWVJ), whether or not it was stopped, verified at least for sandbox `wip-cgroups` by the last unit state run after the daemon-reload run being of `sandbox-wip\x2dcgroups.slice`.

- R-JJNU-5WUR: `down` MUST make a reset-failed run for exactly those of the sandbox's unit names and its sandbox slice whose unit state run after the daemon-reload run returned `ExitCode` 0 and a `Stdout` that, with a trailing newline removed, is `failed`, no reset-failed run for any other unit, and each reset-failed run directly after that unit's state run, before the next unit's state run, verified with the unit state runs after the daemon-reload run of `sandbox-wip-auth.service` and `sandbox-wip-dummy.socket` answered with `ExitCode` 0 and `Stdout` `failed` followed by a newline, and every other unit state run with `ExitCode` 0 and `Stdout` `inactive` followed by a newline, by the runs after the daemon-reload run being, in this order, the unit state runs of `sandbox-wip-nginx.service`, `sandbox-wip-auth.socket` and `sandbox-wip-auth.service`, the reset-failed run of `sandbox-wip-auth.service`, the unit state run of `sandbox-wip-dummy.socket`, the reset-failed run of `sandbox-wip-dummy.socket`, and the unit state runs of `sandbox-wip-dummy.service` and `sandbox-wip.slice`; and with only the slice's unit state run after the daemon-reload run answered `failed`, by the reset-failed run of `sandbox-wip.slice` directly after it, as `down`'s last program run.

- R-OPQ7-LP2R: Every generated entry under the data directory present once `down` held the sandbox lock MUST still be present at each program run `down` makes.

- R-53P6-QDDU: When `down` succeeds, none of `<data>/bin`, `<data>/stage`, `<data>/env`, `<data>/services.json` and `<data>/nginx` MUST exist, verified at least with each of these five entries present before `down` and `<data>/stage` holding a file at `<data>/stage/bin/dummy`.

- R-DR9U-1C9R: When `down` succeeds, `<data>` MUST still exist when it existed before.

- R-DSHQ-F40G: Whether it succeeds or fails, `down` MUST leave `<data>/apps` and `<data>/token`, and every file and directory under them, with the same content, mode and inode as before, verified at least by a successful `down` and by one failing at the removal of a generated entry.

- R-OS60-D8K5: `down` MUST leave `registry.json` unchanged, neither rewriting nor replacing it, whether it succeeds or fails.

- R-OULT-4S1J: `down` MUST NOT create any file or directory other than a lock file and its missing parent directories, verified at least by a known sandbox whose data directory and unit directory do not exist, after which neither exists.

- R-OVTP-IJS8: When `down` succeeds it MUST write nothing to stdout or stderr and return 0, verified at least for a sandbox that is up, one already down with no unit file and no generated entry, one reached by name whose recorded worktree does not exist, and one reached by name from a `Deps.Dir` outside any checkout.

- R-OX1L-WBIX: Once one of its program runs returns a non-zero `ExitCode` or an error, or one of its removals fails, `down` MUST run nothing further through either runner and remove nothing further.

### Teardown failures

- R-OY9I-A39M: When a stop run of a unit returns a non-zero `ExitCode`, `down` MUST fail with D02's external-failure diagnostic with the action `stop <unit>`, `<unit>` the unit's name and no detail, verified by a stop of `sandbox-wip-nginx.service` answered with `ExitCode` 1 and `Output` `Failed to stop sandbox-wip-nginx.service: Transport endpoint is not connected` and a newline writing exactly `sandbox: stop sandbox-wip-nginx.service: exit status 1`, an empty line, and `> Failed to stop sandbox-wip-nginx.service: Transport endpoint is not connected`, each ending in a newline.

- R-OZHE-NV0B: When `Deps.Exec` returns an error for a stop run of a unit, `down` MUST fail with D02's runner-error diagnostic with the action `stop <unit>`, `<unit>` the unit's name.

- R-P0PB-1MR0: When the daemon-reload run returns a non-zero `ExitCode` or an error, `down` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `systemctl --user daemon-reload` and no detail.

- R-DTPM-SVR5: When a unit state run of a unit made by `down` returns a non-zero `ExitCode` or an error, `down` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `show <unit>`, `<unit>` the unit's name, and no detail.

- R-P353-T68E: When a reset-failed run of a unit returns a non-zero `ExitCode` or an error, `down` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `reset-failed <unit>`, `<unit>` the unit's name, and no detail.

- R-P4D0-6XZ3: When `down` cannot remove one of the sandbox's unit files, it MUST fail with D02's file-error diagnostic for that unit file's path and make no daemon-reload run, verified with the unit directory made read-only after the unit files were written.

- R-TWRP-I1T0: When `down` cannot remove a generated entry, it MUST fail with D02's file-error diagnostic for that entry's path (`<data>/bin`, `<data>/stage`, `<data>/env`, `<data>/services.json` or `<data>/nginx`), verified with `<data>/bin` holding a file and made read-only, writing exactly `sandbox: <data>/bin: permission denied`.

- R-JDKC-925A: After a `down` that failed at any one of its program runs or removals, a second `down` of the same sandbox whose program runs all succeed and whose removals are all possible MUST succeed and leave no file named by one of the sandbox's unit names in the unit directory and none of the generated entries under `<data>`, having made a daemon-reload run, verified at least for a first `down` failing at the first unit state run of `sandbox-wip-auth.socket`, at the stop of `sandbox-wip-nginx.service`, at the stop of `sandbox-wip-dummy.service`, at the removal of `<units>/sandbox-wip-auth.socket` set up as in R-JCCF-VAEL (the obstruction removed before the second `down`), at the daemon-reload run, at a unit state run after the daemon-reload run, at a reset-failed run, at the removal of `<data>/bin`, at the first unit state run of `sandbox-wip.slice`, at the stop of `sandbox-wip.slice`, and at the reset-failed run of `sandbox-wip.slice`, the second `down` making the stop run of `sandbox-wip.slice` whenever its first unit state run of the slice reads `active`.

### wipe

- R-IR6A-V8NR: Once `wipe` holds the sandbox lock, the first program it runs MUST be its sandbox's state run (D03).

- R-P98L-Q0XV: When the state run finds the sandbox up (D03 R-LX3F-ASMX), `wipe` MUST write the is-up diagnostic, `sandbox: sandbox '<name>' is up`, one empty line, and `run 'sandbox down <name>' first`, to stderr, with `<name>` the sandbox name whether or not a name was given, write nothing to stdout, and return 2.

- R-PAGI-3SOK: When `wipe` writes the is-up diagnostic, it MUST run nothing further through either runner and leave every file and directory under the data root and the unit directory unchanged, lock files aside.

- R-DXDB-Y6Z8: For `wipe`, the action of the external-failure and runner-error diagnostics a failed state run calls for (D03 R-LYBB-OKDM, R-LZJ8-2C4B) MUST be `systemctl --user`, with no detail.

- R-PCWA-VC5Y: When the state run of `wipe` fails, `wipe` MUST run nothing further through either runner and leave every file and directory under the data root and the unit directory unchanged, lock files aside.

- R-SGZQ-J9GL: When the state run finds the sandbox down, `wipe` MUST next perform the teardown of `down`: given the same registry, unit files, generated entries and runner answers, the `seam.Cmd` values `wipe` runs after its state run MUST be exactly the sequence `down` runs, verified at least with every unit file and generated entry present and with none present.

- R-PGK0-0NE1: When its teardown fails, `wipe` MUST write the same diagnostic and return the same exit code as `down` failing at the same point, and leave `<data>/apps`, `<data>/token` and `registry.json` unchanged.

- R-PHRW-EF4Q: At each program run `wipe` makes, the registry MUST still hold the sandbox's entry and `<data>/apps` and `<data>/token` MUST be as they were when `wipe` acquired the sandbox lock.

- R-PIZS-S6VF: When `wipe` succeeds, `<data>` MUST no longer exist.

- R-PK7P-5YM4: When `wipe` succeeds, `registry.json` MUST hold no entry with the sandbox's name and MUST hold every other entry exactly as before.

- R-PLFL-JQCT: When `wipe` cannot remove `<data>` or anything under it, it MUST fail with D02's file-error diagnostic for the path `<data>` and leave `registry.json` unchanged, verified with `<data>/apps/auth/state` holding a file and made read-only, writing exactly `sandbox: <data>: permission denied`.

- R-PMNH-XI3I: When `wipe` succeeds it MUST write nothing to stdout or stderr and return 0, verified at least for a sandbox found from its worktree, one reached by name from a `Deps.Dir` outside any checkout, one reached by name whose recorded worktree does not exist, and one whose unit files and generated entries are still present while its nginx unit reads not active.

- R-PNVE-B9U7: `wipe` MUST leave every other sandbox's data directory and lock file unchanged.

- R-DYL8-BYPX: `wipe` MUST leave the sandbox's recorded worktree, and every file and directory under it, unchanged.

- R-50OX-YW52: When `wipe` has removed `<data>` but cannot write `registry.json`, it MUST fail with D02's file-error diagnostic for the path `<root>/registry.json`, verified by a `Deps.Exec` fake that, while answering `wipe`'s last program run, replaces `registry.json` with a non-empty directory: `wipe` MUST write a line beginning `sandbox: <root>/registry.json: ` to stderr, nothing to stdout, and return 1, with `<data>` gone.

- R-Y6XO-N4SV: After a `wipe` that failed at removing `<data>` or at writing `registry.json`, a second `wipe` of the same sandbox whose program runs all succeed and whose removals and writes are all possible MUST succeed, leaving `<data>` absent and `registry.json` holding no entry with the sandbox's name and every other entry exactly as before, verified, each time with a second sandbox also in the registry, at least after a first `wipe` that failed as in R-PLFL-JQCT and then had the obstruction removed, after one that failed as in R-50OX-YW52 and then had the directory replaced by a `registry.json` holding exactly the bytes it held before that first `wipe` began, the sandbox's entry included, with `<data>` already gone, and for a registry entry whose `<data>` was never created.

### ls

- R-ISE7-90EG: `ls` MUST make its state runs, one per registry entry, each the state run (D03) of the sandbox that entry names, in ascending byte order of name whatever order the registry lists them in, make none after the first that fails, and run no other program through either runner.

- R-PQB7-2TBL: When the registry holds no entry, or does not exist, `ls` MUST write nothing to stdout or stderr, run nothing through either runner, and return 0.

- R-PRJ3-GL2A: When every state run succeeds, `ls` MUST write to stdout a header row whose cells are `NAME`, `PORT`, `STATE` and `WORKTREE`, followed by one row per registry entry in ascending byte order of name whatever order the registry lists them in, whose cells are the entry's name, its port in decimal, its state, and its worktree; MUST write nothing to stderr; and MUST return 0.

- R-PSQZ-UCSZ: Each row `ls` writes MUST be its cells joined in order, each cell but the last followed by spaces to the width of the widest cell of its column (the header's included) plus two, the last cell not padded, and the row ending in a newline, verified by a registry holding `wip` (port 7400, up, worktree existing) and `other` (port 7401, down, worktree removed) writing exactly `NAME   PORT  STATE  WORKTREE`, `other  7401  down   <other worktree> (gone)` and `wip    7400  up     <wip worktree>`, each ending in a newline.

- R-PTYW-84JO: An entry's state cell MUST be `up` when its state run finds it up and `down` when its state run finds it down (D03 R-LX3F-ASMX).

- R-E28X-H9Y0: An entry's worktree cell MUST be its recorded worktree followed by ` (gone)` exactly when looking that path up, following symbolic links, fails with `ENOENT` or `ENOTDIR`, and its recorded worktree alone otherwise, verified at least by an existing directory (unmarked), a removed directory (marked), a symbolic link to a removed directory (marked), a path below a regular file (marked), and a path inside a directory of mode 000 (unmarked).

- R-E3GT-V1OP: When a state run of `ls` fails, `ls` MUST write nothing to stdout and report that run's failure, verified by a registry listing `wip` before `other` whose state runs both return `ExitCode` 1 with different `Output`, the diagnostic quoting the `Output` of `other`.

- R-DW5F-KF8J: For `ls`, the action of the external-failure and runner-error diagnostics a failed state run calls for (D03 R-LYBB-OKDM, R-LZJ8-2C4B) MUST be `systemctl --user`, with no detail.
