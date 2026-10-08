# Stories — build

Build turns a commit into what deploy carries to a host, in one of two forms
that coexist until the per-app form goes away. An argument that names an app
in the checkout, a sub-project with a `main` package and a committed
`etc/manifest.toml` whose directory name it is, is the per-app build; any
other argument is a commit, given as a full sha, a shorter sha or a tag, and
is the suite build. An argument that names an app is always the per-app
build; release tags are `r<N>` or carry a slash, so no app name is also a
tag.

The suite build makes one release of the whole suite. It resolves its
argument to the full commit sha, 40 lowercase hex digits, in the local
repository and fetches nothing. It builds from that commit alone: the
developer's working tree, its `HEAD`, its branch and its cleanliness are not
read, so uncommitted changes neither block the build nor reach the release.
A tag is read only to find the commit it names; the release is the same
whichever tag, or the bare sha, was typed, and no tag is recorded in it.
Nothing is injected into a binary. The release is `dist/<sha>.tar.xz` at the
checkout root, and a rebuild of the same sha writes the same name. Its only
top-level entry is `<sha>/`, so unpacking it as it is gives the folder a host
keeps for that release. In it are `release.json` and one directory per app,
plus `opsctl/`: every app the commit holds, each laid out as `<app>/bin/<app>`
and, when present, `<app>/etc/`, `<app>/share/`, `<app>/libexec/` and
`<app>/lib/`; nothing else from an app's directory is shipped. opsctl has no
manifest, so it is not an app; it is always built, as `opsctl/bin/opsctl`.
Apps are built in name order and opsctl last, and the first failure stops
the build with no release written.

A per-app file is only ever built from a committed tree, `HEAD`
with no uncommitted changes, and it is named by `HEAD`'s full commit sha, 40
lowercase hex digits, so every file's name says exactly which commit is inside
it. The per-app build reads no tag, and no tag is needed or made: whatever
tags point at `HEAD`, or none, the file is the same. Two apps built at one commit each get
their own file carrying the same sha. Which branch the commit is on does not
matter, and nothing about the branch is recorded anywhere.

An app's name goes three places on a host, and each constrains it. It is a
DNS label, because the app answers at `<app>.<host.name>`: lowercase
letters, digits, and hyphens, at most 63 characters, not starting or ending
with a hyphen. It is a prefix under the space's backup URI, where `host/`,
`deploy/`, `snapshots/`, and `seed/` already live. And it is a unit name,
`ikigenba-<app>.service`, where `backup-host`, `backup-services`, and
`renew-certificate` already live. A usable app name is a DNS label that is
none of those seven, and build refuses any other before it compiles
anything. opsctl applies the same rule
at install, because a file can come from anywhere. The suite build applies
the same rule to every app at the commit, and also refuses an app named
`opsctl`, whose directory in the release is opsctl's own.

## A developer asks what `build` can do

The top-level usage gains the line `  build     build the suite or one app
into a deployable file` under `Commands:`.

Command:

```
$ devctl build --help
```

Output:

```
Usage: devctl build <sha|tag>
       devctl build <app>

Build the suite at <sha|tag> for linux/amd64 and write dist/<sha>.tar.xz, one
release holding every app and opsctl. <sha> is the full commit sha the argument
resolves to; the working tree is not read.

Build <app> for linux/amd64 and write <app>/dist/<app>-<sha>.tar.xz, the file
deploy copies to a host and opsctl installs. <sha> is HEAD's full commit sha;
the working tree must have no uncommitted changes.
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
$ devctl build r1
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
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, and the tag `r1` in it points
  at that commit.
- At that commit the apps, sub-projects with a `main` package and a committed
  `etc/manifest.toml`, are `auth`, `dummy`, `events`, `mcp`, `repos`,
  `scripts`, `sites` and `telemetry`, and `opsctl/` holds opsctl.
- The Go toolchain can build each of them for `linux/amd64`, and each app's
  built binary runs on the developer's machine and its `<app> manifest`
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
  - for each of the eight apps, `<app>/bin/<app>`, the static `linux/amd64`
    binary and the only file in `<app>/bin/`; `<app>/etc/manifest.toml`,
    emitted by running that binary with the argument `manifest`; every other
    file under the app's committed `etc/`, `nginx.conf` included; and the
    app's committed `share/`, `libexec/` and `lib/`, each as it is, when the
    app has one;
  - `opsctl/bin/opsctl`, the static `linux/amd64` binary, and nothing else
    under `opsctl/`.
- No version appears in any path inside the tarball, and no binary has a
  version injected.
- No tag, branch or worktree was created or moved, and none is left behind;
  `git worktree list` shows what it showed before.
- The working tree, its index and its `HEAD` are as they were. Nothing outside
  `dist/` has changed.

## A developer builds something that is neither an app nor a commit

An argument that names no app in the checkout is a commit, and one the local
repository cannot resolve is refused. Nothing is fetched to look for it.

Command:

```
$ devctl build bogus
```

Output:

```
devctl: 'bogus' is neither an app in the checkout nor a commit
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- No sub-project `bogus` with a `main` package and `etc/manifest.toml` in the
  checkout.
