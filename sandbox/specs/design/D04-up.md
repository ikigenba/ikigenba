# D04-up

This document owns what `up` and `url` do once D02 has parsed a well-formed command line: the order of `up`'s checks, the refusal of a checkout without `auth`, the builds and how a new build replaces a running one, what `up` writes and when, the app unit files, the systemd runs that bring the sandbox up or redeploy it, the apps that join or leave, what both commands print, and every way `up` fails after its checks. Finding the sandbox, its name, the layout, the registry, the locks, the port, the unit names, the socket paths, the origins and the up/down rule are D03's; discovery, the manifest and secrets checks, the env file and the services file are D05's; the nginx configuration and the nginx unit are D06's; the help texts and the shared diagnostic shapes are D02's. This document cites them and restates none.

`up` checks everything before it changes anything: the worktree checks D03 orders, then D05's discovery, then that one of the apps is `auth`, then D05's manifest and secrets checks, then D03's unit-clash check, and last, for a sandbox the registry does not know yet, a port. Every other app learns who its user is only from the identity auth's check gives it, and answers 500 without one, so a sandbox without `auth` would serve nothing; `up` refuses such a checkout, with a diagnostic that says why, before it checks a single manifest. A refusal at any check leaves no trace beyond a lock file. Once a port is given, the registry entry exists with no apps recorded, and it stays whatever happens next.

Then `up` builds every app, in app-name order, with `go build -o` into the staging directory `<data>/build`, never straight into `<data>/bin`. A failed `go build` leaves its own output untouched, but the apps built before it would already have landed: staging keeps every earlier successful build out of `<data>/bin` until the last app has built, so a failure anywhere leaves a running sandbox exactly as it was. Only then does `up` move each staged binary over `<data>/bin/<app>` by rename, which gives the path a new file while a running service keeps executing the old one until it restarts. A failed build stops the builds, removes the staging directory, and leaves everything else as it was.

