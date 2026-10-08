# D15-golden-and-seed

A space starts empty; `golden capture` and `seed` fill one from data kept for
the purpose. Both move snapshots, the one artifact that carries an app's data
whole. Snapshots are opsctl's: `opsctl snapshot` on a host writes one per app
under the space's own `snapshots/` prefix and reports each object's URI, and
`opsctl restore <app> --from <uri>` puts one back. devctl reaches both only
through opsctl's published grammar, over the same `host.Host` shape every
host command uses (D06), and never describes what a snapshot holds or what a
restore does on the host.

A **golden set** is a named directory of snapshots, one per app, at
`golden/<set>/<app>/<file>` in the bucket named after the root, outside every
space's prefix; `golden.Prefix` names it. Its name is one label by the rule
`spaceref.ValidLabel` applies, and no space is labelled `golden` (D04
`spaceref.ReservedLabel`), so the two namespaces never meet. Every copy is an
S3 `CopyObject` within that bucket (D03), made with the developer's own
credentials, which reach the whole bucket. The host's role is not widened: it
still reaches only its own space's prefix, and a host restores only from its
own `seed/` prefix (D06 `space.SeedPrefix`).

`golden capture <space> <set>` checks the set's name before anything else,
then reads the root file, parses `<space>`, opens one session, finds the
space, and refuses one that is not running. It asks `opsctl status` first,
which prints nothing on a host with no apps; such a host is refused before
anything is snapshotted, since a set of nothing would only empty the set.
Otherwise it has opsctl snapshot every app in one run, so the apps' snapshots
share one timestamp. Each
snapshot is then copied into the set under its own file name, in app name
order, and the `prune` step deletes every object under the set's prefix the
capture did not write, so a capture that succeeds leaves the set whole from
its one run. A capture that fails part-way prunes nothing; the stories accept
the mixed set that leaves until a capture succeeds. devctl learns which
objects the run wrote from opsctl's report, one line per app naming the URI
it wrote, and refuses a report it cannot read rather than guess.

`seed <space> <source>` reads the root file, parses `<space>` and then
`<source>`, opens one session, and finds and checks the target before it looks
the source up. A bare `<source>` names the golden set of that name when one
holds a snapshot and otherwise the space of that label; a full domain always
names the space, so the bare name `golden` can only be a set and the space
prefix `golden/snapshots/` is never read as a space's. A space source is read
from the bucket alone: it need not be running, or exist, and seed takes no
snapshot of its own. For each app the source holds, the newest snapshot is
the one whose file name sorts last, which for opsctl's
`<timestamp>.tar.zst` names is the latest run. Every snapshot is copied into
the target's `seed/` prefix before any app is touched, then each app is
restored from its copy in name order, and once every restore has succeeded
the `clean` step deletes everything under `seed/`. A failed copy stops before
any ssh connection; a failed restore stops the seed and leaves the copies in
place; running seed again starts over. devctl checks no secret: a seeded
app's `etc/env` is opsctl's to write from the target's own parameters, and a
missing one is opsctl's failure, relayed.

Shared rules are referenced, not restated: usage errors first, then the root
file, then the operand, then the first cloud call (D04 R-N1LD-IX1I and
R-ST4K-APZN, which also brings the `golden` label refusal of R-S8UF-C8XM);
`cloud.Connect` with the root as the profile and the root file's region (D02
R-YPS2-X32R); a `*cloud.NoSpaceError` to `devctl: no space at '<domain>'`,
exit 1 (D03 R-YTFS-2EAU); a `*cloud.Error` to one line and exit 1 (D03
R-QZSL-ZMJL); the checkout and root-file errors to exit 2 (D04 R-EX3T-CTOO);
every error carrying `ExitCode()` and `Detail()` (D05 R-D4G2-IO81); the
fallback that turns a `*space.NotRunningError` into
`devctl: '<domain>' is stopped`, exit 1 (D05 R-0D99-FN33); step lines through
`space.Step` (D06 R-SR5Y-JN2E); and the host runner, `host.Host`,
`(host.Host).Sudo`, and `*host.CommandError`, whose diagnostic quotes opsctl's
standard output and then its standard error (D06 R-T3CY-DCHC, R-D6VV-A7PF,
R-JA8W-5LZ5, R-D9BO-1R6T). Neither command uses the clock.

