package daemonapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

func replaceFixtureHost(t *testing.T, st *store.Store, old store.Host) store.Host {
	t.Helper()
	removeFixtureHost(t, nil, st, old)
	replacement := provisionFixtureHost(t, st, "i-replacement")
	if replacement.Name != old.Name || replacement.Seq == old.Seq {
		t.Fatalf("replacement did not reuse %s: %+v", old.Name, replacement)
	}
	return replacement
}

// R-OKLA-OIOR.
func TestVerifiedStateDoesNotFollowReusedHostName(t *testing.T) {
	for _, tt := range []struct {
		name                string
		poll                bool
		replaceWhileWaiting bool
	}{
		{name: "before desired read"},
		{name: "before version reread", poll: true},
		{name: "during poll", poll: true, replaceWhileWaiting: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, st, old := fixtureStore(t)
			stamps := new(requests)
			replace := func() { replaceFixtureHost(t, st, old) }
			cfg := Config{Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})}
			path := protocol.StatePath
			if tt.poll {
				path = pollingRequest(old, "")
			}
			cfg.After = func(time.Duration) <-chan time.Time {
				if !tt.replaceWhileWaiting {
					t.Fatal("request waited after verified host was replaced")
				}
				replace()
				timer := make(chan time.Time, 1)
				timer <- fixtureNow
				return timer
			}
			s := server(t, cfg)
			if !tt.replaceWhileWaiting {
				s.store = afterHostLookup{st, replace}
			}
			req := stamps.signed(t.Context(), http.MethodGet, path, nil)
			expectError(t, invoke(s, req), http.StatusForbidden, UnknownHost)
		})
	}
}

type beforeBodyRead struct {
	io.Reader
	before func()
}

func (r *beforeBodyRead) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.Reader.Read(p)
}

// R-KGVR-VVMY
func TestVerifiedReportDoesNotFollowReusedHostName(t *testing.T) {
	_, st, old := fixtureStore(t)
	stamps := new(requests)
	s := server(t, Config{Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})})
	raw := fmt.Sprintf(`{"protocol":%d,"applied":%d}`, protocol.Version, old.Version)
	body := &beforeBodyRead{Reader: strings.NewReader(raw), before: func() { replaceFixtureHost(t, st, old) }}
	req := stamps.signed(t.Context(), http.MethodPost, protocol.ReportPath, body)
	expectError(t, invoke(s, req), http.StatusForbidden, UnknownHost)
	report, err := st.Report(t.Context(), old.Name)
	if err != nil || report != nil {
		t.Fatalf("replacement recorded the old instance's report: %+v, %v", report, err)
	}
}

// R-EJXA-RH54
func TestVerifiedBoxCallDoesNotFollowReusedHostAndUserNames(t *testing.T) {
	_, st, old := fixtureStore(t)
	addBoxAccount(t, st, old.Name, "acme")
	oldUser := addBoxUser(t, st, "acme", "daffy")
	stamps := new(requests)
	writer, _ := recorder(t)
	mcpCfg := mcp.ServerConfig{Name: "accounts", Version: "test-version", Telemetry: writer}
	users, admins := mcp.NewServer(mcpCfg), mcp.NewServer(mcpCfg)
	boxCfg := box.Config{Store: st, Telemetry: writer}
	box.RegisterUser(users, boxCfg)
	box.RegisterAdmin(admins, boxCfg)
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		BoxUser: users, BoxAdmin: admins, Telemetry: writer,
	})
	raw := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{"name":%q,"arguments":{"key":%q}}}`,
		protocol.MethodToolsCall, tools.ToolAddKey, boxKey(t, 1).Line)
	var replacement store.User
	body := &beforeBodyRead{Reader: strings.NewReader(raw), before: func() {
		removeFixtureBoxUser(t, st, oldUser, old)
		host := replaceFixtureHost(t, st, old)
		addBoxAccount(t, st, host.Name, "acme")
		replacement = addBoxUser(t, st, "acme", oldUser.Name)
	}}
	req := stamps.signed(t.Context(), http.MethodPost, boxPath(oldUser.UID), body)
	req.Header.Set("Content-Type", protocol.ContentJSON)
	res := invoke(s, req)
	var response struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	err := json.Unmarshal(res.Body.Bytes(), &response)
	content := response.Result.Content
	if err != nil || res.Code != http.StatusOK || !response.Result.IsError ||
		len(content) != 1 || content[0].Type != "text" || content[0].Text != tools.NoUser {
		t.Fatalf("old user's box call = %d %s (%v), want %q", res.Code, res.Body.String(), err, tools.NoUser)
	}
	if replacement.UID == oldUser.UID || replacement.UID == 0 {
		t.Fatalf("fixture user was not replaced: old UID %d, new UID %d", oldUser.UID, replacement.UID)
	}
	got, err := st.UserNamed(t.Context(), oldUser.Name)
	if err != nil || got.UID != replacement.UID || !slices.Equal(got.Keys, replacement.Keys) {
		t.Fatalf("old box call changed the replacement user: %+v, %v", got, err)
	}
}

func removeFixtureBoxUser(t *testing.T, st *store.Store, u store.User, host store.Host) {
	t.Helper()
	ctx := t.Context()
	if _, err := st.Lock(ctx, u.Account, u.Name); err != nil {
		t.Fatal(err)
	}
	removed, err := st.Delete(ctx, u.Account, u.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordReport(ctx, host.Name, protocol.Report{Applied: removed.Version}, fixtureNow); err != nil {
		t.Fatal(err)
	}
	_, op, _, err := st.RemoveAccount(ctx, u.Account, identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(ctx, op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
}
