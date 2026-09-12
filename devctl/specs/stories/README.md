# stories/

User stories: the intent devctl is built to serve, written before any design.
Each file is one group of related stories. A story says who wants what, the
preconditions in the system, the exact command, what each option does, and the
postconditions once the command has run.

Stories are the input to `specs/design/`. A design realises a group of stories
and assigns it requirement ids; when a group has been fully turned into designs,
its file is deleted here, so this folder holds only what is not yet designed.
Stories carry no requirement ids.

Groups, in the order they are meant to be designed:

1. `bootstrap.md` — running devctl at all.
2. `space-lifecycle.md` — `space list`, `create`, `destroy`, `stop`, `start`, `status`.
3. `secrets.md` — putting an app's secrets where a space can read them.
4. `build.md` — turning one app into the file deploy carries.
5. `deploy.md` — putting one built app file on a space.
6. `restore.md` — giving a space another space's data.
