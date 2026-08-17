package opsctl

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newOpsctl builds an Opsctl over a temp root with the stub system and a fake
// runner carrying the given FAKE_* scenario env.
func newOpsctl(t *testing.T, root, app string, sys *stubSystem, base []string) *Opsctl {
	t.Helper()
	sys.app = app
	return &Opsctl{
		Root:   root,
		Keep:   3,
		System: sys,
		Runner: fakeRunner{baseEnv: base},
		Store:  newFakeStore(),
		Out:    &strings.Builder{},
		Err:    &strings.Builder{},
	}
}

func newOpsctlWithEvents(t *testing.T, root, app string, sys *stubSystem, store *fakeStore, events *[]string, base []string) *Opsctl {
	t.Helper()
	sys.app = app
	sys.events = events
	store.events = events
	return &Opsctl{
		Root:   root,
		Keep:   3,
		System: sys,
		Runner: fakeRunner{baseEnv: base, events: events},
		Store:  store,
		Out:    &strings.Builder{},
		Err:    &strings.Builder{},
	}
}

func fakeEnv(app, version string, embedded int, manifest string) []string {
	env := []string{
		"FAKE_APP=" + app,
		"FAKE_VERSION=" + version,
		"FAKE_EMBEDDED=" + itoa(embedded),
	}
	if manifest != "" {
		env = append(env, "FAKE_MANIFEST="+manifest)
	}
	return env
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// stageAndDeploy runs the two-verb cutover the old monolithic Install did in one
// shot: stage the artifact into libexec/<app>-<version> then deploy it. The
// acceptance tests below drive the full lifecycle through this helper so they
// exercise both verbs exactly as an operator would.
func stageAndDeploy(t *testing.T, o *Opsctl, app, version, artifact string) error {
	t.Helper()
	bundle := bundleArtifactFromBinary(t, app, version, filepath.Base(artifact), artifact)
	if err := o.Stage(context.Background(), app, version, bundle, false); err != nil {
		return err
	}
	return o.Deploy(context.Background(), app, version)
}

func stageOnly(t *testing.T, o *Opsctl, app, version, artifact string) {
	t.Helper()
	bundle := bundleArtifactFromBinary(t, app, version, filepath.Base(artifact), artifact)
	if err := o.Stage(context.Background(), app, version, bundle, false); err != nil {
		t.Fatalf("stage %s: %v", version, err)
	}
}

func stageReleaseWithConfig(t *testing.T, o *Opsctl, l Layout, app, version, manifest, nginx string) {
	t.Helper()
	stageOnly(t, o, app, version, stageArtifact(t, app+"-"+version))
	if err := os.WriteFile(l.ManifestFile(version), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest for %s: %v", version, err)
	}
	if err := os.WriteFile(l.NginxConfFile(version), []byte(nginx), 0o644); err != nil {
		t.Fatalf("write nginx config for %s: %v", version, err)
	}
}

type deployRecordingRunner struct {
	fakeRunner
	onMigrate func()
}

func (r deployRecordingRunner) Run(ctx context.Context, binary, verb string, args []string, env []string) (string, error) {
	if verb == "migrate" && r.onMigrate != nil {
		r.onMigrate()
	}
	return r.fakeRunner.Run(ctx, binary, verb, args, env)
}

func eventIndex(events []string, want string) int {
	for i, got := range events {
		if got == want {
			return i
		}
	}
	return -1
}

func countEvents(events []string, want string) int {
	n := 0
	for _, got := range events {
		if got == want {
			n++
		}
	}
	return n
}

func containsEvent(events []string, want string) bool {
	return eventIndex(events, want) >= 0
}

func assertSymlinkText(t *testing.T, link, want string) {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %s: %v", link, err)
	}
	if got != want {
		t.Fatalf("%s -> %q, want %q", link, got, want)
	}
}

func assertSymlinkResolves(t *testing.T, link, want string) {
	t.Helper()
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("%s does not resolve: %v", link, err)
	}
	if got != want {
		t.Fatalf("%s resolves to %q, want %q", link, got, want)
	}
}

// readRunVersion resolves bin/run → its deployed version.
func readRunVersion(t *testing.T, l Layout) string {
	t.Helper()
	v, err := (&Opsctl{}).currentVersion(l)
	if err != nil {
		t.Fatalf("read live version: %v", err)
	}
	return v
}

// dbApplied reads the fake "DB" file (a single integer = applied schema version).
func dbApplied(t *testing.T, l Layout) (int, bool) {
	t.Helper()
	b, err := os.ReadFile(l.DBPath())
	if err != nil {
		return 0, false
	}
	n := 0
	for _, c := range strings.TrimSpace(string(b)) {
		n = n*10 + int(c-'0')
	}
	return n, true
}

