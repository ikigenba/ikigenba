# 2026-05-17 — /etc/metaspot/env node-identity file

## What

Added a node-identity file to the "create a server" standard. Every server's
`user_data` now renders `prod/templates/metaspot-env.sh.tftpl` and writes
`/etc/metaspot/env` — a flat `KEY=value` file (source-able from bash, also
valid as a systemd `EnvironmentFile=`) carrying non-secret identity/topology:
`METASPOT_ENV`, `METASPOT_NODE`, `METASPOT_FQDN`, `METASPOT_ALIAS_FQDN`,
`METASPOT_DNS_ZONE`, `METASPOT_AI_ZONE`, `METASPOT_AWS_ACCOUNT_ID`,
`METASPOT_AWS_REGION`.

`user_data` (with `user_data_replace_on_change = false` and added to
`lifecycle { ignore_changes = [...] }`) was wired into `dnd`, `ralph-scoops`,
and `biz`. `terraform plan` is a clean no-op for all three — the change is
inert in Terraform by design. The file was written to the three running boxes
out-of-band over SSH and verified.

## Why these choices

**A provisioned file beats deriving env at runtime.** The earlier question
"how does a script know prod vs test?" had account-ID-from-IMDS as the
zero-dependency answer, but an explicit on-box file is more useful: it also
carries the FQDNs and zones, is greppable, and any consumer (bash or systemd
unit) reads one fixed path instead of each script re-implementing IMDS +
account mapping. Explicit over implicit.

**Non-secret only, and no public IP.** `user_data` is world-readable via IMDS
and the EC2 API, so secrets stay in the `app-config` parameter. Public IP was
deliberately excluded: referencing `aws_eip.*.public_ip` from
`aws_instance.user_data` creates a Terraform dependency cycle
(instance → eip → instance), and the IP is trivially available from IMDS at
runtime anyway. Volatile facts come from IMDS; only stable identity is baked.

**user_data is the launch baseline, not a live-config channel — same rule as
the AMI pin.** cloud-init runs user_data once per instance at first boot and
never on reboot, so it cannot retrofit a running box, and changing it would
otherwise force destroy+recreate. Setting `user_data_replace_on_change =
false` and adding `user_data` to `ignore_changes` makes a code change
provably unable to replace a live prod box (plan is a no-op). The cost,
accepted: editing the template does not propagate to running boxes — they
must be updated out-of-band, exactly the in-place-patching discipline already
chosen for AMIs. This is why the three existing boxes were written via SSH
rather than "apply + reboot" (a reboot does not re-run user_data).

## Tradeoffs accepted / deferred

- **No drift enforcement.** Nothing verifies a running box's
  `/etc/metaspot/env` still matches its Terraform-intended values; the
  template + this devlog are the only coupling, same as the AMI pin. Fine for
  three boxes; revisit if the fleet grows.
- **Out-of-band writes are manual.** A future rebuild of any box will
  regenerate the file correctly from user_data; only the one-time
  retrofit was manual. No config-management dependency introduced.
