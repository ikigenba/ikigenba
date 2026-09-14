# D6-space

`space` is the command that lists spaces, says what one is running, stops one,
starts one, and destroys one. `internal/space` owns those five subcommands, the
`space` grammar and usage text, and the space-shaped verbs — instance waits,
Elastic IP, records, role — that `space create` reuses. `internal/host` owns the
other half of this design: a space's host as seen from this machine, reached
with the system `ssh` and `scp`. D1's layout names both packages; this is where
they are declared.

`space create` is D7's: its own design, its own weight, its own package. It is
still `space`'s subcommand, so the usage text below lists it and `cli.Run`
routes it — the one command whose subcommands live in two packages.

This design adds nothing of D1–D5's. `seam.Deps`, `cli.Run` and `seam.Cmd` are
D1's; the grammar, the diagnostic shape, the four exit codes and the one
missing-`--account` rule are D2's; the account, the space lookup, the zone, the
`no space at '<domain>'` diagnostic and the whole AWS surface are D3's; the
checkout is D4's; `secrets.Prefix` and the command-package shape — `Run` with
stdout only, errors that classify themselves through `ExitCode()` and `Detail()`,
reusable verbs as functions that return data and print nothing — are D5's.

Three of this story's failures are already requirements elsewhere and are not
restated here: `devctl space list` without `--account` is D2's one
missing-`--account` rule, the account with no properties is D3's rendering of a
`*cloud.Error`, and the one diagnostic that covers `stop`, `start` and `status`
of a space that does not exist is D3's rule for a `*NoSpaceError`. What this
design adds for the last of those is the requirement that the three subcommands
really do ask `(*Account).Space` first, and that `destroy` — alone among them —
treats its answer as "already gone" instead of as a failure.

