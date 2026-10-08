package appref_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
)

func TestValidName(t *testing.T) {
	t.Parallel()

	// R-CVWR-UA16 R-S6EM-KPG8
	valid := []string{"a", "0", "crm", "crm-api", "a--9", "snapshot", "seeds", "seed-1", "golden", strings.Repeat("a", 63)}
	for _, name := range valid {
		if !appref.ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"", "-crm", "crm-", "CRM", "crm_api", "cr.m", "café", strings.Repeat("a", 64),
		"host", "deploy", "snapshots", "seed", "backup-host", "backup-services", "renew-certificate",
	}
	for _, name := range invalid {
		if appref.ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
}

func TestValidVersion(t *testing.T) {
	t.Parallel()

	// R-CX4O-81RV R-CS92-OYT3
	valid := []string{
		"v0.0.0", "v1.2.3", "v10.200.3000", "v1.2.3-alpha", "v1.2.3-alpha.1",
		"v1.2.3-0A-9", "v1.2.3+001", "v1.2.3-alpha+build.001-X",
	}
	for _, version := range valid {
		if !appref.ValidVersion(version) {
			t.Errorf("ValidVersion(%q) = false, want true", version)
		}
	}
	invalid := []string{
		"", "1.2.3", "V1.2.3", "v1", "v1.2", "v1.2.3.4", "v01.2.3", "v1.02.3", "v1.2.03",
		"v1.2.-3", "v1.2.3-", "v1.2.3-alpha..1", "v1.2.3-01", "v1.2.3+", "v1.2.3+build..1",
		"v1.2.3+a+b", "v1.2.3-β", "v1.2.3+meta_1", "v1.2.3-alpha+meta-extra+again",
	}
	for _, version := range invalid {
		if appref.ValidVersion(version) {
			t.Errorf("ValidVersion(%q) = true, want false", version)
		}
	}
}

func TestParseFile(t *testing.T) {
	t.Parallel()

	// R-5D0P-CZ58 R-5E8L-QQVX
	const sha = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"
	for _, name := range []string{"crm", "my-app", "0", strings.Repeat("a", 63)} {
		file := name + "-" + sha + ".tar.xz"
		app, gotSHA, err := appref.ParseFile(file)
		if err != nil || app != name || gotSHA != sha {
			t.Errorf("ParseFile(%q) = (%q, %q, %v), want (%q, %q, nil)", file, app, gotSHA, err, name, sha)
		}
	}

	invalid := []string{
		"notes.tar.xz", "crm-latest.tar.xz", "crm-v0.1.0.tar.xz", "crm-4b22285.tar.xz",
		"crm-" + strings.ToUpper(sha) + ".tar.xz", "crm-" + sha + "0.tar.xz",
		"crm-" + sha[:39] + "g.tar.xz", "-" + sha + ".tar.xz", "host-" + sha + ".tar.xz",
		"crm-" + sha + ".tar.gz", "dist/crm-" + sha + ".tar.xz", `dist\crm-` + sha + ".tar.xz",
		"CRM-" + sha + ".tar.xz", "crm_-" + sha + ".tar.xz", "-crm-" + sha + ".tar.xz",
		strings.Repeat("a", 64) + "-" + sha + ".tar.xz", "crm-" + sha[:39] + ".tar.xz",
		"crm-" + sha + ".tar.xz/extra", "crm" + sha + ".tar.xz", "",
	}
	for _, file := range invalid {
		app, gotSHA, err := appref.ParseFile(file)
		if err == nil {
			t.Errorf("ParseFile(%q) = (%q, %q, nil), want error", file, app, gotSHA)
		}
	}
}
