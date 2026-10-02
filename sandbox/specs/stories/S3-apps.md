# Stories — apps

What `up` does with the checkout's apps: which directories it takes for apps, how it builds them, where each one runs, what each one is given, and every manifest or secret it refuses. The stories share one setting unless they say otherwise: the worktree is `/home/me/src/ikigenba/wip`, its sandbox is `wip` on port `7400`, its data is under `/home/me/.local/state/ikigenba/sandbox/wip/`, the developer is `me` with `XDG_CONFIG_HOME` and `XDG_STATE_HOME` unset, and the checkout holds two apps, `auth` and `dummy`, whose manifests are below. An app is an immediate subdirectory of the checkout root holding `etc/manifest.toml`; its directory name is the app's name, and its `main` package is `cmd/<app>/` inside it. Each app runs as a socket unit and a service unit, `sandbox-wip-<app>.socket` and `sandbox-wip-<app>.service`. Manifests are checked in app-name order, and `up` reports the first fault it finds and stops there. Every refusal in this group is found while `up` checks the checkout, before a port is taken and before anything is built, so nothing has changed: no app is built, no file, unit or registry entry is written (a sandbox not yet known stays unknown), nothing is started or restarted, and a sandbox that is already up keeps running its previous build unchanged. The secrets file is read only when some app's manifest lists at least one secret.

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

The secrets file, `/home/me/.config/ikigenba/sandbox/secrets.toml`, is kept by the developer outside the repository and shared by every sandbox, one table per app:

```toml
[auth]
GOOGLE_CLIENT_ID = "1234-abc.apps.googleusercontent.com"
GOOGLE_CLIENT_SECRET = "GOCSPX-example"
```

## A developer brings up every app the checkout holds

The checkout holds more than apps: other sub-projects, documentation, tools. `up` takes exactly the immediate subdirectories of the checkout root that hold `etc/manifest.toml`, and nothing else. A directory without one is not an app, whatever it holds, and a manifest deeper than one level down does not make an app either.

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

- The current directory is `/home/me/src/ikigenba/wip` or any directory below it.
- `auth/etc/manifest.toml` and `dummy/etc/manifest.toml` hold the manifests above, and `auth/cmd/auth/` and `dummy/cmd/dummy/` are `main` packages that build.
- `sandbox/` and `docs/` sit beside them and hold no `etc/manifest.toml`; `tools/widget/etc/manifest.toml` exists, one level deeper than an app.
- The secrets file holds the `[auth]` table above.

Postconditions:

- `auth` and `dummy` were each built with `go build` from their `cmd/<app>/` and are running as `sandbox-wip-auth.service` and `sandbox-wip-dummy.service`, behind `sandbox-wip-auth.socket` and `sandbox-wip-dummy.socket`.
- Nothing was built, started, or routed for `sandbox/`, `docs/`, or `tools/widget/`.

## A developer brings up apps with edits they have not committed

