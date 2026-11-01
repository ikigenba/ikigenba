# Stories — install

Putting turns on the person's user timer, so it pushes every five minutes without being asked and a machine that was off catches up at its next start. The timer is two systemd user units, `turns.service` and `turns.timer`. On a laptop `turns install` writes them under `~/.config/systemd/user/` and enables the timer in the person's user manager. On a host opsctl installs the same two units in `/etc/systemd/user/` and `opsctl user add` enables the timer for each account it creates; `turns install` there only enables it. There is no command that removes the units; a person who wants the timer gone disables it with `systemctl --user disable --now turns.timer`. `install` does not read the configuration: a timer installed before `turns configure` has run is inert, each run exiting 4, which the service unit counts as success. The top-level usage gains the line `  install   install the user timer that pushes every five minutes` under `Commands:`.

The release carries the two units under `turns/share/systemd/` in their host form, with `ExecStart=/usr/local/bin/turns push`, and turns carries the same text. On a laptop `install` writes that text with only `ExecStart` replaced: it names the absolute path of the turns that ran `install`, symlinks not resolved, followed by `push`; nothing else in either file varies. The two units, as written for a `turns` at `/home/me/go/bin/turns`:

`~/.config/systemd/user/turns.service`:

```
[Unit]
Description=Push this account's agent session turns to its space

[Service]
Type=oneshot
ExecStart=/home/me/go/bin/turns push
SuccessExitStatus=4
```

`~/.config/systemd/user/turns.timer`:

```
[Unit]
Description=Run turns push every five minutes

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
```

## A person asks what `install` can do

Command:

```
$ turns install --help
```

```
$ turns install -h
```

Output:

```
Usage: turns install

Write turns.service and turns.timer under ~/.config/systemd/user/, reload the
user manager, and enable and start the timer, which runs 'turns push' every
five minutes. On a host whose /etc/systemd/user/ already holds the units, only
enable and start the timer. Safe to run again.

Options:
  -h, --help   print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person installs the timer on a laptop

turns is on the person's PATH, installed by hand, and configured, and the person wants it to push on its own from now on. Nothing is printed; the answer to "did it work" is the exit code, and `systemctl --user list-timers turns.timer` shows the next run.

Command:

```
$ turns install
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `turns` is at `/home/me/go/bin/turns` and is the one on the PATH.
- `/etc/systemd/user/turns.timer` does not exist.
- `~/.config/systemd/user/turns.service` and `turns.timer` do not exist.
- The person's systemd user manager is running.

Postconditions:

- `~/.config/systemd/user/turns.service` and `turns.timer` hold exactly the content shown above, the directory created if it was absent.
- The user manager has reloaded its units, and `turns.timer` is enabled and active; its first run is at the next five-minute mark.
- No push ran during `install`, and no session file was read.

## A person installs the timer again

`install` is safe to repeat. A person who moved the turns binary, or whose units an older turns wrote, runs it again and the units are brought back to what this turns would write.

Command:

```
$ turns install
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- As for the previous story, except that the two units already exist, and `turns.service` names `/usr/local/bin/turns push` because the person once installed from a copy there.

Postconditions:

- Both units hold exactly the content shown above, `ExecStart=/home/me/go/bin/turns push` included.
- The user manager has reloaded its units, and `turns.timer` is enabled and active; a timer that was already enabled is still enabled, with its record of the last run kept, so a missed run is still caught up.

## A person installs the timer on a host

On a host opsctl owns the units, in `/etc/systemd/user/`, and keeps them current at each activate, so `turns install` writes nothing of its own: a copy under the person's home would shadow the host's and go stale. It only enables the timer, which is what `opsctl user add` does for accounts it creates, so a person whose account predates that, or who disabled the timer, can turn it on.

Command:

```
$ turns install
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `/etc/systemd/user/turns.service` and `/etc/systemd/user/turns.timer` exist, installed by opsctl.
- `~/.config/systemd/user/` holds no `turns.service` or `turns.timer`.
- The person's systemd user manager is running.

Postconditions:

- No file under `~/.config/systemd/user/` was written.
- `turns.timer` is enabled and active in the person's user manager.
- Linger for the account is as it was; enabling it is opsctl's business.

## A person installs the timer with no user manager to reach

In a session that has no systemd user manager, such as a container or an ssh login on a machine without one, turns cannot enable the timer. It says so and quotes what systemctl answered.

Command:

```
$ turns install
```

Output:

```
turns: could not enable turns.timer

> Failed to connect to bus: No medium found
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `/etc/systemd/user/turns.timer` does not exist.
- `systemctl --user` cannot reach a user manager.

Postconditions:

- The two units under `~/.config/systemd/user/` hold the content shown above; a later `turns install` with a user manager running enables the timer.
- No timer is enabled.