After the builds, `up` first takes apart the apps the registry records but the checkout no longer holds: it stops each one's socket and service, removes its unit files, binary and env file, and keeps its app directory with its `state/`. Only then does it record the discovered apps in the registry, so unit files on disk always belong to recorded apps (D03), and then it writes everything a running sandbox reads: binaries, app directories, env files, the services file, nginx's configuration and unit, and each app's socket and service units. Every path in those unit files is written in the form D03 gives its position: the binary unit-quoted as an executable, the app directory, env file and socket path as unit path values. systemd refuses an executable path holding `"`, `'` or `\`, however it is written, and ignores a setting that is not UTF-8 clean, so D03 refuses a state root holding any of those before `up` changes anything. One `systemctl --user daemon-reload` makes systemd read the units, and the sandbox's state, read from systemd as D03 says, picks what follows.

Every socket is started first, in app-name order, so every app's socket is listening before any app runs and an app that reaches a peer while starting waits in that peer's socket instead of being refused. Then every service is restarted, in app-name order. `restart` starts a service that is not running and replaces one that is, so a service still running from an earlier `up` whose nginx failed is replaced by the new build rather than left alone. Each service inherits its socket from systemd and is `Type=notify`, so systemctl returns only once the app reports ready, or with the failure when it exits first. A socket that is already listening is untouched by its start, so during a redeploy requests queue in the socket while the service restarts and are answered by the new build. nginx comes last: started when the sandbox was down, so the sandbox reads up only once every app is ready; reloaded when it was up, so it never stops listening. The first run that fails ends `up`, leaving what it had done.

`url` prints what the last `up` printed, from the apps the registry records, and refuses a sandbox that is down.

## REQUIREMENTS

### Program runs

- R-Z3G9-8CGU: A build run of an app MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `go`, whose `Args` are exactly `build`, `-o`, the app's staged build path (D03 R-F5OI-EWQ2) and `./cmd/<app>`, naming the app's main package (D05 R-UFFF-9SPP) relative to `Dir`, and whose `Dir` is the app's source directory (D05 R-AUCL-BZBE).

- R-XXC9-Z9PP: A start run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `start` and the unit's name, and whose `Dir` is `/`.

- R-XYK6-D1GE: A restart run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `restart` and the unit's name, and whose `Dir` is `/`.

- R-XZS2-QT73: A reload run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `reload` and the unit's name, and whose `Dir` is `/`.

- R-Y0ZZ-4KXS: `up` and `url` MUST NOT run anything through `Deps.Stream`.

### Checks

- R-FZBT-3513: When `up` discovers at least one app and none of them is named `auth`, it MUST write the no-auth diagnostic, `sandbox: no app named 'auth' in <worktree>`, one empty line, and `every other app needs auth to tell it who its user is`, each ending in a newline, to stderr, where `<worktree>` is the worktree's path, write nothing to stdout, and make `cli.Run` return 2; verified at least by a worktree holding only `dummy/etc/manifest.toml`, by one holding that and an `auth/` directory with no `etc/manifest.toml`, and by one holding `Auth/etc/manifest.toml` and `dummy/etc/manifest.toml`.

- R-CNZG-KYZQ: `up` MUST make its checks in this order and report only the first that fails: the worktree checks D03 orders (R-LKKR-WFZT) other than the unknown-sandbox check, then D05's discovery (R-USUB-H9VC, R-URMF-3I4N), then the no-auth check (R-FZBT-3513), then D05's manifest checks, the icon checks among them (R-U6S0-HHI7, R-O8Y7-YR40), its more-than-one-default check (R-VCCP-LLQG) and its secrets check (R-VH8B-4OP8), in that order, then D03's unit-clash check (R-LUIQ-EJA1), then, only for a sandbox the registry does not know, giving it a port (R-GS7O-RWBE); verified at least by a checkout with no apps while every port in the range is held reporting only the no-apps diagnostic, a checkout holding only `dummy` whose manifest has a top-level `port` reporting only the no-auth diagnostic, a manifest fault together with a missing secret reporting only the manifest fault, an unreadable icon file together with a missing secret reporting only the icon fault, a missing secret together with a unit clash reporting only the missing secrets, and a unit clash while every port is held reporting only the unit clash.

- R-G2ZI-8G96: When one of `up`'s checks refuses it, `up` MUST run nothing through either runner other than its `git rev-parse --show-toplevel` run, MUST create no file or directory other than a lock file and its missing parent directories, and MUST leave `registry.json` unchanged, absent when it was absent, verified at least for the no-apps diagnostic, the no-auth diagnostic, a manifest fault, an icon fault, the missing-secrets diagnostic and the unit-clash diagnostic, each for a sandbox the registry does not know and for one that is up, and for the no-free-port diagnostic for a sandbox the registry does not know.

- R-Y4NO-9W5V: When `up` gives a sandbox a port, the registry MUST hold that sandbox's entry, with that port, the worktree and an empty `apps`, by the time `up` makes its first build run, verified by a fake `go` that reads `registry.json` when it is run.

### Builds

- R-U16K-JJPF: Once its checks have passed, `up` MUST make one build run for each app it discovers (D05 R-UP6M-BYN9), in app-name order (D05 R-UNYP-Y6WK), before it runs `systemctl` at all, and MUST make no other run of `go`.

- R-Y8BD-F7DY: At each build run `up` makes, every generated entry (D03 R-FNZ0-5GUH) other than `<data>/build`, every file under `<data>/apps`, and `registry.json` MUST be as it was once `up` held the sandbox lock and had given any port, verified by a fake `go` that compares them, byte for byte and entry for entry, with their state before `cli.Run` began, in a sandbox that is up.

- R-Y9J9-SZ4N: When a build run of an app returns a non-zero `ExitCode`, `up` MUST fail with D02's external-failure diagnostic with the action `build <app>` and no detail, verified by a build of `dummy` answered with `ExitCode` 1 and `Output` `# github.com/ikigenba/ikigenba/dummy/cmd/dummy`, a newline, `cmd/dummy/main.go:41:2: undefined: render` and a newline writing exactly `sandbox: build dummy: exit status 1`, an empty line, `> # github.com/ikigenba/ikigenba/dummy/cmd/dummy` and `> cmd/dummy/main.go:41:2: undefined: render`, each ending in a newline, and nothing to stdout.