The sandbox runs the checkout as it is on disk. Uncommitted changes, staged or not, and files git does not track are all built in; nothing asks for a clean tree, a commit, or a tag.

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

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/cmd/dummy/main.go` has edits that are not committed, and `dummy/internal/widgets/new.go` is a file git does not track; both build.
- No tag points at `HEAD`.

Postconditions:

- `sandbox-wip-dummy.service` runs a `dummy` built from the files as they are on disk, the uncommitted edits and the untracked file included.
- Nothing in the checkout or in git changed: nothing was committed, stashed, tagged, or reset.

## A developer brings the sandbox up again and the apps keep their data

Each app runs in its own working directory under the sandbox's data, `/home/me/.local/state/ikigenba/sandbox/wip/apps/<app>/`, and keeps what it stores in `state/` there. A manifest's `[database] path`, such as auth's `state/auth.db`, is relative to that directory. Rebuilding and restarting the apps leaves that directory alone.

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
- `wip` is up from an earlier `up`.
- `auth` has written `/home/me/.local/state/ikigenba/sandbox/wip/apps/auth/state/auth.db`, and `dummy` has written files under `/home/me/.local/state/ikigenba/sandbox/wip/apps/dummy/state/`.

Postconditions:

- Both apps were rebuilt and their services restarted.
- `/home/me/.local/state/ikigenba/sandbox/wip/apps/auth/state/auth.db` and every file under `/home/me/.local/state/ikigenba/sandbox/wip/apps/dummy/state/` are exactly as they were.
- Each restarted service's working directory is still its app's directory under `/home/me/.local/state/ikigenba/sandbox/wip/apps/`.

## An app reads the environment the sandbox gives it

An app learns everything it knows about the sandbox from its environment: the sandbox's name, its own public origin, the bare-localhost origin whose every request is sent on to auth (for an app that needs an OAuth redirect Google accepts), where the services file is, and how long it has to drain. These names are the same in every sandbox, and no app needs to know which apps are in the checkout to read them. The app has no `PORT`: it is handed its socket by systemd, as on a host. dummy declares no secrets and no `[env]`, so it gets the sandbox's variables alone.

Command:

```
$ tr '\0' '\n' < /proc/"$(systemctl --user show -p MainPID --value sandbox-wip-dummy.service)"/environ | sort
```

Output: among the lines systemd itself sets, which are not fixed (`LISTEN_FDS`, `LISTEN_FDNAMES`, `LISTEN_PID`, and the user manager's own environment), exactly these lines, where `<services file>` is the absolute path of `wip`'s services file under `/home/me/.local/state/ikigenba/sandbox/wip/`:

```
DRAIN_SECONDS=5
IKIGENBA_CALLBACK_URL=http://localhost:7400
IKIGENBA_PUBLIC_URL=http://dummy.wip.localhost:7400
IKIGENBA_SANDBOX=wip
IKIGENBA_SERVICES=<services file>
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `wip` is up, with `auth` and `dummy`, and `sandbox-wip-dummy.service` is active.

Postconditions:

- Nothing has changed.
- dummy's environment holds no `PORT`, and none of auth's secrets or auth's `[env]` entries.
- dummy's working directory is `/home/me/.local/state/ikigenba/sandbox/wip/apps/dummy/`.
- systemd gives `sandbox-wip-dummy.service` 10 seconds to stop before it kills it, longer than the 5 seconds `DRAIN_SECONDS` gives the app to drain.

## auth reads its secrets and its own settings

An app also gets its manifest's `[env]` entries, and, from the secrets file, the values of the secrets its manifest lists, by name, and no others. A secret is given only to the app whose manifest declares it: a key in an app's table that its manifest does not list, and a table for an app the checkout does not hold, are ignored. Secret values reach the app's environment and nowhere else: no output of `sandbox`, no unit file, and no command line carries them.

Command:

```
$ tr '\0' '\n' < /proc/"$(systemctl --user show -p MainPID --value sandbox-wip-auth.service)"/environ | sort
```

Output: among the lines systemd itself sets, which are not fixed, exactly these lines, where `<services file>` is the same path dummy is given:

```
DRAIN_SECONDS=5
GOOGLE_CLIENT_ID=1234-abc.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=GOCSPX-example
IKIGENBA_CALLBACK_URL=http://localhost:7400
IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400
IKIGENBA_SANDBOX=wip
IKIGENBA_SERVICES=<services file>
WORKSPACE_DOMAIN=michaelgreenly.dev
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `wip` is up, with `auth` and `dummy`, and `sandbox-wip-auth.service` is active.
- The secrets file holds:

  ```toml
  [auth]
  GOOGLE_CLIENT_ID = "1234-abc.apps.googleusercontent.com"
  GOOGLE_CLIENT_SECRET = "GOCSPX-example"
  SIGNING_KEY = "unused"

  [billing]
  STRIPE_KEY = "sk_test_example"
  ```

Postconditions:

- Nothing has changed.
- auth's environment holds no `SIGNING_KEY`, since its manifest does not list it, and no `STRIPE_KEY`, since the checkout holds no `billing`.
- dummy's environment holds none of these secrets.
- auth's working directory is `/home/me/.local/state/ikigenba/sandbox/wip/apps/auth/`, so its database is `/home/me/.local/state/ikigenba/sandbox/wip/apps/auth/state/auth.db`.

## The default app reads its own public origin

Being the default app does not change what an app is told about itself: its `IKIGENBA_PUBLIC_URL` is still its own name's origin, the same as its `url` in the services file, never the sandbox's bare name.

Command:

```
$ tr '\0' '\n' < /proc/"$(systemctl --user show -p MainPID --value sandbox-wip-dummy.service)"/environ | grep '^IKIGENBA_PUBLIC_URL='
```

Output:

```
IKIGENBA_PUBLIC_URL=http://dummy.wip.localhost:7400
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- dummy's manifest sets `default = true`.
- `wip` is up from an `up` run with the checkout in that state, and `sandbox-wip-dummy.service` is active.

