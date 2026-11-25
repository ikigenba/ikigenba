package tools_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type callCase struct {
	name string
	args any
	want string
}

// R-ESC3-AJ89 R-Z1SS-ZWHY R-ZOFG-PH5V R-Y4VI-O3H7 R-D1GS-WBBJ
func TestDisabledAndUnavailableTools(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			f := setupConfigured(t, configured)
			f.serveLoopback()
			f.d.SetFailing(true)
			for _, e := range catalog() {
				args := validArgs(e)
				switch e.wire {
				case tools.ToolCreateHost:
					args = map[string]any{}
				case tools.ToolResizeHost:
					args = map[string]any{"host": "host01", "type": valid.DefaultType}
				case tools.ToolAddUser, tools.ToolAddKey, tools.ToolRemoveKey:
					args["key"] = key(1)
				}
				want := tools.NotConfigured
				if configured {
					want = store.Unreachable
				}
				refusal(t, f.call(e.wire, args), want)
			}
			badSize, badType, zero := 0, "invalid.type", 0
			for _, c := range []callCase{
				{tools.ToolCreateHost, tools.CreateHostArgs{Type: &badType, RootGB: &badSize}, tools.TypeNotAllowed},
				{tools.ToolCreateHost, tools.CreateHostArgs{RootGB: &badSize}, tools.SizeOutOfRange},
				{tools.ToolResizeHost, tools.ResizeHostArgs{Host: "host01"}, tools.NothingToChange},
				{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "bad name"}, tools.InvalidName},
				{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "www"}, tools.ReservedName},
				{tools.ToolAddUser, tools.UserKeyArgs{User: "bad name"}, tools.InvalidName},
				{tools.ToolAddUser, tools.UserKeyArgs{User: "root"}, tools.ReservedName},
				{tools.ToolAddUser, tools.UserKeyArgs{User: "valid-user", Key: "invalid"}, tools.InvalidKey},
				{tools.ToolUnbanIP, tools.UnbanArgs{IP: "192.0.2.0/24"}, tools.InvalidAddress},
				{tools.ToolAllowIP, tools.HostIPArgs{Host: "missing", IP: "invalid"}, tools.InvalidAddress},
				{tools.ToolDisallowIP, tools.HostIPArgs{Host: "missing", IP: "10.0.0.0/8"}, tools.PrefixTooWide},
			} {
				want := tools.NotConfigured
				if configured {
					want = c.want
				}
				refusal(t, f.call(c.name, c.args), want)
			}
			if !configured {
				refusal(t, f.call(tools.ToolHosts, tools.HostsArgs{Limit: &zero}), tools.NotConfigured)
			}
			if len(f.cloud.lookups) != 0 || f.wakes != 0 {
				t.Fatalf("cloud lookups %+v, wakes %d", f.cloud.lookups, f.wakes)
			}
			for _, e := range toolEvents(t, f) {
				if e.Name != trail.EventChangeFinished {
					t.Fatalf("unexpected operation event %+v", e)
				}
			}
			f.d.SetFailing(false)
			host, err := f.st.ListHosts(context.Background(), 0, 1)
			if err != nil || len(host) != 0 {
				t.Fatalf("refusal changed hosts: %v (%v)", host, err)
			}
		})
	}
}

func toolEvents(t *testing.T, f *fixture) []telemetry.Event {
	t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var events []telemetry.Event
	for _, e := range f.capture.Events() {
		switch e.Name {
		case trail.EventChangeFinished, trail.EventOperationFinished, trail.EventOperationStarted, trail.EventHostChanged, trail.EventAccountChanged:
			events = append(events, e)
		}
	}
	return events
}

