# Stories — build

Build turns one app in the checkout into the file that deploy carries to a
host. The file is what `opsctl` installs; devctl and opsctl agree on nothing
else about an app. A file is only ever built from a commit that is on `main`
and carries a release tag, so every file's name says exactly what is inside
it.

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

Build <app> for linux/amd64 and write <app>/dist/<app>-<tag>.tar.xz, the file
deploy copies to a host and opsctl installs. HEAD must be a commit on
origin/main that a tag points at, with no uncommitted changes.
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
agree. The file is named from the tag, but the version the app reports is
compiled into it, so build asks the binary for that too (`<app> --version`) and
checks it against the tag. Nothing downstream compares them again: a host only
ever asks the binary. The one line of output is the path written.

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
- `HEAD` is reachable from `origin/main` and the tag `v0.1.0` points at it.
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

## A developer builds at a commit that is not tagged

Command:

```
$ devctl build crm
```

Output:

```
devctl: no tag points at HEAD (4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a)
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree is clean and `HEAD` is reachable from `origin/main`.
- No tag points at `HEAD`.

Postconditions:

- Nothing has changed.

## A developer builds at a commit that is not on main

Command:

```
$ devctl build crm
```

Output:

```
devctl: HEAD (9f8e7d6c5b4a39281706f5e4d3c2b1a0f9e8d7c6) is not reachable from origin/main
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree is clean and the tag `v0.2.0` points at `HEAD`.
- `HEAD` is not an ancestor of `origin/main`.

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
- The working tree is clean, `HEAD` is on `origin/main`, and a tag points at
  it.
- `crm` compiles, and `crm manifest` emits something other than the committed
  file.

Postconditions:

- Nothing under `crm/dist/` has changed.

## A developer builds an app whose version string is stale

The tag names the file; the binary carries its own. A file whose name and
contents disagree would install as one version and report the other for the
rest of its life, because the host only ever asks the binary.

Command:

```
$ devctl build crm
```

Output:

```
devctl: crm: the tag is v0.1.0 but the binary reports v0.0.9
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `crm/` is a sub-project with a `main` package and `crm/etc/manifest.toml`.
- The working tree is clean, `HEAD` is on `origin/main`, and the tag `v0.1.0`
  points at it.
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

> # github.com/ikigenba/ikigenba/dashboard
> ./main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `dashboard/` is a sub-project with a `main` package and
  `dashboard/etc/manifest.toml`.
- The working tree is clean, `HEAD` is on `origin/main`, and a tag points at
  it.
- `dashboard` does not compile for `linux/amd64`.

Postconditions:

- Nothing under `dashboard/dist/` has changed; an earlier
  `dashboard/dist/dashboard-<tag>.tar.xz`, if any, is as it was.
