---
name: devlog
description: Convention for devlog entries — durable context for future sessions
---

# Devlog

Top-level `devlog/` folders are how past sessions leave context for future ones.

## Why

You have no memory across sessions. Git shows *what* changed, not *why*. Without the reasoning, a future session will re-litigate settled questions or undo deliberate choices. Entries are notes for the next version of yourself.

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