- No sha, shorter sha or tag `bogus` resolves to a commit in the local
  repository.

Postconditions:

- Nothing has changed. Nothing was compiled, and nothing under `dist/` was
  written.

## A developer builds the suite at a commit with a stale manifest

The suite build checks each app's manifest as the per-app build does, against
the manifest committed at that commit.

Command:

```
$ devctl build r1
```

Output:

```
devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `r1` points at a commit where `crm/` is an app, and every app before it in
  name order builds and passes its manifest check.
- At that commit `crm` compiles, and `crm manifest` emits something other
  than its committed `etc/manifest.toml`.

Postconditions:

- Nothing under `dist/` has changed; an earlier `dist/<sha>.tar.xz`, if any,
  is as it was.
- No worktree is left behind, and the working tree is as it was.

## A developer builds the suite at a commit whose manifest names a port

The suite build refuses a committed manifest that names a `port`, whatever
its value, as the per-app build does, before it compiles anything.

Command:

```
$ devctl build r1
```

Output:

```
devctl: crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `r1` points at a commit where `crm/etc/manifest.toml` holds a top-level
  `port` key.

Postconditions:

- Nothing has changed. Nothing was compiled, nothing under `dist/` was
  written, and no worktree is left behind.

## A developer builds the suite at a commit holding an app with a reserved name

Command:

```
$ devctl build r1
```

Output:

```
devctl: 'host' is not a usable app name
```

Exits 2. The line is on stderr; stdout is empty. Each of the other six
reserved names fails the same way, so does a name that is not a DNS label,
and so does an app named `opsctl`.

Preconditions:

- `r1` points at a commit where `host/` is a sub-project with a `main`
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
$ devctl build r1
```

Output:

```
devctl: crm: sbin/ is not allowed in a release
```

Exits 2. The line is on stderr; stdout is empty. An app holding `include/`
fails the same way, naming `include/`.

Preconditions:

- `r1` points at a commit where `crm/` is an app holding a `sbin/` directory.

Postconditions:

- Nothing under `dist/` was written; an earlier `dist/<sha>.tar.xz`, if any,
  is as it was.
- No worktree is left behind, and the working tree is as it was.

## A developer's suite build does not compile

A failed compile reports the same way as in the per-app build, naming the app,
or `opsctl`, that failed; the compiler's output follows, quoted with `> `.

Command:

```
$ devctl build r1
```

Output:

```
devctl: build dashboard: exit status 1

> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard
> cmd/dashboard/main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `r1` points at a commit where `dashboard/` is an app that does not compile
  for `linux/amd64`, and every app before it in name order builds.

Postconditions:

- Nothing under `dist/` has changed; an earlier `dist/<sha>.tar.xz`, if any,
  is as it was. No app after `dashboard`, and not opsctl, was compiled.
- No worktree is left behind, and the working tree is as it was.

## A developer builds one app at a commit