## REQUIREMENTS

- R-SM9B-JQ39: Package `internal/golden` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-SOP4-B9KN: Package `internal/golden` MUST export `UsageError` with exactly `Message string` and `Help string`, methods `Error() string` returning `Message`, `ExitCode() int` returning 2, and `Detail() string` returning `see '<Help>' for usage`.

- R-SPX0-P1BC: Package `internal/golden` MUST export a `NoAppsError` struct whose only field is `Domain string`, with the methods `Error() string`, returning `'<Domain>' has no apps to capture`, and `ExitCode() int`, returning 1, verified at least by reproducing `'sbx1.ikigenba.dev' has no apps to capture`.

- R-SR4X-2T21: Package `internal/golden` MUST export a `ReportError` struct whose only field is `Line string`, with the methods `Error() string`, returning `snapshot: unexpected line from opsctl snapshot: '<Line>'`, and `ExitCode() int`, returning 1.

- R-SSCT-GKSQ: Package `internal/golden` MUST export `Prefix(set string) string`, returning `golden/`, `set`, and `/`, verified at least by `Prefix("demo")` being `golden/demo/`.

- R-STKP-UCJF: `cli.Run` MUST dispatch the command `golden` to `golden.Run`, passing the arguments that follow `golden`, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `golden.Run` returns a nil error.

- R-SUSM-84A4: `devctl golden --help` and `devctl golden -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0, whenever `--help` or `-h` appears anywhere among the arguments that follow `golden`, `devctl golden capture --help` and `devctl golden capture sbx1 demo -h` included; subject to the superuser refusal, help MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`, verified at least with `Deps.Dir` set to a directory that is not inside a git checkout:

  ```
  Usage: devctl golden <subcommand> [arguments]

  Keep a space's data as a named golden set, one snapshot per app under
  golden/<set>/ in the backup bucket, for 'devctl seed' to give to any space.
  A golden set carries no secrets.

  Subcommands:
    capture <space> <set>   snapshot every app on <space> and make the snapshots golden set <set>

  Run 'devctl golden <subcommand> --help' for details.
  ```

- R-SW0I-LW0T: The `golden` subcommands MUST be exactly `capture`, which MUST take exactly two operands, `<space>` then `<set>`, and accept no option other than `--help` and `-h`. With no argument `golden` MUST return a `*UsageError` whose `Message` is `golden needs <subcommand>`; with a first argument that is not `capture` and does not begin with `-`, one whose `Message` is `unknown subcommand '<name>'`; for `capture` with fewer than two operands, one whose `Message` is `golden capture needs <space> and <set>`; with more than two, one whose `Message` is `golden capture takes only <space> and <set>`; and for any argument that begins with `-` and is neither `--help` nor `-h`, one whose `Message` is `unknown option '<option>'`; each with `Help` equal to `devctl golden --help`. These refusals MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`; verified at least by `devctl golden capture sbx1` writing exactly the three lines `devctl: golden capture needs <space> and <set>`, an empty line, and `see 'devctl golden --help' for usage` to stderr with empty stdout and exit 2 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-SX8E-ZNRI: Once its operands are accepted and before it calls `checkout.ReadRootFile`, `golden capture` MUST return an `*spaceref.InvalidLabelError` whose `Operand` is `<set>` when `spaceref.ValidLabel` of `<set>` is false, calling `deps.Cloud` not at all and passing no `seam.Cmd` to `deps.Exec`; verified at least by `devctl golden capture sbx1 Demo_1` writing the single stderr line `devctl: 'Demo_1' is not a valid label` with empty stdout and exit 2 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-SYGB-DFI7: After its set name is accepted, `golden capture` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the space by `spaceref.Parse` as R-ST4K-APZN requires, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the space's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the space's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout, call no method of `Clients.S3`, and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec`; verified at least, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl golden capture gone demo` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` and by `devctl golden capture sbx2 demo` writing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` for a `stopped` instance, each with empty stdout and exit 1.

- R-U2OZ-ZUZ4: After the `opsctl status` check of R-TRPW-JXAV has exited 0 with standard output holding a byte other than whitespace, the `snapshot` step of `golden capture` MUST be exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with the step `snapshot` and exactly the arguments `opsctl` and `snapshot`; when it fails `golden capture` MUST return its error unchanged, write nothing to stdout, and call no method of `Clients.S3`; verified at least through `cli.Run`, for a space at `18.118.7.42`, by a fake `ssh` process that, for `sudo opsctl snapshot`, exits 1 with arbitrary multi-line standard output and arbitrary standard error giving the stderr first line `devctl: snapshot: ssh ec2-user@18.118.7.42 sudo opsctl snapshot: exit status 1`, an empty line, and each line of that standard output and then of that standard error once with one `> ` prefix added, with empty stdout and exit 1.

- R-T0W4-4YZL: When the `snapshot` step exits 0, `golden capture` MUST read each line of its `Output.Stdout` that is not empty as one app's report, which MUST be `<app>`, `: ok (`, `<uri>`, `, `, one or more further bytes, and `)`, where `<app>` is a name `appref.ValidName` accepts that no earlier line named and `<uri>` is `s3://`, `root.Domain`, `/`, `space.SnapshotPrefix` of the space's `Label`, `<app>`, `/`, and a non-empty `<file>` holding no `/`, `,`, or space; for the first line that is not such a report it MUST return a `*ReportError` whose `Line` is that line, write no `snapshot` step line, and call no method of `Clients.S3`; verified at least by the line `crm: failed: no replica under the prefix`, by a `<uri>` under another space's prefix, and by a second line for `crm`, each giving a `*ReportError` for exactly that line.

