# Stories — build

Build turns one app in the checkout into the file that deploy carries to a
host. The file is what `opsctl` installs; devctl and opsctl agree on nothing
else about an app.

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

Build <app> for linux/amd64 and write dist/<app>-<version>.tar.xz, the file
deploy copies to a host and opsctl installs. <version> is the tag on HEAD when
there is one, else HEAD's full SHA. A working tree with uncommitted changes is
refused.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer builds one app

A developer wants the file for one app, to inspect it or to install it on a
host by hand. An app is a top-level directory of the checkout holding
`etc/env.list`; its name is the directory name and its binary is the `main`
package there. The version is the repo-wide tag on `HEAD` when there is one,
else `HEAD`'s full SHA. The one line of output is the path written.

Command:

```
$ devctl build crm
```

Output:

```
dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm/etc/env.list` is in the checkout.
- The working tree has no uncommitted changes, `HEAD` is
  `4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a`, and no tag points at it.
- The Go toolchain can build `crm` for `linux/amd64`.
- The built binary runs on the developer's machine.

Postconditions:

- `dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz` exists in the
  checkout, replacing any earlier file of that name. `dist/` was created if
  it did not exist.
- The tarball holds, relative to its root and with no version anywhere
  inside:
  - `bin/crm`, the static `linux/amd64` binary;
  - `etc/manifest.env`, emitted by running that binary with the argument
    `manifest`;
  - every other file under `crm/etc/`, `env.list` and `nginx.conf` included;
  - `crm/share/` as `share/`, when the app has one.
- Nothing outside `dist/` has changed.

## A developer builds an app at a tagged commit

Command:

```
$ devctl build crm
```

Output:

```
dist/crm-v0.1.0.tar.xz
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm/etc/env.list` is in the checkout.
- The working tree has no uncommitted changes and the tag `v0.1.0` points at
  `HEAD`.
- The Go toolchain can build `crm` for `linux/amd64`.

Postconditions:

- `dist/crm-v0.1.0.tar.xz` exists, with the same contents as a build named by
  SHA.

## A developer builds with uncommitted changes

Nothing is ever built from a tree that differs from its commit, so the file
name always names exactly what is inside it.

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

- `crm/etc/env.list` is in the checkout.
- The working tree has uncommitted changes, whether or not they are under
  `crm/`.

Postconditions:

- Nothing has changed. Nothing under `dist/` was written.

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

- No `bogus/etc/env.list` in the checkout.

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

The compiler's output follows the error line.

Command:

```
$ devctl build dashboard
```

Output:

```
devctl: build dashboard: exit status 1

# github.com/ikigenba/ikigenba/dashboard
./main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `dashboard/etc/env.list` is in the checkout.
- `dashboard` does not compile for `linux/amd64`.

Postconditions:

- Nothing under `dist/` has changed; an earlier
  `dist/dashboard-<version>.tar.xz`, if any, is as it was.
