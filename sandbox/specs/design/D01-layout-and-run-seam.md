# D01-layout-and-run-seam

sandbox is a Go command-line program whose whole behaviour is reachable in-process. A test imports two packages: `internal/cli`, which holds the entry `Run` and the source-declared `Version`, and `internal/seam`, which holds the one injectable run seam every environmental dependency enters through. `cmd/sandbox` is a thin `main` that fills the seam from the real process and calls `Run`.

The seam carries the current directory, the effective uid, the environment (as a lookup function, so a test points HOME and the XDG variables at temporary directories), and two process runners: `Exec`, which runs a program to completion and captures what it wrote, and `Stream`, which hands the program's standard output to a writer as it arrives (for `logs`). Every external program sandbox drives (`git`, `go`, `systemctl --user`, `journalctl --user`, `nginx`) is reached only through these runners, so the gates run with fakes and no real machine. There is no clock in the seam: nothing in the stories depends on time, and waiting for a lock blocks rather than times out. There is no file-system abstraction either: tests build real trees under temporary directories and point the seam at them.

A process runner returns a `Result` holding the program's standard output alone (for parsing a value, such as the git toplevel or a unit's state) and, separately, everything it wrote on both streams (the bytes a diagnostic quotes, as D02 states). A non-zero exit is a result, not an error; an error means the program could not be run at all or the run was cancelled. Each program starts as the leader of its own process group. Cancelling the context, which `cmd/sandbox` does on SIGINT and SIGTERM, kills that whole group (so `go build`'s compiler children die too) and returns, so an interrupted sandbox never leaves a child running. A Ctrl-C at the terminal still reaches sandbox itself, which is in the terminal's foreground group; the children, in their own groups, are stopped through the context.

The two streams of a child are read through two pipes, so the order between a standard-output write and a standard-error write that land close together is the order sandbox read them, not necessarily the order the child wrote them; within each stream the order is exact.

Paths: sandbox never consults its own process environment, working directory or uid outside `cmd/sandbox`. Every location it reads or writes is derived from `Deps.Dir`, the worktree D03 finds, and the HOME and XDG values `Deps.Getenv` gives, and where it may write is D03's confinement rule. When `cmd/sandbox` cannot determine its working directory it passes an empty `Deps.Dir`, and D03 says what a command does then. The per-user runtime directory `/run/user/<uid>` appears only as text inside generated files; sandbox itself never touches it.

## REQUIREMENTS

- R-XWS5-WXD5: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int`, where `args` excludes the program name, and a call MUST return its exit code to the caller without terminating the calling program.

- R-XY02-AP3U: Package `internal/cli` MUST export `var Version string`, whose value MUST match `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`.

- R-XZ7Y-OGUJ: Package `internal/seam` MUST export a `Cmd` struct whose fields are exactly `Path string`, `Args []string`, and `Dir string`.

- R-Y0FV-28L8: Package `internal/seam` MUST export a `Result` struct whose fields are exactly `Stdout []byte`, `Output []byte`, and `ExitCode int`.

- R-Y1NR-G0BX: Package `internal/seam` MUST export the type `Runner func(ctx context.Context, cmd Cmd) (Result, error)` and the function `Exec(ctx context.Context, cmd Cmd) (Result, error)`, and `Exec` MUST be assignable to a `Runner`.

- R-Y2VN-TS2M: Package `internal/seam` MUST export the type `StreamRunner func(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)` and the function `Stream(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)`, and `Stream` MUST be assignable to a `StreamRunner`.

- R-Y43K-7JTB: Package `internal/seam` MUST export a `Deps` struct whose fields are exactly `Dir string`, `EUID int`, `Getenv func(key string) string`, `Exec Runner`, and `Stream StreamRunner`.

- R-Y5BG-LBK0: `cli.Run` with a nil `Deps.Getenv` MUST behave exactly as with a `Deps.Getenv` that returns the empty string for every key: for the same arguments and other `Deps`, both calls MUST return the same exit code and write the same bytes to stdout and stderr, verified at least for `ls` while the test process's own `HOME` and `XDG_STATE_HOME` name a temporary directory whose registry (D03) holds a sandbox.

- R-Y6JC-Z3AP: `cli.Run` with a nil `Deps.Exec` MUST run external programs with `seam.Exec`, verified by `url` with `Deps.Dir` a temporary directory and the test process's `PATH` holding only a fake `git` script that records its invocation and exits 128: the script MUST have run, and the not-in-checkout diagnostic D03 declares for that directory MUST be written with exit code 2.

- R-Y7R9-CV1E: `cli.Run` with a nil `Deps.Stream` MUST run streamed programs with `seam.Stream`, verified by `logs` in a temporary worktree known to a temporary registry (D03), with a `Deps.Exec` fake that answers every run as the case needs and the test process's `PATH` holding a fake `journalctl` script that writes known bytes to its standard output and exits 0: exactly those bytes MUST reach `cli.Run`'s stdout.

- R-Y8Z5-QMS3: `seam.Exec` MUST run the program named by `Cmd.Path` (looked up on the `PATH` of the sandbox process when it contains no slash) with exactly `Cmd.Args` as its arguments after the program name and `Cmd.Dir` as its working directory, verified with a script on a temporary `PATH` that writes its arguments and `pwd` to standard output.

- R-OJVX-QLEL: `seam.Exec` MUST give the program the environment of the sandbox process unchanged, except that `PWD` MAY name `Cmd.Dir`, verified by running `env` with the argument `-0` and finding the set of variables it prints, `PWD` excluded, equal to the test process's own environment, `PWD` excluded, after the test has set a variable of its own.

- R-YCMU-VY06: `seam.Exec` MUST give the program an empty standard input (reading it yields end of file at once) and MUST NOT pass it the sandbox process's standard input, verified while the test process's `os.Stdin` is a pipe holding data: a `/bin/sh -c 'cat'` child MUST produce empty `Result.Stdout`.

- R-YDUR-9PQV: `seam.Exec` MUST return in `Result.Stdout` exactly the bytes the program wrote to its standard output, and nothing it wrote to its standard error.

- R-YF2N-NHHK: `seam.Exec` MUST return in `Result.Output` exactly the bytes the program wrote to its standard output and to its standard error, merged so that the bytes of each stream keep the order in which that stream carried them; when the program writes to only one of the two streams, `Result.Output` MUST equal that stream's bytes.

- R-YGAK-1989: When the program exits with a non-zero status `n`, `seam.Exec` MUST return a `Result` whose `ExitCode` is `n` together with a nil error; when it exits with status 0, `ExitCode` MUST be 0 and the error nil.

- R-YHIG-F0YY: When the program is terminated by a signal that `seam.Exec` did not send, `seam.Exec` MUST return a `Result` whose `ExitCode` is 128 plus the signal number together with a nil error, verified by `/bin/sh -c 'kill -TERM $$'` giving `ExitCode` 143.

- R-YIQC-SSPN: `seam.Exec` MUST return a non-nil error when the program cannot be started, verified at least by a `Cmd.Path` found nowhere on `PATH` and by a `Cmd.Dir` that does not exist.

- R-YJY9-6KGC: `seam.Exec` with an empty `Cmd.Dir` MUST return a non-nil error without starting any program.

- R-OMBQ-I4VZ: When `ctx` ends while the program runs, `seam.Exec` MUST kill every process in the program's process group, the program having been started as the leader of a new process group, and MUST then return, even when a descendant held the program's standard output or standard error, with a non-nil error for which `errors.Is(err, ctx.Err())` holds, verified by cancelling `ctx` once a child `/bin/sh -c 'sleep 600 & echo $$ > "$1"; sleep 600'` (which starts its background descendant holding standard output before it reports) has reported its process id through the FIFO named by `$1`: `seam.Exec` MUST return before a generous test deadline and no non-zombie process MUST remain in the process group whose id is that process id.

- R-YME1-Y3XQ: `seam.Stream` MUST write each chunk of the program's standard output to its `stdout` writer as the program produces it, without waiting for the program to exit, verified by a child that writes a line and then blocks until the test, on receiving that line in the writer, releases it.

- R-YNLY-BVOF: `seam.Stream` MUST return a `Result` whose `Stdout` is empty and whose `Output` is exactly the bytes the program wrote to its standard error, while the bytes the program wrote to its standard output reach the writer exactly and in order.

- R-0EC6-P58T: `seam.Stream` MUST run the program, with its arguments and working directory, exactly as `seam.Exec` does.

- R-0FK3-2WZI: `seam.Stream` MUST give the program the environment `seam.Exec` gives it.

- R-0GRZ-GOQ7: `seam.Stream` MUST give the program an empty standard input, as `seam.Exec` does.

- R-0HZV-UGGW: When the program exits with status `n`, `seam.Stream` MUST return a `Result` whose `ExitCode` is `n` together with a nil error.

- R-0J7S-887L: When the program is terminated by a signal that `seam.Stream` did not send, `seam.Stream` MUST return a `Result` whose `ExitCode` is 128 plus the signal number together with a nil error.

- R-0KFO-LZYA: `seam.Stream` MUST return a non-nil error when the program cannot be started, verified at least by a `Cmd.Path` found nowhere on `PATH` and by a `Cmd.Dir` that does not exist.

- R-0LNK-ZROZ: `seam.Stream` with an empty `Cmd.Dir` MUST return a non-nil error without starting any program.

- R-ONJM-VWMO: When `ctx` ends while the program runs, `seam.Stream` MUST kill every process in the program's process group, the program having been started as the leader of a new process group, and MUST then return, even when a descendant held the program's standard output or standard error, with a non-nil error for which `errors.Is(err, ctx.Err())` holds, verified by cancelling `ctx` from inside the writer once a child `/bin/sh -c 'sleep 600 & echo $$; sleep 600'` (which starts its background descendant holding standard output before it reports) has written its process id: `seam.Stream` MUST return before a generous test deadline and no non-zombie process MUST remain in the process group whose id is that process id.

- R-YR9N-H6WI: When a write to its `stdout` writer fails, `seam.Stream` MUST kill the program and return only after it has exited, with a non-nil error, verified by a writer that fails its first write and a child that writes a line and then runs `exec sleep 600`: `seam.Stream` MUST return that error before a generous test deadline on `ctx` expires.

- R-YSHJ-UYN7: `cli.Run` MUST be safe to call from several goroutines at once: concurrent calls with separate writers and `Deps` MUST each return the exit code and write the bytes a lone call with the same inputs does, with no data race reported under `go test -race`.

- R-YUXC-MI4L: `cli.Run` MUST NOT consult the test process's own environment or working directory: for every command, a call MUST return the same exit code and write the same bytes whether or not the process's `HOME`, `XDG_CONFIG_HOME` and `XDG_STATE_HOME` and its working directory point at a sentinel temporary directory holding a registry (D03) and secrets file of its own, and MUST leave that sentinel directory unchanged.

- R-0PBA-52X2: The `cmd/sandbox` main program, run as a non-root user with the single argument `--help`, MUST write to stdout exactly the bytes `cli.Run` writes to stdout for `--help`, write nothing to stderr, and exit 0.

- R-0QJ6-IUNR: The `cmd/sandbox` main program, run as a non-root user with the single argument `version`, MUST write to stdout exactly `cli.Version` followed by one newline, write nothing to stderr, and exit 0.

- R-0RR2-WMEG: The `cmd/sandbox` main program, run as a non-root user with the argument `url`, its working directory a temporary directory `T`, and a `PATH` holding only a fake `git` script that exits 128, MUST write the not-in-checkout diagnostic D03 declares naming `T`, write nothing to stdout, and exit 2.

- R-0SYZ-AE55: The `cmd/sandbox` main program, run as a non-root user with the argument `ls` and an environment whose only variables are `PATH` and `HOME`, `HOME` naming an empty temporary directory, MUST write nothing to stdout or stderr, exit 0, and leave that directory empty.

- R-56BL-8JQZ: The `cmd/sandbox` main program MUST pass the empty string as `Deps.Dir` when its working directory cannot be determined, and MUST still call `cli.Run`, verified by running it from a working directory that has been removed: with `--help` it MUST write the top-level help and exit 0, and with `url` it MUST write the no-current-directory diagnostic D03 declares for an empty `Deps.Dir` and exit as D03 states.

- R-OORJ-9ODD: The `cmd/sandbox` main program, run as a non-root user with the argument `url` and a `PATH` whose fake `git` script runs `sleep 600 &`, then reports its process id through a FIFO, then runs `sleep 600`, MUST, when sent SIGINT once that process id is reported, exit with status 1 rather than be terminated by the signal, and no non-zombie process MUST remain in the fake `git`'s process group once it has exited.

- R-OPZF-NG42: The `cmd/sandbox` main program, run as a non-root user with the argument `url` and a `PATH` whose fake `git` script runs `sleep 600 &`, then reports its process id through a FIFO, then runs `sleep 600`, MUST, when sent SIGTERM once that process id is reported, exit with status 1 rather than be terminated by the signal, and no non-zombie process MUST remain in the fake `git`'s process group once it has exited.