// resolveThroughStablePaths asserts the launcher-facing stable paths are valid:
// bin/run resolves to an existing binary, and etc/current/manifest.env exists
// and names the app. This must hold after every install/rollback (PLAN §2.6 —
// load-bearing at all times, including mid-swap).
func resolveThroughStablePaths(t *testing.T, l Layout) {
	t.Helper()
	// bin/run -> ../libexec/<app>-<version>; resolving it must reach a real file.
	runResolved, err := filepath.EvalSymlinks(l.RunLink())
	if err != nil {
		t.Fatalf("bin/run does not resolve: %v", err)
	}
	if fi, err := os.Stat(runResolved); err != nil || fi.IsDir() {
		t.Fatalf("bin/run target %s is not a runnable file: %v", runResolved, err)
	}
	man, err := os.ReadFile(l.ActiveManifest())
	if err != nil {
		t.Fatalf("manifest.env missing: %v", err)
	}
	if !strings.Contains(string(man), "APP="+l.App) {
		t.Fatalf("manifest.env does not name app: %q", string(man))
	}
}

// TestInstallInstallRollback is the C2 acceptance core: a full install, then a
// second install, then a rollback, against a temp OPSCTL_ROOT — asserting the
// atomic repoint, the stable paths staying valid throughout, that a no-schema-
// change deploy never modifies the DB, and (in the schema-advance variant below)
// that the backup/restore wires together.
func TestInstallInstallRollback_NoSchemaChange(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	// First install: v1.0.0, embedded schema 2 (creates the DB at applied=2).
	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 2, ""))
	art1 := stageArtifact(t, "ledger-v1.0.0")
	if err := stageAndDeploy(t, o, app, "v1.0.0", art1); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}
	if got := readRunVersion(t, l); got != "v1.0.0" {
		t.Fatalf("live version = %q, want v1.0.0", got)
	}
	resolveThroughStablePaths(t, l)
	applied1, ok := dbApplied(t, l)
	if !ok || applied1 != 2 {
		t.Fatalf("after v1.0.0 install, applied = %d (ok=%v), want 2", applied1, ok)
	}
	dbInfo1, _ := os.Stat(l.DBPath())

	// Second install: v1.1.0, SAME embedded schema 2 → no schema advance → the DB
	// must NOT be modified. Deploy still takes the unconditional object-store
	// backup before migrating.
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.1.0", 2, ""))
	art2 := stageArtifact(t, "ledger-v1.1.0")
	if err := stageAndDeploy(t, o2, app, "v1.1.0", art2); err != nil {
		t.Fatalf("deploy v1.1.0: %v", err)
	}
	if got := readRunVersion(t, l); got != "v1.1.0" {
		t.Fatalf("live version = %q, want v1.1.0", got)
	}
	resolveThroughStablePaths(t, l)

	// DB untouched by a no-schema-change deploy (mtime + content unchanged).
	dbInfo2, _ := os.Stat(l.DBPath())
	if !dbInfo1.ModTime().Equal(dbInfo2.ModTime()) {
		t.Errorf("no-schema-change install modified the DB mtime: %v -> %v", dbInfo1.ModTime(), dbInfo2.ModTime())
	}
	if _, err := os.Stat(l.PreMigrationBackup("v1.1.0")); err == nil {
		t.Errorf("deploy wrote legacy pre-migration backup %s", l.PreMigrationBackup("v1.1.0"))
	}

	// Rollback to the prior release (v1.0.0). No schema advance ⇒ no DB restore.
	if err := o2.Rollback(context.Background(), app, ""); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := readRunVersion(t, l); got != "v1.0.0" {
		t.Fatalf("after rollback live version = %q, want v1.0.0", got)
	}
	resolveThroughStablePaths(t, l)

	if sys.restarts != 3 {
		t.Fatalf("restart count = %d, want 3", sys.restarts)
	}
}

// TestDeploySchemaAdvanceUsesOnlyObjectStoreBackup proves deploy no longer has a
// schema-aware local backup path: a schema-advancing release still migrates the DB
// forward, but it does not write a legacy backups/pre-<version>.db file.
func TestDeploySchemaAdvanceUsesOnlyObjectStoreBackup(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	// v1.0.0 — embedded schema 2, fresh DB → applied=2.
	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 2, ""))
	if err := stageAndDeploy(t, o1, app, "v1.0.0", stageArtifact(t, "v1")); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}
	if applied, _ := dbApplied(t, l); applied != 2 {
		t.Fatalf("applied after v1.0.0 = %d, want 2", applied)
	}

	// v2.0.0 — embedded schema 5 → schema advances (2 → 5). Deploy must back up
	// through the ObjectStore seam, not the removed local pre-migration path.
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v2.0.0", 5, ""))
	if err := stageAndDeploy(t, o2, app, "v2.0.0", stageArtifact(t, "v2")); err != nil {
		t.Fatalf("deploy v2.0.0: %v", err)
	}
	if _, err := os.Stat(l.PreMigrationBackup("v2.0.0")); err == nil {
		t.Fatalf("deploy wrote removed local pre-migration backup %s", l.PreMigrationBackup("v2.0.0"))
	}
	// The live DB advanced to 5.
	if applied, _ := dbApplied(t, l); applied != 5 {
		t.Fatalf("applied after v2.0.0 = %d, want 5 (migrated forward)", applied)
	}
	resolveThroughStablePaths(t, l)
}

