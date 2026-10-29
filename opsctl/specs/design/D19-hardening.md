# D19-hardening

`hardening` is the setup step that brings a host to the rules the people who
sign in to it live under (S11). It is one idempotent operation, `Apply` in
package `internal/hardening`, which `init` runs after `slices` (D05) and
`activate` and `rollback` run right after `resources` (D18). The step knows
nothing of the space: every value it writes is a constant of the package, the
same on every host, and no configuration key changes it. The package also owns
the three names the account commands (D20, D21) build on: the shell group
`shell`, the keys directory `/etc/ssh/authorized_keys`, and the admin account
`ec2-user`.

The step's parts, in the order it runs them:

- The account `ikigenba`, ensured through `apps.EnsureAccount` (D08) before
  anything names it, since in `init` the step runs before the services file
  creates it.
- The resource pool: one drop-in for `user.slice`, which holds every login
  session, and one template drop-in, `user-.slice.d/`, which systemd applies
  to every `user-<uid>.slice` (systemd.unit(5), prefix drop-ins). systemd is
  reloaded only when one changed.
- The group `shell`, created as a system group when `getent` does not find it.
- The keys directory, root's and readable by sshd for every account.
- sshd's drop-in `10-ikigenba.conf`, which sorts before the image's own
  drop-ins and so wins on every first-value directive. A new or changed
  drop-in is renamed into place and the host's own configuration is checked
  with `sshd -t -f /etc/ssh/sshd_config` (sshd(8)), so the check judges
  exactly what sshd will run with, an older drop-in of opsctl's replaced
  rather than read beside the new one, and a line opsctl did not write is
  reported under its own path. A running sshd does not reread its files
  between reloads: the listener hands each connection the configuration it
  loaded (sshd.c, the config buffer sent to every child), and rereads only on
  SIGHUP or restart. So when the check fails, or the reload after it, the
  step puts the previous drop-in back, or removes the new one, before it
  returns, and the failing configuration is never installed. A kill between
  the rename and the check or reload can leave the new drop-in in place
  unchecked; that window is accepted, and the next run checks it. The
  drop-in also sets `UsePAM yes` before its `Match` block (sshd_config(5):
  a global keyword, not one `Match` accepts, that turns on PAM account and
  session processing for every authentication type), so the account expiry
  that locks a person (D21) holds whatever the image's `50-redhat.conf`
  says.
- The packages `fail2ban-server` and `dnf-automatic`, each queried with
  `rpm -q`, and one `dnf install -y` for whichever are missing.
  `fail2ban-server` rather than the `fail2ban` metapackage, which would also
  bring the firewalld action; on the production host `fail2ban-server`
  requires `nftables`, which dnf installs with it, and ships
  `action.d/nftables-multiport.conf`. Neither firewalld nor
  `nftables.service` runs there, and the step enables neither: the action
  needs only the `nft` program.
- fail2ban's jail, a `.local` file under `jail.d/` (jail.conf(5): `.local`
  files are read after every `.conf` and win), banning through
  `banaction = nftables-multiport`, enabled and started, and reloaded when
  the jail changed while fail2ban was running.
- The release lock lifted: `/etc/dnf/vars/releasever` holding `latest`.
  An Amazon Linux 2023 host is locked to the release of its installed
  `system-release`, so dnf, and dnf-automatic with it, sees no update
  published after that release unless this file says `latest` (AWS,
  "Using deterministic upgrades through versioned repositories": the file
  overrides `system-release` and is absent on a new host). On the
  production host the security list is empty under the lock and holds
  148 entries with `--releasever=latest`. The file is written before
  dnf-automatic's timer is enabled; it runs no command, so nothing undoes
  it, and a later failure leaves it written.
- dnf-automatic's settings, set in place in the packaged
  `/etc/dnf/automatic.conf`: the packaged file carries `upgrade_type =
  default` and `apply_updates = no` among settings the timer also needs
  (`download_updates`, `network_online_timeout`, the emitters), so the step
  sets its two keys in `[commands]` and keeps every other line. Its timer is
  enabled and started.
