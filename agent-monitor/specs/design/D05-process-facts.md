# D05-process-facts

`internal/proc` answers the few questions about processes that deciding
whether a session is live needs: when did process N start, what directory
does it work in, and which process holds an exclusive lock on which file.
It answers them from Linux's `/proc` filesystem, documented in proc(5) —
the installed manual pages proc_pid_stat(5), proc_stat(5), proc_pid_cwd(5),
and proc_locks(5) of Linux man-pages, also published at
https://man7.org/linux/man-pages/man5/proc.5.html — read through the
`fs.FS` the command was handed, never through the operating system
directly. A test therefore states a process's facts by placing
`proc/<pid>/stat`, `proc/stat`, a `proc/<pid>/cwd` symbolic link, and
`proc/locks` in a `testing/fstest.MapFS`, and the functions cannot tell the
difference. Every name is given as `fs.ValidPath` spells it, relative to the
filesystem root, so `/proc/locks` is `proc/locks`. The package only reads.

A process's start time is the machine's boot time plus the time since boot
at which the process started. The boot time is the `btime` line of
`proc/stat`, in whole seconds since the Unix epoch. The time since boot is
field 22, `starttime`, of `proc/<pid>/stat`, in clock ticks. The tick rate
is a constant of this design: 100 ticks per second, Linux's USER_HZ, which
is what `sysconf(_SC_CLK_TCK)` reports on every architecture agent-monitor
runs on (observed with `getconf CLK_TCK`), so one tick is 10 milliseconds.
Field 2 of that line is the executable's name in parentheses, and that name
may itself contain spaces or `)`; fields are therefore counted from the
last `)` on the line, whose next field is field 3. A process that does not
exist has no `proc/<pid>` directory, and the error says so in a way
`errors.Is(err, fs.ErrNotExist)` recognises; callers read any error as "not
running".

`StartTicks` returns field 22 alone, the raw tick count, without the boot
time. A Claude Code registration records its process's start as that same
tick count (`procStart`), so comparing ticks with ticks decides pid reuse
exactly, with no rounding of `btime` and no drift if the wall clock is
stepped; `Start` remains for harnesses that record a wall-clock time.

The working directory is the target of the `proc/<pid>/cwd` symbolic link.

A lock names its file by device and inode, not by path, so matching a lock
file on disk to a line of `proc/locks` goes through `FileID`: the major and
minor device numbers and the inode. `FileIDOf` reads them from what
`fs.Stat` returns, decoding the device number the way the C library's
`major` and `minor` do on Linux. `LockHolders` reads `proc/locks` and maps
each file under an exclusive lock to the process holding it. It keeps a
line only when its access is `WRITE` (a POSIX or OFD write lock, or a BSD
exclusive lock) and its owner is a real process id — an OFD lock shows `-1`
there. A line whose number is followed by `->` describes a process blocked
waiting for a lock, not a holder, so it is left out. The device numbers in a
line are hexadecimal and the inode is decimal. A line that does not have
that shape is skipped rather than failing the whole read: the kernel's
format is not ours, and one surprising line must not hide every live Codex
thread. When `proc/locks` cannot be read at all, the cause is returned bare,
so the caller can name `/proc/locks` in its own diagnostic.

## REQUIREMENTS

