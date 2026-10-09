# D02-cli

webhooks as a command: what `cli.Run` does with the arguments, the exit codes, `db status`, and the three constants whose text is fixed here — the usage text, the manifest and the nginx fragment — with the two files under `etc/` that copy the last two. It is cron's command with webhooks' name, its own manifest and its own fragment.

An empty `Args` means serve (`D03-serve`). `--version` writes `Process.Version` and a newline; `manifest` writes `Manifest`; `--help` writes `Usage`; `db status` writes what appkit's `db.Status` writes for `state/webhooks.db` and `webhooks.Migrations()`. Any other `Args` is a usage error, diagnosed as cron diagnoses one: `webhooks: unknown option '<arg>'` or `webhooks: unknown command '<arg>'`, an empty line, and the hint to run `webhooks --help`. A command touches nothing but what it names.

## The manifest

The manifest holds webhooks' name; its description, `pages.Description`; that it is not the default app; that it is an MCP service; that it serves guests (`guests = true`), so the host's nginx admits a request with no credential to `location /` through auth's `/check/open`, which is how a sender outside the suite reaches the ingress, while `/mcp` still requires a credential; no secrets; an `[env]` table setting `WEBHOOKS_RETENTION_DAYS` to `2`; the `[database]` table that has the host keep and replicate `state/webhooks.db`; and a `[resources]` table whose only line is `memory_max = "128M"`.

## The nginx fragment

The fragment, `NginxConf`, raises nginx's `client_max_body_size` to 2 MiB at webhooks' public name, above the ingress's 1 MiB cap, so that an oversized delivery is answered 413 by webhooks (`D05-ingress`) and not by nginx's default 1 MiB limit; and it answers the event bus's two paths, `/events` and `/declarations`, 404 at the public name, as every app's does.

## Recorded decisions

- The fragment's body size is `2m`: the smallest round value above the 1 MiB cap.
- `Manifest` and `NginxConf` are copied into `etc/` byte for byte, which a test proves through the root package's `Etc`.

## REQUIREMENTS

- R-XH49-X1GK: The `internal/cli` package MUST export `Usage` as a string constant whose value is exactly `"Usage: webhooks [command]\n\nAccept deliveries from senders outside the suite at /in/<slug> and emit\neach as an event on the suite's event bus, with MCP tools at /mcp and a\npage of webhooks at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"`.
- R-XIC6-AT79: The `internal/cli` package MUST export `Manifest` as a string constant whose value is exactly `app = "webhooks"\ndescription = "` followed by the text of `pages.Description` followed by `"\ndefault = false\nmcp = true\nguests = true\nsecrets = []\n\n[env]\nWEBHOOKS_RETENTION_DAYS = "2"\n\n[database]\nengine = "sqlite"\npath = "state/webhooks.db"\n\n[resources]\nmemory_max = "128M"\n`.
- R-XKRZ-2CON: The `internal/cli` package MUST export `const NginxConf = "client_max_body_size 2m;\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n"`.
- R-XLZV-G4FC: The `internal/cli` package MUST export `ExitSuccess`, `ExitServerFailed` and `ExitUsage` as untyped integer constants with the values 0, 1 and 2, and every call of `Run` MUST return one of them.
- R-XN7R-TW61: The contents of `manifest.toml` and `nginx.conf` in the file system the root package's `Etc` returns MUST be exactly the bytes of `cli.Manifest` and `cli.NginxConf` respectively.
- R-XOFO-7NWQ: When `Args` is exactly `["--version"]`, `["manifest"]` or `["--help"]`, `Run` MUST write exactly `p.Version + "\n"`, `Manifest` or `Usage` respectively to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-XPNK-LFNF: `Run` MUST treat `Args` as a usage error unless it is empty or exactly one of `["--version"]`, `["manifest"]`, `["--help"]` and `["db", "status"]`; the offending argument MUST be `Args[0]` when it is none of `--version`, `manifest`, `--help` and `db` or when `Args` is exactly `["db"]`, `Args[2]` when `Args[0]` is `db` and `Args[1]` is `status`, and `Args[1]` otherwise.
- R-XQVG-Z7E4: On a usage error from `Args`, `Run` MUST write exactly `"webhooks: unknown option '" + arg + "'\n\nsee 'webhooks --help' for usage\n"` to `Stderr` in one write when the offending argument `arg` begins with `-`, and exactly `"webhooks: unknown command '" + arg + "'\n\nsee 'webhooks --help' for usage\n"` otherwise, write nothing to `Stdout`, and return `ExitUsage`.
- R-XS3D-CZ4T: When `Args` is not empty, `Run` MUST return without calling `LookupEnv`, `Unsetenv`, `Inherit`, `Now`, `Sleep`, `After`, `Banner` or `MCP`, without reading `Rand` or calling the `Deliver` of `Sink` or `EventSink`, and, unless `Args` is exactly `["db", "status"]`, without creating, removing or changing anything under `Dir`.
- R-XTB9-QQVI: When `Args` is exactly `["db", "status"]`, `Run` MUST write to `Stdout` exactly the bytes appkit's `db.Status` writes for a `db.Config` whose `Path` is `filepath.Join(Dir, "state", "webhooks.db")` and whose `Migrations` is `webhooks.Migrations()` over the database as it is when `Run` is called, create nothing under a `Dir` that names an empty directory, and return `ExitSuccess` with nothing on `Stderr` when that call returns nil; and when it returns an error `err`, write exactly `"webhooks: " + r + "\n"` to `Stderr` in one write, `r` being `err.Error()` with every newline replaced by a space, and return `ExitServerFailed`.
