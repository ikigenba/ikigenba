---
name: devlog
description: Convention for devlog entries — durable context for future sessions
---

# Devlog

Top-level `devlog/` folders are how past sessions leave context for future ones.

## Why

You have no memory across sessions. Git shows *what* changed, not *why*. Without the reasoning, a future session will re-litigate settled questions or undo deliberate choices. Entries are notes for the next version of yourself.

## Tactical vs strategic

Commit messages are **tactical**: they describe the specific change in this diff — what was modified, added, removed. Scope: one commit.

Devlog entries are **strategic**: they describe direction, intent, and reasoning that outlives any single commit — why this approach was chosen, what alternatives were rejected, what tradeoffs were accepted, what was deliberately deferred. Scope: the project's evolving thinking.

A commit message answers "what did this change do?" A devlog entry answers "why is the project heading this way?"

## Entries

- Filename: `YYYY-MM-DD-short-slug.md`
- Title: `# YYYY-MM-DD — one-line summary`
- A few paragraphs. Focus on **what** (briefly) and **why** (the real content): decisions named, alternatives rejected, tradeoffs accepted, things deliberately deferred.
- Skip file lists, diffs, and anything `git log` already covers.

### Shape

```
# YYYY-MM-DD — one-line summary

## What
<a few sentences>

## Why these choices
**Decision.** Reasoning.
**Decision.** Reasoning.
```
