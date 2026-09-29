> [!WARNING]
> This is unsupported AI slop.

# Ikigenba

This is a monorepo. Each subfolder is a component of the Ikigenba super project.

## Built from specs

Each component's code is generated from its specification — user stories and
design documents under its `specs/` directory — by agents, not written by
hand. To change a component, change its spec. The `.agents/skills` folder
holds the skills that draft specs and build code from them.

## Philosophy

Ship small changes straight to `main` and deploy right away. Automated checks
take the place of reviews, and the pipeline is fast enough that a revert goes
out as quickly as the bug did.