// TestPreflight_Rejections asserts install refuses a bad artifact and leaves the
// live release untouched.
func TestPreflight_Rejections(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	sys := &stubSystem{}

	// Version mismatch: artifact self-reports v9.9.9 but we stage it as v1.0.0.
	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v9.9.9", 1, ""))
	mismatch := stageBundleArtifact(t, app, "v1.0.0", "mismatch")
	err := o.Stage(context.Background(), app, "v1.0.0", mismatch, false)
	if err == nil || !strings.Contains(err.Error(), "self-reports version") {
		t.Fatalf("version-mismatch stage err = %v, want a version-mismatch refusal", err)
	}
	if sys.restarts != 0 {
		t.Errorf("preflight failure still restarted the unit (%d times)", sys.restarts)
	}
	// Preflight refusal keeps the /tmp artifact for retry (decision 2).
	if _, err := os.Stat(mismatch); err != nil {
		t.Errorf("preflight refusal removed the /tmp artifact, want it kept: %v", err)
	}

	// Not a static ELF: a text file as the bundled libexec binary.
	bad := filepath.Join(t.TempDir(), "not-elf")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	badBundle := bundleArtifactFromBinary(t, app, "v1.0.0", "not-elf", bad)
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := o2.Stage(context.Background(), app, "v1.0.0", badBundle, false); err == nil || !strings.Contains(err.Error(), "ELF") {
		t.Fatalf("non-ELF stage err = %v, want an ELF refusal", err)
	}
}

func TestStageCollisionGuardComparesArtifactBytes(t *testing.T) {
	// R-TBF2-2SE4
	const (
		app     = "ledger"
		version = "v1.0.0"
	)
	ctx := context.Background()
	root := t.TempDir()
	l := NewLayout(root, app)
	o := newOpsctl(t, root, app, &stubSystem{}, fakeEnv(app, version, 1, ""))

	first := stageBundleArtifact(t, app, version, "first")
	if err := o.Stage(ctx, app, version, first, false); err != nil {
		t.Fatalf("first stage: %v", err)
	}
	placed, err := os.ReadFile(l.LibexecBinary(version))
	if err != nil {
		t.Fatalf("read placed binary: %v", err)
	}

	identical := stageBundleArtifact(t, app, version, "identical")
	if err := o.Stage(ctx, app, version, identical, false); err != nil {
		t.Fatalf("identical-byte re-stage: %v", err)
	}
	if _, err := os.Stat(identical); !os.IsNotExist(err) {
		t.Fatalf("identical-byte no-op retained bundle (stat err=%v)", err)
	}

	different := stageBundleArtifactWithBinarySuffix(t, app, version, "different", "controlled-difference")
	err = o.Stage(ctx, app, version, different, false)
	if err == nil || !strings.Contains(err.Error(), "different artifact bytes") {
		t.Fatalf("different-byte re-stage err = %v, want collision refusal", err)
	}
	if _, err := os.Stat(different); err != nil {
		t.Fatalf("different-byte refusal removed retry bundle: %v", err)
	}
	stillPlaced, err := os.ReadFile(l.LibexecBinary(version))
	if err != nil {
		t.Fatalf("read binary after refusal: %v", err)
	}
	if !bytes.Equal(stillPlaced, placed) {
		t.Fatal("different-byte refusal changed the already-staged binary")
	}
}

