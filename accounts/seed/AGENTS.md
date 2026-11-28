# Team folder

This folder is the team's shared context. Everyone on the team, and every agent working for them, reads and writes here.

## The team

Not set up. Do this before other work. Ask what the team or business is called, and what it does in a sentence or two. Then replace this paragraph with the name and that description.

## How this folder works

- An agent loads the `AGENTS.md` of the folder it starts in and of every folder above it, never below. Put general context high and specialized context deep.
- An `AGENTS.md` is always read, so keep it short. Where there is more to say, give a summary and link to a file with the details.
- A section that starts "Not set up" is a stub: it says how to set something up. Leave a stub alone until a task needs it, unless it says to do it before other work. If no one is there to answer its questions, leave it and carry on with the task.
- When a stub's work is done, and you have checked the result works, replace the stub with short instructions for using what was set up, and commit.
- Where a section says it needs another, set that one up first if it is still a stub.
- To add something the team will need later, write a stub for it in the `AGENTS.md` of the folder it belongs to.
- To see who is on the team, run `getent group "$(stat -c %G .)"` in this folder. No file lists them.
- `data/` holds the team's data. It is not kept in git.
- The folder is a git repository. Commit what you change, with a message that says what changed and why.
