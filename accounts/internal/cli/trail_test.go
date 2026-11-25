package cli_test

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/join"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// R-ZFKT-VWM0
func TestRunReadAndPageTrail(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "accountsd"), []byte("chosen daemon"), 0600); err != nil {
		t.Fatal(err)
	}
	clock := newDrivenClock()
	run := startIntegration(t, integrationOptions{
		now: clock.now, after: clock.after, sleep: clock.sleep,
		executable: func() (string, error) { return filepath.Join(bin, "accounts"), nil },
	})
	run.call(tools.ToolCreateHost, struct{}{}, nil)
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, tools.HostArgs{Host: "host01"}, &host)
		return host.State == store.HostRunning
	})
	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: "host01"}, nil)
	run.call(tools.ToolAddUser, tools.UserKeyArgs{Account: "acme", User: "daffy", Key: validKey()}, nil)

	requests := map[string]bool{}
	calls := readCalls()
	successes := len(calls)
	calls = append(calls,
		catalogCall{tools.ToolHost, map[string]any{"host": "missing"}},
		catalogCall{tools.ToolAccount, map[string]any{"account": "missing"}},
		catalogCall{tools.ToolUsers, map[string]any{"account": "missing"}},
	)
	for i, call := range calls {
		for _, viewer := range []string{"owner", ""} {
			id := fmt.Sprintf("read-%d-%s", i, viewer)
			caller := identity.Caller{UserID: viewer, RequestID: id}
			if viewer != "" {
				caller.Email = run.email
			}
			result, err := run.callAs(call.name, call.args, caller)
			if viewer == "" {
				var failed *mcp.HTTPError
				if !errors.As(err, &failed) {
					t.Fatalf("guest %s call: %v, want an HTTP refusal", call.name, err)
				}
			} else if err != nil || result.IsError() != (i >= successes) {
				t.Fatalf("%s call: %+v (%v)", call.name, result, err)
			}
			requests[id] = false
		}
	}
	for _, viewer := range []string{"", "owner"} {
		for _, path := range []string{"/", "/hosts/host01", "/tools?x=1", "/about", protocol.BinaryPath, "/does-not-exist", join.Path, protocol.BoxPath} {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodPost} {
				if method == http.MethodPost && path == protocol.BoxPath {
					continue
				}
				id := fmt.Sprintf("read-page-%d", len(requests))
				run.request(method, path, viewer, id)
				requests[id] = false
			}
		}
	}
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	for _, event := range run.capture.Events() {
		if _, ok := requests[event.RequestID]; !ok {
			continue
		}
		switch event.Name {
		case "request.finished":
			requests[event.RequestID] = true
		case trail.EventChangeFinished, trail.EventOperationStarted, trail.EventOperationFinished, trail.EventHostChanged, trail.EventAccountChanged:
			t.Errorf("%s: unexpected event %s", event.RequestID, event.Name)
		}
	}
	for id, finished := range requests {
		if !finished {
			t.Errorf("%s: missing request.finished", id)
		}
	}
}