// TestInstall_ConvertsLegacyBinRunFile asserts the conversion case the D2 box
// prototype hit: when /opt/<app>/bin/run already exists as a REGULAR FILE (the
// old pre-redesign layout's wrapper script), install must replace it with the
// stable versioned libexec symlink rather than failing with EEXIST.
func TestInstall_ConvertsLegacyBinRunFile(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	// Seed the OLD layout: bin/run is a regular file (the legacy wrapper script).
	if err := os.MkdirAll(l.BinDir(), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(l.RunLink(), []byte("#!/bin/sh\nexec ./ledger.bin\n"), 0o755); err != nil {
		t.Fatalf("seed legacy bin/run: %v", err)
	}

	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v0.1.0", 3, ""))
	if err := stageAndDeploy(t, o, app, "v0.1.0", stageArtifact(t, "ledger-v0.1.0")); err != nil {
		t.Fatalf("deploy over legacy bin/run: %v", err)
	}
	// bin/run is now a symlink at ../current/<app> and resolves to a real binary.
	got, err := os.Readlink(l.RunLink())
	if err != nil {
		t.Fatalf("bin/run is not a symlink after conversion: %v", err)
	}
	if want := l.runTarget("v0.1.0"); got != want {
		t.Fatalf("bin/run -> %q, want %q", got, want)
	}
	resolveThroughStablePaths(t, l)
}

func TestDeploy_SwapsBinRunToVersionedLibexecBinary(t *testing.T) {
	// R-3TIQ-ML04
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := stageAndDeploy(t, o1, app, "v1.0.0", stageArtifact(t, "ledger-v1.0.0")); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.1.0", 1, ""))
	if err := stageAndDeploy(t, o2, app, "v1.1.0", stageArtifact(t, "ledger-v1.1.0")); err != nil {
		t.Fatalf("deploy v1.1.0: %v", err)
	}

	target, err := os.Readlink(l.RunLink())
	if err != nil {
		t.Fatalf("read bin/run: %v", err)
	}
	if want := l.runTarget("v1.1.0"); target != want {
		t.Fatalf("bin/run target = %q, want %q", target, want)
	}
	resolved, err := filepath.EvalSymlinks(l.RunLink())
	if err != nil {
		t.Fatalf("resolve bin/run: %v", err)
	}
	if resolved != l.LibexecBinary("v1.1.0") {
		t.Fatalf("bin/run resolves to %q, want %q", resolved, l.LibexecBinary("v1.1.0"))
	}
	if _, err := os.Stat(l.LibexecBinary("v1.0.0")); err != nil {
		t.Fatalf("previous libexec binary missing after deploy: %v", err)
	}
	for _, forbidden := range []string{
		filepath.Join(l.AppDir(), "releases"),
		filepath.Join(l.AppDir(), "current"),
	} {
		if _, err := os.Lstat(forbidden); err == nil {
			t.Fatalf("legacy path exists after deploy: %s", forbidden)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat legacy path %s: %v", forbidden, err)
		}
	}
}

func TestDeployNoSchemaAdvanceBacksUpBeforeMigrateAndSwap(t *testing.T) {
	// R-863N-LLT9
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 2, ""))
	if err := stageAndDeploy(t, o1, app, "v1.0.0", stageArtifact(t, "ledger-v1.0.0")); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}

	var events []string
	store := newFakeStore()
	sys2 := &stubSystem{}
	o2 := newOpsctlWithEvents(t, root, app, sys2, store, &events, fakeEnv(app, "v1.1.0", 2, ""))
	o2.Runner = deployRecordingRunner{
		fakeRunner: fakeRunner{baseEnv: fakeEnv(app, "v1.1.0", 2, ""), events: &events},
		onMigrate: func() {
			assertSymlinkText(t, l.RunLink(), l.runTarget("v1.0.0"))
			assertSymlinkText(t, l.EtcCurrentLink(), "v1.0.0")
			assertSymlinkText(t, l.ShareCurrentLink(), "v1.0.0")
		},
	}

	stageOnly(t, o2, app, "v1.1.0", stageArtifact(t, "ledger-v1.1.0"))
	events = nil
	if err := o2.Deploy(context.Background(), app, "v1.1.0"); err != nil {
		t.Fatalf("deploy v1.1.0: %v", err)
	}

	if got := countEvents(events, "put:ledger:snapshot"); got != 1 {
		t.Fatalf("snapshot backup Put count = %d, want 1; events=%v", got, events)
	}
	put := eventIndex(events, "put:ledger:snapshot")
	migrate := eventIndex(events, "run:migrate")
	if put == -1 || migrate == -1 || put > migrate {
		t.Fatalf("backup/migrate order wrong: events=%v", events)
	}
	assertSymlinkText(t, l.RunLink(), l.runTarget("v1.1.0"))
	assertSymlinkText(t, l.EtcCurrentLink(), "v1.1.0")
	assertSymlinkText(t, l.ShareCurrentLink(), "v1.1.0")
}

func TestDeploySequenceSwapsReloadsRestartsAndPrunesWithoutManifestVerb(t *testing.T) {
	// R-87BJ-ZDJY
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	o1.Keep = 1
	if err := stageAndDeploy(t, o1, app, "v1.0.0", stageArtifact(t, "ledger-v1.0.0")); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.1.0", 1, ""))
	o2.Keep = 1
	if err := stageAndDeploy(t, o2, app, "v1.1.0", stageArtifact(t, "ledger-v1.1.0")); err != nil {
		t.Fatalf("deploy v1.1.0: %v", err)
	}

	var events []string
	store := newFakeStore()
	sys3 := &stubSystem{}
	sys3.onNginxReload = func() {
		assertSymlinkText(t, l.RunLink(), l.runTarget("v1.2.0"))
		assertSymlinkText(t, l.EtcCurrentLink(), "v1.2.0")
		assertSymlinkText(t, l.ShareCurrentLink(), "v1.2.0")
	}
	sys3.onIsActive = func() {
		if _, err := os.Stat(l.LibexecBinary("v1.0.0")); err != nil {
			t.Fatalf("prune ran before is-active; v1.0.0 stat: %v", err)
		}
	}
	o3 := newOpsctlWithEvents(t, root, app, sys3, store, &events, fakeEnv(app, "v1.2.0", 1, ""))
	o3.Keep = 1

	stageOnly(t, o3, app, "v1.2.0", stageArtifact(t, "ledger-v1.2.0"))
	events = nil
	if err := os.WriteFile(legacyManifestPath(l), []byte("legacy-stable-manifest\n"), 0o644); err != nil {
		t.Fatalf("seed stable manifest: %v", err)
	}
	if err := o3.Deploy(context.Background(), app, "v1.2.0"); err != nil {
		t.Fatalf("deploy v1.2.0: %v", err)
	}

	for _, forbidden := range []string{"run:manifest", "run:schema"} {
		if eventIndex(events, forbidden) != -1 {
			t.Fatalf("deploy ran removed verb %s: events=%v", forbidden, events)
		}
	}
	wantOrder := []string{"put:ledger:snapshot", "run:migrate", "nginx-reload", "restart:ledger", "is-active:ledger"}
	last := -1
	for _, want := range wantOrder {
		idx := eventIndex(events, want)
		if idx == -1 {
			t.Fatalf("missing %s in events %v", want, events)
		}
		if idx <= last {
			t.Fatalf("event %s out of order in %v", want, events)
		}
		last = idx
	}
	if b, err := os.ReadFile(legacyManifestPath(l)); err != nil {
		t.Fatalf("read seeded stable manifest: %v", err)
	} else if string(b) != "legacy-stable-manifest\n" {
		t.Fatalf("stable manifest changed to %q", string(b))
	}
	if _, err := os.Stat(l.LibexecBinary("v1.0.0")); err == nil {
		t.Fatalf("v1.0.0 still exists after prune")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat v1.0.0 after prune: %v", err)
	}
}

