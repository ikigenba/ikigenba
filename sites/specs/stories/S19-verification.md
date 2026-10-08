# Stories — verification

What sites checks as it starts, and what it deliberately leaves unchecked. sites keeps its state in two places: the catalog, `state/sites.db`, which holds every site's id, name, slug, owner, repository, ref, visibility, listing, published commit and times, and the apex setting, and is the only record of them; and the cache, `cache/sites/<site id>/<sha>/`, which holds the unpacked tree of each published commit and is disposable, rebuilt from the repository on demand (`S16`). Each start checks only what sites needs to serve at all, in the order `S02` gives: that `git` is on its `PATH`, that the catalog opens (created, empty, when it is absent) and is brought up to date with the migrations sites carries (`S01`), and that `cache/sites/` exists (created when it is absent). A start that fails one of these is refused with the messages `S02` fixes (`The host starts sites where git is not installed`, `with a database it cannot open`, `with a database a newer sites has upgraded`, `where its state directory cannot be created`, `where its cache directory cannot be created`). Beyond them sites checks nothing before it is ready: it does not look in `REPOS_DIR`, at any repository, at any published commit, or at any tree under `cache/sites/`, and it unpacks, removes, and rewrites no tree. A site's repository or commit is looked at only when a tool or a site request needs it, and a published commit whose tree is missing is rebuilt by the first request for it (`S16`); one that cannot be rebuilt is that request's `503` and `site.unavailable` (`S16`), never a start's refusal and never an event of the start. So a start records only its `service.started`, and writes nothing to stderr beyond what `S02` gives. sites never rebuilds its catalog: unlike repos' catalog (repos' `S14-verification.md`), a lost `state/sites.db` cannot be recovered from the disk, and comes back only from the host's replica (`S21`). The actor is the host, and the paths are relative to sites' working directory, `/var/opt/ikigenba/sites` on a host.

## The host starts sites over a sound catalog and cache

The common start: the catalog opens, `cache/sites/` is there, and every published site's tree is in it. sites is ready without having looked at any of the trees or repositories, and serves each site from its tree as it stands.

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- sites is installed and `ikigenba-sites.service` is not running, as in `S02`'s `The host starts sites`.
- `state/sites.db` holds `S06`'s shared catalog: `blog`, `handbook`, `scratch`, and `recipes`, and no apex.
- `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`, `cache/sites/sit_9a3c5e7b1d0f2468/a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d/`, and `cache/sites/sit_6b1d3f5a7c9e0284/e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92/` hold the unpacked trees of the three published sites.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- sites is serving, and `list` for `u_7f3a9c21` answers `blog`, `handbook`, and `scratch`, sorted by name, as before (`S07`).
- `GET /blog/` is answered `200` with the tree's `index.html` (`S11`), served from the tree already in `cache/`, with no git run for it.
- telemetry has received this start's `service.started` and nothing else from it: no `site.unavailable`.
- `state/sites.db` names what it named before, and every file under `cache/sites/` is as it was.
- sites has written nothing to the journal.

## The host starts sites over an emptied cache

`cache/` is disposable: an operator may empty it to reclaim disk, a host may be rebuilt without it, and nothing backs it up. sites starts over an empty `cache/sites/`, or with no `cache/` at all, exactly as over a full one, creating `cache/sites/` when it is absent. It does not unpack the published trees at start, which would make a start's length grow with every site the space holds; each comes back when its first request asks for it (`S16`).

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- sites is installed and `ikigenba-sites.service` is not running.
- `state/sites.db` holds `S06`'s shared catalog.
- `cache/` does not exist, or `cache/sites/` exists and is empty.
- `REPOS_DIR` holds `rep_8c21d4e0f7a3b915.git`, `rep_3f9a0c1d2e4b5a69.git`, and `rep_d41c7a9e05b28f63.git`, each with its published commit.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- sites is serving, and `cache/sites/` exists, empty: the start unpacked nothing and ran no git.
- The first `GET /blog/` after the start rebuilds `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` from `rep_8c21d4e0f7a3b915.git` and is answered `200` with its `index.html` (`S16`); `handbook` and `recipes` stay unpacked until a request asks for them.
- `show` of each site answers the same `commit` and `published` as before the start (`S07`): emptying the cache published nothing and unpublished nothing.
- telemetry has received this start's `service.started`, and no `site.unavailable` and no `site.published` from it.
- sites has written nothing to the journal.

