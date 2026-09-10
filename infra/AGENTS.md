# infra

The Terraform for the one AWS account this repository manages:
**`295229566359`** (the `ikigenba.dev` account). The account is referred to by
its ID everywhere here — directory names, state key, profile, tags. Nothing about the AWS
organisation, the registration of `ikigenba.dev`, the `mgmt` account, the `int`
account, or any other account or domain is managed or changed from here; those
live elsewhere (the standalone `metaspot` repository) and are out of scope.
Resource names, the `Project = "metaspot"` tag, the bucket names, and the
backend bucket came in with that lineage and are unchanged — renaming any of
them would churn live resources for no gain. The `dev` that survives in resource
names, the key pair, the SSM path, and the host's ssh alias is a host/resource
name, not the account's.

**This module is not spec-governed.** There is no `specs/` here, no requirement
ids, and none of the spec operations apply. It is hand-maintained: changes are
made only on direct instruction. An agent may `init`, `validate`, `fmt`, and
`plan` freely; **it runs `terraform apply` only when directly instructed to**,
and it never imports, moves, or edits state.

Every path below is relative to this directory (`infra/`). Region `us-east-2`.

## Layout

- `295229566359/` — the account root. Owns the authoritative
  `ikigenba.dev` hosted zone (delegated at the registrar from the management
  account, which is out of scope here), one host answering at `ikigenba.dev` and
  `*.ikigenba.dev`, its security group, instance role and profile, Elastic IP,
  and the backup bucket `ikigenba-dev-295229566359`. Backend: S3 bucket
  `metaspot-dev-tfstate-295229566359`, key `295229566359/terraform.tfstate`,
  profile `295229566359`, region `us-east-2`, `use_lockfile = true`. Default
  tags: `Project = "metaspot"`, `Account = "295229566359"`,
  `ManagedBy = "terraform"`. The backend block in
  `providers.tf` is the whole configuration; `terraform init` with no arguments
  is correct.
- `bootstrap/295229566359/` — the state-backend root for the account; calls
  `bootstrap/modules/state-backend/` to create the tfstate bucket above. Same
  default tags plus `Component = "bootstrap"`.
  **Local state**: `terraform.tfstate` sits beside its `.tf` files, gitignored,
  and is present in this worktree. A missing bootstrap state file makes `plan`
  propose creating a bucket that already exists — stop and restore the file
  rather than apply.
- `bootstrap/modules/state-backend/` — the module `bootstrap/295229566359/`
  sources.
- `templates/` — `ikigenba-env.sh.tftpl` (the first-boot user data) and
  `ikigenba-launch` (the platform launcher it installs). The `dev` host was
  created with these; `295229566359/dev.tf` renders `user_data` from them.

`.terraform/` directories and provider caches are not tracked; `init` recreates
them. `.terraform.lock.hcl` files are tracked.

## Credentials

Profile `295229566359` is under `sso-session metaspot` in `~/.aws/config`. Every
Terraform command needs a live session — this is the precondition for the gates:

```
aws sso login --sso-session metaspot
```

## Gates

There is no build run here; these are the operator's checks. Run them after
every change. Each must exit 0, and each `plan` must report `No changes` unless
the change was meant to produce one.

1. `terraform fmt -check -recursive` — from `infra/`.
2. In each of `295229566359/` and `bootstrap/295229566359/`:
   `terraform init -input=false`, then
   `terraform validate`, then
   `terraform plan -input=false -detailed-exitcode` (exit 0 is `No changes`;
   exit 2 is a pending change to read before anyone applies it).

Terraform 1.10+ is required (`required_version`); the AWS provider is pinned
`~> 5.70` and locked by `.terraform.lock.hcl`.

## SSH access

The `dev` host is Amazon Linux 2023, user `ec2-user`. The key pair `ikigenba_dev`
is declared in `295229566359/shared.tf`; the private key is
`~/.ssh/id_ed25519_ikigenba_dev`. The `Host ikigenba.dev dev` entry in
`~/.ssh/config` pins the Elastic IP and selects the key. The `dev_ssh` output
prints the exact command.
