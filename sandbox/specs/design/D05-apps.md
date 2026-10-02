# D05-apps

This document owns what `up` takes from the checkout and from the developer's secrets: which directories are apps, what a manifest may say and every way it is refused, the secrets file and its refusals, the environment each app is given through its env file, and the services file every app reads. When these checks run relative to the port, the builds and the units, and when the env and services files are written, is D04's; the paths of the env file and the services file, the usable-app-name rule, the origins, the socket paths and the config root are D03's; the diagnostic shapes for a file that cannot be read are D02's. Everything here is observed through `cli.Run` (D01) running `up` with fake runners, over a checkout and a secrets file built under temporary directories.

An app is an immediate subdirectory of the worktree (D03) holding `etc/manifest.toml`. The directory's name is the app's name, its `main` package is `cmd/<app>/` inside it, it may ship an icon at `share/icon.svg`, and it may ship nginx configuration of its own as files in its `etc/` whose names begin `nginx.conf`, which D06 includes in the app's server through the app's fragment pattern and sandbox itself never reads. Nothing else in the checkout matters: other sub-projects, documentation, and a manifest one level too deep are not apps. A directory whose name D03 says is not a usable app name is refused rather than skipped, because a developer who added it meant it as an app.

A manifest is TOML, and the platform owns most of it: sandbox reads `app`, `description`, `default`, `mcp`, `secrets` and the `[env]` table, refuses a top-level `port`, and ignores everything else (`[database]` among it), so a manifest written for the platform needs no change to run here. `up` checks the apps one at a time in app-name order and, within a manifest, in a fixed order, and reports only the first fault, one line, exit 2. Only once every manifest passes does it look across them for a second default app. The diagnostics name the manifest relative to the checkout, `<app>: etc/manifest.toml`, which is what the developer edits. A name or `app` value a diagnostic quotes before it is known to be a usable name is written as a printed name (D02 R-MBDF-W0VP), its control characters escaped, so every diagnostic stays one line. A `description` must be one line of text, holding no control character, whatever `mcp` is; an app that sets `mcp = true` must also give a `description` that is not empty or only whitespace, since an MCP service must say what it offers, and that check comes after every other manifest check, so any other fault in the same manifest is the one reported. The last checks of each app are of its icon, when it ships one: anything at `share/icon.svg`, a dangling symbolic link included, counts as shipping one, and nothing there means no icon. The icon must be a regular file (a symbolic link is never followed), readable, at most 64 KiB, valid UTF-8 and an SVG image, checked in that order, because the platform publishes only such an icon and carries it verbatim, as a JSON string, in the services file every app reads; checking it here also means the services file can never fail part-way through `up`. A manifest that is not TOML is refused with the TOML parser's own words; a key of the wrong type gets sandbox's words, which say what the key must be.

An app's environment is the five variables the sandbox sets, its manifest's `[env]` entries, and the secrets its manifest lists, nothing else; systemd adds its own. A manifest may not name a variable the sandbox or systemd sets for the service: overriding `LISTEN_FDS` or `NOTIFY_SOCKET` through the env file would break socket activation or readiness, since systemd lets an env file override them. A name must be a plain shell variable name, so every line of the env file is one assignment.

Three of the five, `IKIGENBA_CALLBACK_URL`, `IKIGENBA_PUBLIC_URL` and `IKIGENBA_SANDBOX`, are the sandbox's documented, generic environment contract: the same names in every sandbox, telling any app the sandbox's name, its own public origin, and the bare-localhost origin that is sent on to auth when the checkout holds `auth`, so an app that needs an OAuth redirect Google accepts, auth in particular, reads its callback origin from `IKIGENBA_CALLBACK_URL` and its public origin from `IKIGENBA_PUBLIC_URL`. Production sets none of them, so an app reads them when present and behaves as on a host when they are absent. `DRAIN_SECONDS` and `IKIGENBA_SERVICES` are set in both places.

Secrets live outside the repository, in one TOML file under the developer's config root, shared by every sandbox, one table per app. It is read only when some manifest lists a secret, so a checkout without secrets never cares whether the file exists or parses. A secret that is absent or empty is missing, and every missing secret is named at once so the developer fixes them all in one pass. A secret value is never printed, never put on a command line and never written into a unit: it reaches the app's env file and nothing else. That is why a secrets file that is not TOML is refused with only the line number of the fault: the parser's own message can quote the text near it, which may be a secret.

