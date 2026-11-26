package tools_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-ZFW6-12Z0
func TestAddressAnswerEncoding(t *testing.T) {
	_ = struct {
		IP, Host string
		Hosts    int
	}(tools.Unbanned{})
	_ = struct {
		Host, IP  string
		Allowlist []string
		Pending   bool
	}(tools.AllowlistChange{})
	wireMembers(t, testEncoding(t, tools.Unbanned{}), "ip", "host", "hosts")
	for _, entries := range [][]string{nil, {}, {"192.0.2.7"}} {
		raw := testEncoding(t, tools.AllowlistChange{Allowlist: entries})
		wireMembers(t, raw, "host", "ip", "allowlist", "pending")
		var got []string
		if err := json.Unmarshal(field(t, raw, "allowlist"), &got); err != nil || got == nil || !slices.Equal(got, entries) {
			t.Fatalf("allowlist = %v (%v), want array containing %v", got, err, entries)
		}
	}
}

func (f *fixture) changeHostState(name, kind string, finish bool) {
	f.t.Helper()
	var op store.Operation
	var err error
	if kind == store.OpDelete {
		_, op, err = f.st.DeleteHost(f.t.Context(), name, true, f.caller)
	} else {
		_, op, err = f.st.StartHostOp(f.t.Context(), name, kind, store.Target{}, f.caller)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	if finish {
		f.finish(op.ID, store.Update{})
	}
}

func (f *fixture) syncHost(name string) {
	f.t.Helper()
	h := f.host(name)
	report := protocol.Report{Protocol: protocol.Version, Applied: h.Version, Attempted: h.Version}
	if _, err := f.st.RecordReport(f.t.Context(), name, report, f.now); err != nil {
		f.t.Fatal(err)
	}
}

// R-FD2D-SMU2 R-ZH42-EUPP
func TestNamedUnban(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	running, _ := f.seedHost(true)
	f.syncHost(running.Name)
	initial := f.host(running.Name)
	call := tools.UnbanArgs{IP: "::ffff:192.0.2.7", Host: &running.Name}
	for range 2 {
		result := f.call(tools.ToolUnbanIP, call)
		wireMembers(t, unpack(t, result).Structured, "ip", "host", "hosts")
		if got, want := answer[tools.Unbanned](t, result), (tools.Unbanned{IP: "192.0.2.7", Host: running.Name, Hosts: 1}); got != want {
			t.Fatalf("unban = %+v, want %+v", got, want)
		}
		h := readHost(t, f, running.Name)
		if h.Version != initial.Version+1 || h.Version != h.Applied+1 {
			t.Fatalf("recorded host = %+v, initial version %d", h, initial.Version)
		}
	}
	desired, err := f.st.Desired(t.Context(), running.Name)
	if err != nil || !slices.Equal(desired.Unban, []string{"192.0.2.7"}) {
		t.Fatalf("desired unbans %v (%v)", desired.Unban, err)
	}
	for i := 1; i < valid.MaxUnbansPerHost; i++ {
		if _, err := f.st.Unban(t.Context(), running.Name, fmt.Sprintf("198.51.100.%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	version := f.host(running.Name).Version
	answer[tools.Unbanned](t, f.call(tools.ToolUnbanIP, call))
	refusal(t, f.call(tools.ToolUnbanIP, tools.UnbanArgs{IP: "203.0.113.9", Host: &running.Name}), tools.TooManyUnbans)
	if h := f.host(running.Name); h.Version != version {
		t.Fatalf("version %d, want %d", h.Version, version)
	}
	stopped, _ := f.seedHost(true)
	f.changeHostState(stopped.Name, store.OpStop, true)
	deleted, _ := f.seedHost(true)
	f.changeHostState(deleted.Name, store.OpDelete, true)
	missing := "missing"
	for _, tc := range []struct{ host, want string }{
		{stopped.Name, tools.HostNotRunning},
		{deleted.Name, tools.NoHost},
		{missing, tools.NoHost},
	} {
		refusal(t, f.call(tools.ToolUnbanIP, tools.UnbanArgs{IP: call.IP, Host: &tc.host}), tc.want)
		refusal(t, f.call(tools.ToolUnbanIP, tools.UnbanArgs{IP: "192.0.2.0/24", Host: &tc.host}), tools.InvalidAddress)
	}
}

// R-FEAA-6EKR
func TestUnbanAllRunning(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	const ip = "192.0.2.7"
	stopped, _ := f.seedHost(true)
	f.changeHostState(stopped.Name, store.OpStop, true)
	missing := answer[tools.Unbanned](t, f.call(tools.ToolUnbanIP, map[string]any{"ip": ip}))
	if missing != (tools.Unbanned{IP: ip}) {
		t.Fatal(missing)
	}
	first, _ := f.seedHost(true)
	second, _ := f.seedHost(true)
	if _, err := f.st.Unban(t.Context(), second.Name, ip); err != nil {
		t.Fatal(err)
	}
	versions := map[string]int64{
		first.Name: f.host(first.Name).Version + 1, second.Name: f.host(second.Name).Version, stopped.Name: f.host(stopped.Name).Version,
	}
	for _, args := range []map[string]any{{"ip": ip}, {"ip": ip, "host": nil}, {"ip": ip, "host": ""}} {
		got := answer[tools.Unbanned](t, f.call(tools.ToolUnbanIP, args))
		if got != (tools.Unbanned{IP: ip, Hosts: 2}) {
			t.Fatal(got)
		}
		for host, version := range versions {
			state, err := f.st.Desired(t.Context(), host)
			var want []string
			if host != stopped.Name {
				want = []string{ip}
			}
			if err != nil || state.Version != version || !slices.Equal(state.Unban, want) {
				t.Fatalf("desired state for %s = %+v (%v)", host, state, err)
			}
		}
	}
	for i := 1; i < valid.MaxUnbansPerHost; i++ {
		if _, err := f.st.Unban(t.Context(), second.Name, fmt.Sprintf("198.51.100.%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	versions[second.Name] = f.host(second.Name).Version
	refusal(t, f.call(tools.ToolUnbanIP, tools.UnbanArgs{IP: "203.0.113.9"}), tools.TooManyUnbans)
	for host, version := range versions {
		if h := f.host(host); h.Version != version {
			t.Fatalf("version of %s = %d, want %d", host, h.Version, version)
		}
	}
}

// R-FFI6-K6BG R-FHXZ-BPSU R-FKDS-39A8 R-ZH42-EUPP
func TestAllowlistChanges(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	f.syncHost(h.Name)
	version := f.host(h.Name).Version
	for _, tc := range []struct {
		tool, ip, canonical string
		allowlist           []string
		pending, bump, sync bool
	}{
		{tools.ToolDisallowIP, "192.0.2.5/24", "192.0.2.0/24", []string{}, false, false, false},
		{tools.ToolAllowIP, "198.51.100.1", "198.51.100.1", []string{"198.51.100.1"}, true, true, false},
		{tools.ToolAllowIP, "192.0.2.77/24", "192.0.2.0/24", []string{"192.0.2.0/24", "198.51.100.1"}, true, true, false},
		{tools.ToolAllowIP, "192.0.2.0/24", "192.0.2.0/24", []string{"192.0.2.0/24", "198.51.100.1"}, true, false, true},
		{tools.ToolAllowIP, "192.0.2.0/24", "192.0.2.0/24", []string{"192.0.2.0/24", "198.51.100.1"}, false, false, false},
		{tools.ToolDisallowIP, "192.0.2.5/24", "192.0.2.0/24", []string{"198.51.100.1"}, true, true, false},
		{tools.ToolDisallowIP, "198.51.100.1", "198.51.100.1", []string{}, true, true, false},
		{tools.ToolDisallowIP, "198.51.100.1", "198.51.100.1", []string{}, true, false, false},
	} {
		result := f.call(tc.tool, tools.HostIPArgs{Host: h.Name, IP: tc.ip})
		wireMembers(t, unpack(t, result).Structured, "host", "ip", "allowlist", "pending")
		want := tools.AllowlistChange{Host: h.Name, IP: tc.canonical, Allowlist: tc.allowlist, Pending: tc.pending}
		if got := answer[tools.AllowlistChange](t, result); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s %q = %+v, want %+v", tc.tool, tc.ip, got, want)
		}
		if tc.bump {
			version++
		}
		record := f.host(h.Name)
		if record.Version != version || !slices.Equal(record.Allowlist, tc.allowlist) {
			t.Fatalf("record after %s %q = %+v, want version %d and allowlist %v", tc.tool, tc.ip, record, version, tc.allowlist)
		}
		if tc.sync {
			f.syncHost(h.Name)
		}
	}
	for i := 1; i <= valid.MaxAllowlistPerHost; i++ {
		if _, err := f.st.Allow(t.Context(), h.Name, fmt.Sprintf("198.51.100.%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	full := f.host(h.Name)
	answer[tools.AllowlistChange](t, f.call(tools.ToolAllowIP, tools.HostIPArgs{Host: h.Name, IP: "198.51.100.1"}))
	refusal(t, f.call(tools.ToolAllowIP, tools.HostIPArgs{Host: h.Name, IP: "203.0.113.9"}), tools.AllowlistFull)
	if got := f.host(h.Name); !reflect.DeepEqual(got, full) {
		t.Fatalf("call changed full host: %+v", got)
	}
}

// R-FGQ2-XY25
func TestAllowlistHostChecks(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	deleting, _ := f.seedHost(true)
	f.changeHostState(deleting.Name, store.OpDelete, false)
	deleted, _ := f.seedHost(true)
	stopped, _ := f.seedHost(true)
	f.changeHostState(stopped.Name, store.OpStop, true)
	failed, op := f.seedHost(false)
	f.fail(op.ID, "create failed")
	provisioning, _ := f.seedHost(false)
	f.changeHostState(deleted.Name, store.OpDelete, true)
	for _, name := range []string{tools.ToolAllowIP, tools.ToolDisallowIP} {
		for _, tc := range []struct{ host, want string }{
			{deleting.Name, tools.HostGone}, {deleted.Name, tools.NoHost}, {"missing", tools.NoHost}, {"", tools.NoHost},
		} {
			refusal(t, f.call(name, tools.HostIPArgs{Host: tc.host, IP: "192.0.2.7"}), tc.want)
			refusal(t, f.call(name, tools.HostIPArgs{Host: tc.host, IP: "bad"}), tools.InvalidAddress)
			refusal(t, f.call(name, tools.HostIPArgs{Host: tc.host, IP: "10.0.0.0/8"}), tools.PrefixTooWide)
		}
		for _, host := range []string{stopped.Name, failed.Name, provisioning.Name} {
			answer[tools.AllowlistChange](t, f.call(name, tools.HostIPArgs{Host: host, IP: "192.0.2.7"}))
		}
	}
}

// R-0PUB-I51D R-FLLO-H10X R-FO1H-8KIB
func TestAddressChangeTrail(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	for _, tc := range []struct {
		tool      string
		args      any
		host, ip  string
		refusal   string
		result    string
		failStore bool
	}{
		{tools.ToolUnbanIP, tools.UnbanArgs{IP: "::ffff:192.0.2.7"}, "", "192.0.2.7", "", trail.ResultOK, false},
		{tools.ToolUnbanIP, tools.UnbanArgs{IP: "192.0.2.7", Host: &h.Name}, h.Name, "192.0.2.7", "", trail.ResultOK, false},
		{tools.ToolAllowIP, tools.HostIPArgs{Host: h.Name, IP: "192.0.2.77/24"}, h.Name, "192.0.2.0/24", "", trail.ResultOK, false},
		{tools.ToolDisallowIP, tools.HostIPArgs{Host: h.Name, IP: "192.0.2.77/24"}, h.Name, "192.0.2.0/24", "", trail.ResultOK, false},
		{tools.ToolUnbanIP, tools.UnbanArgs{IP: "arbitrary input", Host: &h.Name}, h.Name, "", tools.InvalidAddress, trail.ResultRefused, false},
		{tools.ToolAllowIP, tools.HostIPArgs{Host: h.Name, IP: "10.0.0.0/8"}, h.Name, "", tools.PrefixTooWide, trail.ResultRefused, false},
		{tools.ToolDisallowIP, tools.HostIPArgs{Host: "missing", IP: "192.0.2.77/24"}, "missing", "192.0.2.0/24", tools.NoHost, trail.ResultRefused, false},
		{tools.ToolUnbanIP, tools.UnbanArgs{IP: "::ffff:192.0.2.7"}, "", "192.0.2.7", store.Unreachable, trail.ResultFailed, true},
	} {
		previous := len(f.capture.Events())
		f.d.SetFailing(tc.failStore)
		result := f.call(tc.tool, tc.args)
		if tc.refusal != "" {
			refusal(t, result, tc.refusal)
		} else if result.IsError() {
			t.Fatalf("%s refused: %+v", tc.tool, result)
		}
		if err := f.w.Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
		want := telemetry.Attrs{"tool": tc.tool, "host": tc.host, "account": "", "user": "", "ip": tc.ip, "result": tc.result}
		count := 0
		for _, event := range f.capture.Events()[previous:] {
			if event.Name == "tool.called" {
				continue
			}
			if event.Name != trail.EventChangeFinished || !reflect.DeepEqual(event.Attrs, want) || event.User != f.caller.UserID {
				t.Fatalf("%s emitted %+v, want %v", tc.tool, event, want)
			}
			count++
		}
		if count != 1 || f.wakes != 0 || len(f.cloud.lookups) != 0 {
			t.Fatalf("%s emitted %d change events, woke %d times, looked up %v", tc.tool, count, f.wakes, f.cloud.lookups)
		}
	}
}
