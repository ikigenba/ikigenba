package settings_test

import (
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gmail/internal/settings"
)

var names = []string{"DRAIN_SECONDS", "TRANSFER_MAX_BYTES", "TRANSFER_MAX_ACTIVE", "TRANSFER_MAX_QUEUED", "TRANSFER_KEEP_DAYS", "TRANSFER_KEEP_COUNT", "DRAFT_KEEP_DAYS", "DRAFT_KEEP_COUNT"}
var units = []string{"seconds", "bytes", "transfers", "transfers", "days", "transfers", "days", "drafts"}

func values(s settings.Settings) []int64 {
	return []int64{s.DrainSeconds, s.TransferMaxBytes, s.TransferMaxActive, s.TransferMaxQueued, s.TransferKeepDays, s.TransferKeepCount, s.DraftKeepDays, s.DraftKeepCount}
}

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// R-WV92-2MAH R-WWGY-GE16
func TestDefaultsAndAPI(t *testing.T) {
	want := settings.Settings{DrainSeconds: 5, TransferMaxBytes: 52428800, TransferMaxActive: 2, TransferMaxQueued: 10, TransferKeepDays: 15, TransferKeepCount: 100, DraftKeepDays: 15, DraftKeepCount: 100}
	if got := settings.Defaults(); got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
	m := make(map[string]string)
	for i, name := range names {
		m[name] = strconv.Itoa(i + 1)
	}
	s, err := settings.Read(lookup(m))
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range values(s) {
		if value != int64(i+1) {
			t.Errorf("%s = %d", names[i], value)
		}
	}
	var _ error = &settings.Error{Name: "a", Value: "b", Unit: "c"}
}

// R-WXOU-U5RV R-X04N-LP99
func TestReadAcceptance(t *testing.T) {
	for i, name := range names {
		for _, invalid := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1G", "1e9", "15d", "05", " 5", "5 ", "abc", "١", "1\n", "1\x00"} {
			if _, err := settings.Read(lookup(map[string]string{name: invalid})); err == nil {
				t.Errorf("%s accepted %q", name, invalid)
			}
		}
		for _, test := range []struct {
			input string
			want  int64
		}{{"1", 1}, {"987654321", 987654321}, {"9223372036854775807", math.MaxInt64}, {"9223372036854775808", math.MaxInt64}, {"99999999999999999999999", math.MaxInt64}} {
			s, err := settings.Read(lookup(map[string]string{name: test.input, "OTHER": "invalid"}))
			if err != nil {
				t.Fatal(err)
			}
			want := values(settings.Defaults())
			want[i] = test.want
			for j, got := range values(s) {
				if got != want[j] {
					t.Errorf("%s input %q field %s = %d, want %d", name, test.input, names[j], got, want[j])
				}
			}
		}
	}
	for _, f := range []func(string) (string, bool){lookup(nil), func(string) (string, bool) { return "", true }, func(string) (string, bool) { return "invalid", false }} {
		got, err := settings.Read(f)
		if err != nil || got != settings.Defaults() {
			t.Fatalf("unset/empty = %+v, %v", got, err)
		}
	}
}

// R-X1CJ-ZGZY R-X2KG-D8QN
func TestReadFirstError(t *testing.T) {
	for first := range names {
		m := map[string]string{}
		for i := first; i < len(names); i++ {
			m[names[i]] = "bad\nvalue"
		}
		_, err := settings.Read(lookup(m))
		var value *settings.Error
		if !errors.As(err, &value) {
			t.Fatalf("error type = %T", err)
		}
		if value.Name != names[first] || value.Value != m[names[first]] || value.Unit != units[first] {
			t.Fatalf("error = %+v", value)
		}
		want := names[first] + " is '" + m[names[first]] + "', not a positive whole number of " + units[first]
		if value.Error() != want {
			t.Fatalf("text = %q, want %q", value.Error(), want)
		}
	}
	for _, test := range []struct{ name, value, unit string }{{"DRAFT_KEEP_COUNT", "0", "drafts"}, {"DRAFT_KEEP_DAYS", "15d", "days"}} {
		_, err := settings.Read(lookup(map[string]string{test.name: test.value}))
		if err.Error() != test.name+" is '"+test.value+"', not a positive whole number of "+test.unit {
			t.Fatal(err)
		}
	}
}

// R-X3SC-R0HC
func TestDrain(t *testing.T) {
	for _, test := range []struct {
		seconds int64
		want    time.Duration
	}{{1, time.Second}, {5, 5 * time.Second}, {math.MaxInt64 / int64(time.Second), time.Duration(math.MaxInt64/int64(time.Second)) * time.Second}, {math.MaxInt64/int64(time.Second) + 1, time.Duration(math.MaxInt64)}, {math.MaxInt64, time.Duration(math.MaxInt64)}} {
		if got := (settings.Settings{DrainSeconds: test.seconds}).Drain(); got != test.want {
			t.Errorf("%d = %s, want %s", test.seconds, got, test.want)
		}
	}
}