- R-YAR6-6QVC: When `Deps.Exec` returns an error for a build run of an app, `up` MUST fail with D02's runner-error diagnostic with the action `build <app>`.

- R-YBZ2-KIM1: Once a build run has failed, by a non-zero `ExitCode` or an error, `up` MUST make no further run through either runner.

- R-YD6Y-YACQ: After a failed build, every generated entry (D03 R-FNZ0-5GUH) other than `<data>/build`, every file under `<data>/apps`, and `registry.json` MUST be as they were once `up` held the sandbox lock and had given any port, so a first `up` leaves its new entry with an empty `apps` and nothing else of the sandbox, and a sandbox that was up keeps every unit file, binary and generated file it had.

- R-YEEV-C23F: When `up` returns after making a build run, `<data>/build` MUST NOT exist, whether `up` succeeded or failed, verified at least by a successful `up`, a failed build of the second app, and an `up` begun with a stale `<data>/build/old` present.

### Installing binaries

- R-YFMR-PTU4: Once every build run of `up` has succeeded, `up` MUST make `<data>/bin/<app>`, for each discovered app, the file that app's build run wrote at its staged build path, moved there by rename, so that a hard link made to the earlier `<data>/bin/<app>` before `up` began still holds the earlier bytes and `<data>/bin/<app>` holds the bytes the fake `go` wrote.

- R-YGUO-3LKT: After a successful `up`, `<data>/bin` MUST hold exactly one entry for each discovered app, named after the app, and `<data>/env` exactly one for each discovered app, its env file (D03).

### Removed apps

- R-YI2K-HDBI: A removed app of an `up` MUST be an app the sandbox's registry entry records, as read once `up` holds the sandbox lock, that `up` did not discover.

- R-YJAG-V527: Once every build run has succeeded, `up` MUST make, for each removed app in app-name order, a stop run (D07 R-OB3F-0G6F) of its socket unit and then of its service unit, each only when that unit's file exists in the unit directory, before its daemon-reload run, verified with `dummy` removed and both its unit files present by the stop runs of `sandbox-wip-dummy.socket` and then `sandbox-wip-dummy.service`, and with neither present by no stop run.

- R-YKID-8WSW: At each stop run of a removed app, the registry MUST still record that app.

- R-YLQ9-MOJL: Once the stop runs of a removed app have succeeded, `up` MUST remove its socket unit file, its service unit file, `<data>/bin/<app>` and its env file, and MUST leave `<data>/apps/<app>` and everything under it unchanged.

- R-YMY6-0GAA: When a stop run of a removed app returns a non-zero `ExitCode` or an error, `up` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `stop <unit>`, `<unit>` the unit's name, and no detail, and MUST make no further run through either runner.

### Writing the sandbox

- R-U2EG-XBG4: Once every build run has succeeded and every removed app's files are removed, `up` MUST record in its registry entry exactly the discovered apps, each with `default` true exactly when it is the default app (D05 R-VESI-D57U), leaving every other registry entry unchanged.

- R-YQLV-5RID: When `up` writes an app's socket unit file or service unit file, the registry MUST already record that app, verified by an `up` adding `dummy` whose socket unit path is an existing directory, after which the registry records `dummy`.

- R-YRTR-JJ92: At its daemon-reload run, `up` MUST have created, for each discovered app, its app directory (D03 R-FBS0-BRFJ) and its state directory (D03 R-FCZW-PJ68).

- R-KSXM-UDTO: At its daemon-reload run, `up` MUST have written, for each discovered app, its env file with the content D05 gives it (R-O2UQ-1WEJ through R-MA5J-I950), and the sandbox's services file with the content D05 gives it (R-W0QP-90KC, R-O5AI-TFVX), each as computed by this `up`.