The env file is read by systemd's `EnvironmentFile=`. Every value is written in double quotes with `\`, `"`, `` ` `` and `$` escaped by a backslash, which systemd reads back to the exact value, quotes, tabs, leading spaces and all. systemd.exec(5) declares a NUL, a byte order mark and a Unicode noncharacter invalid in an environment file (in practice a noncharacter makes the service fail to start), and the platform refuses a setting or secret holding a carriage return or a line feed (a secret pasted with its line ending is easy to miss), so a value holding any of these is refused before anything is built, a trailing line break included, with the same words for all of them. The refusal names the key, never the value. No value that reaches the env file holds a line break, so every line of the env file is exactly one variable.

The services file is the platform's format: one JSON object whose `services` array lists every app of the last `up`, one service to a line, with fixed member order. The default app is not marked in it; being the default changes only routing (D06) and what `up` prints (D04), never an app's `url` or its `IKIGENBA_PUBLIC_URL`.

## REQUIREMENTS

### Names

- R-AUCL-BZBE: An app's source directory MUST be `<worktree>/<app>`, where `<worktree>` is the worktree D03 finds and `<app>` is the app's name, the name of that directory.

- R-UE7I-W0Z0: An app's manifest MUST be the file `<worktree>/<app>/etc/manifest.toml`.

- R-UFFF-9SPP: An app's main package MUST be the directory `<worktree>/<app>/cmd/<app>`.

- R-UGNB-NKGE: An app's icon file MUST be `<worktree>/<app>/share/icon.svg`.

- R-RKQQ-LM3W: An app's nginx fragment pattern MUST be `<worktree>/<app>/etc/nginx.conf*`, the glob pattern naming every entry of the app's `etc/` whose name begins `nginx.conf`.

- R-UHV8-1C73: The secrets file MUST be `<config root>` (D03) joined with `ikigenba/sandbox/secrets.toml` and cleaned lexically, as Go's `path.Join` does.

- R-UJ34-F3XS: The manifest keys sandbox reads MUST be exactly the top-level keys `app` (a string), `description` (a string, `""` when absent), `default` (a boolean, `false` when absent), `mcp` (a boolean, `false` when absent), `secrets` (an array of strings, empty when absent) and `env` (a table whose values are strings, empty when absent), and the top-level key `port`, which is refused.

- R-UKB0-SVOH: A variable name MUST be one or more characters, each one of `A`–`Z`, `a`–`z`, `0`–`9` or `_`, the first not a digit.

- R-ULIX-6NF6: The sandbox-owned variables MUST be exactly `DRAIN_SECONDS`, `LISTEN_FDS`, `LISTEN_FDNAMES`, `LISTEN_PID`, `NOTIFY_SOCKET`, and every name beginning `IKIGENBA_`.

- R-UMQT-KF5V: A character an env file cannot hold MUST be exactly U+0000, U+FEFF, each of U+FDD0 through U+FDEF, and each code point whose low sixteen bits are `FFFE` or `FFFF`.

- R-NUBF-DI7O: A character a value cannot hold MUST be exactly U+000A, U+000D, and each character an env file cannot hold.

- R-UNYP-Y6WK: App-name order MUST be ascending byte order of app names.

### Discovery

- R-UP6M-BYN9: The apps `up` discovers MUST be exactly the entries `<D>` of the worktree for which `stat(2)` of `<worktree>/<D>/etc/manifest.toml` succeeds, whatever kind of file it names, each app named `<D>`; verified by an `up` that succeeds in a worktree holding `auth/etc/manifest.toml` and `dummy/etc/manifest.toml` with the fixture manifests, a `sandbox/` and a `docs/` holding no `etc/manifest.toml`, a regular file `notes`, and `tools/widget/etc/manifest.toml`, whose services file lists exactly `auth` and `dummy`.

