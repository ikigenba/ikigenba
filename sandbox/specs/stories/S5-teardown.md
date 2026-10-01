# Stories — teardown

Taking a sandbox apart and seeing which sandboxes exist: `down` stops a sandbox and keeps its data, `wipe` deletes a stopped sandbox's data, and `ls` lists every sandbox the registry knows. The registry, `/home/me/.local/state/ikigenba/sandbox/registry.json`, records each sandbox's name, port, worktree path and the apps of its last `up`; `down` keeps a sandbox's entry whole, and only `wipe` removes it. A sandbox's data lives in `/home/me/.local/state/ikigenba/sandbox/<name>/`, holding each app's `apps/<app>/state/`, the stored token, and the files `up` generates; its units live in `/home/me/.config/systemd/user/` as `sandbox-<name>-<app>.socket`, `sandbox-<name>-<app>.service` and `sandbox-<name>-nginx.service`. A sandbox is up while `sandbox-<name>-nginx.service` is active and down otherwise, so after a reboot or logout every sandbox reads down. Without a name, `down` and `wipe` act on this worktree's sandbox, found from the current directory as `up` finds it; with a name they act on that sandbox from any directory, including one whose worktree is gone (an orphan). `ls` needs no checkout. `down` and `wipe` take the sandbox's lock: a second command that changes the same sandbox waits for the first to finish, then runs. The examples use the worktree `/home/me/src/ikigenba/wip` (sandbox `wip`, port `7400`, apps `auth` and `dummy`) and `/home/me/src/ikigenba/other` (sandbox `other`, port `7401`).

## A developer asks what `down` does

Command:

```
$ sandbox down --help
```

```
$ sandbox down -h
```

Output:

```
Usage: sandbox down [<name>]

Stop a sandbox and remove its units and generated files, keeping its data,
token and port. Without a name, the sandbox is this worktree's.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer takes this worktree's sandbox down

The developer is done for now but wants the apps' data, the token and the port back next time. `down` stops nginx and every app's units, removes the unit files and the generated files, and keeps everything else.

Command:

```
$ sandbox down
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip` or a directory below it.
- The sandbox `wip` is up on port `7400`, running `auth` and `dummy`.
- `/home/me/.local/state/ikigenba/sandbox/wip/apps/auth/state/auth.db` exists and a token is stored.

Postconditions:

- `sandbox-wip-nginx.service`, `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service` are stopped, their files are gone from `/home/me/.config/systemd/user/`, and the user's systemd manager has been reloaded so it no longer knows them.
- The files `up` generated under `/home/me/.local/state/ikigenba/sandbox/wip/` are gone.
- `apps/auth/state/auth.db`, every other app's `state/`, and the token are unchanged.
- The registry still holds `wip` with port `7400`, worktree `/home/me/src/ikigenba/wip`, and its record that the last `up` ran `auth` and `dummy`; `sandbox ls` shows it `down`.

## A developer takes a sandbox down by name from anywhere

Naming the sandbox makes the current directory irrelevant; no checkout is consulted.

Command:

```
$ sandbox down wip
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.
- The sandbox `wip` is up on port `7400`.

Postconditions:

- `wip`'s units are stopped and removed and its generated files are gone, as when `sandbox down` is run in its worktree.
- Its app `state/` directories, its token, and its registry entry with port `7400` are kept.

## A developer takes down an orphan whose worktree is gone

The worktree was deleted while its sandbox was still up. The sandbox is reached by name; the missing worktree is not an error.

Command:

```
$ sandbox down other
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The registry holds `other`, port `7401`, worktree `/home/me/src/ikigenba/other`, and that directory no longer exists.
- `sandbox-other-nginx.service` and `other`'s app units are running.

Postconditions:

- `other`'s units are stopped and removed and its generated files are gone.
- Its app `state/` directories, its token, and its registry entry are kept; `sandbox ls` shows it `down` with ` (gone)` after its worktree.

## A developer takes down a sandbox that is already down

`down` makes sure the sandbox is down; it does not complain that it already was.

Command:

```
$ sandbox down
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`; `sandbox-wip-nginx.service` is not active.

Postconditions:

- No `sandbox-wip-*` unit is running or present in `/home/me/.config/systemd/user/`, and no generated file remains under `wip`'s data.
- Its app `state/` directories, its token, and its registry entry are kept.

## A developer takes a sandbox down and systemctl fails

The first line of the output names the unit that could not be stopped; the `> ` lines are systemctl's own output, which varies.

Command:

```
$ sandbox down
```

Output:

```
sandbox: stop sandbox-wip-nginx.service: exit status 1