- R-YU9K-B2QG: At its daemon-reload run, `up` MUST have written the sandbox's nginx configuration file (D03 R-FAK3-XZOU) and its nginx unit file (D03 R-FKBB-05ME) with the content D06 gives them, as computed by this `up`.

- R-YVHG-OUH5: At its daemon-reload run, `up` MUST have written, for each discovered app, its socket unit file and its service unit file with the content the two requirements below give them.

- R-XNFA-ISXR: An app's socket unit file MUST be exactly the following lines, each ending in a newline, where `<name>` is the sandbox name, `<app>` the app's name and `<socket>` the app's socket path (D03 R-FLJ7-DXD3) written as a unit path value (D03 R-XJRL-DHPO):

  ```
  [Unit]
  Description=sandbox <name>: <app> socket

  [Socket]
  ListenStream=<socket>
  RemoveOnStop=yes
  ```

- R-XM7E-5172: An app's service unit file MUST be exactly the following lines, each ending in a newline, where `<name>` is the sandbox name, `<app>` the app's name, `<bin>` is the app's built binary (D03 R-F4GM-14ZD) unit-quoted as an executable (D03 R-XHBS-LY8A), and `<dir>` and `<env>` are its app directory (D03 R-FBS0-BRFJ) and its env file (D03 R-F6WE-SOGR), each written as a unit path value (D03 R-XJRL-DHPO); verified at least with a state root `/home/me/.local/state` and with a state root `/tmp/a b%c$d`, for which the lines of `dummy` in sandbox `wip` are `ExecStart="/tmp/a b%%c$d/ikigenba/sandbox/wip/bin/dummy"`, `WorkingDirectory=/tmp/a b%%c$d/ikigenba/sandbox/wip/apps/dummy` and `EnvironmentFile=/tmp/a b%%c$d/ikigenba/sandbox/wip/env/dummy.env`:

  ```
  [Unit]
  Description=sandbox <name>: <app>
  Requires=sandbox-<name>-<app>.socket
  After=sandbox-<name>-<app>.socket

  [Service]
  Type=notify
  ExecStart=<bin>
  WorkingDirectory=<dir>
  EnvironmentFile=<env>
  TimeoutStopSec=10
  ```

- R-YZ55-U5P8: `up` MUST NOT change or remove any file that existed under `<data>/apps` before it began, verified by a re-`up` leaving `apps/auth/state/auth.db` byte for byte as it was.

- R-Z0D2-7XFX: `up` of a sandbox MUST NOT create, change or remove anything under another sandbox's data directory.

- R-Z1KY-LP6M: When `up` cannot write a file, create a directory, rename a staged build or remove a removed app's file, it MUST fail with D02's file-error diagnostic for that path and make no further run through either runner, verified at least with the unit directory read-only.

### Starting

- R-Z2SU-ZGXB: `up` MUST make exactly one daemon-reload run (D07 R-ODJ7-RZNT), after its last file is written and removed, and only when every build run has succeeded.

- R-IJUW-KM7L: Once its daemon-reload run has succeeded, `up` MUST make its sandbox's state run (D03) before any start, restart or reload run.

- R-Z58N-R0EP: When the state run finds the sandbox down, `up` MUST then make exactly these runs and no other, in this order: a start run of each discovered app's socket unit in app-name order, a restart run of each discovered app's service unit in app-name order, and a start run of the sandbox's nginx unit; verified with `auth` and `dummy` by `sandbox-wip-auth.socket`, `sandbox-wip-dummy.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.service` and `sandbox-wip-nginx.service`, in that order.

- R-Z6GK-4S5E: When the state run finds the sandbox up, `up` MUST then make exactly these runs and no other, in this order: a start run of each discovered app's socket unit in app-name order, a restart run of each discovered app's service unit in app-name order, and a reload run of the sandbox's nginx unit; so no socket unit and no nginx unit is stopped or restarted.