func TestDeployDefaultRendersInstallsAndTestsApexBlock(t *testing.T) {
	// R-MSOP-5MDA
	t.Setenv("IKIGENBA_DOMAIN", "example.test")
	root := t.TempDir()
	sysRoot := t.TempDir()
	app := "dashboard"
	version := "v1.2.3"
	l := NewLayoutSys(root, sysRoot, app)
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, "APP="+app+"\nDEFAULT=true\nPORT=4310\n"))
	o.SysRoot = sysRoot

	manifest := "APP=dashboard\nDEFAULT=true\nPORT=4310\n"
	nginxSrc := "server {\n    server_name __DOMAIN__;\n    proxy_pass http://127.0.0.1:4310;\n}\n"
	stageReleaseWithConfig(t, o, l, app, version, manifest, nginxSrc)

	if err := o.Deploy(context.Background(), app, version); err != nil {
		t.Fatalf("deploy default app: %v", err)
	}

	got, err := os.ReadFile(l.ApexBlockPath())
	if err != nil {
		t.Fatalf("read apex block: %v", err)
	}
	if want := renderApexBlock(nginxSrc, "example.test"); string(got) != want {
		t.Fatalf("apex block = %q, want %q", got, want)
	}
	ops := sys.opSeq()
	testIdx := eventIndex(ops, "nginx-test")
	reloadIdx := eventIndex(ops, "nginx-reload")
	if testIdx == -1 || reloadIdx == -1 || testIdx > reloadIdx {
		t.Fatalf("nginx test/reload order wrong: %v", ops)
	}
	if sys.restarts != 1 {
		t.Fatalf("restart count = %d, want 1", sys.restarts)
	}
}

func TestDeployDefaultReadsApexDomainFromEnvironmentNotManifest(t *testing.T) {
	// R-CNPY-3Z4Y
	t.Setenv("IKIGENBA_DOMAIN", "env-domain.test")
	root := t.TempDir()
	sysRoot := t.TempDir()
	app := "dashboard"
	version := "v1.2.3"
	l := NewLayoutSys(root, sysRoot, app)
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, "APP="+app+"\nDEFAULT=true\nPORT=4310\n"))
	o.SysRoot = sysRoot

	manifest := "APP=dashboard\nDEFAULT=true\nMOUNT=/dashboard\nPORT=4310\n"
	nginxSrc := "server_name __DOMAIN__; proxy_pass http://127.0.0.1:4310;\n"
	stageReleaseWithConfig(t, o, l, app, version, manifest, nginxSrc)

	if err := o.Deploy(context.Background(), app, version); err != nil {
		t.Fatalf("deploy default app without manifest domain: %v", err)
	}
	got, err := os.ReadFile(l.ApexBlockPath())
	if err != nil {
		t.Fatalf("read apex block: %v", err)
	}
	block := string(got)
	if !strings.Contains(block, "env-domain.test") {
		t.Fatalf("apex block = %q, want env domain", block)
	}
	if strings.Contains(block, "__DOMAIN__") {
		t.Fatalf("apex block kept domain placeholder: %q", block)
	}
}

func TestDeployDefaultRequiresDomainBeforeInstallingOrReloading(t *testing.T) {
	// R-MTWL-JE3Z
	t.Setenv("IKIGENBA_DOMAIN", "")
	root := t.TempDir()
	sysRoot := t.TempDir()
	app := "dashboard"
	version := "v1.2.3"
	l := NewLayoutSys(root, sysRoot, app)
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, "APP="+app+"\nDEFAULT=true\nPORT=4310\n"))
	o.SysRoot = sysRoot

	manifest := "APP=dashboard\nDEFAULT=true\nPORT=4310\n"
	nginxSrc := "server_name __DOMAIN__; proxy_pass http://127.0.0.1:4310;\n"
	stageReleaseWithConfig(t, o, l, app, version, manifest, nginxSrc)
	if err := os.MkdirAll(filepath.Dir(l.ApexBlockPath()), 0o755); err != nil {
		t.Fatalf("create apex dir: %v", err)
	}
	const existingApexBlock = "existing apex block\n"
	if err := os.WriteFile(l.ApexBlockPath(), []byte(existingApexBlock), 0o644); err != nil {
		t.Fatalf("seed apex block: %v", err)
	}

	err := o.Deploy(context.Background(), app, version)
	if err == nil || !strings.Contains(err.Error(), "IKIGENBA_DOMAIN") {
		t.Fatalf("deploy err = %v, want IKIGENBA_DOMAIN refusal", err)
	}
	if got, readErr := os.ReadFile(l.ApexBlockPath()); readErr != nil {
		t.Fatalf("read apex block after refusal: %v", readErr)
	} else if string(got) != existingApexBlock {
		t.Fatalf("apex block changed to %q, want %q", got, existingApexBlock)
	}
	ops := sys.opSeq()
	for _, forbidden := range []string{"nginx-test", "nginx-reload"} {
		if containsEvent(ops, forbidden) {
			t.Fatalf("unexpected %s after missing domain: %v", forbidden, ops)
		}
	}
	if sys.restarts != 0 {
		t.Fatalf("restart count = %d, want 0", sys.restarts)
	}
}