- The sudo rule, staged as `/etc/sudoers.d/ikigenba-accounts.new` (a name
  sudo skips, sudoers(5), because it contains a `.`), checked with
  `visudo -c -f` (visudo(8)), and renamed into place only when it passes.
- The five state directories closed to others: each that exists gets the
  owner and mode below, `/run/ikigenba` also the named ACL entry that lets
  nginx reach the sockets, and everything beneath loses its others'
  permission bits and nothing else. Owners and the ACL are read and written
  through commands (`stat`, `chown`, `getfacl`, `setfacl`); modes directly.
  `/run` is a tmpfs, and the production host's kernel supports ACLs there
  (`CONFIG_TMPFS_POSIX_ACL=y`; `setfacl -m u:nginx:x` on a directory under
  `/run` took effect).

| Path | Owner | Mode |
| --- | --- | --- |
| `/etc/ikigenba` | `root:root` | `0700` |
| `/etc/opt/ikigenba` and each directory directly in it | `root:root` | `0700` |
| `/var/opt/ikigenba` | `root:ikigenba` | `0750` |
| `/var/lib/ikigenba` | `root:ikigenba` | `0750` |
| `/run/ikigenba` | `root:ikigenba` | `0750`, plus `user:nginx:--x` |

Nothing in the step undoes anything: a part that fails installs nothing of its
own, the parts before it stay done, and the next run finds them done. The
error a failed command gives is that command's `*host.CommandError` alone, so
the callers print `sshd -t: exit status 255` or `dnf install: exit status 1`
as the stories show and quote the tool's output (D02). A host that already
holds every rule runs only the step's queries and reports no change.

## REQUIREMENTS

