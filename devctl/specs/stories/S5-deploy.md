# Stories — deploy

Deploy puts one release of the whole suite on one space. Its argument after
`<space>` is a commit, given as a full sha, a shorter sha or a tag; tags such
as `auth/v0.18.2` carry a slash and are tags like any other. It resolves the
argument to the full commit sha exactly as build does (see `S4-build.md`):
lowercase hex of 4 to 40 characters, or a tag under `refs/tags/`, looked up in
the local repository with nothing fetched, and a branch or `HEAD` is not a
commit. Every deploy builds: the release is built on the developer's machine
as `devctl build <sha>` builds it, into `dist/<sha>.tar.xz`, and copied to the
space's host over ssh; nothing is uploaded to the bucket and there is no store
of built releases. On the host the tarball is unpacked into
`/opt/ikigenba/releases/<sha>/`, and that release's own opsctl,
`/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl`, activates it with
`activate <sha> [label]`. The label is the tag exactly as typed, `r1` or
`auth/v0.18.2`; a sha, full or short, gives no label. Nothing reaches the host
until the space's secrets objects hold every name the manifests in the
release declare. What activate does on the host, and what its lines say, is
opsctl's; devctl copies opsctl's stdout to its own stdout as opsctl writes
it, and a failure's stderr follows devctl's error line, quoted with `> `.
`rollback` has the release that ran before the current one activated again;
it too is opsctl's, and devctl runs it. Promotion is deploying the same
commit to a different space; no space restricts what it accepts. What each
space is running is read from the host with `space status`, never recorded
anywhere else. `<space>` is the space's label or its full domain, as
everywhere (see `S2-space-lifecycle.md`).

An app is taken off a space by deploying a release that does not hold it:
what activate does with an app the release drops is opsctl's.

## A developer asks what `deploy` can do

The top-level usage gains the line `  deploy    put a release on a space`
under `Commands:`.

Command:

```
$ devctl deploy --help
```

Output:

```
Usage: devctl deploy <space> <sha|tag>

Build the suite at <sha|tag> as build does, check that the space holds every
secret the release's manifests declare, copy dist/<sha>.tar.xz to the space's
host, unpack it into /opt/ikigenba/releases/<sha>/, and have that release's
opsctl activate it. A tag is the release's label, exactly as typed; a sha
gives none.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer deploys a release to a space

A developer wants a space to run the whole suite at one commit, named by a
release tag. devctl first resolves the tag and checks that the space exists
and is running, then builds; it then reads the `secrets` array of every
`<app>/etc/manifest.toml` in the release and checks each against the space's
secrets object for that app, before anything reaches the host. The tarball is
copied to a temporary path on the host and unpacked as root: when
`/opt/ikigenba/releases/<sha>/` is absent it is extracted beside it under a
temporary name and renamed into place, so the folder is either whole or not
there; `/opt/ikigenba/releases/` is created, owned by root with mode `0755`,
if the host has none. The temporary copy is removed once it is unpacked. Then
the release's own opsctl activates it, with the tag as its label.

Each of devctl's lines is one step: `build` names the tag, when a tag was
typed, and the file it wrote, `secrets` how many apps the release holds and
how many names their manifests declare in all, `copy` the file and the host's
address, and `unpack` the folder. The lines from `preflight` on are opsctl's,
copied as it writes them; they are shown here for illustration, and what they
say is opsctl's.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
preflight: ok (8 apps)
label: ok (r1)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a, previous 9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (r1 (4b22285))
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The local repository holds the tag `r1`, pointing at the commit
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, and the suite builds at that
  commit as `S4-build.md` says; its apps are `auth`, `dummy`, `events`,
  `mcp`, `repos`, `scripts`, `sites` and `telemetry`, and only `auth`'s
  manifest names secrets, `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`.
- The space exists and its instance is `running`; its host was set up by
  `space create` (see `S2-space-lifecycle.md`), and it is running the release
  `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c`.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `/sbx1.ikigenba.dev/auth` holds `GOOGLE_CLIENT_ID` and
  `GOOGLE_CLIENT_SECRET`.
- The host holds no `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/`.

Postconditions:

- `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` is in the checkout,
  as `devctl build 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` writes it.
- `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/` on the
  host holds the tarball's `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/` as it
  was built, and no copy of the tarball is left in the host's temporary
  directory.
- `sudo /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a r1`
  has been run on the host over ssh and exited 0, so every app in the release
  answers at its name from the new release. What activate did on the host is
  opsctl's.
- Nothing was uploaded to the bucket and nothing in the account changed.
- No tag, branch or worktree was created or moved, and the working tree is
  as it was.

## A developer deploys a release by its sha

A sha, full or as short as the local repository resolves, deploys the same
release a tag on that commit would, with no label: activate is given the sha
alone. A tag with a slash, such as `auth/v0.18.2`, is a tag like any other and
is the label as typed.

Command:

```
$ devctl deploy sbx1 4b22285
```

Output:

```
build: ok (dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
preflight: ok (8 apps)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a, previous 9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (4b22285)
```

Exits 0. The lines are on stdout; stderr is empty. The lines from `preflight`
on are opsctl's, as above.

Preconditions:

- As for deploying `r1`, with `4b22285` resolving to
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a` in the local repository.

Postconditions:

- As for deploying `r1`, except that the command run on the host was
  `sudo /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`,
  with no label.

## A developer deploys a release the host already holds

A release already unpacked on the host is not unpacked again: the folder is
left exactly as it is, the `unpack` line says so, and the temporary copy is
removed. Activate runs all the same, so deploying the release a space already
runs restarts every app, and deploying it by a tag gives it that label.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a already present, kept)
preflight: ok (8 apps)
label: ok (r1)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
nginx: ok (8 apps)
restart: ok (8 apps)
activate: ok (r1 (4b22285))
```

Exits 0. The lines are on stdout; stderr is empty. The lines from `preflight`
on are opsctl's.

Preconditions:

- As for deploying `r1`, except that the host already holds
  `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/`, here
  because the space runs that release, deployed earlier by its sha.

Postconditions:

- Nothing under `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/`
  was replaced or extracted again, and no copy of the tarball is left in the
  host's temporary directory.
- The release's opsctl ran `activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a r1`
  and exited 0. What activate does with a release that is already current is
  opsctl's.

## A developer deploys something that is not a commit

An argument the local repository cannot resolve to a commit is refused before
anything else is done. Nothing is fetched to look for it. A branch such as
`main`, `HEAD`, a tag the local repository does not hold, and a sha no commit
has all fail the same way, and so does a path to a file, such as
`dist/<sha>.tar.xz`: deploy takes a commit, never a file.

Command:

```
$ devctl deploy sbx1 r9
```

```
$ devctl deploy sbx1 main
```

Output:

```
devctl: 'r9' is not a commit
```

Exits 2. The line is on stderr, naming the argument as typed; stdout is
empty.

Preconditions:

- The working directory is inside the checkout.
- The local repository holds no tag `r9`; `main` is a branch, not a tag.

Postconditions:

- Nothing has changed. Nothing was built, no AWS call was made, and no ssh
  connection was opened.

## A developer deploys a release to a space that does not exist, or one that is stopped

The space is checked before the build, so a deploy that could not reach a host
builds nothing.

Command:

```
$ devctl deploy gone r1
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Command:

```
$ devctl deploy sbx2 r1
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The local repository holds the tag `r1`.
- No instance is tagged `Space=gone.ikigenba.dev`; the instance tagged
  `Space=sbx2.ikigenba.dev` is `stopped`.

Postconditions:

- Nothing has changed. Nothing was built, nothing under `dist/` was written,
  and no ssh connection was opened.

## A developer deploys a release that does not build

The build fails exactly as `devctl build` fails at that commit, with the same
lines (see `S4-build.md`), and nothing reaches the host.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
devctl: build dashboard: exit status 1

> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard
> cmd/dashboard/main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty. A refusal the build makes
before compiling, such as a stale manifest, a manifest naming a `port`, or a
reserved app name, gives its own line from `S4-build.md` and exits 2.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`.
- `r1` points at a commit where `dashboard/` is an app that does not compile
  for `linux/amd64`.

Postconditions:

- Nothing under `dist/` has changed, and no worktree is left behind.
- No secrets object was read and no ssh connection was opened; the host is
  as it was.

## A developer deploys a release while the space lacks a secret it declares

Every manifest in the release is checked before anything is copied, in name
order, and the first app missing a name is the one reported. Keys an object
has that no manifest names are left alone.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
devctl: auth: secrets missing GOOGLE_CLIENT_SECRET