type nginxTestFailSystem struct {
	*stubSystem
}

func (s nginxTestFailSystem) NginxTest(ctx context.Context) error {
	s.record("nginx-test")
	return fmt.Errorf("nginx config invalid")
}

func TestDeployDefaultNginxTestFailureStopsBeforeReloadOrRestart(t *testing.T) {
	// R-MV4H-X5UO
	t.Setenv("IKIGENBA_DOMAIN", "example.test")
	root := t.TempDir()
	sysRoot := t.TempDir()
	app := "dashboard"
	version := "v1.2.3"
	l := NewLayoutSys(root, sysRoot, app)
	baseSys := &stubSystem{app: app}
	o := &Opsctl{
		Root:    root,
		SysRoot: sysRoot,
		Keep:    3,
		System:  nginxTestFailSystem{stubSystem: baseSys},
		Runner:  fakeRunner{baseEnv: fakeEnv(app, version, 1, "APP="+app+"\nDEFAULT=true\nPORT=4310\n")},
		Store:   newFakeStore(),
		Out:     &strings.Builder{},
		Err:     &strings.Builder{},
	}

	manifest := "APP=dashboard\nDEFAULT=true\nPORT=4310\n"
	nginxSrc := "server_name __DOMAIN__; proxy_pass http://127.0.0.1:4310;\n"
	stageReleaseWithConfig(t, o, l, app, version, manifest, nginxSrc)

	err := o.Deploy(context.Background(), app, version)
	if err == nil || !strings.Contains(err.Error(), "nginx -t") {
		t.Fatalf("deploy err = %v, want nginx -t failure", err)
	}
	if _, statErr := os.Stat(l.ApexBlockPath()); statErr != nil {
		t.Fatalf("apex block should be written before nginx test: %v", statErr)
	}
	ops := baseSys.opSeq()
	if !containsEvent(ops, "nginx-test") {
		t.Fatalf("nginx test was not called: %v", ops)
	}
	for _, forbidden := range []string{"nginx-reload", "restart:" + app} {
		if containsEvent(ops, forbidden) {
			t.Fatalf("unexpected %s after nginx test failure: %v", forbidden, ops)
		}
	}
	if baseSys.restarts != 0 {
		t.Fatalf("restart count = %d, want 0", baseSys.restarts)
	}
}

func TestDeployNonDefaultSkipsApexBranchAndPreservesFragmentReload(t *testing.T) {
	// R-MXKA-OPC2
	root := t.TempDir()
	sysRoot := t.TempDir()
	app := "ledger"
	version := "v1.2.3"
	l := NewLayoutSys(root, sysRoot, app)
	if err := os.MkdirAll(l.LocationsDir(), 0o755); err != nil {
		t.Fatalf("create locations dir: %v", err)
	}
	if err := os.Symlink(l.ActiveNginxConf(), l.FragmentPath()); err != nil {
		t.Fatalf("seed fragment symlink: %v", err)
	}
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, "APP="+app+"\nDEFAULT=false\nPORT=3999\n"))
	o.SysRoot = sysRoot

	manifest := "APP=ledger\nDEFAULT=false\nPORT=3999\n"
	nginxSrc := "location /srv/ledger/ { proxy_pass http://127.0.0.1:3999; }\n"
	stageReleaseWithConfig(t, o, l, app, version, manifest, nginxSrc)

	if err := o.Deploy(context.Background(), app, version); err != nil {
		t.Fatalf("deploy non-default app: %v", err)
	}
	target, err := os.Readlink(l.FragmentPath())
	if err != nil {
		t.Fatalf("fragment is not a symlink: %v", err)
	}
	if target != l.ActiveNginxConf() {
		t.Fatalf("fragment target = %q, want %q", target, l.ActiveNginxConf())
	}
	if _, statErr := os.Stat(l.ApexBlockPath()); !os.IsNotExist(statErr) {
		t.Fatalf("apex block stat err = %v, want no apex block", statErr)
	}
	ops := sys.opSeq()
	if containsEvent(ops, "nginx-test") {
		t.Fatalf("non-default deploy ran nginx-test: %v", ops)
	}
	if !containsEvent(ops, "nginx-reload") {
		t.Fatalf("non-default deploy did not reload nginx: %v", ops)
	}
	if sys.restarts != 1 {
		t.Fatalf("restart count = %d, want 1", sys.restarts)
	}
}

