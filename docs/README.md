# docs — feature workflow & naming convention

Every substantial piece of work moves through two documents, in order. Both
live in `docs/` and share one **slug** taken from the work itself
(kebab-case, e.g. `event-triggering`):

| stage | file | what it holds |
|---|---|---|
| 1. design | `<slug>-design.md` | The **how** and the decisions behind it. The work to do is already decided; this fixes the approach, the tradeoffs taken, and the constraints the implementation must honor. Not a plan — no steps. |
| 2. plan | `<slug>-plan.md` | The **steps** that execute the design, broken into ordered **phases**. |

The shared slug is the pairing key: `<slug>-design.md` and `<slug>-plan.md`
sort adjacent and are obviously one feature's story.

## Phases

The plan is always broken into **phases**, and a phase is the unit of
execution. Size every phase so that **one subagent can complete it from a cold
start without overflowing its context** — that bound is the whole reason phases
exist. A phase that won't fit must be split.

Phases run **strictly sequentially. There is no parallelization.** Each phase
assumes the previous ones are done. Order them so that's always true.

## Execution

Once the plan exists and has been **read in full**, a coordinator is directed to
read the plan and `/finish` it. `/finish` runs an **orchestrator** that works
the phases **one at a time**, delegating each to a fresh subagent with a
complete, self-contained brief and integrating the distilled result before
moving to the next. The orchestrator does not do the hands-on work itself and
does not run phases concurrently — which is why the plan's phases are sized and
ordered for exactly that model.

So the lifecycle is:

    <slug>-design.md   →   <slug>-plan.md (phased)   →   read in full   →   /finish

## archive/

`docs/archive/` holds pre-convention documents. They predate this workflow and
are kept for reference only; don't extend them — new work follows the convention
above.
