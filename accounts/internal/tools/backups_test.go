package tools_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func createActiveAccount(f *fixture, slug, host string) tools.AccountArgs {
	f.t.Helper()
	_, op := f.seedAccount(slug, host)
	f.finish(op.ID, store.Update{})
	return tools.AccountArgs{Account: slug}
}

func backupReport(f *fixture, host string, entries ...protocol.AccountBackup) {
	f.t.Helper()
	h := f.host(host)
	r := protocol.Report{InstanceID: h.InstanceID, Protocol: protocol.Version, Backups: entries}
	if _, err := f.st.RecordReport(f.t.Context(), host, r, f.now); err != nil {
		f.t.Fatal(err)
	}
}

func assertChange(t *testing.T, f *fixture, event telemetry.Event, tool, host, account, result string) {
	t.Helper()
	attrs := telemetry.Attrs{"tool": tool, "host": host, "account": account, "user": "", "ip": "", "result": result}
	if event.Name != trail.EventChangeFinished || event.User != f.caller.UserID || !reflect.DeepEqual(event.Attrs, attrs) {
		t.Fatalf("change event %+v, want %v", event, attrs)
	}
}

func assertEvents(t *testing.T, events []telemetry.Event, names ...string) {
	t.Helper()
	var got []string
	for _, event := range events {
		got = append(got, event.Name)
	}
	if !slices.Equal(got, names) {
		t.Fatalf("events %+v, want %v", events, names)
	}
}

const appkitToolCalled = "tool.called"

func backupToolEvents(events []telemetry.Event) []telemetry.Event {
	var changes []telemetry.Event
	for _, event := range events {
		if event.Name != appkitToolCalled {
			changes = append(changes, event)
		}
	}
	return changes
}

func assertReadEvents(t *testing.T, events []telemetry.Event, count int) {
	t.Helper()
	names := make([]string, count)
	for i := range names {
		names[i] = appkitToolCalled
	}
	assertEvents(t, events, names...)
}

func assertCancelled(t *testing.T, f *fixture, e telemetry.Event, id, kind, host, account string) {
	t.Helper()
	attrs := telemetry.Attrs{"operation": id, "kind": kind, "host": host, "account": account,
		"status": store.OpCancelled, "reason": "", "data": ""}
	if e.Name != trail.EventOperationFinished || e.User != f.caller.UserID || !reflect.DeepEqual(e.Attrs, attrs) {
		t.Fatalf("cancelled event %+v, want %v", e, attrs)
	}
	if op := f.operation(id); op.Status != store.OpCancelled {
		t.Fatalf("operation not cancelled: %+v", op)
	}
}

// R-2MSQ-2I2A R-2LKT-OQBL R-2O0M-G9SZ R-2P8I-U1JO R-2QGF-7TAD
func TestCreateAccountBackupCheck(t *testing.T) {
	synctest.Test(t, testCreateAccountBackupCheck)
}

func testCreateAccountBackupCheck(t *testing.T) {
	f := setup(t)
	h, _ := f.seedHost(true)
	args := tools.CreateAccountArgs{Account: "acme", Host: h.Name}
	f.cloud.hasKeys = func(_ context.Context, bucket, prefix string) (bool, error) {
		if len(f.cloud.lookups) != len(f.cloud.backupLookups) {
			t.Errorf("backups checked before DNS: DNS %v, backups %v", f.cloud.lookups, f.cloud.backupLookups)
		}
		if bucket != "metaspot-example-hosts-backups" || prefix != "accounts/acme/" {
			t.Errorf("HasKeys(%q, %q)", bucket, prefix)
		}
		return true, nil
	}
	before := f.host(h.Name)
	earliest := time.Now()
	refusal(t, f.call(tools.ToolCreateAccount, args), tools.NameHasBackups)
	if len(f.cloud.backupLookups) != 1 || f.cloud.backupLookups[0].deadline.Before(earliest) ||
		f.cloud.backupLookups[0].deadline.After(time.Now().Add(tools.BackupCheckTimeout)) {
		t.Fatalf("backup lookups %+v", f.cloud.backupLookups)
	}
	f.cloud.hasKeys = func(context.Context, string, string) (bool, error) { return true, context.DeadlineExceeded }
	deadline := time.Now().Add(4 * time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	refusal(t, f.callContext(ctx, tools.ToolCreateAccount, args), tools.CloudFailed)
	if len(f.cloud.backupLookups) != 2 || !f.cloud.backupLookups[1].deadline.Equal(deadline) {
		t.Fatalf("backup lookups %+v", f.cloud.backupLookups)
	}
	if _, err := f.st.DeleteAccountBackups(t.Context(), args.Account, f.caller); err != nil {
		t.Fatal(err)
	}
	f.cloud.hasKeys = nil
	refusal(t, f.call(tools.ToolCreateAccount, args), tools.BackupsDeleting)
	if len(f.cloud.lookups) != 3 || len(f.cloud.backupLookups) != 3 || f.wakes != 0 || !reflect.DeepEqual(f.host(h.Name), before) {
		t.Fatalf("refusals changed host or called the wrong seam: wakes %d, DNS %v, backups %v", f.wakes, f.cloud.lookups, f.cloud.backupLookups)
	}
	if _, err := f.st.Account(t.Context(), args.Account); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refused account exists: %v", err)
	}
	events := backupToolEvents(allEvents(t, f))
	assertEvents(t, events, trail.EventChangeFinished, trail.EventChangeFinished, trail.EventChangeFinished)
	for i, result := range []string{trail.ResultRefused, trail.ResultFailed, trail.ResultRefused} {
		assertChange(t, f, events[i], tools.ToolCreateAccount, h.Name, args.Account, result)
	}
}