// R-ZCRW-FU67 R-XZZX-50IF R-104G-66SU R-1OF3-PKX6 R-1PN0-3CNV
func TestHostOperations(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	missing := tools.HostArgs{Host: "missing"}
	for _, name := range []string{tools.ToolStopHost, tools.ToolStartHost, tools.ToolRetryHost, tools.ToolDeleteHost} {
		refusal(t, f.call(name, missing), tools.NoHost)
	}
	created := answer[tools.HostChange](t, f.call(tools.ToolCreateHost, tools.CreateHostArgs{}))
	host := tools.HostArgs{Host: created.Host}
	h := f.host(host.Host)
	if created.Host != "host01" || created.State != store.HostProvisioning || h.State != created.State ||
		h.Type != valid.DefaultType || h.RootGB != valid.DefaultRootGB || h.DataGB != valid.DefaultDataGB || h.CreatedBy != f.caller.UserID {
		t.Fatalf("create answer %+v, stored %+v", created, h)
	}
	if op := f.operation(created.Operation); op.Host != h.Name || op.Kind != store.OpCreate || op.Status != store.OpRunning || op.Caller.UserID != f.caller.UserID {
		t.Fatalf("create operation %+v", op)
	}
	instance := "t4g.large"
	badType := "not-an-instance-type"
	tooSmall, tooBig := valid.MinDataGB-1, valid.MaxRootGB+1
	for _, c := range []callCase{
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host}, tools.NothingToChange},
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host, Type: &badType, DataGB: &tooSmall}, tools.TypeNotAllowed},
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host, RootGB: &tooBig}, tools.SizeOutOfRange},
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host, DataGB: &tooSmall}, tools.SizeOutOfRange},
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: missing.Host, Type: &instance}, tools.NoHost},
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host, Type: &instance}, tools.HostBusy},
		{tools.ToolRetryHost, host, tools.HostNotFailed},
		{tools.ToolStartHost, host, tools.HostBusy},
		{tools.ToolStopHost, host, tools.HostBusy},
		{tools.ToolDeleteHost, host, tools.HostBusy},
	} {
		refusal(t, f.call(c.name, c.args), c.want)
	}
	if err := f.st.RecordStep(ctx, created.Operation, "source-step", 3, f.now); err != nil {
		t.Fatal(err)
	}
	f.fail(created.Operation, "cloud failed")
	for _, c := range []callCase{
		{tools.ToolStartHost, host, tools.HostFailed}, {tools.ToolStopHost, host, tools.HostFailed},
		{tools.ToolResizeHost, tools.ResizeHostArgs{Host: host.Host, Type: &instance}, tools.HostFailed},
	} {
		refusal(t, f.call(c.name, c.args), c.want)
	}
	retry := answer[tools.HostChange](t, f.call(tools.ToolRetryHost, host))
	retried := f.operation(retry.Operation)
	if retry != created || retried.Step != "source-step" || retried.Generation != 3 || retried.Status != store.OpRunning {
		t.Fatalf("retry %+v, operation %+v", retry, retried)
	}
	f.finish(retry.Operation, store.Update{RootVolume: "root-vol", DataVolume: "data-vol"})
	refusal(t, f.call(tools.ToolStartHost, host), tools.HostNotStopped)
	sameSize, smaller := valid.DefaultDataGB, valid.DefaultDataGB-1
	refusal(t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: h.Name, DataGB: &sameSize}), tools.NothingToChange)
	refusal(t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: h.Name, Type: &instance, DataGB: &smaller}), tools.SizeShrinks)
	rootSize, dataSize := valid.DefaultRootGB+1, valid.DefaultDataGB+10
	for range store.MaxGrows {
		if err := f.st.NoteGrow(ctx, "root-vol", f.now); err != nil {
			t.Fatal(err)
		}
	}
	refusal(t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: h.Name, RootGB: &rootSize}), tools.GrowLimit)
	f.now = f.now.Add(store.GrowWindow + time.Second)
	resized := answer[tools.HostResize](t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: h.Name, Type: &instance, RootGB: &rootSize, DataGB: &dataSize}))
	if resized.Host != h.Name || resized.State != store.HostResizing || !resized.Restarts {
		t.Fatal(resized)
	}
	wantTarget := store.Target{Type: instance, RootGB: rootSize, DataGB: dataSize}
	if op := f.operation(resized.Operation); op.Kind != store.OpResize || op.Status != store.OpRunning || op.Target != wantTarget || op.Caller.UserID != f.caller.UserID {
		t.Fatalf("resize operation %+v", op)
	}
	f.finish(resized.Operation, store.Update{Type: instance, DataGB: dataSize, RootGB: rootSize})
	dataSize++
	resizeOnlyData := answer[tools.HostResize](t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: h.Name, Type: &instance, DataGB: &dataSize}))
	op := f.operation(resizeOnlyData.Operation)
	if resizeOnlyData.Restarts || op.Target != (store.Target{DataGB: dataSize}) {
		t.Fatal(resizeOnlyData, op)
	}
	f.finish(resizeOnlyData.Operation, store.Update{DataGB: dataSize})
	stopped := checkHostOperation(t, f, tools.ToolStopHost, host, store.OpStop, store.HostStopping)
	f.finish(stopped, store.Update{})
	refusal(t, f.call(tools.ToolStopHost, host), tools.HostNotRunning)
	newType := "t4g.xlarge"
	stoppedResize := answer[tools.HostResize](t, f.call(tools.ToolResizeHost, tools.ResizeHostArgs{Host: h.Name, Type: &newType}))
	if stoppedResize.Restarts {
		t.Fatal(stoppedResize)
	}
	f.finish(stoppedResize.Operation, store.Update{Type: newType})
	started := checkHostOperation(t, f, tools.ToolStartHost, host, store.OpStart, store.HostStarting)
	f.finish(started, store.Update{})
	f.seedAccount("acme", h.Name)
	refusal(t, f.call(tools.ToolDeleteHost, host), tools.HostHasAccounts)
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, tools.AccountArgs{Account: "acme"}))
	checkHostOperation(t, f, tools.ToolDeleteHost, host, store.OpDelete, store.HostDeleting)

	custom := answer[tools.HostChange](t, f.call(tools.ToolCreateHost, tools.CreateHostArgs{Type: &instance, RootGB: &rootSize, DataGB: &dataSize}))
	if actual := f.host(custom.Host); actual.Type != instance || actual.RootGB != rootSize || actual.DataGB != dataSize {
		t.Fatal(actual)
	}
}

