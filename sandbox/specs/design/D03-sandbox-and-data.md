# D03-sandbox-and-data

A sandbox is one worktree's running copy of the suite. This document owns how a command finds the sandbox it acts on, the name that sandbox carries, where every file of every sandbox lives, the registry that records which sandboxes exist, the two locks that keep concurrent commands from deploying over each other or sharing a port, the port range, the unit and socket names, the rule that says whether a sandbox is up, the refusal of a worktree or root holding a control character (or a state root holding a quote or backslash, or a character systemd rejects), and how a path is written into a unit file. The commands themselves (`up` and `url` in D04, `down`, `wipe` and `ls` in D07, `status` and `logs` in D08, `token` and `token set` in D09) cite these names and rules and never restate them. Program runs go through the run seam D01 declares (`seam.Deps`, `seam.Cmd`, `Deps.Exec`); the diagnostic shapes for a failed program, a program that could not start, and a file that could not be read or written are D02's.

Most commands act on this worktree's sandbox. They ask git for the top level of the working tree from the current directory, so any subdirectory works, and name the sandbox after that directory's last element, folded into a DNS label because every app answers at `<app>.<name>.localhost`. `down` and `wipe` may instead be given a name, which reaches any sandbox the registry knows from anywhere, including one whose worktree is gone; `ls` lists every sandbox and needs no checkout. A sandbox found from a worktree is this worktree's only when the registry records that worktree for it, so two worktrees with the same last element never act on each other's sandbox.

All of a sandbox's data lives under the developer's state directory, and its units in the developer's systemd user directory, both found from the XDG variables with the usual fallbacks to `HOME`. Under the data root sit the registry, its lock, one lock per sandbox, and one data directory per sandbox. A data directory holds two kinds of entry. Generated entries are what `up` writes and rewrites: built binaries and their build staging area, env files, the services file, and nginx's directory; together with the unit files they are what `down` removes. Kept entries are each app's working directory with its `state/`, and the stored token; only `wipe` removes them. The lock files are never removed, since deleting a lock file another process holds open would let a third process lock a different file of the same name.

App names, like sandbox names, are DNS labels of at most 63 characters, and `nginx` is not one because the nginx unit takes that place in the unit names. Sockets cannot live under the data directory: a Unix socket path must fit `sun_path`, 108 bytes with its terminating NUL, and a long home directory alone could exceed that. They live instead in the user manager's runtime directory, `/run/user/<uid>`, keyed by the sandbox's port, which no other sandbox holds while this one has it. The longest possible path, with uid 4294967294, port 7499 and a 63-character app name, is 102 bytes. sandbox only names these paths in what it generates; systemd creates the parent directories when it binds a socket; it removes the socket when the socket unit stops only because the socket unit D04 generates sets `RemoveOnStop=yes`, which is not systemd's default.

The registry is one small JSON file, rewritten whole and swapped in by rename so a reader never sees half of it. Every change to it is made under the registry lock, which is what makes port allocation one sandbox at a time: a first `up` takes the lowest port from 7400 to 7499 that no entry holds, and the sandbox keeps it until `wipe`. Commands that change a sandbox (`up`, `down`, `wipe`, `token set`) also hold that sandbox's own lock for as long as they run programs or write its files, so a second such command waits and then runs in full. Both locks are `flock(2)` locks on their lock files; such a lock belongs to the open file description, so two commands in one process exclude each other exactly as two processes do. Commands that only report take no lock.

Unit names join the sandbox name and the app name with `-`, and both may contain `-`, so sandbox `a-b` with app `c` and sandbox `a` with app `b-c` would both own `sandbox-a-b-c.service`. The names stay as they are; `up` instead refuses a sandbox whose units would clash with another registered sandbox's, before it builds or allocates anything.

Whether a sandbox is up is read from systemd each time, never stored, by its state run: it is up while its nginx unit is active, so after a reboot or logout every sandbox reads down.

