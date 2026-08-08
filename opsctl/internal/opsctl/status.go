package opsctl

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
)

// Status reports, for one app or every installed app, the live release and its
// health: the full version `bin/run` points at and the systemd unit's raw state (via
// IsActiveState — the state string regardless of exit code, so a "failed" or
// "inactive" unit is shown rather than turned into an error).
//
// An empty app iterates the discovery helper (every OPSCTL_ROOT child with a
// `bin/run` symlink). The output is a simple aligned table: app · version · active.
// It is read-only — nothing on the box changes.
func (o *Opsctl) Status(ctx context.Context, app string) error {
	var apps []string
	if app != "" {
		apps = []string{app}
	} else {
		var err error
		apps, err = o.discoverApps()
		if err != nil {
			return fmt.Errorf("status: discover apps: %w", err)
		}
	}

	w := o.Out
	if w == nil {
		w = os.Stdout
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "APP\tVERSION\tACTIVE")
	for _, a := range apps {
		version, active, err := o.appStatus(ctx, a)
		if err != nil {
			return fmt.Errorf("status: %s: %w", a, err)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", a, version, active)
	}
	return tw.Flush()
}

// appStatus gathers one app's status row: the live version (bin/run's target
// basename, "-" if absent), and the raw systemd state.
func (o *Opsctl) appStatus(ctx context.Context, app string) (version, active string, err error) {
	l := o.layout(app)

	version = "-"
	v, err := o.currentVersion(l)
	if err != nil {
		return "", "", err
	}
	if v != "" {
		version = v
	}

	active = "unknown"
	if state, err := o.System.IsActiveState(ctx, app); err == nil {
		active = state
	}
	return version, active, nil
}
