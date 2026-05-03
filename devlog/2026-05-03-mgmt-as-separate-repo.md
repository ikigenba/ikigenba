# 2026-05-03 — mgmt/ stays as a separate git repository

## What

Added `mgmt/` to the top-level `.gitignore`. The `mgmt/` directory present in the working tree is its own independent git repository (it has its own `.git/`, its own history, and predates the metaspot repo's structure). It is intentionally **not** tracked by the metaspot repo.

Concretely:

- `metaspot/.gitignore` now includes `mgmt/`
- The metaspot repo tracks `bootstrap/`, `test/`, `prod/`, top-level `devlog/`, and root files (`CLAUDE.md`, `AGENTS.md`, `README` if added later)
- The mgmt repo is checked out at `metaspot/mgmt/` for convenience but is owned and versioned separately

## Why these choices

**Embedded git repos silently break if added.** Running `git add mgmt/` from inside metaspot doesn't add the mgmt files — it adds a "gitlink" (a single SHA pointing at a commit in the inner repo's history). Cloning metaspot then yields an empty `mgmt/` directory with no way to recover the contents. Treating it as a submodule would fix the gitlink behavior but requires the inner repo to have a remote URL and adds operational overhead for something that is, today, a single-user side workspace.

**The two repos have different audiences.** The metaspot repo manages the AWS Terraform that defines the test/prod environments and (eventually) the mgmt apex zone. The `mgmt/` repo holds older mgmt-account scratch work (existing dns.tf, ec2.tf, www/ static content) that hasn't been brought under the new conventions yet. Keeping them separate avoids forcing a premature decision about how — or whether — to migrate that older content.

**Future option: absorb later.** If/when the older mgmt content is rewritten to fit the metaspot conventions (root module per env, devlog/, S3 backend, etc.), it will be re-imported into metaspot directly (`rm -rf mgmt/.git` then commit) rather than added as a submodule. That decision is deferred.

## Operational notes

- `git check-ignore mgmt/some/path` will confirm a file is ignored by the new rule
- The `mgmt/` repo's own working state is irrelevant to metaspot CI or terraform runs
- Anyone cloning metaspot will not get the `mgmt/` directory at all; they'll need to clone the mgmt repo separately if they need it