Every path sandbox writes into a unit file, or prints in a diagnostic, starts at the worktree or at a root found from `HOME` and the XDG variables. A control character (a byte below 0x20, or 0x7F) in one of those would split a unit file's line or a diagnostic's, so a worktree or a needed root holding one is refused before anything is touched, and the refusal prints the path with each such byte escaped as `\xHH`, as a printed name (D02) does. A worktree that is not valid UTF-8 is refused the same way, since Go's JSON encoding would rewrite its invalid bytes and the registry entry would never match it again; the refusal escapes those bytes as `\xHH` too. The same escaping keeps the not-in-checkout and other-worktree diagnostics on one line, since the current directory and a registry's recorded worktree are not checked.

A path in a unit file is written in one of three forms, by where it stands, because systemd reads each position differently. Every position expands `%` specifiers, so `%` is always doubled. An `Exec...=` command line is unquoted as systemd.syntax(7) describes, so an executable or argument is enclosed in double quotes, which keeps a space inside one word. systemd substitutes `$` variables in the arguments, where `$$` stands for `$`, but never in the executable path, where `$$` would stay two characters. A path setting (`WorkingDirectory=`, `EnvironmentFile=`, `PIDFile=`, `ListenStream=`) takes the rest of the line as the path, so it is written bare. systemd refuses an executable path holding `"`, `'` or `\` however it is written, ignores any setting whose value is not UTF-8 clean (invalid UTF-8 or a Unicode noncharacter), and the env file cannot hold some characters either; every path in a unit file or env file lies under the state root or is a socket path, so a state root holding any of those is refused up front like one holding a control character, rather than giving units that fail to load, and no quote or backslash ever needs escaping in a unit file. The refusal escapes each byte that is not valid UTF-8 as `\xHH`, so the line names the root legibly. nginx's executable is the bare name `nginx`, and the worktree reaches no unit file, so neither needs that refusal. Whether a sandbox's units are active is learned by one shape of run, the unit state run, which `up`, `url`, `wipe` and `ls` make for the nginx unit and `down` and `status` make for every unit they report or stop.

## REQUIREMENTS

### Finding the sandbox

- R-EIIF-59MV: The commands `up`, `url`, `status`, `logs`, `token`, `token set`, and `down` and `wipe` given no name (the worktree commands) MUST find their worktree by running, through `Deps.Exec`, a `seam.Cmd` whose `Path` is `git`, whose `Args` are exactly `rev-parse` and `--show-toplevel`, and whose `Dir` is `Deps.Dir`; the worktree is that run's `Stdout` with its trailing newline removed.

- R-EJQB-J1DK: `down` and `wipe` given a name, and `ls`, MUST NOT run `git` through either runner.

- R-YLKH-8DP7: When the `git rev-parse --show-toplevel` run of a worktree command exits non-zero, the command MUST write the not-in-checkout diagnostic, exactly `sandbox: '<Deps.Dir>' is not inside a git checkout` and a newline, with `Deps.Dir` written as a printed name (D02 R-MBDF-W0VP), to stderr, nothing to stdout, and exit 2, running nothing further through either runner and creating no file or directory, verified at least by `Deps.Dir` `/tmp/a`, a newline and `b` writing `sandbox: '/tmp/a\x0ab' is not inside a git checkout` as one line.

- R-CVT7-DMPU: When `Deps.Dir` is empty, a worktree command MUST write the no-current-directory diagnostic, exactly `sandbox: the current directory no longer exists` and a newline, to stderr, nothing to stdout, and exit 2, running no program through either runner and creating no file or directory.

- R-LH3U-724E: When `Deps.Exec` returns an error for the `git rev-parse --show-toplevel` run, the command MUST fail with D02's runner-error diagnostic with the action `git rev-parse --show-toplevel`.

- R-ENE0-OCLN: A sandbox name MUST be 1 to 63 characters, each one of `a`–`z`, `0`–`9` or `-`, neither beginning nor ending with `-` and with no two `-` adjacent.

- R-0I4N-RIZW: An app name MUST be usable exactly when it is 1 to 63 characters, each one of `a`–`z`, `0`–`9` or `-`, neither beginning nor ending with `-`, and is not `nginx`.

- R-EOLX-24CC: A worktree command MUST derive its sandbox's name from the last path element of the worktree by lowercasing ASCII `A`–`Z`, replacing every maximal run of characters other than `a`–`z` and `0`–`9` (any non-ASCII character included) with one `-`, and dropping a leading and a trailing `-`, so `Feature_X` gives `feature-x` and `wip` gives `wip`.

- R-LIBQ-KTV3: When the derived name is empty or longer than 63 characters, the worktree command MUST write the unusable-name diagnostic, `sandbox: worktree '<last path element of the worktree>' does not give a usable sandbox name`, one empty line, and `a name needs a letter or digit and at most 63 characters`, to stderr, nothing to stdout, and exit 2, running nothing further through either runner and creating no file or directory.

- R-LJCV-IO94: When a worktree command's worktree holds a control character, a byte below 0x20 or the byte 0x7F, or is not valid UTF-8 (as Go's `utf8.Valid` decides), the command MUST write the worktree-control diagnostic, exactly `sandbox: worktree '<worktree>' holds a control character or is not valid UTF-8` and a newline, with the worktree written as a printed name (D02 R-MBDF-W0VP), except that each byte Go's `utf8.DecodeRune` reads as an invalid encoding of width 1 is also written as `\x` and two lowercase hexadecimal digits, to stderr, nothing to stdout, and exit 2, running nothing further through either runner and creating no file or directory, verified at least by a worktree `/tmp/a`, a newline and `b` writing `sandbox: worktree '/tmp/a\x0ab' holds a control character or is not valid UTF-8` as one line, by a worktree `/tmp/a`, the byte 0xFF and `b` writing `sandbox: worktree '/tmp/a\xffb' holds a control character or is not valid UTF-8`, by a worktree `/tmp/a`, the byte 0xFF and `/b` writing `sandbox: worktree '/tmp/a\xff/b' holds a control character or is not valid UTF-8`, by a worktree `/tmp/caf` followed by the byte 0xC3 alone writing `sandbox: worktree '/tmp/caf\xc3' holds a control character or is not valid UTF-8`, and by the worktrees `/tmp/x`, U+0085 (the bytes 0xC2 0x85) and `y`, and `/tmp/caf` followed by `é` (the bytes 0xC3 0xA9), each not refused by this check.

- R-LKKR-WFZT: A worktree command MUST make its checks in this order and report only the first that fails: no current directory, not inside a git checkout, worktree holds a control character or is not valid UTF-8 (R-LJCV-IO94), unusable name, a root the command needs falls back to a `HOME` that is not an absolute path (the HOME diagnostic), the root character check, unknown sandbox, belongs to another worktree; a `HOME` that is not absolute fails no check while no root the command needs falls back to it, verified at least by `status` with an absolute `XDG_STATE_HOME`, a relative `HOME` and a sandbox the registry does not know reporting the unknown-sandbox diagnostic.

- R-IG77-FAZI: `down` and `wipe` given a name MUST, before they read the registry, make these checks in this order and report only the first that fails: the name is a sandbox name, a root they need falls back to a `HOME` that is not an absolute path (the HOME diagnostic), the root character check.

- R-IHF3-T2Q7: `ls` MUST, before it reads the registry, make these checks in this order and report only the first that fails: its state root falls back to a `HOME` that is not an absolute path (the HOME diagnostic), the root character check; verified at least by `ls` with an absolute `XDG_STATE_HOME` and an empty `HOME` not writing the HOME diagnostic.

- R-0JCK-5AQL: `down` and `wipe` given a name, and `ls`, MUST behave the same whatever `Deps.Dir` holds, the empty string included, asking the runners for identical `seam.Cmd` values whatever `Deps.Dir` holds.

### Directories

- R-0KKG-J2HA: The state root MUST be the value of `XDG_STATE_HOME` read through `Deps.Getenv` when that value is an absolute path, and otherwise the value of `HOME` followed by `/.local/state`; in both cases cleaned lexically, as Go's `path.Clean` does.

- R-0LSC-WU7Z: The config root MUST be the value of `XDG_CONFIG_HOME` read through `Deps.Getenv` when that value is an absolute path, and otherwise the value of `HOME` followed by `/.config`; in both cases cleaned lexically, as Go's `path.Clean` does.

- R-LJJM-YLLS: When a command needs a root that falls back to `HOME` and the value of `HOME` is not an absolute path (unset, empty or relative), it MUST write the HOME diagnostic, exactly `sandbox: HOME is not an absolute path` and a newline, to stderr, nothing to stdout, and exit 2, creating no file or directory and running nothing through either runner after its `git rev-parse --show-toplevel` run.

- R-XR2Z-O45U: The root character check MUST refuse a command when a root it needs holds a byte below 0x20 or the byte 0x7F, or when its state root holds `"`, `'` or `\`, is not valid UTF-8 (as Go's `utf8.Valid` decides) or holds a character an env file cannot hold (D05 R-UMQT-KF5V), the roots every command but help and version needs being the state root and, for `up`, `down` and `wipe`, also the config root, the state root checked first; the command MUST write the root-character diagnostic, exactly `sandbox: <directory> '<root>' holds a control character, quote, backslash or a character systemd rejects` and a newline, where `<directory>` is `state directory` for the state root and `config directory` for the config root and `<root>` is that root written as a printed name (D02 R-MBDF-W0VP), except that each byte Go's `utf8.DecodeRune` reads as an invalid encoding of width 1 is also written as `\x` and two lowercase hexadecimal digits, to stderr, nothing to stdout, and exit 2, creating no file or directory and running nothing through either runner after its `git rev-parse --show-toplevel` run, verified at least by `HOME` `/home/a`, a tab and `b` with `XDG_STATE_HOME` unset writing `sandbox: state directory '/home/a\x09b/.local/state' holds a control character, quote, backslash or a character systemd rejects`, by `HOME` `/home/q"r` with `XDG_STATE_HOME` unset refusing `ls` with `sandbox: state directory '/home/q"r/.local/state' holds a control character, quote, backslash or a character systemd rejects`, by `XDG_STATE_HOME` `/tmp/q"r`, `/tmp/it's` and `/tmp/a\b` each refusing `up` and `ls`, by `XDG_STATE_HOME` `/tmp/a`, the byte 0xFF and `b` refusing `ls` with `sandbox: state directory '/tmp/a\xffb' holds a control character, quote, backslash or a character systemd rejects`, by `XDG_STATE_HOME` `/tmp/a` followed by U+FDD0 (the bytes 0xEF 0xB7 0x90) refusing `ls`, by `XDG_STATE_HOME` `/tmp/x`, U+0085 (the bytes 0xC2 0x85) and `y`, and `/tmp/caf` followed by `é` (the bytes 0xC3 0xA9), each not refusing `ls`, and by `up` with an absolute `XDG_STATE_HOME` free of these characters and `XDG_CONFIG_HOME` `/tmp/c`, a newline and `d` writing `sandbox: config directory '/tmp/c\x0ad' holds a control character, quote, backslash or a character systemd rejects` as one line, while `ls` in that environment is not refused and `up` with `XDG_CONFIG_HOME` `/tmp/q"r` is not refused by this check.

- R-EVXB-CQSI: `ls`, `url`, `status`, `logs`, `token` and `token set` MUST NOT need the config root: while `XDG_STATE_HOME` is absolute they behave the same whatever `HOME` and `XDG_CONFIG_HOME` hold.

### Layout

- R-8A9R-GL9H: The data root MUST be `<state root>` joined with `ikigenba/sandbox` and cleaned lexically, as Go's `path.Join` does, written `<root>` below; with a state root of `/` it is `/ikigenba/sandbox`.

- R-EZL0-I20L: The registry MUST be the file `<root>/registry.json`.

- R-F0SW-VTRA: The registry lock file MUST be `<root>/registry.json.lock`.

- R-F20T-9LHZ: A sandbox's lock file MUST be `<root>/<name>.lock`, where `<name>` is the sandbox name.

- R-F38P-ND8O: A sandbox's data directory MUST be `<root>/<name>`, written `<data>` below.

- R-F4GM-14ZD: An app's built binary, the program its service runs, MUST be `<data>/bin/<app>`.

- R-F5OI-EWQ2: The build staging directory MUST be `<data>/build`, and an app's staged build MUST be `<data>/build/<app>`.

- R-F6WE-SOGR: An app's env file MUST be `<data>/env/<app>.env`, with mode 0600.

- R-F84B-6G7G: A sandbox's services file MUST be `<data>/services.json`.

- R-F9C7-K7Y5: A sandbox's nginx directory MUST be `<data>/nginx`.

- R-FAK3-XZOU: A sandbox's nginx configuration file MUST be `<data>/nginx/nginx.conf`.

- R-YQG2-RGNZ: A sandbox's nginx pid file MUST be `<data>/nginx/nginx.pid`.

- R-FBS0-BRFJ: An app's directory, the working directory of its service, MUST be `<data>/apps/<app>`.

- R-FCZW-PJ68: An app's state directory MUST be `<data>/apps/<app>/state`.

- R-FE7T-3AWX: A sandbox's stored token MUST be the file `<data>/token`, with mode 0600.

- R-8BHN-UD06: The unit directory MUST be `<config root>` joined with `systemd/user` and cleaned lexically, as Go's `path.Join` does, written `<units>` below; with a config root of `/` it is `/systemd/user`.

- R-FHVI-8M50: An app's socket unit MUST be `<units>/sandbox-<name>-<app>.socket`.

- R-FJ3E-MDVP: An app's service unit MUST be `<units>/sandbox-<name>-<app>.service`.

- R-FKBB-05ME: A sandbox's nginx unit MUST be `<units>/sandbox-<name>-nginx.service`.

- R-FLJ7-DXD3: An app's socket path MUST be `/run/user/<EUID>/sandbox/<port>/<app>.sock`, where `<EUID>` is `Deps.EUID` in decimal and `<port>` is the sandbox's port.

- R-FMR3-RP3S: An app's socket path MUST be the same whatever `XDG_RUNTIME_DIR`, `HOME`, `XDG_STATE_HOME` and `XDG_CONFIG_HOME` hold, and whatever the sandbox's name is.

- R-FNZ0-5GUH: A sandbox's generated entries MUST be `<data>/bin`, `<data>/build`, `<data>/env`, `<data>/services.json`, `<data>/nginx`, and its unit files in `<units>`.

- R-FP6W-J8L6: A sandbox's kept entries MUST be `<data>/apps` and `<data>/token`.

- R-FQES-X0BV: sandbox MUST NOT leave in a data directory, when a command returns, any entry it created other than `bin`, `build`, `env`, `services.json`, `nginx`, `apps` and `token`.

- R-LG0T-Y097: A command other than `wipe` MUST NOT remove `<data>/apps`, an app's directory, or anything under one.

- R-LH8Q-BRZW: A command other than `wipe` and `token set` MUST NOT change or remove `<data>/token`.

- R-LIGM-PJQL: A command acting on a sandbox MUST NOT create, change or remove any file in `<units>` whose name is not one of that sandbox's unit names, taken over its recorded apps and, for `up`, the apps `up` discovered; it never selects unit files by prefix, so acting on `wip` leaves `sandbox-wip-x-auth.service` of sandbox `wip-x` untouched.

- R-LJOJ-3BHA: sandbox MUST NOT create, change or remove any file or directory outside `<root>` and `<units>`, except that it MAY create missing parent directories of `<root>` and of `<units>`.

- R-FVAE-G3AN: `ls`, `url`, `status`, `logs` and `token` MUST NOT create, change or remove any file or directory.

### Registry

- R-FWIA-TV1C: `registry.json` MUST hold one JSON object whose only member is `sandboxes`, an array of entries, each an object whose members are exactly `name` (string, the sandbox name), `port` (number, the sandbox's port), `worktree` (string, the worktree the sandbox belongs to) and `apps` (array of the sandbox's recorded apps, each an object whose members are exactly `name`, a string, and `default`, a boolean); `apps` is an empty array when none is recorded.

- R-FXQ7-7MS1: Every write of `registry.json` MUST order its entries by `name` ascending and each entry's `apps` by `name` ascending.

- R-LKWF-H37Z: A sandbox MUST be known exactly when `registry.json` holds an entry with its name.

- R-LM4B-UUYO: A missing `registry.json` MUST mean that no sandbox is known.

- R-LNC8-8MPD: Reading the registry MUST NOT create `registry.json`.

- R-8CPK-84QV: When `registry.json` cannot be read, is not valid JSON, holds a declared member with a different JSON type, lacks a declared member in the object, an entry or a recorded app, holds an entry whose `name` is not a sandbox name or whose `port` is outside the port range, holds a recorded app whose `name` is not a usable app name, holds an entry recording two apps with the same `name` or more than one app whose `default` is true, or holds two entries with the same `name` or the same `port`, the command that reads it MUST fail with D02's file-error diagnostic for the path `<root>/registry.json` and write nothing to stdout.

- R-LPS1-066R: Every write of `registry.json` MUST replace it by renaming a complete file over it, so a hard link made to the earlier `registry.json` still holds the earlier content.

- R-G3TP-4HHI: sandbox MUST NOT leave in `<root>`, when a command returns, any entry it created other than `registry.json`, `registry.json.lock`, files named `<name>.lock`, and directories named `<name>`, for sandbox names `<name>`.

- R-LQZX-DXXG: A write that leaves no sandbox MUST write `registry.json` with an empty `sandboxes` array.

- R-LS7T-RPO5: A command MUST NOT remove `registry.json` once it exists.

- R-G69H-W0YW: Once a registry entry is written, a command MUST NOT change its `name`, `port` or `worktree`, and a command other than `wipe` MUST NOT remove it.

### Unknown sandboxes and ownership

- R-LLZF-Q536: When the registry knows no sandbox of the derived name, `url`, `status`, `logs`, `token`, `token set`, and `down` and `wipe` given no name, MUST write the unknown-sandbox diagnostic, `sandbox: no sandbox '<name>'`, one empty line, and `run 'sandbox up' to create it`, to stderr, nothing to stdout, and exit 2.

- R-Y5PS-9D26: When `down` or `wipe` is given a name the registry does not know, or a name that is not a sandbox name, it MUST write the unknown-name diagnostic, `sandbox: no sandbox '<name>'`, with `<name>` the name as given written as a printed name (D02 R-MBDF-W0VP), one empty line, and `run 'sandbox ls' to see every sandbox`, to stderr, nothing to stdout, and exit 2, verified at least by `down` given `a`, a newline and `b` writing `sandbox: no sandbox 'a\x0ab'` as its first line.

- R-YMSD-M5FW: When the registry's entry for the derived name records a worktree other than this command's worktree, whether or not that path exists, every worktree command, `up` included, MUST write the other-worktree diagnostic, `sandbox: sandbox '<name>' belongs to another worktree: <recorded worktree>`, with the recorded worktree written as a printed name (D02 R-MBDF-W0VP), one empty line, and `rename this worktree, or wipe that sandbox with 'sandbox wipe <name>' once it is down`, to stderr, nothing to stdout, and exit 2.

- R-GB53-F3XO: `down` and `wipe` given a name MUST act on that registry entry whatever worktree it records and whether or not that path exists.

- R-GCCZ-SVOD: A command refused as an unknown sandbox or as belonging to another worktree MUST create no file or directory, read nothing from stdin, and run nothing through either runner other than its `git rev-parse --show-toplevel` run, unless the registry changed while the command waited for the sandbox lock.

### Locks

- R-D4CI-20WP: While a command holds a lock, `flock(2)` with `LOCK_EX|LOCK_NB` on a separate open of that lock file MUST fail with `EWOULDBLOCK`, also when called from the same process.

- R-D5KE-FSNE: Once a command has returned, `flock(2)` with `LOCK_EX|LOCK_NB` on a separate open of any lock file it took MUST succeed.

- R-D6SA-TKE3: A command MUST acquire a lock by a blocking `flock(2)` with `LOCK_EX` on its own open of the lock file, so while it waits it is a blocked `flock` waiter on that file.

- R-GESS-KF5R: A command that takes a lock MUST create the lock file, and any missing parent directory, when absent.

- R-GG0O-Y6WG: A command MUST NOT remove `registry.json.lock` or any `<name>.lock`; `wipe` leaves the wiped sandbox's lock file in place.

- R-D807-7C4S: `up`, `down`, `wipe` and `token set` MUST hold the sandbox lock of the sandbox they act on whenever they run a program through either runner other than their `git rev-parse --show-toplevel` run.

- R-D983-L3VH: `token set` MUST hold the sandbox lock of its sandbox whenever it reads stdin.

- R-DAFZ-YVM6: Once `up`, `down`, `wipe` or `token set` has acquired a sandbox lock, it MUST hold it continuously until it returns, so the runner calls two such commands on one sandbox make while holding its lock never interleave.

- R-DBNW-CNCV: A command MUST NOT wait for a sandbox lock while it holds the registry lock.

- R-GIGH-PQDU: `up`, `down`, `wipe` and `token set` MUST NOT write the registry, write or remove any file under the sandbox's data directory, or write or remove any of its unit files while another holder has the sandbox lock.

- R-LTFQ-5HEU: A command that finds a lock it needs held by another holder MUST wait until that holder releases it, rather than fail, and then complete as it would have had the lock been free.

- R-LVVI-X0W8: A command waiting for a lock MUST write nothing to stdout or stderr while it waits.

- R-GM46-V1LX: Once `up`, `down`, `wipe` or `token set` holds the sandbox lock, its unknown-sandbox and belongs-to-another-worktree checks and every later decision MUST use the registry as read after the lock was acquired.

- R-GNC3-8TCM: Every change to `registry.json` MUST be made under the registry lock, from the read it is based on until the replacement is in place, so a change another process makes while the command waits for the registry lock is kept, never overwritten.

- R-GOJZ-ML3B: A command MUST NOT hold the registry lock while it runs a program through either runner.

- R-GPRW-0CU0: `ls`, `url`, `status`, `logs` and `token` MUST take neither lock, completing while another holder has the registry lock and every sandbox lock.

### Ports

- R-GQZS-E4KP: The port range MUST be 7400 through 7499 inclusive.

- R-GS7O-RWBE: A sandbox not yet in the registry MUST be given the lowest port in the port range that no registry entry holds, as read under the registry lock.

- R-LPN4-VGB9: When every port in the port range is held by a registry entry, giving a port MUST fail with the no-free-port diagnostic, `sandbox: no free port: every port from 7400 to 7499 belongs to a sandbox`, one empty line, and `run 'sandbox ls' and wipe a sandbox you no longer need`, on stderr, nothing on stdout, exit 2, and `registry.json` unchanged.

### Up or down

- R-0O85-ODPD: Every run of `systemctl` or `journalctl` through either runner MUST have `Dir` `/`.

- R-ICJI-9ZRF: A unit state run of a unit, one of a sandbox's unit names, MUST be a run through `Deps.Exec` of a `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are exactly `--user`, `show`, `--property=ActiveState`, `--value` and the unit's name, and whose `Dir` is `/`.

