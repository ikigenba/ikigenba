# D07-codex

`internal/harness/codex` answers `agent-monitor list codex`: which Codex
threads on this machine are live roots, and what each one's STATUS, LAST
ACTIVE, CWD, and TITLE are. It also answers `agent-monitor tree codex
<thread-id>`: the tree of subagents one root thread started. It exports two
functions, `List` and `Tree`, with the same shapes as the other harness
packages, and it reaches the machine only through the `fs.FS` and the home
directory it is handed, so a test drives it with a `testing/fstest.MapFS` and
nothing else.

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

`Tree` starts from one thread id. Its locating directory is the sessions
directory (`D09-tree` fixes the error contract around it). A thread is a root
session when its rollout is found there, or its lock is held, and its rollout
does not mark it as a subagent the way `list` tells one; a subagent's own
thread id therefore names no session, exactly as an id that names nothing. A
thread that is live but has not written its rollout yet is found, and is drawn
as its root line alone. The root's label is its `list` TITLE, from the index;
when that is empty the root's line has no label. Liveness is `list`'s, but asked of
the one lock file `thread-writer-locks/<id>.lock`: a lock file that does not
exist or that no process holds means the thread has ended — `codex exec`
removes its lock files when it exits — and a lock that cannot be checked
leaves the root's status `unknown`. A live root's status is its `list` STATUS.

Subagents are found by reading rollouts. A parent records each thread it
spawns as an `event_msg` whose `payload.item` is a `SubAgentActivity` of kind
`started`, naming the child's `agent_thread_id` and `agent_path`; the parent
also records `completed` and `interrupted` items about the same child, and a
child records `interacted` items about its parent, but only `started` names a
child. The children of a child are named in the child's own rollout, found by
the same file-name lookup as the root's, so the tree is followed to every
depth. `tree` matches a rollout's name in full — `rollout-`, the time stamp,
the whole thread id, `.jsonl` — so a part of an id finds nothing. A child's
rollout begins with a copy of its parent's history; a `started` item whose
`payload.thread_id` names another thread is such a copy and names no child. A child's label is the last segment of its `agent_path`. Its start
moment is the `timestamp` of the `started` record: that record is already
being read to find the child, it is appended once and never changes, and it
exists even when the child's rollout does not exist yet or cannot be read.
A child's status comes from its own rollout as a root's does in `list`, except
that the three records give three words: `task_started` is `working`,
`task_complete` `done`, and `turn_aborted` — what a child stopped by its
parent records — `killed`. Codex records nothing that says a child failed, so
a Codex tree never shows `failed`. Once the root has ended, or its liveness
cannot be told, a child whose last such record is `task_started` is `unknown`,
since it is not running and whether it finished cannot be told; a child with
no such record, or whose rollout is missing or unreadable, is `unknown` too.
A rollout that cannot be read hides only the children it would name.

Every rollout and the index are read once, by one pass of a fresh
`session.Log` (`D04-sessions-and-table`), so the tree drawn now is the first
frame a later `watch` would draw. The lock file is only stat'ed, as in `list`,
so `tree` takes no lock and changes nothing.

## REQUIREMENTS

