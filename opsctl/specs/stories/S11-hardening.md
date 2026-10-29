# Stories — hardening

`hardening` is the setup step that brings the host to the rules people who sign in to it live under. It is one idempotent step, run by `init` (`S04-init.md`) after `slices` and before `nginx.conf`, and by `activate` and `rollback` (`S10-releases.md`) right after `resources`: after every check, and before `snapshot` and `cutover` on a per-app host and before `label`. It brings the host to these rules, and nothing in them names a space:

- **sshd** takes public keys only: no password or keyboard-interactive authentication and no root login, a short grace period to sign in, a small number of authentication attempts per connection, and idle sessions dropped. Only the admin account `ec2-user` and members of the group `shell` may sign in. A member of `shell` signs in only with a key held in `/etc/ssh/authorized_keys/<name>`, owned by root and not writable by them (`S13-users.md`); a key in their own `~/.ssh/authorized_keys` is ignored. `ec2-user` keeps signing in with the keys in its own `~/.ssh/authorized_keys`. A member of `shell` gets no TCP or port forwarding, no agent or X11 forwarding, and no tunnels. The rules are a drop-in under `/etc/ssh/sshd_config.d/`, checked with `sshd -t` before sshd is reloaded; a configuration that fails the check is not installed, so the admin's access is kept.
- **The group `shell`** exists. `ec2-user` is not a member of it and may still sign in.
- **fail2ban**, with a jail for sshd, bans a source that fails to sign in again and again. The first ban lasts 10 minutes; each repeated ban lasts longer; every ban lifts on its own.
- **dnf-automatic** applies security updates, and only those, unattended, on its timer.
- **The packages** `fail2ban` and `dnf-automatic` are installed with dnf when either is missing; when both are present dnf is not run. `init`'s preflight does not check for them.
- **The resource pool**: `/etc/systemd/system/user.slice.d/ikigenba.conf` sets `MemoryHigh`, `MemoryMax`, `CPUWeight` and `TasksMax` for all login sessions together, `ec2-user`'s included, and `/etc/systemd/system/user-.slice.d/ikigenba.conf` sets `MemoryHigh`, `MemoryMax` and `TasksMax` for each person's own sessions, each below the pool's, leaving the CPU weight at systemd's default. The values are the same on every host, whatever its memory, and no configuration key changes them. When either file changed, the step reloads systemd itself. The suite's own slices and nginx's drop-in (`S04-init.md`) are not touched.
- **The suite's state is closed**: on each of `/etc/ikigenba`, `/etc/opt/ikigenba`, `/var/opt/ikigenba`, `/var/lib/ikigenba` and `/run/ikigenba` that exists, no path at or under it grants any permission to others. Every app still answers, nginx still serves, and Litestream still backs up. A path opsctl itself creates under one of them later in the same command, or in any later command, such as `init`'s `/run/ikigenba/services.json` or `activate`'s environment files and app directories under `/etc/opt/ikigenba` and `/var/opt/ikigenba`, is created granting nothing to others, so the rule holds once the whole command has run, even where a directory did not exist when the step ran. `/opt/ikigenba`, the release code, `/etc/opt` itself, and the system's own directories stay readable.
- **The sudo rule** `/etc/sudoers.d/ikigenba-accounts`, owned by root with mode `0440`, lets the user `ikigenba`, which every app runs as and which `init` creates if it is missing, run `/usr/local/bin/opsctl group ...` and `/usr/local/bin/opsctl user ...` (`S12-groups.md`, `S13-users.md`) as root without a password, and nothing else. It is checked with `visudo -c` before it is installed; a file that fails the check is not installed.