- R-IDRE-NRI4: A sandbox's state run MUST be the unit state run of its nginx unit, so for sandbox `wip` it asks `Deps.Exec` for exactly the `seam.Cmd` whose `Path` is `systemctl`, whose `Args` are `--user`, `show`, `--property=ActiveState`, `--value` and `sandbox-wip-nginx.service`, and whose `Dir` is `/`.

- R-LX3F-ASMX: A sandbox MUST be up when its state run exits 0 and its `Stdout`, with a trailing newline removed, is `active`, `reloading` or `refreshing`, and down when its state run exits 0 with any other `Stdout`.

- R-LYBB-OKDM: When a state run exits non-zero, the command MUST report neither up nor down and MUST fail with D02's external-failure diagnostic under the action its own document names.

- R-LZJ8-2C4B: When `Deps.Exec` returns an error for a state run, the command MUST report neither up nor down and MUST fail with D02's runner-error diagnostic under the action its own document names.

### Addresses

- R-0QNY-FX6R: An app's origin MUST be `http://<app>.<name>.localhost:<port>`, with `<name>` the sandbox name and `<port>` the sandbox's port, and no trailing `/`.

- R-0RVU-TOXG: A sandbox's default origin MUST be `http://<name>.localhost:<port>`, with no trailing `/`.

