package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/cron/internal/settings"
)

// R-7VSS-IO0S R-C6VQ-K6VG R-7Y8L-A7I6
func TestDefaults(t *testing.T) {
	want := settings.Settings{DrainSeconds: 5}
	if got := settings.Defaults(); got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

// R-C83M-XYM5 R-7ZGH-NZ8V R-80OE-1QZK
func TestReadAccepted(t *testing.T) {
	cases := []struct {
		name, value string
		present     bool
		want        int64
	}{
		{"unset", "ignored", false, settings.Defaults().DrainSeconds},
		{"empty", "", true, settings.Defaults().DrainSeconds},
		{"one", "1", true, 1},
		{"five", "5", true, 5},
		{"sixty", "60", true, 60},
		{"largest", "9223372036854775807", true, math.MaxInt64},
		{"overflow", "9223372036854775808", true, math.MaxInt64},
		{"many digits", strings.Repeat("9", 1000), true, math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := settings.Read(func(key string) (string, bool) {
				calls++
				if key != "DRAIN_SECONDS" {
					t.Fatalf("unexpected key %q", key)
				}
				return tc.value, tc.present
			})
			if err != nil || got != (settings.Settings{DrainSeconds: tc.want}) {
				t.Fatalf("Read = %+v, %v", got, err)
			}
			if calls != 1 {
				t.Fatalf("lookup calls = %d", calls)
			}
		})
	}
}

// R-C9BJ-BQCU R-7ZGH-NZ8V R-81WA-FIQ9 R-8346-TAGY
func TestReadRefused(t *testing.T) {
	for _, value := range []string{"0", "-1", "+5", "2.5", "5s", "05", " 5", "5 ", "abc", "\t5", "5\n", "٥", "1２", "1\x00"} {
		t.Run(value, func(t *testing.T) {
			_, err := settings.Read(func(key string) (string, bool) {
				if key != "DRAIN_SECONDS" {
					t.Fatalf("unexpected key %q", key)
				}
				return value, true
			})
			var problem *settings.Error
			if !errors.As(err, &problem) {
				t.Fatalf("error = %v", err)
			}
			if problem.Name != "DRAIN_SECONDS" || problem.Value != value || problem.Unit != "seconds" {
				t.Fatalf("error fields = %+v", problem)
			}
			want := "DRAIN_SECONDS is '" + value + "', not a positive whole number of seconds"
			if problem.Error() != want {
				t.Fatalf("error = %q, want %q", problem.Error(), want)
			}
		})
	}
}

// R-C9BJ-BQCU R-8346-TAGY
func TestErrorUsesAllFields(t *testing.T) {
	var err error = &settings.Error{Name: "COUNT", Value: "raw\nvalue'", Unit: "items"}
	if got, want := err.Error(), "COUNT is 'raw\nvalue'', not a positive whole number of items"; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}