- R-TRPW-JXAV: After the running check and before the `snapshot` step, `golden capture` MUST make exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with an empty step and exactly the arguments `opsctl` and `status`, and MUST return that call's error unchanged when it fails; when it exits 0 and its `Output.Stdout` holds no byte other than whitespace, `golden capture` MUST return a `*NoAppsError` whose `Domain` is the space's `Domain`, write nothing to stdout, run no `snapshot` step, and call no method of `Clients.S3`; when it holds any other byte capture MUST go on to the `snapshot` step without otherwise reading it; verified at least through `cli.Run` by `devctl golden capture sbx1 demo` with a fake `ssh` process that exits 0 with empty standard output for `sudo opsctl status` writing the single stderr line `devctl: 'sbx1.ikigenba.dev' has no apps to capture` with empty stdout, exit 1, and no recorded remote argument vector beginning `sudo`, `opsctl`, `snapshot`.

- R-T3BW-WIGZ: When every line of the `snapshot` step's report is read, `golden capture` MUST write the step `snapshot` with the detail `opsctl snapshot, ` followed by the decimal count of reports and ` apps`, verified at least by reproducing `snapshot: ok (opsctl snapshot, 2 apps)`.

- R-U6CP-5677: After the `snapshot` step, `golden capture` MUST, for each reported app in ascending order of `<app>`, call `Clients.S3.CopyObject` exactly once with `root.Domain` as the bucket, the `<uri>` without its leading `s3://`, `root.Domain`, and `/` as the source, and `Prefix(<set>)`, `<app>`, `/`, and `<file>` as the key, and after each call that returns a nil error write the step `copy` with the detail `<app> -> `, `root.Domain`, `/`, and that key; when a call fails it MUST return that error unchanged and make no further S3 call; verified at least through `cli.Run`, for reports naming `s3://ikigenba.dev/sbx1/snapshots/crm/2026-09-14T09:10:02Z.tar.zst` and `s3://ikigenba.dev/sbx1/snapshots/dashboard/2026-09-14T09:10:02Z.tar.zst`, by reproducing `copy: ok (crm -> ikigenba.dev/golden/demo/crm/2026-09-14T09:10:02Z.tar.zst)` and `copy: ok (dashboard -> ikigenba.dev/golden/demo/dashboard/2026-09-14T09:10:02Z.tar.zst)` with a recording fake `S3` receiving exactly those sources and keys, in that order, also when the report names `dashboard` before `crm`, by a report of three apps whose first `CopyObject` fails giving exactly one `CopyObject` call and only the `snapshot` line on stdout, and by a fake whose second `CopyObject` fails with a `*cloud.Error` whose `Service` is `s3`, `Operation` is `CopyObject`, and `Code` is `SlowDown` leaving exactly `snapshot: ok (opsctl snapshot, 2 apps)` and the `crm` copy line on stdout, the single stderr line `devctl: s3 CopyObject: SlowDown`, exit 1, and no `ListObjects` or `DeleteObjects` call.

