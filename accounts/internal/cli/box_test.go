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

// R-H9S1-98I9 R-YQ2D-PVDA
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
		random: rand.NewChaCha8([32]byte{6}), acceptNotify: true,
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
	u, err := store.New(d, store.Config{Now: clock.now}).User(t.Context(), "acme", "daffy")
	if closeErr := d.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	var next int
	boxRequest := func(method string, params any, requestID string) boxReply {
		t.Helper()
		next++
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
		query := url.Values{protocol.QueryUID: {strconv.FormatInt(u.UID, 10)}}.Encode()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, run.base+protocol.BoxPath+"?"+query, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(hostauth.HeaderName, base64.RawURLEncoding.EncodeToString([]byte(sts.String())))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-Id", "owner")
		req.Header.Set("X-User-Email", run.email)
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
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("box request %s answered %d: %s (%v)", method, res.StatusCode, body, err)
		}
		var reply struct {
			Result boxReply        `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(body, &reply); err != nil || reply.Error != nil {
			t.Fatalf("box reply = %s (%v)", body, err)
		}
		if reply.Result.IsError && !bytes.Contains(body, []byte(box.OnlyAdmin)) {
			t.Fatalf("unexpected box tool refusal: %s", body)
		}
		return reply.Result
	}
	call := func(tool string, args any, requestID string) boxReply {
		t.Helper()
		return boxRequest(protocol.MethodToolsCall, map[string]any{"name": tool, "arguments": args}, requestID)
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
	checkList(boxRequest(protocol.MethodToolsList, nil, "box-user-tools"), catalogs[1], userTools, 2)
	if call(tools.ToolAddKey, box.KeyArgs{Key: extraKey}, "box-add-key").IsError {
		t.Fatal("ordinary user could not add own key")
	}
	var users tools.UserList
	run.call(tools.ToolUsers, tools.AccountArgs{Account: "acme"}, &users)
	index := slices.IndexFunc(users.Users, func(user tools.User) bool { return user.User == "daffy" })
	if index < 0 || !slices.Contains(users.Users[index].Keys, extraKey) {
		t.Fatalf("central list did not read the added key: %+v", users)
	}
	if !call(tools.ToolCreateInvite, struct{}{}, "box-no-admin").IsError {
		t.Fatal("ordinary user could create an invite")
	}
	run.call(tools.ToolSetAdmin, tools.SetAdminArgs{Account: "acme", User: "daffy", Admin: true}, nil)
	checkList(boxRequest(protocol.MethodToolsList, nil, "box-admin-tools"), catalogs[2],
		append(slices.Clone(userTools), tools.ToolUsers, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser,
			tools.ToolSetAdmin, tools.ToolCreateInvite, tools.ToolListInvites, tools.ToolRevokeInvite), 3)
	writeAccountsURL(t, path, "https://accounts.two.test/")
	if call(tools.ToolAddKey, box.KeyArgs{Key: adminKey}, "box-admin-add-key").IsError {
		t.Fatal("admin could not add own key")
	}
	if call(tools.ToolRemoveKey, box.KeyArgs{Key: adminKey}, "box-admin-remove-key").IsError {
		t.Fatal("admin could not remove own key")
	}
	reply := call(tools.ToolCreateInvite, struct{}{}, "box-make-invite")
	var made tools.InviteMade
	if err := json.Unmarshal(reply.Content, &made); err != nil || !strings.HasPrefix(made.Link, "https://accounts.two.test/join/") {
		t.Fatalf("box invite after rewriting service URL: %+v (%v)", made, err)
	}
	for i, test := range []struct {
		name string
		args any
	}{
		{tools.ToolListInvites, struct{}{}},
		{tools.ToolRevokeInvite, tools.InviteArgs{Invite: made.Invite}},
		{tools.ToolSetAdmin, box.SetAdminArgs{User: "bugs", Admin: true}},
		{tools.ToolSetAdmin, box.SetAdminArgs{User: "bugs", Admin: false}},
		{tools.ToolLockUser, box.UserArgs{User: "bugs"}},
	} {
		if call(test.name, test.args, fmt.Sprintf("box-admin-request-%d", i)).IsError {
			t.Fatalf("%s refused an admin", test.name)
		}
	}
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	events := run.capture.Events()
	assertTrailSecrecy(t, events, made)
	var addEvents []telemetry.Event
	for _, event := range events {
		if event.RequestID == "box-add-key" {
			addEvents = append(addEvents, event)
		}
	}
	if len(addEvents) != 4 || addEvents[0].Name != "request.started" || addEvents[1].Name != trail.EventChangeFinished ||
		addEvents[2].Name != "tool.called" || addEvents[3].Name != "request.finished" ||
		addEvents[0].User != "owner" || addEvents[3].User != "owner" ||
		addEvents[1].User != box.ActorPrefix+"daffy" || addEvents[2].User != box.ActorPrefix+"daffy" ||
		addEvents[0].Attrs["path"] != protocol.BoxPath || addEvents[1].Attrs["tool"] != tools.ToolAddKey {
		t.Fatalf("box add_key trail = %+v", addEvents)
	}
}