func checkHostOperation(t *testing.T, f *fixture, name string, args tools.HostArgs, kind, state string) string {
	t.Helper()
	out := answer[tools.HostChange](t, f.call(name, args))
	op := f.operation(out.Operation)
	if out.Host != args.Host || out.State != state || f.host(args.Host).State != state ||
		op.Kind != kind || op.Status != store.OpRunning || op.Caller.UserID != f.caller.UserID {
		t.Fatalf("host operation answer %+v, stored %+v", out, op)
	}
	return op.ID
}

// R-XV4B-LXJN R-XYS0-R8RQ R-1GCH-JWOD
func TestCreateAccountDNS(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	host, _ := f.seedHost(true)
	place := tools.CreateAccountArgs{Account: "acme", Host: host.Name}
	f.seedAccount("held", host.Name)
	answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: "held", User: "held-user", Key: key(1)}))
	deleting, _ := f.seedHost(true)
	checkHostOperation(t, f, tools.ToolDeleteHost, tools.HostArgs{Host: deleting.Name}, store.OpDelete, store.HostDeleting)
	for _, c := range []callCase{
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "bad name", Host: host.Name}, tools.InvalidName},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "www", Host: host.Name}, tools.ReservedName},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "held", Host: "missing"}, tools.NoHost},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "held", Host: deleting.Name}, tools.HostGone},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "held", Host: host.Name}, tools.NameTaken},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "held-user", Host: host.Name}, tools.NameTaken},
	} {
		refusal(t, f.call(c.name, c.args), c.want)
	}
	if len(f.cloud.lookups) != 0 {
		t.Fatal(f.cloud.lookups)
	}
	f.cloud.find = func(context.Context, string, string) ([]cloud.Record, error) {
		return []cloud.Record{{Type: "TXT"}}, nil
	}
	earliest := time.Now()
	refusal(t, f.call(tools.ToolCreateAccount, place), tools.NameHasRecord)
	if len(f.cloud.lookups) != 1 {
		t.Fatal(f.cloud.lookups)
	}
	got := f.cloud.lookups[0]
	if got.name != place.Account+"."+f.hosts.Domain || got.zone != f.hosts.ZoneID ||
		got.deadline.Before(earliest) || got.deadline.After(time.Now().Add(tools.DNSCheckTimeout)) {
		t.Fatalf("DNS lookup %+v", got)
	}
	f.cloud.find = func(context.Context, string, string) ([]cloud.Record, error) { return nil, context.DeadlineExceeded }
	refusal(t, f.call(tools.ToolCreateAccount, place), tools.CloudFailed)
	if len(f.cloud.lookups) != 2 {
		t.Fatal(f.cloud.lookups)
	}
	shortDeadline := time.Now().Add(4 * time.Second)
	short, cancel := context.WithDeadline(ctx, shortDeadline)
	defer cancel()
	refusal(t, f.callContext(short, tools.ToolCreateAccount, place), tools.CloudFailed)
	if len(f.cloud.lookups) != 3 || !f.cloud.lookups[2].deadline.Equal(shortDeadline) {
		t.Fatalf("did not use the shorter request deadline: %v", f.cloud.lookups)
	}

	// Another writer can reserve the same name while DNS is in flight.
	f.cloud.find = func(context.Context, string, string) ([]cloud.Record, error) {
		_, _, err := f.st.PlaceAccount(ctx, place.Account, place.Host, f.caller)
		return nil, err
	}
	refusal(t, f.call(tools.ToolCreateAccount, place), tools.NameTaken)
	if len(f.cloud.lookups) != 4 {
		t.Fatal(f.cloud.lookups)
	}
	f.cloud.find = nil
	place.Account = "fresh"
	good := answer[tools.Account](t, f.call(tools.ToolCreateAccount, place))
	a, err := f.st.Account(ctx, place.Account)
	if err != nil {
		t.Fatal(err)
	}
	op := f.operation(a.Operation)
	if len(f.cloud.lookups) != 5 || good != (tools.Account{Account: a.Slug, Host: a.Host, DNSName: a.Slug + "." + f.hosts.Domain,
		State: store.AccountPending, Created: a.Created.Format(time.RFC3339), CreatedBy: f.caller.UserID}) ||
		op.Kind != store.OpPlace || op.Status != store.OpRunning || op.Host != a.Host || op.Account != a.Slug {
		t.Fatalf("placed account %+v, operation %+v, lookups %+v", good, op, f.cloud.lookups)
	}

	full, _ := f.seedHost(true)
	for i := range valid.MaxAccountsPerHost {
		f.seedAccount(fmt.Sprintf("company-%02d", i), full.Name)
	}
	refusal(t, f.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "unplaced", Host: full.Name}), tools.HostFull)
	if len(f.cloud.lookups) != 5 {
		t.Fatal(f.cloud.lookups)
	}

	lastSlot, _ := f.seedHost(true)
	for i := range valid.MaxAccountsPerHost - 1 {
		f.seedAccount(fmt.Sprintf("tenant-%02d", i), lastSlot.Name)
	}
	f.cloud.find = func(context.Context, string, string) ([]cloud.Record, error) {
		_, _, err := f.st.PlaceAccount(ctx, "last-slot", lastSlot.Name, f.caller)
		return nil, err
	}
	refusal(t, f.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "overflow", Host: lastSlot.Name}), tools.HostFull)
	aboutToDelete, _ := f.seedHost(true)
	f.cloud.find = func(context.Context, string, string) ([]cloud.Record, error) {
		_, _, err := f.st.StartHostOp(ctx, aboutToDelete.Name, store.OpDelete, store.Target{}, f.caller)
		return nil, err
	}
	refusal(t, f.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "too-late", Host: aboutToDelete.Name}), tools.HostGone)
	if len(f.cloud.lookups) != 7 {
		t.Fatal(f.cloud.lookups)
	}
}

