# Stories — up

Bringing a worktree's sandbox up and asking where it answers: `up` and `url`. Both act on this worktree's sandbox. They find the worktree with `git rev-parse --show-toplevel` from the current directory, so any subdirectory of the checkout works. The sandbox's name is the basename of that toplevel, lowercased, with every run of characters other than `a-z` and `0-9` turned into one `-` and any leading or trailing `-` dropped: `wip` stays `wip`, `Feature_X` becomes `feature-x`. Sandboxes are recorded in a registry at `$XDG_STATE_HOME/ikigenba/sandbox/registry.json`, one entry per sandbox holding its name, its port, its worktree path, and the apps of its last `up` that got as far as starting units (the default app marked among them); a sandbox is known while the registry holds its name. `url` prints from that list of apps, so it survives `down`. Its data lives in `$XDG_STATE_HOME/ikigenba/sandbox/<name>/`, and each app's working directory, where its `state/` lives, is `<data>/apps/<app>/`. An unset, empty or relative `XDG_STATE_HOME` or `XDG_CONFIG_HOME` means `~/.local/state` or `~/.config`. The first `up` of a sandbox takes the lowest port from `7400` to `7499` that no registry entry holds and records it; the sandbox keeps that port until `wipe`. The registry entry is written when that port is taken, and stays even if a later step of the same `up` fails.

An app is an immediate subdirectory of the checkout holding `etc/manifest.toml`; its directory name is the app's name and its `main` package is `cmd/<app>/`. `up` runs in a fixed order: first it checks, in this order, the worktree, the name, that the registry does not give the name to another worktree, the apps (that there are some, and that one of them is `auth`), every manifest, and the secrets each manifest declares (the manifest and secrets checks are told in the apps group); last it takes a port, if the sandbox has none yet. Then it builds every app in name order with `go build` from the worktree as it is, uncommitted edits included, then it writes the generated files and units, runs `systemctl --user daemon-reload`, and starts or restarts the units. A check that fails changes nothing: a sandbox not yet known gets no registry entry. A build that fails writes no unit or generated file and starts nothing; a first `up` that fails there keeps the registry entry and port it has just been given, with no apps recorded, and a sandbox that was already up keeps running its previous build. A step after the builds that fails leaves what it had done so far. Units go in `$XDG_CONFIG_HOME/systemd/user/`, named `sandbox-<name>-<app>.socket` and `sandbox-<name>-<app>.service` per app and `sandbox-<name>-nginx.service` for the sandbox's nginx; they are never enabled, so nothing starts at login. A sandbox is up while `sandbox-<name>-nginx.service` is active and down otherwise. `up` takes the sandbox's lock, so a second `up` (or `down`, `wipe`, `token set`) on the same sandbox waits for it to finish; ports are handed out one sandbox at a time. Both commands print the same text: one line per app in name order, the app's name padded to the longest name plus two spaces, then its URL; the app whose manifest sets `default = true` gets a second line with the sandbox's bare URL directly after its own.

The examples use the worktree `/home/me/src/ikigenba/wip` (sandbox `wip`), the developer `me` with home `/home/me`, and a checkout holding two apps, `auth` and `dummy`:

`auth/etc/manifest.toml`:

```toml
app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
```

`dummy/etc/manifest.toml`:

```toml
app = "dummy"
description = "Demo widgets to list and create"
default = false
mcp = true
secrets = []
```

## A developer asks what `up` can do

Help reads nothing and needs no git checkout.

Command:

```
$ sandbox up --help
```

```
$ sandbox up -h
```

Output:

```
Usage: sandbox up

Build every app in this worktree and start its sandbox, or redeploy it if it
is already up. Prints one URL per app.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer asks what `url` can do

Command:

```
$ sandbox url --help
```

```
$ sandbox url -h
```

Output:

```
Usage: sandbox url

Print the URLs of this worktree's sandbox, as the last 'sandbox up' printed
them.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer brings a worktree's sandbox up for the first time

A developer, or an agent working in the worktree, wants the whole suite running from this worktree as it is now. The sandbox is new, so it is given the first free port, recorded, and everything is built, written and started. Each URL answers through the sandbox's nginx on `127.0.0.1:7400`; browsers and recent curl resolve every `*.localhost` name to loopback with no DNS.

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `/home/me/src/ikigenba/wip` is the top level of a git worktree whose checkout holds `auth/etc/manifest.toml` and `dummy/etc/manifest.toml` as above, and each app's `cmd/<app>/` builds.
- `/home/me/.config/ikigenba/sandbox/secrets.toml` holds a non-empty `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` under `[auth]`.
- The registry does not exist, or holds no entry.
- `git`, `go` and `nginx` are on `PATH`, and the developer's systemd user manager is running.
- The effective user is `me`; `XDG_CONFIG_HOME` and `XDG_STATE_HOME` are unset.

