# dory

An experimental coding harness built from agents that remember nothing.

```
$ dory <<'EOF'
Recreate an Asteroids-like game for Linux, in C, using SDL3.
EOF
```

One invocation is one pass: a root supervisor reads the prompt, searches the
session store for what earlier passes learned, delegates to child supervisors
and workers, and ends with a report and a cost line. `-resume UUID` runs the
next pass in the same session. Every agent starts empty; the store
(`~/.dory/sessions/<uuid>.db`, SQLite with full-text search) and the
filesystem are the only memory.

Built spec-first: `specs/design/` is the contract, `AGENTS.md` the gates.
See `docs/spec-system.md` at the repo root.