// R-0XON-ENBG
func TestRemoveAccount(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		f := setup(t)
		f.serveLoopback()
		refusal(t, f.call(tools.ToolRemoveAccount, tools.AccountArgs{Account: "missing"}), tools.NoAccount)
	})
	t.Run("has users", testRemoveAccountWithUsers)
	for _, state := range []string{store.AccountPending, store.AccountActive, store.AccountFailed} {
		t.Run(state, func(t *testing.T) {
			testRemoveAccountFromState(t, state)
		})
	}
}

func testRemoveAccountFromState(t *testing.T, state string) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(false)
	args := tools.AccountArgs{Account: "acme"}
	a, place := f.seedAccount(args.Account, h.Name)
	creator := f.caller.UserID
	f.caller.UserID = "removing-caller"
	wantPlaceStatus := store.OpCancelled
	switch state {
	case store.AccountActive:
		f.finish(place.ID, store.Update{})
		wantPlaceStatus = store.OpDone
	case store.AccountFailed:
		f.fail(place.ID, "place failed")
		wantPlaceStatus = store.OpFailed
	}
	want := tools.Account{
		Account: args.Account, Host: h.Name, DNSName: args.Account + "." + f.hosts.Domain,
		State: store.AccountRemoving, Created: a.Created.UTC().Format(time.RFC3339), CreatedBy: creator,
	}
	remove := func(previous, wantPreviousStatus string) string {
		t.Helper()
		if got := answer[tools.Account](t, f.call(tools.ToolRemoveAccount, args)); got != want {
			t.Fatalf("removal answer %+v, want %+v", got, want)
		}
		ctx := t.Context()
		d, err := db.Open(ctx, db.Config{Path: f.path, Migrations: accounts.Migrations(),
			Now: func() time.Time { return f.now }, Service: "accounts", Stderr: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		}()
		reopened := store.New(d, store.Config{Now: func() time.Time { return f.now }})
		stored, err := reopened.Account(ctx, args.Account)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != store.AccountRemoving || stored.Reason != "" || stored.CreatedBy != creator || stored.Operation == previous {
			t.Fatalf("stored account after removal %+v", stored)
		}
		op, err := reopened.Operation(ctx, stored.Operation)
		if err != nil {
			t.Fatal(err)
		}
		if op.Kind != store.OpUnplace || op.Status != store.OpRunning || op.Account != args.Account ||
			op.Host != h.Name || op.Caller.UserID != f.caller.UserID {
			t.Fatalf("stored removal by %q: %+v", f.caller.UserID, op)
		}
		prior, err := reopened.Operation(ctx, previous)
		if err != nil {
			t.Fatal(err)
		}
		if prior.Status != wantPreviousStatus {
			t.Fatalf("prior operation %+v, want status %q", prior, wantPreviousStatus)
		}
		return op.ID
	}
	first := remove(place.ID, wantPlaceStatus)
	if state == store.AccountFailed {
		refusal(t, f.call(tools.ToolRemoveAccount, args), tools.AccountRemoving)
		f.fail(first, "unplace failed")
		f.caller.UserID = "retrying-caller"
		remove(first, store.OpFailed)
	}
}

