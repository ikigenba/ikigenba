# Stories — apex

The root domain, `ikigenba.dev`, is not a space. It is one `A` record in the
zone that points at the Elastic IP of one space, and on that space one app
answers at it. `apex` is the command that makes, reads, and unmakes that
choice from the developer's machine. The record is `A` only: `*.ikigenba.dev`
is never written, so no name under the root resolves unless a space owns it.

The holder of the apex is the space whose Elastic IP the root's `A` record
points at; `space list` marks it, `space destroy` on it deletes the record
first, and this group reads it the same way. Which app answers there reaches
the host as one opsctl configuration key, `host.apex`, holding the app's
name; the host derives the apex hostname as the parent of its own `host.name`
and never stores it (see opsctl's `S5-nginx.md`). The apex app is chosen
independently of the manifest's `default`: one app may answer at the space's
name and another at the root, or the same app at both.

Answering at the root takes three things on the host, and `apex set` does
them in an order that never lets the root resolve to a host that would refuse
the handshake. The host's role must be allowed to write the `TXT` record at
`_acme-challenge.ikigenba.dev`, a name outside its own subtree that only the
holder may prove. The host's certificate must carry the root as a third name,
which `opsctl cert obtain` does when `host.apex` is set, reissuing whether or
not renewal is due. And nginx must route the root to the app, which `opsctl
nginx apply` does from the same key. Only then is the record written. When
another space held the apex before, it is cleared there last, in the reverse
order, so its certificate stops carrying a name its narrowed role can no
longer renew.

The operand of `set` is the one place devctl takes an app's hostname:
`<app>.<space>`, with `<space>` the label or the full domain as everywhere.
The named app need not be deployed; until it is, the host answers 404 at the
root under its certificate.

## A developer asks what `apex` can do

The top-level usage gains the line `  apex      point the root domain at one
app on one space` under `Commands:`.

Command:

```
$ devctl apex --help
```

Output:

```
Usage: devctl apex <subcommand> [arguments]

Point the root domain at one app on one space, say where it points, or take
it away. The root is an A record at the space's address; the space's host
carries the root in its certificate and routes it to the app.

Subcommands:
  set <app>.<space>   make <app> on <space> answer at the root
  show                print the app and address the root points at
  clear               remove the root's record and the holder's apex configuration

Run 'devctl apex <subcommand> --help' for details.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer points the root at an app

A developer wants `https://ikigenba.dev` to reach `crm` on `sbx1`. Each line
of output is one step; the last line is the root and the app's hostname.

Before any step, the target space must exist and be running, and so must the
space that holds the apex now, if there is one and it is another: both hosts
are about to be changed, and a command that could do half of it would leave
the root pointing at one host while the other still claims it. The `space`
step reports the target. `role` regenerates the target's inline policy to
allow the apex's challenge record. `host` runs three opsctl commands over ssh
in order: `config set host.apex=crm`, `cert obtain`, and `nginx apply`; the
certificate is reissued with the root as a third name before nginx routes it.
`record` writes the root's `A` record to the target's Elastic IP and waits
for `INSYNC`. `previous` clears the old holder: its inline policy is
regenerated without the challenge record, then over ssh `config del
host.apex`, `cert obtain`, which reissues with two names, and `nginx apply`.

Every step runs every time, whether or not the target already holds the
apex: re-putting the same policy, re-obtaining a certificate whose names
already match, and upserting a record to its own value all change nothing,
and the output has one shape.

Command:

```
$ devctl apex set crm.sbx1
```

```
$ devctl apex set crm.sbx1.ikigenba.dev
```

Output:

```
space: ok (sbx1.ikigenba.dev running, 18.118.7.42)
role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)
host: ok (host.apex=crm, certificate obtained, nginx applied)
record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)
previous: ok (sbx2.ikigenba.dev cleared)
ikigenba.dev crm.sbx1.ikigenba.dev
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev`, and a live SSO session for the profile `ikigenba.dev`.
- The space `sbx1.ikigenba.dev` exists, its instance is `running` at
  `18.118.7.42`, and `opsctl` is installed on it.
- The zone's `A` record `ikigenba.dev` points at `18.220.10.5`, the Elastic
  IP of `sbx2.ikigenba.dev`, whose instance is `running`, whose `host.apex`
  is set, and whose certificate carries the root.
- The developer's ssh configuration can reach both instances as `ec2-user`.
- The host of `sbx1` can reach the CA.

Postconditions:

- The inline policy `space` on the role `sbx1.ikigenba.dev` allows `TXT`
  writes at `sbx1.ikigenba.dev`, `*.sbx1.ikigenba.dev`, and
  `_acme-challenge.ikigenba.dev` in the zone, and nothing else it did not
  allow before.
- On `sbx1`'s host, `host.apex` is `crm`, the certificate at
  `/etc/letsencrypt/live/sbx1.ikigenba.dev/` covers `sbx1.ikigenba.dev`,
  `*.sbx1.ikigenba.dev`, and `ikigenba.dev`, and nginx serves the root from
  `crm`'s block, or from the 404 block if `crm` is not installed.
- The zone's `A` record `ikigenba.dev`, TTL 60, holds `18.118.7.42` and the
  change is `INSYNC`. `space list` marks `sbx1.ikigenba.dev` with `apex`.
- The inline policy on `sbx2.ikigenba.dev`'s role no longer allows
  `_acme-challenge.ikigenba.dev`; on its host `host.apex` is not set, the
  certificate covers `sbx2.ikigenba.dev` and `*.sbx2.ikigenba.dev` only, and
  nginx no longer names the root.
- No other record, role, or host has changed. Nothing about `crm`'s
  deployment changed: `set` does not deploy, restart, or stop anything.

## A developer points the root at an app when nothing held it

Command:

```
$ devctl apex set crm.sbx1
```

Output:

```
space: ok (sbx1.ikigenba.dev running, 18.118.7.42)
role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)
host: ok (host.apex=crm, certificate obtained, nginx applied)
record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)
previous: ok (none)
ikigenba.dev crm.sbx1.ikigenba.dev
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- As for the previous story, and the zone holds no `A` record
  `ikigenba.dev`.

Postconditions:

- As for the previous story, with no other space touched. The same output,
  with `previous: ok (none)`, is what moving the apex from one app to
  another on the same space prints, and what pointing it at the app that
  already holds it prints.

## A developer points the root at a space that is stopped, or does not exist

Command:

```
$ devctl apex set crm.sbx2
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Command:

```
$ devctl apex set crm.gone
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The instance tagged `Space=sbx2.ikigenba.dev` is `stopped`; no instance
  is tagged `Space=gone.ikigenba.dev`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer points the root elsewhere while its holder is stopped

The old holder has to give the apex up on its host, and a stopped host
cannot. Starting it, or destroying it, is the way past: `space destroy`
deletes the root's record itself and needs no host.

Command:

```
$ devctl apex set crm.sbx1
```

Output:

```
devctl: 'sbx2.ikigenba.dev' holds the apex and is stopped

run 'devctl space start sbx2' first
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space `sbx1.ikigenba.dev` exists and is `running`.
- The zone's `A` record `ikigenba.dev` points at the Elastic IP of
  `sbx2.ikigenba.dev`, whose instance is `stopped`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer's `apex set` fails on the target host

The CA refused, or the host could not write the challenge record, or nginx
rejected the configuration: whatever opsctl reported is what the developer
needs, and it follows the error line quoted with `> `. The record has not
been written, so the root still points where it did.

Command:

```
$ devctl apex set crm.sbx1
```

Output:

```
space: ok (sbx1.ikigenba.dev running, 18.118.7.42)
role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)
devctl: host: ssh ec2-user@18.118.7.42 sudo opsctl cert obtain: exit status 1

> opsctl: certbot certonly: exit status 1
> 
> > Some challenges have failed.
> > Detail: DNS problem: NXDOMAIN looking up TXT for _acme-challenge.ikigenba.dev
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- As for a set that succeeds, and `opsctl cert obtain` on the target host
  exits non-zero.

Postconditions:

- The target's policy allows the challenge record and its `host.apex` is
  `crm`; its certificate and nginx configuration are as they were.
- The zone's `A` record `ikigenba.dev` is as it was, and the old holder is
  untouched.
- Running `apex set crm.sbx1` again retries from the start.

## A developer's `apex set` cannot clear the previous holder

The root already points at the new holder and it is serving; what is left is
the old holder still claiming a name it can no longer renew. The failure is
reported and the developer runs the command again once the old host is
reachable.

Command:

```
$ devctl apex set crm.sbx1
```

Output:

```
space: ok (sbx1.ikigenba.dev running, 18.118.7.42)
role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)
host: ok (host.apex=crm, certificate obtained, nginx applied)
record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)
devctl: previous: ssh ec2-user@18.220.10.5: connection timed out
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