A run that finds the host already holding every rule writes nothing and reloads nothing. In `init` the step prints no line of its own, as no step of the sequence does. In `activate` and `rollback` it prints `hardening: ok (unchanged)` when it wrote nothing and `hardening: ok (updated)` when it changed anything. A step that fails stops the command where it stands: a failed install, or a check that `sshd -t` or `visudo -c` refuses, is reported as `<command>: exit status <n>`, with that program's output quoted `> ` after an empty line, and the exit is 1. In `init` the report is `opsctl: <command>: exit status <n>` on stderr, as for any step of the sequence, and the steps after it do not run. In `activate` it is `hardening: failed: <command>: exit status <n>` on stdout and `opsctl: activate failed` on stderr, followed by the quoted output, and in `rollback` the same with `opsctl: rollback failed`; nothing after it runs, so the release is not activated and `current` and `previous` are not moved. Either way, the parts of the step that completed before the failing part stay in place, and a later run finds them done; the failing part installs nothing. The stories use the example host `sbx.ikigenba.dev` and the releases of `S10-releases.md`; a person with an account on the host is one `opsctl user add` created (`S13-users.md`), such as `alice`, a member of `shell` and of the business group `acme`.

## An agent's first init hardens the host

A new space gets its host rules from its first `init`, before any release is activated and before any person has an account. The step prints nothing; the run's output is the ready host's.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host (`S04-init.md`), and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is the fresh host of "An agent initialises a fresh host before its first activate" (`S04-init.md`), and every preflight check passes.
- Neither `fail2ban` nor `dnf-automatic` is installed, and the host's package repositories can be reached.
- There is no group `shell`, no drop-in of opsctl's under `/etc/ssh/sshd_config.d/`, `/etc/systemd/system/user.slice.d/` or `/etc/systemd/system/user-.slice.d/`, and no `/etc/sudoers.d/ikigenba-accounts`.

Postconditions:

- `fail2ban` and `dnf-automatic` are installed. fail2ban is running with its sshd jail, and dnf-automatic's timer is enabled, set to apply security updates only.
- The group `shell` exists and has no members; `ec2-user` is not in it.
- sshd runs with opsctl's drop-in, which passed `sshd -t`, and was reloaded: it accepts public keys only, refuses root, gives a short grace period and a small number of attempts per connection, drops idle sessions, and lets in only `ec2-user` and members of `shell`, with no forwarding or tunnels for members of `shell`. `ec2-user` signs in with its key as before.
- `/etc/systemd/system/user.slice.d/ikigenba.conf` and `/etc/systemd/system/user-.slice.d/ikigenba.conf` are written and systemd was reloaded, so `ec2-user`'s next session runs inside the pool.
- Once `init` has finished, no path at or under any of the five state directories that exists grants any permission to others, `/run/ikigenba/services.json`, which `init` writes after the step, included. nginx still serves the host's names.
- `/etc/sudoers.d/ikigenba-accounts` is owned by root with mode `0440` and passed `visudo -c`. With the account `ikigenba` that `init` creates, it lets `ikigenba` run `/usr/local/bin/opsctl group ...` and `/usr/local/bin/opsctl user ...` as root without a password, and nothing else.
- The steps after `hardening` ran as on any fresh host.

## An agent activates the release that brings the rules to an existing space

A space set up before opsctl had the step gets the rules from the first release whose opsctl has it, through the ordinary `activate`; nobody runs `init` for it. The step changes the host, so its line reads `updated`.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
hardening: ok (updated)
label: ok (r142)
env: ok (8 apps)
units: ok (8 apps)
links: ok (current c604e32, previous 1a2b3c4)
systemd: ok (daemon-reload)
nginx: ok (8 apps)
services: ok (8 services)
litestream: ok (unchanged)
service: ok (auth c604e32 active)
service: ok (telemetry c604e32 active)
service: ok (dummy c604e32 active)
service: ok (events c604e32 active)
service: ok (mcp c604e32 active)
service: ok (repos c604e32 active)
service: ok (scripts c604e32 active)
service: ok (sites c604e32 active)
retention: ok (removed 1 release)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is the one of "An agent activates a release on a host that runs releases" (`S10-releases.md`): it runs `1a2b3c4`, whose opsctl has no `hardening` step, and `c604e32` is unpacked whole.
- The host holds none of the rules: no `fail2ban`, no `dnf-automatic`, no group `shell`, none of opsctl's drop-ins and no `/etc/sudoers.d/ikigenba-accounts`. Some files under `/var/opt/ikigenba` and `/etc/ikigenba` are readable by others.