func testRemoveAccountWithUsers(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(false)
	active, place := f.seedAccount("active", h.Name)
	f.finish(place.ID, store.Update{})
	added := answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: active.Slug, User: "daffy", Key: key(1)}))
	user := tools.UserArgs{Account: added.Account, User: added.User}
	unplace := tools.AccountArgs{Account: added.Account}
	refusal(t, f.call(tools.ToolRemoveAccount, unplace), tools.AccountHasUsers)
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, user))
	refusal(t, f.call(tools.ToolRemoveAccount, unplace), tools.AccountHasUsers)
	answer[tools.UserChange](t, f.call(tools.ToolDeleteUser, user))
	refusal(t, f.call(tools.ToolRemoveAccount, unplace), tools.AccountHasUsers)
}

// R-1HKD-XOF2 R-YAZ0-KY6O R-1ISA-BG5R R-1K06-P7WG
func TestUserAndKeyChanges(t *testing.T) {
	f := setup(t)
	h, _ := f.seedHost(false)
	a, _ := f.seedAccount("acme", h.Name)
	f.seedAccount("beta", h.Name)
	f.seedAccount("leaving", h.Name)
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, tools.AccountArgs{Account: "leaving"}))
	arg := tools.UserArgs{Account: a.Slug, User: "daffy"}
	add := tools.UserKeyArgs{Account: arg.Account, User: arg.User, Key: "  " + strings.Replace(key(1), " ", "\t", 1) + "\t  "}
	for _, c := range []callCase{
		{tools.ToolAddUser, tools.UserKeyArgs{Account: "missing", User: "daffy", Key: key(1)}, tools.NoAccount},
		{tools.ToolAddUser, tools.UserKeyArgs{Account: "leaving", User: "acme", Key: key(1)}, tools.AccountRemoving},
		{tools.ToolAddUser, tools.UserKeyArgs{Account: "beta", User: "acme", Key: key(1)}, tools.NameTaken},
	} {
		refusal(t, f.call(c.name, c.args), c.want)
	}
	active := tools.UserChange{Account: a.Slug, User: arg.User, State: store.UserActive, Pending: true}
	if actual := answer[tools.UserChange](t, f.call(tools.ToolAddUser, add)); actual != active {
		t.Fatalf("add user %+v", actual)
	}
	created := f.user(arg.Account, arg.User)
	if created.Version != f.host(h.Name).Version || created.CreatedBy != f.caller.UserID ||
		len(created.Keys) != 1 || created.Keys[0].Line != key(1) {
		t.Fatal(created)
	}
	refusal(t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: "beta", User: "daffy", Key: key(1)}), tools.NameTaken)

	for _, name := range []string{tools.ToolAddKey, tools.ToolRemoveKey, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser} {
		isKey := name == tools.ToolAddKey || name == tools.ToolRemoveKey
		for _, check := range []struct{ account, user, want string }{
			{"missing", "daffy", tools.NoAccount}, {"acme", "absent", tools.NoUser}, {"beta", "daffy", tools.NotInAccount},
		} {
			var args any = tools.UserArgs{Account: check.account, User: check.user}
			if isKey {
				args = tools.UserKeyArgs{Account: check.account, User: check.user, Key: key(1)}
			}
			refusal(t, f.call(name, args), check.want)
		}
		if isKey {
			refusal(t, f.call(name, tools.UserKeyArgs{Account: "missing", User: "absent", Key: "invalid"}), tools.InvalidKey)
		}
	}
	refusal(t, f.call(tools.ToolDeleteUser, arg), tools.UserNotLocked)
	otherComment := func(i byte) tools.UserKeyArgs {
		return tools.UserKeyArgs{Account: arg.Account, User: arg.User, Key: strings.Replace(key(i), "comment with spaces", "different comment", 1)}
	}
	if duplicate := answer[tools.UserChange](t, f.call(tools.ToolAddKey, otherComment(1))); duplicate != active ||
		!reflect.DeepEqual(f.user(arg.Account, arg.User), created) || f.host(h.Name).Version != created.Version {
		t.Fatalf("duplicate key changed the user: %+v", duplicate)
	}
	refusal(t, f.call(tools.ToolRemoveKey, otherComment(0)), tools.KeyNotHeld)
	for i := 2; i <= valid.MaxKeysPerUser; i++ {
		if got := answer[tools.UserChange](t, f.call(tools.ToolAddKey, otherComment(byte(i)))); got != active {
			t.Fatal(got)
		}
	}
	refusal(t, f.call(tools.ToolAddKey, otherComment(0)), tools.TooManyKeys)
	for i := 1; i <= valid.MaxKeysPerUser; i++ {
		different := otherComment(byte(i))
		different.Key = key(byte(i))
		if got := answer[tools.KeyRemoved](t, f.call(tools.ToolRemoveKey, different)); got != (tools.KeyRemoved{
			User: active.User, Account: active.Account, State: active.State, Pending: active.Pending, KeysLeft: valid.MaxKeysPerUser - i,
		}) {
			t.Fatal(got)
		}
	}
	if got := f.user(arg.Account, arg.User); len(got.Keys) != 0 {
		t.Fatal(got)
	}

	for _, c := range []struct{ name, state string }{
		{tools.ToolUnlockUser, store.UserActive}, {tools.ToolLockUser, store.UserLocked},
		{tools.ToolUnlockUser, store.UserActive}, {tools.ToolLockUser, store.UserLocked},
		{tools.ToolDeleteUser, store.UserRemoving},
	} {
		want := tools.UserChange{User: active.User, Account: active.Account, State: c.state, Pending: true}
		if got := answer[tools.UserChange](t, f.call(c.name, arg)); got != want {
			t.Fatal(got)
		}
		before := f.host(h.Name).Version
		if got := answer[tools.UserChange](t, f.call(c.name, arg)); got != want || f.host(h.Name).Version != before {
			t.Fatalf("already in state %s changed state or version: %+v", c.state, got)
		}
	}
	for _, c := range []callCase{
		{tools.ToolLockUser, arg, tools.UserRemoving}, {tools.ToolUnlockUser, arg, tools.UserRemoving},
		{tools.ToolAddKey, otherComment(1), tools.UserRemoving}, {tools.ToolRemoveKey, otherComment(1), tools.UserRemoving},
	} {
		refusal(t, f.call(c.name, c.args), c.want)
	}
}

