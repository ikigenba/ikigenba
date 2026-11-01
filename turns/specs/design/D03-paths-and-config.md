# D03-paths-and-config

turns keeps two small files of its own configuration, reads a third that a host's operator writes, and finds every other directory it touches from the environment. This document owns all three concerns: `internal/paths` turns the environment and the system configuration root into every path turns uses, `internal/config` reads the configuration those paths name, and the `configure` command, reached through `cli.Run`, is the one writer of the person's own two files.

## Paths

`paths.Resolve` takes the environment as a lookup function and the system configuration root (`seam.Deps.Etc`: `/etc` in the binary, empty in tests that must never see the real one) and answers a `Paths` value. It touches no file: it only computes. HOME is the one thing it cannot do without; an unset, empty or relative HOME is an error, and configure reports it as `turns: ` followed by the error's text, exiting 1. The XDG base directories are honored only when absolute, as the XDG Base Directory specification says a relative value is to be ignored. The tools' own overrides (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `GROK_HOME`) follow the same rule: a relative value names a directory relative to whatever working directory the tool happened to start in, which turns, run from a timer, cannot know, so a relative value is ignored and the default under HOME is used. The dispatch tool's job directory ignores `XDG_STATE_HOME`, so turns' `Jobs` does too. With an empty system root there is no host configuration and no host units, and both paths are empty.

Every path is clean (no trailing slash, no `.` or `..` element). The files inside the two turns directories are named by four constants, so whoever uses them joins a directory with a constant rather than spelling a name.

## Reading the configuration

`config.Load` reads the person's `config` and `token` and the host's configuration file, and answers a `Config`: the effective space, the token, and the host's own space. A missing file is simply absent configuration; any other failure to read one is an error naming the file. A path field that is empty is not read at all, which is how a command asks for only the host's file.

Both configuration files have the same form: lines, each possibly ended by a carriage return, of which the first that begins with `space=` (after surrounding blanks) names the space, the value after `=` with surrounding blanks removed. Other lines are ignored. A `space=` line whose value is not a valid space name means the file names no space, so a person's broken `config` falls back to the host's space instead of building an endpoint from garbage. The person's file wins over the host's whenever it names a space.

The token file holds the token; a trailing newline, as an editor would leave, is not part of it. A configuration is complete when it has both a space and a token, and `Configured` answers that. Only a file that does not exist counts as missing; any other failure to read one, a path through a regular file included, is an error.

A space name is a bare DNS host name in lowercase: labels of letters, digits and hyphens, each label one to 63 characters not beginning or ending with a hyphen, at least two labels, at most 253 characters in all. A URL, a port, a path, an uppercase letter, an empty label or a trailing dot is refused.

## configure

`turns configure [<space>] [--token <token>]` stores the space in `config` and the token in `token`. The order of checks is fixed so that refusals never consume a token: help first, wherever it appears; then the shared argument frame (unknown option, unexpected argument, the first offending argument in order); then a `--token` with no value; then whether the space given is a space name, then the paths, then, when the space is omitted, the host's space; only then is the token taken, from `--token` or from the first line of stdin. So a refused invocation never reads stdin, and nothing is written until everything has been checked. A host configuration file that exists but cannot be read fails configure the way push and status report a configuration error: `turns: could not read the configuration`, an empty line, and the error as unprefixed detail.

`--token` takes its value as the next argument or after `=`; the `=` form is how a token beginning with `-` is given, since a following argument beginning with `-` is never taken as the value, and it is also why the form is accepted at all: refusing `--token=<token>` as an unknown option would print the token. Repeated, the last one wins. A help flag anywhere wins, even where a value would go.

A token that is empty or only white space once its line ending is stripped is refused as no token, and one holding a control character is refused without being echoed, since it could not travel in an `Authorization` header; any other token is stored exactly, inner and surrounding blanks included. Writing replaces each file whole. The token is written before `config`; the story says nothing of a failure between the two, so when the token is written and `config` then cannot be, configure fails with the write diagnostic and the new token stays. The token file is mode 0600 whatever mode an earlier file had, and holds exactly the token. Directories are created as needed and private to the person; one that cannot be created is reported as such, a file that cannot be written as such, each with one line of detail. With the space omitted on a host, only the token is written and `config` is neither created nor changed, so a space the person once named keeps overriding the host's. configure makes no request and runs no program: the token is accepted, never checked, and appears in no diagnostic. A space name, a path or an error's text that configure places in a diagnostic is written in its diagnostic rendering (D02), so a value holding a newline or another control byte cannot break a line.

