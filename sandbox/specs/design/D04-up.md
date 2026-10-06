# D04-up

This document owns what `up` and `url` do once D02 has parsed a well-formed command line: the order of `up`'s checks, the builds, the staging of a deployment and nginx's test of it, how a new deployment replaces a running one, what `up` writes and when, the app unit files, the systemd runs that bring the sandbox up or redeploy it, the apps that join or leave, what both commands print, and every way `up` fails after its checks. Finding the sandbox, its name, the layout, the registry, the locks, the port, the unit names, the socket paths, the origins and the up/down rule are D03's; discovery, the manifest and secrets checks, the env file and the services file are D05's; the nginx configuration and the nginx unit are D06's; the help texts and the shared diagnostic shapes are D02's. This document cites them and restates none.

`up` checks everything before it changes anything: the worktree checks D03 orders, then D05's discovery, then D05's manifest and secrets checks, then D03's unit-clash check, and last, for a sandbox the registry does not know yet, a port. Whether one of the apps is `auth` is no check at all: a checkout without one comes up with every app ungated (D06). A refusal at any check leaves no trace beyond a lock file. Once a port is given, the registry entry exists with no apps recorded, and it stays whatever happens next.

Then `up` assembles the whole new deployment in the staging directory `<data>/stage`, never straight over the files a running sandbox reads. It builds every app, in app-name order, with `go build -o` into `<data>/stage/bin`; a failed build stops the builds and changes nothing outside the staging directory, so a running sandbox stays exactly as it was. It then writes there everything the deployment is made of: the env files, the services file, nginx's configuration and unit, and each app's socket and service units. The staged nginx configuration is byte for byte the file that will be deployed, so it names the final paths (the pid file and temporary directories under `<data>/nginx`, the app sockets, each app's own fragments in the worktree), and `up` has nginx test it, running `nginx -t` itself so it can quote nginx's output. The test is made with the prefix the nginx unit uses and the staged file as the configuration; its error log goes to `/dev/null`, so that nginx writes each problem to standard error as one `nginx: [emerg] ...` line rather than as a timestamped log entry. `nginx -t` opens the pid file the configuration names and creates its temporary directories, so `up` makes sure `<data>/nginx` exists before the test, and removes it again when it made it and nginx refuses. Binding a listening address that is already in use does not fail the test, so a running sandbox, or another program on the port, does not stop a good configuration from passing. A refused configuration ends `up` with nginx's output quoted: nothing is moved into place, nothing is stopped or started, the registry is unchanged, and a running sandbox keeps serving its previous deployment.

Once nginx accepts the configuration, `up` first takes apart the apps the registry records but the checkout no longer holds: it stops each one's socket and service, removes its unit files, binary and env file, and keeps its app directory with its `state/`. Then it records the discovered apps in the registry, so unit files on disk always belong to recorded apps (D03), and only then moves the staged deployment into place: each staged binary goes over `<data>/bin/<app>` by rename, which gives the path a new file while a running service keeps executing the old one until it restarts, and the generated files and unit files take the staged content. Each service unit places its app in one of the sandbox's slices (D03), the core slice for an app D05 places `core` and the apps slice otherwise, and sets `Delegate=yes` for an app that delegates; the socket units set no slice. Unit files live under the config root and may sit on another file system than the state root, so they are written in place rather than renamed. `up` also creates every app directory with its `state/`. Every path in the unit files is written in the form D03 gives its position: the binary unit-quoted as an executable, the app directory, env file and socket path as unit path values. systemd refuses an executable path holding `"`, `'` or `\`, however it is written, and ignores a setting that is not UTF-8 clean, so D03 refuses a state root holding any of those before `up` changes anything. One `systemctl --user daemon-reload` makes systemd read the units, and the sandbox's state, read from systemd as D03 says, picks what follows.

Every socket is started first, in app-name order, so every app's socket is listening before any app runs and an app that reaches a peer while starting waits in that peer's socket instead of being refused. Then every service is restarted, in app-name order. `restart` starts a service that is not running and replaces one that is, so a service still running from an earlier `up` whose nginx failed is replaced by the new build rather than left alone. Each service inherits its socket from systemd and is `Type=notify`, so systemctl returns only once the app reports ready, or with the failure when it exits first. A socket that is already listening is untouched by its start, so during a redeploy requests queue in the socket while the service restarts and are answered by the new build. nginx comes last: started when the sandbox was down, so the sandbox reads up only once every app is ready; reloaded when it was up, so it never stops listening. The first run that fails ends `up`, leaving what it had done.

`url` prints what the last `up` printed, from the apps the registry records, and refuses a sandbox that is down.

## REQUIREMENTS

### Program runs

- R-RGOI-M8C3: A build run of an app MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `go`, whose `Args` are exactly `build`, `-o`, the app's staged build path (D03 R-RAL0-PDMM) and `./cmd/<app>`, naming the app's main package (D05 R-UFFF-9SPP) relative to `Dir`, and whose `Dir` is the app's source directory (D05 R-AUCL-BZBE).

- R-RHWF-002S: A configuration test run MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `nginx`, whose `Args` are exactly `-t`, `-p`, the nginx directory (D03 R-F9C7-K7Y5), `-c`, the staged nginx configuration file (D03 R-RD0T-GX40), `-e` and `/dev/null`, in that order, each path as it is with no quoting, and whose `Dir` is `/`; verified at least for sandbox `wip` with a state root `/tmp/a b%c$d` by the `Args` `-t`, `-p`, `/tmp/a b%c$d/ikigenba/sandbox/wip/nginx`, `-c`, `/tmp/a b%c$d/ikigenba/sandbox/wip/stage/nginx/nginx.conf`, `-e` and `/dev/null`.

- R-XXC9-Z9PP: A start run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `start` and the unit's name, and whose `Dir` is `/`.

- R-XYK6-D1GE: A restart run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `restart` and the unit's name, and whose `Dir` is `/`.

- R-XZS2-QT73: A reload run of a unit MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `reload` and the unit's name, and whose `Dir` is `/`.

- R-Y0ZZ-4KXS: `up` and `url` MUST NOT run anything through `Deps.Stream`.

### Checks

- R-JQZ8-GJAX: `up` MUST make its checks in this order and report only the first that fails: the worktree checks D03 orders (R-LKKR-WFZT) other than the unknown-sandbox check, then D05's discovery (R-USUB-H9VC, R-URMF-3I4N), then D05's manifest checks, the icon checks among them (R-JPRC-2RK8, R-O8Y7-YR40), its more-than-one-default check (R-VCCP-LLQG) and its secrets check (R-A0RL-IX8T), in that order, then D03's unit-clash check (R-LUIQ-EJA1), then, only for a sandbox the registry does not know, giving it a port (R-GS7O-RWBE); verified at least by a checkout with no apps while every port in the range is held reporting only the no-apps diagnostic, a manifest fault together with a missing secret reporting only the manifest fault, an unreadable icon file together with a missing secret reporting only the icon fault, a missing secret together with a unit clash reporting only the missing secrets, and a unit clash while every port is held reporting only the unit clash.

- R-RJ4B-DRTH: `up` MUST NOT refuse a checkout because none of its apps is named `auth`, verified at least by `up` of a worktree named `wip` holding only `dummy/etc/manifest.toml`, with `default = false`, for a sandbox the registry does not know, with every run answered with `ExitCode` 0 and the state run's `Stdout` `inactive` and a newline, writing exactly `dummy  http://dummy.wip.localhost:7400` and a newline to stdout, nothing to stderr, and making `cli.Run` return 0.

- R-S969-SQY1: When one of `up`'s checks refuses it, `up` MUST run nothing through either runner other than its `git rev-parse --show-toplevel` run, MUST create no file or directory other than a lock file and its missing parent directories, and MUST leave `registry.json` unchanged, absent when it was absent, verified at least for the no-apps diagnostic, a manifest fault, an icon fault, a secret's bad-character refusal (D05 R-PKCY-6WJV), the missing-secrets diagnostic and the unit-clash diagnostic, each for a sandbox the registry does not know and for one that is up, and for the no-free-port diagnostic for a sandbox the registry does not know.

- R-Y4NO-9W5V: When `up` gives a sandbox a port, the registry MUST hold that sandbox's entry, with that port, the worktree and an empty `apps`, by the time `up` makes its first build run, verified by a fake `go` that reads `registry.json` when it is run.

### Builds

- R-U16K-JJPF: Once its checks have passed, `up` MUST make one build run for each app it discovers (D05 R-UP6M-BYN9), in app-name order (D05 R-UNYP-Y6WK), before it runs `systemctl` at all, and MUST make no other run of `go`.

- R-RMS0-J31K: At each build run `up` makes, every generated entry (D03 R-RE8P-UOUP) other than `<data>/stage`, every file under `<data>/apps`, and `registry.json` MUST be as it was once `up` held the sandbox lock and had given any port, verified by a fake `go` that compares them, byte for byte and entry for entry, with their state before `cli.Run` began, in a sandbox that is up.

- R-Y9J9-SZ4N: When a build run of an app returns a non-zero `ExitCode`, `up` MUST fail with D02's external-failure diagnostic with the action `build <app>` and no detail, verified by a build of `dummy` answered with `ExitCode` 1 and `Output` `# github.com/ikigenba/ikigenba/dummy/cmd/dummy`, a newline, `cmd/dummy/main.go:41:2: undefined: render` and a newline writing exactly `sandbox: build dummy: exit status 1`, an empty line, `> # github.com/ikigenba/ikigenba/dummy/cmd/dummy` and `> cmd/dummy/main.go:41:2: undefined: render`, each ending in a newline, and nothing to stdout.

- R-YAR6-6QVC: When `Deps.Exec` returns an error for a build run of an app, `up` MUST fail with D02's runner-error diagnostic with the action `build <app>`.

- R-YBZ2-KIM1: Once a build run has failed, by a non-zero `ExitCode` or an error, `up` MUST make no further run through either runner.

- R-RNZW-WUS9: After a failed build, every generated entry (D03 R-RE8P-UOUP) other than `<data>/stage`, every file under `<data>/apps`, and `registry.json` MUST be as they were once `up` held the sandbox lock and had given any port, so a first `up` leaves its new entry with an empty `apps` and nothing else of the sandbox, and a sandbox that was up keeps every unit file, binary and generated file it had.

- R-RP7T-AMIY: When `up` returns after making a build run, `<data>/stage` MUST NOT exist, whether `up` succeeded or failed, verified at least by a successful `up`, a failed build of the second app, a configuration test run answered with `ExitCode` 1, a daemon-reload run answered with `ExitCode` 1, and an `up` begun with a stale `<data>/stage/old` present.

### Testing the configuration

- R-RQFP-OE9N: Once every build run has succeeded, `up` MUST make exactly one configuration test run, after its last build run and before any run of `systemctl`.

- R-RRNM-260C: At its configuration test run, the staged nginx configuration file (D03 R-RD0T-GX40) MUST hold the content D06 gives the sandbox's nginx configuration file, as computed by this `up`, and the nginx directory (D03 R-F9C7-K7Y5) MUST exist as a directory, verified by a fake `nginx` that reads them when it is run, in a sandbox that is up and in one given its port by this `up`.

- R-RSVI-FXR1: At its configuration test run, every file in the unit directory, every generated entry (D03 R-RE8P-UOUP) other than `<data>/stage` and `<data>/nginx`, every entry under `<data>/nginx`, `<data>/apps` and every entry under it, and `registry.json` MUST be as they were once `up` held the sandbox lock and had given any port, `<data>/nginx` being a new empty directory when it was absent, verified by a fake `nginx` that compares them, byte for byte and entry for entry, with their state before `cli.Run` began, in a sandbox that is up whose last `up` recorded `auth` and `dummy` while the checkout now holds `auth` and `extra`, and in one that is down with `<data>/nginx` absent.

- R-RU3E-TPHQ: When the configuration test run returns a non-zero `ExitCode`, `up` MUST fail with D02's external-failure diagnostic with the action `test nginx configuration` and no detail, verified by a test run answered with `ExitCode` 1 and `Output` `a`, a newline, `b` and a newline writing exactly `sandbox: test nginx configuration: exit status 1`, an empty line, `> a` and `> b`, each ending in a newline, and nothing to stdout.

- R-RVBB-7H8F: When `Deps.Exec` returns an error for the configuration test run, `up` MUST fail with D02's runner-error diagnostic with the action `test nginx configuration`.

- R-X4D8-ZUG2: Once the configuration test run has failed, by a non-zero `ExitCode` or an error, `up` MUST make no further run through either runner, and when `up` returns, every file in the unit directory, every generated entry (D03 R-RE8P-UOUP) other than `<data>/stage` and `<data>/nginx`, `<data>/apps` and every entry under it, and `registry.json` MUST be as they were once `up` held the sandbox lock and had given any port; `<data>/nginx` MUST NOT exist when it did not exist then, even when the test run created a file in it, and otherwise `<data>/nginx` MUST still be a directory, every entry under it that existed then MUST be as it was then, byte for byte and entry for entry, and it MUST hold no entry other than those and those the test run created; verified at least in a sandbox that is up whose last `up` recorded `auth` and `dummy` while the checkout now holds `auth` and `extra`, so no stop run is made and the registry still records `auth` and `dummy`, in a sandbox that is down with `<data>/nginx` absent and a fake `nginx` that creates `<data>/nginx/nginx.pid`, and in a sandbox given its port by this `up`, whose entry keeps an empty `apps`, and in a sandbox that is up whose `<data>/nginx` holds `nginx.pid` and `client_body/` and a fake `nginx` that creates `<data>/nginx/proxy/`, after which `nginx.pid` and `client_body/` are as they were, and in a sandbox that is down whose `<data>/nginx` is an empty directory, after which it is still an empty directory.

- R-ZFIX-V9WF: At its configuration test run, each discovered app's staged env file (D03 R-ZEB1-HI5Q) MUST exist with permission bits exactly 0600, verified by a fake `nginx` that stats them.

### Installing binaries

- R-RYZ0-CSGI: Once its configuration test run has succeeded, `up` MUST make `<data>/bin/<app>`, for each discovered app, the file that app's build run wrote at its staged build path, moved there by rename, so that a hard link made to the earlier `<data>/bin/<app>` before `up` began still holds the earlier bytes and `<data>/bin/<app>` holds the bytes the fake `go` wrote.

- R-YGUO-3LKT: After a successful `up`, `<data>/bin` MUST hold exactly one entry for each discovered app, named after the app, and `<data>/env` exactly one for each discovered app, its env file (D03).

### Removed apps

- R-YI2K-HDBI: A removed app of an `up` MUST be an app the sandbox's registry entry records, as read once `up` holds the sandbox lock, that `up` did not discover.

- R-S06W-QK77: Once its configuration test run has succeeded, `up` MUST make, for each removed app in app-name order, a stop run (D07 R-OB3F-0G6F) of its socket unit and then of its service unit, each only when that unit's file exists in the unit directory, before its daemon-reload run, verified with `dummy` removed and both its unit files present by the stop runs of `sandbox-wip-dummy.socket` and then `sandbox-wip-dummy.service`, and with neither present by no stop run.

- R-YKID-8WSW: At each stop run of a removed app, the registry MUST still record that app.

- R-YLQ9-MOJL: Once the stop runs of a removed app have succeeded, `up` MUST remove its socket unit file, its service unit file, `<data>/bin/<app>` and its env file, and MUST leave `<data>/apps/<app>` and everything under it unchanged.

- R-YMY6-0GAA: When a stop run of a removed app returns a non-zero `ExitCode` or an error, `up` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `stop <unit>`, `<unit>` the unit's name, and no detail, and MUST make no further run through either runner.

### Writing the sandbox

- R-S1ET-4BXW: Once its configuration test run has succeeded and every removed app's files are removed, `up` MUST record in its registry entry exactly the discovered apps, each with `default` true exactly when it is the default app (D05 R-VESI-D57U), leaving every other registry entry unchanged.

- R-X5L5-DM6R: Before `up` changes `<data>/bin`, `<data>/env`, `<data>/services.json`, the nginx configuration file (D03 R-FAK3-XZOU), `<data>/apps` or any file in the unit directory, other than by removing a removed app's files (R-YLQ9-MOJL), the registry MUST record exactly the discovered apps, verified at least by an `up` adding `dummy` to a sandbox that is up, once each with `dummy`'s socket unit path an existing directory, `<data>/bin` a regular file, `<data>/env/dummy.env` an existing directory, `<data>/services.json` an existing directory, `<data>/apps/dummy` a regular file, `<data>/nginx/nginx.conf` an existing directory, and the nginx unit path an existing directory, after each of which the registry records `auth` and `dummy`.

- R-YRTR-JJ92: At its daemon-reload run, `up` MUST have created, for each discovered app, its app directory (D03 R-FBS0-BRFJ) and its state directory (D03 R-FCZW-PJ68).

- R-SCTY-Y264: At its daemon-reload run, `up` MUST have written, for each discovered app, its env file with the content D05 gives it (R-O2UQ-1WEJ through R-S6QH-17GN), and the sandbox's services file with the content D05 gives it (R-W0QP-90KC, R-O5AI-TFVX), each as computed by this `up`.

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

- R-3R9N-KG35: An app's service unit file MUST be exactly the following lines, each ending in a newline, where `<name>` is the sandbox name, `<app>` the app's name, `<bin>` is the app's built binary (D03 R-F4GM-14ZD) unit-quoted as an executable (D03 R-XHBS-LY8A), `<dir>` and `<env>` are its app directory (D03 R-FBS0-BRFJ) and its env file (D03 R-F6WE-SOGR), each written as a unit path value (D03 R-XJRL-DHPO), and `<slice>` is the sandbox's core slice (D03 R-G0QB-3OM8) when the app's placement (D05 R-G81P-EB2E) is `core` and its apps slice (D03 R-G363-V83M) when it is `apps`; and, exactly when the app delegates (D05 R-G99L-S2T3), followed by one further line, `Delegate=yes`, ending in a newline; verified at least with a state root `/home/me/.local/state` and with a state root `/tmp/a b%c$d`, for which the lines of `dummy` in sandbox `wip` are `ExecStart="/tmp/a b%%c$d/ikigenba/sandbox/wip/bin/dummy"`, `WorkingDirectory=/tmp/a b%%c$d/ikigenba/sandbox/wip/apps/dummy` and `EnvironmentFile=/tmp/a b%%c$d/ikigenba/sandbox/wip/env/dummy.env`; by `auth` placed `core` and `dummy` with no `[resources]` in sandbox `wip`, giving `Slice=sandbox-wip-core.slice` and `Slice=sandbox-wip-apps.slice`; by `dummy` setting `slice = "core"` and `auth` with no `[resources]`, giving `Slice=sandbox-wip-core.slice` for dummy and `Slice=sandbox-wip-apps.slice` for auth; by `dummy` in sandbox `wip-cgroups`, giving `Slice=sandbox-wip\x2dcgroups-apps.slice`, and `auth` placed `core` there, giving `Slice=sandbox-wip\x2dcgroups-core.slice`; and by `dummy` setting `delegate = true`, `delegate = false` and no `delegate`, only the first ending in `Delegate=yes`, while `auth`'s unit, whose manifest does not set `delegate`, ends in its `Slice=` line in each case:

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
  Slice=<slice>
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

- R-S3UL-VVFA: Once a run after its configuration test run fails, by a non-zero `ExitCode` or an error, `up` MUST make no further run through either runner and MUST remove nothing it wrote other than `<data>/stage`, so the registry, the generated files other than `<data>/stage` and the unit files stay as they were at that run.

### Output

- R-ZHFN-KPTN: The URL listing of a sandbox for a list of apps MUST be, for each app in app-name order, a line holding the app's name, then spaces up to a width of the longest app name's length plus two, then the app's origin (D03), and a newline; and, directly after the default app's line when one of the apps is the default app, a line holding the default app's name padded to the same width, then the sandbox's default origin (D03), and a newline; verified at least by `auth` and `dummy` on port 7400 giving `auth   http://auth.wip.localhost:7400` and `dummy  http://dummy.wip.localhost:7400`, by `dummy` as the default app adding `dummy  http://wip.localhost:7400` after dummy's line, and by `auth` alone giving `auth  http://auth.wip.localhost:7400`.

- R-ZINJ-YHKC: When every run of `up` succeeds, `up` MUST write to stdout exactly the URL listing of its sandbox for the discovered apps, write nothing to stderr, and make `cli.Run` return 0.

### url

- R-IMAP-C5OZ: Once D03's worktree checks have passed, `url` MUST make exactly one run, its sandbox's state run (D03), besides its `git rev-parse --show-toplevel` run.

- R-ZL3C-Q11Q: When the state run finds the sandbox up, `url` MUST write to stdout exactly the URL listing of its sandbox for the apps the registry records, each recorded app's `default` marking the default app, write nothing to stderr, and make `cli.Run` return 0, verified at least by a registry recording `auth` and `dummy` while the worktree holds only `auth`, giving both lines.

- R-ZMB9-3SSF: When the state run finds the sandbox down, `url` MUST write the down diagnostic, `sandbox: sandbox '<name>' is down`, one empty line, and `run 'sandbox up' to start it`, each ending in a newline, to stderr, nothing to stdout, and make `cli.Run` return 2.

- R-ZNJ5-HKJ4: When the state run of `url` returns a non-zero `ExitCode` or an error, `url` MUST fail with D02's external-failure or runner-error diagnostic respectively, with the action `systemctl --user` and no detail.
