package settings_test

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/settings"
)

var names = []string{
	"DRAIN_SECONDS", "READ_SLOTS", "WRITE_SLOTS", "QUEUE_LENGTH", "QUEUE_SECONDS",
	"OPERATION_SECONDS", "PUSH_MAX_BYTES", "REPO_MAX_BYTES", "MAINTENANCE_HOURS",
}

func fields(s settings.Settings) []int64 {
	return []int64{
		s.DrainSeconds, s.ReadSlots, s.WriteSlots, s.QueueLength, s.QueueSeconds,
		s.OperationSeconds, s.PushMaxBytes, s.RepoMaxBytes, s.MaintenanceHours,
	}
}

// R-SNRQ-DNJ1 R-SOZM-RF9Q R-SQ7J-570F
func TestSettingsContractAndDefaults(t *testing.T) {
	api := struct {
		Defaults func() settings.Settings
		Read     func(func(string) (string, bool)) (settings.Settings, error)
	}{Defaults: settings.Defaults, Read: settings.Read}
	want := settings.Settings{
		DrainSeconds: 5, ReadSlots: 8, WriteSlots: 2, QueueLength: 16, QueueSeconds: 30,
		OperationSeconds: 600, PushMaxBytes: 104857600, RepoMaxBytes: 1073741824, MaintenanceHours: 24,
	}
	if got := api.Defaults(); got != want {
		t.Fatalf("Defaults() = %+v, want %+v", got, want)
	}
	got, err := api.Read(func(string) (string, bool) { return "", false })
	if err != nil || got != want {
		t.Fatalf("Read(unset) = %+v, %v, want %+v, nil", got, err, want)
	}
	var e error = &settings.Error{Name: "READ_SLOTS", Value: "bad", Unit: "operations"}
	if e.Error() != "READ_SLOTS is 'bad', not a positive whole number of operations" {
		t.Fatal(e)
	}
}

// R-SNRQ-DNJ1 R-SSNB-WQHT
func TestReadMapsEveryVariableAndDefaults(t *testing.T) {
	values := make(map[string]string)
	for i, name := range names {
		values[name] = strconv.Itoa(i + 11)
	}
	got, err := settings.Read(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range fields(got) {
		if v != int64(i+11) {
			t.Errorf("%s = %d, want %d", names[i], v, i+11)
		}
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			for _, absent := range []bool{true, false} {
				got, err := settings.Read(func(key string) (string, bool) {
					if key == name {
						if absent {
							return "ignored nonempty unset value", false
						}
						return "", true
					}
					return "17", true
				})
				if err != nil {
					t.Fatal(err)
				}
				for i, v := range fields(got) {
					want := int64(17)
					if names[i] == name {
						want = fields(settings.Defaults())[i]
					}
					if v != want {
						t.Errorf("%s = %d, want %d", names[i], v, want)
					}
				}
			}
		})
	}
}

// R-SRFF-IYR4 R-SSNB-WQHT
func TestReadPositiveDigitsAndSaturation(t *testing.T) {
	values := []struct {
		text string
		want int64
	}{
		{"1", 1}, {"5", 5}, {"10", 10},
		{strconv.FormatInt(math.MaxInt64, 10), math.MaxInt64},
		{"9223372036854775808", math.MaxInt64},
		{strings.Repeat("9", 10000), math.MaxInt64},
	}
	for _, value := range values {
		got, err := settings.Read(func(string) (string, bool) { return value.text, true })
		if err != nil {
			t.Fatalf("Read(%q): %v", value.text, err)
		}
		for i, field := range fields(got) {
			if field != value.want {
				t.Errorf("%s = %d, want %d", names[i], field, value.want)
			}
		}
	}
}

// R-SRFF-IYR4 R-STV8-AI8I R-SV34-O9Z7
func TestReadRefusesInvalidValuesWithFirstError(t *testing.T) {
	units := []string{"seconds", "operations", "operations", "operations", "seconds", "seconds", "bytes", "bytes", "hours"}
	invalid := []string{"0", "-1", "+5", "2.5", "5s", "8x", "05", " 5", "5 ", "abc", "\t1", "1\n", "１", "١", "1'quoted", strings.Repeat("9", 100) + "x"}
	for i, name := range names {
		t.Run(name, func(t *testing.T) {
			for _, value := range invalid {
				_, err := settings.Read(func(key string) (string, bool) {
					if key == name {
						return value, true
					}
					return "", false
				})
				var e *settings.Error
				if !errors.As(err, &e) {
					t.Fatalf("Read(%q) error = %v, want *settings.Error", value, err)
				}
				if e.Name != name || e.Value != value || e.Unit != units[i] {
					t.Fatalf("error = %+v, want name=%s value=%q unit=%s", e, name, value, units[i])
				}
				want := name + " is '" + value + "', not a positive whole number of " + units[i]
				if e.Error() != want {
					t.Errorf("Error() = %q, want %q", e.Error(), want)
				}
			}
		})
	}
	for first := range names {
		_, err := settings.Read(func(key string) (string, bool) {
			for i := first; i < len(names); i++ {
				if key == names[i] {
					return "bad-" + strconv.Itoa(i), true
				}
			}
			return "1", true
		})
		var e *settings.Error
		if !errors.As(err, &e) || e.Name != names[first] || e.Value != "bad-"+strconv.Itoa(first) || e.Unit != units[first] {
			t.Fatalf("first invalid at %d: error = %v", first, err)
		}
	}
}