**Step output is one shape.** `create`, `destroy`, `stop`, `start`, `deploy`
and `restore` all print one line per completed step and stop at the first
failure, which goes to stderr as an ordinary diagnostic. The line is the
step's name, then `: ok`, then the step's detail in parentheses — or just
`<step>: ok` when the step has no detail to report, which is `create`'s `init:
ok`. There is no `fail` line and no summary: a step that fails prints nothing
and the command returns an error, which `cli.Run` renders. So a multi-step
command needs exactly one function, and this package declares it:
`space.Step(w, name, detail)`. It cannot live in `internal/cli` — D1's import
rule makes `internal/cli` a package nothing else may import — and it must not
be copied into six packages, so it lives with the first design that needs it.
`internal/spacecreate`, `internal/deploy` and `internal/restore` import
`internal/space` for it; a test greps the tree to keep a seventh copy from
appearing.

**The host, and the two ways it fails.** `internal/host` is one struct — a
public address and a `seam.Deps` — and four methods over `Deps.Exec`, all
declared in the requirements: a remote command, the same command under `sudo`,
a file copy, and a wait for the host to answer, over the one `ec2-user` target
they all connect as.

It connects to the **address**, never to the domain, on purpose: `stop` deletes
the records of a space with no Elastic IP, `start` has only just rewritten
them, and `create` writes them for a host that does not answer yet, so the name
is the least reliable way to reach the machine devctl is holding the instance id
of.

Every command line carries the same three options before the target:
`-o BatchMode=yes`, `-o ConnectTimeout=10`, and
`-o StrictHostKeyChecking=accept-new`.

`BatchMode=yes` because devctl is run by agents as often as by people and a
password prompt in a pipe is a hang; `ConnectTimeout=10` because the unreachable
host must be a bounded failure and not a TCP timeout; `accept-new` because
`space create` ssh's to a host nobody has ever seen and `start` to one on a new
address, while a *changed* key still stops the command — which is the point of
keeping the check on at all. Everything else — the user's key, a proxy, a
jump host — is the developer's own `~/.ssh/config`, which is why devctl shells
out to `ssh` rather than speaking the protocol.

The three options are not part of the diagnostic. Two stories fix its bytes,
and they show the command a human would retype: a remote command that failed
names its step and the bare `ssh <target> <command>` that failed, with the exit
status, and carries opsctl's own stderr as a second block; a host that never
answered is one line naming only `ssh <target>` and the reason.

So there are two error types, and they are the reason `internal/host` exists
before `deploy` does. A remote command that ran and failed is a
`*CommandError`: the step it belongs to, the command as a reader needs to see
it, the exit status, and what the remote command said on both streams — which
it surfaces through D5's mechanism, `ExitCode() int` returning 1 and
`Detail() string` returning the remote output, so D2's two-block shape is
produced by `cli.Run` and not by six commands. The detail is the remote stderr
when there is one; when the remote command said nothing there — as a command
whose product is a report of what it found does, putting the whole report on
stdout and failing by its exit status alone — the detail is its stdout, so the
report reaches the developer instead of being dropped. Neither stream is
parsed; devctl relays the bytes and reads only the exit status. An empty
`Step` drops the `<step>: ` prefix, which is what `space status` wants, having
no steps. A host that could not be reached at all is an
`*UnreachableError`, one line, no detail, no step prefix: `Wait` probes with
`ssh … true` every `ProbeInterval` up to `ProbeAttempts` times and then gives
up. It says `connection timed out` because that is what a budget running out
means to the developer; ssh's own reason is not parsed, and no story asks for
it.

**`space list`** is D3's `Spaces` and nothing else: the domain, the state, the
address or `-`, in the order `Spaces` sorted them. It opens no checkout and runs
no process.

**`space status` asks the host, and relays what it says.** The story is clear
about where the answer comes from — "over ssh, `opsctl` lists the installed
apps, asks each app's binary its version, and reads each app's systemd unit
state" — and about the shape of the line: `crm v0.1.0 active`. All three facts
are host facts that devctl cannot compute, check, or correct, and the sort is
over a set only the host knows. So devctl runs one command — `ssh` to the
host, then `sudo opsctl status` — and copies its standard output to stdout
byte for byte. `sudo`, because opsctl refuses to run as anything but root.
Relaying rather than parsing is the whole of devctl's side: there is nothing
to validate, a parse would only re-render bytes devctl cannot check, and an
app that has never been deployed is the empty output the "no apps" story asks
for, with no special case. The ordering and the line format are opsctl's
contract, and opsctl has no such command today: `opsctl/specs/design/` covers
`config`, `dns` and `init`. `specs/issues/opsctl-status-command.md` records
it.

A stopped space is refused before any ssh: `devctl: 'bar.sbx.ikigenba.dev' is
stopped`, exit 1. The text is the story's, and the state in it is the instance's
own, so a space caught mid-transition reports `is stopping` rather than a lie.

**`stop` and `start` turn on one question: does the space have an Elastic IP?**
The address of a space without one is released with the instance, so the records
must go with it and be written again on the way back; the address of a space
with one survives, so the records are left alone. That is the whole difference,
and it is visible in the step details:

| step | with an Elastic IP | without one |
|---|---|---|
| `stop` / `records` | `kept, elastic ip` | `2 deleted` |
| `start` / `records` | `unchanged, 18.220.10.5` | `foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.145.72.19, INSYNC` |

Both subcommands are idempotent in the direction they push: `stop` of a stopped
space prints `instance: ok (already stopped)` and `records: ok (already gone)`,
and `start` of a running one skips `StartInstance` but still re-points the
records and still runs the renewal check — which is exactly what the
unreachable-host story's postcondition promises ("Running start again on the
running instance re-points the records and runs the check").

`start`'s last two steps are the host's. `host: ok (status checks passed)` is
D3's `InstanceChecksPassed` polled to true. `certificate: ok (certbot renew:
…)` is `sudo certbot renew` — never forced, as the story insists; certbot
decides whether anything is due. Its two details are the story's bytes, so
devctl has to tell the two outcomes apart, and certbot's exit status is 0 for
both. The signal is certbot's own renewal report, which marks each certificate
it acted on `(success)` and each one it skipped `(skipped)`: output holding
`(success)` is `renewed`, output without it is `not yet due`. That is a fact
about another program's output and wants a live observation before check-spec.

**`destroy` is six steps, idempotent and resumable.** It never reports `no
space at '<domain>'`: a space whose instance is gone is a space whose remaining
litter is still worth collecting, which is why a destroy that fails part-way can
simply be run again. Each step reports `already gone` when there was nothing to
do — with two wrinkles the stories fix:

- The `address` step has three details, not two. `no elastic ip` is what a
  space *that existed* reports when it had none; `already gone` is what the same
  step reports when the instance was gone too. The same asymmetry decides
  `records`, `secrets` and `backups`: a destroy that terminated an instance
  reports its zero as a zero (`backups: ok (0 objects deleted)`), and a destroy
  that found no instance reports it as `already gone`.
- `secrets` and `backups` honour the account: with `delete_secrets_on_destroy`
  or `delete_backups_on_destroy` false the step makes no call at all and reports
  `kept, delete_secrets_on_destroy=false`, naming the property so the developer
  knows where the decision was made.

The wildcard record is the one deletion that cannot be written from the domain.
Route 53 returns `*.foo.sbx.ikigenba.dev` as `\052.foo.sbx.ikigenba.dev`, and
D3's `ListRecords` hands that spelling through undecoded on purpose, so
`DeleteRecords` lists the zone, matches a record whose name is the domain or
either spelling of the wildcard, and sends back the `cloud.Record` it was given
rather than one it built. A zone that is gone is the same answer as
a record that is gone: `records: ok (already gone)`.

**The waits are above the cloud boundary**, as D3 requires: every one of them is
a poll on `Deps.After`, bounded by `PollAttempts`, and a budget that runs out is
a `*WaitError` — `devctl: timed out waiting for i-0c9e94542d98846a8 to be
running`. Four things are waited for, and three of them are D7's as much as
mine: an instance state (`stopped`, `running`, `terminated`), the status checks,
a Route 53 change reaching `INSYNC`, and a host answering ssh. A test injects an
`After` that fires immediately, so the whole surface runs in microseconds with
no clock.

**The usage texts.** `devctl space --help` and each subcommand's are declared
byte for byte in the requirements below, from the story.

**The grammar, and its refusals.** `space <subcommand> [<domain>]`, six
subcommands, no options of their own but `--help` and `-h`. The refusals take
the form the stories fix for `space create` and `space destroy` — a message, a
blank line, and a pointer at `devctl space --help`, never at the subcommand's
own help.

Six messages: `space needs <subcommand>`, `unknown subcommand 'stpo'`, `space
stop needs <domain>`, `space stop takes only <domain>`, `space list takes no
arguments`, and `unknown option '--force'`. All six are exit 2, and all six are
one `*UsageError` carrying the message and a `Help` of `devctl space --help`, so
the second line is written once, by `cli.Run`, through D5's `Detail()`.

**What is deliberately not here.** Everything `space create` does with these
verbs — the role it creates and the policy it carries, the launch, the Elastic IP it
allocates, the opsctl it installs, its usage text and its nine steps — is D7's.
`deploy` and `restore` are D9's, over `host.Host` and `space.Step`. The backup
bucket's per-app prefix is D9's, over `BackupPrefix`.

## REQUIREMENTS

- R-SPY2-5VBP: Package `internal/space` MUST export `Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error`, and `Run` MUST take no writer other than `stdout`.
- R-SR5Y-JN2E: Package `internal/space` MUST export `Step(w io.Writer, name, detail string)`, which MUST write `<name>: ok (<detail>)` followed by one newline to `w` when `detail` is not empty and `<name>: ok` followed by one newline when `detail` is empty, verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 terminated)` and `init: ok`.
- R-SSDU-XET3: Package `internal/space` MUST export `RoleName(domain string) string`, returning `ikigenba-space-` followed by `domain`; `PolicyName = "space"`; `BackupPrefix(domain string) string`, returning `domain` followed by `/`; `RecordNames(domain string) []string`, returning exactly `domain` and `*.` followed by `domain`, in that order; and `RecordTTL = 60`; verified at least by `RoleName("foo.sbx.ikigenba.dev")` being `ikigenba-space-foo.sbx.ikigenba.dev` and `BackupPrefix("foo.sbx.ikigenba.dev")` being `foo.sbx.ikigenba.dev/`.
- R-STLR-B6JS: Package `internal/space` MUST export `WaitState(ctx context.Context, deps seam.Deps, acct *account.Account, id string, state cloud.InstanceState) (cloud.Instance, error)`, `WaitChecks(ctx context.Context, deps seam.Deps, acct *account.Account, id string) error`, `PollInterval = 5 * time.Second`, and `PollAttempts = 60`.
- R-SUTN-OYAH: Package `internal/space` MUST export `ElasticIP(ctx context.Context, acct *account.Account, domain string) (cloud.Address, bool, error)` and `ReleaseElasticIP(ctx context.Context, acct *account.Account, addr cloud.Address) error`.
- R-SW1K-2Q16: Package `internal/space` MUST export `PutRecords(ctx context.Context, deps seam.Deps, acct *account.Account, zone cloud.Zone, domain, address string) error` and `DeleteRecords(ctx context.Context, acct *account.Account, zone cloud.Zone, domain string) (int, error)`.
- R-SX9G-GHRV: Package `internal/space` MUST export `DeleteRole(ctx context.Context, acct *account.Account, domain string) (bool, error)`.
- R-SZP9-8199: Package `internal/space` MUST export a `UsageError` struct whose fields are exactly `Message string` and `Help string`, with the methods `Error() string`, returning `Message`, `Detail() string`, returning `see '<Help>' for usage`, and `ExitCode() int`, returning 2.
- R-T0X5-LSZY: Package `internal/space` MUST export a `NotRunningError` struct whose fields are exactly `Domain string` and `State cloud.InstanceState`, with the method `Error() string`, returning `'<Domain>' is <State>`, verified at least by reproducing `'bar.sbx.ikigenba.dev' is stopped`.
- R-T251-ZKQN: Package `internal/space` MUST export a `WaitError` struct whose fields are exactly `Subject string` and `Want string`, with the method `Error() string`, returning `timed out waiting for <Subject> to <Want>`, verified at least by reproducing `timed out waiting for i-0c9e94542d98846a8 to be running`.
- R-T3CY-DCHC: Package `internal/host` MUST export `User = "ec2-user"`, a `Host` struct whose fields are exactly `Address string` and `Deps seam.Deps`, and the method `Target() string` on `Host`, returning `User`, `@`, and `Address` joined, verified at least by the `Target()` of a `Host` whose `Address` is `3.19.79.227` being `ec2-user@3.19.79.227`.
- R-T4KU-R481: Package `internal/host` MUST export an `Output` struct whose fields are exactly `Stdout string` and `Stderr string`, and the methods `Run(ctx context.Context, step string, args ...string) (Output, error)`, `Sudo(ctx context.Context, step string, args ...string) (Output, error)`, `Copy(ctx context.Context, step, local, remote string) error`, and `Wait(ctx context.Context) error` on `Host`.
- R-T5SR-4VYQ: Package `internal/host` MUST export `ProbeInterval = 5 * time.Second` and `ProbeAttempts = 12`.
- R-KTAH-P7JA: Package `internal/host` MUST export a `CommandError` struct whose fields are exactly `Step string`, `Command []string`, `Status int`, `Stdout string`, and `Stderr string`, with the methods `Error() string`, returning the elements of `Command` joined by single spaces followed by `: exit status <Status>`, preceded by `<Step>: ` when `Step` is not empty and by nothing when it is empty; `Detail() string`, returning `Stderr` with its trailing newlines removed when `Stderr` holds any character other than whitespace and otherwise `Stdout` with its trailing newlines removed; and `ExitCode() int`, returning 1; verified at least by reproducing `install: ssh ec2-user@3.19.79.227 sudo opsctl install /tmp/gmail-v0.1.0.tar.xz: exit status 1`.
- R-T88J-WFG4: Package `internal/host` MUST export an `UnreachableError` struct whose only field is `Address string`, with the method `Error() string`, returning `ssh ` followed by `ec2-user@<Address>` and `: connection timed out`, verified at least by reproducing `ssh ec2-user@3.145.72.19: connection timed out`.
- R-T9GG-A76T: `(host.Host).Run` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `ssh`, whose `Dir` is `Deps.Dir`, and whose `Args` are exactly `-o`, `BatchMode=yes`, `-o`, `ConnectTimeout=10`, `-o`, `StrictHostKeyChecking=accept-new`, `Target()`, and then every element of `args`, each as one argument with no quoting of its own; and `(host.Host).Sudo` MUST pass the `seam.Cmd` that `Run` passes for `sudo` followed by the elements of `args`.
- R-TAOC-NYXI: `(host.Host).Copy` MUST pass exactly one `seam.Cmd` to `Deps.Exec`, whose `Path` is `scp`, whose `Dir` is `Deps.Dir`, and whose `Args` are exactly `-o`, `BatchMode=yes`, `-o`, `ConnectTimeout=10`, `-o`, `StrictHostKeyChecking=accept-new`, `local`, and `Target()` followed by `:` and `remote`.
- R-KUIE-2Z9Z: `(host.Host).Run`, `(host.Host).Sudo`, and `(host.Host).Copy` MUST return an `Output` holding the process's standard output and standard error as strings, unmodified, and a nil error when it exits 0; MUST return a `*CommandError` whose `Step` is `step`, whose `Command` is the `seam.Cmd`'s `Path` followed by its `Args` with the six option arguments removed, whose `Status` is that process's exit status, and whose `Stdout` and `Stderr` are that process's standard output and standard error unmodified, when it exits non-zero; and MUST return an error that `errors.As` does not match to a `*CommandError` and whose message contains the `seam.Cmd`'s `Path` when `Deps.Exec` returns a non-nil error.
- R-TD45-FIEW: `(host.Host).Wait` MUST pass to `Deps.Exec` the `seam.Cmd` that `Run` passes for the single argument `true`, MUST return a nil error as soon as one of those processes exits 0, MUST wait on `Deps.After(ProbeInterval)` between consecutive ones, MUST pass at most `ProbeAttempts` of them, and MUST return an `*UnreachableError` whose `Address` is `Address` when that many have not exited 0; verified with a fake `Deps.After` that fires immediately.
- R-TEC1-TA5L: `space.WaitState` MUST call `acct.Clients.EC2.DescribeInstance` with `id` and return that `cloud.Instance` and a nil error as soon as its `State` equals `state` and — when `state` is `cloud.StateRunning` — its `Address` is not empty, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `be ` followed by `state` when that many calls have not satisfied it; verified with a fake `Deps.After` that fires immediately.
- R-TFJY-71WA: `space.WaitChecks` MUST call `acct.Clients.EC2.InstanceChecksPassed` with `id` and return a nil error as soon as it reports true, MUST wait on `deps.After(PollInterval)` between consecutive calls, MUST make at most `PollAttempts` calls, and MUST return a `*WaitError` whose `Subject` is `id` and whose `Want` is `pass its status checks` when that many calls have not reported true.
- R-THZQ-YLDO: `space.ElasticIP` MUST call `acct.Clients.EC2.ListSpaceAddresses` and return, with a true second result, the one `cloud.Address` whose `Space` equals `domain`; MUST return a false second result and a nil error when none does; and MUST return a non-nil error naming `domain` when two or more do.
- R-TJ7N-CD4D: `space.ReleaseElasticIP` MUST call `acct.Clients.EC2.DisassociateAddress` with `addr.AssociationID` exactly once when that field is not empty and not at all when it is, and MUST then call `acct.Clients.EC2.ReleaseAddress` with `addr.AllocationID` exactly once.
- R-TKFJ-Q4V2: `space.PutRecords` MUST call `acct.Clients.Route53.ChangeRecords` for `zone.ID` exactly once, with one `cloud.RecordChange` per element of `RecordNames(domain)` in that order, each whose `Action` is `cloud.ChangeUpsert` and whose `Record` has that element as `Name`, `A` as `Type`, `RecordTTL` as `TTL`, and exactly `address` as `Values`; MUST then call `acct.Clients.Route53.ChangeStatus` with the change id that call returned until it reports `cloud.ChangeInsync`, waiting on `deps.After(PollInterval)` between consecutive calls and making at most `PollAttempts` of them; and MUST return a `*WaitError` whose `Subject` is that change id and whose `Want` is `reach INSYNC` when that many calls have not reported it.
- R-TLNG-3WLR: `space.DeleteRecords` MUST call `acct.Clients.Route53.ListRecords` for `zone.ID` and MUST pass to `acct.Clients.Route53.ChangeRecords` for `zone.ID` one `cloud.RecordChange` whose `Action` is `cloud.ChangeDelete` for each returned `cloud.Record` whose `Type` is `A` and whose `Name` is `domain`, `*.` followed by `domain`, or `\052.` followed by `domain`, each carrying that `cloud.Record` exactly as `ListRecords` returned it, MUST return the number of those changes, and MUST return 0 and call `ChangeRecords` not at all when there are none; verified at least with a `ListRecords` that returns the wildcard record as `\052.foo.sbx.ikigenba.dev` and a `ChangeRecords` that records the `cloud.Record` it was given.
- R-TMVC-HOCG: `space.DeleteRole` MUST call `acct.Clients.IAM.RoleExists` and `acct.Clients.IAM.InstanceProfileRoles` for `RoleName(domain)`, MUST return false and a nil error and call no other IAM operation when neither reports existence, and otherwise MUST call `DeleteRolePolicy` for that role and `PolicyName`, `RemoveRoleFromInstanceProfile` for that instance profile and each role it holds, `DeleteInstanceProfile`, and `DeleteRole`, in that order, and return true.
- R-TO38-VG35: When a command's `Run` returns a non-nil error that `errors.As` matches to a `*account.NoZoneError`, `cli.Run` MUST write `devctl: ` followed by that error's message as the only line of stderr, write nothing further to stdout, and return 1.
- R-TPB5-97TU: Every line that `internal/space`, `internal/spacecreate`, `internal/deploy`, and `internal/restore` write to stdout to report a completed step MUST be written by `space.Step`, verified by a test over those packages' source that no non-test file of a package other than `internal/space` contains the string `: ok (`; and a command MUST write no step line for a step that did not complete.
- R-TDM1-14W2: `devctl space --help` and `devctl space -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space <subcommand> [arguments]

  List, create, destroy, stop, start, and inspect spaces in one account. A space
  is one instance named by its full domain; the cloud's tags are the only
  registry.

  Subcommands:
    list                            one line per space in the account
    create <domain> [--elastic-ip]  create the space at <domain>
    destroy <domain>                remove the space and everything it owned
    stop <domain>                   stop the instance; state is kept
    start <domain>                  start the instance and point its records at it
    status <domain>                 one line per app on the space: version and service state

  Every subcommand needs --account. Run 'devctl space <subcommand> --help' for details.
  ```