Postconditions:

- `/home/me/.local/state/ikigenba/sandbox/registry.json` holds an entry for `wip` with port `7400` and worktree `/home/me/src/ikigenba/wip`.
- `/home/me/.local/state/ikigenba/sandbox/wip/` exists and holds the built binaries and generated files, with `apps/auth/` and `apps/dummy/` as the apps' working directories.
- `/home/me/.config/systemd/user/` holds `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket`, `sandbox-wip-dummy.service` and `sandbox-wip-nginx.service`; none is enabled.
- Every one of those units is active, so the sandbox is up. nginx listens on `127.0.0.1:7400` and routes each app's host to its socket, as the routing group tells; each app's service runs with the environment and services file the apps group tells.
- Each app's socket is `/run/user/<uid>/sandbox/7400/<app>.sock`, where `<uid>` is the developer's user id, bound by systemd in the developer's own runtime directory and kept out of the data directory because a Unix socket path may hold at most 108 bytes; it is the same whatever `XDG_RUNTIME_DIR` holds.
- Nothing was written under `/etc`, `/opt` or `/var`, nor under `/run` outside `/run/user/<uid>/`, and nothing in the worktree changed.

## A developer runs up from deep inside the worktree

The sandbox belongs to the worktree, not the directory; `up` from any subdirectory is the same `up`.

Command:

```
$ cd /home/me/src/ikigenba/wip/dummy/cmd/dummy && sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the first `up`, with the current directory `/home/me/src/ikigenba/wip/dummy/cmd/dummy`.

Postconditions:

- As for the first `up`: the sandbox is `wip`, its port `7400`, its worktree `/home/me/src/ikigenba/wip`. Nothing was written under the current directory.

## A developer brings up a second worktree beside the first

Each worktree is its own sandbox with its own port, units and data, so the two run side by side without touching each other.

Command:

```
$ cd /home/me/src/ikigenba/other && sandbox up
```

Output:

```
auth   http://auth.other.localhost:7401
dummy  http://dummy.other.localhost:7401
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `/home/me/src/ikigenba/other` is the top level of a second worktree of the same repository, holding the same two apps.
- The registry holds `wip` with port `7400` and no other entry; `wip` is up.

Postconditions:

- The registry holds `other` with port `7401` and worktree `/home/me/src/ikigenba/other`, beside the unchanged `wip` entry.
- `/home/me/.local/state/ikigenba/sandbox/other/` and the units `sandbox-other-auth.socket`, `sandbox-other-auth.service`, `sandbox-other-dummy.socket`, `sandbox-other-dummy.service` and `sandbox-other-nginx.service` exist, and every one is active.
- Nothing of `wip` has changed; it is still up on `7400`.

## A developer keeps their state somewhere other than `~/.local/state`

The registry and every sandbox's data live under `$XDG_STATE_HOME/ikigenba/sandbox/`. A developer who sets `XDG_STATE_HOME` to an absolute path finds both there, and nothing under `~/.local/state`.

Command:

```
$ cd /home/me/src/ikigenba/wip && XDG_STATE_HOME=/home/me/state sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the first `up`, except that `XDG_STATE_HOME` is `/home/me/state`.
- `/home/me/state/ikigenba/sandbox/registry.json` does not exist, or holds no entry.

Postconditions:

- `/home/me/state/ikigenba/sandbox/registry.json` holds an entry for `wip` with port `7400` and worktree `/home/me/src/ikigenba/wip`.
- `/home/me/state/ikigenba/sandbox/wip/` holds the built binaries and generated files, with `apps/auth/` and `apps/dummy/` as the apps' working directories.
- Nothing under `/home/me/.local/state/ikigenba/` was read or written.
- The units are in `/home/me/.config/systemd/user/` and active, as for the first `up`.

## A developer redeploys a running sandbox after editing code

The developer has changed code in the worktree and runs `up` again. Every app is rebuilt from the worktree as it is now and every generated file rewritten; each app's `.service` is restarted and nginx reloaded, while each app's `.socket` stays up and keeps holding the socket. A request that arrives while an app restarts waits in its socket and is answered by the new build; none is refused.

Command:

```
$ sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is known with port `7400` and is up; the worktree has changed since its last `up`.
- A client requests `http://dummy.wip.localhost:7400/` while the `up` runs.

Postconditions:

- `sandbox-wip-auth.service` and `sandbox-wip-dummy.service` run the new builds. `sandbox-wip-auth.socket` and `sandbox-wip-dummy.socket` stayed active throughout, and nginx was reloaded, not stopped.
- The client's request was answered, not refused.
- The registry entry, the port and each app's `apps/<app>/` directory, its `state/` included, are as they were.

## A developer brings up a sandbox after adding an app

An app added to the checkout since the last `up` joins the running sandbox: it is built, its units are written and started, and nginx and the services file gain it.

Command:

```
$ sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is up on `7400` from an `up` run when the checkout held only `auth`.
- `dummy/etc/manifest.toml` and `dummy/cmd/dummy/` have since been added to the worktree.

Postconditions:

- `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service` exist in `/home/me/.config/systemd/user/` and are active; `apps/dummy/` exists in the sandbox's data.
- nginx routes `dummy.wip.localhost:7400` to dummy, and the services file lists dummy beside auth.
- auth was redeployed as in any re-`up`.

## A developer brings up a sandbox after removing an app

An app gone from the checkout since the last `up` leaves the running sandbox: its units are stopped and removed, and it drops out of nginx and the services file. Its working directory, with its `state/`, stays until `wipe`, so bringing the app back finds its data where it left it.

Command:

```
$ sandbox up
```

Output:

```
auth  http://auth.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is up on `7400` from an `up` run when the checkout held `auth` and `dummy`.
- `dummy/` has since been removed from the worktree.

Postconditions:

- `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service` are stopped and no longer exist in `/home/me/.config/systemd/user/`.
- nginx no longer routes `dummy.wip.localhost:7400`, and the services file no longer lists dummy.
- `apps/dummy/` is still in the sandbox's data, unchanged.
- `sandbox url` now prints only the auth line, padded to `auth` plus two spaces.

## A developer brings a stopped sandbox back up

A sandbox that is known but down, after `sandbox down`, a reboot or a logout, comes back on the port it was given, with the data it had.

Command:

```
$ sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- The registry holds `wip` with port `7400` and worktree `/home/me/src/ikigenba/wip`.
- `sandbox-wip-nginx.service` is not active, so `wip` is down.
- The registry also holds `other` with port `7401`.

Postconditions:

- `wip` is up on `7400`, every one of its units active. The registry is unchanged.
- Each app's `apps/<app>/` directory, its `state/` included, and any stored token are as they were.

## A developer brings up a sandbox that has a default app

The app whose manifest sets `default = true` also answers at the sandbox's bare name, so its URL is printed twice: its own line, then the bare URL on a line of its own directly after.

`dummy/etc/manifest.toml`:

```toml
app = "dummy"
description = "Demo widgets to list and create"
default = true
mcp = true
secrets = []
```

Command:

```
$ sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
dummy  http://wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- As for the first `up`, with the current directory inside `/home/me/src/ikigenba/wip` and dummy's manifest as above.

Postconditions:

- As for the first `up`; nginx also sends `wip.localhost:7400` to dummy.

## Two agents bring up the same sandbox at once

Two agents working in the same worktree each run `up` at nearly the same moment. They never deploy over each other: the second waits until the first has finished, then runs a full `up` of its own, a redeploy of what the first started.

Command:

```
$ sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0 for each. Each prints the text on its own stdout; stderr is empty for each.

Preconditions:

- Two processes run `sandbox up` in `/home/me/src/ikigenba/wip` at once.
- The other preconditions of the first `up`, or of a redeploy, hold.

Postconditions:

- The second `up` began only after the first had finished. `wip` is up on `7400` from the second `up`'s builds.
- The registry holds one entry for `wip`.

## Two agents bring up two new worktrees at once

Two worktrees' first `up`s run at the same moment. Ports are handed out one sandbox at a time, so the two never share a port: whichever reaches the registry first takes `7400`, the other `7401`. Each prints its own sandbox's URLs with the port it got. The output below is for the case where `wip` took its port first; had `other` gone first, the ports would be swapped.

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

```
$ cd /home/me/src/ikigenba/other && sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

```
auth   http://auth.other.localhost:7401
dummy  http://dummy.other.localhost:7401
```

Exits 0 for each. Each prints the text on its own stdout; stderr is empty for each.

Preconditions:

- The registry does not exist, or holds no entry.
- Both worktrees hold `auth` and `dummy`, and both `up`s run at once.

Postconditions:

- The registry holds `wip` and `other`, one with port `7400` and the other with `7401`, each with its own worktree path.
- Both sandboxes are up, each nginx on its own port.

