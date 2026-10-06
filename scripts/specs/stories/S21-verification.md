# Stories — verification

What scripts checks as it starts, and what it deliberately leaves unchecked. scripts keeps its state in two places: the catalog, `state/scripts.db`, which holds every script's id, name, owner, repository, ref and creation time and every run's record, and is the only record of them; and the run folders, `state/runs/<script id>/<run id>/`, which hold each run's tree, input, output and files, are the product and not a cache, and are never rebuilt (`S20`). Each start checks only what scripts needs to serve at all, in the order `S02` gives: its settings, its socket, that `git` is on its `PATH`, that `python3.12` is (`S22`), that the catalog opens (created, empty, when it is absent) and is brought up to date with every migration scripts carries (`S01`), and that `state/runs/` exists (created when it is absent). A start that fails one of these is refused with the messages `S02` and `S22` fix (`The host starts scripts where git is not installed`, `where python3.12 is not installed`, `with a database it cannot open`, `with a database a newer scripts has upgraded`, `where its state directory cannot be created`, `where its runs directory cannot be created`). Then it prepares the control group the host delegates to it, in which it runs every script (`S02`); when it cannot, it still starts and serves, and writes the one line `S02` fixes, `scripts: runs are unavailable: <reason>` (`The host starts scripts without a delegated control group`), and refuses every `run`. Then it settles what a previous process left: it marks every run still recorded `running` as `killed` (`S18`) and prunes the runs past keeping, row and folder together (`S19`). Beyond these scripts checks nothing before it is ready: it does not look in `REPOS_DIR` or at any repository, it does not look for, read, or require any run's folder, and it unpacks, rebuilds, and rewrites no tree. A repository is looked at only when a tool, a run or a page needs it, and a run whose folder is missing is shown as one whose files are gone (`S20`), never as a start's refusal and never as an event of the start. So a start records only its `service.started`, beside what `S18` tells of the runs it marks `killed`, and writes nothing to stderr beyond what `S02` gives. scripts never rebuilds its catalog: a lost `state/scripts.db` cannot be recovered from the run folders, and comes back only from the host's replica (`S02`). The actor is the host, and the paths are relative to scripts' working directory, `/opt/scripts` on a host.

## The host starts scripts over a sound catalog and run folders

The common start: the catalog opens, `state/runs/` is there, and every kept run's folder is in it. scripts is ready without having looked at any folder or repository, and shows each run from its folder as it stands. A script whose repository is gone from repos, `backfill`'s, makes no difference to the start.

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- scripts is installed and `ikigenba-scripts.service` is not running, as in `S02`'s `The host starts scripts`.
- `state/scripts.db` holds `S06`'s shared catalog: `nightly-report`, `sync-crm`, `rotate-keys`, `backfill`, and `digest`, and their runs, none of which is recorded as `running` and none past what `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` keep.
- `state/runs/` holds the folder of every run but `nightly-report`'s `run_72b0c8f5e3d1a946`, whose folder is gone.
- `/opt/repos/state/repos/` holds every repository `S06`'s shared catalog names but `rep_0f6a2d9e8c4b7153.git`.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- scripts is serving, and `list` for `u_7f3a9c21` answers `backfill`, `nightly-report`, `rotate-keys`, and `sync-crm`, sorted by name, as before (`S07`).
- `result` of `run_3f9a1c2e8b7d4a60` answers its `stdout`, `stderr`, and `files` from its folder as it stands, and `result` of `run_72b0c8f5e3d1a946` answers `files_gone` `true` (`S11`). No git and no script ran.
- telemetry has received this start's `service.started` and nothing else from it.
- `state/scripts.db` names what it named before, and every file under `state/runs/` is as it was.
- scripts has written nothing to the journal.

## The host starts scripts with its runs directory emptied