// R-I8IF-6KQY R-I9QB-KCHN R-8OBJ-WZXT R-8QRC-OJF7 R-HYR8-4ETE R-HAD8-GZZI R-AR65-3I4X
// R-GTAN-47LS R-32NF-1IPB R-ASE1-H9VM R-H31U-6DJC R-2U44-D4IG R-HWBF-CVC0
func TestCloseOpenTools(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	missing := tools.HostArgs{Host: "missing"}
	refusal(t, f.call(tools.ToolCloseHost, missing), tools.NoHost)
	refusal(t, f.call(tools.ToolOpenHost, missing), tools.NoHost)
	p, creation := f.seedHost(false)
	busy := tools.HostArgs{Host: p.Name}
	refusal(t, f.call(tools.ToolCloseHost, busy), tools.HostBusy)
	refusal(t, f.call(tools.ToolOpenHost, busy), tools.HostBusy)
	f.fail(creation.ID, "create failed")
	refusal(t, f.call(tools.ToolCloseHost, busy), tools.HostFailed)

	h, _ := f.seedHost(true)
	args := tools.HostArgs{Host: h.Name}
	refusal(t, f.call(tools.ToolOpenHost, args), tools.HostNotClosed)
	stopping := answer[tools.HostChange](t, f.call(tools.ToolStopHost, args))
	f.finish(stopping.Operation, store.Update{})
	refusal(t, f.call(tools.ToolCloseHost, args), tools.HostNotRunning)
	events := backupToolEvents(allEvents(t, f))
	assertChange(t, f, events[len(events)-1], tools.ToolCloseHost, h.Name, "", trail.ResultRefused)
	start := answer[tools.HostChange](t, f.call(tools.ToolStartHost, args))
	f.finish(start.Operation, store.Update{})

	before := readHost(t, f, h.Name)
	if before.Closed != "" || before.Replace != "" || before.Operation != nil {
		t.Fatal(before)
	}
	var woke []string
	f.onWake = func() {
		current := f.host(h.Name)
		if op := f.operation(current.Operation); op.Status != store.OpRunning || op.Caller.UserID != f.caller.UserID {
			t.Errorf("wake observed %+v", op)
		}
		woke = append(woke, current.Operation)
	}
	eventsAt := len(allEvents(t, f))
	closure := answer[tools.HostChange](t, f.call(tools.ToolCloseHost, args))
	closing := readHost(t, f, h.Name)
	if closure.State != store.HostClosing || closure.Host != h.Name || closure.OnDelete != "" ||
		closing.Operation == nil || closing.Operation.Operation != closure.Operation ||
		closing.Operation.Kind != store.OpClose || closing.Operation.Status != store.OpRunning || closing.Version != before.Version+1 {
		t.Fatalf("close %+v, read %+v, before %+v", closure, closing, before)
	}
	for _, tc := range []callCase{
		{tools.ToolStopHost, args, tools.HostBusy},
		{tools.ToolStartHost, args, tools.HostBusy},
		{tools.ToolResizeHost, map[string]any{"host": h.Name, "data_gb": valid.DefaultDataGB}, tools.HostBusy},
		{tools.ToolDeleteHost, args, tools.HostBusy},
		{tools.ToolCloseHost, args, tools.HostBusy},
	} {
		refusal(t, f.call(tc.name, tc.args), tc.want)
	}
	events = backupToolEvents(allEvents(t, f)[eventsAt:])
	assertEvents(t, events, trail.EventHostChanged, trail.EventOperationStarted, trail.EventChangeFinished,
		trail.EventChangeFinished, trail.EventChangeFinished, trail.EventChangeFinished, trail.EventChangeFinished, trail.EventChangeFinished)
	assertChange(t, f, events[2], tools.ToolCloseHost, h.Name, "", trail.ResultOK)

	eventsAt = len(allEvents(t, f))
	open := answer[tools.HostChange](t, f.call(tools.ToolOpenHost, args))
	opening := readHost(t, f, h.Name)
	if op := f.operation(open.Operation); op.Kind != store.OpOpen || op.Supersedes != closure.Operation || op.Status != store.OpRunning ||
		open.Host != h.Name || open.State != store.HostOpening || opening.Closed != "" ||
		opening.Version != closing.Version+1 || opening.Operation == nil || opening.Operation.Kind != store.OpOpen {
		t.Fatalf("open %+v, read %+v, stored %+v", open, opening, op)
	}
	events = backupToolEvents(allEvents(t, f)[eventsAt:])
	assertEvents(t, events, trail.EventOperationFinished, trail.EventHostChanged, trail.EventOperationStarted, trail.EventChangeFinished)
	assertCancelled(t, f, events[0], closure.Operation, store.OpClose, h.Name, "")
	if !slices.Equal(woke, []string{closure.Operation, open.Operation}) {
		t.Fatalf("wakes %+v", woke)
	}
	f.finish(open.Operation, store.Update{})
	closure = answer[tools.HostChange](t, f.call(tools.ToolCloseHost, args))
	retry := checkRetryHost(t, f, closure.Operation, store.HostClosing)
	if retry != closure {
		t.Fatalf("retry %+v, want %+v", retry, closure)
	}
	f.finish(retry.Operation, store.Update{})
	when := f.now.UTC().Format(time.RFC3339)
	for _, state := range []string{store.HostRunning, store.HostStopped} {
		current := readHost(t, f, h.Name)
		list := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{}))
		if current.Closed != when || current.Replace != "" || list.Hosts[1].Closed != when || list.Hosts[1].Replace != "" {
			t.Fatalf("closed time %q, read %+v, list %+v", when, current, list)
		}
		eventsAt = len(allEvents(t, f))
		wakes := len(woke)
		got := answer[tools.HostChange](t, f.call(tools.ToolCloseHost, args))
		if got != (tools.HostChange{Host: h.Name, State: state}) || readHost(t, f, h.Name).Version != current.Version || len(woke) != wakes {
			t.Fatalf("repeat close %+v changed state or woke engine", got)
		}
		assertEvents(t, backupToolEvents(allEvents(t, f)[eventsAt:]), trail.EventChangeFinished)
		if state == store.HostRunning {
			stop := answer[tools.HostChange](t, f.call(tools.ToolStopHost, args))
			f.finish(stop.Operation, store.Update{})
		}
	}
	refusal(t, f.call(tools.ToolOpenHost, args), tools.HostNotRunning)
	start = answer[tools.HostChange](t, f.call(tools.ToolStartHost, args))
	f.finish(start.Operation, store.Update{})
	open = answer[tools.HostChange](t, f.call(tools.ToolOpenHost, args))
	if got := readHost(t, f, h.Name); got.Closed != "" || got.Operation == nil || got.Operation.Operation != open.Operation {
		t.Fatal(got)
	}
	checkRetryHost(t, f, open.Operation, store.HostOpening)
	if len(f.cloud.lookups)+len(f.cloud.backupLookups) != 0 {
		t.Fatal("unexpected cloud call")
	}
}

