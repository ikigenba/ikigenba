# TODO

## Next week: tear down the old standalone account `900253156012`

Retire the old account once the migration is fully settled. **Do NOT close it
until all prerequisites below are met** — closing the account deletes its hosted
zones and orphans any registration still inside it.

Prerequisites / checklist:
- [ ] All 4 `logic-refinery.*` registrations transferred into `mgmt`
      (`.com` / `.io` / `.net` / `.tv`) — transfer lock cleared, `transfer` +
      `accept` done. Verify via `route53domains list-domains --profile mgmt`.
- [ ] `logic-refinery.io` / `.net` / `.tv` nameservers repointed to the `mgmt`
      zones (like `.com` already is).
- [ ] 48h NS TTL window drained for every cut-over domain; confirm public
      resolvers serve only the new `mgmt` nameservers before deleting old zones.
- [ ] The two external domains resolved (see section below) — re-hosted or
      consciously abandoned. Their zones live in this account.
- [ ] `space.logic-refinery.io` / `public.logic-refinery.io` confirmed
      discardable (already agreed: yes) and any real infra behind
      `space.*` (an ALB + EC2 `agent1`/`server`) confirmed already gone.
- [ ] Remove temp `metaspot-migrate` IAM role from `900253156012` and the
      `lr-domain` profile from `~/.aws/config`.
- [ ] Then close the account: it is standalone (never joined the org), so close
      it by signing in as **root** → Account → Close Account (NOT via
      Organizations).

## Two externally-registered domains left behind in the 900253156012 migration

When we migrated the `logic-refinery.*` domains out of the old standalone account
`900253156012` (DNS moved into `mgmt`, see `mgmt/logic-refinery.tf`), two domains
were **deliberately not moved** because their *DNS was hosted in `900253156012`*
but their *registration lives at an unknown external registrar* (they did NOT
appear in that account's Route 53 registrar domain list):

- **`greenwaterrpg.com`** — parked (only NS/SOA in its old hosted zone).
- **`pine-ttg.org`** — formerly a Google Workspace, now dead (Google returns
  `550 NoSuchUser`; MX records are stale cargo). Mail does not flow today.

### Why this matters
Their hosted zones currently live in `900253156012`. When that account is
retired (and its zones deleted), **these two domains will stop resolving** unless
their DNS is first re-hosted somewhere we control and their nameservers repointed
at the external registrar.

### Future work
1. **Find where `greenwaterrpg.com` and `pine-ttg.org` are registered** (the
   external registrar — GoDaddy / Namecheap / etc.). We need registrar access to
   repoint nameservers.
2. Decide per-domain: re-host DNS in `mgmt` (recreate zones + NS cutover at the
   external registrar) **or** let them lapse. Both are parked/dead, so lapsing is
   acceptable if we don't want them.
3. Do this **before** deleting the old hosted zones / closing `900253156012`,
   otherwise we lose resolution for whichever we decide to keep.

### Status of the rest of the migration (context)
- `logic-refinery.com` DNS fully cut over to `mgmt`; mail verified intact.
- `logic-refinery.io` / `.net` / `.tv` zones created in `mgmt`; NS cutover +
  registration transfers to `mgmt` still pending.
- Old `900253156012` zones kept alive through the 48h NS TTL window before teardown.
- Temp `metaspot-migrate` role in `900253156012` + `lr-domain` AWS profile to be
  removed once the migration is fully done.
