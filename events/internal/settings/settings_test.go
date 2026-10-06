package settings_test

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/events/internal/settings"
)

var names = []string{"DRAIN_SECONDS", "EVENTS_DEPTH_MAX", "EVENTS_DELIVERY_TIMEOUT_SECONDS", "EVENTS_DELIVERY_ATTEMPTS", "EVENTS_INFLIGHT_MAX", "EVENTS_RETENTION_DAYS", "EVENTS_DECLARATIONS_SECONDS"}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}

func fields(s settings.Settings) []int64 {
	return []int64{s.DrainSeconds, s.DepthMax, s.DeliveryTimeoutSeconds, s.DeliveryAttempts, s.InflightMax, s.RetentionDays, s.DeclarationsSeconds}
}

func TestPublicContractAndDefaults(t *testing.T) {
	// R-152N-8W4K R-16AJ-MNV9 R-18QC-E7CN
	var diagnostic error = &settings.Error{Name: "name", Value: "value", Unit: "unit"}
	if diagnostic.Error() == "" {
		t.Fatal("empty diagnostic")
	}
	want := settings.Settings{DrainSeconds: 5, DepthMax: 8, DeliveryTimeoutSeconds: 5, DeliveryAttempts: 10, InflightMax: 4, RetentionDays: 2, DeclarationsSeconds: 60}
	if got := settings.Defaults(); got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
	values := map[string]string{}
	for i, name := range names {
		values[name] = strconv.Itoa(i + 11)
	}
	got, err := settings.Read(lookup(values))
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range fields(got) {
		if value != int64(i+11) {
			t.Errorf("%s field = %d", names[i], value)
		}
	}
}

func TestAcceptedValues(t *testing.T) {
	// R-19Y8-RZ3C R-1B65-5QU1
	for _, name := range names {
		for _, input := range []string{"", "1", "9", "10", "987654321", strconv.FormatInt(math.MaxInt64, 10), "9223372036854775808", strings.Repeat("9", 200)} {
			t.Run(name+"/"+input, func(t *testing.T) {
				got, err := settings.Read(lookup(map[string]string{name: input}))
				if err != nil {
					t.Fatal(err)
				}
				want := fields(settings.Defaults())
				for i, variable := range names {
					if variable == name && input != "" {
						n, parseErr := strconv.ParseInt(input, 10, 64)
						if parseErr != nil {
							n = math.MaxInt64
						}
						want[i] = n
					}
				}
				for i, value := range fields(got) {
					if value != want[i] {
						t.Errorf("%s = %d, want %d", names[i], value, want[i])
					}
				}
			})
		}
	}
	got, err := settings.Read(func(string) (string, bool) { return "abc", false })
	if err != nil || got != settings.Defaults() {
		t.Fatalf("unset = %+v, %v", got, err)
	}
}

func TestRejectedValuesAndOrder(t *testing.T) {
	// R-19Y8-RZ3C R-1CE1-JIKQ
	units := []string{"seconds", "levels", "seconds", "attempts", "deliveries", "days", "seconds"}
	for i, name := range names {
		for _, input := range []string{"0", "-1", "+5", "2.5", "5s", "8x", "10x", "4x", "2d", "60s", "1e1", "05", " 5", "5 ", "abc", "１２", "1\n", "1'bad"} {
			values := map[string]string{name: input}
			for _, later := range names[i+1:] {
				values[later] = "0"
			}
			_, err := settings.Read(lookup(values))
			var diagnostic *settings.Error
			if !errors.As(err, &diagnostic) {
				t.Fatalf("%s=%q: error = %v", name, input, err)
			}
			if diagnostic.Name != name || diagnostic.Value != input || diagnostic.Unit != units[i] {
				t.Fatalf("%s=%q: error = %+v", name, input, diagnostic)
			}
		}
	}
}

func TestErrorText(t *testing.T) {
	// R-1DLX-XABF
	for _, diagnostic := range []*settings.Error{{Name: "custom", Value: "a'\nb", Unit: "units"}, {Name: "EVENTS_INFLIGHT_MAX", Value: "abc", Unit: "deliveries"}} {
		want := diagnostic.Name + " is '" + diagnostic.Value + "', not a positive whole number of " + diagnostic.Unit
		if got := diagnostic.Error(); got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
	}
	_, err := settings.Read(lookup(map[string]string{"EVENTS_INFLIGHT_MAX": "abc"}))
	if err == nil || err.Error() != "EVENTS_INFLIGHT_MAX is 'abc', not a positive whole number of deliveries" {
		t.Fatalf("read diagnostic = %v", err)
	}
}

func TestLookupKeys(t *testing.T) {
	// R-1ETU-B224
	seen := map[string]bool{}
	_, err := settings.Read(func(key string) (string, bool) {
		seen[key] = true
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		delete(seen, name)
	}
	if len(seen) != 0 {
		t.Fatalf("unexpected lookup keys: %v", seen)
	}
}

func TestDurationMethods(t *testing.T) {
	// R-17IG-0FLY R-1G1Q-OTST R-1H9N-2LJI R-1IHJ-GDA7 R-1JPF-U50W
	for _, unit := range []time.Duration{time.Second, 24 * time.Hour} {
		boundary := math.MaxInt64 / int64(unit)
		for _, value := range []int64{1, 2, 60, boundary - 1, boundary, boundary + 1, math.MaxInt64} {
			s := settings.Settings{DrainSeconds: value, DeliveryTimeoutSeconds: value, RetentionDays: value, DeclarationsSeconds: value}
			methods := []func() time.Duration{s.Drain, s.DeliveryTimeout, s.DeclarationsInterval}
			if unit == 24*time.Hour {
				methods = []func() time.Duration{s.Retention}
			}
			want := time.Duration(math.MaxInt64)
			if value <= boundary {
				want = time.Duration(value) * unit
			}
			for _, method := range methods {
				if got := method(); got != want {
					t.Fatalf("value %d, unit %s = %s, want %s", value, unit, got, want)
				}
			}
		}
	}
}