- R-TG1T-SODG: `devctl --account <name> space list --help` and `devctl --account <name> space list -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space list

  Print one line per space in the account: the domain, the instance state, and the
  public address, or - when it has none. The lines are sorted by domain, and an
  account with no spaces prints nothing.

  The cloud's tags are the only registry: a space is an instance tagged
  Project=ikigenba with a Space tag naming its domain. What a space is running is
  'devctl space status'.
  ```
- R-TH9Q-6G45: `devctl --account <name> space destroy --help` and `devctl --account <name> space destroy -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space destroy <domain>

  Remove everything the space owned: its instance, its Elastic IP, its A records,
  its secrets, its backups, and its role. Each line of output is one step, and a
  step with nothing left to do reports 'already gone', so a destroy that failed
  part-way is resumed by running it again.

  The account's delete_secrets_on_destroy and delete_backups_on_destroy decide
  whether the secrets and the backups go with the space; where either is false,
  that step reports what it kept.

  Arguments:
    <domain>   the space to destroy
  ```
- R-TIHM-K7UU: `devctl --account <name> space stop --help` and `devctl --account <name> space stop -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space stop <domain>

  Stop the space's instance. Its volume, its tags, its role, its secrets, and its
  backups are kept, so 'devctl space start' brings the space back as it was.

  A space with an Elastic IP keeps its A records, because its address survives the
  stop. Without one the A records <domain> and *.<domain> are deleted, because the
  address is released with the instance and the name must not point at a stranger.

  Arguments:
    <domain>   the space to stop
  ```
