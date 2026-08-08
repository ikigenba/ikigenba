package opsctl

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStagePreflightReadsAuthoredAppWithoutManifestVerb(t *testing.T) {
	// R-TA75-P0NF
	const (
		app     = "ledger"
		version = "v1.2.3"
	)
	ctx := context.Background()
	root := t.TempDir()
	l := NewLayout(root, app)
	bin := stageArtifact(t, "ledger-bin")

	var matchEvents []string
	matching := bundleArtifactFromBinaryManifest(t, app, version, "matching", bin, "APP="+app+"\nPORT=3999\n")
	o := newOpsctl(t, root, app, &stubSystem{}, fakeEnv("would-answer-if-called", version, 1, "APP=wrong\n"))
	o.Runner = fakeRunner{baseEnv: fakeEnv("would-answer-if-called", version, 1, "APP=wrong\n"), events: &matchEvents}
	if err := o.Stage(ctx, app, version, matching, false); err != nil {
		t.Fatalf("stage matching authored APP: %v", err)
	}
	if got := strings.Join(matchEvents, ","); got != "run:version" {
		t.Fatalf("matching preflight runner calls = %q, want only version", got)
	}

	root = t.TempDir()
	l = NewLayout(root, app)
	var mismatchEvents []string
	mismatching := bundleArtifactFromBinaryManifest(t, app, version, "mismatching", bin, "APP=crm\n")
	o = newOpsctl(t, root, app, &stubSystem{}, fakeEnv("would-answer-if-called", version, 1, "APP="+app+"\n"))
	o.Runner = fakeRunner{baseEnv: fakeEnv("would-answer-if-called", version, 1, "APP="+app+"\n"), events: &mismatchEvents}
	err := o.Stage(ctx, app, version, mismatching, false)
	if err == nil || !strings.Contains(err.Error(), "APP=\"crm\"") {
		t.Fatalf("stage mismatching authored APP err = %v, want APP mismatch refusal", err)
	}
	if got := strings.Join(mismatchEvents, ","); got != "run:version" {
		t.Fatalf("mismatching preflight runner calls = %q, want only version", got)
	}
	if _, statErr := os.Stat(l.AppDir()); !os.IsNotExist(statErr) {
		t.Fatalf("APP mismatch placed app tree (stat err=%v)", statErr)
	}
}