// R-1HKD-XOF2
func TestAccountUserCapacity(t *testing.T) {
	f := setup(t)
	h, _ := f.seedHost(false)
	f.seedAccount("acme", h.Name)
	f.seedAccount("beta", h.Name)
	canonical, err := valid.ParseKey(key(1))
	if err != nil {
		t.Fatal(err)
	}
	k := store.Key{Type: canonical.Type, Body: canonical.Body, Line: canonical.Line()}
	for i := range valid.MaxUsersPerHost {
		if _, err := f.st.AddUser(context.Background(), "acme", fmt.Sprintf("person-%03d", i), k, f.caller); err != nil {
			t.Fatal(err)
		}
	}
	refusal(t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: "beta", User: "overflow", Key: k.Line}), tools.AccountFull)
}

// R-FMTK-USRM R-FO1H-8KIB R-FP9D-MC90
func TestChangeTrailAndWake(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	ctx := context.Background()
	var ready []map[string]bool
	f.onWake = func() {
		ops, err := f.st.Runnable(ctx, f.now, "", 25)
		if err != nil {
			t.Error(err)
			return
		}
		observed := make(map[string]bool)
		for _, op := range ops {
			observed[op.ID] = true
		}
		ready = append(ready, observed)
	}
	host := answer[tools.HostChange](t, f.call(tools.ToolCreateHost, tools.CreateHostArgs{}))
	f.fail(host.Operation, "cloud refused create")
	answer[tools.HostChange](t, f.call(tools.ToolRetryHost, tools.HostArgs{Host: host.Host}))
	place := tools.CreateAccountArgs{Account: "acme", Host: host.Host}
	answer[tools.Account](t, f.call(tools.ToolCreateAccount, place))
	p := readAccount(t, f, place.Account).Operation.Operation
	account := tools.AccountArgs{Account: place.Account}
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, account))
	removal := readAccount(t, f, account.Account).Operation.Operation
	f.fail(removal, "first removal failed")
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, account))
	retry := readAccount(t, f, account.Account).Operation.Operation

	f.seedAccount("beta", host.Host)
	user := tools.UserArgs{Account: "beta", User: "daffy"}
	answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: user.Account, User: user.User, Key: key(1)}))
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, user))
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, user))
	badType := "t4g.nano"
	refusal(t, f.call(tools.ToolCreateHost, tools.CreateHostArgs{Type: &badType}), tools.TypeNotAllowed)
	f.cloud.find = func(context.Context, string, string) ([]cloud.Record, error) {
		return nil, errors.New("provider failed")
	}
	refusal(t, f.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "other", Host: host.Host}), tools.CloudFailed)
	f.d.SetFailing(true)
	refusal(t, f.call(tools.ToolLockUser, user), store.Unreachable)

	wantOps := []string{host.Operation, host.Operation, p, removal, retry}
	if f.wakes != len(wantOps) || len(ready) != len(wantOps) {
		t.Fatalf("wake count %d, operations %v", f.wakes, ready)
	}
	for i, op := range wantOps {
		if !ready[i][op] {
			t.Fatalf("wake %d could not read committed operation %s", i, op)
		}
	}
	events := toolEvents(t, f)
	type expectedEvent struct {
		name  string
		attrs telemetry.Attrs
	}
	changed := func(tool, h, a, u, result string) expectedEvent {
		return expectedEvent{trail.EventChangeFinished, telemetry.Attrs{"tool": tool, "host": h, "account": a, "user": u, "ip": "", "result": result}}
	}
	started := func(id, kind, account string) expectedEvent {
		return expectedEvent{trail.EventOperationStarted, telemetry.Attrs{"operation": id, "kind": kind, "host": host.Host, "account": account}}
	}
	hostChanged := func(from string) expectedEvent {
		return expectedEvent{trail.EventHostChanged, telemetry.Attrs{"host": host.Host, "from": from, "to": store.HostProvisioning, "operation": host.Operation}}
	}
	accountChanged := func(from, to string) expectedEvent {
		return expectedEvent{trail.EventAccountChanged, telemetry.Attrs{"account": account.Account, "host": host.Host, "from": from, "to": to}}
	}
	want := []expectedEvent{
		hostChanged(""), started(host.Operation, store.OpCreate, ""), changed(tools.ToolCreateHost, host.Host, "", "", trail.ResultOK),
		hostChanged(store.HostFailed), started(host.Operation, store.OpCreate, ""), changed(tools.ToolRetryHost, host.Host, "", "", trail.ResultOK),
		accountChanged("", store.AccountPending), started(p, store.OpPlace, account.Account), changed(tools.ToolCreateAccount, host.Host, account.Account, "", trail.ResultOK),
		{trail.EventOperationFinished, telemetry.Attrs{"operation": p, "kind": store.OpPlace, "host": host.Host, "account": account.Account, "status": store.OpCancelled, "reason": ""}},
		accountChanged(store.AccountPending, store.AccountRemoving), started(removal, store.OpUnplace, account.Account), changed(tools.ToolRemoveAccount, host.Host, account.Account, "", trail.ResultOK),
		started(retry, store.OpUnplace, account.Account), changed(tools.ToolRemoveAccount, host.Host, account.Account, "", trail.ResultOK),
		changed(tools.ToolAddUser, host.Host, user.Account, user.User, trail.ResultOK),
		changed(tools.ToolLockUser, host.Host, user.Account, user.User, trail.ResultOK),
		changed(tools.ToolLockUser, host.Host, user.Account, user.User, trail.ResultOK),
		changed(tools.ToolCreateHost, "", "", "", trail.ResultRefused),
		changed(tools.ToolCreateAccount, host.Host, "other", "", trail.ResultFailed),
		changed(tools.ToolLockUser, "", user.Account, user.User, trail.ResultFailed),
	}
	if len(events) != len(want) {
		t.Fatalf("events %+v, want %+v", events, want)
	}
	for i, got := range events {
		expected := want[i]
		if got.Name != expected.name || !reflect.DeepEqual(got.Attrs, expected.attrs) || got.User != f.caller.UserID {
			t.Fatalf("event %d = %+v, want %+v", i, got, expected)
		}
	}
}