func TestDeploySwapsRunEtcAndShareToSameVersion(t *testing.T) {
	// R-1A79-JG03
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := stageAndDeploy(t, o1, app, "v1.0.0", stageArtifact(t, "ledger-v1.0.0")); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.1.0", 1, ""))
	if err := stageAndDeploy(t, o2, app, "v1.1.0", stageArtifact(t, "ledger-v1.1.0")); err != nil {
		t.Fatalf("deploy v1.1.0: %v", err)
	}

	assertSymlinkText(t, l.RunLink(), l.runTarget("v1.1.0"))
	assertSymlinkText(t, l.EtcCurrentLink(), "v1.1.0")
	assertSymlinkText(t, l.ShareCurrentLink(), "v1.1.0")
	assertSymlinkResolves(t, l.RunLink(), l.LibexecBinary("v1.1.0"))
	assertSymlinkResolves(t, l.ActiveNginxConf(), l.NginxConfFile("v1.1.0"))
	assertSymlinkResolves(t, l.ActiveManifest(), l.ManifestFile("v1.1.0"))
	assertSymlinkResolves(t, filepath.Join(l.ShareCurrentLink(), "assets", "resource.txt"), filepath.Join(l.ShareVersionDir("v1.1.0"), "assets", "resource.txt"))
}

func TestDeploySymlinksResolveBeforeAndAfterAtomicRepoint(t *testing.T) {
	// R-3VYJ-E4HI
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := stageAndDeploy(t, o1, app, "v1.0.0", stageArtifact(t, "ledger-v1.0.0")); err != nil {
		t.Fatalf("deploy v1.0.0: %v", err)
	}
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.1.0", 1, ""))
	stageOnly(t, o2, app, "v1.1.0", stageArtifact(t, "ledger-v1.1.0"))

	assertSymlinkResolves(t, l.RunLink(), l.LibexecBinary("v1.0.0"))
	assertSymlinkResolves(t, l.EtcCurrentLink(), l.EtcVersionDir("v1.0.0"))
	assertSymlinkResolves(t, l.ShareCurrentLink(), l.ShareVersionDir("v1.0.0"))
	if _, err := os.Stat(l.LibexecBinary("v1.1.0")); err != nil {
		t.Fatalf("new run target missing before deploy: %v", err)
	}
	if _, err := os.Stat(l.EtcVersionDir("v1.1.0")); err != nil {
		t.Fatalf("new etc target missing before deploy: %v", err)
	}
	if _, err := os.Stat(l.ShareVersionDir("v1.1.0")); err != nil {
		t.Fatalf("new share target missing before deploy: %v", err)
	}

	if err := o2.Deploy(context.Background(), app, "v1.1.0"); err != nil {
		t.Fatalf("deploy v1.1.0: %v", err)
	}

	assertSymlinkResolves(t, l.RunLink(), l.LibexecBinary("v1.1.0"))
	assertSymlinkResolves(t, l.EtcCurrentLink(), l.EtcVersionDir("v1.1.0"))
	assertSymlinkResolves(t, l.ShareCurrentLink(), l.ShareVersionDir("v1.1.0"))
}

// TestInstall_ChownsStateDirToAppUser asserts install hands the state tree back to
// the `<app>` service user after the root-run migrate (the cutover-reset bug:
// migrate, run as root, creates a fresh DB owned root:root, which the unit's
// dedicated <app> user cannot write — crash-loop). The chown must request the
// bare app name as BOTH owner and group (matching setup's EnsureSystemUser) and
// target the state dir, on every install. The stub records (never executes) the
// op, so no real system path is chowned under the temp OPSCTL_ROOT.
func TestInstall_ChownsStateDirToAppUser(t *testing.T) {
	root := t.TempDir()
	app := "crm"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 2, ""))
	if err := stageAndDeploy(t, o, app, "v1.0.0", stageArtifact(t, "crm-v1.0.0")); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	want := "chown:" + app + ":" + app + ":" + l.StateDir()
	var found bool
	for _, op := range sys.opSeq() {
		if op == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("install did not request the data-dir chown; want %q in ops %v", want, sys.opSeq())
	}
}

func TestDeployChownsCacheAfterMigrateAndBeforeRestart(t *testing.T) {
	// R-50PW-I47U
	root := t.TempDir()
	const app = "crm"
	const version = "v1.0.0"
	l := NewLayout(root, app)
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 2, ""))
	cacheChown := "chown:" + app + ":" + app + ":" + l.CacheDir()
	o.Runner = deployRecordingRunner{
		fakeRunner: fakeRunner{baseEnv: fakeEnv(app, version, 2, "")},
		onMigrate: func() {
			if containsEvent(sys.opSeq(), cacheChown) {
				t.Fatalf("cache ownership was handed back before migrate: %v", sys.opSeq())
			}
		},
	}
	sys.onRestart = func() {
		// Restart invokes this hook while holding the stub's mutex, so inspect the
		// protected operation slice directly rather than re-locking via opSeq.
		if !containsEvent(sys.ops, cacheChown) {
			t.Fatalf("restart occurred before cache ownership hand-back: %v", sys.ops)
		}
	}

	if err := stageAndDeploy(t, o, app, version, stageArtifact(t, app+"-"+version)); err != nil {
		t.Fatalf("ordinary deploy: %v", err)
	}

	ops := sys.opSeq()
	if got := countEvents(ops, cacheChown); got != 1 {
		t.Fatalf("cache ownership call count = %d, want 1 for %q; ops=%v", got, cacheChown, ops)
	}
	stateChown := "chown:" + app + ":" + app + ":" + l.StateDir()
	if state, cache := eventIndex(ops, stateChown), eventIndex(ops, cacheChown); state == -1 || cache <= state {
		t.Fatalf("state/cache ownership order wrong: %v", ops)
	}
}

