package opsctl

import (
	"context"
	"fmt"
	"os"
)

// apexApp is the well-known DEFAULT/apex app name (owns the apex server block +
// cert via init-box). teardown refuses to remove it: tearing down the apex would
// strip the box's TLS/auth substrate and break every path-routed service.
const apexApp = "dashboard"

// TeardownOptions parameterises `opsctl teardown <app>` — the inverse of Setup.
// It cleanly decommissions a path-routed service from the box, undoing setup's
// four provisioning steps in REVERSE order. The DB under /opt/<app>/data is
// discarded with the rest of the tree (teardown is a removal, not a backup).
type TeardownOptions struct {
	App      string // service name (== systemd unit / fragment basename / app user)
	Force    bool   // required: teardown is destructive (removes the unit, fragment, /opt/<app>, user)
	KeepUser bool   // retain the dedicated --system app user (default: drop it)
}

// Teardown removes a deployed path-routed service from the box — the inverse of
// Setup (PLAN §3 Commit 3). It undoes setup's four steps in REVERSE order,
// through the same System seam:
//
//  1. stop, then disable the systemd unit (seam),
//  2. remove the unit file + daemon-reload,
//  3. remove the nginx location fragment + nginx -t + reload,
//  4. rm -rf /opt/<app> (the DB is intentionally discarded with the tree),
//  5. drop the dedicated --system app user (unless KeepUser).
//
// Guards: it refuses the apex/DEFAULT app (removing it would strip the box's
// TLS/auth substrate), requires --force (the removal is irreversible), and
// errors clearly when /opt/<app> is not provisioned (nothing to tear down).
func (o *Opsctl) Teardown(ctx context.Context, opts TeardownOptions) error {
	if opts.App == "" {
		return fmt.Errorf("teardown: app is required")
	}
	app := opts.App
	if app == apexApp {
		return fmt.Errorf("teardown: refusing to tear down the apex/DEFAULT app %q — it owns the box's nginx apex block and TLS cert", app)
	}
	if !opts.Force {
		return fmt.Errorf("teardown: %s removes the unit, nginx fragment, /opt/%s (including its DB), and the app user — re-run with --force to confirm", app, app)
	}
	l := o.layout(app)

	// Precondition: the app must actually be provisioned. Tearing down a name with
	// no /opt/<app> tree is almost certainly a typo — fail loudly rather than
	// silently "succeed" at removing nothing.
	if _, err := os.Stat(l.AppDir()); err != nil {
		return fmt.Errorf("teardown: %s not provisioned (%s missing): %w", app, l.AppDir(), err)
	}

	unit := app + ".service"

	// 1. Stop then disable the unit (inverse of setup's enable-not-started). Stop
	//    first so the service is down before its config is removed.
	o.logf("stop unit %s", unit)
	if err := o.System.Systemctl(ctx, "stop", unit); err != nil {
		return fmt.Errorf("teardown: stop unit: %w", err)
	}
	o.logf("disable unit %s", unit)
	if err := o.System.Systemctl(ctx, "disable", unit); err != nil {
		return fmt.Errorf("teardown: disable unit: %w", err)
	}

	// 2. Remove the unit file + daemon-reload (inverse of setup step 3).
	o.logf("remove systemd unit %s", l.UnitPath())
	if err := os.Remove(l.UnitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("teardown: remove unit file: %w", err)
	}
	if err := o.System.DaemonReload(ctx); err != nil {
		return fmt.Errorf("teardown: daemon-reload: %w", err)
	}

	// 3. Remove the nginx location fragment, then validate + reload (inverse of
	//    setup step 4). If no fragment was ever dropped (worker/batch service), the
	//    removal is a no-op and nginx is left untouched.
	if _, err := os.Stat(l.FragmentPath()); err == nil {
		o.logf("remove nginx fragment %s", l.FragmentPath())
		if err := os.Remove(l.FragmentPath()); err != nil {
			return fmt.Errorf("teardown: remove fragment: %w", err)
		}
		if err := o.System.NginxTest(ctx); err != nil {
			return fmt.Errorf("teardown: nginx -t: %w", err)
		}
		if err := o.System.NginxReload(ctx); err != nil {
			return fmt.Errorf("teardown: nginx reload: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("teardown: stat fragment: %w", err)
	} else {
		o.logf("no nginx fragment for %s (worker/batch service)", app)
	}

	// 4. Remove the /opt/<app> tree (inverse of setup step 2). The DB lives under
	//    data/ and is discarded with the tree — a teardown abandons the service's
	//    state by design (PLAN §3: box migration uses a fresh DB).
	o.logf("remove /opt/%s tree", app)
	if err := os.RemoveAll(l.AppDir()); err != nil {
		return fmt.Errorf("teardown: remove app tree: %w", err)
	}

	// 5. Drop the dedicated --system app user (inverse of setup step 1), unless the
	//    operator asked to retain it.
	if opts.KeepUser {
		o.logf("keep-user: retaining the %s system user", app)
	} else {
		o.logf("drop app user %s", app)
		if err := o.System.DeleteSystemUser(ctx, app); err != nil {
			return fmt.Errorf("teardown: drop app user: %w", err)
		}
	}

	o.logf("teardown complete for %s — unit, nginx fragment, /opt/%s, and the app user removed", app, app)
	return nil
}