// R-K4OS-2680
func TestPendingAccountAndRemovingUserThroughTools(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	host := answer[tools.HostChange](t, f.call(tools.ToolCreateHost, tools.CreateHostArgs{}))
	if host.Host != "host01" {
		t.Fatalf("created host = %q, want host01", host.Host)
	}
	place := tools.CreateAccountArgs{Account: "acme", Host: host.Host}
	answer[tools.Account](t, f.call(tools.ToolCreateAccount, place))
	user := tools.UserArgs{Account: place.Account, User: "daffy"}
	k := key(2)
	answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: user.Account, User: user.User, Key: k}))
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, user))
	answer[tools.UserChange](t, f.call(tools.ToolDeleteUser, user))

	users := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{})).Users
	if len(users) != 1 || users[0].User != user.User || users[0].Account != user.Account ||
		users[0].State != store.UserRemoving || !users[0].Pending || !slices.Equal(users[0].Keys, []string{k}) {
		t.Fatalf("users = %+v", users)
	}
	args := tools.AccountArgs{Account: place.Account}
	account := answer[tools.AccountRead](t, f.call(tools.ToolAccount, args))
	if account.State != store.AccountPending || !slices.Equal(account.UserNames, []string{user.User}) {
		t.Fatalf("account = %+v", account)
	}
	hosts := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{})).Hosts
	if len(hosts) != 1 || hosts[0].Host != host.Host || hosts[0].Accounts != 1 {
		t.Fatalf("hosts = %+v", hosts)
	}
	refusal(t, f.call(tools.ToolRemoveAccount, args), tools.AccountHasUsers)
	f.finish(host.Operation, store.Update{})
	refusal(t, f.call(tools.ToolDeleteHost, tools.HostArgs{Host: host.Host}), tools.HostHasAccounts)
}