Preconditions:

- As for a set that succeeds, and the developer's machine cannot open an ssh
  connection to `sbx2`'s instance.

Postconditions:

- The steps that printed `ok` hold; the root resolves to `sbx1` and `crm`
  answers there.
- `sbx2`'s policy no longer allows the challenge record; on its host
  `host.apex` is still set and its certificate still carries the root.
  `certbot renew` there will fail until this is finished.
- Running `apex set crm.sbx1` again repeats every step; the first four
  change nothing and `previous` is retried.

## A developer asks where the root points

The record names the space, and the space's host names the app: `show`
reads the zone, matches the address to a space's Elastic IP, and asks that
host over ssh for `host.apex`. The one line is the app's hostname and the
address.

Command:

```
$ devctl apex show
```

Output:

```
crm.sbx1.ikigenba.dev 18.118.7.42
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone's `A` record `ikigenba.dev` points at `18.118.7.42`, the Elastic
  IP of `sbx1.ikigenba.dev`, whose instance is `running` and whose
  `host.apex` is `crm`.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- Nothing has changed. `sudo opsctl config get host.apex` was run on the
  host over ssh.

## A developer asks where the root points when it points nowhere

Command:

```
$ devctl apex show
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone holds no `A` record `ikigenba.dev`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer asks where the root points when its holder is stopped

