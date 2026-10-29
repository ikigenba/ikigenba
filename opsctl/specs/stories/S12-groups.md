# Stories — groups

A business group is one business on the host, named by a SLUG valid as a Linux account name. The SLUG names three things: a Linux group, a service account of the same name whose home is the folder, and the folder `/srv/<slug>` itself. The folder is owned by the service account and the group, is setgid, is writable by the group's members and unreadable by anyone else, and carries a default ACL that makes every file anyone creates in it group-writable whatever its creator or umask. It is a git repository in shared (group) mode, so members commit to it as themselves; nothing in it refers to ikigenba or the host. A member is a user, as `S13-users.md` describes one. Slugs and user names share one namespace with every other account and group on the host. The `accounts` app runs these commands through sudo, as `sudo /usr/local/bin/opsctl group ...`, which the sudo rule `hardening` lays down (`S11-hardening.md`) allows its account `ikigenba` without a password, and parses what they print; an operator may run the same commands by hand as root. The changing subcommands print nothing on success. `list` and `members` print one JSON document on stdout, read from the host alone: a business group is a group with a same-named service account whose home is `/srv/<slug>`, so `shell`, `ikigenba`, `nginx` and every other group never appear. Named to `members` or `remove`, such a group, `shell` or `wheel` for instance, is no group: the answer is `opsctl: no group 'shell'` and exit 4, as for a name the host does not have at all.

The top-level usage gains the line `  group     manage business groups and their folders` under `Commands:`.

The command answers with the exit codes of the top-level usage: `0  success`, `1  the operation failed`, `2  usage error, or a preflight check failed`, `3  refused: opsctl must run as root` (`S01-bootstrap.md`), and `4  refused: the host's state does not allow it`. A refusal (4) is one line on stderr naming the case, with no usage hint. Malformed input (2) is the usual form on stderr: `opsctl: <message>`, an empty line, then `see 'opsctl group --help' for usage`. A host operation that fails (1) puts `opsctl: <tool>: exit status <n>` on stderr, an empty line, then the tool's output with every line prefixed `> `; stdout is empty. Only `group add` undoes what it made before such a failure, and only the group and service account; no folder is ever deleted. The other subcommands make no undo promise: each exits 0 once the host is in the state it asks for, so running it again finishes the job.

## An operator asks what `group` can do

Command:

```
$ opsctl group --help
```

```
$ opsctl group -h
```

Output:

```
Usage: opsctl group <subcommand> [arguments]

Manage the host's business groups. A group SLUG is a Linux group, a service
account of the same name, and the folder /srv/SLUG, a shared git repository
the group's members can write and no one else can read.

Subcommands:
  add SLUG      create the group, its account and /srv/SLUG, adopting a folder
                already there
  remove SLUG   remove the group and its account; refused while it has members;
                /srv/SLUG is kept
  list          print every business group and its member count as JSON
  members SLUG  print the group's members as JSON

SLUG must be valid as a Linux account name, and add refuses one any account or
group on the host already has.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## The accounts app adds a group on a clean host

The owner of a new business asks the `accounts` app for a group, and the app runs the command through sudo. An operator gets the same result by hand.

Command:

```
$ sudo /usr/local/bin/opsctl group add acme
```

```
$ sudo opsctl group add acme
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- No account and no group named `acme` exists on the host, and `/srv/acme` does not exist.

Postconditions:

- A group `acme` exists, with no members.
- A service account `acme` exists, with home `/srv/acme`.
- `/srv/acme` exists, owned by account `acme` and group `acme`, setgid, writable by members of `acme`, and unreadable by others.
- A file any member creates anywhere under `/srv/acme` is group-writable by `acme`, whatever that member's umask.
- `/srv/acme` is an empty git repository in shared (group) mode; a member can commit to it as themselves. Nothing in it names ikigenba or the host.
- `opsctl group list` includes `{"slug":"acme","members":0}`.

## The accounts app adds a group whose folder an earlier remove left behind

`group remove` never deletes a folder, so a business that comes back finds its work where it left it. `add` adopts the folder rather than refusing it.

Command:

```
$ sudo opsctl group add acme
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- No account and no group named `acme` exists on the host.
- `/srv/acme` exists, left by an earlier `opsctl group remove acme`, and holds that group's files and its git repository.

Postconditions:

- The group `acme` and the service account `acme` exist as for a new group.
- `/srv/acme` has the owner, group, mode, setgid and default ACL of a new group's folder.
- Everything inside `/srv/acme` is as it was found. Git was not initialised again, because the folder was already a repository; a folder that was not one is made an empty shared repository, as for a new group.

## The accounts app adds a group whose name is taken

A slug may not be any name the host already uses for an account or a group: an existing business group, a user (`alice`), a system account or group. Adding it is refused rather than reused.

Command:

```
$ sudo opsctl group add acme
```

Output:

```
opsctl: name taken: acme
```

Exits 4. The line is on stderr; stdout is empty. `sudo opsctl group add alice`, with `alice` a user, gives `opsctl: name taken: alice` and exits 4 the same way.

Preconditions:

- `opsctl` is running as root.
- An account or a group named `acme` already exists on the host; for example the business group `acme` itself.

Postconditions:

- Nothing has changed. The existing account or group, and `/srv/acme`, are as they were.

## The accounts app adds a group whose folder path is not a directory

A folder left behind is adopted, but only a directory can be one. When `/srv/<slug>` exists and is anything else, a plain file for instance, adding the group is refused before anything changes.

Command:

```
$ sudo opsctl group add acme
```

Output:

```
opsctl: not a directory: /srv/acme
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- No account or group named `acme` exists on the host.
- `/srv/acme` exists and is a plain file.

