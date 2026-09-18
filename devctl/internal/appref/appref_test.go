package appref_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
)

func TestValidName(t *testing.T) {
	t.Parallel()

	// R-CVWR-UA16 R-CR16-B72E R-YHTO-8IAI
	valid := []string{"a", "0", "crm", "crm-api", "a--9", strings.Repeat("a", 63)}
	for _, name := range valid {
		if !appref.ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"", "-crm", "crm-", "CRM", "crm_api", "cr.m", "café", strings.Repeat("a", 64),
		"host", "deploy", "backup-host", "backup-services", "renew-certificate",
	}
	for _, name := range invalid {
		if appref.ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
}

func TestValidVersion(t *testing.T) {
	t.Parallel()

	// R-CX4O-81RV R-CS92-OYT3 R-YHTO-8IAI
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

func TestVersionForTag(t *testing.T) {
	t.Parallel()

	// R-CZKG-ZL99 R-CTGZ-2QJS R-YHTO-8IAI
	for _, tc := range []struct {
		app     string
		tag     string
		version string
		ok      bool
	}{
		{app: "crm-api", tag: "crm-api/v1.2.3-alpha+build.7", version: "v1.2.3-alpha+build.7", ok: true},
		{app: "crm", tag: "other/v1.2.3"},
		{app: "crm", tag: "v1.2.3"},
		{app: "crm", tag: "crm/v1.2"},
		{app: "host", tag: "host/v1.2.3"},
		{app: "crm", tag: "crm/v1.2.3/extra"},
	} {
		version, ok := appref.VersionForTag(tc.app, tc.tag)
		if version != tc.version || ok != tc.ok {
			t.Errorf("VersionForTag(%q, %q) = (%q, %t), want (%q, %t)", tc.app, tc.tag, version, ok, tc.version, tc.ok)
		}
	}
}

func TestParseFile(t *testing.T) {
	t.Parallel()

	// R-D0SD-DCZY R-CUOV-GIAH R-YHTO-8IAI
	for _, tc := range []struct {
		name    string
		app     string
		version string
	}{
		{name: "crm-v1.2.3.tar.xz", app: "crm", version: "v1.2.3"},
		{name: "crm-api-v1.2.3-alpha-one.2+build.007.tar.xz", app: "crm-api", version: "v1.2.3-alpha-one.2+build.007"},
	} {
		app, version, err := appref.ParseFile(tc.name)
		if err != nil || app != tc.app || version != tc.version {
			t.Errorf("ParseFile(%q) = (%q, %q, %v), want (%q, %q, nil)", tc.name, app, version, err, tc.app, tc.version)
		}
	}

	invalid := []string{
		"crm-v1.2.3", "crm-1.2.3.tar.xz", "host-v1.2.3.tar.xz", "CRM-v1.2.3.tar.xz",
		"crm-v1.2.tar.xz", "crm/v1.2.3.tar.xz", `crm\v1.2.3.tar.xz`, "crm-v1.2.3.tar.xz/extra",
	}
	for _, name := range invalid {
		app, version, err := appref.ParseFile(name)
		if err == nil || app != "" || version != "" {
			t.Errorf("ParseFile(%q) = (%q, %q, %v), want empty components and error", name, app, version, err)
		}
	}
}