- R-0T3R-7GO5: A sandbox's callback origin MUST be `http://localhost:<port>`, with no trailing `/`.

### Unit names

- R-LTAU-0RJC: The unit names of a sandbox MUST be the name of its nginx unit and the names of the socket unit and the service unit of each of its apps, where an app's and the nginx unit's names are the file names of the units declared above; for a registry entry, its apps are its recorded apps.

- R-DFBL-HYKY: After any command on a sandbox N returns, every unit file sandbox has written for N in `<units>` MUST be named by one of N's unit names, taken over N's recorded apps.

- R-LUIQ-EJA1: When `up` of a sandbox N would give N, with the apps `up` discovered, a unit name that is also a unit name of another registry entry M, `up` MUST write the unit-clash diagnostic, `sandbox: unit '<unit>' would also belong to sandbox '<M>'`, one empty line, and `rename this worktree or the app, or wipe sandbox '<M>' once it is down`, to stderr, nothing to stdout, and exit 2, building, starting and allocating nothing and writing no file other than a lock file, so `registry.json` is unchanged.

- R-LVQM-SB0Q: The unit-clash diagnostic MUST name as `<unit>` the clashing unit name lowest in byte order and as `<M>` the lowest-named registry entry holding it, so `up` of sandbox `a` with app `b-c`, beside an entry `a-b` recording app `c`, writes `sandbox: unit 'sandbox-a-b-c.service' would also belong to sandbox 'a-b'`.

