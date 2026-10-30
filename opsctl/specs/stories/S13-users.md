# Stories — users

A user is a person who signs in to the host over SSH. A user is an account whose login name, NAME, is valid as a Linux account name and unique on the host: no other account or group has it, a business group's slug included, since slugs and user names share one namespace. A user has a home, `/home/NAME`, readable by them alone; the login shell `/bin/bash`; a private primary group named after them; membership in the pooled group `shell` and in zero or more business groups (`S12-groups.md`); the ed25519 keys they sign in with, held one per line in `/etc/ssh/authorized_keys/NAME`, owned by root and not writable by the user, the only keys that sign a user in; and a state, `active` or `locked`. A user has no other access: no sudo, no membership in the suite's `ikigenba` group or the web server's `nginx` group, and opsctl never touches what is in their home. A user with no key cannot sign in. The users of a host are exactly the members of `shell`; `ec2-user` and service accounts are never users.

The `accounts` app is the usual caller. It runs these commands as `sudo /usr/local/bin/opsctl user ...`, which the host's sudo rule allows (`S11-hardening.md`), and parses what they print; an operator may run the same commands by hand as root. The stories show them as `sudo opsctl user ...`, the same program.

The top-level usage gains the line `  user      manage SSH users, their keys and groups` under `Commands:`.

A command that changes the host prints nothing on success: the answer is the exit code. A command asked for a state the host is already in changes nothing and exits 0. The exit codes are those of the top-level help: `0  success`; `1  the operation failed`, with `opsctl: <tool>: exit status <n>` on stderr and, after one blank line, the tool's output with every line prefixed `> `; `2  usage error, or a preflight check failed`, here malformed input, with the three-line form ending `see 'opsctl user --help' for usage`; `3  refused: opsctl must run as root`; and `4  refused: the host's state does not allow it`, one line on stderr naming the case. Only `user add` undoes what it made when it fails. Every other command finishes the job when run again, because each exits 0 once the host is in the state asked for; a `delete` that failed partway, run again, completes, or answers `opsctl: no user 'bob'` with exit 4 once the account is gone. A business group is a group with a same-named service account whose home is `/srv/<slug>`; any other group named as GROUP, `shell` or `wheel` included, answers `opsctl: no group '<name>'` with exit 4. KEY is always one argument, the whole OpenSSH public key line `ssh-ed25519 BODY [COMMENT]`; a key is matched by its type and body, never by its comment.

The stories use the business groups `acme` and `beta`, made by `opsctl group add`, the users `alice` and `bob`, and three keys: `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop`, `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAZbrkOd+Y6S7A73l8INVj18asxLPNd5ESn8jJF92cOm alice@desktop` and `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOX98ItbYTqX546sPOUFYsJXWCgEsWBavHBIkCjPHj9P bob@work`.

## An operator asks what `user` can do

Command:

```
$ opsctl user --help
```

```
$ opsctl user -h
```

Output:

```
Usage: opsctl user <subcommand> [arguments]

Manage the people who sign in to this host over SSH: their keys, their
business groups, and whether they may sign in.

Subcommands:
  add NAME GROUP KEY   create NAME in shell and GROUP, signing in with KEY
  list                 print every user as one JSON document
  key add NAME KEY     let NAME sign in with KEY; succeeds if NAME has it
  key remove NAME KEY  take KEY from NAME; succeeds if NAME lacks it
  join NAME GROUP      add NAME to GROUP; succeeds if already a member
  leave NAME GROUP     take NAME out of GROUP; succeeds if not a member
  lock NAME            end NAME's sessions and refuse every sign-in
  unlock NAME          let a locked NAME sign in again
  delete NAME          remove a locked NAME and their home

NAME is a Linux account name, unique on the host. GROUP is a business group
made by 'opsctl group add'. KEY is one argument, a whole ed25519 OpenSSH
public key line, 'ssh-ed25519 BODY [COMMENT]'; keys match on type and BODY,
never COMMENT. A deleted user's files under /srv/SLUG go to the account SLUG.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## The accounts app adds a user

A person has been given access to the host for the business `acme`. They are created with the one key they gave, in `shell`, which is what lets them sign in, and in `acme`, which is what lets them work in `/srv/acme`.

Command:

```
$ sudo opsctl user add alice acme 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `acme` is a business group.
- No account or group named `alice` exists on the host.

Postconditions:

- The account `alice` exists with the login shell `/bin/bash` and the primary group `alice`, a group of her own. It is in `shell` and `acme` and in no other business group; it is not in `ikigenba` or `nginx` and has no sudo.
- `/home/alice` exists, owned by `alice` and readable by no one else. opsctl put nothing in it.
- `/etc/ssh/authorized_keys/alice` exists, owned by root and not writable by `alice`, holding the one key line given.
- `alice` can sign in over SSH with that key and by no other means.
- `opsctl user list` shows `alice` in `acme`, holding that one key, `active`, with `last_sign_in` `null`.

## The accounts app adds a user whose name is taken

A user name collides with any account or group on the host, not only with another user: a service account, a system account, `ec2-user`, `shell`, or a business group such as `acme`.

Command:

```
$ sudo opsctl user add acme acme 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

