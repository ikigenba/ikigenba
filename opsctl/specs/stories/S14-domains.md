# Stories — domains

A custom domain is a DNS name outside the space's own names whose A record points at the host's address. It is one of two kinds. A primary name serves a site: nginx routes it to the `sites` app, and which site it serves is `sites`' to decide, set there by an agent; opsctl neither asks `sites` nor tells it. A redirect name, such as `www.example.com`, belongs to one primary name, such as `example.com`, and nginx answers every request to it with a 301 to that primary. Adding `example.com` never adds `www.example.com`; a second name for a site is a redirect name added on its own. opsctl keeps the list of custom domains, each a name and, for a redirect name, its primary, in the configuration store `/etc/ikigenba/config.json`, so the host backup carries it; `opsctl domain list` shows it. No configuration key is added for an operator to set by hand.

Each name has a certificate lineage of its own, redirect names included, named after the name and holding that one name, at `/etc/letsencrypt/live/<name>/`. It is obtained by HTTP-01 from the webroot `/var/www/acme`, which the port-80 server serves at `/.well-known/acme-challenge/` on every host (`S05-nginx.md`), so a first add needs no nginx change before its challenge. opsctl does not look the name up in DNS before asking: the CA's refusal is the check. Renewal is `ikigenba-renew-certificate.timer`'s (`S06-certificates.md`), and there is no second timer: its `certbot renew` renews each name's lineage by HTTP-01 from the same webroot and reloads nginx afterwards, and a name that has stopped pointing at the host fails only its own renewal. The host's own lineage, its names, its DNS-01 hooks and the apex are unchanged. What the server blocks for a primary name and a redirect name look like is `S05-nginx.md`'s; applying nginx here means exactly what `opsctl nginx apply` does there.

The top-level usage gains the line `  domain    manage the host's custom domains` under `Commands:`. The command runs as root (`S01-bootstrap.md`); `--help` and `-h` print for any user. It reads the configuration keys `acme.email`, `host.name` and `host.apex`.

The command answers with the exit codes of the top-level usage: `0  success`, `1  the operation failed`, `2  usage error, or a preflight check failed`, `3  refused: opsctl must run as root`, and `4  refused: the host's state does not allow it`. A refusal (4) is one line on stderr naming the case, with no usage hint. Malformed input (2) is the usual form on stderr: `opsctl: <message>`, an empty line, then `see 'opsctl domain --help' for usage`. A host tool that fails (1) puts `opsctl: <tool>: exit status <n>` on stderr, an empty line, then the tool's output with every line prefixed `> `; stdout is empty. `add` and `remove` print nothing on success. `add` checks, in order, the form of its arguments (2), that `host.name` and `acme.email` are set (1), and the refusals (4), all before anything is obtained or written; then it obtains the certificate, stores the name, and applies nginx. `remove` takes the name out of the store, applies nginx, and deletes the name's lineage.

## An operator asks what `domain` can do

Command:

```
$ opsctl domain --help
```

```
$ opsctl domain -h
```

Output:

```
Usage: opsctl domain <subcommand> [arguments]

Manage the host's custom domains: names outside host.name whose A record points
at this host. A primary name is served by the sites app, which decides the site;
a redirect name answers 301 to its primary. Each name has a certificate of its
own, obtained by HTTP-01 from /var/www/acme.

Subcommands:
  add NAME     obtain NAME's certificate, add NAME, and apply nginx
  remove NAME  remove NAME, apply nginx, and delete NAME's certificate; refused
               while a redirect name points at NAME
  list         print each name, and a redirect name's primary

Options (add):
  --redirect PRIMARY  make NAME redirect to PRIMARY, a listed primary name

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at
  host.apex   the app that answers at the parent of host.name; unset means none

NAME is a host name in lower-case ASCII; an internationalized name is given in
its xn-- form. It may not be host.name or a name under it, nor, while host.apex
is set, the parent of host.name. Renewal is certbot's: the timer 'opsctl init'
writes, ikigenba-renew-certificate.timer, renews every name's certificate.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator adds a primary name

The owner has pointed `example.com`'s A record at the host and wants a site served there. The operator adds the name; an agent sets it as the site's domain in `sites`, before or after, in either order.

Command:

```
$ sudo opsctl domain add example.com
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `host.name` is `sbx.ikigenba.dev` and `acme.email` is set; `certbot`, `nginx` and `systemctl` are on the PATH.
- `example.com`'s A record points at the host's address, and the host can reach the CA.
- nginx has been applied, so the port-80 server answers `/.well-known/acme-challenge/` from `/var/www/acme`.
- `example.com` is not in the list.

