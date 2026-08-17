# nginx — Product

**Authority: intent.** This document owns *why* the `nginx/` tree exists, who
uses it, what is in and out of scope, and what it **promises** in outcome terms.
It does **not** own mechanism, file formats, directive shapes, exit codes, or
test assertions — those belong to `project/design/`. Where the two could
overlap, this document states the *promise*; design states the exact, checkable
proof of it.

## Problem

nginx is the suite's sole trust boundary. It is the thing that decides whether a
`/srv/<svc>/` request is allowed through, strips the path prefix, and hands the
service the identity headers it then trusts unconditionally. None of that is
application code, so none of it is exercised by running a service directly. A
developer who tests by talking to a service on its loopback port is testing the
one configuration the product never runs in: no introspection, no prefix
stripping, forged identity trivially accepted. Bugs in the boundary — a missing
gate, a wrong header, a route that never matched — surface only in production.

The same box has a second, unrelated exposure. The operator's other registered
domains resolve to it, and with only the account's apex server block configured,
any request whose `Host` does not match the apex falls through to whichever
block nginx picked first. Domains that have nothing to do with the product end
up showing the product, and the answer a stranger gets depends on config
ordering rather than on a decision anyone made.

One of those domains is not merely parked. `michaelgreenly.dev` is the
operator's own public web address: the destination printed on outreach material
and handed to prospects. A visitor who types it must get the operator's site,
over valid HTTPS, at the bare domain — not a parking page, not the suite's
dashboard, and not a long `/srv/…` path on the account's apex.

## Purpose

`nginx/` is the suite's front-door tree. It holds a **local development front
door** that fronts the suite on the developer's machine the way the account's
apex block fronts it in production, the **committed parked answer** served to
every host the apex block does not claim, and the **committed vhost** that
gives the operator's own domain its site instead of the parked answer. One
tree, one subject: what the front door does with a request before any service
sees it.

## Users

- **The developer or agent working on a service**, who needs the full request
  chain — gate, strip, inject, route — in front of the service they are changing,
  on their own machine, without root and without touching the system nginx.
- **The operator of the live box**, who needs every domain pointed at that box to
  give a deliberate answer, and needs that answer to survive a rebuild rather
  than living only as hand-applied configuration.

## Scope

In scope: the local front door's configuration and the script that starts it; the
per-service route fragments it serves locally, kept in step with what each
service ships; the committed parked configuration and page for non-apex hosts;
and the committed vhost configuration for the operator's site domain, including
one reserved same-origin path on that domain that forwards a tracking beacon to
the suite's inbound-webhook ingress. Nothing else — this tree runs no
application logic, holds no state, owns no service's route definition (each
service ships its own), does not author the site the domain serves (that is
content living in the `sites` service), does not originate the tracking (the
beacon it forwards is acted on by the `webhooks` and `scripts` services, not
here), and does not install anything on a box. Installing the parked and vhost artifacts on the
live box is an operator runbook step described in the repo-root `deploy.md`,
not something this tree performs. The site domain is served at the bare domain
only: `www` has no address today, and giving it one is DNS and certificate work
outside this tree.

The local front door is a development tool. It deliberately does not reproduce
production's TLS, its certificates, its privileged ports, or its parked
default-host behavior; a developer who needs those tests them on a real box.

## Contractual constants

- The local front door answers on **port 8080**, plain HTTP, on the loopback
  interface. (The box's `:80` is not free, and the front door needs no
  privilege.)
- The parked page's message is exactly **`These aren't the droids you're looking
  for!`** — both its title and its body text.
- The parked answer is served with a **success** status, not an error status.
- The operator's site domain is exactly **`michaelgreenly.dev`**, bare domain
  only, and what it serves is the operator's public site hosted by the suite's
  `sites` service.

## What we promise (user-facing behavior)

- **The suite, locally, through the real boundary.** With the suite running, a
  developer reaches every service through one local address, and each request
  crosses the same gate-and-inject chain it would cross in production: an
  unauthenticated request is refused before the service sees it, an authenticated
  one arrives at the service with its prefix stripped and its identity attached.
  A service reached this way behaves as it will on the box.
- **Starting it is one command, and it needs nothing from the system.** The
  developer runs it in the foreground, watches the log, and stops it with Ctrl-C.
  It requires no root, changes nothing outside the tree, and leaves nothing
  behind that a second run would trip over.
- **New services appear without editing the front door.** A service that ships
  its own route is routed locally the next time the front door starts; the
  developer does not hand-maintain a copy of it.
- **Every host that reaches the live box gets a deliberate answer.** A domain the
  account's apex does not claim gets a plain page saying so, over its own valid
  certificate for the domains worth securing — and never a service, never the
  dashboard, never a page that depends on which server block nginx happened to
  read first.
- **The operator's own domain serves the operator's own site.** A visitor to
  `michaelgreenly.dev` gets the operator's public site over valid HTTPS with a
  certificate of its own — never the parking page, never the dashboard. The
  site's content is edited where site content lives, in the `sites` service,
  and appears at the domain with no change to this tree.
- **The operator's domain can receive a tracking beacon.** A page on the site
  can fire a fire-and-forget beacon to a reserved path on the domain, and the
  front door delivers it to the suite's inbound-webhook ingress as an
  authenticated trigger — without the page, the served site, or the committed
  configuration ever holding the secret that authenticates it.
- **The account's apex is untouched by all of it.** Adding the parked answer
  and the site-domain vhost changes nothing about how the account's own domain
  serves the dashboard and its services.

## Success criteria (outcomes)

- With the suite running, a request to the local front door's root address
  returns the dashboard, and a request to a service path returns that service —
  the same service reachable directly on its loopback port.
- A request to a service path with no credentials is refused at the front door,
  and the service records no call for it.
- A request to a service path with valid credentials reaches the service, which
  sees the caller's identity without having been told it by the caller.
- Starting the front door on a machine where it has never run before succeeds
  without root, and stopping and restarting it succeeds again with no manual
  cleanup in between.
- A service that ships a route the front door has never seen is reachable through
  the front door after the next start, with no edit to this tree.
- On the live box, a request over HTTPS to one of the operator's kept non-apex
  domains, with normal certificate validation, returns a successful page carrying
  the parked message.
- On the live box, a request to the box's bare address returns the parked page
  rather than any service — showing the parked answer is the deliberate default,
  not a name match.
- With the parked answer live, the account's apex still returns the dashboard and
  still routes its service paths, over validated TLS.
- The parked files installed on the box are byte-identical to the ones committed
  in this tree, so a rebuild reproduces them exactly.
- On the live box, a request over HTTPS to `michaelgreenly.dev`, with normal
  certificate validation, returns the operator's public site — the same content
  the site serves at its `sites`-service address — and the domain no longer
  shows the parked page.
- With the site domain live, the other kept non-apex domains still return the
  parked page, and the account's apex still returns the dashboard and routes
  its service paths, over validated TLS.
- The site-domain vhost file installed on the box is byte-identical to the one
  committed in this tree, so a rebuild reproduces it exactly.
- On the live box, a fire-and-forget `POST` to `michaelgreenly.dev`'s reserved
  beacon path is accepted and delivered to the suite's inbound-webhook ingress
  as an authenticated trigger, while a non-`POST` to that path is refused at the
  front door; the bearer that authenticates it appears nowhere in the committed
  configuration.