## REQUIREMENTS

- R-I8XM-BZ81: Package `internal/paths` MUST export a `Paths` struct whose fields are exactly `Home string`, `Config string`, `State string`, `HostConfig string`, `UserUnits string`, `HostUnits string`, `Claude string`, `Codex string`, `Grok string`, and `Jobs string`.

- R-IBDF-3IPF: Package `internal/paths` MUST export `Resolve(getenv func(key string) string, etc string) (Paths, error)`.

- R-ICLB-HAG4: Package `internal/paths` MUST export the untyped string constant `ConfigFile = "config"`.

- R-IDT7-V26T: Package `internal/paths` MUST export the untyped string constant `TokenFile = "token"`.

- R-IF14-8TXI: Package `internal/paths` MUST export the untyped string constant `LastPushFile = "last-push"`.

- R-IG90-MLO7: Package `internal/paths` MUST export the untyped string constant `LockFile = "lock"`.

- R-OPF4-LBRE: `paths.Resolve` MUST return a non-nil error whose text is exactly `HOME is not set` when `getenv("HOME")` is empty.

- R-OQN0-Z3I3: `paths.Resolve` MUST treat a nil `getenv` as an environment in which every variable is empty, so that `Resolve(nil, "")` returns the error whose text is `HOME is not set`.

- R-IIOT-E55L: `paths.Resolve` MUST return a non-nil error whose text is exactly `HOME '<value>' is not an absolute path`, with `<value>` the variable's value, when `getenv("HOME")` is non-empty and not an absolute path, verified at least by `home/me` giving `HOME 'home/me' is not an absolute path`.

- R-IJWP-RWWA: When `getenv("HOME")` is an absolute path, `paths.Resolve` MUST return a nil error whatever the other variables and `etc` hold and whether or not any of the paths exists, MUST create nothing, and MUST set `Home` to `filepath.Clean` of that value, verified at least by `/home/me/` giving `Home` `/home/me` for a HOME that does not exist.

- R-IL4M-5OMZ: `paths.Resolve` MUST set `Config` to `<base>/turns` and `UserUnits` to `<base>/systemd/user`, where `<base>` is `filepath.Clean` of `XDG_CONFIG_HOME` when that value is an absolute path and `<Home>/.config` otherwise, verified at least by `XDG_CONFIG_HOME` unset, `/x/cfg/`, and `rel/cfg` (each with HOME `/home/me`) giving `Config` `/home/me/.config/turns`, `/x/cfg/turns`, and `/home/me/.config/turns`.

- R-IMCI-JGDO: `paths.Resolve` MUST set `State` to `filepath.Clean` of `XDG_STATE_HOME` joined with `turns` when that value is an absolute path and to `<Home>/.local/state/turns` otherwise, verified at least by `XDG_STATE_HOME` unset, `/x/state`, and `rel/state`.

- R-INKE-X84D: `paths.Resolve` MUST set `HostConfig` to `<etc>/xdg/turns/config` and `HostUnits` to `<etc>/systemd/user`, each `filepath.Clean`'d, when `etc` is not empty, and both to the empty string when `etc` is empty, verified at least by `etc` `/etc` giving `/etc/xdg/turns/config` and `/etc/systemd/user`.

- R-IOSB-AZV2: `paths.Resolve` MUST set `Claude` from `CLAUDE_CONFIG_DIR` with default `<Home>/.claude`, `Codex` from `CODEX_HOME` with default `<Home>/.codex`, and `Grok` from `GROK_HOME` with default `<Home>/.grok`, each field being `filepath.Clean` of its variable when the variable is an absolute path and its default otherwise (unset, empty, or relative), verified at least for each of the three with the variable unset, set to an absolute path with a trailing slash, and set to a relative path.

- R-IQ07-ORLR: `paths.Resolve` MUST set `Jobs` to `<Home>/.local/state/dispatch/jobs` whatever `XDG_STATE_HOME` holds, verified at least with `XDG_STATE_HOME` unset and set to `/x/state`.

