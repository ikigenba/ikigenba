# Stories — verification

What repos checks of its repositories as it starts, and how it rebuilds its catalog when the catalog is gone. A repository lives in two places: its row in the catalog, `state/repos.db`, which holds its id, name, owner, created time, and whether it is available; and its directory, the bare git repository `state/repos/<id>.git` (`S15`). Each start, after it has opened the database (`S02`) and before it tells systemd it is ready, repos verifies every repository the catalog names, one by one: it opens the repository's directory as a bare git repository, as git does before it runs any command in it. That check finds a directory that is missing, or one git cannot open as a bare repository — its `HEAD`, its `config`, or its `objects/` or `refs/` directory gone or unreadable; it does not walk the repository's objects, so damage deep in its history is not this check's to find. A repository that fails is marked unavailable: `list`, `show`, and `status` give it `available` `false` (`S07`, `S10`), git's requests for it are answered `503` with the one line `repository unavailable` (`S11`), and its maintenance is skipped (`S13`). repos records `repo.unavailable` for it, with `repo` its id, under an empty request id and an empty user, and goes on: one broken repository never keeps repos from serving the rest. A repository that verifies is available, whatever it was before, so a repository repaired while repos was stopped is available again on the next start. Verification only reads: it changes no repository's directory, creates none, and removes none. It writes nothing to stderr, as a handled failure never does (`S02`); a broken repository is in the trail and the tools. The `repo.unavailable` events of a start are recorded before its `service.started`, since verification comes before repos is ready. The catalog shapes below follow the caller `u_7f3a9c21`, whose repositories are `notes`, `rep_3f9a0c1d2e4b5a69`, and `site`, `rep_8c21d4e0f7a3b915`; `<size>` is a repository's size on disk in bytes and `<sha>` the 40-hexadecimal-digit sha its `main` names. The actor is the host, and the paths are relative to repos' working directory, `/opt/repos` on a host.

A repository's directory carries its own identity, so the catalog can be rebuilt from the disk alone. At creation repos writes `ikigenba.id`, `ikigenba.name`, `ikigenba.owner`, and `ikigenba.created` into the repository's own git config (`S06`), and keeps `ikigenba.name` current through a rename (`S08`). On a host the database is replicated (`S01`), and a host rebuilt from scratch gets it back from its replica before repos starts; rebuilding from disk is for when there is no replica to restore. repos rebuilds only when `state/repos.db` is absent at start, or names no repository and no earlier start finished opening it, and `state/repos/` holds directories. So a first start that is killed, or refused with `repos: cannot open database state/repos.db: <reason>` (`S02`), before it has finished opening the database leaves nothing the next start mistakes for a catalog: the next start rebuilds from the directories as if the database were absent. A database an earlier repos left naming no repository is rebuilt the same way. Any other database is the catalog, even one whose every repository has since been deleted, and a directory it does not name is left alone, neither served nor removed.

## The host starts repos over sound repositories

The common start: every repository the catalog names is where it should be and opens, so every one is available and the trail holds nothing but the start.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is installed and `ikigenba-repos.service` is not running, as in `S02`'s `The host starts repos`.
- `state/repos.db` names `notes` and `site`, both owned by `u_7f3a9c21`, and `state/repos/rep_3f9a0c1d2e4b5a69.git` and `state/repos/rep_8c21d4e0f7a3b915.git` are sound bare repositories.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- repos is serving, and `list` for `u_7f3a9c21` answers:

  ```
  {"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":<size>,"head":"<sha>","available":true},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<size>,"head":"<sha>","available":true}]}
  ```

- Cloning either repository works (`S11`).
- telemetry has received `service.started` from this start and no `repo.unavailable`.
- Both directories are as they were, and repos has written nothing to the journal.

## The host starts repos when a repository's directory is missing