An operator may empty `state/runs/` to reclaim disk, or a host may be rebuilt without it. scripts starts over an empty `state/runs/`, or with no `state/runs/` at all, exactly as over a full one, creating it when it is absent. It does not recreate any run's folder: unlike a cache, a run folder holds what only the run produced, and nothing brings it back. Every run stays in the catalog, its files shown as gone.

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- scripts is installed and `ikigenba-scripts.service` is not running.
- `ikigenba-scripts.service` delegates its control group (`S01`), and the host offers that group the `cpu`, `memory` and `pids` controllers.
- `state/scripts.db` holds `S06`'s shared catalog, none of its runs recorded as `running` and none past keeping.
- `state/runs/` does not exist, or exists and is empty.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- scripts is serving, and `state/runs/` exists, empty: the start unpacked nothing and ran no git.
- `runs` of `nightly-report` answers the same runs as before the start (`S11`), and `result` of each answers its details with `files_gone` `true` and no `stdout`, `stderr`, or `files` (`S20`); each run's page shows `Files no longer kept` (`S13`).
- A new `run` of `nightly-report` makes its own folder under `state/runs/scr_6d1f4a9b2e8c7035/` as on any day (`S08`).
- telemetry has received this start's `service.started` and nothing else from it.
- scripts has written nothing to the journal.

## The host starts scripts with a catalog restored from its replica and no run folders

A host rebuilt from scratch gets `state/scripts.db` back from its replica before scripts starts, but the run folders were never replicated, so the catalog names runs whose folders are nowhere on disk. That is the same as an emptied runs directory: scripts starts, every script and every run is listed as it was, and every run's files are gone. A run the replica still records as `running` ran on the lost host and is marked `killed`, as at any start (`S18`). Where repos has been restored too, a new run of a script resolves and unpacks from its repository as before; where a repository has not come back, a run of its script fails to start with `repository_missing` until it does (`S08`).

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- scripts is installed and `ikigenba-scripts.service` is not running.
- `ikigenba-scripts.service` delegates its control group (`S01`), and the host offers that group the `cpu`, `memory` and `pids` controllers.
- `state/scripts.db` was restored from its replica and holds `S06`'s shared catalog, in which `nightly-report`'s `run_8a2c6e1f9b3d5074` and `sync-crm`'s `run_6b2d8f4a0c9e1735` are recorded as `running`.
- `state/runs/` does not exist.
- repos' repositories were restored too: `REPOS_DIR` holds `rep_9c2e4b7a1d3f8e05.git`, `rep_41d8f0a6b2c97e13.git`, and `rep_7b3e9a0c5d1f2846.git`; `rep_d41c7a9e05b28f63.git` has not come back.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- scripts is serving; `state/runs/` exists, created by this start, and empty.
- `list` for each caller answers the scripts it answered before the host was lost (`S07`), and `runs` of each script answers its runs, `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` now `killed` (`S18`), the rest as they were.
- `result` of any run answers `files_gone` `true` (`S20`).
- A `run` of `nightly-report` by `u_7f3a9c21` resolves `main` in the restored `rep_9c2e4b7a1d3f8e05.git` and answers `running` with its `sha` (`S08`). A `run` of `digest` by `u_2b8e1d04` answers `failed` with `reason` `repository_missing` (`S08`), and runs again once `rep_d41c7a9e05b28f63.git` is back, with no restart of scripts.
- Beyond what `S18` tells of the two runs it marked `killed`, telemetry has received only this start's `service.started` from the start itself.
- scripts has written nothing to the journal.

## The host starts scripts before repos' repositories exist

scripts may be installed before repos, or `REPOS_DIR` may name a directory that is not there yet. scripts does not look in `REPOS_DIR` at start, so it starts all the same, and creates nothing there: the directory is repos', and scripts never writes in it. Until a repository appears, `create` answers `no repository '<repo>'` for it (`S06`), and a run of a script over it fails to start with `repository_missing` (`S08`); neither needs a restart once repos is there, since the directory is looked at afresh each time.

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- scripts is installed and `ikigenba-scripts.service` is not running.
- `ikigenba-scripts.service` delegates its control group (`S01`), and the host offers that group the `cpu`, `memory` and `pids` controllers.
- `state/scripts.db` exists and names no script, or does not exist and is created as on a first start (`S02`).
- `REPOS_DIR` is unset, so it is `../repos/state/repos`, and `/opt/repos/state/repos` does not exist: repos is not installed yet.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- scripts is serving, and telemetry has received this start's `service.started`.
- `/opt/repos/state/repos` still does not exist; scripts created nothing outside its own working directory.
- `create` by `u_7f3a9c21` of `nightly-report` with `repo` `rep_9c2e4b7a1d3f8e05` is refused `no repository 'rep_9c2e4b7a1d3f8e05'` (`S06`). Once repos is installed and holds that repository, owned by `u_7f3a9c21`, the same call makes the script, with no restart of scripts.
- scripts has written nothing to the journal.