// R-MZTN-5NUP R-N11J-JFLE R-HYR8-4ETE R-HZZ4-I6K3 R-AR65-3I4X R-8OBJ-WZXT
func TestReplaceHostTool(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	f.d.SetFailing(true)
	for _, tc := range []callCase{
		{tools.ToolReplaceHost, map[string]any{"host": h.Name, "from": "other", "type": "bad"}, tools.BadReplace},
		{tools.ToolReplaceHost, map[string]any{"host": h.Name, "from": store.ReplaceVolume, "type": "bad", "data_gb": 0}, tools.TypeNotAllowed},
		{tools.ToolReplaceHost, map[string]any{"host": h.Name, "from": store.ReplaceVolume, "data_gb": 0}, tools.SizeOutOfRange},
		{tools.ToolReplaceHost, map[string]any{"host": h.Name, "from": store.ReplaceVolume, "data_gb": valid.DefaultDataGB}, tools.KeepsVolumeSize},
	} {
		refusal(t, f.call(tc.name, tc.args), tc.want)
	}
	f.d.SetFailing(false)
	args := tools.ReplaceHostArgs{Host: "missing", From: store.ReplaceVolume}
	refusal(t, f.call(tools.ToolReplaceHost, args), tools.NoHost)
	bare, badOp := f.seedHost(false)
	args.Host = bare.Name
	refusal(t, f.call(tools.ToolReplaceHost, args), tools.HostBusy)
	f.fail(badOp.ID, "no machine")
	refusal(t, f.call(tools.ToolReplaceHost, args), tools.NoDataVolume)
	args.From = store.ReplaceBackup
	refusal(t, f.call(tools.ToolReplaceHost, args), tools.NoAddress)
	failedDelete, _ := f.seedHost(true)
	del := answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, tools.HostArgs{Host: failedDelete.Name}))
	f.fail(del.Operation, "delete failed")
	args.Host = failedDelete.Name
	refusal(t, f.call(tools.ToolReplaceHost, args), tools.HostFailed)

	for _, slug := range []string{"acme", "beta"} {
		createActiveAccount(f, slug, h.Name)
	}
	snapshot := f.now.UTC().Format(time.RFC3339)
	backupReport(f, h.Name, protocol.AccountBackup{Account: "acme", Snapshots: []int64{f.now.Unix()}},
		protocol.AccountBackup{Account: "beta", Snapshots: []int64{f.now.Unix()}})
	var restores []tools.RestoreMade
	for _, slug := range []string{"acme", "beta"} {
		restores = append(restores, answer[tools.RestoreMade](t, f.call(tools.ToolRestoreFiles, tools.RestoreFilesArgs{Account: slug, Snapshot: snapshot})))
	}
	slices.SortFunc(restores, func(a, b tools.RestoreMade) int {
		return cmp.Compare(a.Restore, b.Restore)
	})
	closure := answer[tools.HostChange](t, f.call(tools.ToolCloseHost, tools.HostArgs{Host: h.Name}))
	f.fail(closure.Operation, "final backup failed")
	before := f.host(h.Name)
	size := before.DataGB - 1
	at := len(allEvents(t, f))
	replaced := answer[tools.HostChange](t, f.call(tools.ToolReplaceHost, tools.ReplaceHostArgs{Host: h.Name, From: store.ReplaceBackup, DataGB: &size}))
	op := f.operation(replaced.Operation)
	after := readHost(t, f, h.Name)
	if replaced.Host != h.Name || replaced.State != store.HostReplacing || replaced.OnDelete != "" ||
		op.Kind != store.OpReplace || op.Status != store.OpRunning || op.Caller.UserID != f.caller.UserID ||
		op.Target != (store.Target{Type: before.Type, RootGB: before.RootGB, DataGB: size}) ||
		after.State != store.HostReplacing || after.Version != before.Version || after.Replace != store.ReplaceBackup ||
		after.Operation == nil || after.Operation.Operation != op.ID || after.Operation.Kind != store.OpReplace ||
		after.Operation.Status != store.OpRunning {
		t.Fatalf("replace %+v, host %+v, operation %+v", replaced, after, op)
	}
	events := backupToolEvents(allEvents(t, f)[at:])
	assertEvents(t, events, trail.EventOperationFinished, trail.EventOperationFinished, trail.EventHostChanged, trail.EventOperationStarted, trail.EventChangeFinished)
	for i, restore := range restores {
		assertCancelled(t, f, events[i], restore.Restore, store.OpRestore, h.Name, restore.Account)
	}
	assertChange(t, f, events[4], tools.ToolReplaceHost, h.Name, "", trail.ResultOK)
	refusal(t, f.call(tools.ToolOpenHost, tools.HostArgs{Host: h.Name}), tools.HostBusy)
	if err := f.st.RecordStep(t.Context(), replaced.Operation, "relaunch", 3, f.now); err != nil {
		t.Fatal(err)
	}
	f.fail(replaced.Operation, "replace failed")
	if failed := readHost(t, f, h.Name); failed.Replace != store.ReplaceBackup || failed.Operation == nil || failed.Operation.Status != store.OpFailed {
		t.Fatal(failed)
	}
	refusal(t, f.call(tools.ToolOpenHost, tools.HostArgs{Host: h.Name}), tools.HostNotRunning)
	retry := answer[tools.HostChange](t, f.call(tools.ToolRetryHost, tools.HostArgs{Host: h.Name}))
	if op := f.operation(retry.Operation); retry != replaced || op.Step != "relaunch" || op.Generation != 3 ||
		op.Kind != store.OpReplace || op.Status != store.OpRunning {
		t.Fatalf("retry %+v, want %+v, operation %+v", retry, replaced, op)
	}
	f.now = f.now.Add(time.Minute)
	f.finish(retry.Operation, store.Update{DataVolume: "fresh-volume"})
	kept := listKept(t, f, tools.KeptVolumesArgs{})
	if len(kept.KeptVolumes) != 1 || kept.KeptVolumes[0].Cause != store.CauseReplaced ||
		kept.KeptVolumes[0].Volume != before.DataVolume || kept.KeptVolumes[0].Deleted != f.now.UTC().Format(time.RFC3339) {
		t.Fatal(kept)
	}
	deletion := answer[tools.KeptVolume](t, f.call(tools.ToolDeleteKeptVolume, tools.KeptVolumeArgs{Volume: before.DataVolume}))
	if deletion.Cause != store.CauseReplaced || deletion.State != store.VolumeDeleting {
		t.Fatal(deletion)
	}
	answer[tools.HostChange](t, f.call(tools.ToolOpenHost, tools.HostArgs{Host: h.Name}))

	noOverlay, _ := f.seedHost(true)
	resize := answer[tools.HostResize](t, f.call(tools.ToolResizeHost, map[string]any{"host": noOverlay.Name, "type": "t4g.large"}))
	f.fail(resize.Operation, "resize failed")
	refusal(t, f.call(tools.ToolOpenHost, tools.HostArgs{Host: noOverlay.Name}), tools.HostNotClosed)
	volume := answer[tools.HostChange](t, f.call(tools.ToolReplaceHost, tools.ReplaceHostArgs{Host: noOverlay.Name, From: store.ReplaceVolume}))
	if got := readHost(t, f, noOverlay.Name); got.Replace != store.ReplaceVolume || got.Operation == nil || got.Operation.Operation != volume.Operation {
		t.Fatal(got)
	}
	if len(f.cloud.lookups)+len(f.cloud.backupLookups) != 0 {
		t.Fatal("unexpected cloud calls")
	}
}

