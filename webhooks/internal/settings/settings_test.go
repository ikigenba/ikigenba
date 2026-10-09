package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/webhooks/internal/settings"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

// R-WV63-1642 R-WWDZ-EXUR
func TestDefaults(t *testing.T) {
	s := settings.Defaults()
	if s != (settings.Settings{DrainSeconds: 5, RetentionDays: 2}) {
		t.Fatalf("defaults = %+v", s)
	}
	read := settings.Read
	if _, err := read(func(string) (string, bool) { return "", false }); err != nil {
		t.Fatal(err)
	}
	var e error = &settings.Error{Name: "N", Value: "V", Unit: "U"}
	if e.Error() == "" {
		t.Fatal("empty error")
	}
	drain, retention := s.Drain, s.Retention
	if drain() != 5*time.Second || retention() != 48*time.Hour {
		t.Fatal("default durations")
	}
}

// R-WXLV-SPLG R-X01O-K92U
func TestReadAccepted(t *testing.T) {
	cases := []struct {
		name, value string
		present     bool
		want        int64
		def         bool
	}{
		{"unset", "ignored", false, 0, true},
		{"empty", "", true, 0, true},
		{"one", "1", true, 1, false},
		{"sixty", "60", true, 60, false},
		{"largest", "9223372036854775807", true, math.MaxInt64, false},
		{"overflow", "9223372036854775808", true, math.MaxInt64, false},
		{"many digits", strings.Repeat("9", 1000), true, math.MaxInt64, false},
	}
	for _, variable := range []string{"DRAIN_SECONDS", "WEBHOOKS_RETENTION_DAYS"} {
		for _, tc := range cases {
			t.Run(variable+"/"+tc.name, func(t *testing.T) {
				values := map[string]string{"OTHER": "abc"}
				if tc.present {
					values[variable] = tc.value
				}
				got, err := settings.Read(lookup(values))
				if err != nil {
					t.Fatal(err)
				}
				want := settings.Defaults()
				if !tc.def {
					if variable == "DRAIN_SECONDS" {
						want.DrainSeconds = tc.want
					} else {
						want.RetentionDays = tc.want
					}
				}
				if got != want {
					t.Fatalf("Read = %+v, want %+v", got, want)
				}
			})
		}
	}
	got, err := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": "7", "WEBHOOKS_RETENTION_DAYS": "30"}))
	if err != nil || got != (settings.Settings{DrainSeconds: 7, RetentionDays: 30}) {
		t.Fatalf("Read = %+v, %v", got, err)
	}
}

// R-WXLV-SPLG R-X19K-Y0TJ R-X2HH-BSK8
func TestReadRefused(t *testing.T) {
	units := map[string]string{"DRAIN_SECONDS": "seconds", "WEBHOOKS_RETENTION_DAYS": "days"}
	for _, variable := range []string{"DRAIN_SECONDS", "WEBHOOKS_RETENTION_DAYS"} {
		for _, value := range []string{"0", "-1", "+5", "2.5", "5s", "05", " 5", "5 ", "abc", "\t5", "٥"} {
			t.Run(variable+"/"+value, func(t *testing.T) {
				_, err := settings.Read(lookup(map[string]string{variable: value}))
				var problem *settings.Error
				if !errors.As(err, &problem) {
					t.Fatalf("error = %v", err)
				}
				if problem.Name != variable || problem.Value != value || problem.Unit != units[variable] {
					t.Fatalf("fields = %+v", problem)
				}
				want := variable + " is '" + value + "', not a positive whole number of " + units[variable]
				if problem.Error() != want {
					t.Fatalf("Error = %q, want %q", problem.Error(), want)
				}
			})
		}
	}
	_, err := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": "x", "WEBHOOKS_RETENTION_DAYS": "y"}))
	var problem *settings.Error
	if !errors.As(err, &problem) || problem.Name != "DRAIN_SECONDS" {
		t.Fatalf("both refused: %v", err)
	}
}

// R-X2HH-BSK8
func TestErrorUsesAllFields(t *testing.T) {
	var err error = &settings.Error{Name: "COUNT", Value: "raw\nvalue'", Unit: "items"}
	if got, want := err.Error(), "COUNT is 'raw\nvalue'', not a positive whole number of items"; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

// R-X3PD-PKAX
func TestDurations(t *testing.T) {
	s := settings.Settings{DrainSeconds: 7, RetentionDays: 3}
	if s.Drain() != 7*time.Second || s.Retention() != 72*time.Hour {
		t.Fatalf("durations = %v, %v", s.Drain(), s.Retention())
	}
	largest := time.Duration(math.MaxInt64)
	big := settings.Settings{DrainSeconds: math.MaxInt64, RetentionDays: math.MaxInt64}
	if big.Drain() != largest || big.Retention() != largest {
		t.Fatalf("saturation = %v, %v", big.Drain(), big.Retention())
	}
	edge := settings.Settings{RetentionDays: int64(largest / (24 * time.Hour))}
	if edge.Retention() != time.Duration(edge.RetentionDays)*24*time.Hour {
		t.Fatalf("edge = %v", edge.Retention())
	}
	over := settings.Settings{RetentionDays: edge.RetentionDays + 1}
	if over.Retention() != largest {
		t.Fatalf("over = %v", over.Retention())
	}
}
