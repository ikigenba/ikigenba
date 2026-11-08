# Stories — build

Build turns a commit into the release deploy carries to a host: one release
of the whole suite. Its argument is a commit, given as a full sha, a shorter
sha or a tag. It resolves that argument to the full commit sha, 40 lowercase
hex digits, in the local repository and fetches nothing. It builds from that
commit alone: the developer's working tree, its `HEAD`, its branch and its
cleanliness are not read, so uncommitted changes neither block the build nor
reach the release. A tag is read only to find the commit it names; the
release is the same whichever tag, or the bare sha, was typed, and no tag is
recorded in it. Nothing is injected into a binary. The release is
`dist/<sha>.tar.xz` at the checkout root, and a rebuild of the same sha
writes the same name. Its only top-level entry is `<sha>/`, so unpacking it
as it is gives the folder a host keeps for that release. In it are
`release.json` and one directory per app, plus `opsctl/` and `turns/`: every
app the commit holds, each laid out as `<app>/bin/<app>` and, when present,
`<app>/etc/`, `<app>/share/`, `<app>/libexec/` and `<app>/lib/`; nothing else
from an app's directory is shipped. opsctl and turns are the release's two
tools: neither has a manifest, so neither is an app, and both are always
built. opsctl ships as
`opsctl/bin/opsctl` alone; turns ships as `turns/bin/turns` with its committed
`share/`, which holds its user systemd units. Apps are built in name order,
then opsctl, then turns, and the first failure stops the build with no release
written. Each app is built a second time, for the developer's own machine, so
it can be run there to emit its manifest; that build is not shipped, and the
release holds only the `linux/arm64` binaries.

An app's name goes three places on a host, and each constrains it. It is a
DNS label, because the app answers at `<app>.<host.name>`: lowercase
letters, digits, and hyphens, at most 63 characters, not starting or ending
with a hyphen. It is a prefix under the space's backup URI, where `host/`,
`deploy/`, `snapshots/`, and `seed/` already live. And it is a unit name,
`ikigenba-<app>.service`, where `backup-host`, `backup-services`, and
`renew-certificate` already live. A usable app name is a DNS label that is
none of those seven, and build refuses an app at the commit with any other
name before it compiles anything. It also refuses an app named `opsctl` or
`turns`, whose directory in the release is that tool's own.

## A developer asks what `build` can do

The top-level usage gains the line `  build     build the suite at a commit
into a release` under `Commands:`.

Command:

```
$ devctl build --help
```

Output:

```
Usage: devctl build <sha|tag>

Build the suite at <sha|tag> for linux/arm64 and write dist/<sha>.tar.xz, one
release holding every app, opsctl and turns. <sha> is the full commit sha the
argument resolves to; the working tree is not read.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer builds the suite at a commit

A developer wants one release of the whole suite at a commit, to deploy it
or to inspect it. The commit can be named by its tag, its full sha, or a
shorter sha the local repository resolves. All three forms below name the
same commit and write the same release. The one line of output is the path
written, relative to the checkout root.

Command:

```
$ devctl build release-87
```

```
$ devctl build 4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a
```

```
$ devctl build 4b22285
```

Output:

```
dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The local repository holds the commit
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, and the tag `release-87` in it points
  at that commit.
- At that commit the apps, sub-projects with a `main` package and a committed
  `etc/manifest.toml`, are `auth`, `dummy`, `events`, `mcp`, `repos`,
  `scripts`, `sites` and `telemetry`; `opsctl/` holds opsctl; and `turns/`
  holds turns, with `turns/share/systemd/turns.service` and
  `turns/share/systemd/turns.timer` committed.
- The Go toolchain can build each of them for `linux/arm64`, and each app
  also for the developer's machine, where that build's `<app> manifest`
  emits exactly that app's committed `etc/manifest.toml`.
- The working tree may have uncommitted changes, be on any branch or none,
  and have `HEAD` at any commit; none of it is read.

Postconditions:

- `dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` exists at the
  checkout root, replacing any earlier file of that name. `dist/` was created
  if it did not exist.
