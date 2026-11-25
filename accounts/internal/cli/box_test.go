package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type boxCloud struct {
	*memory.Cloud
}

func (c boxCloud) CallerOf(ctx context.Context, raw, _ string) (cloud.Caller, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return cloud.Caller{}, err
	}
	instance := u.Query().Get("X-Amz-Credential")
	if _, err := c.Metadata(instance).Instance(ctx); err != nil {
		return cloud.Caller{}, err
	}
	return cloud.Caller{
		Account: memory.Account, Arn: "arn:aws:sts::" + memory.Account + ":assumed-role/" + settings.SandboxHosts.InstanceProfile + "/" + instance,
		UserID: "chosen-role:" + instance,
	}, nil
}

type boxReply struct {
	body    []byte
	Tools   []struct{ Name string } `json:"tools"`
	Content json.RawMessage         `json:"structuredContent"`
	IsError bool                    `json:"isError"`
	Meta    struct {
		Server struct{ Version string } `json:"io.modelcontextprotocol/serverInfo"`
	} `json:"_meta"`
}

func writeAccountsURL(t *testing.T, file, accountsURL string) {
	t.Helper()
	body := fmt.Sprintf(`{"services":[{"name":"accounts","url":%q,"description":"fixture","socket":"","enabled":true,"mcp":true}]}`, accountsURL)
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

type boxTrailExpectation struct {
	status           int
	headerUser, user string
	tool, result     string
	detail           telemetry.Event
}

// R-1NIG-J0PT R-IJ8P-GADI
func TestRunBoxToolWiringAndSecrecy(t *testing.T) {
	clock := newDrivenClock()
	hosts := settings.SandboxHosts
	c, err := memory.New(memory.Config{
		Path: filepath.Join(t.TempDir(), "cloud.json"), Hosts: hosts, Now: clock.now, Rand: rand.NewChaCha8([32]byte{5}),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "services")
	writeAccountsURL(t, path, "https://accounts.one.test/")
	serverVersion := func(number int) string { return runVersion + "-" + strconv.Itoa(number) }
	var servers int
	run := startIntegration(t, integrationOptions{
		now: clock.now, after: clock.after, sleep: clock.sleep, path: path, email: emailMarker + "@fixture.test",
		random: rand.NewChaCha8([32]byte{6}), acceptNotify: true, noSandbox: true,
		env: map[string]string{
			"AWS_REGION": hosts.Region, "IKIGENBA_HOSTS_ZONE_ID": hosts.ZoneID, "IKIGENBA_HOSTS_DOMAIN": hosts.Domain,
			"IKIGENBA_HOSTS_LAUNCH_TEMPLATE": hosts.LaunchTemplate, "IKIGENBA_HOSTS_INSTANCE_PROFILE": hosts.InstanceProfile,
		},
		cloud: func(context.Context, string) (cloud.Cloud, error) { return boxCloud{c}, nil },
		mcp: func(w *telemetry.Writer) *mcp.Server {
			servers++
			return mcp.NewServer(mcp.ServerConfig{Name: "accounts", Version: serverVersion(servers), Telemetry: w})
		},
	})
	run.call(tools.ToolCreateHost, struct{}{}, nil)
	var host tools.HostRead
	driveUntil(t, clock, func() bool {
		run.call(tools.ToolHost, tools.HostArgs{Host: "host01"}, &host)
		return host.State == store.HostRunning && host.InstanceID != ""
	})
	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: "host01"}, nil)
	key := secrecyKey(t, "")
	extraKey := secrecyKey(t, "ABCD")
	adminKey := secrecyKey(t, "EFGH")
	for _, name := range []string{"daffy", "bugs"} {
		run.call(tools.ToolAddUser, tools.UserKeyArgs{Account: "acme", User: name, Key: key}, nil)
	}
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(run.dir, "state", "accounts.db"), Migrations: accounts.Migrations(), Now: clock.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	record := store.New(d, store.Config{Now: clock.now})
	daffy, err := record.User(t.Context(), "acme", "daffy")
	if err != nil {
		t.Fatal(err)
	}
	bugs, err := record.User(t.Context(), "acme", "bugs")
	if err != nil {
		t.Fatal(err)
	}
	requests := make(map[string]*boxTrailExpectation)
	var next int
	boxRequest := func(method string, params any, requestID string, user store.User, status int) boxReply {
		t.Helper()
		next++
		want := &boxTrailExpectation{status: status, headerUser: "owner"}
		if user.Name == "bugs" {
			want.headerUser = ""
		}
		want.user = want.headerUser
		if user.Name != "" {
			want.user = box.ActorPrefix + user.Name
		}
		requests[requestID] = want
		sts := url.URL{Scheme: "https", Host: "sts." + hosts.Region + ".amazonaws.com", Path: "/"}
		sts.RawQuery = url.Values{
			"Action": {"GetCallerIdentity"}, "Version": {"2011-06-15"},
			"X-Amz-Algorithm": {"AWS4-HMAC-SHA256"}, "X-Amz-SignedHeaders": {"host;" + strings.ToLower(hostauth.BindingHeader)},
			"X-Amz-Expires": {"60"}, "X-Amz-Date": {clock.now().Format("20060102T150405Z")},
			"X-Amz-Signature": {fmt.Sprintf("%064x", next)}, "X-Amz-Credential": {host.InstanceID},
		}.Encode()
		if method == protocol.MethodToolsList {
			params = map[string]any{"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion":    mcp.ProtocolVersion,
				"io.modelcontextprotocol/clientCapabilities": struct{}{},
			}}
		}
		payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": next, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		query := url.Values{protocol.QueryUID: {strconv.FormatInt(user.UID, 10)}}.Encode()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, run.base+protocol.BoxPath+"?"+query, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(hostauth.HeaderName, base64.RawURLEncoding.EncodeToString([]byte(sts.String())))
		req.Header.Set("Content-Type", "application/json")
		req.Host = "accounts.test"
		req.Header.Set("X-Forwarded-Proto", "https")
		if want.headerUser != "" {
			req.Header.Set("X-User-Id", want.headerUser)
			req.Header.Set("X-User-Email", run.email)
		}
		req.Header.Set("X-Request-Id", requestID)
		if method == protocol.MethodToolsList {
			req.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
			req.Header.Set("Mcp-Method", method)
		}
		res, err := run.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != status {
			t.Fatalf("%s: box request %s answered %d: %s (%v)", requestID, method, res.StatusCode, body, err)
		}
		if status != http.StatusOK {
			return boxReply{}
		}
		var reply struct {
			Result boxReply        `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(body, &reply); err != nil || reply.Error != nil {
			t.Fatalf("%s: box reply = %s (%v)", requestID, body, err)
		}
		reply.Result.body = body
		return reply.Result
	}
	call := func(requestID string, user store.User, tool string, args any, result, refusal string) boxReply {
		t.Helper()
		reply := boxRequest(protocol.MethodToolsCall, map[string]any{"name": tool, "arguments": args}, requestID, user, http.StatusOK)
		if reply.IsError != (refusal != "") || (refusal != "" && !bytes.Contains(reply.body, []byte(refusal))) {
			t.Fatalf("%s: %s reply = %s, expected refusal %q", requestID, tool, reply.body, refusal)
		}
		requests[requestID].tool, requests[requestID].result = tool, result
		return reply
	}
	checkList := func(list boxReply, registered, want []string, factoryNumber int) {
		t.Helper()
		var wire []string
		for _, tool := range list.Tools {
			wire = append(wire, tool.Name)
		}
		slices.Sort(wire)
		registered = slices.Clone(registered)
		slices.Sort(registered)
		slices.Sort(want)
		if !slices.Equal(wire, want) || !slices.Equal(registered, want) {
			t.Fatalf("tool list on the wire = %v, factory registration = %v, want %v", wire, registered, want)
		}
		if got := list.Meta.Server.Version; got != serverVersion(factoryNumber) {
			t.Fatalf("tool list served by version %q, want %q", got, serverVersion(factoryNumber))
		}
	}
	catalogs := run.catalogsBeforeReady()
	if len(catalogs) != 3 {
		t.Fatalf("factory catalogs before readiness notification: %v", catalogs)
	}
	userTools := []string{tools.ToolAddKey, box.ToolKeys, tools.ToolRemoveKey}
	checkList(boxRequest(protocol.MethodToolsList, nil, "box-user-tools", daffy, http.StatusOK), catalogs[1], userTools, 2)
	call("box-last-key", daffy, tools.ToolRemoveKey, box.KeyArgs{Key: key}, trail.ResultRefused, box.LastKey)
	call("box-add-key", daffy, tools.ToolAddKey, box.KeyArgs{Key: extraKey}, trail.ResultOK, "")
	var users tools.UserList
	run.call(tools.ToolUsers, tools.AccountArgs{Account: "acme"}, &users)
	index := slices.IndexFunc(users.Users, func(user tools.User) bool { return user.User == "daffy" })
	if index < 0 || !slices.Contains(users.Users[index].Keys, extraKey) {
		t.Fatalf("central list did not read the added key: %+v", users)
	}
	call("box-no-admin", daffy, tools.ToolCreateInvite, box.NoArgs{}, trail.ResultRefused, box.OnlyAdmin)
	call("box-lock-no-admin", bugs, tools.ToolLockUser, box.UserArgs{User: "daffy"}, trail.ResultRefused, box.OnlyAdmin)
	call("box-keys", daffy, box.ToolKeys, box.NoArgs{}, "", "")
	missingKey := boxRequest(protocol.MethodToolsCall, map[string]any{"name": tools.ToolAddKey, "arguments": struct{}{}},
		"box-missing-key", daffy, http.StatusOK)
	if !missingKey.IsError {
		t.Fatal("box add_key accepted a call with no key argument")
	}
	requests["box-missing-key"].tool = tools.ToolAddKey
	boxRequest(protocol.MethodToolsCall, map[string]any{"name": tools.ToolAddKey, "arguments": box.KeyArgs{Key: extraKey}},
		"box-unresolved-uid", store.User{}, http.StatusForbidden)

	run.call(tools.ToolSetAdmin, tools.SetAdminArgs{Account: "acme", User: "daffy", Admin: true}, nil)
	checkList(boxRequest(protocol.MethodToolsList, nil, "box-admin-tools", daffy, http.StatusOK), catalogs[2],
		append(slices.Clone(userTools), tools.ToolUsers, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser,
			tools.ToolSetAdmin, tools.ToolCreateInvite, tools.ToolListInvites, tools.ToolRevokeInvite), 3)
	writeAccountsURL(t, path, "https://accounts.two.test/")
	call("box-admin-add-key", daffy, tools.ToolAddKey, box.KeyArgs{Key: adminKey}, trail.ResultOK, "")
	call("box-admin-remove-key", daffy, tools.ToolRemoveKey, box.KeyArgs{Key: adminKey}, trail.ResultOK, "")
	reply := call("box-make-invite", daffy, tools.ToolCreateInvite, box.NoArgs{}, trail.ResultOK, "")
	var made tools.InviteMade
	if err := json.Unmarshal(reply.Content, &made); err != nil || !strings.HasPrefix(made.Link, "https://accounts.two.test/join?token=") {
		t.Fatalf("box invite after rewriting service URL: %+v (%v)", made, err)
	}
	requests["box-make-invite"].detail = telemetry.Event{Name: trail.EventInviteCreated, Attrs: telemetry.Attrs{
		"invite": made.Invite, "account": made.Account, "host": host.Host, "admin": false,
	}}
	call("box-list-invites", daffy, tools.ToolListInvites, box.NoArgs{}, "", "")
	reply = call("box-revoke-invite", daffy, tools.ToolRevokeInvite, tools.InviteArgs{Invite: made.Invite}, trail.ResultOK, "")
	var revoked tools.Invite
	if err := json.Unmarshal(reply.Content, &revoked); err != nil {
		t.Fatal(err)
	}
	requests["box-revoke-invite"].detail = telemetry.Event{Name: trail.EventInviteRevoked, Attrs: telemetry.Attrs{
		"invite": revoked.Invite, "account": revoked.Account, "host": host.Host,
	}}
	call("box-revoke-absent", daffy, tools.ToolRevokeInvite, tools.InviteArgs{Invite: made.Invite}, trail.ResultRefused, tools.NoInvite)

	for _, admin := range []bool{true, false} {
		requestID := fmt.Sprintf("box-set-admin-%t", admin)
		args := box.SetAdminArgs{User: "bugs", Admin: admin}
		reply := call(requestID, daffy, tools.ToolSetAdmin, args, trail.ResultOK, "")
		var changed tools.UserChange
		if err := json.Unmarshal(reply.Content, &changed); err != nil || changed.User != args.User || changed.Admin != admin {
			t.Fatalf("set_admin reply: %+v (%v)", changed, err)
		}
		requests[requestID].detail = telemetry.Event{Name: trail.EventAdminChanged, Attrs: telemetry.Attrs{
			"account": daffy.Account, "host": host.Host, "user": args.User, "admin": args.Admin,
		}}
	}
	call("box-set-admin-unchanged", daffy, tools.ToolSetAdmin, box.SetAdminArgs{User: "bugs"}, trail.ResultOK, "")
	call("box-set-admin-self", daffy, tools.ToolSetAdmin, box.SetAdminArgs{User: "daffy"}, trail.ResultRefused, box.NotSelf)
	call("box-users", daffy, tools.ToolUsers, box.NoArgs{}, "", "")
	call("box-lock", daffy, tools.ToolLockUser, box.UserArgs{User: "bugs"}, trail.ResultOK, "")
	call("box-unlock", daffy, tools.ToolUnlockUser, box.UserArgs{User: "bugs"}, trail.ResultOK, "")
	call("box-lock-again", daffy, tools.ToolLockUser, box.UserArgs{User: "bugs"}, trail.ResultOK, "")
	call("box-delete", daffy, tools.ToolDeleteUser, box.UserArgs{User: "bugs"}, trail.ResultOK, "")

	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	events := run.capture.Events()
	assertBoxTrail(t, events, requests)
	assertTrailSecrecy(t, events, made)
}

func assertBoxTrail(t *testing.T, events []telemetry.Event, requests map[string]*boxTrailExpectation) {
	t.Helper()
	byRequest := make(map[string][]telemetry.Event)
	for _, event := range events {
		if _, ok := requests[event.RequestID]; ok {
			byRequest[event.RequestID] = append(byRequest[event.RequestID], event)
		}
	}
	for id, want := range requests {
		got := byRequest[id]
		if len(got) < 2 {
			t.Errorf("%s: missing request pair: %+v", id, got)
			continue
		}
		start, finish := got[0], got[len(got)-1]
		if start.Name != "request.started" || start.User != want.headerUser ||
			!reflect.DeepEqual(start.Attrs, telemetry.Attrs{"method": http.MethodPost, "path": protocol.BoxPath}) ||
			finish.Name != "request.finished" || finish.User != want.headerUser || fmt.Sprint(finish.Attrs["status"]) != strconv.Itoa(want.status) {
			t.Errorf("%s: request pair: %+v, %+v", id, start, finish)
		}

		var changes []string
		for _, event := range got[1 : len(got)-1] {
			if event.User != want.user {
				t.Errorf("%s: %s actor = %q, want %q", id, event.Name, event.User, want.user)
			}
			switch event.Name {
			case trail.EventChangeFinished:
				changes = append(changes, event.Name)
				if event.Attrs["tool"] != want.tool || event.Attrs["result"] != want.result {
					t.Errorf("%s: box change = %+v, want tool %q and result %q", id, event.Attrs, want.tool, want.result)
				}
			case trail.EventInviteCreated, trail.EventInviteRevoked, trail.EventAdminChanged:
				changes = append(changes, event.Name)
				if !reflect.DeepEqual(event.Attrs, want.detail.Attrs) {
					t.Errorf("%s: %s fields = %+v, want %+v", id, event.Name, event.Attrs, want.detail.Attrs)
				}
			case "tool.called":
				changes = append(changes, event.Name)
				if event.Attrs["tool"] != want.tool {
					t.Errorf("%s: tool.called tool = %v, want %q", id, event.Attrs["tool"], want.tool)
				}
			case trail.EventOperationStarted, trail.EventOperationFinished, trail.EventHostChanged, trail.EventAccountChanged,
				trail.EventInviteUsed, "request.started", "request.finished":
				t.Errorf("%s: unexpected event %+v", id, event)
			}
		}
		var order []string
		if want.detail.Name != "" {
			order = append(order, want.detail.Name)
		}
		if want.result != "" {
			order = append(order, trail.EventChangeFinished)
		}
		if want.tool != "" {
			order = append(order, "tool.called")
		}
		if !slices.Equal(changes, order) {
			t.Errorf("%s: box events = %v, want %v", id, changes, order)
		}
	}
}