- R-URMF-3I4N: When `up` discovers no app, it MUST write the no-apps diagnostic, `sandbox: no apps in <worktree>`, one empty line, and `an app is a directory holding etc/manifest.toml`, to stderr, nothing to stdout, and exit 2.

- R-USUB-H9VC: When the worktree's entries cannot be listed, `up` MUST fail with D02's file-error diagnostic for the worktree's path, verified with a worktree of mode 0300.

### Manifest checks

- R-U6S0-HHI7: When `up` checks manifests it MUST take the discovered apps in app-name order and, for each, make these checks in this order: the app's name is a usable app name (D03), the manifest can be read, it is valid TOML, it has no top-level `port`, each read key has its declared type (in the order `app`, `description`, `default`, `mcp`, `secrets`, `env`), `app` is present, `app` equals the app's name, `description` is one line of text, the variable checks, a manifest setting `mcp = true` has a `description` that is not empty, and then the icon checks: the icon file is a regular file, it can be read, it holds at most 65,536 bytes, it is valid UTF-8, and it is an SVG image; and it MUST report only the first fault it finds, as one diagnostic, making no further check; verified at least by `auth` with a top-level `port` and `dummy` with `app = "demo"` reporting only auth's `port` fault, by a manifest holding both `port = 8080` and `app = "demo"` reporting only the `port` fault, by dummy's manifest without its `description` line reporting only the `port` fault when it also holds `port = 8080`, only the type fault when it also holds `default = 1`, only the one-line fault when it holds `description = "\n"` instead, only the one-line fault when it holds `description = "a\nb"` and its `env` table also sets `IKIGENBA_X = "x"`, and only the sandbox-owned fault when its `env` table also sets `IKIGENBA_X = "x"`, by that same manifest with an icon file that is a directory reporting only the empty-description fault, by `auth` with an unreadable icon file and `dummy` with `app = "demo"` reporting only auth's icon fault, by an icon file of 65,537 bytes and mode 000 reporting the read fault, by an icon file of 65,537 bytes that is neither valid UTF-8 nor an SVG image reporting the size fault, and by an icon file holding `caf` and the byte 0xE9 reporting the UTF-8 fault.

- R-UVA4-8TCQ: Every refusal of a manifest check, and the more-than-one-default refusal, MUST write its one line to stderr, nothing to stdout, and make `cli.Run` return 2.

- R-MCLC-9SME: When an app's name is not a usable app name (D03), `up` MUST write the unusable-app-name diagnostic, exactly `sandbox: '<app>' is not a usable app name` with `<app>` written as a printed name, verified at least by `Dummy_2`, `nginx`, `-dummy`, a 64-character name, a directory named `a`, a newline and `b` writing `sandbox: 'a\x0ab' is not a usable app name` as one line, a directory named `a`, a tab and `b` writing `sandbox: 'a\x09b' is not a usable app name`, and a directory named `a`, the byte 0x7F and `b` writing `sandbox: 'a\x7fb' is not a usable app name`.

- R-UXPX-0CU4: When an app's manifest cannot be read, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: <reason>`, where `<reason>` is the operating-system error's message alone as D02's file-error diagnostic gives it, verified at least by a manifest of mode 000 giving `permission denied` and a manifest that is a directory giving `is a directory`.

- R-UYXT-E4KT: When an app's manifest is not valid TOML, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: <reason>`, where `<reason>` is the `Error()` text of the `toml.ParseError` that `github.com/BurntSushi/toml`'s `Decode` returns for the manifest's bytes with its leading `toml: ` removed, verified at least by a manifest whose first line is `app = "dummy` writing `sandbox: dummy: etc/manifest.toml: line 1 (last key "app"): strings cannot contain newlines`.

- R-V05P-RWBI: When an app's manifest has a top-level `port`, whatever its value or type, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: 'port' is not allowed; the sandbox gives the app its socket`, verified at least by `port = 8080` and `port = "8080"`, while a `port` key inside `env` is not this fault.

- R-V1DM-5O27: When a read key of an app's manifest has a value of another type than declared, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: '<key>' must be <type>`, where `<type>` is `a string` for `app` and `description`, `a boolean` for `default` and `mcp`, `an array of strings` for `secrets`, and `a table of strings` for `env`, verified at least by `mcp = "yes"`, `default = 1`, `app = 5`, `secrets = "A"`, `secrets = [1]`, `env = "x"` and an `env` table holding `X = 4`.