A repository's directory can go while its row stays — removed by hand, or lost from a disk the database was restored without. repos cannot serve what is not there, and it does not make an empty repository in its place, which would hide the loss behind a repository that clones as if it had never had a commit. It marks the repository unavailable, says so in the trail, and serves the rest. The repository keeps its name, so its owner cannot reuse it by accident.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is installed and `ikigenba-repos.service` is not running.
- `state/repos.db` names `notes` and `site`, both owned by `u_7f3a9c21`, both available.
- `state/repos/rep_3f9a0c1d2e4b5a69.git` does not exist; `state/repos/rep_8c21d4e0f7a3b915.git` is a sound bare repository.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- repos is serving. `notes` is unavailable and `site` is not; `list` for `u_7f3a9c21` answers:

  ```
  {"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":0,"available":false},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<size>,"head":"<sha>","available":true}]}
  ```

  A repository whose directory is missing has nothing on disk, so its `size_bytes` is `0` and its entry has no `head` member.
- `git clone https://repos.sbx.ikigenba.dev/notes.git`, through the gate as `u_7f3a9c21`, fails: repos answered `503` with the one line `repository unavailable`, with no `Retry-After` (`S11`). Cloning `site` works.
- telemetry has received, before this start's `service.started`:

  ```
  {"time":"<time>","service":"repos","event":"repo.unavailable","request_id":"","user":"","attrs":{"repo":"rep_3f9a0c1d2e4b5a69"}}
  ```

- `state/repos/rep_3f9a0c1d2e4b5a69.git` still does not exist; repos created nothing in its place. `state/repos/rep_8c21d4e0f7a3b915.git` is as it was.
- repos has written nothing to the journal.

## The host starts repos when a repository's directory is broken

A directory that is there but that git cannot open as a bare repository — a disk fault, or a hand that deleted `HEAD` or `objects/` — is as unservable as a missing one, and is treated the same. repos leaves the directory exactly as it found it, so whoever repairs it has everything that was there.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is installed and `ikigenba-repos.service` is not running.
- `state/repos.db` names `notes` and `site`, both owned by `u_7f3a9c21`, both available.
- `state/repos/rep_3f9a0c1d2e4b5a69.git` exists, but its `HEAD` file has been deleted, so git cannot open it as a repository; `state/repos/rep_8c21d4e0f7a3b915.git` is a sound bare repository.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- repos is serving. `show` of `notes` for `u_7f3a9c21` answers with `available` `false` (`S07`), and git's requests for `notes` are answered `503` with the one line `repository unavailable`; `site` is available and served.
- telemetry has received `repo.unavailable` with `repo` `rep_3f9a0c1d2e4b5a69`, under an empty request id and an empty user, before this start's `service.started`.
- Every file in `state/repos/rep_3f9a0c1d2e4b5a69.git` is as it was, and `HEAD` is still absent. `state/repos/rep_8c21d4e0f7a3b915.git` is as it was.
- repos has written nothing to the journal.

## The host starts repos after a broken repository is repaired

Unavailable is what verification found, not a verdict that sticks: an operator who restores a repository's directory, from a backup or by repairing it in place, while repos is stopped, gets it back by starting repos again. Nothing else is needed, and nothing is recorded for the recovery beyond the start: the absence of a `repo.unavailable` is how the trail shows it.

Command:

```
$ sudo systemctl restart ikigenba-repos.service
```

Output:

```
```

Exits 0, once the new repos has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- repos is serving with `notes` unavailable, as `The host starts repos when a repository's directory is broken` left it.
- Its `HEAD` has since been put back, so `state/repos/rep_3f9a0c1d2e4b5a69.git` is a sound bare repository again.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `list` for `u_7f3a9c21` gives `notes` with `available` `true`, its `head` the sha its `main` names, and its `size_bytes` its size on disk.
- `git clone https://repos.sbx.ikigenba.dev/notes.git`, through the gate as `u_7f3a9c21`, succeeds, and `notes` is maintained again (`S13`).
- telemetry has received the new repos' `service.started` and no `repo.unavailable` from this start.

## The host starts repos with no database but repositories on disk