- The tarball's only top-level entry is
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/`, and it holds:
  - `release.json`, one JSON object with exactly the keys `sha`, `built` and
    `devctl`: the full sha, the build time in UTC as RFC 3339 to the second,
    and the version `devctl --version` prints, for example
    `{"sha": "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a", "built": "2026-10-08T14:03:12Z", "devctl": "<version>"}`
    where `<version>` is that version;
  - for each of the eight apps, `<app>/bin/<app>`, the static `linux/arm64`
    binary and the only file in `<app>/bin/`; `<app>/etc/manifest.toml`,
    emitted by running the app's build for the developer's machine with the
    argument `manifest`; every other file under the app's committed `etc/`,
    `nginx.conf` included; and the app's committed `share/`, `libexec/` and
    `lib/`, each as it is, when the app has one;
  - `opsctl/bin/opsctl`, the static `linux/arm64` binary, and nothing else
    under `opsctl/`;
  - `turns/bin/turns`, the static `linux/arm64` binary and the only file in
    `turns/bin/`, and turns' committed `share/` as it is,
    `turns/share/systemd/turns.service` and
    `turns/share/systemd/turns.timer`; nothing else is under `turns/`.
- No version appears in any path inside the tarball, and no binary has a
  version injected.
- No tag, branch or worktree was created or moved, and none is left behind;
  `git worktree list` shows what it showed before.
- The working tree, its index and its `HEAD` are as they were. Nothing outside
  `dist/` has changed.

## A developer builds something that is not a commit

An argument the local repository cannot resolve to a commit is refused.
Nothing is fetched to look for it. A branch such as `main`, `HEAD`, a tag the
local repository does not hold, and a sha no commit has all fail the same
way, and so does an app's name: build takes a commit, never an app.

Command:

```
$ devctl build bogus
```

Output:

```
devctl: 'bogus' is not a commit
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- No sha, shorter sha or tag `bogus` resolves to a commit in the local
  repository.

Postconditions:

- Nothing has changed. Nothing was compiled, and nothing under `dist/` was
  written.

## A developer builds the suite at a commit with a stale manifest

Each app's build for the developer's machine is run with the argument
`manifest`, and what it emits must be byte for byte the `etc/manifest.toml`
committed at that commit.

Command:

```
$ devctl build release-87
```

Output:

```
devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `release-87` points at a commit where `crm/` is an app, and every app before it in
  name order builds and passes its manifest check.
- At that commit `crm` compiles, and `crm manifest` emits something other
  than its committed `etc/manifest.toml`.

Postconditions:

- Nothing under `dist/` has changed; an earlier `dist/<sha>.tar.xz`, if any,
  is as it was.
- No worktree is left behind, and the working tree is as it was.

## A developer builds the suite at a commit whose manifest names a port

Build refuses a committed manifest that names a `port`, whatever its value,
before it compiles anything.

Command:

```
$ devctl build release-87
```

Output:

```
devctl: crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `release-87` points at a commit where `crm/etc/manifest.toml` holds a top-level
  `port` key.

Postconditions:

- Nothing has changed. Nothing was compiled, nothing under `dist/` was
  written, and no worktree is left behind.

## A developer builds the suite at a commit holding an app with a reserved name

Command:

```
$ devctl build release-87
```

Output:

```
devctl: 'host' is not a usable app name
```

Exits 2. The line is on stderr; stdout is empty. Each of the other six
reserved names fails the same way, so does a name that is not a DNS label,
and so does an app named `opsctl` or `turns`.

Preconditions:

- `release-87` points at a commit where `host/` is a sub-project with a `main`
  package and `host/etc/manifest.toml`.

Postconditions:

- Nothing has changed. Nothing was compiled, nothing under `dist/` was
  written, and no worktree is left behind.

## A developer builds the suite at a commit where an app holds `sbin/` or `include/`

A release's app tree holds only `bin/`, `etc/`, `share/`, `libexec/` and
`lib/`. `sbin/` and `include/` are not used, and an app that has either at
the commit is refused rather than shipped without it.

Command:

```
$ devctl build release-87
```

Output:

```
devctl: crm: sbin/ is not allowed in a release
```

Exits 2. The line is on stderr; stdout is empty. An app holding `include/`
fails the same way, naming `include/`.

Preconditions:

- `release-87` points at a commit where `crm/` is an app holding a `sbin/` directory.

Postconditions:

- Nothing under `dist/` was written; an earlier `dist/<sha>.tar.xz`, if any,
  is as it was.
- No worktree is left behind, and the working tree is as it was.

## A developer's suite build does not compile

A failed compile names the app, or `opsctl` or `turns`, that failed; the
compiler's output follows the error line, each line quoted with `> ` so it is
plainly the compiler's and not devctl's.

Command:

```
$ devctl build release-87
```

Output:

```
devctl: build dashboard: exit status 1

> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard
> cmd/dashboard/main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `release-87` points at a commit where `dashboard/` is an app that does not compile
  for `linux/arm64`, and every app before it in name order builds.

Postconditions:

- Nothing under `dist/` has changed; an earlier `dist/<sha>.tar.xz`, if any,
  is as it was. No app after `dashboard`, and neither opsctl nor turns,
  was compiled.
- No worktree is left behind, and the working tree is as it was.

## A developer runs `build` without an argument

Command:

```
$ devctl build
```

Output:

```
devctl: build needs <sha|tag>

see 'devctl build --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.