- R-IR84-2JCG: `paths.Resolve` MUST call `getenv` with no key other than `HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and `GROK_HOME`, verified with a recording `getenv`.

- R-ISG0-GB35: Package `internal/config` MUST export a `Config` struct whose fields are exactly `Space string`, `Token string`, and `HostSpace string`.

- R-ITNW-U2TU: Package `internal/config` MUST export `Load(p paths.Paths) (Config, error)`.

- R-IW3P-LMB8: Package `internal/config` MUST export `ValidSpace(s string) bool`.

- R-IXBL-ZE1X: Package `internal/config` MUST export the method `func (c Config) Configured() bool`.

- R-IYJI-D5SM: `config.ValidSpace` MUST return true exactly when `s` is at most 253 bytes long and consists of at least two labels joined by `.`, each label 1 to 63 bytes of `a`-`z`, `0`-`9` and `-` that neither begins nor ends with `-`, verified at least by true for `us.ikigenba.dev`, `sbx.ikigenba.dev`, `a.b`, `x-1.y2` and `9.example`, and false for the empty string, `ikigenba`, `https://us.ikigenba.dev`, `us.ikigenba.dev:443`, `us.ikigenba.dev/mcp`, `US.ikigenba.dev`, `us.ikigenba.dev.`, `.us.ikigenba.dev`, `us..ikigenba.dev`, `-us.ikigenba.dev`, `us-.ikigenba.dev`, `us_x.ikigenba.dev`, ` us.ikigenba.dev`, a label of 64 bytes, and a 254-byte name of valid labels.

- R-IZRE-QXJB: `config.Load` MUST read a configuration file (the file at `filepath.Join(p.Config, paths.ConfigFile)` and the file at `p.HostConfig`) as naming a space exactly when, splitting its contents on `\n` and removing one trailing `\r` and then leading and trailing spaces and tabs from each line, the first line that begins with `space=` has a remainder after `space=` that, with leading and trailing spaces and tabs removed, satisfies `config.ValidSpace`; the space it names is that remainder, other lines are ignored, and a file whose first `space=` line is not valid names no space even when a later one is, verified at least by `space=us.ikigenba.dev` with no final newline, `# note\n  space = x\nspace=us.ikigenba.dev \r\n` naming `us.ikigenba.dev`, and `space=https://us.ikigenba.dev\nspace=us.ikigenba.dev\n` naming none.

- R-J0ZB-4PA0: `config.Load` MUST set `HostSpace` to the space the file at `p.HostConfig` names, or the empty string when it names none or is absent, and MUST set `Space` to the space the user's configuration file names when it names one, else to `HostSpace`, verified at least by a user file naming `sbx.ikigenba.dev` over a host file naming `us.ikigenba.dev` giving `Space` `sbx.ikigenba.dev` and `HostSpace` `us.ikigenba.dev`, a user file `space=bad` over that host file giving `Space` `us.ikigenba.dev`, and no user file and no host file giving both empty.

- R-OT2T-QMZH: `config.Load` MUST set `Token` to the contents of the file at `filepath.Join(p.Config, paths.TokenFile)` with one trailing `\n`, and then one trailing `\r`, removed and nothing else trimmed, and to the empty string when that file is absent, verified at least by `tok`, `tok\n` and `tok\r\n` each giving `tok`, ` tok \t\n` giving ` tok \t`, `tok\n\n` giving `tok\n`, and `\n` giving the empty string.

- R-J3F3-W8RE: `config.Load` MUST return a nil error for each of the three files that does not exist, MUST read no user file when `p.Config` is empty and no host file when `p.HostConfig` is empty, and MUST NOT create or change any file, verified at least by a `Paths` whose `Config` names a missing directory and whose `HostConfig` is empty answering the zero `Config` and a nil error, and by a `Paths` with only `HostConfig` set answering that file's space as `Space` and `HostSpace` with an empty `Token` and a nil error.

- R-VHZU-7NBM: `config.Load` MUST treat one of the three files as missing only when reading it fails with an error for which `errors.Is(err, fs.ErrNotExist)` holds, and for every other failure (for example the path is a directory, has mode 0000, or passes through a regular file so that the error is ENOTDIR) MUST return a non-nil error whose text is a single line consisting of `could not read '<path>': `, `<path>` being that file's path in its diagnostic rendering (D02's R-QXXN-LZUW), followed by non-empty text holding no byte below 0x20 and no byte 0x7f, so that a cause whose own text holds the path is rendered too, verified at least by a `Paths` that `paths.Resolve` answers for an absolute temporary HOME and an `etc` that is an absolute temporary directory ending in `host`, LF, `root`, its `HostConfig` path created as a directory, giving an error text that begins `could not read '<etc>/xdg/turns/config': ` with the LF of `<etc>` written as the two bytes backslash and `n`, and holds no LF.