- R-QP1Z-BOQ4: Package `internal/hardening` MUST export the untyped string constants `ShellGroup = "shell"`, `KeysDir = "/etc/ssh/authorized_keys"`, and `AdminAccount = "ec2-user"`.
- R-QQ9V-PGGT: Package `internal/hardening` MUST export `Apply(ctx context.Context, env host.Env) (changed bool, err error)`.
- R-ZC9B-Q97J: `Apply` MUST run its parts in this order, each finishing before the next begins, so the commands it executes through `env.Execute` appear in this order: the account (R-ZB1F-CHGU), the pool (R-SALL-UGL5), the shell group (R-QV5H-8JFL), the keys directory (R-QWDD-MB6A), sshd (R-3AA6-8JF9), the packages (R-3BI2-MB5Y), fail2ban (R-SD1E-M02J), the release lock (R-3DXV-DUNC), dnf-automatic (R-SE9A-ZRT8), the sudo rule (R-98VK-1BB5), and the state directories (R-YEOK-DJ3V); every host path it reads, writes, or passes in a command's arguments MUST be resolved under `env.Root` (D01), and every command MUST be executed through `env.Execute`.
- R-ZB1F-CHGU: `Apply` MUST first execute `id --user ikigenba`, treating exit status 1 as an absent account and exit status 0 as a present one, and then call `apps.EnsureAccount(ctx, env)` (D08), so the account `ikigenba` exists before any later part names it; an account that was absent MUST make `changed` true. When `EnsureAccount` returns an error, `Apply` MUST stop at once and return, when `errors.As` finds a `*host.CommandError` in that error, that `*host.CommandError` itself, unchanged, its `Label` and `Result` as `EnsureAccount` set them and no prefix added to its `Error()`, and otherwise the error unchanged.
- R-SALL-UGL5: `Apply` MUST make `/etc/systemd/system/user.slice.d/ikigenba.conf` hold exactly `"[Slice]\nMemoryHigh=1.5G\nMemoryMax=2G\nTasksMax=512\nCPUWeight=50\n"` and `/etc/systemd/system/user-.slice.d/ikigenba.conf` exactly `"[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n"`, `\n` denoting one LF byte, each with mode `0644`, creating either directory with mode `0755` when absent; it MUST rewrite neither file when its bytes are already right (setting only its mode when that differs), and after both MUST execute `systemctl daemon-reload` exactly when either file was created or its bytes changed, and otherwise no command for the pool, so a test observes the bytes and modes with `Lstat` and reads under `env.Root` and the reload in the recorded commands. When that `systemctl daemon-reload` cannot be run or exits nonzero, `Apply` MUST, before returning its error, put each drop-in it created or rewrote back to its previous bytes and mode, or remove it when it was absent, so the next run finds the pool not done and writes and reloads again.
- R-QV5H-8JFL: `Apply` MUST execute `getent group shell` (`ShellGroup`), treat exit status 0 as a present group and exit status 2 as an absent one (getent(1)), and only for an absent group execute `groupadd --system shell` (groupadd(8)), which makes `changed` true; it MUST NOT add any account to the group.
- R-QWDD-MB6A: `Apply` MUST leave `KeysDir` under `env.Root` a directory with mode `0755`, creating it (and `/etc/ssh/` when absent, mode `0755`) when absent and setting its mode when it differs, and MUST NOT create, modify, or remove anything inside it.
- R-3929-UROK: *The sshd drop-in* MUST be `/etc/ssh/sshd_config.d/10-ikigenba.conf` holding exactly `"PubkeyAuthentication yes\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\nLoginGraceTime 30\nMaxAuthTries 3\nClientAliveInterval 300\nClientAliveCountMax 2\nAllowGroups ec2-user shell\nUsePAM yes\n\nMatch Group shell\n  AllowTcpForwarding no\n  AllowAgentForwarding no\n  X11Forwarding no\n  PermitTunnel no\n  AuthorizedKeysFile /etc/ssh/authorized_keys/%u\n"`, `\n` denoting one LF byte, where `ec2-user` is `AdminAccount`, `shell` is `ShellGroup`, and `/etc/ssh/authorized_keys` is `KeysDir` written as the host path, never resolved under `env.Root`, with mode `0600`.
- R-3AA6-8JF9: When `/etc/ssh/sshd_config.d/10-ikigenba.conf` under `env.Root` is absent or its bytes differ from R-3929-UROK's, `Apply` MUST put those bytes in place with mode `0600` by renaming a temporary file over the drop-in, then execute `sshd -t -f <C>`, `<C>` being `/etc/ssh/sshd_config` resolved under `env.Root`, so sshd judges exactly the configuration it will run with (sshd(8) `-t`, `-f`), and a fake `Execute` observes the new bytes in place when `sshd -t` is recorded; when it exits 0 `Apply` MUST execute `systemctl reload sshd.service`, which makes `changed` true. When `sshd -t` or that reload cannot be run or exits nonzero, `Apply` MUST, before returning the error, put the drop-in back to its previous bytes and mode, or remove it when it was absent, and after a failed `sshd -t` execute no reload, so once `Apply` returns a configuration that failed is not installed, a drop-in that failed to reload is not left looking done, and the next run checks again. When the drop-in already holds those bytes, `Apply` MUST execute no `sshd` and no reload of sshd, setting only its mode when it is not `0600`.
- R-3BI2-MB5Y: `Apply` MUST execute `rpm -q fail2ban-server` and then `rpm -q dnf-automatic`, treating exit status 0 as installed and exit status 1 as missing, and, when either is missing, execute exactly one `dnf install -y` followed by the missing names in that order (so `dnf install -y fail2ban-server dnf-automatic` when both are missing, `dnf install -y dnf-automatic` when only it is), which makes `changed` true on success; when both are installed it MUST execute no `dnf` command. It MUST execute these before writing the jail (R-3CPZ-02WN) or `/etc/dnf/automatic.conf`, and a failed `dnf install -y` MUST stop `Apply` before either is written.
- R-3CPZ-02WN: `Apply` MUST make `/etc/fail2ban/jail.d/ikigenba.local` hold exactly `"[sshd]\nenabled = true\nbackend = systemd\nbanaction = nftables-multiport\nmaxretry = 5\nfindtime = 10m\nbantime = 10m\nbantime.increment = true\nbantime.maxtime = 1w\n"`, `\n` denoting one LF byte, with mode `0644`, creating `/etc/fail2ban/jail.d/` with mode `0755` when absent, and MUST NOT rewrite it when its bytes are already right, setting only its mode when that differs.
- R-SD1E-M02J: After writing the jail, `Apply` MUST execute `systemctl is-enabled fail2ban.service` and `systemctl is-active fail2ban.service`, each exit status 0 meaning enabled or active and any other exit status meaning not; when either is not, it MUST execute `systemctl enable --now fail2ban.service`, which makes `changed` true; when the jail file was created or its bytes changed and the service was active at the query, it MUST then execute `systemctl reload fail2ban.service`; and it MUST execute no other command for fail2ban, so on a host where the service is enabled, active, and the jail unchanged only the two queries run. When that `enable --now` or `reload` cannot be run or exits nonzero and the jail file was created or its bytes changed in this call, `Apply` MUST, before returning the error, put the jail back to its previous bytes and mode, or remove it when it was absent, so the next run writes it and reloads again.
- R-3DXV-DUNC: `Apply` MUST make `/etc/dnf/vars/releasever` hold exactly `"latest\n"`, `\n` denoting one LF byte, with mode `0644`, creating `/etc/dnf/vars/` with mode `0755` when absent, and MUST NOT rewrite it when its bytes are already right, setting only its mode when that differs; it MUST execute no command for this part.
- R-R4WO-APD5: `Apply` MUST set two keys of the section `[commands]` of `/etc/dnf/automatic.conf` in place: in that section (from the line that is `[commands]` once surrounding whitespace is removed up to the next line that, so trimmed, begins with `[` and ends with `]`), every line that, leading whitespace removed, does not begin with `#` or `;` and whose text before its first `=`, with surrounding whitespace removed, is `upgrade_type` MUST become `upgrade_type = security`, and every such line whose key is `apply_updates` MUST become `apply_updates = yes`; a key with no such line MUST be inserted directly after the `[commands]` line, `upgrade_type = security` before `apply_updates = yes`; a file with no `[commands]` section MUST gain, after one LF when its last byte is not LF, `[commands]\nupgrade_type = security\napply_updates = yes\n`, and an absent file MUST be created holding exactly those bytes with mode `0644`. Every other line MUST stay byte-identical and the file's mode unchanged, so the packaged file changes only in its `upgrade_type = default` and `apply_updates = no` lines; a file whose bytes would not change MUST NOT be rewritten.
- R-SE9A-ZRT8: After `/etc/dnf/automatic.conf`, `Apply` MUST execute `systemctl is-enabled dnf-automatic.timer` and `systemctl is-active dnf-automatic.timer`, with exit statuses read as in R-SD1E-M02J, and when either is not 0 execute `systemctl enable --now dnf-automatic.timer`, which makes `changed` true, and no other command for dnf-automatic. When that `enable --now` cannot be run or exits nonzero and `/etc/dnf/automatic.conf` was created or rewritten in this call, `Apply` MUST, before returning the error, put it back to its previous bytes, or remove it when it was absent.
- R-R7CH-28UJ: *The sudo rule* MUST be `/etc/sudoers.d/ikigenba-accounts` holding exactly `"ikigenba ALL=(root) NOPASSWD: /usr/local/bin/opsctl group *, /usr/local/bin/opsctl user *\n"`, `\n` denoting one LF byte, with mode `0440`.
- R-98VK-1BB5: When `/etc/sudoers.d/ikigenba-accounts` under `env.Root` is absent or its bytes differ from R-R7CH-28UJ's, `Apply` MUST write those bytes with mode `0440` to `/etc/sudoers.d/ikigenba-accounts.new`, execute `visudo -c -f <P>`, `<P>` that staged file resolved under `env.Root`, and only when it exits 0 rename the staged file over the rule, which makes `changed` true; when `visudo` cannot be run or exits nonzero the rule MUST be left byte for byte as it was, or absent when it was absent. Whatever the outcome, and also when the rule already holds those bytes, `/etc/sudoers.d/ikigenba-accounts.new` MUST be absent when `Apply` returns from a call that reached this part, so one left by an interrupted run is removed. When the rule already holds those bytes `Apply` MUST execute no `visudo`, setting only its mode when it is not `0440`.
- R-R9S9-TSBX: The *state directories* MUST be `/etc/ikigenba`, `/etc/opt/ikigenba`, `/var/opt/ikigenba`, `/var/lib/ikigenba`, and `/run/ikigenba`, and their *table* MUST give owner `root:root` and permission bits `0700` to `/etc/ikigenba`, to `/etc/opt/ikigenba`, and to each directory directly in `/etc/opt/ikigenba`; owner `root:ikigenba` and permission bits `0750` to `/var/opt/ikigenba`, `/var/lib/ikigenba`, and `/run/ikigenba`; and to `/run/ikigenba` the named ACL entry `user:nginx:--x`.
- R-YEOK-DJ3V: For each state directory that is, under `env.Root` and judged without following a link, a directory, `Apply` MUST bring it to its table entry; to read owners it MUST execute exactly one `stat --format=%U:%G` naming every existing table path in table order, the entries directly in `/etc/opt/ikigenba` in ascending bytewise name order after `/etc/opt/ikigenba` itself, each such entry being a table path only when it is a directory judged without following a link (stat(1), which does not follow a link), reading its output as one `<user>:<group>` line per path, and none when no table path exists. For each path whose line differs from its table owner it MUST execute `chown <user>:<group> <path>` without `--recursive`; for `/run/ikigenba` it MUST execute `getfacl --absolute-names --omit-header <path>` and, unless one output line's text before any whitespace is exactly `user:nginx:--x`, execute `setfacl -m u:nginx:x <path>` (setfacl(1), which recalculates the mask so the entry's `x` takes effect); then it MUST set each table path's permission bits to its table value directly under `env.Root`, and for every entry beneath a state directory that is not a symbolic link and whose permission bits grant others any permission, clear exactly its others' bits, changing no other bit. It MUST execute the `chown` of a path and, for `/run/ikigenba`, the `setfacl`, before that path's permission bits change, so a fake `Execute` observes the previous mode when they are recorded; it MUST NOT follow a symbolic link or change its target, MUST NOT create a state directory that is absent, and MUST NOT change the owner, group, or ACL of anything but the table paths. Each `chown`, `setfacl`, or mode change makes `changed` true, and a host whose table paths hold their owners, modes, and ACL and on which nothing beneath grants others anything MUST see only the `stat` and the `getfacl` for this part, with no mode changed.
- R-RC82-LBTB: Every file `Apply` writes MUST be written whole to a temporary file in its destination directory and renamed over the destination, so a reader sees the previous file or the complete new one; no temporary file MUST remain when `Apply` returns, a temporary name in `/etc/sudoers.d/` MUST contain a `.`, and one in any other directory MUST NOT end in `.conf` or `.local`.
- R-3HLK-J5VF: `Apply` MUST create, modify, or remove nothing under `env.Root` other than: the directories `/etc/systemd/system/user.slice.d/`, `/etc/systemd/system/user-.slice.d/`, `/etc/ssh/`, `KeysDir`, `/etc/fail2ban/jail.d/`, and `/etc/dnf/vars/` when absent and the mode of `KeysDir`; the two pool drop-ins; the sshd drop-in; `/etc/fail2ban/jail.d/ikigenba.local`; `/etc/dnf/vars/releasever`; `/etc/dnf/automatic.conf`; the sudo rule; `/etc/sudoers.d/ikigenba-accounts.new` and the temporary files of R-RC82-LBTB, each gone when it returns; and, under the state directories, the permission bits R-YEOK-DJ3V sets or clears and the owners and ACL of the table paths, and nothing else, no file's bytes, no entry created or removed. It MUST execute through `env.Execute` no command other than `id --user ikigenba` and the commands of `apps.EnsureAccount`, `systemctl daemon-reload`, `getent group shell`, `groupadd --system shell`, `sshd -t -f`, `systemctl reload sshd.service`, `rpm -q`, `dnf install -y`, `systemctl is-enabled`, `systemctl is-active`, `systemctl enable --now` and `systemctl reload` of `fail2ban.service`, `systemctl is-enabled`, `systemctl is-active` and `systemctl enable --now` of `dnf-automatic.timer`, `visudo -c -f`, `stat --format=%U:%G`, `chown`, `getfacl`, and `setfacl`, as the requirements of this design for each part state them; so it changes no symbolic link, release label, unit file, environment file's bytes, nginx file, services file's bytes, Litestream configuration, or data directory's contents.
- R-3ITG-WXM4: `changed` MUST be true exactly when `Apply` created the account `ikigenba` or the group `shell`, installed a package, created or rewrote a file or created a directory that R-3HLK-J5VF lets it keep (never a staged or temporary one) and did not put back, changed a mode, executed a `chown` or `setfacl`, or executed a `systemctl enable --now`, and false otherwise; on a host that holds every rule `Apply` MUST return false and nil, having written nothing and executed only `id --user ikigenba`, the commands of `apps.EnsureAccount` for a present account, `getent group shell`, the two `rpm -q`, the four `systemctl is-enabled`/`is-active` queries, the `stat`, and the `getfacl` when `/run/ikigenba` exists, so neither sshd, fail2ban, nor systemd is reloaded and `dnf` is not run.
- R-RFVR-QN1E: When a command `Apply` executes cannot be run, or exits with a status the part does not read as an answer (any status but 0 and 1 for `id --user` and `rpm -q`, but 0 and 2 for `getent`, but 0 for every other command except the `systemctl is-enabled` and `is-active` queries, whose every completed status is an answer), `Apply` MUST stop at once, execute nothing further, undo nothing that earlier parts did, and return `changed` as it stood and an error that `errors.As` resolves to that command's `*host.CommandError` (D01), carrying its captured `Result`, and whose `Error()` is exactly that `CommandError`'s `Error()`, no prefix added; its `Label` MUST be `id` for `id`, `getent group` for `getent`, `groupadd` for `groupadd`, `sshd -t` for `sshd`, `rpm -q` for `rpm`, `dnf install` for `dnf`, `systemctl <verb>` for `systemctl <verb> ...`, `visudo -c` for `visudo`, and the program's name for `stat`, `chown`, `getfacl`, and `setfacl`, so a refused check reads `sshd -t: exit status 255` or `visudo -c: exit status 1` and a failed install `dnf install: exit status 1`.
- R-RH3O-4ES3: A filesystem failure (a read, write, rename, removal, directory creation, or mode change under `env.Root`, or `stat` output that does not hold one `<user>:<group>` line per named path) MUST make `Apply` stop at once, execute nothing further, undo nothing earlier parts did, and return a non-nil error identifying the host path written as a host path rather than resolved under `env.Root`; the part that failed MUST install nothing, leaving its destination file as it was and removing its staged and temporary files.
- R-SLKP-AE9E: `Apply` MUST read no configuration store, no release, no manifest, and no host fact other than the files and command results this design's requirements for each part name, so its outcome is the same on every host holding the same such state, whatever its name, memory, or store.