// R-ZOFG-PH5V
func TestClosedRecord(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []callCase{
		{tools.ToolHosts, tools.HostsArgs{}, store.Unreachable},
		{tools.ToolHost, tools.HostArgs{Host: "host01"}, store.Unreachable},
		{tools.ToolCreateHost, tools.CreateHostArgs{}, store.Unreachable},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: "host01"}, store.Unreachable},
		{tools.ToolAccounts, tools.AccountsArgs{}, store.Unreachable},
		{tools.ToolAccount, tools.AccountArgs{Account: "acme"}, store.Unreachable},
		{tools.ToolUsers, tools.UsersArgs{}, store.Unreachable},
		{tools.ToolAddUser, tools.UserKeyArgs{Account: "acme", User: "daffy", Key: key(1)}, store.Unreachable},
		{tools.ToolRemoveAccount, tools.AccountArgs{Account: "acme"}, store.Unreachable},
		{tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "invalid!"}, tools.InvalidName},
		{tools.ToolUnbanIP, tools.UnbanArgs{IP: "192.0.2.7"}, store.Unreachable},
		{tools.ToolAllowIP, tools.HostIPArgs{Host: "host01", IP: "192.0.2.7"}, store.Unreachable},
		{tools.ToolDisallowIP, tools.HostIPArgs{Host: "host01", IP: "192.0.2.7"}, store.Unreachable},
	} {
		refusal(t, f.call(c.name, c.args), c.want)
	}
	if f.wakes != 0 || len(f.cloud.lookups) != 0 {
		t.Fatal(f.wakes, f.cloud.lookups)
	}
}