- R-HLG2-VR1L: The `internal/proc` package MUST export `func Start(root fs.FS, pid int) (time.Time, error)`.
- R-F8QR-G78N: The `internal/proc` package MUST export `func StartTicks(root fs.FS, pid int) (uint64, error)`.
- R-HMNZ-9ISA: The `internal/proc` package MUST export `func Cwd(root fs.FS, pid int) (string, error)`.
- R-HNVV-NAIZ: The `internal/proc` package MUST export the struct type `type FileID struct { Major, Minor uint32; Inode uint64 }`, with exactly these fields in this order.
- R-HP3S-129O: The `internal/proc` package MUST export `func FileIDOf(fi fs.FileInfo) (FileID, bool)`.
- R-HQBO-EU0D: The `internal/proc` package MUST export `func LockHolders(root fs.FS) (map[FileID]int, error)`.
- R-D9JS-MYQ8: The package `internal/proc` (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/proc`) MUST export exactly six identifiers: the functions `Start`, `StartTicks`, `Cwd`, `FileIDOf`, and `LockHolders`, and the type `FileID` with its exported fields `Major`, `Minor`, and `Inode`, as their own requirements declare them, and no other exported identifier, method, field, constant, or variable.
- R-FB6K-7QQ1: Each of `Start`, `StartTicks`, `Cwd`, and `LockHolders` MUST open, stat, or read-link through `root` no name other than, respectively, `proc/stat` and `proc/<pid>/stat`; `proc/<pid>/stat`; `proc/<pid>/cwd`; and `proc/locks` — where `<pid>` is `strconv.Itoa(pid)` — and MUST NOT write, create, or remove anything.
- R-8ZKD-MKNC: When `pid` is positive and `root` holds a `proc/stat` with a line whose first whitespace-separated field is `btime` and whose second consists of one or more ASCII digits only and denotes an integer `B` that fits in `int64`, and a `proc/<pid>/stat` whose content, after its last byte `)`, splits by whitespace into at least 20 fields the 20th of which consists of one or more ASCII digits only and denotes an integer `T` that fits in `uint64` (field 22 of proc_pid_stat(5), counting the fields before and including the last `)` as fields 1 and 2), and `B + T/100` fits in `int64`, `Start(root, pid)` MUST return a nil error and a time for which `Compare` with `time.Unix(B + int64(T/100), int64(T%100) * 10000000)` returns 0, using the design constant of 100 clock ticks per second (USER_HZ) — so that a `proc/stat` holding `btime 1787539631` and a `proc/42/stat` of `42 (a) b) c) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 150 0 0` give `time.Unix(1787539632, 500000000)`.
- R-HTZD-K58G: When `root` has no `proc/<pid>/stat`, `Start(root, pid)` MUST return a non-nil error `err` for which `errors.Is(err, fs.ErrNotExist)` is true.
- R-A4BE-4UUD: `Start(root, pid)` MUST return a non-nil error when `pid` is not positive; when `proc/stat` or `proc/<pid>/stat` cannot be read; when `proc/stat` has no line whose first whitespace-separated field is `btime` and whose second consists of one or more ASCII digits only and fits in `int64` (so a signed value such as `-5` or `+5` fails); when `proc/<pid>/stat` has no byte `)` or has, after its last `)`, fewer than 20 whitespace-separated fields or a 20th field that is not one or more ASCII digits only or does not fit in `uint64`; or when `B + T/100` does not fit in `int64`.
- R-9206-E44Q: When `pid` is positive and `root` holds a `proc/<pid>/stat` whose content, after its last byte `)`, splits by whitespace into at least 20 fields the 20th of which consists of one or more ASCII digits only and denotes an integer `T` that fits in `uint64` (field 22, `starttime`, of proc_pid_stat(5), clock ticks since boot), `StartTicks(root, pid)` MUST return `T` and a nil error, whatever `proc/stat` holds or whether it exists — so that a `proc/42/stat` of `42 (a) b) c) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 150 0 0` gives 150.
- R-A6R6-WEBR: `StartTicks(root, pid)` MUST return a non-nil error when `pid` is not positive, when `proc/<pid>/stat` cannot be read, or when `proc/<pid>/stat` has no byte `)` or has, after its last `)`, fewer than 20 whitespace-separated fields or a 20th field that is not one or more ASCII digits only (so a signed value such as `-5` or `+5` fails) or does not fit in `uint64`; when `root` has no `proc/<pid>/stat`, that error `err` MUST satisfy `errors.Is(err, fs.ErrNotExist)`.
- R-HWF6-BOPU: `Cwd(root, pid)` MUST return exactly the string and error `fs.ReadLink(root, "proc/" + strconv.Itoa(pid) + "/cwd")` returns, so that a `proc/42/cwd` symbolic link to `/home/dev/src/tools` gives `/home/dev/src/tools` and a nil error, and a missing link gives a non-nil error.
- R-I8M6-5E4S: When `fi.Sys()` is a `*syscall.Stat_t` `st` with device number `d = uint64(st.Dev)`, `FileIDOf(fi)` MUST return true and the `FileID` with `Major = uint32(((d & 0x00000000000fff00) >> 8) | ((d & 0xfffff00000000000) >> 32))`, `Minor = uint32((d & 0x00000000000000ff) | ((d & 0x00000ffffff00000) >> 12))`, and `Inode = uint64(st.Ino)`, the Linux C library's `major` and `minor` decoding — so that `Dev` 0x10305 gives major 259 and minor 5, and `Dev` 0x0000123456789abc gives major 0x189a and minor 0x234567bc.
- R-I02V-GZXX: When `fi.Sys()` is not a `*syscall.Stat_t` (a nil `Sys()` included), `FileIDOf(fi)` MUST return false and the zero `FileID`.
- R-IC9V-APCV: A *holder line* of `proc/locks` is a line whose whitespace-separated fields `f` number at least 6, where `f[0]` ends in `:`, `f[1]` is not `->`, `f[3]` is exactly `WRITE`, `f[4]` consists of one or more ASCII digits only and denotes an integer greater than 0 that fits in `int`, and `f[5]` is three `:`-separated parts of which the first two each consist of one or more hexadecimal digits (either case) only and fit in `uint32` and the third consists of one or more ASCII decimal digits only and fits in `uint64`; for each holder line, `LockHolders(root)` MUST map the `FileID` whose `Major` and `Minor` are the first two parts read as hexadecimal and whose `Inode` is the third read as decimal to the integer `f[4]`, so that the line `1: FLOCK  ADVISORY  WRITE 1761980 103:05:42618649 0 EOF` maps `FileID{Major: 259, Minor: 5, Inode: 42618649}` to 1761980.
- R-DVNY-TNQ6: `LockHolders` MUST contribute nothing to its map for a line of `proc/locks` that is not a holder line — among them a blocked waiter such as `93: -> FLOCK  ADVISORY  WRITE 1961333 00:30:5192906 0 EOF`, a `READ` lock, an owner of `-1`, an empty line, and a line with too few fields or a malformed device and inode field — and MUST neither fail nor stop reading because of such a line, so that the map's keys are exactly the `FileID`s of the holder lines.
- R-DWVV-7FGV: When several holder lines of `proc/locks` give the same `FileID`, `LockHolders` MUST map it to the owner of the first such line in file order.
- R-IB1Y-WXM6: When `proc/locks` is readable, `LockHolders(root)` MUST return a nil error and a non-nil map, empty when the file has no holder line, an empty file included.
- R-I66D-DUNE: When reading `proc/locks` through `root` fails with an error `err`, `LockHolders(root)` MUST return a nil map and a non-nil error that is `pe.Err` when `err` is a `*fs.PathError` `pe`, and `err` otherwise, so that a permission failure returns an error whose `Error()` is `permission denied`.