Output:

```
opsctl: name taken: acme
```

Exits 4. The line is on stderr; stdout is empty. Adding `alice` when `alice` is already a user says `opsctl: name taken: alice` the same way.

Preconditions:

- `opsctl` is running as root.
- `acme` is a business group, so the group `acme` and the account `acme` exist.

Postconditions:

- Nothing has changed.

## The accounts app adds a user to a group that does not exist

Command:

```
$ sudo opsctl user add alice acme 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

Output:

```
opsctl: no group 'acme'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `acme` is not a business group.
- No account or group named `alice` exists.

Postconditions:

- Nothing has changed. No account `alice` was created.

## The accounts app adds a user with a name that is not an account name

A NAME or GROUP the host could never hold as an account or group is the caller's mistake, not the host's, so it is a usage error.

Command:

```
$ sudo opsctl user add alice/admin acme 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

Output:

```
opsctl: invalid name: alice/admin

see 'opsctl user --help' for usage
```

Command:

```
$ sudo opsctl user join alice acme/corp
```

Output:

```
opsctl: invalid name: acme/corp

see 'opsctl user --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty. Every subcommand that takes NAME refuses an invalid one the same way, and `user add`, `user join` and `user leave` refuse a GROUP that is not valid as an account name the same way.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## The accounts app adds a user with a key that is not an ed25519 key

Only ed25519 keys are accepted, in OpenSSH's one-line form. An RSA key, a private key, a key in another format, or text that does not decode as a key is refused.

Command:

```
$ sudo opsctl user add alice acme "$(cat id_rsa.pub)"
```

Output:

```
opsctl: invalid key: not an ed25519 OpenSSH public key

see 'opsctl user --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `user key add` and `user key remove` refuse such a KEY the same way.

Preconditions:

- `opsctl` is running as root.
- `id_rsa.pub` holds an `ssh-rsa` public key line.

Postconditions:

- Nothing has changed.

## The accounts app adds a user and a tool on the host fails

Adding a user takes several steps on the host. When one of them fails, `user add` undoes what it had made before the failure, so the host is as it was and the same command can simply be run again.

Command:

```
$ sudo opsctl user add alice acme 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

Output:

```
opsctl: useradd: exit status 12

> useradd: cannot create directory /home/alice
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `acme` is a business group.
- The file system holding `/home` is full, so no directory can be made in it.
- No account or group named `alice` exists.

Postconditions:

- The host is as it was: there is no account `alice`, no group `alice`, no `/home/alice` and no `/etc/ssh/authorized_keys/alice`.
- Nothing the command did not make was touched.

## The accounts app lists the users

The list is read from the host alone, so it is what the host enforces, whatever the app believes. It is one JSON document: users sorted by name; `groups` the user's business groups sorted by name, never `shell`; `keys` each line of `/etc/ssh/authorized_keys/NAME`, comment included; `state` `active` or `locked`; `last_sign_in` the time of the newest public-key sign-in sshd accepted for the user, read from sshd's records in the host's journal and given in UTC as RFC 3339 to the second, or `null` when the journal holds no such record: the user has never signed in, or every sign-in is older than what the journal keeps. A sign-in that only runs a command, git over ssh or scp, counts.

Command:

```
$ sudo opsctl user list
```

Output:

```
{"users":[{"name":"alice","groups":["acme","beta"],"keys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop","ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAZbrkOd+Y6S7A73l8INVj18asxLPNd5ESn8jJF92cOm alice@desktop"],"state":"active","last_sign_in":"2026-10-29T14:03:00Z"},{"name":"bob","groups":["acme"],"keys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOX98ItbYTqX546sPOUFYsJXWCgEsWBavHBIkCjPHj9P bob@work"],"state":"locked","last_sign_in":null}]}
```

Exits 0. The document is on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.
- `alice` is active, in `acme` and `beta`, holds her two keys, and last signed in over SSH at 14:03 UTC on 29 October 2026.
- `bob` is locked, in `acme`, holds his one key, and has never signed in.
- `ec2-user` and the service accounts `acme` and `beta` exist and are not in `shell`.

Postconditions:

- Nothing has changed.

## The accounts app lists the users of a host that has none

Command:

```
$ sudo opsctl user list
```

Output:

```
{"users":[]}
```

Exits 0. The document is on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.
- No account is in `shell`. `ec2-user` and any service accounts exist.

Postconditions:

- Nothing has changed.

## The accounts app gives a user another key

A person who works from a second machine adds its key beside the first.

Command:

```
$ sudo opsctl user key add alice 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAZbrkOd+Y6S7A73l8INVj18asxLPNd5ESn8jJF92cOm alice@desktop'
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user holding only the `alice@laptop` key.