- R-V2LI-JFSW: When an app's manifest has no `app` key, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: 'app' is missing`.

- R-AY0A-HAJH: When an app's manifest sets `app` to a value other than the app's name, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: app '<value>' does not match its directory '<app>'`, with `<value>` written as a printed name, verified at least by `app = "demo"` in `dummy`, by `app = ""`, and by `app = "de\nmo"` writing `app 'de\x0amo'` as one line.

- R-U5K4-3PRI: When an app's manifest sets `description` to a string holding a character from U+0000 through U+001F or U+007F, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: 'description' must be one line of text`, whatever `mcp` is, verified at least by `description = "Demo widgets\nto list and create"`, a `description` ending in a line feed, one holding a carriage return, one holding a tab, one holding U+0000, one holding U+001F, one holding U+007F, and `description = "\n"` with `mcp = true`, while `description = ""`, `description = "   "` with `mcp` unset, and a `description` holding U+0085 or `é` are not refused.

- R-NXZ4-ITFR: When an app's manifest sets `mcp = true` and its `description` is absent, empty, or holds only characters Go's `unicode.IsSpace` reports as space, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: 'mcp' is true but 'description' is empty; an MCP service must say what it offers`, verified at least by dummy's manifest without its `description` line, with `description = ""`, with `description = "   "` and with `description` holding U+00A0 and U+3000, while auth's manifest, which sets neither `mcp` nor `description`, and a manifest setting `mcp = false` and `description = ""` are not refused.

- R-NZ70-WL6G: The variable checks of a manifest MUST take its `env` keys in ascending byte order and then its `secrets` entries in the order listed, and for each in turn check that it is a variable name, that it is not a sandbox-owned variable, that it is not both an `env` key and a `secrets` entry, and, for an `env` key, that its value holds no character a value cannot hold, reporting the first fault found.

- R-AZ86-V2A6: When an `env` key or a `secrets` entry is not a variable name, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: '<name>' is not a variable name`, with `<name>` written as a printed name, verified at least by an `env` key `A B`, an `env` key `1X`, a `secrets` entry `""`, and a quoted `env` key `"A\nB"` writing `'A\x0aB'` as one line.

- R-V7H4-2IRO: When an `env` key or a `secrets` entry is a sandbox-owned variable, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: '<name>' is set by the sandbox`, verified at least by an `env` key `IKIGENBA_SERVICES`, a `secrets` entry `IKIGENBA_SERVICES`, and each of `DRAIN_SECONDS`, `LISTEN_FDS`, `LISTEN_FDNAMES`, `LISTEN_PID`, `NOTIFY_SOCKET` and `IKIGENBA_X` as an `env` key.

- R-V8P0-GAID: When a name is both an `env` key and a `secrets` entry of one manifest, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: '<name>' is both in [env] and in secrets`.

- R-O0EX-ACX5: When an `env` value holds a character a value cannot hold, `up` MUST write exactly `sandbox: <app>: etc/manifest.toml: '<name>' holds a character an env file cannot hold`, naming the `env` key, verified at least by `BANNER` set to `line one\nline two`, to `line one\r`, to `line one\n` and to `a\u0000b`, and by values holding U+FEFF, U+FDD0 and U+1FFFF, while a value holding a tab, U+0085 or `é` is not refused.

- R-VCCP-LLQG: When every manifest passes its checks and more than one app's manifest sets `default = true`, `up` MUST write exactly `sandbox: more than one default app: <apps>`, where `<apps>` is every such app's name in app-name order joined by `, `, verified at least by `auth` and `dummy` giving `sandbox: more than one default app: auth, dummy`.

- R-VDKL-ZDH5: `up` MUST accept a manifest holding top-level keys and tables other than those it reads, whatever their values, and MUST give the app the same env file and services-file entry as without them, verified at least with `[database]` holding `engine = "sqlite"` and `path = "state/auth.db"` and with `extra = 1`.