- R-LWYJ-62RF: `up` MUST make the unit-clash check against the registry as read under the registry lock, so an entry another process records while `up` waits for that lock takes part in the check.

### Unit file values

- R-XG3W-86HL: A path written in a generated unit file as an argument of an `Exec...=` command line (unit-quoted as an argument) MUST be written as one word enclosed in `"`, inside which every `%` of the path is written `%%`, every `$` is written `$$`, and no other character is changed, verified at least by `/tmp/a b%c$d` written `"/tmp/a b%%c$$d"`; every such path lies under the state root, which the root character check keeps free of `"`, `'` and `\`.

- R-XHBS-LY8A: A path written in a generated unit file as the executable of an `Exec...=` command line (unit-quoted as an executable) MUST be written as one word enclosed in `"`, inside which every `%` of the path is written `%%` and no other character is changed, `$` included, verified at least by `/tmp/a b%c$d` written `"/tmp/a b%%c$d"`; every such path lies under the state root, which the root character check keeps free of `"`, `'` and `\`.

- R-XJRL-DHPO: A path written in a generated unit file as the whole value of a path setting (`WorkingDirectory=`, `EnvironmentFile=`, `PIDFile=` or `ListenStream=`), written as a unit path value, MUST be the path with every `%` written `%%` and no other character changed, not enclosed in quotes, verified at least by `/tmp/a b%c$d` written `/tmp/a b%%c$d`; every such path is a socket path or lies under the state root, which the root character check keeps free of `"`, `'` and `\`.
