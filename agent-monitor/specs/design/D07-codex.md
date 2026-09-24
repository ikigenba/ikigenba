# D07-codex

`internal/harness/codex` answers `agent-monitor list codex`: which Codex
threads on this machine are live roots, and what each one's STATUS, LAST
ACTIVE, CWD, and TITLE are. It exports one function, `List`, with the same
shape as the other harness packages, and it reaches the machine only through
the `fs.FS` and the home directory it is handed, so a test drives it with a
`testing/fstest.MapFS` and nothing else.

Codex has no registry of running threads. What it has is a lock: a loaded
thread holds an exclusive lock on
`$HOME/.codex/thread-writer-locks/<thread-id>.lock`, and the kernel lists
every held lock in `/proc/locks` with the device and inode of the locked file
and the pid of the holder. So a thread is live exactly when the lock file's
device and inode appear among the held write locks. `internal/proc`
(`D05-process-facts`) owns reading `/proc`: `proc.LockHolders` turns
`/proc/locks` into a map from `proc.FileID` to holder pid, and `proc.FileIDOf`
turns the `fs.FileInfo` of the lock file into a `proc.FileID`. The lock file
itself is only ever stat'ed: it is never opened, read, or locked, so looking
cannot disturb the thread. A lock file nobody holds, as a crashed Codex leaves
behind, is simply not live. The lock directory also holds Codex's own
`.coordination.lock`, which is not a thread; a name beginning with `.` is
never a thread id.

Everything else about a thread comes from its rollout,
`$HOME/.codex/sessions/YYYY/MM/DD/rollout-<time>-<thread-id>.jsonl`, found by
listing the three date levels and matching the name, and from
`$HOME/.codex/session_index.jsonl`. The rollout's first record is
`session_meta`; its `payload.cwd` is CWD, and a subagent carries the id of the
thread that spawned it as the story's `parent_thread_id`, observed both at
`payload.parent_thread_id` and at
`payload.source.subagent.thread_spawn.parent_thread_id`; either one keeps a
subagent out of the list. STATUS is decided by the last of the rollout's `event_msg` records whose
`payload.type` is `task_started`, `task_complete`, or `turn_aborted`. LAST
ACTIVE is the latest top-level `timestamp`, under the shared definitions of a
record and a timestamp in `D04-sessions-and-table`. TITLE is the
`thread_name` of the last line of `session_index.jsonl` for the thread; Codex
appends a new line when a thread is renamed, so the last line is the current
name.

Each field falls back on its own. A thread whose rollout does not exist yet,
cannot be read, or holds no complete record is still listed, as `unknown`
with no LAST ACTIVE and with the working directory of the process that holds
its lock as CWD; its TITLE is still read from the index. A half-written last
line of either file is ignored, because only complete lines are records
(`session.Lines`). Only a lock directory that exists but cannot be listed, or
a `/proc/locks` that cannot be read, fails the whole listing, as a
`*session.ReadError` naming the path tried. A lock directory that does not
exist means Codex has never run here: the answer is no sessions, and nothing
else is read.

## REQUIREMENTS