- R-VESI-D57U: An app MUST be the default app exactly when its manifest sets `default = true`, verified by the registry (D03) after an `up` that succeeds recording `default` true for `dummy` alone when only dummy's manifest sets it, and false for every app when no manifest sets it.

### Secrets

- R-VG0E-QWYJ: When no discovered app's manifest lists a secret, `up` MUST NOT read the secrets file, verified by an `up` that succeeds with exactly the same output, exit code and written files whether the secrets file is absent, a directory, of mode 000, or holds `[auth` as its first line.

- R-VH8B-4OP8: When some app's manifest lists a secret, the secrets check MUST find, in this order, that the secrets file can be read, that it is valid TOML, that its values are usable, and that no secret is missing, and MUST report only the first fault.

- R-VIG7-IGFX: A secrets file that does not exist (opening it fails with `ENOENT`) MUST count as a file holding no table, and any other failure to read it MUST fail `up` with D02's file-error diagnostic for the secrets file's path and exit 2, verified at least by a secrets file that is a directory.

- R-B0G3-8U0V: When the secrets file is not valid TOML, `up` MUST write exactly `sandbox: <secrets file>: not valid TOML at line <n>`, where `<n>` is the line of the `toml.ParseError` that `github.com/BurntSushi/toml`'s `Decode` returns for the file's bytes, to stderr, nothing to stdout, and exit 2, verified at least by a file whose first line is `[auth` followed by `GOOGLE_CLIENT_ID = "1234-abc.apps.googleusercontent.com"` writing `not valid TOML at line 2`, and by a file whose lines are `[auth]` and `GOOGLE_CLIENT_SECRET = GOCSPX-example` writing `not valid TOML at line 2` and no `GOCSPX`.

- R-VKW0-9ZXB: The usable-values check MUST take each app whose manifest lists a secret in app-name order and, for each, check first that the secrets file's top-level key named for the app, when present, holds a table, and then each secret the app lists in ascending byte order, reporting the first fault found.

- R-VM3W-NRO0: When the secrets file's top-level key named for an app that lists a secret holds something other than a table, `up` MUST write exactly `sandbox: <secrets file>: '<app>' must be a table`, to stderr, nothing to stdout, and exit 2.

- R-VNBT-1JEP: When an app's table in the secrets file sets a secret the app lists to a value that is not a string, `up` MUST write exactly `sandbox: <secrets file>: '<app>.<secret>' must be a string`, to stderr, nothing to stdout, and exit 2.

- R-O1MT-O4NU: When an app's table in the secrets file sets a secret the app lists to a string holding a character a value cannot hold, `up` MUST write exactly `sandbox: <secrets file>: '<app>.<secret>' holds a character an env file cannot hold`, to stderr, nothing to stdout, and exit 2, verified at least by `GOOGLE_CLIENT_SECRET` set to `GOCSPX-example\n`, to `GOCSPX-example\r\n`, to `GOCSPX-\rexample`, and to `GOCSPX-example` with U+0000 or U+FFFE appended, nothing `sandbox` writes holding `GOCSPX-example`.

- R-B1NZ-MLRK: A secret an app's manifest lists MUST be missing exactly when the secrets file has no table for that app, or that table does not set it, or sets it to the empty string; any other key or table of the secrets file, whatever its value, MUST NOT affect the secrets check, verified at least with auth's table also setting `SIGNING_KEY = 1` and a `[billing]` table setting `STRIPE_KEY = []`.

- R-VQZI-6UMS: When any secret is missing, `up` MUST write the missing-secrets diagnostic: `sandbox: secrets missing from <secrets file>`, one empty line, and then one line `<app> <secret>` for each distinct missing secret, sorted by app in app-name order and then by secret in ascending byte order, to stderr, nothing to stdout, and exit 2, verified at least with auth listing `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` and the secrets file absent, holding no `[auth]` table, holding an `[auth]` table that sets neither, and setting both to `""`, each writing both lines; and with only `GOOGLE_CLIENT_SECRET` missing writing only its line.

- R-B2VW-0DI9: A value a secrets file sets MUST NOT appear in anything `up` writes to stdout or stderr, in the `Path`, `Args` or `Dir` of any `seam.Cmd` it passes to either runner, or in any file it writes other than an app's env file, verified with sentinel values in a successful `up`, in each secrets-file refusal, and in a missing-secrets refusal for another key.