> Failed to stop sandbox-wip-nginx.service: Transport endpoint is not connected
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The sandbox `wip` is up, and systemctl cannot stop `sandbox-wip-nginx.service`.

Postconditions:

- Every unit and generated file `down` had not yet removed when systemctl failed is still there; running `sandbox down` again finishes the job.
- Its app `state/` directories, its token, and its registry entry with port `7400` are kept.

## A developer runs `down` again after one failed part-way

A `down` that fails stops where it failed and leaves the rest. Running it again, once whatever made systemctl fail is gone, removes what is left.

Command:

```
$ sandbox down
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- An earlier `sandbox down` of `wip` failed part-way, so some of `wip`'s units are still present in `/home/me/.config/systemd/user/` and some of its generated files are still under its data.
- systemctl now works.

Postconditions:

- No `sandbox-wip-*` unit is running or present in `/home/me/.config/systemd/user/`, the user's systemd manager has been reloaded so it no longer knows them, and no generated file remains under `wip`'s data.
- Its app `state/` directories, its token, and its registry entry with port `7400` are kept.

## A developer takes down a sandbox name nobody has

Command:

```
$ sandbox down old
```

Output:

```
sandbox: no sandbox 'old'

run 'sandbox ls' to see every sandbox
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The registry holds no sandbox named `old`.

Postconditions:

- Nothing has changed. No systemctl command was run.

## A developer takes down this worktree's sandbox before it ever came up

Command:

```
$ sandbox down
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

## A developer runs `down` outside any checkout without a name

Without a name the sandbox comes from the worktree, and there is none.

Command:

```
$ sandbox down
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

## A developer names two sandboxes to take down

Command:

```
$ sandbox down wip other
```

Output:

```
sandbox: down takes at most one name

see 'sandbox down --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed. Neither sandbox was stopped.

## A developer gives `down` an option it does not have

Command:

```
$ sandbox down --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox down --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer asks what `wipe` does

Command:

```
$ sandbox wipe --help
```

```
$ sandbox wipe -h
```

Output:

```
Usage: sandbox wipe [<name>]

Delete a stopped sandbox's data, token and registry entry, freeing its port.
Without a name, the sandbox is this worktree's.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer wipes this worktree's sandbox

The developer wants a clean start: no app state, no token, and the port given back. The next `up` from this worktree is a first `up` again and takes the lowest free port, which may be `7400` or may not.

Command:

```
$ sandbox wipe
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip`, port `7400`; the sandbox is down.
- `/home/me/.local/state/ikigenba/sandbox/wip/` holds `apps/auth/state/auth.db` and a stored token.

Postconditions:

- `/home/me/.local/state/ikigenba/sandbox/wip/` no longer exists: every app's state and the token are gone.
- The registry no longer holds `wip`. Port `7400` belongs to no sandbox, so a later first `up` of any sandbox may take it.
- The worktree itself is untouched.

## A developer wipes a sandbox by name from anywhere

Command:

```
$ sandbox wipe wip
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.
- The registry holds `wip`, port `7400`; the sandbox is down.

Postconditions:

- `wip`'s data directory and registry entry are gone and port `7400` is free, as when `sandbox wipe` is run in its worktree.

## A developer wipes an orphan whose worktree is gone

Wiping is how a sandbox whose worktree was deleted is finally forgotten.

Command:

```
$ sandbox wipe other
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The registry holds `other`, port `7401`, worktree `/home/me/src/ikigenba/other`, and that directory no longer exists.
- The sandbox `other` is down.

Postconditions:

- `/home/me/.local/state/ikigenba/sandbox/other/` no longer exists and the registry no longer holds `other`; port `7401` is free.
- `sandbox ls` no longer lists `other`.

## A developer wipes a sandbox that is still up

Data is deleted only once nothing is running against it.

Command:

```
$ sandbox wipe
```

Output:

```
sandbox: sandbox 'wip' is up

run 'sandbox down wip' first
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The sandbox `wip` is up.

Postconditions:

- Nothing has changed. The sandbox is still up and nothing was deleted.

## A developer wipes a sandbox whose units outlived a reboot

The machine rebooted while `wip` was up, so it reads down, but its unit files and generated files are still on disk because `down` never ran. `wipe` removes those too.

Command:

```
$ sandbox wipe wip
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The registry holds `wip`, port `7400`.
- `sandbox-wip-nginx.service` is not active, and `sandbox-wip-nginx.service`, `sandbox-wip-auth.socket`, `sandbox-wip-auth.service`, `sandbox-wip-dummy.socket` and `sandbox-wip-dummy.service` are still in `/home/me/.config/systemd/user/`.

Postconditions:

- No `sandbox-wip-*` file remains in `/home/me/.config/systemd/user/`, and the user's systemd manager has been reloaded so it no longer knows them.
- `wip`'s data directory and registry entry are gone and port `7400` is free.

## A developer wipes a sandbox name nobody has

Command:

```
$ sandbox wipe old
```

Output:

```
sandbox: no sandbox 'old'