Postconditions:

- `/etc/letsencrypt/live/example.com/` holds a certificate for `example.com` alone, obtained by HTTP-01 from `/var/www/acme`. The host's own lineage at `/etc/letsencrypt/live/sbx.ikigenba.dev/` is untouched.
- When a current lineage for `example.com` was already on disk, left by an earlier add that failed or by a remove that could not delete it, it was kept and the CA was not asked for anything.
- `opsctl domain list` prints the line `example.com`.
- nginx was applied exactly as `opsctl nginx apply` does: `/etc/nginx/conf.d/ikigenba.conf` holds a server block for `example.com` (`S05-nginx.md`), `nginx -t` passed, and nginx was reloaded. `/var/www/acme` exists with mode `0755`, root-owned as opsctl created it, created if it was missing.
- `certbot renew`, run by `ikigenba-renew-certificate.timer`, renews the lineage by HTTP-01 and reloads nginx afterwards.
- `sites` was not asked or told anything.

## An operator adds a redirect name

`www.example.com` should land on `example.com`. It gets a lineage of its own, so if it stops pointing at the host only its own renewal fails.

Command:

```
$ sudo opsctl domain add www.example.com --redirect example.com
```

Options (add):

- `--redirect PRIMARY` makes the name a redirect name to `PRIMARY`, which must be a primary name already in the list.

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `host.name` is `sbx.ikigenba.dev` and `acme.email` is set.
- `example.com` is in the list as a primary name.
- `www.example.com`'s A record points at the host's address, and the host can reach the CA.

Postconditions:

- `/etc/letsencrypt/live/www.example.com/` holds a certificate for `www.example.com` alone. The lineage of `example.com` is untouched.
- `opsctl domain list` prints the line `www.example.com example.com`.
- nginx was applied: `/etc/nginx/conf.d/ikigenba.conf` holds a server block for `www.example.com` that answers every request with a 301 to `https://example.com` and the request's own target (`S05-nginx.md`).

## An operator lists the custom domains

One line per name, sorted by name. A primary name's line is the name alone; a redirect name's line is the name, a space, and its primary.

Command:

```
$ sudo opsctl domain list
```

Output:

```
example.com
www.example.com example.com
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `opsctl` is running as root.
- `example.com` is in the list as a primary name, and `www.example.com` as a redirect name to it.

Postconditions:

- Nothing has changed.

## An operator lists the custom domains on a host that has none

Command:

```
$ sudo opsctl domain list
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- The list is empty.

Postconditions:

- Nothing has changed.

## An operator removes a redirect name

Command:

```
$ sudo opsctl domain remove www.example.com
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `www.example.com` is in the list as a redirect name to `example.com`, and its lineage is at `/etc/letsencrypt/live/www.example.com/`.

Postconditions:

- `opsctl domain list` no longer prints `www.example.com`.
- nginx was applied: `/etc/nginx/conf.d/ikigenba.conf` holds no block for `www.example.com`, so a handshake for it is rejected.
- `/etc/letsencrypt/live/www.example.com/` no longer exists; certbot deleted the lineage and no longer renews it.
- `example.com`, its block and its lineage are untouched.

## An operator removes a primary name

Command:

```
$ sudo opsctl domain remove example.com
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `example.com` is in the list as a primary name, no redirect name points at it, and its lineage is at `/etc/letsencrypt/live/example.com/`.

Postconditions:

- `opsctl domain list` no longer prints `example.com`.
- nginx was applied: `/etc/nginx/conf.d/ikigenba.conf` holds no block for `example.com`, so a handshake for it is rejected.
- `/etc/letsencrypt/live/example.com/` no longer exists; certbot deleted the lineage and no longer renews it.
- `sites` was not told. A site whose domain is `example.com` keeps it there until an agent clears it.

## An operator removes a primary name a redirect name still points at

The redirect names go first, one `opsctl domain remove` each.

Command:

```
$ sudo opsctl domain remove example.com
```

Output:

```
opsctl: domain has redirects: example.com
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `example.com` is in the list as a primary name, and `www.example.com` is in it as a redirect name to `example.com`.

Postconditions:

- Nothing has changed. Both names, their blocks and their lineages are as they were.

## An operator removes a name that is not a custom domain

Command:

```
$ sudo opsctl domain remove example.com
```

Output:

```
opsctl: no domain 'example.com'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `example.com` is not in the list, and `/etc/letsencrypt/live/example.com/` does not exist.

Postconditions:

- Nothing has changed.

## An operator finishes a remove whose certificate was left behind

An earlier remove took the name out of the list and out of nginx, but certbot could not delete its lineage. Running the same remove again deletes what is left.

Command:

```
$ sudo opsctl domain remove example.com
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl` is running as root.
- `example.com` is not in the list, and its lineage is at `/etc/letsencrypt/live/example.com/`.

Postconditions:

- `/etc/letsencrypt/live/example.com/` no longer exists; certbot deleted the lineage and no longer renews it.
- The list is unchanged.

## An operator adds a name that is not a lower-case host name

A name is a host name in lower-case ASCII. An upper-case letter, a character a host name cannot hold, or an internationalized name written in Unicode is refused as malformed; the internationalized name is accepted in its `xn--` form, `xn--bcher-kva.de` for `bücher.de`.

Command:

```
$ sudo opsctl domain add Example.com
```

Output:

```
opsctl: invalid name: Example.com

see 'opsctl domain --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `exa_mple.com` and `bücher.de` are answered the same way, `opsctl: invalid name: exa_mple.com` and `opsctl: invalid name: bücher.de`. The same check applies to `PRIMARY` in `--redirect PRIMARY` and to `remove`'s `NAME`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed. certbot was not run.

## An operator adds the host's own name or a name under it

The space's own names are served under the host's own certificate and are never custom domains.

Command:

```
$ sudo opsctl domain add blog.sbx.ikigenba.dev
```

Output:

```
opsctl: name belongs to the host: blog.sbx.ikigenba.dev
```

Exits 4. The line is on stderr; stdout is empty. `sudo opsctl domain add sbx.ikigenba.dev` gives `opsctl: name belongs to the host: sbx.ikigenba.dev` and exits 4 the same way. `remove` refuses these names with the same line and exit, so the host's own lineage is never reachable through `domain remove`.

Preconditions:

- `opsctl` is running as root.
- `host.name` is `sbx.ikigenba.dev` and `acme.email` is set.

Postconditions:

- Nothing has changed. certbot was not run, and the host's own lineage is untouched.

## An operator adds the apex while the host holds it

The parent of `host.name` is served under the host's own certificate while `host.apex` is set (`S06-certificates.md`). With `host.apex` unset, that name is an ordinary name and can be added.

Command:

```
$ sudo opsctl domain add ikigenba.dev
```

Output:

```
opsctl: name is the apex: ikigenba.dev
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `host.name` is `sbx.ikigenba.dev`, `acme.email` is set, and `host.apex` is `crm`.

Postconditions:

- Nothing has changed. certbot was not run.

## An operator adds a name already listed

Command:

```
$ sudo opsctl domain add example.com
```

Output:

```
opsctl: domain exists: example.com
```

Exits 4. The line is on stderr; stdout is empty. A name already listed is refused whatever kind it is listed as or is being added as: `sudo opsctl domain add www.example.com --redirect example.com` with `www.example.com` listed gives `opsctl: domain exists: www.example.com`.

Preconditions:

- `opsctl` is running as root.
- `host.name` and `acme.email` are set.
- `example.com` is in the list.

Postconditions:

- Nothing has changed. certbot was not run.