- R-T6ZM-1TP2: After every copy, the `prune` step of `golden capture` MUST call `Clients.S3.ListObjects` with `root.Domain` and `Prefix(<set>)`, MUST then call `Clients.S3.DeleteObjects` exactly once with `root.Domain` and every key it returned that is not one of the keys the copies wrote when there is at least one such key and not at all when there is none, and MUST write the step `prune` with the detail `<n> objects deleted`, where `<n>` is the decimal count of those keys, or `nothing to delete` when there are none; when either call fails it MUST return that error unchanged and write no `prune` line; verified at least by a fake `S3` that lists `golden/demo/crm/2026-09-12T14:22:51Z.tar.zst`, `golden/demo/dashboard/2026-09-12T14:22:51Z.tar.zst`, and `golden/demo/gmail/2026-09-12T14:22:51Z.tar.zst` beside the two new copies reproducing `prune: ok (3 objects deleted)` with `DeleteObjects` receiving exactly those three keys, and by one that lists only the new copies reproducing `prune: ok (nothing to delete)` with no `DeleteObjects` call.

- R-U1H3-M38F: `golden capture` MUST obtain nothing from the checkout but the root file; MUST pass to `deps.Exec` no `seam.Cmd` beyond those `checkout.ReadRootFile` passes, the one `(host.Host).Sudo` call of the `opsctl status` check, and the one of the `snapshot` step; MUST call no method of `Clients.SSM`, `Clients.Route53`, or `Clients.IAM`, no method of `Clients.EC2` other than those `cloud.LookupSpace` makes, and no `Clients.S3.PutObject`; and MUST write or delete no key that does not begin with `Prefix(<set>)`; verified with recording fake clients after a successful `devctl golden capture sbx1 demo` and after one whose prune fails.

- R-T9FE-TD6G: Package `internal/seed` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error`, and `Run` MUST take no writer other than `stdout`.

- R-TANB-74X5: Package `internal/seed` MUST export `UsageError` with exactly `Message string` and `Help string`, methods `Error() string` returning `Message`, `ExitCode() int` returning 2, and `Detail() string` returning `see '<Help>' for usage`.

- R-TBV7-KWNU: Package `internal/seed` MUST export a `NotASourceError` struct whose only field is `Operand string`, with the methods `Error() string`, returning `'<Operand>' is not a golden set or a space`, and `ExitCode() int`, returning 2, and a `NoSourceError` struct whose only field is `Operand string`, with the methods `Error() string`, returning `no golden set or space snapshots for '<Operand>'`, and `ExitCode() int`, returning 1; verified at least by reproducing `'crm.sbx1' is not a golden set or a space` and `no golden set or space snapshots for 'gone'`.

- R-TD33-YOEJ: `cli.Run` MUST dispatch the command `seed` to `seed.Run`, passing the arguments that follow `seed`, the `stdout` writer `cli.Run` was given, and `deps`, and MUST return 0 when `seed.Run` returns a nil error.

- R-TEB0-CG58: `devctl seed --help` and `devctl seed -h` MUST write exactly the following text with a final newline to stdout, with empty stderr and exit 0, whenever `--help` or `-h` appears anywhere among the arguments that follow `seed`; subject to the superuser refusal, help MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`, verified at least by `devctl seed --help` and `devctl seed sbx2 demo -h` with `Deps.Dir` set to a directory that is not inside a git checkout:

  ```
  Usage: devctl seed <space> <source>

  Give <space> the data of <source>, a golden set or another space. Each app's
  newest snapshot in <source> is copied under <space>'s own seed/ prefix and
  put back there with 'opsctl restore <app> --from <uri>', which replaces the
  app's etc/, state/ and database, and writes its etc/env from <space>'s own
  secrets. An app on <space> that <source> holds no snapshot of is left alone.

  <source> is a golden set when one has that name, and otherwise the space with
  that label. A space's full domain always names the space.
  ```