Postconditions:

- The host holds every rule, as the first init leaves them: the packages installed, fail2ban banning, dnf-automatic's timer enabled, the group `shell`, sshd checked and reloaded with opsctl's drop-in, both slice drop-ins written and systemd reloaded for them, the five state directories closed to others, and the sudo rule installed. The environment files and app directories `activate` writes after the step under `/etc/opt/ikigenba` and `/var/opt/ikigenba` grant others nothing either, so once the command has finished no path under the five directories grants others any permission.
- `ec2-user`, signed in over ssh while the command ran, is still signed in and can sign in again.
- Every app answers on `c604e32`, and Litestream still replicates every database. Everything else is as "An agent activates a release on a host that runs releases" leaves it.

## An agent runs init again on a host that holds the rules

Running `init` on a host already hardened finds nothing to change. The step prints no line in either case, so the output is the ready host's.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is the ready released host (`S04-init.md`), and a run of the step has left every rule in place since.

Postconditions:

- dnf was not run. Not one file of the step's was rewritten, no mode, owner or ACL was changed, and neither sshd nor systemd was reloaded for the step. fail2ban's bans stand.

## An agent activates a release on a host that holds the rules

Every activate runs the step, and on a host already hardened it finds nothing to change and says so.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output: the lines of "An agent activates the release that brings the rules to an existing space", with the `hardening` line reading

```
hardening: ok (unchanged)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for "An agent activates the release that brings the rules to an existing space", except that `1a2b3c4`'s opsctl ran the step and the host holds every rule: nothing under the five state directories grants others any permission, and every file of the step's holds what the step writes.

Postconditions:

- dnf was not run, no file of the step's was rewritten, no mode, owner or ACL was changed, and neither sshd nor systemd was reloaded for the step.
- Everything else is as "An agent activates a release on a host that runs releases" (`S10-releases.md`) leaves it.

## An agent activates a release on a host whose rules have drifted

Someone with root loosened what the step holds since it last ran: they made an app's database readable by others and edited the per-person drop-in. Since every activate runs the step, the next one puts both back; its line reads `updated`. A file a release's app created since the last activate is closed the same way.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output: the lines of "An agent activates the release that brings the rules to an existing space", `hardening: ok (updated)` included.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host held every rule, and then `chmod o+r /var/opt/ikigenba/repos/state/repos.db` was run and a line in `/etc/systemd/system/user-.slice.d/ikigenba.conf` was changed by hand.
- Every other rule is in place.

Postconditions:

- `/var/opt/ikigenba/repos/state/repos.db` grants others no permission; `repos` still answers.
- `/etc/systemd/system/user-.slice.d/ikigenba.conf` holds what the step writes again, and systemd was reloaded.
- dnf was not run, sshd was not reloaded, and no other file of the step's was rewritten.

## An agent initialises a host where dnf cannot install the packages

The step installs the two packages it needs when they are missing, and an install that fails stops `init` at the step, with dnf's own words quoted. The steps before it have run; nothing after it runs.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
opsctl: dnf install: exit status 1

> Error: Failed to download metadata for repo 'amazonlinux': Cannot download repomd.xml: Cannot download repodata/repomd.xml: All mirrors were tried
exit 1
```

Exits 1. The `ok` lines and the `exit` line are on stdout; the `opsctl:` line and dnf's quoted output are on stderr. Whatever dnf printed is quoted, every line prefixed `> `.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for renewal.
- `fail2ban` is not installed, and the host's package repositories cannot be reached.

