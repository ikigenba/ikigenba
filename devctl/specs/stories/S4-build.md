# Stories — build

Build turns one app in the checkout into the file that deploy carries to a
host. The file is what `opsctl` installs; devctl and opsctl agree on nothing
else about an app. A file is only ever built from a commit that the app's own
version tag points at, so every file's name says exactly what is inside it.
Every sub-project in the checkout is tagged `<name>/v<semver>`, and an app is
no different: `crm/v0.1.0`, a prerelease such as `crm/v0.2.0-rc.1`, or one
with build metadata such as `crm/v1.0.0+build.7`. The version is the part
after the slash, and it is all the file name and the binary carry. Apps
version independently: only `crm/...` tags say anything about `crm`, and two
apps tagged at one commit each get their own file from their own tag. Which
branch the commit is on does not matter; a prerelease tag on a branch is how
work reaches a sandbox before it is released.

An app's name goes three places on a host, and each constrains it. It is a
DNS label, because the app answers at `<app>.<host.name>`: lowercase
letters, digits, and hyphens, at most 63 characters, not starting or ending
with a hyphen. It is a prefix under the space's backup URI, where `host/`
and `deploy/` already live. And it is a unit name, `ikigenba-<app>.service`,
where `backup-host`, `backup-services`, and `renew-certificate` already
live. A usable app name is a DNS label that is none of those five, and build
refuses any other before it compiles anything. opsctl applies the same rule
at install, because a file can come from anywhere.

## A developer asks what `build` can do

The top-level usage gains the line `  build     build one app into its
deployable file` under `Commands:`.

Command:

```
$ devctl build --help
```

Output:

```
Usage: devctl build <app>

Build <app> for linux/amd64 and write <app>/dist/<app>-<version>.tar.xz, the
file deploy copies to a host and opsctl installs. HEAD must be a commit that
the app's version tag (<app>/v<semver>) points at, with no uncommitted
changes.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer builds one app at a release

A developer at a tagged commit wants the file for one app, to deploy it or to
inspect it. An app is a sub-project of the checkout that has a `main` package
and a committed `etc/manifest.toml`; its name is the directory name. The
manifest is emitted by the binary itself (`<app> manifest`) and committed, so
the binary is the source of truth; build regenerates it and checks the two
agree. The file is named from the tag's version, verbatim, but the version
the app reports is compiled into it, so build asks the binary for that too
(`<app> --version`) and checks it against the tag. Nothing downstream
compares them again: a host only ever asks the binary. The one line of output
is the path written.

Command:

```
$ devctl build crm
```

Output:

```
crm/dist/crm-v0.1.0.tar.xz
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree has no uncommitted changes.
- The tag `crm/v0.1.0` points at `HEAD`.
- The Go toolchain can build `crm` for `linux/amd64`.
- The built binary runs on the developer's machine, `crm --version` prints
  `v0.1.0`, and `crm manifest` emits exactly the committed
  `crm/etc/manifest.toml`.

Postconditions:

- `crm/dist/crm-v0.1.0.tar.xz` exists in the checkout, replacing any earlier
  file of that name. `crm/dist/` was created if it did not exist.
- The tarball holds, relative to its root and with no version anywhere
  inside:
  - `bin/crm`, the static `linux/amd64` binary;
  - `etc/manifest.toml`, emitted by running that binary with the argument
    `manifest`;
  - every other file under `crm/etc/`, `nginx.conf` included;
  - `crm/share/` as `share/`, when the app has one.
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

## A developer builds a prerelease on a branch

The same build at a commit on a branch, where the tag is a prerelease. The
file carries the tag's version verbatim, and the binary reports it verbatim;
nothing about the branch is recorded anywhere.

Command:

```
$ devctl build crm
```

Output:

```
crm/dist/crm-v0.2.0-rc.1.tar.xz
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree has no uncommitted changes.
- The tag `crm/v0.2.0-rc.1` points at `HEAD`. `HEAD` is on any branch, or
  on none.
- The Go toolchain can build `crm` for `linux/amd64`.
- The built binary runs on the developer's machine, `crm --version` prints
  `v0.2.0-rc.1`, and `crm manifest` emits exactly the committed
  `crm/etc/manifest.toml`.

Postconditions:

- `crm/dist/crm-v0.2.0-rc.1.tar.xz` exists in the checkout, with the same
  layout as any other file build writes.
- Nothing outside `crm/dist/` has changed.

## A developer builds at a commit that is not tagged

Only the app's own tags count. A commit whose only tags are
`release-2026-09`, a bare `v0.1.0`, or `dashboard/v0.1.0` is untagged as far
as building `crm` is concerned.

Command:

```
$ devctl build crm
```

Output:

```
devctl: no tag crm/v<semver> points at HEAD (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree is clean.
- No tag of the form `crm/v<semver>` points at `HEAD`.

Postconditions:

- Nothing has changed.

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
- The working tree is clean and a `crm/v<semver>` tag points at `HEAD`.
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
- The working tree is clean and a `crm/v<semver>` tag points at `HEAD`.

Postconditions:

- Nothing has changed. Nothing was compiled, and nothing under `crm/dist/`
  was written; an earlier `crm/dist/crm-<version>.tar.xz`, if any, is as it
  was.

## A developer builds an app whose version string is stale

The tag's version names the file; the binary carries its own. A file whose
name and contents disagree would install as one version and report the other
for the rest of its life, because the host only ever asks the binary.

Command:

```
$ devctl build crm
```

Output:

```
devctl: crm: tagged crm/v0.1.0 but the binary reports v0.0.9
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree is clean and the tag `crm/v0.1.0` points at `HEAD`.
- `crm` compiles, and `crm --version` prints `v0.0.9`.

Postconditions:

- Nothing under `crm/dist/` has changed.

## A developer builds an app that is not in the checkout

Command:

```
$ devctl build bogus
```

Output:

```
devctl: no app 'bogus' in the checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- No sub-project `bogus` with a `main` package and `etc/manifest.toml` in the
  checkout.

Postconditions:

- Nothing has changed.

## A developer builds an app with a reserved name

Command:

```
$ devctl build host
```

Output:

```
devctl: 'host' is not a usable app name
```

Exits 2. The line is on stderr; stdout is empty. A name that is not a DNS
label, `Crm` or `crm_v2` say, fails the same way.

Preconditions:

- `host/` is a sub-project with a `main` package and `host/etc/manifest.toml`.

Postconditions:

- Nothing has changed. Nothing was compiled.

## A developer runs `build` without an app

Command:

```
$ devctl build
```

Output:

```
devctl: build needs <app>

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
- The working tree is clean and a `dashboard/v<semver>` tag points at
  `HEAD`.
- `dashboard` does not compile for `linux/amd64`.

Postconditions:

- Nothing under `dashboard/dist/` has changed; an earlier
  `dashboard/dist/dashboard-<version>.tar.xz`, if any, is as it was.