- R-Z8WC-WBMS: When the daemon-reload run returns a non-zero `ExitCode` or an error, `up` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `systemctl --user daemon-reload` and no detail, verified by a daemon-reload answered with `ExitCode` 1 and `Output` `Failed to connect to bus: No medium found` and a newline writing exactly `sandbox: systemctl --user daemon-reload: exit status 1`, an empty line and `> Failed to connect to bus: No medium found`, each ending in a newline.

- R-ZA49-A3DH: When the state run of `up` returns a non-zero `ExitCode` or an error, `up` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `systemctl --user` and no detail.

- R-ZBC5-NV46: When a start run of an app's socket unit or a restart run of an app's service unit returns a non-zero `ExitCode`, `up` MUST fail with D02's external-failure diagnostic with the action `start <unit>`, `<unit>` the unit's name, and the detail `run 'sandbox logs <app>' for its journal`, verified at least by a restart of `sandbox-wip-dummy.service` and a start of `sandbox-wip-dummy.socket` each answered with `ExitCode` 1.

- R-ZCK2-1MUV: When `Deps.Exec` returns an error for a start run of an app's socket unit or a restart run of an app's service unit, `up` MUST fail with D02's runner-error diagnostic with the action `start <unit>`, `<unit>` the unit's name.

- R-ZDRY-FELK: When the start run or reload run of the nginx unit returns a non-zero `ExitCode`, `up` MUST fail with D02's external-failure diagnostic with the action `start <unit>` or `reload <unit>` respectively, `<unit>` the nginx unit's name, and the detail `run 'sandbox logs' for the journal`, verified at least by a start of `sandbox-wip-nginx.service` answered with `ExitCode` 1 writing `sandbox: start sandbox-wip-nginx.service: exit status 1` as its first line.

- R-ZEZU-T6C9: When `Deps.Exec` returns an error for the start run or reload run of the nginx unit, `up` MUST fail with D02's runner-error diagnostic with the action `start <unit>` or `reload <unit>` respectively, `<unit>` the nginx unit's name.

- R-ZG7R-6Y2Y: Once a run after its builds fails, by a non-zero `ExitCode` or an error, `up` MUST make no further run through either runner and MUST remove nothing it wrote, so the registry, the generated files and the unit files stay as they were at that run.

### Output

- R-ZHFN-KPTN: The URL listing of a sandbox for a list of apps MUST be, for each app in app-name order, a line holding the app's name, then spaces up to a width of the longest app name's length plus two, then the app's origin (D03), and a newline; and, directly after the default app's line when one of the apps is the default app, a line holding the default app's name padded to the same width, then the sandbox's default origin (D03), and a newline; verified at least by `auth` and `dummy` on port 7400 giving `auth   http://auth.wip.localhost:7400` and `dummy  http://dummy.wip.localhost:7400`, by `dummy` as the default app adding `dummy  http://wip.localhost:7400` after dummy's line, and by `auth` alone giving `auth  http://auth.wip.localhost:7400`.

- R-ZINJ-YHKC: When every run of `up` succeeds, `up` MUST write to stdout exactly the URL listing of its sandbox for the discovered apps, write nothing to stderr, and make `cli.Run` return 0.

### url

- R-IMAP-C5OZ: Once D03's worktree checks have passed, `url` MUST make exactly one run, its sandbox's state run (D03), besides its `git rev-parse --show-toplevel` run.

- R-ZL3C-Q11Q: When the state run finds the sandbox up, `url` MUST write to stdout exactly the URL listing of its sandbox for the apps the registry records, each recorded app's `default` marking the default app, write nothing to stderr, and make `cli.Run` return 0, verified at least by a registry recording `auth` and `dummy` while the worktree holds only `auth`, giving both lines.

- R-ZMB9-3SSF: When the state run finds the sandbox down, `url` MUST write the down diagnostic, `sandbox: sandbox '<name>' is down`, one empty line, and `run 'sandbox up' to start it`, each ending in a newline, to stderr, nothing to stdout, and make `cli.Run` return 2.

- R-ZNJ5-HKJ4: When the state run of `url` returns a non-zero `ExitCode` or an error, `url` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `systemctl --user` and no detail.
