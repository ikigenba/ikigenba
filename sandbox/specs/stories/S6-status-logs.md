# Stories — status and logs

Looking inside this worktree's sandbox: `status` shows the state of each unit, and `logs` prints the journal its units wrote. Both act on this worktree's sandbox, found from the current directory as `up` finds it, and both are reports: their whole product goes to stdout, including a unit that has failed, and they exit 0 when they could produce it. They read the systemd user manager and its journal and change nothing. The apps they cover are those the sandbox's registry entry records from its last `up` whose builds all succeeded and whose configuration nginx accepted; `down` keeps that record, so they work whether the sandbox is up or down. The examples use the worktree `/home/me/src/ikigenba/wip`, sandbox `wip`, port `7400`, with apps `auth` and `dummy`, whose units are `sandbox-wip-nginx.service`, `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service`.

## A developer asks what `status` does

Command:

```
$ sandbox status --help
```

```
$ sandbox status -h
```

Output:

```
Usage: sandbox status

Show the state of nginx and of each app's service in this worktree's
sandbox.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer checks a sandbox whose units are all running

`status` prints a header, a row for nginx, then one row per app of the last `up` in name order. The state is the service unit's systemd active state, verbatim: `active`, `inactive`, `failed`, `activating` or `deactivating`. The first column is padded to its widest entry plus two spaces.

Command:

```
$ sandbox status
```

Output:

```
UNIT   STATE
nginx  active
auth   active
dummy  active
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip` or a directory below it.
- The sandbox `wip` is up and every unit is running.

Postconditions:

- Nothing has changed.

## A developer checks a sandbox where one app has failed

A failed unit is a finding about the sandbox, not a failure of `status`, so it is reported on stdout and the command still succeeds.

Command:

```
$ sandbox status
```

Output:

```
UNIT   STATE
nginx  active
auth   active
dummy  failed
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The sandbox `wip` is up and `sandbox-wip-dummy.service` has failed.

Postconditions:

- Nothing has changed. The failed unit was not restarted.

## A developer checks a sandbox that is down

Command:

```
$ sandbox status
```

Output:

```
UNIT   STATE
nginx  inactive
auth   inactive
dummy  inactive
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`, whose last `up` ran `auth` and `dummy`, and the sandbox is down.

Postconditions:

- Nothing has changed. Nothing was started.

## A developer checks a sandbox that was never brought up

Command:

```
$ sandbox status
```

Output:

```
sandbox: no sandbox 'wip'

run 'sandbox up' to create it
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds no sandbox named `wip`.

Postconditions:

- Nothing has changed.

## A developer checks status outside any checkout

Command:

```
$ sandbox status
```

Output:

```
sandbox: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.

Postconditions:

- Nothing has changed.

## A developer checks status or reads logs in a worktree whose sandbox name belongs to another worktree

Two worktrees with the same basename would be the same sandbox. `status` and `logs` report on the sandbox only when the registry records this worktree as its own; otherwise they refuse as `up` does, whether or not the recorded worktree still exists. Both forms print the same text.

Command:

```
$ sandbox status
```

```
$ sandbox logs
```

Output:

```
sandbox: sandbox 'wip' belongs to another worktree: /home/me/old/wip

rename this worktree, or wipe that sandbox with 'sandbox wipe wip' once it is down
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip` with worktree `/home/me/old/wip`.

Postconditions:

- Nothing has changed. Neither systemctl nor journalctl was run.

## A developer checks status and systemctl fails

When the user manager cannot be asked, there is no report to give. The `> ` lines of the output are systemctl's own output, which varies.

Command:

```
$ sandbox status
```

Output:

```
sandbox: systemctl --user: exit status 1

> Failed to connect to bus: No medium found
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`.
- `systemctl --user` fails because no systemd user manager is reachable.

Postconditions:

- Nothing has changed.

## A developer gives `status` an argument

Command:

```
$ sandbox status dummy
```

Output:

```
sandbox: status takes no arguments

see 'sandbox status --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer gives `status` an option it does not have

Command:

```
$ sandbox status --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox status --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer asks what `logs` does

Command:

```
$ sandbox logs --help
```

```
$ sandbox logs -h
```

Output:

```
Usage: sandbox logs [options] [<app>]

Print the journal of this worktree's sandbox: nginx and every app, or only
<app>.

Options:
  -n, --lines <count>  print the last <count> lines (default 100)
  -f, --follow         keep printing new lines until interrupted
  -h, --help           print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer reads the whole sandbox's journal

Without an app, `logs` prints the last 100 lines the journal holds for nginx and every app's socket and service, interleaved in time order, passed through from `journalctl --user` verbatim in its short format. The lines vary; these are an example.

Command:

```
$ sandbox logs
```

Output:

```
Oct 01 09:12:01 laptop systemd[1802]: Started sandbox-wip-nginx.service.
Oct 01 09:12:02 laptop auth[4118]: ready
Oct 01 09:12:02 laptop dummy[4120]: ready
Oct 01 09:12:40 laptop dummy[4120]: GET /widgets 200
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`, and the journal holds lines from its units.

Postconditions:

- Nothing has changed.

## A developer reads one app's journal

With an app, only that app's socket and service are included; nginx and the other apps are left out. The output lines vary; they are the last 100 the journal holds for `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service`.

Command:

```
$ sandbox logs dummy
```

Output:

```
Oct 01 09:12:02 laptop dummy[4120]: ready
Oct 01 09:12:40 laptop dummy[4120]: GET /widgets 200
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy` is an app of `wip`'s last `up`.

Postconditions:

- Nothing has changed.

## A developer reads only the last few lines

Command:

```
$ sandbox logs -n 20 dummy
```

```
$ sandbox logs --lines 20 dummy
```

Options:

- `-n, --lines <count>` prints the last `<count>` lines instead of the last 100. `<count>` is a positive whole number.

Output: at most the last 20 lines the journal holds for `dummy`'s socket and service, verbatim.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `dummy` is an app of `wip`'s last `up`.

Postconditions:

- Nothing has changed.

## A developer follows the journal as it grows

The developer watches the sandbox while exercising it in a browser. `logs` prints the last lines as usual, then keeps printing each new line as the units write it, until the developer interrupts it with Ctrl-C. Being interrupted is how following ends, so it is a success.

Command:

```
$ sandbox logs -f
```

```
$ sandbox logs --follow
```

Options:

- `-f, --follow` keeps printing new lines until interrupted.

Output: the last 100 lines for the sandbox's units, then every new line as it arrives, verbatim.

Exits 0 when interrupted with SIGINT. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The sandbox `wip` is up.

Postconditions:

- Nothing has changed.

## A developer reads the journal of a sandbox that is down

The journal outlives the units, so a sandbox that is down still has the lines its last run wrote; reading them is how the developer finds out why it stopped. The output lines vary; those shown are an example.

Command:

```
$ sandbox logs
```

Output:

```
Oct 01 09:12:02 laptop dummy[4120]: ready
Oct 01 10:05:17 laptop dummy[4120]: draining
Oct 01 10:05:17 laptop systemd[1802]: Stopped sandbox-wip-dummy.service.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`, the sandbox is down, and the journal holds lines from its earlier run.

Postconditions:

- Nothing has changed. Nothing was started.

## A developer asks for the journal of an app the sandbox does not have

The app is checked against the sandbox, not the command line's shape, so this is a precondition that was not met rather than a usage error.

Command:

```
$ sandbox logs nope
```

Output:

```
sandbox: no app 'nope' in sandbox 'wip'
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- `wip`'s last `up` ran `auth` and `dummy` only.

Postconditions:

- Nothing has changed. journalctl was not run.

## A developer reads logs outside any checkout

Command:

```
$ sandbox logs
```

Output:

```
sandbox: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.

Postconditions:

- Nothing has changed. journalctl was not run.

## A developer gives `--lines` no value

Command:

```
$ sandbox logs --lines
```

Output:

```
sandbox: --lines needs a value

see 'sandbox logs --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer gives `-n` no value

Command:

```
$ sandbox logs -n
```

Output:

```
sandbox: -n needs a value

see 'sandbox logs --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer gives `--lines` something other than a count

Zero and negative numbers are refused the same way.

Command:

```
$ sandbox logs --lines x
```

Output:

```
sandbox: --lines needs a positive whole number, not 'x'

see 'sandbox logs --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer gives `-n` something other than a count

The option is named as it was typed. Zero and negative numbers are refused the same way.

Command:

```
$ sandbox logs -n x
```

Output:

```
sandbox: -n needs a positive whole number, not 'x'

see 'sandbox logs --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer names two apps for `logs`

Command:

```
$ sandbox logs auth dummy
```

Output:

```
sandbox: logs takes at most one app

see 'sandbox logs --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer gives `logs` an option it does not have

Command:

```
$ sandbox logs --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox logs --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer reads the journal and journalctl fails

The `> ` lines of the output are journalctl's own output, which varies.

Command:

```
$ sandbox logs
```

Output:

```
sandbox: journalctl --user: exit status 1

> Failed to get journal access: Permission denied
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`.
- `journalctl --user` fails.

Postconditions:

- Nothing has changed.