### The env file

- R-O2UQ-1WEJ: An app's env file (D03) MUST hold one line for each variable of the app's environment, in ascending byte order of name, each `<name>="<value>"` and a newline, where `<value>` is the variable's value with a `\` written before each `\`, `"`, `` ` `` and `$` and every other character written unchanged, and MUST hold nothing else; verified at least by an `env` entry `X` whose value is `a"b\c$d`, a backtick, `e`, a tab and two spaces being written as `X="a\"b\\c\$d\`, a backtick, `e`, a tab, two spaces, `"` and a newline.

- R-VUN7-C5UV: An app's environment MUST hold `DRAIN_SECONDS` with the value `5`, `IKIGENBA_CALLBACK_URL` with the sandbox's callback origin (D03), `IKIGENBA_PUBLIC_URL` with the app's origin (D03), `IKIGENBA_SANDBOX` with the sandbox's name, and `IKIGENBA_SERVICES` with the path of the sandbox's services file (D03); verified at least by dummy's env file, after an `up` of the fixture checkout succeeds as sandbox `wip` on port 7400 with state root `S`, being exactly the five lines `DRAIN_SECONDS="5"`, `IKIGENBA_CALLBACK_URL="http://localhost:7400"`, `IKIGENBA_PUBLIC_URL="http://dummy.wip.localhost:7400"`, `IKIGENBA_SANDBOX="wip"` and `IKIGENBA_SERVICES="S/ikigenba/sandbox/wip/services.json"`, and by the same `IKIGENBA_PUBLIC_URL` line when dummy's manifest sets `default = true`.

- R-B43S-E58Y: An app's environment MUST hold each of its manifest's `env` entries, with the entry's value, verified at least by auth's env file holding the line `WORKSPACE_DOMAIN="michaelgreenly.dev"` for the fixture manifest.

- R-M8XN-4HEB: An app's environment MUST hold each secret its manifest lists, with the value the app's own table in the secrets file sets, verified at least by auth's env file holding the lines `GOOGLE_CLIENT_ID="1234-abc.apps.googleusercontent.com"` and `GOOGLE_CLIENT_SECRET="GOCSPX-example"` when the secrets file's `[auth]` table sets those values and both an `[aaa]` table and a `[billing]` table also set `GOOGLE_CLIENT_ID = "other"`.

- R-MA5J-I950: An app's environment MUST hold no variable other than those R-VUN7-C5UV, R-B43S-E58Y and R-M8XN-4HEB name, verified at least by auth's env file, with the secrets file's `[auth]` table also setting `SIGNING_KEY` and a `[billing]` table setting `STRIPE_KEY`, being exactly `DRAIN_SECONDS`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `IKIGENBA_CALLBACK_URL`, `IKIGENBA_PUBLIC_URL`, `IKIGENBA_SANDBOX`, `IKIGENBA_SERVICES` and `WORKSPACE_DOMAIN`, and dummy's env file holding none of auth's secrets or `env` entries and no `PORT`.

### The services file

- R-W0QP-90KC: The services file (D03) MUST be exactly the line `{`, the line `  "services": [`, one line for each app the `up` that wrote it discovered, in app-name order, the line `  ]` and the line `}`, each ending in a newline, where an app's line is four spaces, `{ `, its members each written `"<member>": <value>` and joined by `, `, then ` }`, followed by `,` on every app's line but the last.