- R-TFIW-Q7VX: `seed` MUST take exactly two operands, `<space>` then `<source>`, in that order, and MUST accept no option other than `--help` and `-h`; fewer than two operands MUST return a `*UsageError` whose `Message` is `seed needs <space> and <source>`, more than two operands one whose `Message` is `seed takes only <space> and <source>`, and an argument that begins with `-` and is neither `--help` nor `-h` one whose `Message` is `unknown option '<option>'`, each with `Help` equal to `devctl seed --help`; these refusals MUST call `deps.Cloud` not at all and pass no `seam.Cmd` to `deps.Exec`; verified at least by `devctl seed sbx2` writing exactly the three lines `devctl: seed needs <space> and <source>`, an empty line, and `see 'devctl seed --help' for usage` to stderr with empty stdout and exit 2 with `Deps.Dir` set to a directory that is not inside a git checkout.

- R-TZ1A-UJR1: After its operands are accepted, `seed` MUST call `checkout.ReadRootFile(ctx, deps)` and return its error unchanged, MUST then obtain the target space from `<space>` by `spaceref.Parse` as R-ST4K-APZN requires, MUST then parse `<source>` as R-THYP-HRDB states, MUST then call `cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)` with the `Domain` and `Region` of the `RootFile` it read and return its error unchanged, MUST then call `cloud.LookupSpace` with the `EC2` of the session's `Clients`, `root.Domain`, and the target's `Domain` and return its error unchanged, and MUST return a `*space.NotRunningError` whose `Domain` is the target's `Domain` and whose `State` is the found `cloud.Space`'s `State` when that state is not `cloud.StateRunning`; in each failing case it MUST write nothing to stdout, call no method of `Clients.S3`, and pass no `seam.Cmd` whose `Path` is `ssh` to `deps.Exec`; verified at least, in a temporary checkout whose root file holds `{"domain": "ikigenba.dev", "region": "us-east-2"}`, by `devctl seed gone demo` writing the single stderr line `devctl: no space at 'gone.ikigenba.dev'` and by `devctl seed sbx2 demo` writing the single stderr line `devctl: 'sbx2.ikigenba.dev' is stopped` for a `stopped` instance, each with empty stdout and exit 1; and by `devctl seed crm.sbx1 Demo` writing the single stderr line `devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'` with exit 2.

- R-THYP-HRDB: `seed` MUST parse `<source>` against `root.Domain` before any call to `deps.Cloud`: when it ends in `.` followed by `root.Domain` and the remainder after one removal of that suffix is a label `spaceref.ValidLabel` accepts, it names the space of that label, unless that label is `spaceref.ReservedLabel`, for which `seed` MUST return a `*spaceref.ReservedLabelError` whose `Label` is that label; when it does not end so and is itself such a label, it is a bare name; otherwise `seed` MUST return a `*NotASourceError` whose `Operand` is `<source>` as typed; each refusal MUST call `deps.Cloud` not at all; verified at least through `cli.Run`, in a temporary checkout whose root file names `ikigenba.dev`, by `devctl seed sbx2 crm.sbx1`, `devctl seed sbx2 Demo`, `devctl seed sbx2 ikigenba.dev`, and `devctl seed sbx2 crm.sbx1.ikigenba.dev` each writing the single stderr line `devctl: '<source>' is not a golden set or a space` with empty stdout and exit 2, by `devctl seed sbx2 golden.ikigenba.dev` writing `devctl: 'golden' is not a usable space label: golden/ holds the golden sets` with exit 2, and by a recording fake `Deps.Cloud` left with no call in each case.