Postconditions:

- `/etc/ssh/authorized_keys/alice` holds both key lines and `alice` can sign in with either. `opsctl user list` shows both.
- Nothing else about `alice` has changed.

## The accounts app gives a user a key they already have

A key is the same key whatever its comment, so giving one whose body the user already holds, under another comment or none, changes nothing: the line already held is kept as it is.

Command:

```
$ sudo opsctl user key add alice 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

```
$ sudo opsctl user key add alice 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B laptop-2026'
```

Output:

```
```

Both exit 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user holding the `alice@laptop` key.

Postconditions:

- Nothing has changed. `/etc/ssh/authorized_keys/alice` holds the key once, as the line `... alice@laptop` it held before.

## The accounts app takes a key away from a user

A person has lost a machine. The key is named as the whole line, but only its type and body are matched, so a comment that differs from the one held, or none at all, still names it.

Command:

```
$ sudo opsctl user key remove alice 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAZbrkOd+Y6S7A73l8INVj18asxLPNd5ESn8jJF92cOm'
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` holds the `alice@laptop` and `alice@desktop` keys.

Postconditions:

- `/etc/ssh/authorized_keys/alice` holds only the `alice@laptop` line. The `alice@desktop` key no longer signs her in.
- Nothing else about `alice` has changed.

## The accounts app takes away a key the user does not have

Command:

```
$ sudo opsctl user key remove alice 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOX98ItbYTqX546sPOUFYsJXWCgEsWBavHBIkCjPHj9P bob@work'
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user who does not hold that key.

Postconditions:

- Nothing has changed.

## The accounts app takes away a user's last key

Removing the last key is allowed. The user stays a user, active and in their groups, but can no longer sign in until a key is added; telling the person is the app's business.

Command:

```
$ sudo opsctl user key remove alice 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` holds only that key.

Postconditions:

- `/etc/ssh/authorized_keys/alice` holds no key and `alice` cannot sign in. `opsctl user list` shows her with `"keys":[]` and `"state":"active"`.
- Her home, her groups and her state are unchanged.

## The accounts app adds a user to another group

Command:

```
$ sudo opsctl user join alice beta
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user in `acme`. `beta` is a business group.

Postconditions:

- `alice` is in `acme` and `beta`.
- Nothing else about `alice` has changed.

## The accounts app adds a user to a group they are already in

Command:

```
$ sudo opsctl user join alice acme
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user in `acme`.

Postconditions:

- Nothing has changed.

## The accounts app names a group that does not exist

Command:

```
$ sudo opsctl user join alice beta
```

Output:

```
opsctl: no group 'beta'
```

Exits 4. The line is on stderr; stdout is empty. `user leave alice beta` says the same, and so does naming a group that exists but is not a business group: `user join alice shell` answers `opsctl: no group 'shell'`, as does `user add` with `shell` or `wheel` as GROUP.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user. `beta` is not a business group.

Postconditions:

- Nothing has changed.

## The accounts app takes a user out of a group

Command:

```
$ sudo opsctl user leave alice beta
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user in `acme` and `beta`.

Postconditions:

- `alice` is in `acme` and not in `beta`. She stays in `shell`.
- Nothing else about `alice` has changed.

## The accounts app takes a user out of their last group

A user may be in no business group at all, and leaving the last one does not remove them: they stay in `shell`, keep their keys and home, and can still sign in.

Command:

```
$ sudo opsctl user leave alice acme
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user in `acme` and no other business group.

Postconditions:

- `alice` is in `shell` and in no business group. `opsctl user list` shows her with `"groups":[]`.
- Her keys, home and state are unchanged.

## The accounts app takes a user out of a group they are not in

Command:

```
$ sudo opsctl user leave alice beta
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is a user in `acme`. `beta` is a business group `alice` is not in.

Postconditions:

- Nothing has changed.

## The accounts app locks a user

A person must lose access now, but what they have is kept. Locking ends everything they are running and refuses every way of signing in, while their home, keys and groups stay as they were, so unlocking gives back exactly what they had.

Command:

```
$ sudo opsctl user lock bob
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `bob` is an active user in `acme`, signed in over SSH with a session open.

Postconditions:

- Every process running as `bob` has ended, his SSH session included.
- `bob` cannot sign in by any means: not with his keys, not with a password, not otherwise.
- His home, his keys and his groups are unchanged. `opsctl user list` shows him `locked`.