func TestDeployStateChownOwnsServedTree(t *testing.T) {
	// R-3MPQ-A91D
	root := t.TempDir()
	app := "sites"
	version := "v1.0.0"
	l := NewLayout(root, app)
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, "APP="+app+"\n"))

	for _, dir := range []string{l.WWWRoot(), l.WWWPublicDir(), l.WWWPrivateDir()} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("create www dir %s: %v", dir, err)
		}
	}

	if err := stageAndDeploy(t, o, app, version, stageArtifact(t, app+"-"+version)); err != nil {
		t.Fatalf("deploy served app: %v", err)
	}

	ops := sys.opSeq()
	stateChown := "chown:" + app + ":" + app + ":" + l.StateDir()
	if eventIndex(ops, stateChown) == -1 {
		t.Fatalf("deploy ops = %v, missing state chown %q", ops, stateChown)
	}
	for _, op := range ops {
		if strings.Contains(op, ":web:") || strings.HasPrefix(op, "chmod:") {
			t.Fatalf("deploy requested retired served-tree permission op %q; ops = %v", op, ops)
		}
		if strings.HasPrefix(op, "chown:") && strings.HasSuffix(op, ":"+l.WWWDir()) {
			t.Fatalf("deploy requested redundant www chown %q; state chown must own the tree", op)
		}
	}
}

func TestDeploySkipsServedTreePermsWhenWWWDirAbsent(t *testing.T) {
	t.Setenv("IKIGENBA_DOMAIN", "example.test")
	root := t.TempDir()
	sysRoot := t.TempDir()
	app := "dashboard"
	version := "v1.0.0"
	l := NewLayoutSys(root, sysRoot, app)
	sys := &stubSystem{}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, "APP="+app+"\nDEFAULT=true\nPORT=4310\n"))
	o.SysRoot = sysRoot

	manifest := "APP=dashboard\nDEFAULT=true\nPORT=4310\n"
	nginxSrc := "server_name __DOMAIN__; proxy_pass http://127.0.0.1:4310;\n"
	stageReleaseWithConfig(t, o, l, app, version, manifest, nginxSrc)

	if err := o.Deploy(context.Background(), app, version); err != nil {
		t.Fatalf("deploy default app: %v", err)
	}

	if _, err := os.Stat(l.WWWDir()); !os.IsNotExist(err) {
		t.Fatalf("default deploy has www dir stat err = %v, want absent", err)
	}
	for _, op := range sys.opSeq() {
		if strings.HasPrefix(op, "chown:") && strings.Contains(op, ":web:") {
			t.Fatalf("default deploy requested served-tree chown %q; ops = %v", op, sys.opSeq())
		}
		if strings.HasPrefix(op, "chmod:") && strings.Contains(op, l.WWWDir()) {
			t.Fatalf("default deploy requested served-tree chmod %q; ops = %v", op, sys.opSeq())
		}
	}
}

// TestInstall_IsActiveFailure asserts a failed is-active surfaces an error that
// points the operator at rollback (the release dir + backup are left intact).
func TestInstall_IsActiveFailure(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	sys := &stubSystem{failIsActive: true}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	err := stageAndDeploy(t, o, app, "v1.0.0", stageArtifact(t, "v1"))
	if err == nil || !strings.Contains(err.Error(), "did not come up") {
		t.Fatalf("is-active failure err = %v, want a 'did not come up' error", err)
	}
}

// R-I80H-SAQ3
func TestNotifyDeployUsesHermeticSystemSeamWithStateAndCachePaths(t *testing.T) {
	root := t.TempDir()
	const app = "notify"
	const version = "v1.2.3"
	l := NewLayout(root, app)
	isActiveCalled := false
	sys := &stubSystem{onIsActive: func() { isActiveCalled = true }}
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, ""))

	if err := stageAndDeploy(t, o, app, version, stageArtifact(t, app+"-"+version)); err != nil {
		t.Fatalf("deploy notify through fake system seam: %v", err)
	}
	if _, err := os.Stat(l.DBPath()); err != nil {
		t.Fatalf("notify DB was not created under state/: %v", err)
	}
	if got := filepath.Dir(l.GenerationPath()); got != l.CacheDir() {
		t.Fatalf("generation sidecar parent = %q, want cache dir %q", got, l.CacheDir())
	}
	if _, err := os.Stat(l.LibexecBinary(version)); err != nil {
		t.Fatalf("notify binary missing under libexec/: %v", err)
	}
	target, err := os.Readlink(l.RunLink())
	if err != nil {
		t.Fatalf("bin/run is not a symlink: %v", err)
	}
	if want := l.runTarget(version); target != want {
		t.Fatalf("bin/run -> %q, want %q", target, want)
	}
	manifest, err := os.ReadFile(l.ActiveManifest())
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifest), "APP=notify") {
		t.Fatalf("manifest missing APP=notify\n--- manifest ---\n%s", manifest)
	}
	if !isActiveCalled {
		t.Fatal("deploy did not verify service activity through the fake System seam")
	}
}