Command:

```
$ devctl apex show
```

Output:

```
devctl: 'sbx1.ikigenba.dev' is stopped
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone's `A` record `ikigenba.dev` points at the Elastic IP of
  `sbx1.ikigenba.dev`, whose instance is `stopped`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer asks where the root points when it points at no space

devctl only ever writes the root's record to a space's Elastic IP, so a
record that points anywhere else was not devctl's doing and is reported
rather than guessed at.

Command:

```
$ devctl apex show
```

Output:

```
devctl: ikigenba.dev points at 203.0.113.9, which is not a space's address
```

Exits 1. The line is on stderr; stdout is empty. `apex set` refuses the same
way before any step, and `apex clear` deletes the record as below.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone's `A` record `ikigenba.dev` holds `203.0.113.9`, which is no
  space's Elastic IP.

Postconditions:

- Nothing has changed.

## A developer takes the root away

The reverse of `set`, in the reverse order: the record goes first, so the
root stops resolving to the host before the host stops carrying the name;
then the role is narrowed; then the host drops the key, reissues its
certificate with two names, and applies nginx. Nothing points at the root
afterwards, and the holder's host answers only at its own names.

Command:

```
$ devctl apex clear
```

Output:

```
space: ok (sbx1.ikigenba.dev running, 18.118.7.42)
record: ok (ikigenba.dev deleted)
role: ok (sbx1.ikigenba.dev may no longer prove ikigenba.dev)
host: ok (host.apex removed, certificate obtained, nginx applied)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone's `A` record `ikigenba.dev` points at `18.118.7.42`, the Elastic
  IP of `sbx1.ikigenba.dev`, whose instance is `running`, whose `host.apex`
  is `crm`, and whose certificate carries the root.
- The developer's ssh configuration can reach the instance as `ec2-user`,
  and the host can reach the CA.

Postconditions:

- The zone holds no `A` record `ikigenba.dev`. `space list` shows `-` in the
  last column of every line, and `apex show` prints nothing.
- The inline policy on `sbx1.ikigenba.dev`'s role no longer allows
  `_acme-challenge.ikigenba.dev`.
- On the host, `host.apex` is not set, the certificate covers
  `sbx1.ikigenba.dev` and `*.sbx1.ikigenba.dev` only, and nginx no longer
  names the root. `crm` is still deployed and answers at its own name.

## A developer takes the root away when nothing holds it

Command:

```
$ devctl apex clear
```

Output:

```
record: ok (already gone)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone holds no `A` record `ikigenba.dev`.

Postconditions:

- Nothing has changed. No ssh connection was opened. A record that points at
  no space's address is deleted and reported as `record: ok (ikigenba.dev
  deleted)`, the only line, since there is no host to clear.

## A developer takes the root away while its holder is stopped

Command:

```
$ devctl apex clear
```

Output:

```
devctl: 'sbx1.ikigenba.dev' is stopped
```

Exits 1. The line is on stderr; stdout is empty. Starting the space, or
destroying it, is the way past; `space destroy` deletes the record itself.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The zone's `A` record `ikigenba.dev` points at the Elastic IP of
  `sbx1.ikigenba.dev`, whose instance is `stopped`.

Postconditions:

- Nothing has changed. The record was not deleted.

## A developer gives `apex set` an operand that is not an app on a space

Command:

```
$ devctl apex set sbx1
```

```
$ devctl apex set crm.sbx1.example.com
```

```
$ devctl apex set ikigenba.dev
```

Output:

```
devctl: 'sbx1' is not an app on a space: <app>.<space>
```

Exits 2. The line is on stderr, naming the operand as typed; stdout is empty.
After the root suffix is stripped, exactly two labels must remain, each a
valid DNS label; `devctl: 'Crm.sbx1' is not a valid label` refuses a bad one
the way `space` does.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev`.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer runs `apex set` without an operand, or `show` or `clear` with one

Command:

```
$ devctl apex set
```

Output:

```
devctl: apex set needs <app>.<space>

see 'devctl apex --help' for usage
```

Command:

```
$ devctl apex show sbx1
```

Output:

```
devctl: apex show takes no arguments

see 'devctl apex --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty. `apex clear sbx1` says
`devctl: apex clear takes no arguments`.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.
