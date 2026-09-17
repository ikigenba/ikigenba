---
name: draft-stories
description: Turn what the user wants into user stories under specs/stories/ — drafting new stories or updating existing ones in the canonical story format, grilling the user one question at a time for whatever the intent leaves open. Produces stories, not design.
---

# intent into stories

The user explains what they want the project to do; the output is that intent
written as stories in `specs/stories/`, in the format the sibling `spec`
skill dictates in `references/story-format.md`. Load that skill and that
reference first. Stories are the input to `draft-spec`; this skill never
writes design, mints ids, or touches source.

"Draft" means make `specs/stories/` say what the user now intends. Sometimes
that is a new group file, sometimes new stories in an existing group,
sometimes a change to stories already there. Existing stories are never
deleted and never left contradicting the new ones.

## Procedure

1. **Read what exists.** Resolve the project root: the sub-project
   directory holding `specs/` and its own `AGENTS.md`, never the git
   root, whose `AGENTS.md` is repo-wide guidance. If the request does not
   identify the sub-project, ask. Read every group file in `specs/stories/`; they are short
   and the new stories must fit them. Read `specs/design/` and the
   code enough to know the current behavior the stories build on: the usage
   frame, the exit codes, the option grammar, the names already in use.
   Existing behavior is evidence of the current target, not a constraint;
   the user may be changing it, and then the stories change with it.

2. **Place the intent.** Decide which group owns it. An existing group owns
   it when the intent extends that group's seam: a new option on its command,
   a new failure of it, a changed output. A new group is needed when the
   intent is a seam no group covers. A new group takes the next number and
   a slug per the format's filename rules; the number places it after
   every existing group, which is where a group that builds on them
   belongs.

3. **Enumerate the stories.** Each distinct interaction is one story, and
   each distinct failure the actor can cause or meet is one story. List
   them by heading before writing any, so the set is visible: the happy
   path, each option, each refusal, the help text. A story the user has not
   mentioned but the format demands (an unknown-option refusal, a missing
   argument) is proposed, not silently added.

4. **Grill for what is open.** Everything the format requires must be
   decided: the exact command grammar, what each option does, the literal
   output, the exit code and streams, the preconditions, the
   postconditions. Where the user's description or the existing stories and
   code settle a point, settle it and say so. Where they do not, follow the
   `grill-me` procedure: one question at a time, each with a recommendation
   and the reasoning behind it, until nothing is open. Do not invent an
   output text, exit code, or postcondition the user has not agreed to;
   these are the story's substance, and a guess becomes design.

5. **Write.** Author or update the group file in the canonical format. Then
   make the rest of `specs/stories/` consistent: a changed exit code or
   message changes every story that quotes it; a renamed thing is renamed
   wherever a story says it. If a new group's number needs more digits,
   re-pad the existing filenames to match.

6. **Report.** List the stories added and changed by heading and file, the
   existing stories touched for consistency, and any question the user
   deferred. If a group that changed already has a design realising it, say
   so: the design now diverges from its stories and `draft-spec` is the next
   step. If nothing was open and nothing conflicted, say that; the user is
   not asked questions the inputs already answer.

## What this skill does not do

- It does not write `specs/design/`, mint requirement ids, or run
  `draft-spec`. Stories carry no ids.
- It does not change source or tests, and does not commit. The user reviews
  the stories and decides when they are committed.
- It does not delete a story on its own. A story the user no longer wants
  is a change of intent: rewrite it to say what is wanted now. If the
  interaction itself is gone, say so and let the user decide.
- It does not resolve a question by asking a sibling agent or reading
  another project's stories as authority. Stories are the user's intent;
  only the user settles them.