- R-J5UW-NS8S: `Config.Configured` MUST return true exactly when both `Space` and `Token` are non-empty, verified at least by all four combinations of empty and non-empty `Space` and `Token`, each with `HostSpace` empty and non-empty.

- R-OXYF-9PY9: `turns configure --help` and `turns configure -h`, and every `configure` invocation with `--help` or `-h` among its arguments (including `turns configure 'not a space' --token -h` and `turns configure --bogus -h`), MUST print exactly this text to stdout, ending in exactly one `\n` after its last line `  -h, --help        print this help`, write nothing to stderr, and return `cli.ExitOK`:

  ```
  Usage: turns configure [<space>] [--token <token>]

  Store the space turns pushes to and the personal token it pushes with, in
  $XDG_CONFIG_HOME/turns/ (~/.config/turns/ by default). The token is read as
  one line from stdin unless --token gives it. Running it again replaces both,
  or, with the space omitted, only the token. On a host the space may be
  omitted; the host's own space is used.

  Options:
    --token <token>   take the token from the argument instead of stdin
    -h, --help        print this help
  ```

- R-J8AP-FBQ6: A `configure` invocation that prints its usage MUST NOT read stdin and MUST NOT create or change any file, verified with a stdin reader that records reads and a temporary HOME.

- R-J9IL-T3GV: `configure` MUST accept at most one positional argument, the space, and the option `--token <token>` or `--token=<token>` before or after it, any number of times, the last occurrence giving the token, verified at least by `turns configure --token old us.ikigenba.dev --token=new` storing `new`, and by `turns configure us.ikigenba.dev --token=-abc=` storing `-abc=`.

- R-D8CZ-79O4: After D02's frame accepts the arguments, `configure` MUST write exactly the three lines `turns: option '--token' needs a value`, an empty line, and `see 'turns configure --help' for usage` to stderr, nothing to stdout, and return `cli.ExitUsage` when `--token` is the last argument or is followed by an argument beginning with `-`, verified at least by `turns configure us.ikigenba.dev --token` and `turns configure --token --token=x us.ikigenba.dev`, with stdin not read and nothing created or changed.

- R-D9KV-L1ET: `configure`'s refusals MUST come in this order: D02's frame refusals (an unknown option or an unexpected argument, for the first offending argument in argument order), then `--token` without a value, then a space that is not a space name, then the paths; verified at least by `turns configure https://us.ikigenba.dev --bogus` giving D02's unknown-option error for `--bogus`, `turns configure https://x extra` giving D02's unexpected-argument error for `extra`, `turns configure us.ikigenba.dev --token --bogus` giving the unknown-option error for `--bogus`, `turns configure https://x --token` giving the needs-a-value error, and `turns configure us.ikigenba.dev --token` with HOME unset giving the needs-a-value error, each with stdin not read.

- R-VJ7Q-LF2B: When the space given is not a space name by `config.ValidSpace`, `configure` MUST write exactly the one line `turns: '<space>' is not a space name` to stderr, `<space>` being the space as given in its diagnostic rendering (D02's R-QXXN-LZUW), nothing to stdout, and return `cli.ExitUsage`, without reading stdin and without creating or changing any file, whether or not HOME is set and whatever `--token` holds, verified at least by `https://us.ikigenba.dev`, `us.ikigenba.dev:443`, `us.ikigenba.dev/mcp`, `US.ikigenba.dev`, `ikigenba` and the empty string, and by `bad`, LF, `space` giving exactly the one line `turns: 'bad\nspace' is not a space name` (`\n` being the two bytes backslash and `n`).

- R-VKFM-Z6T0: When the argument frame and the space are acceptable and `paths.Resolve` over `Deps.Getenv` and `Deps.Etc` returns an error, `configure` MUST write exactly the one line `turns: ` followed by that error's text in its diagnostic rendering (D02's R-QXXN-LZUW) to stderr, nothing to stdout, and return `cli.ExitFailed`, without reading stdin and without creating any file, verified at least with HOME unset and with HOME `home/me`, with and without a space given, and with HOME `home`, LF, `me` giving exactly the one line `turns: HOME 'home\nme' is not an absolute path` (`\n` being the two bytes backslash and `n`).

