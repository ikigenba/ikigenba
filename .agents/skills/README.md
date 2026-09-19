# Shared agent skills

This directory is the canonical, repo-scoped home for Agent Skills
(`<name>/SKILL.md` plus optional `scripts/`, `references/`, and `assets/`).

Skills follow the [Agent Skills](https://agentskills.io) open standard.
The format is specified at https://agentskills.io/specification.

One skill per subdirectory: `spec`, `draft-stories`, `draft-spec`,
`check-spec`, `build-spec`, `audit-spec`, `fanout`, `grill-me`, `handoff`.

`spec` is the shared foundation (layout, ids, the gap, issue filing, the
story and design formats); `draft-stories` and the four `*-spec` verbs are
the operations and each loads `spec`. Invoke each operation directly.
`draft-stories`, `draft-spec`, `build-spec`, and `audit-spec` load `fanout`
internally for delegation and independent verification; the user need not
name both skills.

## Who loads this path

Agents that look for `$PROJ_ROOT/.agents/skills/`, including when you are
working in a package subdirectory of the same checkout:

- Codex (walks CWD up to the git root)
- Cursor
- Gemini CLI (workspace skills)
- GitHub Copilot (VS Code, CLI, and cloud agent)
- OpenCode (walks CWD up to the git worktree)
- Grok (walks CWD up to the repo root)

Discovery stops at the nearest git root. A nested repository or submodule
will not see skills from this checkout.

## Claude Code

Claude Code does not scan `.agents/skills/`. It only loads `.claude/skills/`.

This repo bridges the gap with a symlink at the checkout root:

```
.claude/skills  →  ../.agents/skills
```