// R-MZTN-5NUP R-N11J-JFLE
func TestReplaceWithheldHost(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	first := failRetiredReplacement(t, f, h.Name)
	volume := tools.ReplaceHostArgs{Host: h.Name, From: store.ReplaceVolume}
	refusal(t, f.call(tools.ToolReplaceHost, volume), tools.HostWithheld)

	typ, root, data := "t4g.large", h.RootGB+1, h.DataGB+1
	backup := tools.ReplaceHostArgs{Host: h.Name, From: store.ReplaceBackup, Type: &typ, RootGB: &root, DataGB: &data}
	second := answer[tools.HostChange](t, f.call(tools.ToolReplaceHost, backup))
	op := f.operation(second.Operation)
	if got := readHost(t, f, h.Name); got.Version != first.Version+1 || second.Host != h.Name ||
		second.State != store.HostReplacing || op.Kind != store.OpReplace || op.Status != store.OpRunning ||
		op.Target != (store.Target{Type: typ, RootGB: root, DataGB: data}) || op.Caller.UserID != f.caller.UserID {
		t.Fatalf("replace answer %+v, host %+v, operation %+v", second, got, op)
	}
	f.fail(second.Operation, "failed before retiring an instance")
	refusal(t, f.call(tools.ToolReplaceHost, volume), tools.HostWithheld)
	third := answer[tools.HostChange](t, f.call(tools.ToolReplaceHost, map[string]any{
		"host": h.Name, "from": store.ReplaceBackup, "type": nil, "root_gb": nil, "data_gb": nil,
	}))
	op = f.operation(third.Operation)
	if got := readHost(t, f, h.Name); got.Version != first.Version+1 ||
		op.Target != (store.Target{Type: h.Type, RootGB: h.RootGB, DataGB: h.DataGB}) {
		t.Fatalf("replacement after an early failure: host %+v, operation %+v", got, op)
	}
	f.fail(third.Operation, "failed before retiring an instance")
	deleted := answer[tools.HostChange](t, f.call(tools.ToolDeleteHost, tools.DeleteHostArgs{Host: h.Name}))
	f.fail(deleted.Operation, "delete failed")
	refusal(t, f.call(tools.ToolReplaceHost, volume), tools.HostFailed)

	bare, create := f.seedHost(false)
	if err := f.st.Advance(t.Context(), create.ID, store.Update{AllocationID: "allocation-without-volume"}); err != nil {
		t.Fatal(err)
	}
	f.fail(create.ID, "create failed before allocating the volume")
	failRetiredReplacement(t, f, bare.Name)
	refusal(t, f.call(tools.ToolReplaceHost, tools.ReplaceHostArgs{Host: bare.Name, From: store.ReplaceVolume}), tools.HostWithheld)
}

func failRetiredReplacement(t *testing.T, f *fixture, host string) tools.HostRead {
	t.Helper()
	started := answer[tools.HostChange](t, f.call(tools.ToolReplaceHost, tools.ReplaceHostArgs{Host: host, From: store.ReplaceBackup}))
	if err := f.st.Retire(t.Context(), started.Operation); err != nil {
		t.Fatal(err)
	}
	f.fail(started.Operation, "replacement failed after retirement")
	return readHost(t, f, host)
}

