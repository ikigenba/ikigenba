# certbot hook validation, 2026-09-17

An authoring observation supporting D07's hook requirements (`R-K9Q8-1AET`,
`R-KAY4-F25I`). It is not a build run and no gate was involved.

## What was observed

Host: the live box `ikigenba.dev` (Amazon Linux 2023), certbot 2.6.0 from
the distribution package. Run as root by an operator on 2026-09-17.

`sudo opsctl init` passed its whole preflight, then failed at the
`certificate` step. stderr, verbatim:

```
opsctl: certbot certonly: exit status 1

> Unable to find deploy-hook command if in the PATH.
> (PATH is /usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/var/lib/snapd/snap/bin)
> See also the --disable-hook-validation option.
```

The deploy hook opsctl passed was the shell fragment
`if systemctl is-active --quiet nginx; then systemctl reload nginx; fi`.
certbot split it like a shell command line, took its first word `if`, looked
that word up on PATH, and refused the invocation. No ACME request, DNS write,
or certificate change happened; the failure preceded all of them.

## What it proves

- certbot validates every hook it is given (`--manual-auth-hook`,
  `--manual-cleanup-hook`, `--deploy-hook`) before performing any ACME work,
  by resolving the hook string's first shell word on PATH.
- A hook whose first word is a shell keyword, operator, or compound command
  is rejected outright. A hook whose first word is an executable on PATH
  passes: the auth and cleanup hooks `opsctl dns acme-auth` and
  `opsctl dns acme-cleanup` were accepted in the same invocation, and only the
  deploy hook was named in the refusal.
- The message quotes the first word and the PATH searched, and points at
  `--disable-hook-validation`. opsctl does not use that option; the fix is a
  hook that is a plain command.

## What it does not prove

- The behavior of `systemctl try-reload-or-restart nginx` itself (reload a
  running unit, do nothing for a stopped one, fail when the reload fails) is
  taken from systemd's published interface and has not yet been exercised on
  `dev` as part of this observation. `R-YPZO-4SX8` states the required
  observable outcome; confirming it on the host is a one-line check with
  nginx stopped and with it running.
- Nothing here exercises issuance, renewal, or the DNS-01 round-trip; those
  remain as previously observed or as pending pre-check items.

## Why the tests did not catch it

The `internal/cert` fixture ran hooks through `sh -c`, which accepts a shell
fragment. That is a fixture that knows more about the hook than certbot
allows, which is exactly what `R-KAY4-F25I` now forbids at the boundary.