- R-YG7H-5IZ5: The package `internal/harness/codex` (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/harness/codex`) MUST export exactly one identifier, the function `List(root fs.FS, home string) ([]session.Session, error)`, where `session` is `internal/session` (`D04-sessions-and-table`).
- R-YIN9-X2GJ: `List` MUST treat the lock directory as the absolute path `path.Join("/", home, ".codex", "thread-writer-locks")`, the sessions directory as `path.Join("/", home, ".codex", "sessions")`, and the index as `path.Join("/", home, ".codex", "session_index.jsonl")`, and MUST name each of them, and every path below them, to `root` as that absolute path without its leading `/` (for `home` `/home/dev`, the names `home/dev/.codex/thread-writer-locks`, `home/dev/.codex/sessions`, and `home/dev/.codex/session_index.jsonl`).
- R-YJV6-AU78: When listing the lock directory through `root` fails with an error satisfying `errors.Is(err, fs.ErrNotExist)`, `List` MUST return a slice of length zero and a nil error, and MUST pass to `root` no name other than the lock directory's own; in particular it MUST NOT read `proc/locks`, the sessions directory, or the index.
- R-0AI3-54UD: When listing the lock directory through `root` fails with any error not satisfying `errors.Is(err, fs.ErrNotExist)`, `List` MUST return a nil slice and an error whose dynamic type is `*session.ReadError`, whose `Path` is the lock directory's absolute path (for `home` `/home/dev`, `/home/dev/.codex/thread-writer-locks`), and whose `Err` is as `R-H0PS-DNFS` (`D04-sessions-and-table`) requires for that failure.
- R-0BPZ-IWL2: When the lock directory can be listed, `List` MUST call `proc.LockHolders(root)` (`D05-process-facts`) whether or not the directory holds any entry, and when that call returns an error `List` MUST return a nil slice and an error whose dynamic type is `*session.ReadError`, whose `Path` is `/proc/locks`, and whose `Err` is as `R-H0PS-DNFS` (`D04-sessions-and-table`) requires, taking the failure to be the `*fs.PathError` that the returned error is or wraps when it is or wraps one.
- R-YNIV-G5FB: `List` MUST return a nil error whenever the lock directory can be listed and `proc.LockHolders(root)` returns a nil error, whatever else fails to be read.
- R-YOQR-TX60: A lock entry is an entry of the lock directory whose name is `<id>.lock` where `<id>` is non-empty and does not begin with `.`; that `<id>` is the entry's thread id. `List` MUST ignore every other entry of the lock directory, `.coordination.lock` included, and MUST NOT look for a rollout or an index line for it.
- R-YR6K-LGNE: A lock entry is live if and only if `fs.Stat(root, name)` on the entry's name returns a nil error, `proc.FileIDOf` of the returned `fs.FileInfo` reports true, and the map returned by `proc.LockHolders(root)` holds the resulting `proc.FileID`; the value the map holds for it is the entry's holder pid. `List` MUST NOT return a session for a lock entry that is not live, and MUST NOT return an error because a lock entry is not live or cannot be stat'ed.
- R-YSEG-Z8E3: `List` MUST return exactly one session for each live lock entry that is not a subagent, with `ID` equal to the entry's thread id, and no other session.
- R-082A-DLCZ: `List` MUST obtain the metadata of an entry of the lock directory only through `fs.Stat(root, name)`, and MUST NOT lock, write, or read the content of any entry of the lock directory; when `root` implements `fs.StatFS`, `List` MUST NOT pass the name of any entry of the lock directory to `root.Open` or `root.ReadFile`.
- R-P4IW-XO2A: A thread's rollout MUST be the first file named `<sessions directory>/<year>/<month>/<day>/<file>`, where `<file>` begins with `rollout-` and ends with `-<thread id>.jsonl`, found by listing the sessions directory, then each `<year>` directory, then each `<month>` directory, then each `<day>` directory; `<year>`, `<month>`, and `<day>` directories are entries of the level above whose `fs.DirEntry` reports it is a directory (`IsDir` true; a symbolic link is not one), and at every level entries are taken in bytewise order of their names, so the first rollout is the one with the bytewise-least `<year>`, then `<month>`, then `<day>`, then `<file>`; a file at any other depth below the sessions directory is not a rollout, and a directory that does not exist or cannot be listed holds no rollout.
- R-04EL-8A4W: The rollout and the index MUST each be treated as a log in the sense of `R-HE4O-L4LF` (`D04-sessions-and-table`), and every value `List` takes from either comes from that log's records as `R-HE4O-L4LF` defines them. A rollout is usable when it is found, can be read, and holds at least one record.
- R-05MH-M1VL: A rollout whose last line is a final fragment not terminated by a newline MUST be usable when an earlier line is a record, and MUST NOT be usable when that fragment is its only content.
- R-YYHY-W33K: A thread's session meta MUST be the first record of its usable rollout when that record's top-level `type` is the JSON string `session_meta`; a usable rollout whose first record has any other `type` has no session meta, even when a later record's `type` is `session_meta`.
- R-I2R2-VX3Y: A live lock entry is a subagent when its thread's session meta holds a non-empty JSON string at `payload.parent_thread_id` or at `payload.source.subagent.thread_spawn.parent_thread_id` (either suffices); `List` MUST NOT return a session for a subagent, and a live lock entry whose rollout is not usable or has no session meta is not a subagent.
- R-P234-64KW: A returned session's `CWD` MUST be the string `encoding/json` decodes from the `payload.cwd` of its thread's session meta when that is a JSON string and the decoded string is non-empty; otherwise it MUST be the path `proc.Cwd(root, pid)` (`D05-process-facts`) returns for the entry's holder pid, or the empty string when `proc.Cwd` returns an error.
- R-Z25O-1EBN: When a returned session's rollout is usable, its `Status` MUST be decided by the last record, in file order, whose top-level `type` is the JSON string `event_msg` and whose `payload.type` is the JSON string `task_started`, `task_complete`, or `turn_aborted`: `session.StatusWorking` for `task_started`, `session.StatusIdle` for `task_complete` or `turn_aborted`; when the rollout holds no such record, its `Status` MUST be `session.StatusIdle`.
- R-06UD-ZTMA: When a returned session's rollout is usable, `List` MUST set the session's `LastActive` and `HasLastActive` from the rollout's latest timestamp under the member name `timestamp`, as `R-HFCK-YWC4` and `R-HHSD-QFTI` (`D04-sessions-and-table`) define a timestamp and the latest timestamp.
- R-I50A-PCF3: When a live lock entry's rollout is not usable (the sessions directory does not exist, no rollout is found, the rollout cannot be read, or it holds no record), `List` MUST still return a session for it, with `Status` `session.StatusUnknown`, `HasLastActive` false, and `CWD` equal to the path `proc.Cwd(root, pid)` returns for the entry's holder pid, or the empty string when `proc.Cwd` returns an error.
- R-P3B0-JWBL: A returned session's `Title` MUST be the string `encoding/json` decodes from the `thread_name` of the last record of the index, in file order, whose `id` is a JSON string equal to the session's `ID`, when that `thread_name` is a JSON string, apostrophes included; it MUST be the empty string when that record's `thread_name` is absent or not a JSON string, when no record of the index has that `id`, and when the index does not exist or cannot be read.
- R-Z719-KHAF: A returned session's `Title` MUST NOT depend on whether its rollout exists, can be read, or holds any record, and its `Status`, `LastActive`, `HasLastActive`, and `CWD` MUST NOT depend on whether the index exists, can be read, or holds a record for it.
- R-Z895-Y914: `List` MUST pass to `root` only these names: the lock directory; the names of its lock entries, to `fs.Stat` only; `proc/locks` and the names below `proc/` that `proc.LockHolders` and `proc.Cwd` read; the sessions directory and names below it down to the files of its day directories; and the index. In particular it MUST NOT read any other file under `path.Join("/", home, ".codex")`, and MUST NOT consult any environment variable such as `CODEX_HOME`.
- R-I1CL-K170: `List` MUST call no method of `root`, or of any value obtained from `root`, other than the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.ReadLinkFS`, `fs.File`, `fs.ReadDirFile`, `fs.DirEntry`, and `fs.FileInfo`; in particular it MUST NOT call a `Write`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chtimes`, or `Symlink` method on any of them.
- R-I2KH-XSXP: Every non-nil error `List` returns MUST have dynamic type `*session.ReadError`, and whenever `List` returns a non-nil error it MUST return a nil slice.
- R-I3SE-BKOE: `List` MAY return its sessions in any order.
