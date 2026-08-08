package opsctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStage_SameArtifactBytesNoOp asserts re-staging the same version with the
// same artifact bytes is
// an idempotent no-op: the already-placed release binary is NOT re-copied, and the
// /tmp artifact is still deleted (decision 2 — the release is confirmed in place).
func TestStage_SameArtifactBytesNoOp(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	l := NewLayout(root, app)
	sys := &stubSystem{}

	art1 := stageBundleArtifact(t, app, "v1.0.0", "ledger-v1.0.0-a")
	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := o.Stage(context.Background(), app, "v1.0.0", art1, false); err != nil {
		t.Fatalf("first stage: %v", err)
	}
	if _, err := os.Stat(art1); !os.IsNotExist(err) {
		t.Fatalf("first stage did not delete the /tmp artifact (err=%v)", err)
	}
	relBin := l.LibexecBinary("v1.0.0")
	info1, err := os.Stat(relBin)
	if err != nil {
		t.Fatalf("release binary missing after stage: %v", err)
	}

	// Re-stage the same version with identical bytes → idempotent no-op (no re-copy),
	// /tmp still deleted.
	art2 := stageBundleArtifact(t, app, "v1.0.0", "ledger-v1.0.0-b")
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := o2.Stage(context.Background(), app, "v1.0.0", art2, false); err != nil {
		t.Fatalf("idempotent re-stage: %v", err)
	}
	if _, err := os.Stat(art2); !os.IsNotExist(err) {
		t.Fatalf("idempotent re-stage did not delete the /tmp artifact (err=%v)", err)
	}
	info2, err := os.Stat(relBin)
	if err != nil {
		t.Fatalf("release binary missing after re-stage: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("identical-byte re-stage re-copied the release binary (mtime %v -> %v)", info1.ModTime(), info2.ModTime())
	}
}

// TestStage_DifferentArtifactBytesRefuses asserts a differing-byte collision is refused
// without --force, and the /tmp artifact is KEPT so the operator can retry.
func TestStage_DifferentArtifactBytesRefuses(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := o1.Stage(context.Background(), app, "v1.0.0", stageBundleArtifact(t, app, "v1.0.0", "ledger-a"), false); err != nil {
		t.Fatalf("first stage: %v", err)
	}

	// A second build of the same version with different bytes must be refused.
	art := stageBundleArtifactWithBinarySuffix(t, app, "v1.0.0", "ledger-b", "different-build")
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	err := o2.Stage(context.Background(), app, "v1.0.0", art, false)
	if err == nil || !strings.Contains(err.Error(), "different artifact bytes") {
		t.Fatalf("different-byte stage err = %v, want a collision refusal", err)
	}
	// Refusal keeps the /tmp artifact (decision 2).
	if _, err := os.Stat(art); err != nil {
		t.Errorf("collision refusal removed the /tmp artifact, want it kept: %v", err)
	}
}

// TestStage_ForceOverride asserts --force replaces an already-staged release
// whose artifact bytes differ.
func TestStage_ForceOverride(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	sys := &stubSystem{}

	o1 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := o1.Stage(context.Background(), app, "v1.0.0", stageBundleArtifact(t, app, "v1.0.0", "ledger-a"), false); err != nil {
		t.Fatalf("first stage: %v", err)
	}

	art := stageBundleArtifactWithBinarySuffix(t, app, "v1.0.0", "ledger-b", "different-build")
	o2 := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	if err := o2.Stage(context.Background(), app, "v1.0.0", art, true); err != nil {
		t.Fatalf("--force stage: %v", err)
	}
	// On success --force deletes the /tmp artifact like any other placement.
	if _, err := os.Stat(art); !os.IsNotExist(err) {
		t.Fatalf("--force stage did not delete the /tmp artifact (err=%v)", err)
	}
}

// TestDeploy_NotStagedGuard asserts deploy refuses early when the release was never
// staged, pointing the operator at stage — before any manifest/schema/migrate exec.
func TestDeploy_NotStagedGuard(t *testing.T) {
	root := t.TempDir()
	app := "ledger"
	sys := &stubSystem{}

	o := newOpsctl(t, root, app, sys, fakeEnv(app, "v1.0.0", 1, ""))
	err := o.Deploy(context.Background(), app, "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "not staged") {
		t.Fatalf("unstaged deploy err = %v, want a 'not staged' refusal", err)
	}
	if sys.restarts != 0 {
		t.Errorf("unstaged deploy still restarted the unit (%d times)", sys.restarts)
	}
}

func TestStage_RefusesInvalidVersionAndDifferentRestageThenUnpacksBundleTiers(t *testing.T) {
	// R-84VR-7U2K
	root := t.TempDir()
	app := "ledger"
	version := "v1.0.0"
	l := NewLayout(root, app)
	sys := &stubSystem{}
	ctx := context.Background()

	badVersionArtifact := stageBundleArtifact(t, app, version, "ledger-invalid-version")
	o := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, ""))
	if err := o.Stage(ctx, app, "v1.2", badVersionArtifact, false); err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("invalid-version stage err = %v, want invalid version refusal", err)
	}
	if _, err := os.Stat(l.AppDir()); !os.IsNotExist(err) {
		t.Fatalf("invalid version unpacked app tree (stat err=%v)", err)
	}
	if _, err := os.Stat(badVersionArtifact); err != nil {
		t.Fatalf("invalid-version refusal removed artifact: %v", err)
	}

	first := stageBundleArtifact(t, app, version, "ledger-first")
	firstOps := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, ""))
	if err := firstOps.Stage(ctx, app, version, first, false); err != nil {
		t.Fatalf("first stage: %v", err)
	}
	assertBundleTiers(t, l, version)
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("successful stage did not delete artifact (err=%v)", err)
	}

	refused := stageBundleArtifactWithBinarySuffix(t, app, version, "ledger-different", "different-build")
	refusedOps := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, ""))
	err := refusedOps.Stage(ctx, app, version, refused, false)
	if err == nil || !strings.Contains(err.Error(), "different artifact bytes") {
		t.Fatalf("different re-stage err = %v, want byte collision refusal", err)
	}
	if _, err := os.Stat(refused); err != nil {
		t.Fatalf("collision refusal removed artifact: %v", err)
	}

	forced := stageBundleArtifactWithBinarySuffix(t, app, version, "ledger-forced", "forced-build")
	forcedOps := newOpsctl(t, root, app, sys, fakeEnv(app, version, 1, ""))
	if err := forcedOps.Stage(ctx, app, version, forced, true); err != nil {
		t.Fatalf("forced re-stage: %v", err)
	}
	assertBundleTiers(t, l, version)
	if _, err := os.Stat(forced); !os.IsNotExist(err) {
		t.Fatalf("forced stage did not delete artifact (err=%v)", err)
	}
}

func TestStage_UnpacksVersionedBundlePaths(t *testing.T) {
	// R-1BF5-X7QS
	root := t.TempDir()
	app := "ledger"
	version := "v2.3.4"
	l := NewLayout(root, app)
	artifact := stageBundleArtifact(t, app, version, "ledger-v2.3.4")
	o := newOpsctl(t, root, app, &stubSystem{}, fakeEnv(app, version, 1, ""))

	if err := o.Stage(context.Background(), app, version, artifact, false); err != nil {
		t.Fatalf("stage bundle: %v", err)
	}
	assertBundleTiers(t, l, version)
}

func assertBundleTiers(t *testing.T, l Layout, version string) {
	t.Helper()
	for _, path := range []string{
		l.LibexecBinary(version),
		l.NginxConfFile(version),
		l.ManifestFile(version),
		filepath.Join(l.ShareVersionDir(version), "assets", "resource.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("staged tier path %s missing: %v", path, err)
		}
	}
	body, err := os.ReadFile(l.ManifestFile(version))
	if err != nil {
		t.Fatalf("read staged manifest: %v", err)
	}
	if !strings.Contains(string(body), "APP="+l.App) {
		t.Fatalf("staged manifest does not name app: %q", string(body))
	}
}