- R-JGU0-3PX1: When the space is omitted and the host's configuration file names no space (`Deps.Etc` empty, the file absent, or the file naming none), `configure` MUST write exactly the three lines `turns: no space given`, an empty line, and `see 'turns configure --help' for usage` to stderr, nothing to stdout, and return `cli.ExitUsage`, without reading stdin and without creating or changing any file, even when the user's `config` names a space and even with `--token` given.

- R-VLNJ-CYJP: `configure` MUST consult the host's configuration file only when the space is omitted; when it is omitted and that file exists but cannot be read, `configure` MUST write to stderr exactly the line `turns: could not read the configuration`, an empty line, and the text of the error `config.Load` reports for it unprefixed in its diagnostic rendering (D02's R-QXXN-LZUW), nothing to stdout, and return `cli.ExitFailed` without reading stdin or changing any file, and when a space is given, a host configuration path that is an unreadable directory MUST NOT prevent success; verified at least with `Deps.Etc` an absolute temporary directory ending in `host`, LF, `root` whose `xdg/turns/config` is a directory, giving exactly three lines of stderr.

- R-ETSY-BIID: Without `--token`, `configure` MUST take as the token the first line read from stdin: the bytes before the first `\n`, or all of stdin when it holds none, with one trailing `\r` removed and nothing else trimmed, the rest of stdin ignored, verified at least by `tok\n`, `tok\r\n`, `tok` and `tok\nsecond\n` each storing `tok`, and ` tok \n` storing ` tok `.

- R-OVIM-I6GV: `configure` MUST stop reading stdin once it has read the first `\n`, verified at least by a stdin reader whose first read yields exactly `tok\n` and which records every later read, leaving `token` holding `tok`, no later read recorded, and `cli.ExitOK` returned, and by a reader that yields `tok\n` and then an error storing `tok` and returning `cli.ExitOK`.

- R-JKHP-9154: With `--token` given, `configure` MUST store its value and MUST NOT read stdin, verified with a stdin reader that records reads and holds another token.

- R-89VY-KJJO: When the token, after its line ending is stripped, is empty after removing leading and trailing Unicode white space as `strings.TrimSpace` defines it (stdin empty, stdin's first line empty as in `\n` or `\r\n`, stdin ` \t\n`, `--token ''`, `--token=`, or `--token ' '`), `configure` MUST write exactly the one line `turns: no token given` to stderr, nothing to stdout, and return `cli.ExitUsage`, leaving an existing `config` and `token` byte-identical with their modes unchanged and creating no file or directory; any other token without a control character MUST be stored exactly, verified at least by `--token ' a b '` storing ` a b `.

- R-8B3U-YBAD: When the token, after its line ending is stripped, is not refused as no token given but holds a byte below 0x20 or the byte 0x7f, `configure` MUST write exactly the one line `turns: the token holds a control character` to stderr, nothing to stdout, and return `cli.ExitUsage`, leaving an existing `config` and `token` byte-identical with their modes unchanged and creating no file or directory, verified at least by stdin `to\tk\n`, stdin `to\rk\r\n`, stdin `tok\x7f`, and `--token $'a\x01b'`, the token's bytes appearing in neither stdout nor stderr.

- R-JMXI-0KMI: `configure` MUST treat a nil stdin as an empty one, so that without `--token` a nil stdin gives the no-token-given refusal.

- R-VMVF-QQAE: When reading stdin returns an error other than end of file, `configure` MUST write exactly the line `turns: could not read the token from stdin`, an empty line, and the error's text unprefixed in its diagnostic rendering (D02's R-QXXN-LZUW) to stderr, nothing to stdout, and return `cli.ExitFailed`, creating or changing no file, verified at least by a stdin reader failing with an error whose text is `broken`, LF, `pipe` giving exactly the three lines, the last `broken\npipe` (`\n` being the two bytes backslash and `n`).

- R-JPDA-S43W: When a space is given and a token taken, `configure` MUST leave the file at `filepath.Join(Config, paths.ConfigFile)`, for the `Paths` `paths.Resolve` answers over `Deps.Getenv` and `Deps.Etc`, holding exactly `space=<space>` followed by one `\n`, whatever the host's configuration file names, verified at least with `XDG_CONFIG_HOME` unset (the file at `<HOME>/.config/turns/config`) and set to an absolute temporary directory.