run 'sandbox ls' to see every sandbox
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The registry holds no sandbox named `old`.

Postconditions:

- Nothing has changed. Nothing was deleted.

## A developer runs `wipe` outside any checkout without a name

Without a name the sandbox comes from the worktree, and there is none.

Command:

```
$ sandbox wipe
```

Output:

```
sandbox: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.

Postconditions:

- Nothing has changed. Nothing was deleted.

## A developer runs `down` or `wipe` without a name in a worktree whose sandbox name belongs to another worktree

Two worktrees with the same basename would be the same sandbox. Without a name, `down` and `wipe` act on the sandbox only when the registry records this worktree as its own; otherwise they refuse as `up` does, whether or not the recorded worktree still exists. Both forms print the same text.

Command:

```
$ sandbox down
```

```
$ sandbox wipe
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

- Nothing has changed. The `wip` sandbox of `/home/me/old/wip`, up or down, is untouched: nothing was stopped and nothing was deleted.

## A developer names two sandboxes to wipe

Command:

```
$ sandbox wipe wip other
```

Output:

```
sandbox: wipe takes at most one name

see 'sandbox wipe --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed. Nothing was deleted.

## A developer gives `wipe` an option it does not have

Command:

```
$ sandbox wipe --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox wipe --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer asks what `ls` does

Command:

```
$ sandbox ls --help
```

```
$ sandbox ls -h
```

Output:

```
Usage: sandbox ls

List every known sandbox: its name, port, state and worktree, marking a
worktree that no longer exists.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer lists several sandboxes, one of them an orphan

`ls` prints a header and one row per registry entry in name order. Each column is padded to its widest entry plus two spaces; the last is not padded. The state is `up` or `down`, read from the sandbox's nginx unit. A worktree that no longer exists is followed by ` (gone)`.

Command:

```
$ sandbox ls
```

Output:

```
NAME   PORT  STATE  WORKTREE
other  7401  down   /home/me/src/ikigenba/other (gone)
wip    7400  up     /home/me/src/ikigenba/wip
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me/src/ikigenba/wip`.
- The registry holds `wip` (port `7400`, worktree `/home/me/src/ikigenba/wip`, up) and `other` (port `7401`, worktree `/home/me/src/ikigenba/other`, down).
- `/home/me/src/ikigenba/other` no longer exists.

Postconditions:

- Nothing has changed.

## A developer lists sandboxes when there are none

Command:

```
$ sandbox ls
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The registry holds no sandbox, or does not exist.

Postconditions:

- Nothing has changed. No registry was created.

## A developer lists sandboxes from outside any checkout

`ls` reads only the registry and the units' state; it never asks git where it is.

Command:

```
$ sandbox ls
```

Output:

```
NAME   PORT  STATE  WORKTREE
other  7401  down   /home/me/src/ikigenba/other
wip    7400  up     /home/me/src/ikigenba/wip
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The current directory is `/home/me`, which is not inside a git checkout.
- The registry holds `wip` (up) and `other` (down), and both worktrees exist.

Postconditions:

- Nothing has changed.

## A developer lists sandboxes and systemctl fails

`ls` asks systemctl whether each sandbox is up. When it cannot answer, `ls` prints no partial table. The `> ` lines of the output are systemctl's own output, which varies.

Command:

```
$ sandbox ls
```

Output:

```
sandbox: systemctl --user: exit status 1

> Failed to connect to bus: No medium found
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The registry holds `wip` and `other`.
- systemctl cannot reach the user's systemd manager.

Postconditions:

- Nothing has changed.

## A developer lists sandboxes after a reboot

Both sandboxes were up before the machine rebooted. Their units are never enabled, so nothing started them again, and both read `down`.

Command:

```
$ sandbox ls
```

Output:

```
NAME   PORT  STATE  WORKTREE
other  7401  down   /home/me/src/ikigenba/other
wip    7400  down   /home/me/src/ikigenba/wip
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- The registry holds `wip` and `other`, both worktrees exist, and both sandboxes were up before the machine rebooted.
- Since the reboot, no `sandbox-*` unit has been started.

Postconditions:

- Nothing has changed.

## A developer gives `ls` an argument

Command:

```
$ sandbox ls wip
```

Output:

```
sandbox: ls takes no arguments

see 'sandbox ls --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer gives `ls` an option it does not have

Command:

```
$ sandbox ls --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox ls --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.
