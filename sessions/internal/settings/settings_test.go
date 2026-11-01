package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/settings"
)

// R-2EK7-CJXU R-2FS3-QBOJ R-2H00-43F8 R-2N3I-0Y4P
func TestDefaultsAndSettingsShape(t *testing.T) {
	check := func(defaults func() settings.Settings, read func(func(string) (string, bool)) (settings.Settings, error)) {
		for range 3 {
			var shape struct{ DrainSeconds int64 } = defaults()
			if shape.DrainSeconds != 5 {
				t.Fatalf("default drain = %d", shape.DrainSeconds)
			}
			s, err := read(func(string) (string, bool) { return "", false })
			if err != nil || s != defaults() {
				t.Fatalf("unset = %+v, %v", s, err)
			}
		}
	}
	check(settings.Defaults, settings.Read)
}

// R-2OBE-EPVE R-2PJA-SHM3 R-2QR7-69CS
func TestReadDrain(t *testing.T) {
	tests := []struct {
		value string
		set   bool
		want  int64
		bad   bool
	}{
		{"ignored", false, 5, false}, {"", true, 5, false},
		{"1", true, 1, false}, {"30", true, 30, false},
		{"9223372036854775807", true, math.MaxInt64, false},
		{"9223372036854775808", true, math.MaxInt64, false},
		{"99999999999999999999", true, math.MaxInt64, false},
		{strings.Repeat("9", 10000), true, math.MaxInt64, false},
	}
	for _, v := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc", "5\n", "１", "١", "9\x00", "9_0"} {
		tests = append(tests, struct {
			value string
			set   bool
			want  int64
			bad   bool
		}{v, true, 0, true})
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			var keys []string
			s, err := settings.Read(func(k string) (string, bool) {
				keys = append(keys, k)
				if k == "DRAIN_SECONDS" {
					return tc.value, tc.set
				}
				return "unrelated invalid setting", true
			})
			if len(keys) != 1 || keys[0] != "DRAIN_SECONDS" {
				t.Fatalf("lookup keys = %v", keys)
			}
			if !tc.bad {
				if err != nil || s.DrainSeconds != tc.want {
					t.Fatalf("Read = %+v, %v; want %d", s, err, tc.want)
				}
				return
			}
			var e *settings.Error
			if !errors.As(err, &e) {
				t.Fatalf("error = %v, want *settings.Error", err)
			}
			if e.Name != "DRAIN_SECONDS" || e.Value != tc.value || e.Want != settings.WantSeconds {
				t.Fatalf("error fields = %+v", e)
			}
		})
	}
}

// R-2I7W-HV5X R-2LVL-N6E0 R-2RZ3-K13H
func TestErrorAndWant(t *testing.T) {
	const want string = settings.WantSeconds
	if want == "" || strings.ContainsAny(want, "\r\n") {
		t.Fatalf("invalid WantSeconds %q", want)
	}
	e := &settings.Error{Name: "a setting", Value: "raw\n' value", Want: "a form"}
	var shape struct{ Name, Value, Want string } = *e
	render := func(method func(*settings.Error) string) string { return method(e) }
	if got := render((*settings.Error).Error); got != shape.Name+" is '"+shape.Value+"', not "+shape.Want {
		t.Fatalf("Error = %q", got)
	}
	_, err := settings.Read(func(string) (string, bool) { return "05", true })
	if err.Error() != "DRAIN_SECONDS is '05', not "+settings.WantSeconds {
		t.Fatalf("Read error = %q", err.Error())
	}
}

// R-2JFS-VMWM R-2T6Z-XSU6
func TestDrain(t *testing.T) {
	check := func(drain func(settings.Settings) time.Duration) {
		limit := int64(math.MaxInt64) / int64(time.Second)
		for _, tc := range []struct {
			seconds int64
			want    time.Duration
		}{
			{0, 0}, {1, time.Second}, {5, 5 * time.Second}, {30, 30 * time.Second},
			{limit, time.Duration(limit) * time.Second},
			{limit + 1, time.Duration(math.MaxInt64)}, {math.MaxInt64, time.Duration(math.MaxInt64)},
		} {
			if got := drain(settings.Settings{DrainSeconds: tc.seconds}); got != tc.want {
				t.Errorf("Drain(%d) = %v; want %v", tc.seconds, got, tc.want)
			}
		}
	}
	check(settings.Settings.Drain)
}
