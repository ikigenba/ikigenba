# Shared agent skills

This directory is the canonical, repo-scoped home for Agent Skills
(`<name>/SKILL.md` plus optional `scripts/`, `references/`, and `assets/`).

Skills follow the [Agent Skills](https://agentskills.io) open standard.
The format is specified at https://agentskills.io/specification.

There are no skills here yet. Add one skill per subdirectory.

## Who loads this path

Agents that look for `$PROJ_ROOT/.agents/skills/`, including when you are
working in a package subdirectory of the same checkout:

- Codex (walks CWD up to the git root)
- Cursor
- Gemini CLI (workspace skills)
- GitHub Copilot (VS Code, CLI, and cloud agent)
- OpenCode (walks CWD up to the git worktree)
- Grok (walks CWD up to the repo root)

Personal copies of the same layout live at `~/.agents/skills/` for some of
these tools. This folder is the project copy, shared by every worktree.

Discovery stops at the nearest git root. A nested repository or submodule
will not see skills from this checkout.

## Claude Code

Claude Code does not scan `.agents/skills/`. It only loads `.claude/skills/`.

This repo bridges the gap with a symlink at the checkout root:

```
.claude/skills  →  ../.agents/skills
```

Keep skills only in this directory. Do not add a second tree under
`.claude/skills/`.