## A developer asks where the sandbox answers

An agent or a developer that has lost the `up` output asks for it again. `url` prints exactly what the last `up` printed: the apps of that `up`, even if the checkout has gained or lost an app since. It builds and starts nothing.

Command:

```
$ sandbox url
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is known with port `7400` and is up from an `up` of `auth` and `dummy`.

Postconditions:

- Nothing has changed.

## A developer asks for the URLs of a stopped sandbox

A down sandbox answers at none of its URLs, so `url` prints none and says how to start it.

Command:

```
$ sandbox url
```

Output:

```
sandbox: sandbox 'wip' is down

run 'sandbox up' to start it
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is known with port `7400`, and `sandbox-wip-nginx.service` is not active.

Postconditions:

- Nothing has changed.

## A developer asks for the URLs of a sandbox that was never brought up

Command:

```
$ sandbox url
```

Output:

```
sandbox: no sandbox 'wip'

run 'sandbox up' to create it
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- The registry does not exist, or holds no entry for `wip`.

Postconditions:

- Nothing has changed. No registry entry was created.

## A developer passes up an argument

Command:

```
$ sandbox up now
```

Output:

```
sandbox: up takes no arguments

see 'sandbox up --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer passes up an unknown option

Command:

```
$ sandbox up --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox up --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer passes url an argument

Command:

```
$ sandbox url dummy
```

Output:

```
sandbox: url takes no arguments

see 'sandbox url --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer passes url an unknown option

Command:

```
$ sandbox url --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox url --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer runs up or url outside a git checkout

Without a checkout there is no worktree to name the sandbox or to build, so the command stops before it looks at anything else. The path named is the current directory. `url` refuses the same way.

Command:

```
$ cd /home/me && sandbox up
```

```
$ cd /home/me && sandbox url
```

Output:

```
sandbox: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `/home/me` is not inside a git working tree.

Postconditions:

- Nothing has changed. Nothing was built, and the registry was not read or written.

## A developer runs up in a worktree whose name gives no sandbox name

The sandbox's name must be a DNS label, since every app answers at `<app>.<name>.localhost`. A worktree basename with no letter or digit normalises to nothing, and one longer than 63 characters after normalising is too long; either is refused.

Command:

```
$ cd /home/me/src/ikigenba/___ && sandbox up
```

Output:

```
sandbox: worktree '___' does not give a usable sandbox name

a name needs a letter or digit and at most 63 characters
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `/home/me/src/ikigenba/___` is the top level of a git worktree.

Postconditions:

- Nothing has changed. Nothing was built, and the registry was not written.

## A developer runs up in a worktree whose sandbox name another worktree holds

Two worktrees with the same basename would be the same sandbox. The registry records which worktree a sandbox belongs to, and `up` from any other worktree refuses, whether or not the recorded worktree still exists. `url` refuses with the same text, since the sandbox is not this worktree's.

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

```
$ cd /home/me/src/ikigenba/wip && sandbox url
```

Output:

```
sandbox: sandbox 'wip' belongs to another worktree: /home/me/old/wip

rename this worktree, or wipe that sandbox with 'sandbox wipe wip' once it is down
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `/home/me/src/ikigenba/wip` is the top level of a git worktree.
- The registry holds `wip` with worktree `/home/me/old/wip`.

Postconditions:

- Nothing has changed. The `wip` sandbox of `/home/me/old/wip`, up or down, is untouched.

## A developer brings up a new sandbox when every port is taken

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

Output:

```
sandbox: no free port: every port from 7400 to 7499 belongs to a sandbox

run 'sandbox ls' and wipe a sandbox you no longer need
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The registry holds no entry for `wip`, and holds a hundred other sandboxes with ports `7400` through `7499`.

Postconditions:

- Nothing has changed. Nothing was built, and no registry entry was created.

## A developer runs up in a checkout with no apps

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

Output:

```
sandbox: no apps in /home/me/src/ikigenba/wip

an app is a directory holding etc/manifest.toml
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `/home/me/src/ikigenba/wip` is the top level of a git worktree, and no immediate subdirectory of it holds `etc/manifest.toml`.
- The registry holds no entry for `wip`.

Postconditions:

- Nothing has changed. Nothing was built, and no registry entry was created: the check for apps comes before a port is taken.

## A developer runs up in a checkout with no auth app

The suite's apps learn who their user is only from the identity auth's check gives them, and answer 500 without one, so a sandbox without `auth` would serve nothing. `up` refuses a checkout with no app named `auth`. A sandbox that is already up stays as it was, running its previous build.

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

Output:

```
sandbox: no app named 'auth' in /home/me/src/ikigenba/wip

every other app needs auth to tell it who its user is
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `/home/me/src/ikigenba/wip` is the top level of a git worktree whose checkout holds `dummy/etc/manifest.toml` as above and no `auth/etc/manifest.toml`.
- The registry holds no entry for `wip`.

Postconditions:

- Nothing has changed. Nothing was built, and no registry entry was created: the check for `auth` comes before a port is taken.

## A developer brings up a sandbox whose code does not compile

Apps are built in name order and the builds stop at the first that fails; go's output is quoted so the developer sees why. No unit or generated file is written and nothing is started, so a sandbox that was already up keeps running its previous build.

Command:

```
$ sandbox up
```

Output:

```
sandbox: build dummy: exit status 1

> # github.com/ikigenba/ikigenba/dummy/cmd/dummy
> cmd/dummy/main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is known with port `7400` and is up.
- `auth` builds; `dummy/cmd/dummy/main.go` does not compile.

Postconditions:

- `wip` is still up, every unit running its previous build. No unit was started, restarted or stopped, and nginx was not reloaded.
- No unit file, generated file or registry entry changed.

## A developer's first up of a sandbox fails to build

Every check passed, so the sandbox was given its port and recorded before the builds began. The build then fails, and the registry entry stays: the sandbox is known, holds `7400`, and has no apps recorded, so the next `up` reuses the port. Nothing else of the sandbox exists yet.

Command:

```
$ cd /home/me/src/ikigenba/wip && sandbox up
```

Output:

```
sandbox: build dummy: exit status 1

> # github.com/ikigenba/ikigenba/dummy/cmd/dummy
> cmd/dummy/main.go:41:2: undefined: render
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The preconditions of the first `up` hold, except that `dummy/cmd/dummy/main.go` does not compile; `auth` builds.
- The registry does not exist, or holds no entry.

Postconditions:

- `/home/me/.local/state/ikigenba/sandbox/registry.json` holds an entry for `wip` with port `7400`, worktree `/home/me/src/ikigenba/wip`, and no apps.
- No unit file or generated file was written for `wip`, and no unit was started; `wip` is down.
- `sandbox ls` lists `wip` on `7400` as `down`.

## A developer brings up a sandbox whose app fails to start

Everything was built and written, but one app's service would not start: the app exited, say, on bad configuration. systemctl's output is quoted, and the detail names the command that shows the app's journal.

Command:

```
$ sandbox up
```

Output:

```
sandbox: start sandbox-wip-dummy.service: exit status 1

> Job for sandbox-wip-dummy.service failed because the control process exited with error code.
> See "systemctl --user status sandbox-wip-dummy.service" and "journalctl --user -xeu sandbox-wip-dummy.service" for details.

run 'sandbox logs dummy' for its journal
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- The preconditions of the first `up` hold, except that dummy's service exits before it is ready.

Postconditions:

- The registry entry for `wip`, the generated files and the unit files are written.
- Units started before the failure stay started. `sandbox-wip-dummy.service` is failed, and `sandbox status` shows `dummy failed`.

## A developer brings up a sandbox whose port is in use

The sandbox's port is recorded, but something else on the machine can still be listening on it. nginx cannot bind, its unit fails, and the sandbox reads down.

Command:

```
$ sandbox up
```

Output:

```
sandbox: start sandbox-wip-nginx.service: exit status 1

> Job for sandbox-wip-nginx.service failed because the control process exited with error code.
> See "systemctl --user status sandbox-wip-nginx.service" and "journalctl --user -xeu sandbox-wip-nginx.service" for details.

run 'sandbox logs' for the journal
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is known with port `7400` and is down.
- Another process, not a sandbox, listens on `127.0.0.1:7400`.

Postconditions:

- `sandbox-wip-nginx.service` is failed, so `wip` is down, and `sandbox status` shows `nginx failed`.
- Units started before the failure stay started. The registry entry, the port, the generated files and the unit files stay.

## A developer runs up with no systemd user manager

`up` reaches systemd only through `systemctl --user`. With no user manager to talk to, as in a shell with no login session, the first systemctl run fails and its output is quoted.

Command:

```
$ sandbox up
```

Output:

```
sandbox: systemctl --user daemon-reload: exit status 1

> Failed to connect to bus: No medium found
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside `/home/me/src/ikigenba/wip`.
- `wip` is known with port `7400` and is down.
- No systemd user manager is reachable for `me`.

Postconditions:

- Every app was built, and the generated files and unit files were written.
- No unit was started; `wip` is still down. The registry is unchanged.