## An operator adds a redirect name whose primary is not listed

Command:

```
$ sudo opsctl domain add www.example.com --redirect example.com
```

Options (add):

- `--redirect PRIMARY` makes the name a redirect name to `PRIMARY`, which must be a primary name already in the list.

Output:

```
opsctl: no domain 'example.com'
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `host.name` and `acme.email` are set.
- `example.com` is not in the list.

Postconditions:

- Nothing has changed. certbot was not run.

## An operator adds a redirect name whose primary is a redirect name

A redirect name points at a primary name, never at another redirect name.

Command:

```
$ sudo opsctl domain add web.example.com --redirect www.example.com
```

Options (add):

- `--redirect PRIMARY` makes the name a redirect name to `PRIMARY`, which must be a primary name already in the list.

Output:

```
opsctl: not a primary name: www.example.com
```

Exits 4. The line is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `host.name` and `acme.email` are set.
- `www.example.com` is in the list as a redirect name to `example.com`.

Postconditions:

- Nothing has changed. certbot was not run.

## An operator adds a name and the CA refuses

The usual cause is a name whose A record does not yet point at the host. certbot's output follows the error line, each line quoted with `> `.

Command:

```
$ sudo opsctl domain add example.com
```

Output:

```
opsctl: certbot certonly: exit status 1

> Some challenges have failed.
> Detail: DNS problem: NXDOMAIN looking up A for example.com - check that a DNS record exists for this domain
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `host.name` is `sbx.ikigenba.dev` and `acme.email` is set.
- `example.com` is not in the list.
- The CA could not reach the challenge at `example.com`, or refused for any other reason.

Postconditions:

- The list is unchanged: `example.com` is not in it.
- `/etc/nginx/conf.d/ikigenba.conf` is as it was, and nginx was not reloaded.
- No lineage was written. An earlier lineage for `example.com`, if there was one, is exactly as it was.

## An operator adds a name and nginx refuses

The certificate was obtained and the name stored, but nginx rejected the configuration. The name is taken back out of the list; the certificate is kept, so running the same add again does not ask the CA.

Command:

```
$ sudo opsctl domain add example.com
```

Output:

```
opsctl: nginx -t: exit status 1

> nginx: [emerg] unknown directive "proxy_pas" in /etc/nginx/conf.d/other.conf:3
> nginx: configuration file /etc/nginx/nginx.conf test failed
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `host.name` is `sbx.ikigenba.dev` and `acme.email` is set.
- `example.com` is not in the list; its A record points at the host and the CA issues its certificate.
- nginx rejects the configuration; here another file under `/etc/nginx` is broken.

Postconditions:

- The list is as it was before the command: `example.com` is not in it.
- `/etc/letsencrypt/live/example.com/` holds the certificate just obtained, and is kept.
- `/etc/nginx/conf.d/ikigenba.conf` is exactly as it was before the command, and nginx was not reloaded.
- Once nginx accepts the configuration, running `sudo opsctl domain add example.com` again reuses the kept lineage without asking the CA, stores the name, applies nginx, and exits 0.

## An operator removes a name and nginx refuses

The name was taken out of the list, but nginx rejected the configuration. The name is put back in the list and its certificate is not deleted, so the host is as it was and the same remove can be run again.

Command:

```
$ sudo opsctl domain remove www.example.com
```

Output:

```
opsctl: nginx -t: exit status 1

> nginx: [emerg] unknown directive "proxy_pas" in /etc/nginx/conf.d/other.conf:3
> nginx: configuration file /etc/nginx/nginx.conf test failed
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `www.example.com` is in the list as a redirect name to `example.com`, and its lineage is at `/etc/letsencrypt/live/www.example.com/`.
- nginx rejects the configuration; here another file under `/etc/nginx` is broken.

Postconditions:

- The list is as it was before the command: `opsctl domain list` still prints `www.example.com example.com`.
- `/etc/nginx/conf.d/ikigenba.conf` is exactly as it was before the command, and nginx was not reloaded; it still answers `www.example.com`.
- `/etc/letsencrypt/live/www.example.com/` is kept; certbot was not asked to delete it.
- Once nginx accepts the configuration, running `sudo opsctl domain remove www.example.com` again takes the name out of the list, applies nginx, deletes the lineage, and exits 0.