A developer at a commit wants the file for one app, to deploy it or to
inspect it. An app is a sub-project of the checkout that has a `main` package
and a committed `etc/manifest.toml`; its name is the directory name. The
manifest is emitted by the binary itself (`<app> manifest`) and committed, so
the binary is the source of truth; build regenerates it and checks the two
agree. The file is named from `HEAD`'s sha. Build never runs the binary's
`--version`: the manifest is the only thing it asks the binary for. The one
line of output is the path written.

Command:

```
$ devctl build crm
```

Output:

```
crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree has no uncommitted changes.
- `HEAD` is the commit `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, on any
  branch or on none. Whatever tags point at it, or none, are not read.
- The Go toolchain can build `crm` for `linux/amd64`.
- The built binary runs on the developer's machine, and `crm manifest` emits
  exactly the committed `crm/etc/manifest.toml`.

Postconditions:

- `crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` exists in the
  checkout, replacing any earlier file of that name. `crm/dist/` was created if
  it did not exist.
- The tarball holds, relative to its root and with no version anywhere
  inside:
  - `bin/crm`, the static `linux/amd64` binary;
  - `etc/manifest.toml`, emitted by running that binary with the argument
    `manifest`;
  - every other file under `crm/etc/`, `nginx.conf` included;
  - `crm/share/` as `share/`, when the app has one.
- No tag was created, moved, or read.
- Nothing outside `crm/dist/` has changed.

## A developer builds with uncommitted changes

Command:

```
$ devctl build crm
```

Output:

```
devctl: the working tree has uncommitted changes; commit them first
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree has uncommitted changes, whether or not they are under
  `crm/`.

Postconditions:

- Nothing has changed. Nothing under `crm/dist/` was written.

## A developer builds an app whose committed manifest is stale

The binary emits the manifest; the committed copy exists so the checkout can
be read without a build. When they differ, the committed copy is wrong.

Command:

```
$ devctl build crm
```

Output:

```
devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree is clean.
- `crm` compiles, and `crm manifest` emits something other than the committed
  file.

Postconditions:

- Nothing under `crm/dist/` has changed.

## A developer builds an app whose manifest names a port

No app listens on a port: the host hands each app its socket, and opsctl
refuses to install a file whose manifest carries a `port`. Build refuses it
first, so such a file is never written, and says so from the committed
manifest before it compiles anything. The key is refused whatever its value.

```toml
app = "crm"
port = 3100
default = false
secrets = ["CRM_API_KEY", "CRM_API_SECRET", "CRM_ORG"]
```

Command:

```
$ devctl build crm
```

Output:

```
devctl: crm: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package, and the committed
  `crm/etc/manifest.toml` is the one above.
- The working tree is clean.

Postconditions:

- Nothing has changed. Nothing was compiled, and nothing under `crm/dist/`
  was written; an earlier `crm/dist/crm-<sha>.tar.xz`, if any, is as it
  was.

## A developer builds an app with a reserved name

Command:

```
$ devctl build host
```

Output:

```
devctl: 'host' is not a usable app name
```

Exits 2. The line is on stderr; stdout is empty. Each of the other six
reserved names, `seed` and `snapshots` among them, fails the same way, and so
does a name that is not a DNS label, `Crm` or `crm_v2` say, whenever the
checkout has a sub-project of that name with a `main` package and
`etc/manifest.toml`.

Preconditions:

- `host/` is a sub-project with a `main` package and `host/etc/manifest.toml`.

Postconditions:

- Nothing has changed. Nothing was compiled.

## A developer runs `build` without an argument

Command:

```
$ devctl build
```

Output:

```
devctl: build needs <sha|tag> or <app>

see 'devctl build --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer's build does not compile

The compiler's output follows the error line, each line quoted with `> ` so
it is plainly the compiler's and not devctl's.

Command:

```
$ devctl build dashboard
```

Output:

```
devctl: build dashboard: exit status 1

> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard
> cmd/dashboard/main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `dashboard/` is a sub-project with a `main` package and
  `dashboard/etc/manifest.toml`.
- The working tree is clean.
- `dashboard` does not compile for `linux/amd64`.

Postconditions:

- Nothing under `dashboard/dist/` has changed; an earlier
  `dashboard/dist/dashboard-<sha>.tar.xz`, if any, is as it was.