// R-QWUX-5IGZ R-QY2T-JA7O R-QZAP-X1YD R-R0IM-ATP2 R-IFTT-H774 R-AR65-3I4X R-32NF-1IPB R-ASE1-H9VM
func TestRestoreToolsAndRemoveCancellation(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	acme := createActiveAccount(f, "acme", h.Name)
	createActiveAccount(f, "beta", h.Name)
	f.seedAccount("pending", h.Name)
	for _, user := range []tools.UserKeyArgs{{Account: "acme", User: "daffy", Key: key(1)}, {Account: "beta", User: "bugs", Key: key(2)}} {
		answer[tools.UserChange](t, f.call(tools.ToolAddUser, user))
	}
	f.now = f.now.UTC().Truncate(time.Second)
	snapshot := f.now.Add(-time.Hour)
	asOf := snapshot.Format(time.RFC3339)
	db := tools.RestoreDatabaseArgs{Account: acme.Account, Path: "data/app.db", AsOf: asOf}
	files := tools.RestoreFilesArgs{Account: acme.Account, Snapshot: asOf}
	f.d.SetFailing(true)
	for _, tc := range []callCase{
		{tools.ToolRestoreDatabase, map[string]any{"account": "missing", "path": "app.db", "as_of": "bad"}, tools.InvalidPath},
		{tools.ToolRestoreDatabase, map[string]any{"account": "missing", "path": db.Path, "as_of": "bad"}, tools.BadTime},
		{tools.ToolRestoreFiles, map[string]any{"account": "missing", "path": "../etc", "snapshot": "bad"}, tools.InvalidPath},
		{tools.ToolRestoreFiles, map[string]any{"account": "missing", "path": nil, "snapshot": "bad"}, tools.BadTime},
	} {
		refusal(t, f.call(tc.name, tc.args), tc.want)
	}
	f.d.SetFailing(false)
	for _, tc := range []callCase{
		{tools.ToolRestoreDatabase, map[string]any{"account": "missing", "path": db.Path, "as_of": asOf}, tools.NoAccount},
		{tools.ToolRestoreFiles, map[string]any{"account": "missing", "snapshot": asOf}, tools.NoAccount},
		{tools.ToolRestoreDatabase, map[string]any{"account": "pending", "path": db.Path, "as_of": asOf}, tools.AccountNotActive},
		{tools.ToolRestoreDatabase, db, tools.NoDatabase},
		{tools.ToolRestoreFiles, map[string]any{"account": "pending", "user": "missing", "snapshot": asOf}, tools.NoUser},
		{tools.ToolRestoreFiles, map[string]any{"account": "pending", "user": "bugs", "snapshot": asOf}, tools.NotInAccount},
		{tools.ToolRestoreFiles, map[string]any{"account": "pending", "snapshot": asOf}, tools.AccountNotActive},
		{tools.ToolRestoreFiles, files, tools.NoSnapshot},
	} {
		refusal(t, f.call(tc.name, tc.args), tc.want)
	}
	stop := answer[tools.HostChange](t, f.call(tools.ToolStopHost, tools.HostArgs{Host: h.Name}))
	f.finish(stop.Operation, store.Update{})
	refusal(t, f.call(tools.ToolRestoreDatabase, db), tools.HostNotRunning)
	refusal(t, f.call(tools.ToolRestoreFiles, files), tools.HostNotRunning)
	start := answer[tools.HostChange](t, f.call(tools.ToolStartHost, tools.HostArgs{Host: h.Name}))
	f.finish(start.Operation, store.Update{})
	closure := answer[tools.HostChange](t, f.call(tools.ToolCloseHost, tools.HostArgs{Host: h.Name}))
	refusal(t, f.call(tools.ToolRestoreDatabase, db), tools.HostNotRunning)
	f.finish(closure.Operation, store.Update{})

	backupReport(f, h.Name, protocol.AccountBackup{Account: acme.Account, Snapshots: []int64{snapshot.Unix()},
		Databases: []protocol.Database{{Path: db.Path}}})
	refusal(t, f.call(tools.ToolRestoreDatabase, db), tools.NotRestorable)
	var zero time.Time
	backupReport(f, h.Name, protocol.AccountBackup{Account: acme.Account, Snapshots: []int64{snapshot.Unix(), zero.Unix()},
		Databases: []protocol.Database{{Path: db.Path, Oldest: snapshot, Synced: f.now}}},
		protocol.AccountBackup{Account: "beta", Snapshots: []int64{snapshot.Unix()}})
	for _, timeArg := range []string{"0001-01-01T00:00:00Z", snapshot.Add(-time.Second).Format(time.RFC3339), f.now.Add(time.Second).Format(time.RFC3339)} {
		args := db
		args.AsOf = timeArg
		refusal(t, f.call(tools.ToolRestoreDatabase, args), tools.NotRestorable)
	}
	refusal(t, f.call(tools.ToolRestoreFiles, tools.RestoreFilesArgs{Account: acme.Account, Snapshot: snapshot.Add(time.Second).Format(time.RFC3339)}), tools.NoSnapshot)

	before := readHost(t, f, h.Name)
	wakes := f.wakes
	f.onWake = func() {
		restores, _, err := f.st.RestoresOn(t.Context(), h.Name, valid.MaxRestoresPerHost)
		if err != nil || len(restores) != f.wakes-wakes {
			t.Errorf("wake saw restores %+v (%v)", restores, err)
		}
	}
	db.AsOf = f.now.Add(900 * time.Millisecond).In(time.FixedZone("east", 3600)).Format(time.RFC3339Nano)
	at := len(allEvents(t, f))
	database := answer[tools.RestoreMade](t, f.call(tools.ToolRestoreDatabase, db))
	if database != (tools.RestoreMade{Restore: database.Restore, Account: acme.Account, Host: h.Name,
		Kind: protocol.RestoreDatabase, Path: db.Path, AsOf: f.now.Format(time.RFC3339), State: protocol.StepPending}) {
		t.Fatal(database)
	}
	if got := readHost(t, f, h.Name); got.Version != before.Version+1 || got.State != before.State {
		t.Fatalf("database restore changed host to %+v", got)
	}
	file := answer[tools.RestoreMade](t, f.call(tools.ToolRestoreFiles, map[string]any{
		"account": acme.Account, "snapshot": snapshot.Add(900 * time.Millisecond).Format(time.RFC3339Nano), "path": nil, "user": nil,
	}))
	if file != (tools.RestoreMade{Restore: file.Restore, Account: acme.Account, Host: h.Name,
		Kind: protocol.RestoreFiles, AsOf: asOf, State: protocol.StepPending}) {
		t.Fatal(file)
	}
	if got := readHost(t, f, h.Name); got.Version != before.Version+2 || got.State != before.State {
		t.Fatalf("files restore changed host to %+v", got)
	}
	chosen := answer[tools.RestoreMade](t, f.call(tools.ToolRestoreFiles, map[string]any{"account": acme.Account, "snapshot": asOf, "path": "notes", "user": "daffy"}))
	if chosen.User != "daffy" || chosen.Path != "notes" || chosen.AsOf != asOf {
		t.Fatal(chosen)
	}
	zeroFile := answer[tools.RestoreMade](t, f.call(tools.ToolRestoreFiles, tools.RestoreFilesArgs{Account: acme.Account, Snapshot: "0001-01-01T00:00:00Z"}))
	if rec, err := f.st.Restore(t.Context(), zeroFile.Restore); err != nil || !rec.AsOf.IsZero() || zeroFile.AsOf != "" {
		t.Fatalf("zero snapshot answer %+v, stored %+v (%v)", zeroFile, rec, err)
	}
	pending := []tools.RestoreMade{database, file, chosen, zeroFile}
	for len(pending) < valid.MaxRestoresPerHost {
		pending = append(pending, answer[tools.RestoreMade](t, f.call(tools.ToolRestoreFiles, tools.RestoreFilesArgs{Account: "beta", Snapshot: asOf})))
	}
	for _, r := range pending {
		stored, err := f.st.Restore(t.Context(), r.Restore)
		op := f.operation(r.Restore)
		if err != nil || r.Restore == "" || r.Account != stored.Account || r.Host != stored.Host || r.Kind != stored.Kind ||
			r.Path != stored.Path || r.User != stored.User || r.State != stored.State || r.State != protocol.StepPending ||
			r.AsOf != stampRestoreTime(stored.AsOf) || op.Kind != store.OpRestore ||
			op.Status != store.OpRunning || op.Caller.UserID != f.caller.UserID || op.Account != r.Account || op.Host != r.Host {
			t.Fatalf("restore answer %+v, record %+v, operation %+v (%v)", r, stored, op, err)
		}
	}
	after := readHost(t, f, h.Name)
	if after.State != before.State || after.Version != before.Version+int64(valid.MaxRestoresPerHost) ||
		readAccount(t, f, acme.Account).State != store.AccountActive || f.wakes != wakes+valid.MaxRestoresPerHost {
		t.Fatalf("restore changed host or account state: %+v", after)
	}
	refusal(t, f.call(tools.ToolRestoreDatabase, db), tools.TooManyRestores)
	refusal(t, f.call(tools.ToolRestoreFiles, files), tools.TooManyRestores)
	events := backupToolEvents(allEvents(t, f)[at:])
	if len(events) != len(pending)*2+2 {
		t.Fatal(events)
	}
	for i, r := range pending {
		e := events[2*i]
		want := telemetry.Attrs{"operation": r.Restore, "kind": store.OpRestore, "host": h.Name, "account": r.Account, "data": ""}
		if e.Name != trail.EventOperationStarted || !reflect.DeepEqual(e.Attrs, want) {
			t.Fatalf("event %+v, want %v", e, want)
		}
		tool := tools.ToolRestoreFiles
		if r.Kind == protocol.RestoreDatabase {
			tool = tools.ToolRestoreDatabase
		}
		assertChange(t, f, events[2*i+1], tool, h.Name, r.Account, trail.ResultOK)
	}
	f.onWake = nil
	beta := tools.AccountArgs{Account: "beta"}
	refusal(t, f.call(tools.ToolRemoveAccount, beta), tools.AccountHasUsers)
	answer[tools.UserChange](t, f.call(tools.ToolLockUser, tools.UserArgs{Account: beta.Account, User: "bugs"}))
	answer[tools.UserChange](t, f.call(tools.ToolDeleteUser, tools.UserArgs{Account: beta.Account, User: "bugs"}))
	host := f.host(h.Name)
	if _, err := f.st.RecordReport(t.Context(), h.Name, protocol.Report{InstanceID: host.InstanceID, Applied: host.Version, Attempted: host.Version}, f.now); err != nil {
		t.Fatal(err)
	}
	at = len(allEvents(t, f))
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, beta))
	events = backupToolEvents(allEvents(t, f)[at:])
	var cancelled []string
	for _, r := range pending {
		if r.Account == beta.Account {
			cancelled = append(cancelled, r.Restore)
		}
	}
	slices.Sort(cancelled)
	if len(events) != len(cancelled)+3 {
		t.Fatal(events)
	}
	for i, id := range cancelled {
		assertCancelled(t, f, events[i], id, store.OpRestore, h.Name, beta.Account)
	}
	assertEvents(t, events[len(cancelled):], trail.EventAccountChanged, trail.EventOperationStarted, trail.EventChangeFinished)
	if len(f.cloud.lookups)+len(f.cloud.backupLookups) != 0 {
		t.Fatal("unexpected cloud calls")
	}
}