Postconditions:

- The `certificate` and `slices` steps ran as on any host.
- `fail2ban` is still not installed. Nothing from `nginx.conf` on ran: the nginx file, the services file, `/etc/litestream.yml`, the timers and every app are as they were.
- Running `init` again once the repositories can be reached runs to the end.

## An agent initialises a host whose sshd configuration fails its check

sshd reads opsctl's drop-in together with the rest of its configuration, so `sshd -t` may refuse the whole for a line opsctl did not write. A configuration sshd refuses is never put in place, so the admin keeps the access they had; the step stops `init` with sshd's own words quoted.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
opsctl: sshd -t: exit status 255

> /etc/ssh/sshd_config.d/50-local.conf: line 1: Bad configuration option: PermitRootLgin
> /etc/ssh/sshd_config.d/50-local.conf: terminating, 1 bad configuration options
exit 1
```

Exits 1. The `ok` lines and the `exit` line are on stdout; the `opsctl:` line and sshd's quoted output are on stderr.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for renewal.
- An operator left `/etc/ssh/sshd_config.d/50-local.conf` holding the line `PermitRootLgin no`.
- opsctl's sshd drop-in is not in place.

Postconditions:

- opsctl's sshd drop-in is not in place, sshd was not reloaded, and `ec2-user` signs in as before. Parts of the step that completed before the check stay in place.
- Nothing from `nginx.conf` on ran.
- Running `init` again stops at the same place with the same output. Once `50-local.conf` is fixed or removed, `init` runs to the end.

## An agent initialises a host whose sudo rule fails its check

The sudo rule is checked with `visudo -c` before it is installed, and a rule that fails the check is never put in place, so sudo keeps working for the admin. The step stops `init` with visudo's own words quoted.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, then

```
opsctl: visudo -c: exit status 1

> <each line visudo printed>
exit 1
```

Exits 1. The `ok` lines and the `exit` line are on stdout; the `opsctl:` line and visudo's quoted output are on stderr. Whatever visudo printed is quoted, every line prefixed `> `.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for renewal.
- `visudo -c` rejects the rule the step would install.

Postconditions:

- The rule was not installed: `/etc/sudoers.d/ikigenba-accounts` is as it was before the run, absent on a host that never had it, and `ec2-user`'s sudo works as before. Parts of the step that completed before the check stay in place.
- Nothing from `nginx.conf` on ran.

## An agent activates a release on a host where dnf cannot install the packages

In `activate` the step comes after every check and before the release's first write, so a failed install leaves the release unactivated and the host running the release it ran.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
hardening: failed: dnf install: exit status 1
opsctl: activate failed

> Error: Failed to download metadata for repo 'amazonlinux': Cannot download repomd.xml: Cannot download repodata/repomd.xml: All mirrors were tried
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and dnf's quoted output are on stderr.

Preconditions:

- As for "An agent activates the release that brings the rules to an existing space", except that the host's package repositories cannot be reached.

Postconditions:

- The release is not activated: `/opt/ikigenba/current` still names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` is not moved, no step after `hardening` ran, no label, environment file or unit was written, and every app keeps running `1a2b3c4`.
- The failed install installed nothing: `fail2ban` and `dnf-automatic` are still not installed.
- Activating `c604e32` again once the repositories can be reached runs the whole activate.

## An agent activates a release on a host whose sshd configuration fails its check

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
hardening: failed: sshd -t: exit status 255
opsctl: activate failed

> /etc/ssh/sshd_config.d/50-local.conf: line 1: Bad configuration option: PermitRootLgin
> /etc/ssh/sshd_config.d/50-local.conf: terminating, 1 bad configuration options
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and sshd's quoted output are on stderr.

Preconditions:

- As for "An agent activates the release that brings the rules to an existing space", except that an operator left `/etc/ssh/sshd_config.d/50-local.conf` holding the line `PermitRootLgin no`.

