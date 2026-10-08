package apps_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-93Z5-D770
func TestIconAndServicesConstants(t *testing.T) {
	const icon, path, environment = apps.IconPath, apps.ServicesPath, apps.ServicesEnv
	if icon != "share/icon.svg" || path != "/run/ikigenba/services.json" || apps.PerAppServicesPath != "/var/lib/ikigenba/services.json" || environment != "IKIGENBA_SERVICES" {
		t.Fatalf("icon and services constants: %q, %q, %q", icon, path, environment)
	}
}

// R-8YZ3-XU4K
func TestIconSentinelErrors(t *testing.T) {
	if apps.ErrIconNotSVG.Error() != "share/icon.svg is not an SVG image" {
		t.Errorf("ErrIconNotSVG: %q", apps.ErrIconNotSVG)
	}
	if apps.ErrIconTooLarge.Error() != "share/icon.svg is larger than 64 KiB" {
		t.Errorf("ErrIconTooLarge: %q", apps.ErrIconTooLarge)
	}
}

// R-9070-BLV9
var _ func([]byte) error = apps.CheckIcon

// R-91EW-PDLY
func TestCheckIconSizeAndSentinelResults(t *testing.T) {
	maximum := append([]byte("<svg>"), bytes.Repeat([]byte{' '}, 65536-len("<svg></svg>"))...)
	maximum = append(maximum, []byte("</svg>")...)
	if err := apps.CheckIcon(maximum); err != nil {
		t.Fatalf("65536-byte SVG: %v", err)
	}
	if err := apps.CheckIcon(append(maximum, ' ')); !reflect.ValueOf(err).Equal(reflect.ValueOf(apps.ErrIconTooLarge)) {
		t.Errorf("65537-byte SVG: %v", err)
	}
	if err := apps.CheckIcon(bytes.Repeat([]byte{'x'}, 65537)); !reflect.ValueOf(err).Equal(reflect.ValueOf(apps.ErrIconTooLarge)) {
		t.Errorf("oversize non-SVG: %v", err)
	}
	if err := apps.CheckIcon([]byte("not SVG")); !reflect.ValueOf(err).Equal(reflect.ValueOf(apps.ErrIconNotSVG)) {
		t.Errorf("non-SVG: %v", err)
	}
}

// R-M1NV-I2WL
func TestCheckIconSVGDefinition(t *testing.T) {
	valid := []string{
		"<svg/>",
		"\xef\xbb\xbf<svg/>",
		" \t\r\n<?xml version=\"1.0\"?>\n<!-- before --><svg><g/></svg><!-- after -->\n",
		"<!DOCTYPE svg><a:svg xmlns:a=\"urn:example\"/>",
		"<svg xmlns:a=\"urn:a\" xmlns:b=\"urn:b\" a:key=\"1\" b:key=\"2\"/>",
	}
	for _, data := range valid {
		if err := apps.CheckIcon([]byte(data)); err != nil {
			t.Errorf("valid SVG %q: %v", data, err)
		}
	}
	invalid := []string{
		"", "plain text", "\x89PNG\r\n", "\xff<svg/>",
		"<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><svg/>",
		"<svg><g></svg>", "<svg>", "<svg>&undefined;</svg>",
		"<svg x=\"1\" x=\"2\"/>",
		"<svg xmlns:a=\"urn:one\" xmlns:b=\"urn:one\" a:x=\"1\" b:x=\"2\"/>",
		"<html/>", "<svg/><svg/>", "<svg/>after", "\xef\xbb\xbf\xef\xbb\xbf<svg/>",
		"\u00a0<svg/>",
	}
	for _, data := range invalid {
		if err := apps.CheckIcon([]byte(data)); !errors.Is(err, apps.ErrIconNotSVG) {
			t.Errorf("invalid SVG %q: %v", data, err)
		}
	}
}

// R-93UP-GX3C
func TestCheckIconDependsOnlyOnInput(t *testing.T) {
	inputs := [][]byte{
		nil,
		[]byte("<svg/>"),
		[]byte("<svg><g/></svg>"),
		[]byte("plain text"),
		[]byte("<svg>&undefined;</svg>"),
		bytes.Repeat([]byte{'x'}, 65537),
	}
	results := func() []error {
		out := make([]error, len(inputs))
		for i, data := range inputs {
			out[i] = apps.CheckIcon(data)
		}
		return out
	}

	first := t.TempDir()
	t.Chdir(first)
	t.Setenv("HOME", first)
	t.Setenv("PATH", first)
	t.Setenv("TMPDIR", first)
	want := results()

	second := t.TempDir()
	t.Chdir(second)
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("PATH", "")
	t.Setenv("TMPDIR", "/nonexistent")
	got := results()

	for i := range inputs {
		if !errors.Is(got[i], want[i]) {
			t.Errorf("CheckIcon(%q) = %v under a different environment and directory, want %v", inputs[i], got[i], want[i])
		}
	}
	for _, dir := range []string{first, second} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("CheckIcon created files in %s: %v", dir, entries)
		}
	}
}
