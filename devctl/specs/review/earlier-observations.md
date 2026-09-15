# Earlier observation provenance

These observations were recorded in the pre-draft design at git commit `bcebbc8d16357833b1590efd0aad0adeb38fa639`.
They are retained as authoring provenance, not newly repeated experiments.

- D1 recorded that a temporary module requiring the ten approved module paths
  resolved with go mod tidy and a program importing all ten compiled. It did
  not record the resolved versions; this does not settle version approval.
- D9 recorded GNU tar member listing and manifest extraction over an xz archive
  made with the proposed bin/etc layout, including a nonzero missing-member
  result. That supports the retained archive inspection boundary.
- D7 recorded an HTTP 200 redirect for an agent-repl release asset on
  2026-09-14. This demonstrates GitHub's generic asset serving and does not
  establish an opsctl release or installer.

The original prose can be inspected with git show at that commit. New evidence
must meet the external-contract issue; it need not repeat an already-recorded
observation when that observation actually covers the current dependency.