- R-JQL7-5VUL: When a token is taken, `configure` MUST leave the file at `filepath.Join(Config, paths.TokenFile)` holding exactly the token's bytes with no trailing newline and with permission bits exactly 0600, including when a token file already existed with mode 0644 or 0400.

- R-JRT3-JNLA: Running `configure` again MUST replace each file it writes whole, verified at least by a run storing `space=sbx.ikigenba.dev` and token `t2` after one that stored `space=a-much-longer-space.ikigenba.dev` and token `a-much-longer-token`, leaving exactly the new contents.

- R-JT0Z-XFBZ: `configure` MUST create the configuration directory and any missing parent of it, every directory it creates having no permission bits outside 0700, verified at least with HOME an empty temporary directory and with `XDG_CONFIG_HOME` a missing absolute path below the test's root.

- R-JU8W-B72O: When the space is omitted and the host's configuration file names a space, `configure` MUST write only the token file, MUST NOT create `config` when it is absent, and MUST leave an existing `config` byte-identical, verified at least by `turns configure < token` and `turns configure --token <token>` with `Deps.Etc` a temporary directory whose `xdg/turns/config` holds `space=us.ikigenba.dev`.

- R-JVGS-OYTD: `configure` MUST succeed and replace both files when the existing `config` and `token` cannot be read (mode 0000), leaving the token file with mode 0600.

- R-JWOP-2QK2: A `configure` invocation that stores its files MUST write nothing to stdout or stderr and return `cli.ExitOK`.

- R-OZ6B-NHOY: A successful `configure` MUST NOT create or change any file below the test's temporary root other than the configuration directory (with any parent it created), `config` when a space was given, and `token`, and MUST NOT leave any other file, temporary or otherwise.

- R-VO3C-4I13: When `configure` cannot write `config` or `token`, it MUST write to stderr exactly three lines, `turns: could not write '<path>'` with `<path>` that file's path in its diagnostic rendering (D02's R-QXXN-LZUW), an empty line, and one more line, the diagnostic rendering of the failure's error text, that is non-empty and does not begin with `> `, write nothing to stdout, and return `cli.ExitFailed`, verified at least with `token` and, separately, `config` replaced by a non-empty directory, and with `XDG_CONFIG_HOME` an absolute temporary directory ending in `x`, LF, `y` and `token` replaced by a non-empty directory, giving exactly three lines whose first is `turns: could not write '<XDG_CONFIG_HOME>/turns/token'` with that LF written as the two bytes backslash and `n`.

- R-VQJ4-W1IH: When `configure` cannot create a directory it needs, it MUST write to stderr exactly three lines, `turns: could not create '<dir>'` with `<dir>` the configuration directory (`Paths.Config`) in its diagnostic rendering (D02's R-QXXN-LZUW), an empty line, and one more line, the diagnostic rendering of the failure's error text, that is non-empty and does not begin with `> `, write nothing to stdout, return `cli.ExitFailed`, and create neither `config` nor `token`, verified at least with the configuration directory's parent a regular file, and with `XDG_CONFIG_HOME` a regular file at an absolute temporary path ending in `x`, LF, `y`, giving exactly three lines whose first is `turns: could not create '<XDG_CONFIG_HOME>/turns'` with that LF written as the two bytes backslash and `n`.

- R-K1KA-LTIU: A file `configure` could not write MUST keep its earlier contents, and when the token file cannot be written `config` MUST be left byte-identical, verified at least with `token` replaced by a non-empty directory and an existing `config` naming another space.

- R-P2U0-SSX1: When the token file is written and `config` then cannot be, `configure` MUST give the could-not-write diagnostic for `config` and return `cli.ExitFailed`, and the token file MUST hold the new token with mode 0600, verified at least with `config` replaced by a non-empty directory.

- R-P0E8-19FN: `configure` MUST NOT send a request through `Deps.HTTP` or call `Deps.Exec` in any invocation, verified with a transport and a runner that record calls, across at least a stored space and token, a host token-only run, the help, and each refusal this document states.

- R-MJYW-1ZHF: For a token distinctive for the run (D09's R-MH6Z-4PMD), `configure` MUST NOT write that token, given by `--token` or stdin, to stdout or stderr, verified at least by the no-space-given, not-a-space-name, HOME-unset and could-not-write cases, each run with a distinctive token given by `--token` and, separately, on stdin.