func stampRestoreTime(when time.Time) string {
	if when.IsZero() {
		return ""
	}
	return when.UTC().Format(time.RFC3339)
}

// R-QWUX-5IGZ R-QZAP-X1YD
func TestRestoreToolsCountReportedUnsettledRestores(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	account := createActiveAccount(f, "acme", h.Name).Account
	snapshot := f.now.UTC().Truncate(time.Second)
	entry := protocol.AccountBackup{
		Account: account, Snapshots: []int64{snapshot.Unix()},
		Databases: []protocol.Database{{Path: "data/app.db", Oldest: snapshot}},
	}
	backupReport(f, h.Name, entry)
	db := tools.RestoreDatabaseArgs{Account: account, Path: entry.Databases[0].Path, AsOf: snapshot.Format(time.RFC3339)}
	files := tools.RestoreFilesArgs{Account: account, Snapshot: db.AsOf}
	results := make([]protocol.RestoreResult, 0, valid.MaxRestoresPerHost)
	for i := range valid.MaxRestoresPerHost {
		r := answer[tools.RestoreMade](t, f.call(tools.ToolRestoreFiles, files))
		state := protocol.StepDone
		if i%2 != 0 {
			state = protocol.StepFailed
		}
		results = append(results, protocol.RestoreResult{ID: r.Restore, State: state})
	}
	current := f.host(h.Name)
	if _, err := f.st.RecordReport(t.Context(), h.Name, protocol.Report{
		Protocol: protocol.Version, InstanceID: current.InstanceID, Attempted: current.Version, Applied: current.Version,
		Backups: []protocol.AccountBackup{entry}, Restores: results,
	}, f.now); err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		r, err := f.st.Restore(t.Context(), result.ID)
		if err != nil || r.State != result.State || f.operation(result.ID).Status != store.OpRunning {
			t.Fatalf("report did not leave restore %s unsettled: %+v (%v)", result.ID, r, err)
		}
	}
	refusal(t, f.call(tools.ToolRestoreDatabase, db), tools.TooManyRestores)
	refusal(t, f.call(tools.ToolRestoreFiles, files), tools.TooManyRestores)
}