Postconditions:

- Nothing has changed.
- No variable in dummy's environment holds the bare URL `http://wip.localhost:7400`.

## An app reads the services file

Every app can find every other app in the sandbox through the file `IKIGENBA_SERVICES` names, written in the platform's published format: one JSON object whose `services` array lists every app of the last `up` in name order, one service to a line. Each service gives its `name`; its `url` in this sandbox; its manifest's `description`, `""` when it has none; its `socket` in this sandbox; `enabled`, always `true`; and `mcp`, `false` when the manifest does not set it. `<socket of auth>` and `<socket of dummy>` are `/run/user/<uid>/sandbox/7400/auth.sock` and `/run/user/<uid>/sandbox/7400/dummy.sock`, where `<uid>` is the developer's user id: sockets live in the developer's runtime directory keyed by the sandbox's port, not under the sandbox's data, because a Unix socket path may hold at most 108 bytes.

Command:

```
$ cat "$IKIGENBA_SERVICES"
```

Output:

```
{
  "services": [
    { "name": "auth", "url": "http://auth.wip.localhost:7400", "description": "", "socket": "<socket of auth>", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "http://dummy.wip.localhost:7400", "description": "Demo widgets to list and create", "socket": "<socket of dummy>", "enabled": true, "mcp": true }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `wip` is up, with `auth` and `dummy`.
- The command runs with an app's environment, where `IKIGENBA_SERVICES` is set as the sandbox sets it.
- Neither `auth/share/icon.svg` nor `dummy/share/icon.svg` exists.

Postconditions:

- Nothing has changed.

## An app reads the services file when an app has an icon and is the default

An app that ships `share/icon.svg` carries an `icon` member last, holding the file's bytes verbatim as a JSON string; an app without one has no `icon` member at all. Being the default app does not change a service's `url`: it is always the app's own name in the sandbox.

Command:

```
$ cat "$IKIGENBA_SERVICES"
```

Output:

```
{
  "services": [
    { "name": "auth", "url": "http://auth.wip.localhost:7400", "description": "", "socket": "<socket of auth>", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "http://dummy.wip.localhost:7400", "description": "Demo widgets to list and create", "socket": "<socket of dummy>", "enabled": true, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n" }
  ]
}
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- dummy's manifest sets `default = true`.
- `dummy/share/icon.svg` holds the one line `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>` and a newline; `auth/share/icon.svg` does not exist.
- `wip` is up from an `up` run with the checkout in that state.
- The command runs with an app's environment, where `IKIGENBA_SERVICES` is set as the sandbox sets it.

Postconditions:

- Nothing has changed.

## A developer keeps their configuration somewhere other than `~/.config`

`up` reads the secrets file from `$XDG_CONFIG_HOME/ikigenba/sandbox/secrets.toml` and writes its units to `$XDG_CONFIG_HOME/systemd/user/`. When `XDG_CONFIG_HOME` is unset, empty, or not an absolute path, both are under `/home/me/.config` instead.

Command:

```
$ XDG_CONFIG_HOME=/home/me/cfg sandbox up
```

Output:

```
auth   http://auth.wip.localhost:7400
dummy  http://dummy.wip.localhost:7400
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `/home/me/cfg/ikigenba/sandbox/secrets.toml` holds the `[auth]` table above.
- `/home/me/.config/ikigenba/sandbox/secrets.toml` does not exist.

Postconditions:

- auth was given the secrets from `/home/me/cfg/ikigenba/sandbox/secrets.toml`; nothing under `/home/me/.config/ikigenba/` was read.
- `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket`, `sandbox-wip-dummy.service`, and `sandbox-wip-nginx.service` are in `/home/me/cfg/systemd/user/`.

## A developer brings up an app whose manifest names a port

On the platform an app never chooses a port, and the sandbox holds the same line: it hands each app its socket.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: 'port' is not allowed; the sandbox gives the app its socket
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` holds the manifest above plus a top-level `port = 8080`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose manifest is not valid TOML

A manifest that is not valid TOML is refused with the TOML parser's own description of the fault, which says the line and varies with the fault.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: line 1 (last key "app"): strings cannot contain newlines
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` is not valid TOML: its first line reads `app = "dummy`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose manifest gives a key the wrong type

A key sandbox reads that holds a value of the wrong type is refused in sandbox's own words, which say what the key must be: `app` and `description` a string, `default` and `mcp` a boolean, `secrets` an array of strings, `env` a table of strings.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: 'mcp' must be a boolean
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` holds the manifest above with `mcp = "yes"` in place of `mcp = true`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose description is not one line

The platform refuses a manifest whose `description` is not one line of text, and the sandbox refuses it too. A `description` is one line of text when it holds no control character (U+0000–U+001F, U+007F): no line break, not even a trailing one, and no tab. An empty `description`, or one of only spaces, is one line. This fault is reported whatever `mcp` is, so an MCP app whose `description` is a line break alone gets this refusal, not the one for an empty description.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: 'description' must be one line of text
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` holds the manifest above with `description = "Demo widgets\nto list and create"` in place of its `description`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an MCP app that does not say what it offers

The platform refuses an app whose manifest sets `mcp = true` but gives no `description` to say what it offers, and the sandbox refuses it too. A `description` that is missing, empty, or only whitespace is empty. An app that does not set `mcp`, or sets `mcp = false`, may leave its `description` out or empty, as auth does. When its manifest has another fault as well, such as a `port`, a key of the wrong type, or a `description` that is not one line, that other fault is the one reported.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: 'mcp' is true but 'description' is empty; an MCP service must say what it offers
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` holds the manifest above without its `description` line; or with `description = ""` or `description = "   "` in its place.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose manifest names another app

An app's name is its directory's name, and its manifest must agree.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: app 'demo' does not match its directory 'dummy'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` sets `app = "demo"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose name the sandbox cannot use

An app's name becomes part of its host name, `<app>.wip.localhost`, and of its unit names, so it must be a DNS label: lowercase letters, digits, and `-`.

Command:

```
$ sandbox up
```

Output:

```
sandbox: 'Dummy_2' is not a usable app name
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `Dummy_2/etc/manifest.toml` exists and sets `app = "Dummy_2"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app named `nginx`

An app named `nginx` would have the unit `sandbox-wip-nginx.service`, which is the sandbox's own nginx, so `nginx` is not a usable app name, even though it is a valid DNS label.

Command:

```
$ sandbox up
```

Output:

```
sandbox: 'nginx' is not a usable app name
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `nginx/etc/manifest.toml` exists and sets `app = "nginx"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up two apps that both claim to be the default

Only one app can answer at the sandbox's bare name. Every app claiming it is named, in name order.

Command:

```
$ sandbox up
```

Output:

```
sandbox: more than one default app: auth, dummy
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- Both `auth/etc/manifest.toml` and `dummy/etc/manifest.toml` set `default = true`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose manifest sets a variable the sandbox owns

The sandbox sets `DRAIN_SECONDS`, every name beginning `IKIGENBA_`, and systemd sets `LISTEN_FDS`, `LISTEN_FDNAMES`, and `LISTEN_PID`. A manifest may not name any of them, either as an `[env]` key or in `secrets`, and the refusal names the variable.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: 'IKIGENBA_SERVICES' is set by the sandbox
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` holds the manifest above plus an `[env]` table setting `IKIGENBA_SERVICES = "/tmp/services.json"`, or lists `"IKIGENBA_SERVICES"` in `secrets`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose setting holds a line break

The platform refuses a setting whose value holds a NUL, a carriage return or a line feed, and the sandbox refuses those too, along with the characters systemd cannot read from an env file: a byte-order mark (U+FEFF) and a Unicode noncharacter. A line break anywhere in the value is refused, a trailing one included. The refusal names the `[env]` key, never its value.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: etc/manifest.toml: 'BANNER' holds a character an env file cannot hold
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/etc/manifest.toml` holds the manifest above plus an `[env]` table setting `BANNER = "line one\nline two"`, or `BANNER = "line one\r"`, or `BANNER = "a\u0000b"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose icon is not a regular file

The platform publishes only an icon that is a regular file, and the sandbox holds the same line. Anything else at `share/icon.svg` is refused: a directory, a symbolic link (which is never followed, so a link to a good SVG and a link to nothing are both refused), a socket or a named pipe. An app with nothing at `share/icon.svg` has no icon and is not refused. An app's icon is checked last, after its manifest has passed, and in this order: that it is a regular file, that it can be read, that it is at most 64 KiB, that it is valid UTF-8, and that it is an SVG image. Only the first fault is reported.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: share/icon.svg is not a regular file
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/share/icon.svg` is a directory; or it is a symbolic link, either to `../../auth/share/logo.svg`, a regular file that exists and is a valid SVG image of at most 64 KiB, or to a path that does not exist.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose icon cannot be read

An icon that is a regular file but cannot be read is refused with the operating system's reason.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: share/icon.svg: permission denied
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/share/icon.svg` is a regular file holding a valid SVG image of at most 64 KiB, with mode 000.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose icon is larger than 64 KiB

Every app reads the services file, which carries every icon in it, so the platform holds each icon to 64 KiB (65,536 bytes), and the sandbox does too. An icon of exactly 65,536 bytes is accepted. Size is judged before content, so an icon that is both too large and not an SVG image gets this refusal.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: share/icon.svg is larger than 64 KiB
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/share/icon.svg` is a regular file of 65,537 bytes or more: an SVG image, or anything else.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose icon is not UTF-8

The platform refuses to publish an icon whose bytes are not valid UTF-8, and the sandbox refuses it too: the icon goes into the services file verbatim as a JSON string. A leading UTF-8 byte-order mark is valid UTF-8.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: share/icon.svg is not valid UTF-8
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/share/icon.svg` is an SVG saved in Latin-1, at most 64 KiB: its `<title>` holds `Café`, the `é` the single byte 0xE9.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up an app whose icon is not an SVG image

The platform refuses an icon that is not an SVG image, and the sandbox refuses it too. An icon is an SVG image when, after one leading UTF-8 byte-order mark is set aside, it is one well-formed XML document, read strictly: its one top-level element is named `svg`, in any namespace or none; no element carries the same attribute twice; and outside that element there is nothing but whitespace, comments, processing instructions (an XML declaration among them) and directives such as a `DOCTYPE`. So an empty file, plain text, an XML declaration naming an encoding other than UTF-8, an unclosed tag, an undefined entity, a duplicate attribute, a root element other than `svg`, a second top-level element, and text after the root element are each refused this way, while an SVG image that begins with a byte-order mark is accepted.

Command:

```
$ sandbox up
```

Output:

```
sandbox: dummy: share/icon.svg is not an SVG image
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy/share/icon.svg` is a regular file of at most 64 KiB, valid UTF-8, that is not an SVG image: the plain text `widget icon`, say, or `<svg xmlns="http://www.w3.org/2000/svg"><circle r="1" r="2"/></svg>`, whose `circle` carries `r` twice.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.

## A developer brings up two apps whose manifests are both faulty

Manifests are checked in app-name order, and `up` stops at the first fault: only that one is reported. Once it is fixed, the next `up` reports the next.

Command:

```
$ sandbox up
```

Output:

```
sandbox: auth: etc/manifest.toml: 'port' is not allowed; the sandbox gives the app its socket
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `auth/etc/manifest.toml` holds the manifest above plus a top-level `port = 8080`.
- `dummy/etc/manifest.toml` sets `app = "demo"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.
- Nothing was said about dummy's manifest.

## A developer brings up an app whose secrets they have not provided

Every secret a manifest lists must have a value. A secret the file lacks, or sets to the empty string, is missing, and a secrets file that does not exist is missing every secret. `up` names every missing secret at once, one line each, sorted by app and then by name, so the developer can fix them all before trying again. It never prints a secret's value. Had only `GOOGLE_CLIENT_SECRET` been missing, only its line would follow.

Command:

```
$ sandbox up
```

Output:

```
sandbox: secrets missing from /home/me/.config/ikigenba/sandbox/secrets.toml

auth GOOGLE_CLIENT_ID
auth GOOGLE_CLIENT_SECRET
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- auth's manifest lists `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in `secrets`.
- `/home/me/.config/ikigenba/sandbox/secrets.toml` does not exist; or it has no `[auth]` table; or its `[auth]` table sets neither key, or sets both to `""`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.
- The secrets file was not created or changed.

## A developer brings up the sandbox with a secrets file that is not valid TOML

A secrets file that is not valid TOML is refused with only the line where the fault was found, never the parser's own description of it, which can quote the text near the fault, and that text may be a secret. No value from the file is printed.

Command:

```
$ sandbox up
```

Output:

```
sandbox: /home/me/.config/ikigenba/sandbox/secrets.toml: not valid TOML at line 2
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `/home/me/.config/ikigenba/sandbox/secrets.toml` is not valid TOML: its first line reads `[auth` and its second `GOOGLE_CLIENT_ID = "1234-abc.apps.googleusercontent.com"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.
- The secrets file was not changed.

## A developer brings up the sandbox with a secret that holds a line break

A secret pasted with its line ending is easy to miss. The platform refuses a secret whose value holds a NUL, a carriage return or a line feed, and the sandbox refuses those too, along with a byte-order mark (U+FEFF) and a Unicode noncharacter, which systemd cannot read from an env file. A line break anywhere in the value is refused, a trailing one included. The refusal names the app and the secret, never the value. It is made only for a secret some app's manifest lists; a key the manifests do not list is ignored whatever it holds.

Command:

```
$ sandbox up
```

Output:

```
sandbox: /home/me/.config/ikigenba/sandbox/secrets.toml: 'auth.GOOGLE_CLIENT_SECRET' holds a character an env file cannot hold
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The secrets file's `[auth]` table sets `GOOGLE_CLIENT_ID` as above and `GOOGLE_CLIENT_SECRET = "GOCSPX-example\n"`, or `GOOGLE_CLIENT_SECRET = "GOCSPX-example\r\n"`.
- `wip` is up from an earlier `up`, or is not yet known.

Postconditions:

- Nothing has changed: no app was built, no file, unit or registry entry was written, nothing was started or restarted. If `wip` was up, it still runs its previous build; if it was not yet known, it still is not.
- The secrets file was not changed.
- Nothing `sandbox` printed holds `GOCSPX-example`.

## A developer brings up apps that declare no secrets

When no app's manifest lists a secret, `up` does not read the secrets file at all, so it does not matter whether the file exists or whether it parses.

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

- The current directory is `/home/me/src/ikigenba/wip`.
- The checkout holds `auth` and `dummy`: auth's manifest is the one above with `secrets = []` in place of its `secrets` line, dummy's is the one above (`secrets = []`), and both `auth/cmd/auth/` and `dummy/cmd/dummy/` build.
- `/home/me/.config/ikigenba/sandbox/secrets.toml` does not exist; or it is not valid TOML, its first line reading `[auth`.
- The other preconditions of a first `up` hold.

Postconditions:

- `wip` is up with `auth` and `dummy`.
- The secrets file was not read, created or changed.