- R-UBNS-BLZW: The package `internal/harness/codex` (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/harness/codex`) MUST export the function `List(root fs.FS, home string) ([]session.Session, error)`, where `session` is `internal/session` (`D04-sessions-and-table`).
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
- R-HSS7-IYUS: The package `internal/harness/codex` MUST export the function `Tree(root fs.FS, home, id string) (tree.Tree, error)`, where `fs` is the standard library's `io/fs` and `tree` is `internal/tree` (`D09-tree`).
- R-4117-ZUHJ: `Tree` MUST take the index to be the absolute path `path.Join("/", home, ".codex", "session_index.jsonl")`, the lock directory to be `path.Join("/", home, ".codex", "thread-writer-locks")`, and the root's *lock file* to be the path of the entry named `id + ".lock"` in the lock directory, and MUST name each of them, and every path below its locating directory, to `root` as that absolute path without its leading `/` (for `home` `/home/dev` and `id` `t1`, the names `home/dev/.codex/session_index.jsonl` and `home/dev/.codex/thread-writer-locks/t1.lock`).
- R-HP4I-DNMP: When `id` is the empty string, is `.` or `..`, or contains `/`, `Tree` MUST return `tree.ErrNotFound` unless `R-52XS-OVAY` (`D09-tree`) requires it to return a `*session.ReadError`, and MUST pass to `root` no name other than the locating directory's.
- R-8J28-VOD3: When `fs.Stat` of the locating directory through `root` fails with an error satisfying `errors.Is(err, fs.ErrNotExist)`, `Tree` MUST pass to `root` no name other than the locating directory's; in particular it MUST NOT stat the lock file or read `proc/locks` or the index.
- R-4294-DM88: Within `Tree`, a thread's *tree rollout* MUST be the first file, in the order `R-P4IW-XO2A` fixes (with the locating directory as the sessions directory, and the same year, month, and day directories), whose name is exactly `rollout-`, then a 19-byte stamp `YYYY-MM-DDThh-mm-ss` whose bytes at positions 1–4, 6–7, 9–10, 12–13, 15–16, and 18–19 are each an ASCII digit, whose bytes at positions 5, 8, 14, and 17 are `-` and whose byte at position 11 is `T`, then `-`, then the thread id in full, then `.jsonl`; a file whose name ends in `-<thread id>.jsonl` but does not have exactly this form, such as one whose name only ends with a final part of the thread id, is not that thread's tree rollout, so no prefix or suffix of a thread id finds a rollout. Wherever this design speaks of a thread's rollout within `Tree` (the root's and every subagent's), it MUST mean its tree rollout.
- R-837J-WNQ2: Within `Tree`, a thread is a *subagent thread* when its rollout is usable and its session meta holds a non-empty JSON string at `payload.parent_thread_id` or at `payload.source.subagent.thread_spawn.parent_thread_id` (either suffices); a thread whose rollout is not usable, or has no session meta, is not a subagent thread.
- R-85NC-O77G: The root is *live* when `fs.Stat(root, name)` on the lock file's name returns a nil error, `proc.FileIDOf` (`D05-process-facts`) of the returned `fs.FileInfo` reports true, `proc.LockHolders(root)` returns a nil error, and the returned map holds the resulting `proc.FileID`; it is *not live* when that `fs.Stat` fails with an error satisfying `errors.Is(err, fs.ErrNotExist)`, or when every one of those conditions holds except that the map does not hold the `proc.FileID`; in every other case — that `fs.Stat` failing with any other error, `proc.FileIDOf` reporting false, or `proc.LockHolders(root)` returning an error after that `fs.Stat` succeeded — the root's liveness *cannot be told*.
- R-43H0-RDYX: Within `Tree`, the tree rollout (`R-4294-DM88`) of every thread and the index MUST each be a log in the sense of `R-3564-LW25` and `R-BC89-8UF3` (`D04-sessions-and-table`); a thread's rollout is *readable* when it is found and the one pass of a zero-value `session.Log` that `Tree` makes of it returns a nil error, and *usable* when it is readable and holds at least one record; a thread's session meta is the first record of its usable rollout when that record's top-level `type` is the JSON string `session_meta`, and a usable rollout whose first record has any other `type` has no session meta.
- R-44OX-55PM: The places that record Codex root sessions (`R-JERO-H7QH`, `D09-tree`) MUST be exactly the locating directory and the lock directory; the locating directory holds a root session with the id `id` if and only if `id`'s tree rollout (`R-4294-DM88`) is found and `id` is not a subagent thread, and the lock directory holds one if and only if the root is live and `id` is not a subagent thread, so that it holds none when the root's liveness cannot be told; so a thread whose usable rollout's session meta names a parent is held by neither place, whether or not its lock is held, and a live thread whose tree rollout is not found is found.
- R-86V9-1YY5: When `Tree` returns a nil error, the returned `Root` MUST have `ID` equal to `id`, an empty `Parent`, `HasStarted` false, and `Started` the zero `time.Time`.
- R-8835-FQOU: The returned `Root`'s `Label` MUST be the string `encoding/json` decodes from the `thread_name` of the last record of the index, in file order, whose `id` is a JSON string equal to `id`, when that `thread_name` is a JSON string; it MUST be the empty string when that record's `thread_name` is absent or not a JSON string, when no record of the index has that `id`, and when the index does not exist or is not readable, and it MUST NOT depend on the root's liveness or on its rollout.
- R-89B1-TIFJ: When the root is live, the returned `Root`'s `Status` MUST be decided, when `id`'s rollout is usable, by the last record, in file order, whose top-level `type` is the JSON string `event_msg` and whose `payload.type` is the JSON string `task_started`, `task_complete`, or `turn_aborted`: `tree.StatusWorking` for `task_started`, `tree.StatusIdle` for `task_complete` or `turn_aborted`, and `tree.StatusIdle` when the rollout holds no such record; when `id`'s rollout is not usable it MUST be `tree.StatusUnknown`.
- R-HU03-WQLH: The returned `Root`'s `Status` MUST be `tree.StatusEnded` when the root is not live, whatever its rollout and the index hold or whether they can be read.
- R-HV80-AIC6: The returned `Root`'s `Status` MUST be `tree.StatusUnknown` when the root's liveness cannot be told.
- R-45WT-IXGB: A *started item* of a readable rollout of a thread `x` is a record whose top-level `type` is the JSON string `event_msg`, whose `payload.item.type` is the JSON string `SubAgentActivity`, whose `payload.item.kind` is the JSON string `started`, whose `payload.item.agent_thread_id` is a non-empty JSON string, the thread the item *names*, and whose `payload.thread_id` is absent, not a JSON string, or a JSON string equal to `x`; a record whose `payload.thread_id` is a JSON string other than `x` (a copy of another thread's history) names no thread, a record whose `payload.item.kind` is anything else (`completed`, `interrupted`, and `interacted` among them) names no thread, and a rollout that is not readable has no started item.
- R-8CYQ-YTNM: The *subagents* of the root MUST be the smallest set of thread ids that contains every thread named by a started item of `id`'s rollout, other than `id`, and every thread named by a started item of the rollout of a member of the set, other than `id`; the returned `Subagents` MUST hold exactly one element for each subagent, whose `ID` is that thread id, and no other element, in any order.
- R-HQCE-RFDE: The *starter* of a subagent is the root when a started item of `id`'s rollout names it, and otherwise, of the subagents a started item of whose rollout names it, the one whose thread id is least in bytewise order; the subagent's element MUST have an empty `Parent` when a started item of `id`'s rollout names it or when started items of the rollouts of more than one subagent name it, and otherwise `Parent` equal to the thread id of the one subagent whose rollout names it.
- R-8FEJ-QD50: The *naming item* of a subagent is the first started item, in file order, of its starter's rollout that names it; the subagent's element MUST have `Label` equal to the part after the last `/` of the string `encoding/json` decodes from the naming item's `payload.item.agent_path` (the whole string when it holds no `/`) when that member is a JSON string, and the empty string when it is absent or not a JSON string, so that `/root/forecast/radar` gives `radar` and `/root/` gives the empty string.
- R-8GMG-44VP: A subagent's element MUST have its `Started` and `HasStarted` set, as `R-38TT-R7A8` (`D04-sessions-and-table`) requires, from the timestamp of its naming item under the member name `timestamp`, and its start moment MUST be unknown when the naming item has no timestamp under `timestamp`; its start moment MUST NOT depend on its own rollout.
- R-HWFW-OA2V: A subagent's *status record* is the last record, in file order, of its own usable rollout whose top-level `type` is the JSON string `event_msg` and whose `payload.type` is the JSON string `task_started`, `task_complete`, or `turn_aborted`; its element's `Status` MUST be `tree.StatusDone` when that record is `task_complete`, `tree.StatusKilled` when it is `turn_aborted`, and `tree.StatusWorking` when it is `task_started` and the root is live.
- R-HXNT-21TK: A subagent's element MUST have `Status` `tree.StatusUnknown` when its status record is `task_started` and the root is not live or its liveness cannot be told.
- R-HYVP-FTK9: A subagent's element MUST have `Status` `tree.StatusUnknown` when it has no status record: its own rollout is not usable (not found, not readable, or holding no record) or holds no record whose top-level `type` is `event_msg` and whose `payload.type` is `task_started`, `task_complete`, or `turn_aborted`.
- R-8KA5-9G3S: `Tree` MUST pass to `root` only these names: the locating directory and names below it down to the files of its day directories, reading the content of none of those files except the rollouts of `id` and of the root's subagents; the index; the lock file, to `fs.Stat` only; and `proc/locks`, through `proc.LockHolders`. In particular it MUST NOT list the lock directory, MUST NOT read any other file under `path.Join("/", home, ".codex")`, and MUST NOT consult any environment variable such as `CODEX_HOME`.
- R-8LI1-N7UH: `Tree` MUST obtain the lock file's metadata only through `fs.Stat(root, name)`, and MUST NOT lock, write, or read the content of the lock file; when `root` implements `fs.StatFS`, `Tree` MUST NOT pass the lock file's name to `root.Open` or `root.ReadFile`.
- R-8NXU-ERBV: `Tree` MUST call no method of `root`, or of any value obtained from `root`, other than the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.ReadLinkFS`, `fs.File`, `fs.ReadDirFile`, `io.ReaderAt`, `fs.DirEntry`, and `fs.FileInfo`; in particular it MUST NOT call a `Write`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chtimes`, or `Symlink` method on any of them.
- R-HRKB-5743: Whenever `Tree` returns a non-nil error it MUST return the zero `tree.Tree`.