// R-1XGJ-IPPB R-1YOF-WHG0 R-1ZWC-A96P R-I170-VYAS R-PQKT-Z939 R-PJXN-MABK R-2U44-D4IG R-HWBF-CVC0
func TestBackupReadTools(t *testing.T) {
	f := setup(t)
	h, _ := f.seedHost(true)
	other, _ := f.seedHost(true)
	acme := createActiveAccount(f, "acme", h.Name)
	_, closeOp, err := f.st.CloseHost(t.Context(), h.Name, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	finalAt := f.now.UTC().Truncate(time.Second)
	backupReport(f, h.Name, protocol.AccountBackup{Account: acme.Account,
		Final: protocol.Step{ID: closeOp.ID, State: protocol.StepDone, At: finalAt}})
	f.finish(closeOp.ID, store.Update{})
	if _, _, _, err := f.st.ReplaceHost(t.Context(), h.Name, store.ReplaceBackup, store.Target{}, f.caller); err != nil {
		t.Fatal(err)
	}
	beta := createActiveAccount(f, "beta", h.Name)
	delta := createActiveAccount(f, "delta", h.Name)
	gamma := createActiveAccount(f, "gamma", other.Name)
	at := len(allEvents(t, f))
	result := f.call(tools.ToolRestorePoints, gamma)
	empty := answer[tools.RestorePoints](t, result)
	if !reflect.DeepEqual(empty, tools.RestorePoints{Account: gamma.Account, Host: other.Name, Snapshots: []string{}, Databases: []tools.DatabasePoint{}}) {
		t.Fatalf("no report: %+v", empty)
	}
	refusal(t, f.call(tools.ToolRestorePoints, tools.AccountArgs{Account: "missing"}), tools.NoAccount)
	assertReadEvents(t, allEvents(t, f)[at:], 2)

	f.now = finalAt.Add(5 * time.Minute)
	now := f.now.Format(time.RFC3339)
	snapshot := f.now.Add(-time.Minute)
	checked := f.now.Add(-2 * time.Minute)
	synced := f.now.Add(-3 * time.Minute)
	written := f.now.Add(-4 * time.Minute)
	snapshots := make([]int64, protocol.MaxSnapshots+3)
	for i := range snapshots {
		snapshots[i] = f.now.Add(time.Duration(i-len(snapshots)+1) * time.Minute).Unix()
	}
	databases := make([]protocol.Database, protocol.MaxDatabases+3)
	for i := range databases {
		databases[i] = protocol.Database{
			Path: fmt.Sprintf("data/db-%03d", len(databases)-1-i), Oldest: f.now.Add(-time.Hour),
			Written: f.now.Add(-time.Minute), Synced: f.now,
		}
	}
	backupReport(f, h.Name, protocol.AccountBackup{Account: acme.Account, Snapshot: snapshot, Snapshots: snapshots,
		Databases: databases, DatabaseTotal: len(databases), Checked: checked, Synced: synced, Written: written},
		protocol.AccountBackup{Account: delta.Account, Snapshot: f.now, Error: "backup failed", DatabaseTotal: 3, DatabaseErrors: 2})
	removed := answer[tools.Account](t, f.call(tools.ToolRemoveAccount, delta))
	if removed.State != store.AccountRemoving {
		t.Fatal(removed)
	}
	f.finish(readAccount(t, f, delta.Account).Operation.Operation, store.Update{})
	startEvents, startWakes := len(allEvents(t, f)), f.wakes
	restores := answer[tools.RestorePoints](t, f.call(tools.ToolRestorePoints, acme))
	if restores.Account != acme.Account || restores.Host != h.Name || restores.Reported != now ||
		len(restores.Snapshots) != protocol.MaxSnapshots || len(restores.Databases) != protocol.MaxDatabases || restores.DatabaseTotal != len(databases) {
		t.Fatalf("restore points %+v", restores)
	}
	for i, stamp := range restores.Snapshots {
		if want := time.Unix(snapshots[len(snapshots)-1-i], 0).UTC().Format(time.RFC3339); stamp != want {
			t.Fatalf("snapshot %d = %q, want %q", i, stamp, want)
		}
	}
	for i, got := range restores.Databases {
		want := tools.DatabasePoint{Path: fmt.Sprintf("data/db-%03d", i), Health: store.BackupHealthy, Oldest: f.now.Add(-time.Hour).Format(time.RFC3339),
			Written: f.now.Add(-time.Minute).Format(time.RFC3339), Synced: now}
		if got != want {
			t.Fatalf("database %d = %+v, want %+v", i, got, want)
		}
	}
	if got, want := answer[tools.RestorePoints](t, f.call(tools.ToolRestorePoints, beta)), (tools.RestorePoints{
		Account: beta.Account, Host: h.Name, Reported: now, Snapshots: []string{}, Databases: []tools.DatabasePoint{},
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("report without an entry: %+v, want %+v", got, want)
	}

	list := func(args any) tools.BackupStatusList {
		return answer[tools.BackupStatusList](t, f.call(tools.ToolBackupStatus, args))
	}
	slugs := func(rows []tools.AccountBackups) []string {
		var names []string
		for _, row := range rows {
			names = append(names, row.Account)
		}
		return names
	}
	all := list(map[string]any{"host": nil, "page": "", "limit": nil})
	want := []tools.AccountBackups{
		{Account: acme.Account, Host: h.Name, State: store.AccountActive, Health: store.BackupHealthy,
			Snapshot: snapshot.Format(time.RFC3339), Checked: checked.Format(time.RFC3339), Databases: len(databases),
			Synced: synced.Format(time.RFC3339), Written: written.Format(time.RFC3339), Final: protocol.StepDone,
			FinalAt: finalAt.Format(time.RFC3339), Restore: protocol.StepPending, Reported: now},
		{Account: beta.Account, Host: h.Name, State: store.AccountActive, Health: store.BackupNone},
		{Account: delta.Account, Host: h.Name, State: store.BackupsRemoved, Health: store.BackupError,
			Snapshot: now, Error: "backup failed", Databases: 3, DatabaseErrors: 2, Reported: now},
		{Account: gamma.Account, Host: other.Name, State: store.AccountActive, Health: store.BackupNone},
	}
	if all.Next != nil || !slices.Equal(all.Accounts, want) {
		t.Fatalf("backup status %+v, want %+v", all.Accounts, want)
	}
	host := list(tools.BackupStatusArgs{Host: &h.Name})
	if !slices.Equal(slugs(host.Accounts), []string{"acme", "beta", "delta"}) || host.Next != nil {
		t.Fatal(host)
	}
	refusal(t, f.call(tools.ToolBackupStatus, map[string]any{"host": "missing"}), tools.NoHost)
	token := paging.Token{Tool: tools.ToolBackupStatus}
	one := 1
	args := tools.BackupStatusArgs{Limit: &one}
	for i, row := range all.Accounts {
		window := list(args)
		if !slices.Equal(window.Accounts, []tools.AccountBackups{row}) || (window.Next == nil) != (i == len(all.Accounts)-1) {
			t.Fatal(window)
		}
		args.Page = window.Next
		if args.Page != nil && *args.Page != token.Encode(row.Account) {
			t.Fatal(*args.Page)
		}
	}
	hostPage := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{Limit: &one})).Next
	if hostPage == nil {
		t.Fatal("hosts had no next token")
	}
	f.d.SetFailing(true)
	zero := 0
	_, sizeErr := paging.Window(&zero)
	refusal(t, f.call(tools.ToolBackupStatus, map[string]any{"host": "missing", "page": hostPage, "limit": zero}), sizeErr.Error())
	refusal(t, f.call(tools.ToolBackupStatus, map[string]any{"host": "missing", "page": hostPage}), paging.BadPage)
	f.d.SetFailing(false)
	assertReadEvents(t, allEvents(t, f)[startEvents:], 8+len(all.Accounts))
	if f.wakes != startWakes || len(f.cloud.lookups)+len(f.cloud.backupLookups) != 0 {
		t.Fatal("read tools produced side effects")
	}

	answer[tools.BackupsDeletion](t, f.call(tools.ToolDeleteAccountBackups, delta))
	pending := list(tools.BackupStatusArgs{}).Accounts[2]
	if pending.Delete != store.DeleteBackupsDeleting || pending.DeleteReason != "" || pending.Error != "backup failed" {
		t.Fatal(pending)
	}
	if err := f.st.FailBackupDelete(t.Context(), delta.Account, "deletion failed"); err != nil {
		t.Fatal(err)
	}
	failed := list(tools.BackupStatusArgs{}).Accounts[2]
	if failed.Delete != store.DeleteBackupsFailed || failed.DeleteReason != "deletion failed" || failed.Error != "backup failed" {
		t.Fatal(failed)
	}
	answer[tools.BackupsDeletion](t, f.call(tools.ToolDeleteAccountBackups, delta))
	if err := f.st.FinishBackupDelete(t.Context(), delta.Account); err != nil {
		t.Fatal(err)
	}
	if got := slugs(list(tools.BackupStatusArgs{}).Accounts); !slices.Equal(got, []string{"acme", "beta", "gamma"}) {
		t.Fatalf("accounts after backup deletion %v", got)
	}
}

