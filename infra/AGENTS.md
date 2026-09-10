# infra

The Terraform for the one AWS account this repository manages: **`dev`**
(`295229566359`, the `ikigenba.dev` account). Nothing about the AWS
organisation, the registration of `ikigenba.dev`, the `mgmt` account, the `int`
account, or any other account or domain is managed or changed from here; those
live elsewhere (the standalone `metaspot` repository) and are out of scope.
Resource names, tags (`Project = "metaspot"`), the bucket name, and the backend
came in with that lineage and are unchanged — renaming any of them would churn
live resources for no gain.

**This module is not spec-governed.** There is no `specs/` here, no requirement
ids, and none of the spec operations apply. It is hand-maintained: changes are
made only on direct instruction, and `terraform apply` is always run by a human.
An agent may `init`, `validate`, `fmt`, and `plan`; **it never applies, imports,
moves, or edits state.**

Every path below is relative to this directory (`infra/`). Region `us-east-2`.

## Layout

- `dev/` — the `dev` account root (`295229566359`). Owns the authoritative
  `ikigenba.dev` hosted zone (delegated at the registrar from the management
  account, which is out of scope here), one host answering at `ikigenba.dev` and
  `*.ikigenba.dev`, its security group, instance role and profile, Elastic IP,
  and the backup bucket `ikigenba-dev-295229566359`. Backend: S3 bucket
  `metaspot-dev-tfstate-295229566359`, key `dev/terraform.tfstate`, profile
  `dev`, region `us-east-2`, `use_lockfile = true`. The backend block in
  `providers.tf` is the whole configuration; `terraform init` with no arguments
  is correct.
- `bootstrap/dev/` — the state-backend root for the `dev` account; calls
  `bootstrap/modules/state-backend/` to create the tfstate bucket above.
  **Local state**: `terraform.tfstate` sits beside its `.tf` files, gitignored,
  and is present in this worktree. A missing bootstrap state file makes `plan`
  propose creating a bucket that already exists — stop and restore the file
  rather than apply.
- `bootstrap/modules/state-backend/` — the module `bootstrap/dev/` sources.
- `templates/` — `ikigenba-env.sh.tftpl` (the first-boot user data) and
  `ikigenba-launch` (the platform launcher it installs). The `dev` host was
  created with these; `dev/dev.tf` renders `user_data` from them.

`.terraform/` directories and provider caches are not tracked; `init` recreates
them. `.terraform.lock.hcl` files are tracked.

## Credentials

Profile `dev` is under `sso-session metaspot` in `~/.aws/config`. Every
Terraform command needs a live session — this is the precondition for the gates:

```
aws sso login --sso-session metaspot
```

## Gates

There is no build run here; these are the operator's checks. Run them after
every change. Each must exit 0, and each `plan` must report `No changes` unless
the change was meant to produce one.

1. `terraform fmt -check -recursive` — from `infra/`.
2. In each of `dev/` and `bootstrap/dev/`: `terraform init -input=false`, then
   `terraform validate`, then
   `terraform plan -input=false -detailed-exitcode` (exit 0 is `No changes`;
   exit 2 is a pending change to read before anyone applies it).

Terraform 1.10+ is required (`required_version`); the AWS provider is pinned
`~> 5.70` and locked by `.terraform.lock.hcl`.

## SSH access

The `dev` host is Amazon Linux 2023, user `ec2-user`. The key pair `ikigenba_dev`
is declared in `dev/shared.tf`; the private key is
`~/.ssh/id_ed25519_ikigenba_dev`. The `Host ikigenba.dev dev` entry in
`~/.ssh/config` pins the Elastic IP and selects the key. The `dev_ssh` output
prints the exact command.