## The host starts sites with a catalog restored from its replica and no cache

A host rebuilt from scratch gets `state/sites.db` back from its replica before sites starts (`S21`), but `cache/` was never backed up, so the catalog names published commits whose trees are nowhere on disk. That is the same as an emptied cache: sites starts, and each site is rebuilt from its repository on its first request, so the restore needs no step of sites' own. A catalog restored from before a release that carries a migration it has not had is brought up to date by the start, as any older database is (`S02`). Where repos has been restored too, every site comes back as it was published; where a repository or a commit has not come back, its site is answered `503` with the unavailable page on each request until it does, and stays published (`S16`).

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- sites is installed and `ikigenba-sites.service` is not running.
- `state/sites.db` was restored from its replica and holds `S06`'s shared catalog, with `blog` as the apex site.
- `cache/` does not exist.
- repos' repositories were restored too: `REPOS_DIR` holds `rep_8c21d4e0f7a3b915.git` and `rep_3f9a0c1d2e4b5a69.git`, each with its published commit; `rep_d41c7a9e05b28f63.git` has not come back.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- sites is serving; `cache/sites/` exists, created by this start, and empty.
- `apex` with no arguments answers `blog`'s site object (`S13`), and `list` for each caller answers what it answered before the host was lost (`S07`).
- `GET /blog/` and, for a signed-in user, `GET /handbook/` each rebuild their tree and are answered `200` (`S16`).
- `GET /recipes/` is answered `503` with the unavailable page and `Retry-After: 60`, and records `site.unavailable` with `site` `sit_6b1d3f5a7c9e0284`, `commit` `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`, and `reason` `repository_missing` (`S16`); `recipes` is still published, and is served again once its repository is back.
- telemetry has received this start's `service.started`, and nothing else from the start itself.
- sites has written nothing to the journal.

## The host starts sites before repos' repositories exist

sites may be installed before repos, or `REPOS_DIR` may name a directory that is not there yet. sites does not look in `REPOS_DIR` at start, so it starts all the same, and creates nothing there: the directory is repos', and sites never writes in it. Until a repository appears, `create` answers `no repository '<repo>'` for it (`S06`), and a published site whose tree must be rebuilt is unavailable (`S16`); neither needs a restart once repos is there, since the directory is looked at afresh each time.

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- sites is installed and `ikigenba-sites.service` is not running.
- `state/sites.db` exists and names no site, or does not exist and is created as on a first start (`S02`).
- `REPOS_DIR` is unset, so it is `../repos/state/repos`, and `/var/opt/ikigenba/repos/state/repos` does not exist: repos is not installed yet.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- sites is serving, and telemetry has received this start's `service.started`.
- `/var/opt/ikigenba/repos/state/repos` still does not exist; sites created nothing outside its own working directory.
- `create` by `u_7f3a9c21` of `blog` with `repo` `rep_8c21d4e0f7a3b915` is refused `no repository 'rep_8c21d4e0f7a3b915'` (`S06`). Once repos is installed and holds that repository, owned by `u_7f3a9c21`, the same call makes the site, with no restart of sites.
- sites has written nothing to the journal.

## The host starts sites with a published tree that cannot be rebuilt

A start does not check that a site can be served: a repository removed, a commit gone from its repository, or a tree grown past `SITE_MAX_BYTES` while sites was stopped is found by the first request that needs the tree, never by the start. So a start is as quick, and as quiet, with a broken site as without one, and the broken site never keeps sites from serving the rest. A tree already in `cache/` is served as it is even when its repository has gone, since sites does not look at the repository to serve a tree it has.

Command:

```
$ sudo systemctl start ikigenba-sites.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- sites is installed and `ikigenba-sites.service` is not running.
- `state/sites.db` holds `S06`'s shared catalog.
- `cache/sites/` holds `blog`'s tree at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and no tree of `handbook`.
- `rep_8c21d4e0f7a3b915.git` and `rep_3f9a0c1d2e4b5a69.git` have been removed from `REPOS_DIR`.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- sites is serving, and telemetry has received this start's `service.started` and no `site.unavailable` from the start.
- `GET /blog/` is answered `200` from the tree in `cache/`.
- `GET /handbook/` for a signed-in user is answered `503` with the unavailable page and records `site.unavailable` with `reason` `repository_missing` (`S16`).
- `state/sites.db` is as it was: both sites are still published at the same commits.
- sites has written nothing to the journal.