The database is gone — deleted, or a host rebuilt with its repositories restored and no replica of the catalog — and the repositories are still in `state/repos/`. repos does not start with an empty catalog, which would leave every repository on disk unreachable; it creates the database and rebuilds the catalog from the directories. Each directory named `<id>.git` (a directory, not a symbolic link) whose own `config` file carries all four `ikigenba.*` values, valid, with `ikigenba.id` matching its directory's name, becomes a row with that id, name, owner, and created time, and is then verified like any other repository, so one whose `HEAD` or `objects/` has gone is catalogued, its owner known from its config, and found unavailable, with its `repo.unavailable`. When two such directories claim the same owner and name, the one whose `ikigenba.created` is earliest, the smaller id breaking a tie, becomes the row, and each other is left on disk untouched and uncatalogued and is reported with `repo.unavailable`, its id as `repo`, before this start's `service.started`, so an operator can act. Rebuilding is not creating: it records no `repo.created`, and nothing for the rebuild itself.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is installed and `ikigenba-repos.service` is not running.
- `state/repos.db` does not exist.
- `state/repos/` holds `rep_3f9a0c1d2e4b5a69.git` and `rep_8c21d4e0f7a3b915.git`, both sound bare repositories, whose git configs hold, in turn:

  ```
  [ikigenba]
  	id = rep_3f9a0c1d2e4b5a69
  	name = notes
  	owner = u_7f3a9c21
  	created = 2026-09-30T10:15:00Z
  ```

  ```
  [ikigenba]
  	id = rep_8c21d4e0f7a3b915
  	name = site
  	owner = u_7f3a9c21
  	created = 2026-10-01T09:00:00Z
  ```

- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `state/repos.db` exists, created by this start and up to date (`S01`), and names both repositories.
- `list` for `u_7f3a9c21` answers both, available, as in `The host starts repos over sound repositories`; `show` of `notes` gives `created` `2026-09-30T10:15:00Z`; and `list` for `u_2b8e1d04` answers `{"repos":[]}`.
- Cloning and pushing to either repository through its old clone URL works (`S11`).
- telemetry has received this start's `service.started`, and no `repo.created` and no `repo.unavailable`.
- Both directories are as they were.
- repos has written nothing to the journal.

## The host starts repos with no database and a directory it cannot identify

A directory in `state/repos/` that does not say whose repository it is — a bare repository with no `ikigenba.*` values in its git config, or not all four of them, or an `ikigenba.id` that is not its directory's name, or no `config` file to carry them, as in a directory that is no git repository at all, or a symbolic link named `<id>.git` rather than a directory — cannot be put back in the catalog without guessing an owner, and a guessed owner could hand one user's content to another. repos leaves it untouched and uncatalogued, and rebuilds the rest. It records nothing for it, since there is no repository id to name; the directory stays where an operator can look at it.

Command:

```
$ sudo systemctl start ikigenba-repos.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- repos is installed and `ikigenba-repos.service` is not running.
- `state/repos.db` does not exist.
- `state/repos/` holds `rep_3f9a0c1d2e4b5a69.git`, a sound bare repository whose git config holds its four `ikigenba.*` values as in `The host starts repos with no database but repositories on disk`, and `rep_5d0e7b2a9c4f1863.git`, a sound bare repository whose git config holds no `ikigenba.*` value.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `state/repos.db` exists, created by this start and up to date (`S01`), and names `notes`, `rep_3f9a0c1d2e4b5a69`, alone.
- `list` for `u_7f3a9c21` answers `notes` alone, available; no caller's `list` names `rep_5d0e7b2a9c4f1863`, and `show` of it for any caller answers `repo: no repository 'rep_5d0e7b2a9c4f1863'` (`S07`).
- `state/repos/rep_5d0e7b2a9c4f1863.git` is exactly as it was, its config unchanged.
- telemetry has received this start's `service.started`, and no event naming `rep_5d0e7b2a9c4f1863`.
- repos has written nothing to the journal.