- R-O5AI-TFVX: An app's members in the services file MUST be, in this order, `name` (the app's name), `url` (the app's origin, D03), `description` (its manifest's `description`), `socket` (the app's socket path, D03), `enabled` (`true`), `mcp` (its manifest's `mcp`), and, exactly when the app has an icon (R-O6IF-77MM), `icon` (the icon file's bytes); a string value MUST be written as Go's `encoding/json` encodes a string with HTML escaping turned off, and a boolean as `true` or `false`; verified at least, after an `up` of the fixture checkout as sandbox `wip` on port 7400 with `Deps.EUID` 1000 and dummy's manifest setting `default = true`, by the services file being exactly:

  ```
  {
    "services": [
      { "name": "auth", "url": "http://auth.wip.localhost:7400", "description": "", "socket": "/run/user/1000/sandbox/7400/auth.sock", "enabled": true, "mcp": false },
      { "name": "dummy", "url": "http://dummy.wip.localhost:7400", "description": "Demo widgets to list and create", "socket": "/run/user/1000/sandbox/7400/dummy.sock", "enabled": true, "mcp": true, "icon": "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n" }
    ]
  }
  ```

  when `dummy/share/icon.svg` holds `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>` and a newline and `auth/share/icon.svg` does not exist.

- R-O6IF-77MM: An app MUST have an icon exactly when `lstat(2)` of its icon file succeeds, whatever kind of file it names, a symbolic link not followed; an app with no icon MUST pass the icon checks, verified at least by an `up` that succeeds when `auth/share/` does not exist and when it is an empty directory, auth's line in the services file carrying no `icon` member.

- R-O7QB-KZDB: When an app has an icon and its icon file is not a regular file as `lstat(2)` reports it, `up` MUST write exactly `sandbox: <app>: share/icon.svg is not a regular file`, verified at least by an icon file that is a directory, a symbolic link to a regular file holding a valid SVG image of at most 65,536 bytes, a symbolic link to a path that does not exist, a named pipe with no writer (refused without waiting for one), and a Unix socket.

- R-O8Y7-YR40: When an app's icon file is a regular file that cannot be read, `up` MUST write exactly `sandbox: <app>: share/icon.svg: <reason>`, where `<reason>` is the operating-system error's message alone as D02's file-error diagnostic gives it, verified at least by an icon file of mode 000 holding a valid SVG image of at most 65,536 bytes giving `permission denied`.

- R-OA64-CIUP: When an app's icon file holds more than 65,536 bytes, `up` MUST write exactly `sandbox: <app>: share/icon.svg is larger than 64 KiB`, verified at least by a valid SVG image of 65,537 bytes and a file of 65,537 bytes that is neither valid UTF-8 nor an SVG image each writing it, while a valid SVG image of exactly 65,536 bytes is accepted, its bytes dummy's `icon` member in the services file.

- R-OBE0-QALE: When an app's icon file's bytes are not valid UTF-8 as Go's `unicode/utf8.Valid` decides, `up` MUST write exactly `sandbox: <app>: share/icon.svg is not valid UTF-8`, verified at least by an SVG image whose `<title>` holds `Caf` followed by the byte 0xE9, and by an otherwise valid SVG image whose comment holds the bytes 0xED 0xA0 0x80, while a valid SVG image beginning with the bytes 0xEF 0xBB 0xBF is accepted.

- R-U4C7-PY0T: An app's icon file MUST be an SVG image exactly when, with one leading byte sequence 0xEF 0xBB 0xBF set aside, Go's `encoding/xml` `Decoder`, with `Strict` true and no `CharsetReader`, reads its tokens to the end without error, they hold exactly one top-level element, whose local name is `svg` in any namespace or none, no element carries two attributes whose `Name.Space` and `Name.Local`, as the `Decoder` reports them, are equal, and every token outside that element is a comment, a processing instruction, a directive, or character data of only spaces, tabs, carriage returns and line feeds; when it is not, `up` MUST write exactly `sandbox: <app>: share/icon.svg is not an SVG image`, verified at least by an empty file, the text `widget icon`, `<?xml version="1.0" encoding="ISO-8859-1"?><svg/>`, `<svg>`, `<svg>&foo;</svg>`, `<svg xmlns="http://www.w3.org/2000/svg"><circle r="1" r="2"/></svg>`, `<svg xmlns:p="u" xmlns:q="u" p:a="1" q:a="2"/>`, `<html/>`, `<svg/><svg/>` and `<svg/>text` each writing it, and by `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE svg><!-- icon --><svg xmlns="http://www.w3.org/2000/svg"/>` and a newline, `<s:svg xmlns:s="http://www.w3.org/2000/svg"/>`, `<svg a="1" xmlns:p="u" p:a="2"/>`, and the bytes 0xEF 0xBB 0xBF followed by `<svg/>`, each accepted.
