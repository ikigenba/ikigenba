// Command cron is the loopback-only scheduled-event-emitter service behind
// nginx. It serves a bearer-gated MCP surface for agents and a
// dashboard-session-cookie-gated human web landing page under /srv/cron/. It trusts
// the X-Owner-Email / X-Client-Id headers nginx injects after a successful
// auth_request against the dashboard's authorization server, and performs no
// token logic of its own; nginx remains the sole trust boundary for both doors.
//
// The uniform chassis — the fixed subcommands, config-from-env, the migration
// runner + downgrade guard, the loopback HTTP server + PRM + identity gate, and
// the /feed producer mount — is owned by appkit. main.go declares only cron's
// identity (the Spec) and wires its
// domain surface: the crontab CRUD MCP tools, the minute-aligned tick worker that
// emits cron.<name> events, and the LIVE Publishes provider that reports those
// types from the crontab. RESOURCE_ID / AUTH_SERVER are composed in-binary by
// appkit/config from IKIGENBA_DOMAIN + MOUNT.
package main

import (
	"appkit"

	"cron/internal/cronapp"
)

func main() {
	appkit.Main(cronapp.Spec())
}