- R-U7KL-IXXW: In this design a key is a **snapshot key** of a prefix when it is that prefix, an `<app>` that `appref.ValidName` accepts, `/`, and a non-empty `<file>` holding no `/`; and the **newest** snapshot of an app among such keys is the one whose `<file>` is greatest in byte order. After the target check, `seed` MUST find its source in the bucket `root.Domain`: for a bare name `<n>` it MUST call `Clients.S3.ListObjects` with `golden.Prefix(<n>)` and, when that returns at least one snapshot key of that prefix, take the golden set `<n>` as the source; otherwise, and when `<n>` is not `spaceref.ReservedLabel`, it MUST call `ListObjects` with `space.SnapshotPrefix(<n>)` and take the space `<n>.<root.Domain>` as the source; for a space named by its full domain it MUST call `ListObjects` with `space.SnapshotPrefix` of its label only, never with a `golden.Prefix`; the source's snapshots MUST be the newest snapshot of each app among the snapshot keys of the prefix it was found under, every other key ignored. When no listing yields a snapshot key `seed` MUST return a `*NoSourceError` whose `Operand` is `<source>` as typed, write nothing to stdout, call no `CopyObject`, and pass no `ssh` `seam.Cmd` to `deps.Exec`; verified at least by `devctl seed sbx2 gone` with nothing under `golden/gone/` or `gone/snapshots/` writing the single stderr line `devctl: no golden set or space snapshots for 'gone'` with empty stdout and exit 1, and by `devctl seed sbx2 golden` with nothing under `golden/golden/` giving `devctl: no golden set or space snapshots for 'golden'` with no `ListObjects` call for `golden/snapshots/`; by `devctl seed sbx2 sbx1` with `golden/sbx1/` holding only `golden/sbx1/notes.txt` falling back to `sbx1/snapshots/`; and by `sbx1/snapshots/` also holding `sbx1/snapshots/crm/x/y` and `sbx1/snapshots/Bad_1/2026-09-20T00:00:00Z.tar.zst`, neither of which is copied or counted; and by `golden/sbx1/` holding only `golden/sbx1/crm/`, an empty `<file>`, also falling back to `sbx1/snapshots/`.

- R-TKEI-9AUP: Once its source is found, `seed` MUST write the step `source` with the detail `golden set <n>, ` for a golden set or `space <domain>, ` for a space, where `<domain>` is that space's full domain, followed by the decimal count of the source's apps and ` apps`; verified at least by reproducing `source: ok (golden set demo, 2 apps)` for `devctl seed sbx2 demo`, `source: ok (space sbx1.ikigenba.dev, 2 apps)` for both `devctl seed sbx2 sbx1` and `devctl seed sbx2 sbx1.ikigenba.dev` with nothing under `golden/sbx1/`, and, with both `golden/demo/` and `demo/snapshots/` holding snapshot keys, `source: ok (golden set demo, 2 apps)` for `devctl seed sbx2 demo` with no `ListObjects` call for `demo/snapshots/` and `source: ok (space demo.ikigenba.dev, 2 apps)` for `devctl seed sbx2 demo.ikigenba.dev` with no `ListObjects` call for `golden/demo/`.

- R-U8SH-WPOL: After the `source` step, `seed` MUST, for each of the source's apps in ascending order of `<app>`, call `Clients.S3.CopyObject` exactly once with `root.Domain` as the bucket, that app's newest snapshot key as the source, and `space.SeedPrefix` of the target's `Label`, `<app>`, `/`, and that key's `<file>` as the key, and after each call that returns a nil error write the step `copy` with the detail `<app> -> `, `root.Domain`, `/`, and that key; when a call fails it MUST return that error unchanged, make no further S3 call, and pass no `ssh` `seam.Cmd` to `deps.Exec`; verified at least by `devctl seed sbx2 sbx1` with `sbx1/snapshots/crm/` holding `2026-09-12T14:22:51Z.tar.zst`, `2026-09-13T00:00:00Z.tar.zst`, and `2026-09-14T09:10:02Z.tar.zst` and `sbx1/snapshots/dashboard/` holding `2026-09-12T14:22:51Z.tar.zst` reproducing `copy: ok (crm -> ikigenba.dev/sbx2/seed/crm/2026-09-14T09:10:02Z.tar.zst)` and `copy: ok (dashboard -> ikigenba.dev/sbx2/seed/dashboard/2026-09-12T14:22:51Z.tar.zst)` with a recording fake `S3` receiving exactly the sources `sbx1/snapshots/crm/2026-09-14T09:10:02Z.tar.zst` and `sbx1/snapshots/dashboard/2026-09-12T14:22:51Z.tar.zst` in that order from a fake `ListObjects` that lists `dashboard`'s key first and then `crm`'s keys in the order `2026-09-12T14:22:51Z`, `2026-09-14T09:10:02Z`, `2026-09-13T00:00:00Z`, so that neither listing order, first-listed, nor last-listed selection gives that result, by a source of three apps whose first `CopyObject` fails giving exactly one `CopyObject` call and only the `source` line on stdout, and by a fake whose second `CopyObject` fails with a `*cloud.Error` whose `Service` is `s3`, `Operation` is `CopyObject`, and `Code` is `SlowDown` leaving exactly the `source` line and the `crm` copy line on stdout, the single stderr line `devctl: s3 CopyObject: SlowDown`, and exit 1.