- R-TJPI-XZLJ: `devctl --account <name> space start --help` and `devctl --account <name> space start -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space start <domain>

  Start the space's instance, point its records at it, and have certbot renew the
  certificate if it fell due while the space was stopped. Each line of output is
  one step; the last line is the domain and the address.

  A space with an Elastic IP comes back on the same address and its A records are
  left alone. Without one it comes back on a new address and the A records
  <domain> and *.<domain> are written to it.

  Arguments:
    <domain>   the space to start
  ```
- R-TKXF-BRC8: `devctl --account <name> space status --help` and `devctl --account <name> space status -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl --account <name> space status <domain>

  Ask the space's host what it is running: one line per installed app, in name
  order, holding the app, the version its binary reports, and the state of its
  systemd unit. The answer comes from opsctl over ssh and from nowhere else, so
  the space's instance must be running and a space with no apps prints nothing.

  Arguments:
    <domain>   the space to ask
  ```
- R-TSYU-EJ1X: `cli.Run` MUST dispatch the command `space` to `space.Run`, passing the arguments that follow `space`, the `stdout` writer `cli.Run` was given, `deps`, and the profile name `--account` carried, except when the first of those arguments is `create`, which it MUST dispatch to `spacecreate.Run`, passing the arguments that follow `create` and those same three further arguments, and MUST return 0 when the `Run` it called returns a nil error.
- R-TU6Q-SASM: The subcommand set of `space` MUST be exactly `list`, `create`, `destroy`, `stop`, `start`, and `status`; `list` MUST take no operand; `destroy`, `stop`, `start`, and `status` MUST each take exactly one `<domain>` operand; and none of `list`, `destroy`, `stop`, `start`, and `status` MUST accept any option other than `--help` and `-h`.
- R-TVEN-62JB: `devctl --account <name> space` MUST write exactly the three lines `devctl: space needs <subcommand>`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.
- R-TWMJ-JUA0: `space` invoked with a first argument that is none of its subcommands, `--help`, and `-h` and does not begin with `-` MUST write exactly the three lines `devctl: unknown subcommand '<name>'`, an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2.
- R-TXUF-XM0P: `space destroy`, `space stop`, `space start`, and `space status` invoked with no operand MUST each write exactly the three lines `devctl: space <subcommand> needs <domain>` — where `<subcommand>` is the invoked subcommand's own name — an empty line, and `see 'devctl space --help' for usage` to stderr, write nothing to stdout, and exit 2, verified at least by reproducing `devctl: space destroy needs <domain>`.
- R-U0A8-P5I3: `space destroy`, `space stop`, `space start`, and `space status` invoked with more than one operand MUST each write `devctl: space <subcommand> takes only <domain>`, and `space list` invoked with any operand MUST write `devctl: space list takes no arguments`, as the first of exactly three lines followed by an empty line and `see 'devctl space --help' for usage` on stderr, write nothing to stdout, and exit 2.
- R-U1I5-2X8S: An argument of `space` or of one of its subcommands other than `create` that begins with `-` and is neither `--help` nor `-h` MUST cause exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl space --help' for usage` to be written to stderr, nothing to stdout, and exit 2.
- R-U2Q1-GOZH: `space.Run` MUST NOT call `checkout.Open`, and `space list`, `space stop`, and `space destroy` MUST pass no `seam.Cmd` to `deps.Exec`.
- R-U3XX-UGQ6: `space list` MUST write one line to stdout for each `account.Space` that `(*Account).Spaces` returns, in the order it returns them, consisting of that `Space`'s `Domain`, one space, its `State`, one space, and either its `Address` or `-` when `Address` is empty; MUST write nothing to stdout when `Spaces` returns none; and MUST exit 0; verified at least by reproducing the three lines `bar.sbx.ikigenba.dev stopped -`, `foo.sbx.ikigenba.dev running 3.19.79.227`, and `new.sbx.ikigenba.dev running 18.220.10.5`, and by reproducing empty output.
- R-U55U-88GV: `space status`, `space stop`, and `space start` MUST each call `(*Account).Space(ctx, domain)` and return its error unchanged, before any call to `deps.Exec`, any write to stdout, and any call to a method of `Account.Clients` other than those `Space` itself makes; and `space destroy` MUST treat a `*account.NoSpaceError` from that call as the space having no instance and MUST NOT return it.
- R-U6DQ-M07K: `space status` MUST return a `*NotRunningError` whose `Domain` is the `<domain>` operand and whose `State` is the space's `State`, write nothing to stdout, and pass no `seam.Cmd` to `deps.Exec`, when that state is not `cloud.StateRunning`, verified at least by reproducing the single stderr line `devctl: 'bar.sbx.ikigenba.dev' is stopped` with exit 1.
- R-S9WX-LSSA: `space status` MUST obtain its answer from exactly one call to `(host.Host).Sudo` on a `host.Host` whose `Address` is the space's `Address` and whose `Deps` is `deps`, with an empty `step` and exactly the arguments `opsctl` and `status`, and MUST write that call's `Output.Stdout` to stdout byte for byte and nothing else, neither parsing, reordering, reformatting, nor validating it; verified at least with a fake `Deps.Exec` that exits 0 with arbitrary multi-line standard output, with one whose standard output has no trailing newline, and with one whose standard output is empty, each reproduced on stdout unchanged.
- R-U8TJ-DJOY: `space stop` MUST write the step `instance` first, whose detail MUST be `already stopped`, with `StopInstance` not called, when the space's `State` is `cloud.StateStopped`, and otherwise MUST call `acct.Clients.EC2.StopInstance` with the space's `ID` exactly once, then `WaitState` for `cloud.StateStopped`, and report the space's `ID` followed by ` stopped`; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 stopped)` and `instance: ok (already stopped)`.
- R-UA1F-RBFN: `space stop` MUST write the step `records` second and last, whose detail MUST be `kept, elastic ip`, with no Route 53 operation called, when `ElasticIP` reports an Elastic IP for the domain; MUST otherwise be `<n> deleted`, where `<n>` is the count `DeleteRecords` returned for the zone `(*Account).Zone` named; and MUST be `already gone` when that count is 0 or when `Zone` returned a `*account.NoZoneError`; verified at least by reproducing the two-line outputs `instance: ok (i-0c9e94542d98846a8 stopped)` with `records: ok (2 deleted)`, `instance: ok (i-0a1b2c3d4e5f60718 stopped)` with `records: ok (kept, elastic ip)`, and `instance: ok (already stopped)` with `records: ok (already gone)`.
- R-UB9C-536C: `space start` MUST write the step `instance` first, MUST call `acct.Clients.EC2.StartInstance` with the space's `ID` exactly once and then `WaitState` for `cloud.StateRunning` when the space's `State` is not `cloud.StateRunning` and MUST call `StartInstance` not at all when it is, and MUST report the space's `ID`, ` running, `, and the running instance's `Address`; verified at least by reproducing `instance: ok (i-0c9e94542d98846a8 running, 3.145.72.19)`.
- R-UCH8-IUX1: `space start` MUST write the step `records` second, whose detail MUST be `unchanged, ` followed by the Elastic IP's `IP`, with no Route 53 operation called, when `ElasticIP` reports an Elastic IP for the domain, and MUST otherwise call `PutRecords` with the zone `(*Account).Zone` named and the running instance's `Address` and report `<domain>, *.<domain> -> <that address>, INSYNC`; verified at least by reproducing `records: ok (foo.sbx.ikigenba.dev, *.foo.sbx.ikigenba.dev -> 3.145.72.19, INSYNC)` and `records: ok (unchanged, 18.220.10.5)`.
- R-UDP4-WMNQ: `space start` MUST write the step `host` third, with the detail `status checks passed`, after `WaitChecks` for the space's `ID` returned a nil error.
- R-UEX1-AEEF: `space start` MUST write the step `certificate` fourth, after exactly one call to `(host.Host).Wait` and then exactly one call to `(host.Host).Sudo` with the step `certificate` and exactly the arguments `certbot` and `renew`, both on a `host.Host` whose `Address` is the running instance's `Address` and whose `Deps` is `deps`, and its detail MUST be `certbot renew: renewed` when either field of that call's `Output` contains `(success)` and `certbot renew: not yet due` when neither does; verified at least by reproducing `certificate: ok (certbot renew: not yet due)` and `certificate: ok (certbot renew: renewed)`.
- R-UG4X-O654: `space start` MUST write, only when all four of its steps completed and as the last line of stdout, the `<domain>` operand, one space, and the running instance's `Address`; verified at least by reproducing the five lines of the `space start` story ending in `foo.sbx.ikigenba.dev 3.145.72.19` with exit 0, and by reproducing its first three lines on stdout with the single stderr line `devctl: ssh ec2-user@3.145.72.19: connection timed out`, exit 1, and no further stdout line.
- R-UHCU-1XVT: `space destroy` MUST write the steps `instance`, `address`, `records`, `secrets`, `backups`, and `role`, in that order and no others, MUST exit 0 when every step completed, and MUST attempt no later step once one has failed; verified at least by reproducing the six lines of the destroy story with exit 0, and by reproducing `instance: ok (i-0c9e94542d98846a8 terminated)` and `address: ok (no elastic ip)` on stdout with the single stderr line `devctl: route53 ChangeResourceRecordSets: Throttling` and exit 1.
- R-UIKQ-FPMI: The `instance` step of `space destroy` MUST report `already gone`, with no EC2 operation other than the space lookup called, when `(*Account).Space` returned a `*account.NoSpaceError`, and otherwise MUST call `acct.Clients.EC2.TerminateInstance` with the space's `ID` exactly once, then `WaitState` for `cloud.StateTerminated`, and report the space's `ID` followed by ` terminated`.
- R-UL0J-793W: The `address` step of `space destroy` MUST report `elastic ip `, the `cloud.Address`'s `IP`, and ` released` after calling `ReleaseElasticIP` for it when `ElasticIP` reports an Elastic IP for the domain; MUST report `no elastic ip` when it reports none and the `instance` step terminated an instance; and MUST report `already gone` when it reports none and the `instance` step reported `already gone`; verified at least by reproducing `address: ok (elastic ip 18.220.10.5 released)`, `address: ok (no elastic ip)`, and `address: ok (already gone)`.
- R-UM8F-L0UL: The `records` step of `space destroy` MUST report the count `DeleteRecords` returned for the zone `(*Account).Zone` named followed by ` deleted` when that count is not 0 or the `instance` step terminated an instance, MUST report `already gone` when that count is 0 and the `instance` step reported `already gone`, and MUST report `already gone` with no Route 53 operation other than the zone lookup called when `Zone` returned a `*account.NoZoneError`; verified at least by reproducing `records: ok (2 deleted)` and `records: ok (already gone)`.
- R-UNGB-YSLA: The `secrets` step of `space destroy` MUST report `kept, delete_secrets_on_destroy=false`, with no SSM operation called, when `acct.Properties.DeleteSecretsOnDestroy` is false, and MUST otherwise call `acct.Clients.SSM.ListParameters` with `secrets.Prefix(domain)` and `acct.Clients.SSM.DeleteParameter` exactly once for each `cloud.Parameter` it returned, and report that count followed by ` parameters deleted` when the count is not 0 or the `instance` step terminated an instance and `already gone` otherwise; verified at least by reproducing `secrets: ok (3 parameters deleted)`, `secrets: ok (kept, delete_secrets_on_destroy=false)`, and `secrets: ok (already gone)`.
- R-UOO8-CKBZ: The `backups` step of `space destroy` MUST report `kept, delete_backups_on_destroy=false`, with no S3 operation called, when `acct.Properties.DeleteBackupsOnDestroy` is false, and MUST otherwise call `acct.Clients.S3.ListObjects` with `acct.Properties.BackupBucket` and `BackupPrefix(domain)` and then `acct.Clients.S3.DeleteObjects` with every key it returned, and report the number of those keys followed by ` objects deleted` when that number is not 0 or the `instance` step terminated an instance and `already gone` otherwise; verified at least by reproducing `backups: ok (0 objects deleted)`, `backups: ok (kept, delete_backups_on_destroy=false)`, and `backups: ok (already gone)`.
- R-UPW4-QC2O: The `role` step of `space destroy` MUST report `RoleName(domain)` followed by ` deleted` when `DeleteRole` returned true and `already gone` when it returned false; verified at least by reproducing `role: ok (ikigenba-space-foo.sbx.ikigenba.dev deleted)` and `role: ok (already gone)`.
