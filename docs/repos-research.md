# repos: prior-art research

Companion research for `repos-design.md`. Question: how do existing
products run autonomous coding agents against git repos, and which of their
conventions should the repos service adopt?

Method and confidence: facts below come from (a) direct reads of official
vendor documentation (July 2026) and (b) a fan-out research pass whose claims
survived 3-vote adversarial verification (marked **verified**). Vendor docs
churn; URLs were live at research time. Products surveyed: GitHub Copilot
coding agent (rebranding to "Copilot cloud agent"), OpenAI Codex cloud,
Google Jules, Devin (Cognition), Cursor cloud agents, Claude Code (GitHub
Action + web), OpenHands.

## 1. Execution environments

Every vendor converged on **ephemeral workspace per task, warm-started from a
cached snapshot**. No product persists session mutations back into base
state.

- **Copilot coding agent** (verified): ephemeral environment powered by
  GitHub Actions; VM booted per task, repo cloned, tests/linters runnable;
  destroyed after the session; hard 59-minute cap. No reuse at all.
  https://docs.github.com/copilot/concepts/agents/coding-agent/about-coding-agent
- **Codex cloud** (verified): OpenAI-managed isolated containers with a
  **two-phase runtime**: setup phase with network (deps install), then agent
  phase offline by default. Container state cached up to 12h; a maintenance
  script runs on resume. https://developers.openai.com/codex/cloud/environments
- **Jules**: fresh Ubuntu VM per task, repo cloned into it, internet on by
  default (least restrictive); a validated per-repo **environment snapshot**
  seeds future tasks. https://jules.google/docs/environment/
- **Devin**: sessions boot fresh copies of a frozen **machine snapshot**
  (repos pre-cloned, deps resolved); "session changes don't persist back to
  the snapshot"; parallel sessions are conflict-free because each is an
  independent VM off the same read-only image.
  https://docs.devin.ai/onboard-devin/environment.md
- **Cursor cloud agents**: isolated Ubuntu VMs on Cursor's AWS;
  `.cursor/environment.json` (+ optional Dockerfile) defines setup; VM
  snapshot reused after successful setup, 90-day expiry.
  https://cursor.com/docs/cloud-agent/setup.md
- **Claude Code GitHub Action** (verified): runs on the user's own Actions
  runners (your minutes, your infra), driven by webhook events in the
  workflow `on:` block. https://code.claude.com/docs/en/github-actions
- **Claude Code on the web**: fresh Anthropic-managed VM per session
  (Ubuntu 24.04), setup-script filesystem snapshot cached ~7 days.
  https://code.claude.com/docs/en/claude-code-on-the-web
- **OpenHands**: Docker sandbox per run; workspace bind-mounted; the
  non-Docker CLI runtime explicitly warns "NO SANDBOX IS USED."
  https://docs.openhands.dev/openhands/usage/sandboxes/docker.md

## 2. Trigger and gating conventions

Execution is universally gated behind an **explicit act** (verified):

- **Issue assignment**: assign the issue to Copilot (👀 reaction
  acknowledges, then the session starts); Devin via Linear/Jira assignment.
- **Labels — direct precedent for our `execute` gate**:
  - Jules: apply the label `jules` to an issue to auto-create a task.
    https://jules.google/docs/running-tasks/
  - OpenHands: `fix-me` label (self-hosted resolver) or `openhands` label
    (cloud app) on an issue triggers a run.
    https://docs.openhands.dev/openhands/usage/run-openhands/github-action.md
  - Claude Code Action: configurable `label_trigger` fires when a named
    label is applied (verified).
- **Mentions**: `@copilot`, `@codex`, `@claude`, `@openhands-agent`,
  `@cursor`, `@Devin` in issue/PR comments for scoped follow-up work.
- **Plan approval**: **Jules is the only product with an explicit plan
  gate** — it presents a plan after setup, the user iterates then approves;
  unattended plans **auto-approve on a timer**; the API defaults to
  auto-approve unless `requirePlanApproval: true`.
  https://jules.google/docs/review-plan/
  Devin gates the launch of parallel managed-session fleets on approval, and
  its Jira mode has a scoping-only option (plan + confidence, no execution).
  Copilot and Codex have no plan gate; their gates sit later (PR review,
  CI approval).
- **Who may trigger**: Copilot and Claude Code Action respond only to users
  with **repository write access** (verified) — the anti-abuse baseline.

## 3. Branch and PR conventions

Strong convergence (verified across Copilot/Claude; consistent elsewhere):

- **Agent-owned branch namespaces**: Copilot pushes only to `copilot/`
  branches (never default); Claude Code Action creates timestamped branches
  under a configurable prefix (default `claude/`).
- **One branch, one PR, per task**: Copilot "can only work on one branch at
  a time and can open exactly one pull request per task," pushing commits
  incrementally to a draft PR.
- **Human-controlled PR creation**: Claude Code Action deliberately does
  NOT open PRs — it pushes commits and links a pre-filled PR page, so
  branch protection and final PR creation stay human. Jules likewise:
  user clicks "Create branch"/"Publish branch"; "you are the branch owner."
- **Merge gates**: on Copilot PRs, Actions CI will not run without approval
  from a write-access user, and the requester cannot be the approving
  reviewer (verified).
