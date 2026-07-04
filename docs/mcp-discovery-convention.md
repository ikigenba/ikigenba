# MCP self-discovery: the pattern every service follows

> **Status: normative pattern, one pilot shipped.** This defines the convention
> every ikigenba service's MCP surface should follow so a connecting agent can
> discover and correctly use the service from the connection alone, with no
> external skill. It was piloted on **crm** (design Decisions D9–D11 in
> `crm/project/design/`). Other services adopt it as they next revise their MCP
> surface; this doc is the overview of the approach, not a per-service
> prescription. It defines *the shape*, not the exact strings, which each service
> writes for its own domain.

## Why this exists

Each service is its own MCP server. An agent connects to it directly and learns
what it can do from three MCP-native channels, in this order:

1. the server's `initialize` **`instructions`** string,
2. the **tool descriptions** returned by `tools/list`,
3. anything the agent explicitly **calls**.

Two facts shape everything below. First, channels 1 and 2 are loaded into the
agent's context on *every* connection, before the agent has read the user's
request, so their size is a standing cost. Second, an agent should not need a
separately installed skill, plugin, or doc to route work to the service or to
recall how to drive its tools. The surface has to describe itself.

The goal is a surface that is **findable** (an agent can tell what the service is
for, in the words a user actually uses) and **self-sufficient once found** (an
agent can drive every tool correctly from what the service itself provides).

## The three tiers, ordered by delivery guarantee

Content lives in the channel whose delivery guarantee matches how badly the agent
needs it. Stronger guarantee, tighter budget:

| Tier | Channel | Guarantee | Holds |
|---|---|---|---|
| 0 | `initialize` `instructions` | Always loaded, once | Orientation + routing vocabulary + one pointer to the guide |
| 1 | tool descriptions (`tools/list`) | Always loaded, per tool | When to use each tool, its args, what it returns |
| 2 | a `guide` tool (`tools/call`) | Loaded only when called | The full reference: field catalogs and worked examples |

The move that makes it work: reference bulk (field catalogs, exhaustive option
lists, long examples) does **not** live in Tier 0 or Tier 1, where every
connection pays for it. It lives in Tier 2, pulled on demand.

### Tier 0 — the service `instructions`

Two or three sentences, loaded once. It carries three things:

- **What the service is over**, named in the vocabulary a user actually types,
  not only the service's internal nouns. Include the everyday synonyms (a CRM's
  "organizations" is also "companies"; "deals" is also "pipeline"). These
  synonyms are the routing hooks that let an agent match the service without an
  external alias skill.
- **The normal working flow** across the service's verbs, in one clause.
- **One pointer to the guide** (Tier 2), so an agent that needs the full
  reference knows where it is.

It is orientation, not reference. It never carries a field catalog.

### Tier 1 — the tool descriptions

Lean and keyword-forward. Each description says *when to use the tool*, *how to
shape its arguments*, and *what it returns*. It carries the cross-cutting
semantics an agent needs to call the tool correctly at all (an upsert's dedup
rule, an append-only constraint, a declarative-set replacement rule). It does
**not** carry per-type or per-entity reference catalogs; those move to Tier 2.
The vocabulary that makes a tool findable belongs here too, because tool search
indexes tool descriptions.

### Tier 2 — the `guide` tool

A single read-only tool, conventionally named `guide`, that returns the service's
usage guide as text: the entity/field catalogs relocated out of Tier 1, plus
**basic and advanced** worked examples. Properties of the tier:

- **It is a tool, not an MCP resource.** A tool is the channel with the hard
  delivery guarantee: it appears in `tools/list` in every client and the agent
  can call it like any other. MCP resources are the semantically tidier home for
  reference text, but most clients do not surface resources into the agent's
  context, so an agent told to "read the guide" may have no way to. A service may
  additionally expose the same bytes as a resource for clients that render them,
  but the tool is the guarantee, not the resource.
- **It is a non-domain tool.** Like a `health` or a reflection tool, `guide`
  describes the service; it is not a per-entity verb. Adding domain entities still
  adds no tools. A surface whose tool count is a function of its verbs keeps that
  property.
- **Start flat and read-only.** One call returns the whole document; no
  parameters to reason about. Add scoping (a topic parameter, or splitting into
  several guides) only when a single service's guide grows past roughly one
  screen. The tool reads nothing, mutates nothing, and does not error.
- **The document is embedded**, versioned and shipped with the binary, so the
  guide an agent reads always matches the running service.

## Where the guide is referenced

In exactly two places: the Tier 0 `instructions` and the `guide` tool's own
description. Deliberately **not** in every tool description. The `instructions`
pointer is always loaded, so it reaches the agent before it first needs the
guide; repeating the pointer per tool would re-inflate Tier 1 for no gain. The
accepted cost is that an agent which ignores the Tier 0 pointer and calls a
write tool cold may need one corrective round; the service's own validation
messages carry that self-correction.

## Boundaries

- **Discovery describes; it does not change behavior.** Adopting this pattern
  rewrites how a surface is *described* and adds one read-only tool. It changes
  nothing about what the existing tools *do*: the entity model, the verb
  semantics, validation, and the event surface are untouched. Adoption should be
  provably behavior-neutral for every existing tool call.
- **No aggregation.** This is a per-service pattern. Each service describes
  itself; nothing here routes or proxies across services. A suite-level index
  that helps an agent pick *which* service to use is a separate concern and is
  out of scope for this doc.
- **Each service owns its own strings.** The vocabulary, the tool wording, and
  the guide are the service's to write for its own domain. This doc fixes the
  shape and the tier boundaries, not the words.

## Adopting the pattern in a service

1. **Rewrite the `initialize` `instructions`** to name the domain in user
   vocabulary (with synonyms), state the verb flow, and point at `guide`.
2. **Slim the tool descriptions** to when/args/returns plus cross-cutting
   semantics; move every reference catalog out.
3. **Add the `guide` tool**: embed a usage document (the relocated catalogs plus
   basic and advanced worked examples), list it, and dispatch it as a flat,
   read-only, input-free call. It is one added tool, not one per entity.
4. **Reference the guide in two places only**: the `instructions` and the
   `guide` tool's own description.
5. **Prove behavior neutrality**: every existing tool call returns exactly what
   it did before. The discovery changes are surface-only.

The crm pilot (Decisions D9, D10, D11) is the worked example of every step.