Postconditions:

- The release is not activated: `/opt/ikigenba/current` still names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` is not moved, no step after `hardening` ran, and every app keeps running `1a2b3c4`.
- opsctl's sshd drop-in is not in place, sshd was not reloaded, and `ec2-user` signs in as before. Parts of the step that completed before the check stay in place.

## An agent activates a release on a host whose sudo rule fails its check

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl activate c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18 r142
```

Output:

```
release: ok (c604e32, r142)
layout: ok (releases)
manifests: ok (8 apps)
secrets: ok (2 keys)
resources: ok (8 apps)
hardening: failed: visudo -c: exit status 1
opsctl: activate failed

> <each line visudo printed>
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and visudo's quoted output are on stderr.

Preconditions:

- As for "An agent activates the release that brings the rules to an existing space", except that `visudo -c` rejects the rule the step would install.

Postconditions:

- The release is not activated: `/opt/ikigenba/current` still names `releases/1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d` and `/opt/ikigenba/previous` is not moved, no step after `hardening` ran, and every app keeps running `1a2b3c4`.
- `/etc/sudoers.d/ikigenba-accounts` was not installed, so it is still absent, and `ec2-user`'s sudo works as before. Parts of the step that completed before the check stay in place.

## A shell user asks for a port forward and is refused

A person signs in to work in a shell, not to reach the host's own ports or to carry traffic through it. sshd lets them in and refuses every channel that would forward: a local or remote port forward, a dynamic (SOCKS) forward, the agent, X11, and a tunnel device. `ec2-user`, who is not in `shell`, is not limited this way.

Command:

```
$ ssh -N -L 8080:127.0.0.1:443 alice@sbx.ikigenba.dev
```

and, from another terminal on the same machine,

```
$ curl -sk https://127.0.0.1:8080/
```

Output: ssh prints, for each connection made to the forwarded port,

```
channel 2: open failed: administratively prohibited: open failed
```

with the channel number counting up. curl's connection is closed with no response.

Exits 255 once interrupted. ssh's lines are on its stderr and its stdout is empty; curl prints nothing. Nothing reaches the host's port 443 through the forward.

Preconditions:

- The host holds every rule. `alice` is a member of `shell`, and `/etc/ssh/authorized_keys/alice` holds the key the client offers.

Postconditions:

- Nothing has changed on the host. `alice` can sign in and use a shell as before.

## An outsider tries to sign in with a password

sshd offers public keys and nothing else, so a client that has no key the account accepts is turned away before any password is asked for, whoever the account is.

Command:

```
$ ssh -o PubkeyAuthentication=no alice@sbx.ikigenba.dev
```

Output:

```
alice@sbx.ikigenba.dev: Permission denied (publickey).
```

Exits 255. The line is on stderr; stdout is empty.

Preconditions:

- The host holds every rule. `alice` is a member of `shell`; whether her account has a password does not matter.

Postconditions:

- Nothing has changed. The failure counts toward fail2ban's ban of the client's address.

## An outsider tries to sign in as an account outside the shell group

Only `ec2-user` and members of `shell` may sign in. Every other account is refused even with a key it accepts: `root`, the suite's account `ikigenba`, and the service account a business group owns (`S12-groups.md`).

Command:

```
$ ssh root@sbx.ikigenba.dev
```

```
$ ssh acme@sbx.ikigenba.dev
```

Output:

```
root@sbx.ikigenba.dev: Permission denied (publickey).
```

with the account's own name for each form.

Exits 255. The line is on stderr; stdout is empty.

Preconditions:

- The host holds every rule, and the business group `acme` exists.
- The client's key is in the account's authorized keys.

Postconditions:

- Nothing has changed. The failure counts toward fail2ban's ban of the client's address.

## A shell user tries to sign in with a key from their own home

A person's keys are the ones `opsctl user key add` put in `/etc/ssh/authorized_keys/<name>`, which they cannot change. A key they add to `~/.ssh/authorized_keys` themselves does not let them in.

Command:

```
$ ssh -i ~/.ssh/other alice@sbx.ikigenba.dev
```

Output:

```
alice@sbx.ikigenba.dev: Permission denied (publickey).
```

Exits 255. The line is on stderr; stdout is empty.

Preconditions:

- The host holds every rule. `alice` is a member of `shell`, and `/etc/ssh/authorized_keys/alice` holds her key from `~/.ssh/id_ed25519` but not `~/.ssh/other.pub`.
- Signed in with her held key, `alice` appended `~/.ssh/other.pub` to `/home/alice/.ssh/authorized_keys`.

Postconditions:

- Nothing has changed. Signing in with `~/.ssh/id_ed25519` still works.
- `ec2-user` is not affected: a key added to its own `~/.ssh/authorized_keys` lets it sign in.

## An outsider fails to sign in again and again from one address

fail2ban watches sshd's failures and bans an address that keeps failing: from then on the host does not let it reach sshd at all, whichever account it names, `ec2-user` included. The ban lifts on its own; the first lasts 10 minutes, and an address banned again is banned for longer each time.

Command: the same failing sign-in, repeated from one address, then one more attempt with a key the account accepts:

```
$ ssh -o PubkeyAuthentication=no alice@sbx.ikigenba.dev
$ ssh alice@sbx.ikigenba.dev
```

Output: each failure reads as in "An outsider tries to sign in with a password". Once the address is banned, the next attempt, even with `alice`'s key, gets no answer from sshd: ssh reports that the connection was refused or timed out.

Exits 255 for every attempt. The lines are on stderr; stdout is empty.

Preconditions:

- The host holds every rule, and `/etc/ssh/authorized_keys/alice` holds the key the client offers.
- The address has not been banned before.

Postconditions:

- The address is banned. Ten minutes after the ban began, `ssh alice@sbx.ikigenba.dev` from it signs in again.
- Another address is not affected: `alice` signs in from it throughout.
- If the address fails again and again after the ban lifts, it is banned again, for longer than 10 minutes, and that ban too lifts on its own.

## A shell user tries to read the suite's state

The suite's configuration, secrets, databases and runtime files are closed to everyone who is not the suite: a person signed in with a shell cannot list, read or write anything under the five state directories. The release code under `/opt/ikigenba` stays readable, as do the system's own directories.

Command:

```
$ cat /etc/ikigenba/config.json; echo "exit $?"
```

Output:

```
cat: /etc/ikigenba/config.json: Permission denied
exit 1
```

Exits 1. The `cat:` line is on stderr; the `exit` line is on stdout. Listing or reading anything at or under `/etc/opt/ikigenba`, `/var/opt/ikigenba`, `/var/lib/ikigenba` or `/run/ikigenba` is refused with `Permission denied` the same way.

Preconditions:

- The host holds every rule, and `alice` is signed in with a shell. She is a member of `shell` and `acme` only.

Postconditions:

- Nothing has changed.
- `ls /opt/ikigenba/current/` run by `alice` lists the release's folders.

## An app runs opsctl through the sudo rule

Every app runs as `ikigenba`, and the sudo rule lets that account run opsctl's `group` and `user` commands as root without a password, the way the accounts app does. Any other command, opsctl's or not, needs a password `ikigenba` does not have.

Command:

```
$ sudo -u ikigenba sudo -n /usr/local/bin/opsctl group list
```

```
$ sudo -u ikigenba sudo -n /usr/local/bin/opsctl status
```

Output, for the first:

```
{"groups":[]}
```

The second is refused by sudo, which says a password is required, and opsctl does not run.

Exits 0 for the first, with the text on stdout and stderr empty. sudo exits 1 for the second, with its line on stderr and stdout empty.

Preconditions:

- The host holds every rule, and it has no business group.

Postconditions:

- Nothing has changed.
