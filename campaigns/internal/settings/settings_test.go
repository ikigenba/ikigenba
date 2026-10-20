package settings_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/campaigns/internal/settings"
)

// R-AO58-561L R-APD4-IXSA R-AQL0-WPIZ R-AVGM-FSHR R-AZ4B-L3PU
func TestDefaultsAndAcceptedSettings(t *testing.T) {
	want := settings.Settings{DrainSeconds: 5, DefaultDestination: "https://michaelgreenly.dev/", LinkPrefix: "https://michaelgreenly.dev/c/?t="}
	for range 2 {
		if settings.Defaults() != want {
			t.Fatal("defaults differ")
		}
	}
	if settings.WantSeconds == "" || settings.WantURL == "" || settings.WantSeconds == settings.WantURL || strings.ContainsAny(settings.WantSeconds+settings.WantURL, "\n") {
		t.Fatal("invalid explanation constants")
	}
	for _, empty := range []bool{false, true} {
		var keys []string
		got, err := settings.Read(func(k string) (string, bool) { keys = append(keys, k); return "", empty })
		if err != nil || got != want {
			t.Fatalf("defaults: %+v %v", got, err)
		}
		if !reflect.DeepEqual(keys, []string{"DRAIN_SECONDS", "CAMPAIGNS_DEFAULT_DESTINATION", "CAMPAIGNS_LINK_PREFIX"}) {
			t.Fatal(keys)
		}
	}
	got, err := settings.Read(func(k string) (string, bool) {
		return map[string]string{"DRAIN_SECONDS": "999999999999999999999999999999999999", "CAMPAIGNS_DEFAULT_DESTINATION": "HTTPS://Example.test", "CAMPAIGNS_LINK_PREFIX": "https://example.test/t/"}[k], true
	})
	if err != nil || got.DrainSeconds != math.MaxInt64 || got.DefaultDestination != "HTTPS://Example.test" || got.LinkPrefix != "https://example.test/t/" {
		t.Fatalf("values: %+v %v", got, err)
	}
	for _, n := range []int64{0, 1, 5, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		expected := time.Duration(math.MaxInt64)
		if n <= math.MaxInt64/int64(time.Second) {
			expected = time.Duration(n) * time.Second
		}
		if (settings.Settings{DrainSeconds: n}).Drain() != expected {
			t.Fatal(n)
		}
	}
}

// R-AT0T-O90D R-AWOI-TK8G R-AXWF-7BZ5 R-AVGM-FSHR
func TestSecondsAndErrorOrder(t *testing.T) {
	for _, v := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc", "١", "1\n"} {
		_, err := settings.Read(func(k string) (string, bool) {
			if k == "DRAIN_SECONDS" {
				return v, true
			}
			return "invalid", true
		})
		var e *settings.Error
		if !errors.As(err, &e) || e.Name != "DRAIN_SECONDS" || e.Value != v || e.Want != settings.WantSeconds || e.Error() != e.Name+" is '"+v+"', not "+settings.WantSeconds {
			t.Fatalf("%q: %v", v, err)
		}
	}
	for _, tc := range []struct {
		value   string
		seconds int64
	}{{"1", 1}, {"5", 5}, {"9223372036854775807", math.MaxInt64}, {"9223372036854775808", math.MaxInt64}} {
		got, err := settings.Read(func(k string) (string, bool) { return tc.value, k == "DRAIN_SECONDS" })
		want := settings.Defaults()
		want.DrainSeconds = tc.seconds
		if err != nil || got != want {
			t.Fatalf("%q: got %+v, want %+v: %v", tc.value, got, want, err)
		}
	}
}

// R-AU8Q-20R2 R-AWOI-TK8G R-AXWF-7BZ5
func TestAddresses(t *testing.T) {
	for _, key := range []string{"CAMPAIGNS_DEFAULT_DESTINATION", "CAMPAIGNS_LINK_PREFIX"} {
		for _, v := range []string{"https://michaelgreenly.dev/c/?t=", "https://example.test/t/", "http://localhost:8080/", "HTTPS://example.test"} {
			got, err := settings.Read(func(k string) (string, bool) { return v, k == key })
			if err != nil {
				t.Fatalf("%s %q: %v", key, v, err)
			}
			if key == "CAMPAIGNS_DEFAULT_DESTINATION" && got.DefaultDestination != v || key == "CAMPAIGNS_LINK_PREFIX" && got.LinkPrefix != v {
				t.Fatal(got)
			}
		}
		for _, v := range []string{"michaelgreenly.dev/", "/c/?t=", "ftp://example.test/", "mailto:x@example.test", "https://", "https:///path", "https://:80/", "https://example.test/ a", " https://example.test/", "https://example.test/\x7f", "https://example.test/é", "https://example.test/%zz"} {
			_, err := settings.Read(func(k string) (string, bool) {
				if k == key {
					return v, true
				}
				if k == "CAMPAIGNS_LINK_PREFIX" {
					return "bad", true
				}
				return "", false
			})
			var e *settings.Error
			if !errors.As(err, &e) || e.Name != key || e.Value != v || e.Want != settings.WantURL || e.Error() != key+" is '"+v+"', not "+settings.WantURL {
				t.Fatalf("%s %q: %v", key, v, err)
			}
		}
	}
}