Postconditions:

- Nothing has changed. No group or account `acme` was created, and `/srv/acme` is as it was.

## The accounts app adds a group with a slug that is not a valid name

Command:

```
$ sudo opsctl group add acme/corp
```

Output:

```
opsctl: invalid name: acme/corp

see 'opsctl group --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. Any subcommand that takes a SLUG answers an invalid one the same way.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed. No account, group or folder was created.

## The accounts app adds a group and a host tool fails

When a tool `add` runs on the host fails, the command removes the group and the service account it made before the failure and quotes the tool's output. No folder is ever deleted.

Command:

```
$ sudo opsctl group add acme
```

Output:

```
opsctl: setfacl: exit status 1

> setfacl: /srv/acme: Operation not supported
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- No account and no group named `acme` exists on the host.
- Setting the default ACL on `/srv/acme` fails.

Postconditions:

- No group `acme` and no account `acme` exist; the undo touched only what this run made.
- `/srv/acme` stays, whether this run created it or adopted it, and nothing in it was removed. Running `sudo opsctl group add acme` again adopts it.

## The accounts app lists the groups

One JSON document, the groups sorted by slug, each with the number of users in it.

Command:

```
$ sudo opsctl group list
```

Output:

```
{"groups":[{"slug":"acme","members":2},{"slug":"globex","members":0}]}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.
- The business groups `acme` and `globex` exist. `alice` and `bob` are in `acme`; `globex` has no members.
- The host also has the groups `shell`, `ikigenba` and `nginx`, and the account `ec2-user`.

Postconditions:

- Nothing has changed. No group but the two business groups appears.

## The accounts app lists the groups on a host that has none

Command:

```
$ sudo opsctl group list
```

Output:

```
{"groups":[]}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.
- No business group exists on the host.

Postconditions:

- Nothing has changed.

## The accounts app lists a group's members

The members are the group's users, sorted by name. A group with none gives `"members":[]`.

Command:

```
$ sudo opsctl group members acme
```

Output:

```
{"group":"acme","members":["alice","bob"]}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.
- The business group `acme` exists, and `alice` and `bob` are in it.

Postconditions:

- Nothing has changed.

## The accounts app lists the members of a group that does not exist

Command:

```
$ sudo opsctl group members acme
```

Output:

```
opsctl: no group 'acme'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- No business group `acme` exists on the host. A group that is not a business group, such as `shell`, answers the same way: `opsctl: no group 'shell'`, exit 4.

Postconditions:

- Nothing has changed.

## The accounts app removes an empty group

A business that has ended gives up its group and account; its work stays on disk for an operator to keep, archive or delete by hand.

Command:

```
$ sudo opsctl group remove acme
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- The business group `acme` exists and has no members.

Postconditions:

- The group `acme` and the service account `acme` no longer exist.
- `/srv/acme` is left in place, its contents untouched.
- `opsctl group list` no longer includes `acme`.

## The accounts app removes a group that still has members

A group is emptied first, one `opsctl user leave` per member.

Command:

```
$ sudo opsctl group remove acme
```

Output:

```
opsctl: group not empty: acme
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- The business group `acme` exists, and `alice` is in it.

Postconditions:

- Nothing has changed. The group, its account, its members and `/srv/acme` are as they were.

## The accounts app removes a group that does not exist

Command:

```
$ sudo opsctl group remove acme
```

Output:

```
opsctl: no group 'acme'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- No business group `acme` exists on the host. `sudo opsctl group remove wheel` answers `opsctl: no group 'wheel'` and exits 4 the same way, and `wheel` is untouched.

Postconditions:

- Nothing has changed.

## An operator runs `group` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl group
```

Output:

```
opsctl: no group subcommand given

see 'opsctl group --help' for usage
```

Command:

```
$ sudo opsctl group delete acme
```

Output:

```
opsctl: unknown group subcommand 'delete'

see 'opsctl group --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator gives a group subcommand too few or too many arguments

Each subcommand takes exactly the arguments its help line names. Too few says what the subcommand needs; too many says what it takes.

Command:

```
$ sudo opsctl group add
```

Output:

```
opsctl: group add needs SLUG

see 'opsctl group --help' for usage
```

Command:

```
$ sudo opsctl group add acme globex
```

Output:

```
opsctl: group add takes one SLUG

see 'opsctl group --help' for usage
```

Command:

```
$ sudo opsctl group list acme
```

Output:

```
opsctl: group list takes no arguments

see 'opsctl group --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty. The other subcommands answer the same way, each first line followed by the same empty line and usage hint:

- `sudo opsctl group remove` gives `opsctl: group remove needs SLUG`.
- `sudo opsctl group remove acme globex` gives `opsctl: group remove takes one SLUG`.
- `sudo opsctl group members` gives `opsctl: group members needs SLUG`.
- `sudo opsctl group members acme globex` gives `opsctl: group members takes one SLUG`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