- R-TMUB-0UC3: After every copy, `seed` MUST, for each of the source's apps in ascending order of `<app>`, make exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the target's found `cloud.Space`'s `Address` and whose `Deps` is `deps`, with the step `restore` and exactly the arguments `opsctl`, `restore`, `<app>`, `--from`, and `s3://`, `root.Domain`, `/`, and that app's copy key joined as one argument, and after each call that exits 0 MUST discard its output and write the step `restore` with the detail `opsctl restore <app> --from ` followed by that same URI; when a call fails `seed` MUST return its error unchanged, make no further host call, and call no `DeleteObjects`; verified at least through `cli.Run`, for a target at `18.224.31.9` and a source holding `crm`, `dashboard`, and `gmail`, by reproducing `restore: ok (opsctl restore crm --from s3://ikigenba.dev/sbx2/seed/crm/2026-09-12T14:22:51Z.tar.zst)` and the like line for `dashboard`, then for a fake `ssh` process that exits 1 on `gmail` with arbitrary standard output and standard error, the stderr first line `devctl: restore: ssh ec2-user@18.224.31.9 sudo opsctl restore gmail --from s3://ikigenba.dev/sbx2/seed/gmail/2026-09-12T14:22:51Z.tar.zst: exit status 1`, an empty line, and each line of that standard output and then of that standard error once with one `> ` prefix added, with exit 1 and the three copies left in the fake `S3`.

- R-U3WW-DMPT: After every restore has exited 0, the `clean` step of `seed` MUST call `Clients.S3.ListObjects` with `root.Domain` and `space.SeedPrefix` of the target's `Label`, MUST then call `Clients.S3.DeleteObjects` exactly once with `root.Domain` and every key it returned when it returned at least one and not at all when it returned none, and MUST write the step `clean` with the detail `<n> objects deleted`, where `<n>` is the decimal count of those keys, or `nothing to delete` when there are none; when either call fails it MUST return that error unchanged and write no `clean` line, leaving every `restore` line written; verified at least by reproducing `clean: ok (2 objects deleted)` as the last line of a successful `devctl seed sbx2 demo` with the fake `S3` left holding nothing under `sbx2/seed/`, by `clean: ok (3 objects deleted)` when `sbx2/seed/` also held `sbx2/seed/gmail/2026-09-01T00:00:00Z.tar.zst` left by an earlier seed, that key among those `DeleteObjects` receives and the fake left holding nothing under `sbx2/seed/`, and by a `DeleteObjects` that fails leaving both `restore` lines on stdout and exit 1.

- R-TQI0-65K6: `seed` MUST obtain nothing from the checkout but the root file; MUST pass to `deps.Exec` no `seam.Cmd` beyond those `checkout.ReadRootFile` passes and the `(host.Host).Sudo` calls of the `restore` step, so that it asks no host which apps it runs and opens no ssh connection to a source space's host; MUST call no method of `Clients.SSM`, `Clients.Route53`, or `Clients.IAM`, no method of `Clients.EC2` other than those the one `cloud.LookupSpace` for the target makes, and no `Clients.S3.PutObject`; and MUST write or delete no key that does not begin with `space.SeedPrefix` of the target's `Label`; verified with recording fake clients after a successful `devctl seed sbx2 sbx1` that leaves every key under `sbx1/` and `golden/` as it was, and after a successful `devctl seed sbx2 sbx2` whose copies come from `sbx2/snapshots/`.