// R-PJXN-MABK
func TestRestorePointsUseReportsFromCurrentPlacement(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	h, _ := f.seedHost(true)
	acme := createActiveAccount(f, "acme", h.Name)
	f.now = f.now.UTC().Truncate(time.Second).Add(time.Minute)
	received := f.now
	snapshot := received.Add(-time.Minute)
	entry := protocol.AccountBackup{
		Account: acme.Account, Snapshots: []int64{snapshot.Unix()}, DatabaseTotal: 1,
		Databases: []protocol.Database{{Path: "data/old.db", Error: "sync failed"}},
	}
	backupReport(f, h.Name, entry)
	readPoints := func() tools.RestorePoints {
		return answer[tools.RestorePoints](t, f.call(tools.ToolRestorePoints, acme))
	}
	want := tools.RestorePoints{
		Account: acme.Account, Host: h.Name, Reported: stampRestoreTime(received),
		Snapshots: []string{stampRestoreTime(snapshot)}, DatabaseTotal: 1,
		Databases: []tools.DatabasePoint{{Path: "data/old.db", Health: store.BackupError, Error: "sync failed"}},
	}
	if got := readPoints(); !reflect.DeepEqual(got, want) {
		t.Fatalf("original placement: %+v, want %+v", got, want)
	}

	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, acme))
	f.finish(readAccount(t, f, acme.Account).Operation.Operation, store.Update{})
	f.now = received.Add(time.Second)
	create := tools.CreateAccountArgs{Account: acme.Account, Host: h.Name}
	placed := answer[tools.Account](t, f.call(tools.ToolCreateAccount, create))
	if placed.Created != stampRestoreTime(f.now) {
		t.Fatalf("new placement created at %q, want %q", placed.Created, stampRestoreTime(f.now))
	}
	want.Snapshots, want.Databases, want.DatabaseTotal = []string{}, []tools.DatabasePoint{}, 0
	if got := readPoints(); !reflect.DeepEqual(got, want) {
		t.Fatalf("before the new placement reports: %+v, want %+v", got, want)
	}

	backupReport(f, h.Name, protocol.AccountBackup{
		Account: acme.Account, Snapshots: []int64{received.Unix()},
	}, entry)
	want.Reported = placed.Created
	want.Snapshots = []string{stampRestoreTime(received)}
	if got := readPoints(); !reflect.DeepEqual(got, want) {
		t.Fatalf("report received at placement time: %+v, want %+v", got, want)
	}
}

// R-IN57-RTNA R-IOD4-5LDZ R-H31U-6DJC R-32NF-1IPB R-ASE1-H9VM R-HWBF-CVC0
func TestDeleteAccountBackupsTool(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	f.d.SetFailing(true)
	refusal(t, f.call(tools.ToolDeleteAccountBackups, tools.AccountArgs{Account: "no spaces"}), tools.InvalidName)
	refusal(t, f.call(tools.ToolDeleteAccountBackups, tools.AccountArgs{Account: "www"}), tools.ReservedName)
	f.d.SetFailing(false)
	h, _ := f.seedHost(true)
	for _, state := range []string{store.AccountPending, store.AccountActive, store.AccountFailed, store.AccountRemoving} {
		a, op := f.seedAccount("acme-"+state, h.Name)
		switch state {
		case store.AccountActive:
			f.finish(op.ID, store.Update{})
		case store.AccountFailed:
			f.fail(op.ID, "placement failed")
		case store.AccountRemoving:
			if _, _, _, err := f.st.RemoveAccount(ctx, a.Slug, f.caller); err != nil {
				t.Fatal(err)
			}
		}
		at := len(allEvents(t, f))
		refusal(t, f.call(tools.ToolDeleteAccountBackups, tools.AccountArgs{Account: a.Slug}), tools.AccountExists)
		raw := allEvents(t, f)[at:]
		assertEvents(t, raw, trail.EventChangeFinished, appkitToolCalled)
		events := backupToolEvents(raw)
		assertChange(t, f, events[0], tools.ToolDeleteAccountBackups, "", a.Slug, trail.ResultRefused)
	}
	if f.wakes != 0 {
		t.Fatal(f.wakes)
	}

	const slug = "unused"
	var wakes atomic.Int32
	srv := mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	tools.Register(srv, tools.Config{Store: f.st, Cloud: f.cloud, Configured: true, Telemetry: f.w, Wake: func() {
		wakes.Add(1)
		due, err := f.st.DueBackupDeletes(ctx, f.now, "", 2)
		if err != nil || len(due) != 1 || due[0].Caller != f.caller || due[0].Account != slug ||
			due[0].State != store.DeleteBackupsDeleting || due[0].Reason != "" || !due[0].NextTry.Equal(f.now.Truncate(time.Second)) {
			t.Errorf("wake observed %+v (%v)", due, err)
		}
	}})
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://accounts.example/mcp",
		HTTPClient: &http.Client{Transport: testTransport{identity.Require(srv)}},
	})
	args, err := json.Marshal(tools.AccountArgs{Account: slug})
	if err != nil {
		t.Fatal(err)
	}
	at := len(allEvents(t, f))
	var wg sync.WaitGroup
	goNow := make(chan struct{})
	replies := make(chan mcp.Result, 2)
	for range 2 {
		wg.Go(func() {
			<-goNow
			result, err := client.CallTool(ctx, f.caller, tools.ToolDeleteAccountBackups, args)
			if err != nil {
				t.Errorf("delete call: %v", err)
				return
			}
			replies <- result
		})
	}
	close(goNow)
	wg.Wait()
	close(replies)
	for result := range replies {
		got := answer[tools.BackupsDeletion](t, result)
		if got != (tools.BackupsDeletion{Account: slug, State: store.DeleteBackupsDeleting}) {
			t.Fatal(got)
		}
	}
	if wakes.Load() != 1 {
		t.Fatalf("duplicates woke %d times", wakes.Load())
	}
	if err := f.st.FailBackupDelete(ctx, slug, "provider failed"); err != nil {
		t.Fatal(err)
	}
	if result, err := client.CallTool(ctx, f.caller, tools.ToolDeleteAccountBackups, args); err != nil ||
		answer[tools.BackupsDeletion](t, result).State != store.DeleteBackupsDeleting {
		t.Fatalf("retry %v (%v)", result, err)
	}
	if wakes.Load() != 2 {
		t.Fatal(wakes.Load())
	}
	raw := allEvents(t, f)[at:]
	events := backupToolEvents(raw)
	assertEvents(t, events, trail.EventChangeFinished, trail.EventChangeFinished, trail.EventChangeFinished)
	if len(raw) != len(events)+3 || len(f.cloud.lookups)+len(f.cloud.backupLookups) != 0 {
		t.Fatalf("unexpected telemetry or cloud use: events %+v, DNS %v, backups %v", raw, f.cloud.lookups, f.cloud.backupLookups)
	}
	for _, event := range events {
		assertChange(t, f, event, tools.ToolDeleteAccountBackups, "", slug, trail.ResultOK)
	}
}
