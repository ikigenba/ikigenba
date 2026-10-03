# Stories — disk

The layout on disk a consumer relies on. A consumer is a sibling on the same host as repos — prompts, scripts, sites, any app that runs content kept in git — running as the same `ikigenba` user, which reads a repo straight from its directory with local git: no HTTP, no credential, no socket, and no load on the repos process. repos keeps one bare repository per repo at `state/repos/<id>.git` under its working directory, so on a host `/opt/repos/state/repos/<id>.git`, where `<id>` is the repo's id, `rep_` followed by 16 lowercase hexadecimal digits (`S06-create.md`). The directory is named by the id and never by the name, so a consumer that has stored a repo's id finds its directory without asking repos, and keeps finding it across a rename (`S08-rename.md`); a consumer refers to a repo by its id for this reason, and to a revision by a full 40-hex commit sha, a pin, never by a ref, since a ref moves when the owner pushes and a pin does not. `HEAD` in every repo names `refs/heads/main`. Each repo's own git config, its `config` file, holds `ikigenba.id`, `ikigenba.name`, `ikigenba.owner`, and `ikigenba.created` (`S06-create.md`), so the directory says what it is without the catalog. Consumers only read: nothing but repos writes under `state/repos/`, and a consumer that ran a command writing to a repo's directory (a commit, a ref update, a `gc`) would be outside everything these stories promise. A read that runs while repos accepts a push sees the repo either before the push or after it, as git guarantees for any reader of a bare repository. The stories share one host: repos `v<semver>` is installed and active at `/opt/repos`, and the user `u_7f3a9c21` owns `notes`, `rep_3f9a0c1d2e4b5a69`, and `site`, `rep_8c21d4e0f7a3b915`. Each command below is run on the host as `ikigenba`, the user every app runs as; how a consumer comes to hold an id and a sha is the consumer's own design.

## A sibling resolves a pinned commit from disk

A consumer holds the pin `rep_3f9a0c1d2e4b5a69` at `<sha>` and asks git whether that commit is in the repo's directory before it runs anything from it. A commit that is there resolves to itself.

Command:

```
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git rev-parse --verify '<sha>^{commit}'
```

Output:

```
<sha>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `<sha>` is the full 40-hex sha of a commit the owner pushed to `notes` (`S11-git.md`), on any branch or none still reachable from a ref.
- The command runs as the `ikigenba` user on the host repos runs on.

Postconditions:

- Nothing has changed. repos received no request and recorded no event.

## A sibling reads the files of a pinned commit

Having resolved its pin, the consumer takes the commit's tree out of the repo's directory with git's own archive, and unpacks or reads it as it likes. It gets exactly the files of that commit, whatever `main` points at now.

Command:

```
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git archive --format=tar <sha> | tar -tf -
```

Output:

```
README.md
prompt.md
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `<sha>` is a commit in `notes` whose tree holds exactly the two files `README.md` and `prompt.md`.
- Commits pushed to `notes` after `<sha>` added or changed other files; `refs/heads/main` no longer points at `<sha>`.
- The command runs as the `ikigenba` user.

Postconditions:

- Nothing has changed. The repo's directory is as it was; repos received no request and recorded no event.

## A sibling reads a repo's current head from disk

A consumer promoting its pin to whatever the owner last pushed asks the repo's directory where `main` is. It gets the same sha `show` reports as `head` (`S07-list-and-show.md`).

Command:

```
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git symbolic-ref HEAD
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git rev-parse --verify refs/heads/main
```

Output:

```
refs/heads/main
<head>
```

Each command exits 0. The lines are on stdout; stderr is empty. `<head>` is the 40-hex sha `show` gives as `head` for `notes` at the same moment.

Preconditions:

- `notes` has at least one commit on `main`.
- The command runs as the `ikigenba` user.

Postconditions:

- Nothing has changed.

## A sibling asks for a commit the repo does not hold

A pin naming a sha that is not in the repo — mistyped, or taken from another repo — is git's to refuse, and the consumer learns it from git's exit status before it reads anything.

Command:

```
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git rev-parse --verify '<other-sha>^{commit}'
```

Output:

```
fatal: Needed a single revision
```

Exits 128. The line is on stderr, and it is git's; stdout is empty.

Preconditions:

- `<other-sha>` is 40 hexadecimal digits naming no object in `notes`.
- The command runs as the `ikigenba` user.

Postconditions:

- Nothing has changed.

## A sibling reads what a directory is from its own config

The directory carries its identity in the repo's own git config, written when repos created it (`S06-create.md`): the id, the current name, the owner's `X-User-Id`, and the creation time in RFC 3339 UTC seconds. A consumer, or an operator looking at the disk, can tell what a directory holds without the catalog, and it is from these that repos rebuilds a lost catalog (`S14-verification.md`).

Command:

```
$ git config --file /opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git/config --get-regexp '^ikigenba\.'
```

Output:

```
ikigenba.id rep_3f9a0c1d2e4b5a69
ikigenba.name notes
ikigenba.owner u_7f3a9c21
ikigenba.created <created>
```

Exits 0. The lines are on stdout; stderr is empty. `<created>` is the time `show` gives as `created` for `notes`, `2026-09-30T10:15:00Z` in the shared fixture.

Preconditions:

- `notes` was created by `u_7f3a9c21` through `create` (`S06-create.md`).
- The command runs as the `ikigenba` user.

Postconditions:

- Nothing has changed.

## A sibling keeps reading a repo after its owner renames it

The owner renames `notes` to `journal` (`S08-rename.md`). Nothing moves on disk: the directory is named by the id, so a consumer's pin keeps resolving at the same path, and only the name recorded in the directory's config follows the rename.

Command:

```
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git rev-parse --verify '<sha>^{commit}'
$ git config --file /opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git/config ikigenba.name
```

Output:

```
<sha>
journal
```

Each command exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A consumer pinned `rep_3f9a0c1d2e4b5a69` at `<sha>` while the repo was named `notes`.
- The owner then called `rename` with `{"repo":"notes","name":"journal"}`, and it succeeded (`S08-rename.md`).
- The command runs as the `ikigenba` user.

Postconditions:

- Nothing has changed by these commands.
- `/opt/repos/state/repos/` holds no directory named after `notes` or `journal`; the repo's directory is still `rep_3f9a0c1d2e4b5a69.git`, and every object and ref in it is as it was before the rename.

## A sibling finds a deleted repo gone

The owner deletes `notes` (`S09-delete.md`). repos removes its directory, so a consumer still pinned to it finds no repository at the path and learns it from git's exit status; nothing of the repo is left to read.

Command:

```
$ ls /opt/repos/state/repos/
$ git --git-dir=/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git rev-parse --verify '<sha>^{commit}'
```

Output:

```
rep_8c21d4e0f7a3b915.git
fatal: not a git repository: '/opt/repos/state/repos/rep_3f9a0c1d2e4b5a69.git'
```

`ls` exits 0 with its line on stdout. `git` exits 128; its line is on stderr, and it is git's; its stdout is empty.

Preconditions:

- The owner called `delete` with `{"repo":"notes"}`, and it succeeded (`S09-delete.md`).
- `site`, `rep_8c21d4e0f7a3b915`, is the only other repo on the host.
- The commands run as the `ikigenba` user.

Postconditions:

- Nothing has changed by these commands. A consumer that needs the content after a delete has no copy to read in repos; keeping one is the consumer's own design.
