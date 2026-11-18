package settings_test

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
}

// R-YFG7-HLY8 R-CTID-2XG6 R-YNZI-6053
func TestDefaults(t *testing.T) {
	defaults := settings.Defaults
	want := settings.Settings{DrainSeconds: 5}
	for range 2 {
		if got := defaults(); got != want || got.Cloud() != settings.ModeNone {
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

// R-YP7E-JRVS R-YGO3-VDOX
func TestHostSettingsCannotRefuseDrain(t *testing.T) {
	for _, drain := range []string{"", "17", "05"} {
		for _, sandbox := range []struct {
			value string
			set   bool
		}{{"", false}, {"", true}, {"space", true}} {
			values := map[string]string{
				"DRAIN_SECONDS": drain, "AWS_REGION": " chosen region ",
				"IKIGENBA_HOSTS_DOMAIN": " hosts.test ", "IKIGENBA_HOSTS_ZONE_ID": " zone ",
				"IKIGENBA_HOSTS_LAUNCH_TEMPLATE": " template ", "IKIGENBA_HOSTS_INSTANCE_PROFILE": " profile ",
			}
			if sandbox.set {
				values["IKIGENBA_SANDBOX"] = sandbox.value
			}
			got, err := settings.Read(lookup(values))
			if drain == "05" {
				var refused *settings.Error
				if !errors.As(err, &refused) || *refused != (settings.Error{"DRAIN_SECONDS", drain, settings.WantSeconds}) {
					t.Fatalf("host selection changed refusal: %#v, %v", got, err)
				}
			} else {
				want := settings.Hosts{Region: " chosen region ", Domain: " hosts.test ", ZoneID: " zone ", LaunchTemplate: " template ", InstanceProfile: " profile "}
				if err != nil || got.Hosts != want || got.Sandbox != sandbox.set {
					t.Fatalf("Read = %#v, %v; want hosts %#v and sandbox %v", got, err, want, sandbox.set)
				}
			}
		}
	}
	got, err := settings.Read(func(string) (string, bool) { return "ignored", false })
	if err != nil || got.Hosts != (settings.Hosts{}) || got.Sandbox {
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

// R-YHW0-95FM R-YJ3W-MX6B R-YKBT-0OX0 R-YQFA-XJMH R-YRN7-BBD6 R-YSV3-P33V
func TestCloudMode(t *testing.T) {
	var zero settings.Mode
	if zero != settings.ModeNone || zero == settings.ModeAWS || zero == settings.ModeSandbox || settings.ModeAWS == settings.ModeSandbox {
		t.Fatal("mode values overlap")
	}
	wantSandbox := settings.Hosts{Region: "sandbox", Domain: "hosts.test", ZoneID: "ZSANDBOX", LaunchTemplate: "sandbox-hosts", InstanceProfile: "sandbox-hosts"}
	if settings.SandboxHosts != wantSandbox || settings.SandboxBinding != "accounts.sandbox.test" {
		t.Fatal("sandbox resources", settings.SandboxHosts, settings.SandboxBinding)
	}
	for subset := range 32 {
		var hosts settings.Hosts
		for i, field := range []*string{&hosts.Region, &hosts.Domain, &hosts.ZoneID, &hosts.LaunchTemplate, &hosts.InstanceProfile} {
			if subset&(1<<i) != 0 {
				*field = " "
			}
		}
		if hosts.Configured() != (subset == 31) {
			t.Fatal("configured", hosts)
		}
		for _, sandbox := range []bool{false, true} {
			s := settings.Settings{Sandbox: sandbox, Hosts: hosts}
			mode, inUse := settings.ModeNone, settings.Hosts{}
			switch {
			case subset == 31:
				mode, inUse = settings.ModeAWS, hosts
			case sandbox && subset == 0:
				mode, inUse = settings.ModeSandbox, wantSandbox
			}
			if s.Cloud() != mode || s.HostsInUse() != inUse {
				t.Fatal(s, s.Cloud(), s.HostsInUse(), mode, inUse)
			}
		}
	}
}

// R-YLJP-EGNP R-YU30-2UUK R-YVAW-GML9
func TestServiceURLAndBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	var unconfigured settings.Settings
	sandbox := settings.Settings{Sandbox: true}
	check := func(s settings.Settings, path, wantURL, wantBinding string, wantErr error) {
		t.Helper()
		value, err := s.AccountsURL(path)
		binding, bindingErr := s.Binding(path)
		if value != wantURL || binding != wantBinding || !errors.Is(err, wantErr) || !errors.Is(bindingErr, wantErr) {
			t.Fatalf("URL %q (%v), binding %q (%v)", value, err, binding, bindingErr)
		}
	}
	for _, tc := range []struct{ input, url, binding string }{
		{"https://accounts.one.test///", "https://accounts.one.test", "accounts.one.test"},
		{"https://Accounts.TWO.test:8443", "https://Accounts.TWO.test:8443", "accounts.two.test:8443"},
		{"ftp://accounts.test", "", ""},
		{"https:relative", "", ""},
		{"%not-escaped", "", ""},
	} {
		data, err := json.Marshal(map[string]any{"services": []map[string]any{{
			"name": "accounts", "url": tc.input, "socket": "", "description": "", "enabled": true, "mcp": true,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if tc.url == "" {
			check(unconfigured, path, "", "", settings.ErrNoAccountsURL)
			check(sandbox, path, "http://"+settings.SandboxBinding, settings.SandboxBinding, nil)
		} else {
			check(unconfigured, path, tc.url, tc.binding, nil)
			check(sandbox, path, tc.url, tc.binding, nil)
		}
	}
	check(unconfigured, "", "", "", settings.ErrNoAccountsURL)
	check(sandbox, "", "http://"+settings.SandboxBinding, settings.SandboxBinding, nil)
}