## The accounts app locks a user who is already locked

Command:

```
$ sudo opsctl user lock bob
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `bob` is a locked user.

Postconditions:

- Nothing has changed.

## The accounts app unlocks a user

Command:

```
$ sudo opsctl user unlock bob
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `bob` is a locked user holding his key, in `acme`.

Postconditions:

- `bob` can sign in over SSH with his key again. `opsctl user list` shows him `active`.
- His home, keys and groups are unchanged.

## The accounts app unlocks a user who is active

Command:

```
$ sudo opsctl user unlock alice
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `alice` is an active user.

Postconditions:

- Nothing has changed.

## The accounts app deletes a locked user

A user is deleted only once locked, so nothing of theirs is running. What they made in a business's folder belongs to the business, so it is kept and handed to that folder's service account, whether or not the user is still in that business's group; their account, private group, keys and home go.

Command:

```
$ sudo opsctl user delete bob
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `bob` is a locked user in `acme`; he left `beta` earlier.
- `/srv/acme/notes.md` and `/srv/beta/plan.txt` are owned by `bob`.

Postconditions:

- `/srv/acme/notes.md` is owned by the account `acme` and `/srv/beta/plan.txt` by the account `beta`; every other file anywhere under `/srv` that `bob` owned went the same way, to the service account of the folder it is in. Each file's group and contents are unchanged.
- The account `bob`, his group `bob`, `/etc/ssh/authorized_keys/bob` and `/home/bob` are gone. `bob` is in no group, and `opsctl user list` does not show him.
- The name `bob` is free for a later `user add`.

## The accounts app deletes a user who is not locked

Deleting an active user would remove an account that may be in use. The caller locks first.

Command:

```
$ sudo opsctl user delete alice
```

Output:

```
opsctl: user not locked: alice
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `alice` is an active user.

Postconditions:

- Nothing has changed.

## The accounts app names a user who does not exist

Every subcommand that takes NAME, but `add`, needs NAME to be a user. A name that is an account but not a member of `shell`, such as `ec2-user` or a service account, is not a user either.

Command:

```
$ sudo opsctl user lock carol
```

```
$ sudo opsctl user key add carol 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B alice@laptop'
```

```
$ sudo opsctl user join carol acme
```

Output:

```
opsctl: no user 'carol'
```

Exits 4. The line is on stderr; stdout is empty. `key remove`, `leave`, `unlock` and `delete` say the same.

Preconditions:

- `opsctl` is running as root.
- No user is named `carol`. `acme` is a business group.

Postconditions:

- Nothing has changed.

## An operator runs `user` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl user
```

Output:

```
opsctl: no user subcommand given

see 'opsctl user --help' for usage
```

Command:

```
$ sudo opsctl user rename alice alicia
```

Output:

```
opsctl: unknown user subcommand 'rename'

see 'opsctl user --help' for usage
```

Command:

```
$ sudo opsctl user key list alice
```

Output:

```
opsctl: unknown user key subcommand 'list'

see 'opsctl user --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty. `opsctl user key` with nothing after it says `opsctl: no user key subcommand given` the same way.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator gives a subcommand too few or too many arguments

Each subcommand takes exactly the arguments its help line names. Too few says what the subcommand needs; too many says what it takes.

Command:

```
$ sudo opsctl user add alice acme
```

Output:

```
opsctl: user add needs NAME GROUP KEY

see 'opsctl user --help' for usage
```

Command:

```
$ sudo opsctl user lock alice bob
```

Output:

```
opsctl: user lock takes one NAME

see 'opsctl user --help' for usage
```

Command:

```
$ sudo opsctl user list alice
```

Output:

```
opsctl: user list takes no arguments

see 'opsctl user --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty. With too few arguments the lines are `opsctl: user add needs NAME GROUP KEY`, `opsctl: user key add needs NAME KEY`, `opsctl: user key remove needs NAME KEY`, `opsctl: user join needs NAME GROUP`, `opsctl: user leave needs NAME GROUP`, `opsctl: user lock needs NAME`, `opsctl: user unlock needs NAME` and `opsctl: user delete needs NAME`. With too many they are `opsctl: user add takes NAME, GROUP and KEY`, `opsctl: user key add takes NAME and KEY`, `opsctl: user key remove takes NAME and KEY`, `opsctl: user join takes NAME and GROUP`, `opsctl: user leave takes NAME and GROUP`, `opsctl: user lock takes one NAME`, `opsctl: user unlock takes one NAME`, `opsctl: user delete takes one NAME` and `opsctl: user list takes no arguments`. A KEY left unquoted arrives as several arguments and is refused this way.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
