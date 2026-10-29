package settings_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
}

// R-CSAG-P5PH R-CTID-2XG6 R-D21N-RBN1
func TestDefaults(t *testing.T) {
	defaults := settings.Defaults
	want := settings.Settings{5, "", false}
	for range 2 {
		if got := defaults(); got != want {
			t.Fatalf("Defaults() = %#v, want %#v", got, want)
		}
	}
}

// R-CUQ9-GP6V R-D39K-53DQ R-D4HG-IV4F
func TestReadDrain(t *testing.T) {
	read := settings.Read
	for _, tc := range []struct {
		value string
		set   bool
		want  int64
	}{
		{"", false, settings.Defaults().DrainSeconds},
		{"", true, settings.Defaults().DrainSeconds},
		{"1", true, 1},
		{"30", true, 30},
		{"1234567890", true, 1234567890},
		{"9223372036854775807", true, math.MaxInt64},
		{"9223372036854775808", true, math.MaxInt64},
		{"99999999999999999999", true, math.MaxInt64},
		{strings.Repeat("9", 1000), true, math.MaxInt64},
	} {
		values := map[string]string{}
		if tc.set {
			values["DRAIN_SECONDS"] = tc.value
		}
		got, err := read(lookup(values))
		if err != nil || got.DrainSeconds != tc.want {
			t.Fatalf("Read(%q, %v) = %#v, %v; want drain %d", tc.value, tc.set, got, err, tc.want)
		}
	}
	for _, value := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc", "١", "５", "1\n", "1\x00", "1\t2"} {
		if _, err := read(lookup(map[string]string{"DRAIN_SECONDS": value})); err == nil {
			t.Fatalf("Read accepted %q", value)
		}
	}
}

// R-D5PC-WMV4
func TestHostSettingsCannotRefuseDrain(t *testing.T) {
	for _, drain := range []string{"", "17", "05"} {
		for _, command := range []string{"", " \t ", "/chosen/stub -x", "\x00\n"} {
			for _, sandbox := range []struct {
				value string
				set   bool
			}{{"", false}, {"", true}, {"space", true}} {
				values := map[string]string{"DRAIN_SECONDS": drain, "ACCOUNTS_HOST_COMMAND": command}
				if sandbox.set {
					values["IKIGENBA_SANDBOX"] = sandbox.value
				}
				got, err := settings.Read(lookup(values))
				if drain == "05" {
					var refused *settings.Error
					if !errors.As(err, &refused) || *refused != (settings.Error{"DRAIN_SECONDS", drain, settings.WantSeconds}) {
						t.Fatalf("host selection changed refusal: %#v, %v", got, err)
					}
				} else if err != nil || got.HostCommand != command || got.Sandbox != sandbox.set {
					t.Fatalf("Read = %#v, %v; command %q, sandbox %v", got, err, command, sandbox.set)
				}
			}
		}
	}
	got, err := settings.Read(func(string) (string, bool) { return "ignored", false })
	if err != nil || got.HostCommand != "" || got.Sandbox {
		t.Fatalf("absent settings = %#v, %v", got, err)
	}
}

// R-CX62-88O9 R-D6X9-AELT R-D855-O6CI
func TestSettingError(t *testing.T) {
	const want string = settings.WantSeconds
	if want == "" || strings.Contains(want, "\n") {
		t.Fatalf("invalid WantSeconds %q", want)
	}
	for _, value := range []string{"05", " 5", "-1", "1\n2"} {
		_, err := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": value}))
		var refused *settings.Error
		if !errors.As(err, &refused) || *refused != (settings.Error{"DRAIN_SECONDS", value, want}) {
			t.Fatalf("refusal for %q: %v", value, err)
		}
		if got, expected := refused.Error(), "DRAIN_SECONDS is '"+value+"', not "+want; got != expected {
			t.Fatalf("Error() = %q, want %q", got, expected)
		}
	}
	custom := &settings.Error{"chosen-name", "chosen-value", "chosen-want"}
	if got := custom.Error(); got != custom.Name+" is '"+custom.Value+"', not "+custom.Want {
		t.Fatalf("custom Error() = %q", got)
	}
}

// R-CYDY-M0EY R-D9D2-1Y37
func TestDrainSaturates(t *testing.T) {
	for _, seconds := range []int64{1, 5, 30, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		want := time.Duration(math.MaxInt64)
		if seconds <= math.MaxInt64/int64(time.Second) {
			want = time.Duration(seconds) * time.Second
		}
		if got := (settings.Settings{DrainSeconds: seconds}).Drain(); got != want {
			t.Fatalf("Drain(%d) = %v, want %v", seconds, got, want)
		}
	}
}

// R-D0TR-DJWC R-DAKY-FPTW
func TestHostFields(t *testing.T) {
	for _, command := range []string{"", " \t\n", "\t/chosen/stub   -x\t", " /chosen/stub \u2003 --flag "} {
		for _, sandbox := range []bool{false, true} {
			s := settings.Settings{5, command, sandbox}
			want := host.Fields(command)
			if sandbox && strings.TrimSpace(command) == "" {
				want = nil
			}
			if got := s.HostFields(); !reflect.DeepEqual(got, want) {
				t.Fatalf("HostFields(%q, %v) = %v, want %v", command, sandbox, got, want)
			}
		}
	}
}