- **Traceability**: Copilot commits link to session logs and are signed;
  Cursor signs with an HSM-backed key; Claude web commits carry a
  `Claude-Session: <url>` trailer.

## 4. Credential handling

Convergence on **short-lived, repo-scoped GitHub App installation tokens**
(verified); nobody hands agents long-lived PATs:

- Claude Code Action: default auth is a GitHub App token minted via OIDC,
  "sandboxed to the current repository only"; custom apps mint per-run
  installation tokens (`actions/create-github-app-token`, ~1h). App
  permissions: Contents/Issues/PRs read-write.
- **Claude Code web goes further — a credential proxy**: "sensitive
  credentials such as git credentials or signing keys are never inside the
  sandbox"; the in-sandbox git client holds a scoped credential the proxy
  verifies and translates, and **push is restricted to the session's own
  working branch**. https://code.claude.com/docs/en/claude-code-on-the-web
- Codex cloud: environment **secrets are stripped before the agent phase**
  (setup-only) (verified); env vars persist, secrets don't.
- Copilot: no access to Actions org/repo secrets — only the dedicated
  `copilot` environment's secrets (plus `COPILOT_MCP_*` for MCP servers).
- OpenHands cloud app: short-lived 8-hour tokens, per-repo selectable.
- Devin: central secrets manager exposed as env vars; scopes from org-wide
  down to session-only.

## 5. Loop suppression and failure modes

- **Platform loop-breaking is the durable anti-echo convention** (verified):
  events created with the workflow's `GITHUB_TOKEN` (the github-actions bot)
  cannot trigger further workflows; deliberately chaining bots requires a
  distinct identity (PAT or second App).
- Write-access-only triggering (above) is the anti-spam baseline.
- Prompt injection is the live threat model: Claude Code Action sanitizes
  hidden content (HTML comments, invisible chars, image alt text) and had a
  June 2026 vulnerability (fixed in v1.0.94) where injected issues abused
  the token; Codex docs carry explicit exfiltration warnings and default the
  agent phase offline; Copilot's firewall exists to "manage data
  exfiltration risks" but covers only Bash-tool processes.
- Network posture spread: Codex offline-by-default < Copilot
  allowlist-by-default < Jules internet-on. All treat setup and agent phases
  differently.
- **Task sizing** (Devin's guidance): agents reliably complete work a human
  would do in **~3 hours or less**; break bigger goals into independent
  sessions with explicit success criteria (passing tests) and rich context.
  https://docs.devin.ai/essential-guidelines/when-to-use-devin.md
- Repo-carried agent instructions are a de facto standard: `AGENTS.md`
  supported by Copilot, Codex, Jules, Devin (auto-ingested), plus
  vendor-specific files.

## 6. Implications for the repos service

**Not applicable to us (multi-tenant machinery):** VM/container-per-task
isolation, network firewalls, offline agent phases, and content sanitization
exist to contain untrusted tenants and injected third-party content. A
single-owner box running owner code (the `scripts` precedent) doesn't need
them. One caveat: if discussion runs ever ingest *public* issue comments,
the prompt-injection threat returns and write-access-style trigger filtering
becomes load-bearing, not optional.

**Validated by convergence (adopt):**

- **The gate.** Label-gated execution is shipping practice (Jules,
  OpenHands, Claude Action), not an invention. Jules is the closest match to
  our guide-then-release lifecycle, including a plan step; its auto-approve
  timer is a design option for us (unattended goals eventually execute)
  though the safer default for us is to require the explicit act.
- **Session = one branch, incremental commits, human-controlled PR.** Adopt
  an agent branch namespace (e.g. `session/<id>` or `repos/<name>/...`),
  never push default, and prefer the Claude-Action stance: sessions push a
  branch; opening the PR is a deliberate act (ours can be automatic per
  policy, but the default matches the industry's human-in-control posture).
- **Ephemeral worktree off canonical state.** Our worktree-per-session over
  a canonical repo is exactly the industry's snapshot-boot pattern, minus
  the VM: nobody persists session mutations into base state; commits are
  the only durable product. Devin's snapshot model further suggests keeping
  per-project setup (deps installed, env prepared) *outside* the session
  loop — our analog is the project tree itself staying warm.
- **Ephemeral scoped credentials.** Installation tokens via a loopback
  credential helper (design doc) matches the verified consensus. Claude
  web's proxy adds an idea worth stealing cheaply: the helper can be
  **branch-aware**, refusing to mint for pushes outside the session's
  branch namespace.
- **Loop suppression by identity.** The suite analog of the GITHUB_TOKEN
  rule already exists: events carry `origin`/client-id (dropbox precedent),
  and the bot identity on GitHub is distinct from the owner. Rule to write
  down: webhook-driven triggers must ignore events authored by our own App.
- **Task sizing and instruction files.** Session instructions should carry
  explicit success criteria; goals bigger than a few hours of human work get
  decomposed during guidance, not executed whole. Honor `AGENTS.md` in
  project repos — every vendor does.

**Where we deliberately differ:** execution on the owned box, native, with
persistent project trees and no per-session VM — cheaper, simpler, and
consistent with the suite's operating bet (accept the trust model, skip the
cluster). The compensating controls are the gate, the branch namespace, the
scoped tokens, and PR review — the tenancy-independent half of what the
industry ships.