run 'devctl secrets push sbx1 auth'
```

Exits 2. The `ok` line is on stdout; the rest is on stderr.

Preconditions:

- As for deploying `r1`, except that `/sbx1.ikigenba.dev/auth` holds
  `GOOGLE_CLIENT_ID` and not `GOOGLE_CLIENT_SECRET`.

Postconditions:

- `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` is in the checkout.
- No ssh connection was opened; the host is as it was.

## A developer's release cannot be copied to the host

The copy is made with `scp`, and what it said follows the error line, quoted
with `> `.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
devctl: copy: scp /home/dev/ikigenba/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz ec2-user@18.118.7.42:/tmp/tmp.Xb3kQ9aLpz: exit status 1

> scp: /tmp/tmp.Xb3kQ9aLpz: No space left on device
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- As for deploying `r1`, with the checkout at `/home/dev/ikigenba`, and the
  host's temporary directory cannot hold the tarball.

Postconditions:

- Nothing under `/opt/ikigenba/releases/` changed, and activate was not run:
  the space runs what it ran before.

## A developer's release cannot be unpacked on the host

The extraction's own output follows the error line, quoted with `> `. A
release is extracted under a temporary name and renamed into place only once
it is whole, so a failed unpack leaves no folder for that sha.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
devctl: unpack: ssh ec2-user@18.118.7.42 sudo tar -x -J --no-same-owner -f /tmp/tmp.Xb3kQ9aLpz -C /opt/ikigenba/releases/.unpack.Q7mN2cRt4w: exit status 2

> tar: 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/auth/bin/auth: Cannot write: No space left on device
> tar: Exiting with failure status due to previous errors
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- As for deploying `r1`, and the host's disk under `/opt/ikigenba/` cannot
  hold the unpacked release.

Postconditions:

- No `/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/` is on
  the host, and activate was not run: the space runs what it ran before.

## A developer's release fails to activate on the host

opsctl's stdout has already been copied, up to the step that failed; its
stderr follows devctl's error line, each line quoted with `> `. The lines
from `preflight` on are opsctl's, shown for illustration.

Command:

```
$ devctl deploy sbx1 r1
```

Output:

```
build: ok (r1, dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz)
secrets: ok (8 apps, 2 keys)
copy: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz -> 18.118.7.42)
unpack: ok (/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
preflight: ok (8 apps)
label: ok (r1)
env: ok (8 apps)
units: ok (8 apps)
current: ok (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a, previous 9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c)
nginx: ok (8 apps)
restart: failed: events: service failed to start
devctl: activate: ssh ec2-user@18.118.7.42 sudo /opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl activate 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a r1: exit status 1

> opsctl: activate failed
> 
> > ikigenba-events.service: Main process exited, code=exited, status=1/FAILURE
```

Exits 1. The lines up to the failed step are on stdout; the rest is on
stderr.

Preconditions:

- As for deploying `r1`, and `events` in that release exits as soon as it
  starts.

Postconditions:

- The release is unpacked on the host, and the space is whatever activate
  left it; `space status` reports it, and `rollback` or another deploy is the
  way on.

## A developer runs `deploy` without a release

Command:

```
$ devctl deploy sbx1
```

Output:

```
devctl: deploy needs <space> and <sha|tag>

see 'devctl deploy --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer asks what `rollback` can do

The top-level usage gains the line `  rollback  put a space back on the
release it ran before` under `Commands:`.

Command:

```
$ devctl rollback --help
```

Output:

```
Usage: devctl rollback <space>

Have opsctl on the space activate the release it ran before the current one
again: current points at previous, previous is removed, and every app is
restarted. With no previous release opsctl refuses, so a second rollback is
refused until the next deploy. What rollback does on the host is opsctl's.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer rolls a space back

A deploy went badly and the developer wants the space on what it ran before.
devctl runs `sudo opsctl rollback` on the host over ssh and copies opsctl's
stdout to its own as opsctl writes it; devctl adds no line of its own. Nothing
is built and the checkout's commits are not read. The lines are opsctl's,
shown here for illustration.

Command:

```
$ devctl rollback sbx1
```

Output:

```
current: ok (9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c, previous removed)
env: ok (8 apps)
units: ok (8 apps)
nginx: ok (8 apps)
restart: ok (8 apps)
rollback: ok (9e1c7a3)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; its host runs the release
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, deployed over
  `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c`, which is its previous release.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- `sudo opsctl rollback` has been run on the host over ssh and exited 0, so
  the space runs `9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c` and has no
  previous release. What rollback did on the host is opsctl's.
- Nothing in the checkout or the account changed.

## A developer rolls back a space that has no previous release

A space just created, or one already rolled back once, has no previous
release, and opsctl refuses, changing nothing. Its refusal follows devctl's
error line, quoted with `> `.

Command:

```
$ devctl rollback sbx1
```

Output:

```
devctl: rollback: ssh ec2-user@18.118.7.42 sudo opsctl rollback: exit status 1

> opsctl: no previous release to roll back to
```

Exits 1. The text is on stderr; stdout is empty. The quoted line is opsctl's,
shown for illustration.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists and its instance is `running`; its host has a current
  release and no previous one.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- Nothing has changed.

## A developer rolls back a space that does not exist, or one that is stopped

Command:

```
$ devctl rollback gone
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Command:

```
$ devctl rollback sbx2
```

Output:

```
devctl: 'sbx2.ikigenba.dev' is stopped
```

Each exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance is tagged `Space=gone.ikigenba.dev`; the instance tagged
  `Space=sbx2.ikigenba.dev` is `stopped`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer runs `rollback` without a space

Command:

```
$ devctl rollback
```

Output:

```
devctl: rollback needs <space>

see 'devctl rollback --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.
