# Stories — python

scripts runs every script as `python3.12 main.py` in the run's `tree/`, with the environment `S15` tells, and it has no Python of its own: the interpreter is the host's `python3.12`, found on the `PATH` scripts is started with, which is also the `PATH` every run is given (`S15`). Python is pinned to one minor version, `3.12`, declared once in scripts' source; the command's name, the developer's prerequisite, and what scripts' tests need of the interpreter all derive from that one declaration. The patch level is whatever the host's package, or the developer's install, provides; scripts names none and asks for none. Like `git`, `python3.12` is a dependency of the host, and scripts looks for it at start, after its settings, its socket and `git`, and before it opens its database (`S02`). On a space the host gets it from the space's first-boot script, which installs it beside `git` and names the same version on its own, so a change of version changes both places; a space whose instance was launched before that script named `python3.12` lacks it until an operator installs it once by hand. A developer's machine needs `python3.12` on its `PATH` for a sandbox and for scripts' tests, and installs it with uv. The space in these stories is `sbx.ikigenba.dev`, where scripts answers at `scripts.sbx.ikigenba.dev` (`S24`).

## The host starts scripts where python3.12 is not installed

scripts runs every script with `python3.12`, so without it every run would fail, and it says so at start rather than with a failed run later. `python3.12` is looked for as `git` is (`S02`): on the `PATH` scripts was started with, in the order of its directories as a shell would look, an executable file named `python3.12` in one of them, skipping an empty or relative entry, so scripts never finds one through its working directory. Only the name is looked for: scripts does not run the interpreter at start or ask its version. A `python3` or `python3.11` on the `PATH` does not stand in for it. This is not the caller's usage but a host missing a dependency it provides, so it exits 1, and it does so before it opens the database, so a host without `python3.12` keeps its state as it was. scripts looks for `python3.12` after its settings, its socket and `git` (`S02`), so this is reported only when those are in order.

Command:

```
$ scripts
```

Output:

```
scripts: python3.12 not found on PATH
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists and is run by its path.
- No directory on the `PATH` scripts is started with holds an executable named `python3.12`; one of them holds an executable named `git`.
- `LISTEN_PID` is scripts' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the thirteen settings are unset, or valid.

Postconditions:

- Nothing has changed. scripts opened no database, and an absent `state/scripts.db` is still absent, as is an absent `state/runs/`; no run was marked `killed` and none was pruned; it ran no git and no script, served nothing, told systemd nothing, and sent telemetry nothing.

## A model runs a script under the host's python3.12

The interpreter a run gets is the host's `python3.12`, the one on scripts' `PATH`, so a script can rely on Python 3.12 and its standard library. A script cannot choose another: the command is fixed (`S15`), so a shebang line in `main.py` is never read. Here the caller's script prints the interpreter's version, and `result` shows what it printed. The script `py-version`, `scr_861d0c0e777390ea`, runs from the caller's repository `py-version`, `rep_42682f90f4accfed`, at `main`, which is the commit `7bba0fe0f28485190d9cbaeb9ae679b92331b65d`, whose tree holds only this `main.py`:

```
import sys

print("%d.%d" % sys.version_info[:2])
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_e25e93cd2c1d7798"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_e25e93cd2c1d7798","script":"scr_861d0c0e777390ea","sha":"7bba0fe0f28485190d9cbaeb9ae679b92331b65d","ref":"main","user":"u_7f3a9c21","request_id":"<request-id>","trigger":"manual","status":"exited","exit_code":0,"started":"2026-10-05T09:20:00Z","finished":"2026-10-05T09:20:00Z","stdout_bytes":5,"stderr_bytes":0,"truncated":false,"stdout":"3.12\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is exactly that line. `<request-id>` is the request id of the `run` call that started the run (`S08`).

Preconditions:

- scripts is serving, started with `git` and `python3.12` on its `PATH`, and telemetry takes every event.
- The caller is `u_7f3a9c21` (`mg@example.com`), who owns the script `py-version` and the repository it runs from.
- The caller's `run` of `py-version`, with no `ref` and no `input`, started the run `run_e25e93cd2c1d7798` at `2026-10-05T09:20:00Z`, and the run has exited.

Postconditions:

- Nothing has changed.

## A developer installs python3.12 for the sandbox and the tests

A sandbox runs scripts from the developer's checkout (`S25`), and scripts' tests run real scripts with the real interpreter, so the developer's machine needs `python3.12` on its `PATH` as a host does; without it scripts refuses to start, as above, and the tests fail rather than skip. uv installs the version scripts declares and puts a `python3.12` in its executable directory, `~/.local/bin`. A later `uv python install 3.12` finds it installed and changes nothing.

Command:

```
$ uv python install 3.12
$ python3.12 --version
```

Output: uv's report of what it installed, which this story does not fix, then:

```
Python 3.12.<n>
```

`<n>` is the patch level uv installed. Exits 0. The version line is on stdout; uv's report is on stderr.

Preconditions:

- `uv` is on the developer's `PATH`.
- `~/.local/bin` is on the developer's `PATH`, and on the `PATH` their systemd user manager gives the services it starts.
- No `python3.12` is on that `PATH`.

