package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/settings"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
}

// R-Y2K7-WCY8 R-Y3S4-A4OX R-Y500-NWFM
func TestSettingsDeclarations(t *testing.T) {
	declared := struct {
		Defaults func() settings.Settings
		Read     func(func(string) (string, bool)) (settings.Settings, error)
	}{Defaults: settings.Defaults, Read: settings.Read}
	defaults, read := declared.Defaults, declared.Read
	want := settings.Settings{DrainSeconds: 5, SiteMaxBytes: 268435456, OperationSeconds: 600, ReposDir: "../repos/state/repos"}
	if defaults() != want {
		t.Fatalf("defaults: %+v", defaults())
	}
	got, err := read(lookup(nil))
	if err != nil || got != want {
		t.Fatalf("read: %+v %v", got, err)
	}
	var e error = &settings.Error{Name: "a", Value: "b", Unit: "c"}
	if e.Error() == "" {
		t.Fatal("empty error")
	}
}

// R-Y67X-1O6B R-Y8NP-T7NP
func TestNumericValues(t *testing.T) {
	fields := []string{"DRAIN_SECONDS", "SITE_MAX_BYTES", "OPERATION_SECONDS"}
	for _, name := range fields {
		for _, value := range []string{"", "1", "42", "9223372036854775807", "9223372036854775808", strings.Repeat("9", 1000)} {
			got, err := settings.Read(lookup(map[string]string{name: value}))
			if err != nil {
				t.Fatalf("%s=%q: %v", name, value, err)
			}
			want := settings.Defaults()
			number := int64(42)
			switch value {
			case "":
				number = map[string]int64{"DRAIN_SECONDS": want.DrainSeconds, "SITE_MAX_BYTES": want.SiteMaxBytes, "OPERATION_SECONDS": want.OperationSeconds}[name]
			case "1":
				number = 1
			case "42":
			default:
				number = math.MaxInt64
			}
			switch name {
			case "DRAIN_SECONDS":
				want.DrainSeconds = number
			case "SITE_MAX_BYTES":
				want.SiteMaxBytes = number
			case "OPERATION_SECONDS":
				want.OperationSeconds = number
			}
			if got != want {
				t.Fatalf("got %+v want %+v", got, want)
			}
		}
		for _, value := range []string{"0", "-1", "+5", "2.5", "5s", "256M", "10m", "05", " 5", "5 ", "abc", "١", "1\n", "1\x00"} {
			if _, err := settings.Read(lookup(map[string]string{name: value})); err == nil {
				t.Fatalf("accepted %s=%q", name, value)
			}
		}
	}
	// An unset variable ignores the value returned alongside false.
	got, err := settings.Read(func(string) (string, bool) { return "invalid", false })
	if err != nil || got != settings.Defaults() {
		t.Fatalf("unset: %+v %v", got, err)
	}
}

// R-Y7FT-FFX0 R-Y8NP-T7NP
func TestRepositoryDirectory(t *testing.T) {
	for _, v := range []string{"", "/absolute", "../relative", " ", "\x00", "0", "日本語"} {
		got, err := settings.Read(lookup(map[string]string{"REPOS_DIR": v}))
		if err != nil {
			t.Fatal(err)
		}
		want := settings.Defaults()
		if v != "" {
			want.ReposDir = v
		}
		if got != want {
			t.Fatalf("%q: %+v", v, got)
		}
	}
}

// R-YB3I-KR53 R-YCBE-YIVS
func TestFirstError(t *testing.T) {
	names := []string{"DRAIN_SECONDS", "SITE_MAX_BYTES", "OPERATION_SECONDS"}
	for i, name := range names {
		values := map[string]string{}
		for _, later := range names[i:] {
			values[later] = "bad ' value"
		}
		_, err := settings.Read(lookup(values))
		var settingError *settings.Error
		if !errors.As(err, &settingError) {
			t.Fatalf("error: %v", err)
		}
		unit := "seconds"
		if name == "SITE_MAX_BYTES" {
			unit = "bytes"
		}
		want := settings.Error{Name: name, Value: "bad ' value", Unit: unit}
		if *settingError != want || err.Error() != name+" is 'bad ' value', not a positive whole number of "+unit {
			t.Fatalf("error: %+v %v", settingError, err)
		}
	}
}
