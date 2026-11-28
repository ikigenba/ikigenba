package tools_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func checkTemplateAnswer(t *testing.T, result mcp.Result, want tools.TeamTemplate) {
	t.Helper()
	if got := answer[tools.TeamTemplate](t, result); got != want {
		t.Fatalf("team template %+v, want %+v", got, want)
	}
	wireMembers(t, unpack(t, result).Structured, "url", "ref")
}

// R-W7IY-4I1V R-W9YQ-W1J9 R-WB6N-9T9Y R-WCEJ-NL0N R-WDMG-1CRC
func TestTeamTemplateSetting(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	host, _ := f.seedHost(true)
	before := readHost(t, f, host.Name)
	operations, err := f.st.OperationsOf(t.Context(), host.Name, "", store.BatchSize)
	if err != nil {
		t.Fatal(err)
	}
	read := func(want tools.TeamTemplate) {
		t.Helper()
		start := len(allEvents(t, f))
		result := f.call(tools.ToolTeamTemplate, tools.TeamTemplateArgs{})
		checkTemplateAnswer(t, result, want)
		assertReadEvents(t, allEvents(t, f)[start:], 1)
	}
	want := tools.TeamTemplate{URL: protocol.DefaultTemplateURL, Ref: protocol.DefaultTemplateRef}
	read(want)

	const url, ref = "https://git.example.test/team", "v2"
	for _, tc := range []struct {
		args    map[string]any
		want    tools.TeamTemplate
		changed bool
	}{
		{map[string]any{"ref": ref}, tools.TeamTemplate{URL: want.URL, Ref: ref}, true},
		{map[string]any{"ref": ref}, tools.TeamTemplate{URL: want.URL, Ref: ref}, false},
		{map[string]any{"url": url, "ref": nil}, tools.TeamTemplate{URL: url, Ref: ref}, true},
		{map[string]any{"url": want.URL, "ref": want.Ref}, want, true},
	} {
		start := len(allEvents(t, f))
		checkTemplateAnswer(t, f.call(tools.ToolSetTeamTemplate, tc.args), tc.want)
		events := allEvents(t, f)[start:]
		if tc.changed {
			assertEvents(t, events, trail.EventTemplateChanged, trail.EventChangeFinished, appkitToolCalled)
			wantAttrs := telemetry.Attrs{"url": tc.want.URL, "ref": tc.want.Ref}
			if event := events[0]; event.User != f.caller.UserID || !reflect.DeepEqual(event.Attrs, wantAttrs) {
				t.Fatalf("template change %+v, want %+v under %q", event, wantAttrs, f.caller.UserID)
			}
			events = events[1:]
		} else {
			assertEvents(t, events, trail.EventChangeFinished, appkitToolCalled)
		}
		assertChange(t, f, events[0], tools.ToolSetTeamTemplate, "", "", trail.ResultOK)
		read(tc.want)
		if got, err := f.st.TemplateSettings(t.Context()); err != nil || tools.TeamTemplate(got) != tc.want {
			t.Fatalf("stored setting %+v, want %+v (%v)", got, tc.want, err)
		}
	}
	if after := readHost(t, f, host.Name); after.Version != before.Version || after.Operation != nil {
		t.Fatalf("host after setting changes %+v, initial version %d", after, before.Version)
	}
	if after, err := f.st.OperationsOf(t.Context(), host.Name, "", store.BatchSize); err != nil || !reflect.DeepEqual(after, operations) {
		t.Fatalf("operations after setting changes %+v, want %+v (%v)", after, operations, err)
	}
	assertTemplateToolEffects(t, f)
}

// R-WB6N-9T9Y R-XVA0-V9DW R-WCEJ-NL0N R-WDMG-1CRC
func TestTeamTemplateRefusals(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, tools.NothingToChange},
		{map[string]any{"url": nil, "ref": nil}, tools.NothingToChange},
		{map[string]any{"url": "http://example.test/team", "ref": "-x"}, tools.InvalidURL},
		{map[string]any{"url": "https://example.test/team", "ref": "-x"}, tools.InvalidRef},
		{map[string]any{"ref": "-x"}, tools.InvalidRef},
	}
	check := func(args map[string]any, want, outcome string) {
		t.Helper()
		start := len(allEvents(t, f))
		refusal(t, f.call(tools.ToolSetTeamTemplate, args), want)
		events := allEvents(t, f)[start:]
		assertEvents(t, events, trail.EventChangeFinished, appkitToolCalled)
		assertChange(t, f, events[0], tools.ToolSetTeamTemplate, "", "", outcome)
	}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, tc := range cases {
			check(tc.args, tc.want, trail.ResultRefused)
		}
	}
	check(map[string]any{"ref": "v2"}, store.Unreachable, trail.ResultFailed)
	start := len(allEvents(t, f))
	refusal(t, f.call(tools.ToolTeamTemplate, tools.TeamTemplateArgs{}), store.Unreachable)
	assertReadEvents(t, allEvents(t, f)[start:], 1)
	f.d.SetFailing(false)
	if setting, err := f.st.TemplateSettings(t.Context()); err != nil || setting != store.DefaultTemplateSettings() {
		t.Fatalf("refusals changed setting to %+v (%v)", setting, err)
	}
	assertTemplateToolEffects(t, f)
}

func assertTemplateToolEffects(t *testing.T, f *fixture) {
	t.Helper()
	if f.wakes != 0 || len(f.cloud.lookups) != 0 || len(f.cloud.backupLookups) != 0 {
		t.Fatalf("template tools woke engine %d times and made %d DNS and %d backup requests", f.wakes, len(f.cloud.lookups), len(f.cloud.backupLookups))
	}
}

// R-W535-CYKH R-W6B1-QQB6
func TestAccountAndUserTemplateCommits(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	host, _ := f.seedHost(true)
	const slug = "acme"
	answer[tools.Account](t, f.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: slug, Host: host.Name}))
	account, err := f.st.Account(t.Context(), slug)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"daffy", "porky"} {
		answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: slug, User: name, Key: key(byte(i + 1))}))
	}
	read := func(seed, home string) {
		t.Helper()
		if got := readAccount(t, f, slug).Template; got != seed {
			t.Fatalf("account template %q, want %q", got, seed)
		}
		list := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{Account: &account.Slug}))
		if len(list.Users) != 2 || list.Users[0].User != "daffy" || list.Users[0].Template != home ||
			list.Users[1].User != "porky" || list.Users[1].Template != "" {
			t.Fatalf("users %+v, want daffy at %q and porky with no commit", list.Users, home)
		}
	}
	read("", "")
	seed, home := strings.Repeat("a", 40), strings.Repeat("b", 64)
	current := f.host(host.Name)
	user := f.user(slug, "daffy")
	report := protocol.Report{
		Protocol: protocol.Version, InstanceID: current.InstanceID, Applied: current.Version, Attempted: current.Version,
		Seeds: []protocol.Seed{{GID: account.GID, Commit: seed}},
		Homes: []protocol.Home{{UID: user.UID, Commit: home}},
	}
	if _, err := f.st.RecordReport(t.Context(), current.Name, report, f.now); err != nil {
		t.Fatal(err)
	}
	read(seed, home)
}