Postconditions:

- `python3.12` is on the developer's `PATH`, and scripts started from the checkout finds it.
- The next `sandbox up` starts scripts beside the other apps, and its runs are run with that `python3.12`.

## A developer brings up a sandbox where python3.12 is not installed

The sandbox starts scripts as it starts any app, and scripts refuses to start without `python3.12`, before it is ready, so the sandbox reports the app's service failed, as for any app that will not start (sandbox's `S2-up.md`). The journal it points to holds scripts' own line.

Command:

```
$ sandbox up
```

Output:

```
sandbox: start sandbox-wip-scripts.service: exit status 1

> Job for sandbox-wip-scripts.service failed because the control process exited with error code.
> See "systemctl --user status sandbox-wip-scripts.service" and "journalctl --user -xeu sandbox-wip-scripts.service" for details.

run 'sandbox logs scripts' for its journal
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The current directory is inside the developer's worktree, whose sandbox is `wip`, and the checkout holds `scripts` beside the other apps.
- The preconditions of a first `up` hold, except that no directory on the `PATH` the developer's systemd user manager gives the services it starts holds an executable named `python3.12`.

Postconditions:

- `sandbox-wip-scripts.service` is failed, and `sandbox status` shows `scripts failed`.
- `sandbox logs scripts` shows the line `scripts: python3.12 not found on PATH`.
- scripts created no database and no runs directory under its app directory.

## An agent deploys scripts to a space launched before first boot installed python3.12

The space's host was launched before its first-boot script named `python3.12`, so the host has `git` but no `python3.12`. devctl deploys the suite release to it as to any space, and the release's own opsctl activates it: activate restarts every app one at a time, and scripts' service refuses to start. opsctl reports that as it reports any service that will not come up, with the journal quoted (opsctl's `S10-releases.md`), and `devctl deploy` relays the same report (devctl's `S5-deploy.md`).

Command:

```
$ devctl deploy sbx.ikigenba.dev <sha>
```

Output: deploy's and activate's step lines, as for any release, which devctl's and opsctl's stories own, up to the step that starts scripts' service, which fails with `scripts: service failed to start`; then devctl's error line and, quoted from opsctl, `opsctl: activate failed` and the journal, whose quoted lines include:

```
ikigenba-scripts.service: Main process exited, code=exited, status=1/FAILURE
scripts: python3.12 not found on PATH
```

Exits 1. The step outcome lines are on stdout; the diagnostics and quoted journal are on stderr.

Preconditions:

- The host of `sbx.ikigenba.dev` was launched before its first-boot script installed `python3.12`, and `git` is on the `PATH`.
- No directory on the `PATH` scripts' service runs with holds an executable named `python3.12`.
- No release the host has activated held scripts, and the release at the commit `<sha>` holds it (`S23`).

Postconditions:

- `/opt/ikigenba/current` names `/opt/ikigenba/releases/<sha>/`, scripts' units are written and enabled, its socket is listening, and nginx routes `scripts.sbx.ikigenba.dev`; `ikigenba-scripts.service` is `failed`. Nothing was rolled back.
- `/run/ikigenba/services.json` has been rewritten and lists `scripts` as enabled: the entry is written before the service starts, and a failed service does not undo it.
- scripts created no `/var/opt/ikigenba/scripts/state/scripts.db` and no `/var/opt/ikigenba/scripts/state/runs/`, and sent telemetry nothing.

## An operator installs python3.12 on a space launched before first boot installed it

The fix is a one-off install with the host's package manager, the same package the first-boot script names, followed by a restart of scripts, which then finds `python3.12` and starts as on any other host (`S02`). The operator works on the space's host as opsctl's commands are run there, as root over ssh; `devctl space restart sbx scripts`, run from the developer's machine, does the restart the same way and prints the same line. A space launched after the first-boot script named `python3.12`, including one rebuilt with `devctl space destroy` and `devctl space create`, has it from first boot and needs none of this.

Command:

```
$ sudo dnf install -y python3.12
$ sudo opsctl restart scripts
```

Output: dnf's report of the install, which this story does not fix, then opsctl's one line for the restart, reporting scripts' service `ok` and `active`, in the layout opsctl's stories own (opsctl's `S7-apps.md`).

Exits 0. opsctl's line is on stdout, after dnf's report; opsctl writes nothing to stderr.

Preconditions:

- scripts is installed on the host of `sbx.ikigenba.dev` and enabled, and its service is `failed` because no directory on the `PATH` it runs with holds `python3.12`, as in the previous story.
- `git` is on that `PATH`.
- telemetry is installed on the host and takes every event.

Postconditions:

- `python3.12` is installed on the host, on the `PATH` scripts' service runs with.
- `ikigenba-scripts.service` is `active`: scripts started for the first time, creating `/var/opt/ikigenba/scripts/state/scripts.db` and `/var/opt/ikigenba/scripts/state/runs/` (`S02`), and is serving on `/run/ikigenba/scripts.sock`.
- telemetry has received scripts' `service.started`, whose `version` is `<display>`, the display string of the environment the host gives scripts (`S02`).
- Every run of a script on this host runs with that `python3.12`.