## An operator removes a name and certbot cannot delete its lineage

The name is already out of the list and out of nginx when certbot fails. The last line, unprefixed, is opsctl's own: the next command to run.

Command:

```
$ sudo opsctl domain remove example.com
```

Output:

```
opsctl: certbot delete: exit status 1

> An unexpected error occurred:
> PermissionError: [Errno 13] Permission denied: '/etc/letsencrypt/renewal/example.com.conf'

the certificate for example.com remains; run 'opsctl domain remove example.com' again to delete it
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.
- `example.com` is in the list as a primary name with no redirect name pointing at it, and its lineage is at `/etc/letsencrypt/live/example.com/`.
- `certbot delete` fails.

Postconditions:

- `opsctl domain list` no longer prints `example.com`.
- nginx was applied and holds no block for `example.com`.
- `/etc/letsencrypt/live/example.com/` remains. Running `sudo opsctl domain remove example.com` again deletes it and exits 0.

## An agent adds a name on an unconfigured host

The same checks `opsctl cert obtain` makes, made after the arguments' form and before the refusals.

Command:

```
$ sudo opsctl domain add example.com
```

Output:

```
opsctl: acme.email not set
```

Exits 1. The line is on stderr; stdout is empty. With `host.name` unset the line is `opsctl: host.name not set`.

`sudo opsctl domain remove example.com` needs `host.name` too, but not `acme.email`: with `host.name` unset it answers `opsctl: host.name not set`, and for `add` and `remove` alike, with `host.apex` set and `host.name` `localhost`, which has no parent domain, the line is `opsctl: host.apex is set but host.name 'localhost' has no parent domain`; each exits 1 and changes nothing. A `host.domains` someone edited by hand into a value opsctl did not write, such as `example.com,Example.org`, makes `add`, `remove` and `list` answer `opsctl: host.domains malformed: "Example.org"` on stderr, exit 1 and change nothing.

Preconditions:

- `opsctl` is running as root.
- `acme.email` is unset or empty.

Postconditions:

- Nothing has changed. certbot was not run.

## An operator runs `domain` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl domain
```

Output:

```
opsctl: no domain subcommand given

see 'opsctl domain --help' for usage
```

Command:

```
$ sudo opsctl domain delete example.com
```

Output:

```
opsctl: unknown domain subcommand 'delete'

see 'opsctl domain --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator gives a domain subcommand too few or too many arguments, or an option it does not take

Each subcommand takes exactly the arguments its help line names, and only `add` takes an option. Too few says what the subcommand needs; too many says what it takes.

Command:

```
$ sudo opsctl domain add
```

Output:

```
opsctl: domain add needs NAME

see 'opsctl domain --help' for usage
```

Command:

```
$ sudo opsctl domain add example.com www.example.com
```

Output:

```
opsctl: domain add takes one NAME

see 'opsctl domain --help' for usage
```

Command:

```
$ sudo opsctl domain add www.example.com --redirect
```

Options (add):

- `--redirect PRIMARY` makes the name a redirect name to `PRIMARY`, which must be a primary name already in the list.

Output:

```
opsctl: option '--redirect' needs a value

see 'opsctl domain --help' for usage
```

Command:

```
$ sudo opsctl domain add example.com --bogus
```

Output:

```
opsctl: unknown option '--bogus'

see 'opsctl domain --help' for usage
```

Each exits 2. The text is on stderr; stdout is empty. The other subcommands answer the same way, each first line followed by the same empty line and usage hint:

- `sudo opsctl domain remove` gives `opsctl: domain remove needs NAME`.
- `sudo opsctl domain remove example.com www.example.com` gives `opsctl: domain remove takes one NAME`.
- `sudo opsctl domain list example.com` gives `opsctl: domain list takes no arguments`.
- `sudo opsctl domain remove www.example.com --redirect example.com` gives `opsctl: unknown option '--redirect'`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed. certbot was not run.
