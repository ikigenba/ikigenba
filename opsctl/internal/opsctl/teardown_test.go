package opsctl

import (
	"context"
	"os"
	"strings"
	"testing"
)

// setupForTeardown provisions a path-routed service (ledger) so a subsequent
// teardown has a real /opt/<app> tree, unit, and fragment to remove. It returns
// the layout and resets the op recorder so the test asserts only teardown's ops.
func setupForTeardown(t *testing.T, o *Opsctl, sys *stubSystem, app string, fragSrc string) Layout {
	t.Helper()
	runInitBox(t, o, readRepoFile(t, "../../../dashboard/etc/nginx.conf"))
	if err := o.Setup(context.Background(), SetupOptions{App: app, Fragment: fragSrc}); err != nil {
		t.Fatalf("setup %s: %v", app, err)
	}
	sys.ops = nil // assert only teardown's ops
	return NewLayoutSys(o.Root, o.SysRoot, app)
}

// TestTeardown_PathRoutedService is the teardown acceptance: against a fully
// set-up ledger it removes the unit + nginx fragment + /opt/<app> tree and drops
// the app user, requesting the privileged ops through the seam in REVERSE order.
func TestTeardown_PathRoutedService(t *testing.T) {
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := newProvisioner(t, root, sysRoot, sys)

	app := "ledger"
	fragSrc := readRepoFile(t, "../../../ledger/etc/nginx.conf")
	l := setupForTeardown(t, o, sys, app, fragSrc)

	if err := o.Teardown(context.Background(), TeardownOptions{App: app, Force: true}); err != nil {
		t.Fatalf("teardown ledger: %v", err)
	}

	// The unit file, nginx fragment, and /opt/<app> tree are all gone.
	if _, err := os.Stat(l.UnitPath()); !os.IsNotExist(err) {
		t.Errorf("teardown left the unit file (err=%v)", err)
	}
	if _, err := os.Stat(l.FragmentPath()); !os.IsNotExist(err) {
		t.Errorf("teardown left the nginx fragment (err=%v)", err)
	}
	if _, err := os.Stat(l.AppDir()); !os.IsNotExist(err) {
		t.Errorf("teardown left the /opt/%s tree (err=%v)", app, err)
	}

	// Privileged ops requested through the seam, in REVERSE of setup's order: stop
	// + disable the unit, daemon-reload, nginx validate/reload, then drop the user.
	wantOps := []string{
		"systemctl:stop ledger.service",
		"systemctl:disable ledger.service",
		"daemon-reload",
		"nginx-test",
		"nginx-reload",
		"delete-user:ledger",
	}
	if got := sys.opSeq(); strings.Join(got, "|") != strings.Join(wantOps, "|") {
		t.Fatalf("teardown ops = %v, want %v", got, wantOps)
	}
}

// TestTeardown_KeepUser retains the app user: every other removal happens, but no
// delete-user op is requested.
func TestTeardown_KeepUser(t *testing.T) {
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := newProvisioner(t, root, sysRoot, sys)

	app := "ledger"
	fragSrc := readRepoFile(t, "../../../ledger/etc/nginx.conf")
	l := setupForTeardown(t, o, sys, app, fragSrc)

	if err := o.Teardown(context.Background(), TeardownOptions{App: app, Force: true, KeepUser: true}); err != nil {
		t.Fatalf("teardown --keep-user: %v", err)
	}
	if _, err := os.Stat(l.AppDir()); !os.IsNotExist(err) {
		t.Errorf("teardown left the /opt/%s tree", app)
	}
	wantOps := []string{
		"systemctl:stop ledger.service",
		"systemctl:disable ledger.service",
		"daemon-reload",
		"nginx-test",
		"nginx-reload",
	}
	if got := sys.opSeq(); strings.Join(got, "|") != strings.Join(wantOps, "|") {
		t.Fatalf("teardown --keep-user ops = %v, want %v", got, wantOps)
	}
}

// TestTeardown_Worker tears down a service with no nginx fragment (worker/batch):
// the unit + tree + user go, but nginx is never touched.
func TestTeardown_Worker(t *testing.T) {
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := newProvisioner(t, root, sysRoot, sys)

	app := "worker"
	runInitBox(t, o, readRepoFile(t, "../../../dashboard/etc/nginx.conf"))
	if err := o.Setup(context.Background(), SetupOptions{App: app, Fragment: ""}); err != nil {
		t.Fatalf("setup worker: %v", err)
	}
	sys.ops = nil

	if err := o.Teardown(context.Background(), TeardownOptions{App: app, Force: true}); err != nil {
		t.Fatalf("teardown worker: %v", err)
	}
	// No nginx-test / nginx-reload — there was no fragment.
	wantOps := []string{
		"systemctl:stop worker.service",
		"systemctl:disable worker.service",
		"daemon-reload",
		"delete-user:worker",
	}
	if got := sys.opSeq(); strings.Join(got, "|") != strings.Join(wantOps, "|") {
		t.Fatalf("teardown worker ops = %v, want %v", got, wantOps)
	}
}

// TestTeardown_RequiresForce asserts teardown refuses without --force (the removal
// is irreversible) and runs NO box ops before the guard.
func TestTeardown_RequiresForce(t *testing.T) {
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := newProvisioner(t, root, sysRoot, sys)
	setupForTeardown(t, o, sys, "ledger", readRepoFile(t, "../../../ledger/etc/nginx.conf"))

	err := o.Teardown(context.Background(), TeardownOptions{App: "ledger"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("teardown without --force err = %v, want a --force guard error", err)
	}
	if len(sys.opSeq()) != 0 {
		t.Errorf("teardown ran box ops before the --force guard: %v", sys.opSeq())
	}
	// The tree is untouched.
	if _, err := os.Stat(o.layout("ledger").AppDir()); err != nil {
		t.Errorf("teardown removed the tree despite missing --force: %v", err)
	}
}

// TestTeardown_RefusesApex asserts teardown refuses the apex/DEFAULT app, which
// owns the box's nginx apex block + TLS cert.
func TestTeardown_RefusesApex(t *testing.T) {
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := newProvisioner(t, root, sysRoot, sys)

	err := o.Teardown(context.Background(), TeardownOptions{App: apexApp, Force: true})
	if err == nil || !strings.Contains(err.Error(), "apex") {
		t.Fatalf("teardown apex err = %v, want an apex-app refusal", err)
	}
	if len(sys.opSeq()) != 0 {
		t.Errorf("teardown ran box ops before the apex guard: %v", sys.opSeq())
	}
}

// TestTeardown_NotProvisioned asserts teardown fails loudly when /opt/<app> does
// not exist (almost certainly a typo) rather than silently removing nothing.
func TestTeardown_NotProvisioned(t *testing.T) {
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := newProvisioner(t, root, sysRoot, sys)

	err := o.Teardown(context.Background(), TeardownOptions{App: "ghost", Force: true})
	if err == nil || !strings.Contains(err.Error(), "not provisioned") {
		t.Fatalf("teardown of unprovisioned app err = %v, want a not-provisioned error", err)
	}
	if len(sys.opSeq()) != 0 {
		t.Errorf("teardown ran box ops before the provisioned check: %v", sys.opSeq())
	}
}
