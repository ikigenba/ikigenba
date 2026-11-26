package ops_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const accountURL = "https://accounts.example.test"

var author = identity.Caller{UserID: "operator", RequestID: "original-request"}

type fixture struct {
	t       *testing.T
	ctx     context.Context
	now     time.Time
	db      *db.DB
	st      *store.Store
	memory  *memory.Cloud
	capture *telemetry.Capture
	cfg     ops.Config
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, ctx: context.Background(), now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	d, err := db.Open(f.ctx, db.Config{Path: filepath.Join(dir, "accounts.db"), Migrations: accounts.Migrations()})
	ok(t, err)
	f.db = d
	t.Cleanup(func() { ok(t, d.Close()) })
	f.st = store.New(d, store.Config{Now: f.clock, Rand: testRandom(1)})
	f.memory, err = memory.New(memory.Config{Path: filepath.Join(dir, "cloud.json"), Now: f.clock, Rand: testRandom(2), Hosts: settings.SandboxHosts})
	ok(t, err)
	f.capture = &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "accounts", Sink: f.capture, Now: f.clock, Rand: testRandom(3), Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(f.ctx, "done") })
	f.cfg = ops.Config{Store: f.st, Cloud: f.memory, Hosts: settings.SandboxHosts,
		AccountsURL: func() (string, error) { return accountURL, nil },
		Telemetry:   w, Now: f.clock, After: time.After}
	return f
}

func testRandom(seed byte) io.Reader { return rand.NewChaCha8([32]byte{seed}) }

func (f *fixture) clock() time.Time { return f.now }

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func equal[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func (f *fixture) create() (store.Host, store.Operation) {
	f.t.Helper()
	h, op, err := f.st.CreateHost(f.ctx, store.HostSpec{Type: "t4g.medium", RootGB: 20, DataGB: 50}, author)
	ok(f.t, err)
	return h, op
}

func (f *fixture) ready() store.Host {
	f.t.Helper()
	h, op := f.create()
	f.complete(ops.New(f.cfg), op.ID)
	return f.host(h.Name)
}

func (f *fixture) host(name string) store.Host {
	f.t.Helper()
	h, err := f.st.Host(f.ctx, name)
	ok(f.t, err)
	return h
}

func (f *fixture) operation(id string) store.Operation {
	f.t.Helper()
	op, err := f.st.Operation(f.ctx, id)
	ok(f.t, err)
	return op
}

func (f *fixture) account(slug string) store.Account {
	f.t.Helper()
	a, err := f.st.Account(f.ctx, slug)
	ok(f.t, err)
	return a
}

func (f *fixture) start(h store.Host, kind string, target store.Target) store.Operation {
	f.t.Helper()
	_, op, err := f.st.StartHostOp(f.ctx, h.Name, kind, target, author)
	ok(f.t, err)
	return op
}

func (f *fixture) deleteHost(h store.Host, keep bool, caller identity.Caller) store.Operation {
	f.t.Helper()
	_, op, err := f.st.DeleteHost(f.ctx, h.Name, keep, caller)
	ok(f.t, err)
	return op
}

func (f *fixture) place(slug, host string) (store.Account, store.Operation) {
	f.t.Helper()
	a, op, err := f.st.PlaceAccount(f.ctx, slug, host, author)
	ok(f.t, err)
	return a, op
}

func (f *fixture) remove(slug string) (store.Account, store.Operation) {
	f.t.Helper()
	a, op, err := f.st.RemoveAccount(f.ctx, slug, author)
	ok(f.t, err)
	return a, op
}

func (f *fixture) step(e *ops.Engine, id string) store.Operation {
	f.t.Helper()
	f.now = f.now.Add(ReconcileAdvance)
	ok(f.t, e.Pass(f.ctx))
	return f.operation(id)
}

const ReconcileAdvance = 25 * time.Second

func (f *fixture) complete(e *ops.Engine, id string) {
	f.t.Helper()
	for range 30 {
		op := f.step(e, id)
		switch op.Status {
		case store.OpDone:
			return
		case store.OpRunning:
		default:
			f.t.Fatalf("operation ended: %+v", op)
		}
	}
	f.t.Fatalf("operation stayed in step %s", f.operation(id).Step)
}

func (f *fixture) report(host string, attempted, applied int64) {
	f.t.Helper()
	r := protocol.Report{Protocol: protocol.Version, Attempted: attempted, Applied: applied}
	if attempted > applied {
		r.Error = "fixture apply failure"
	}
	_, err := f.st.RecordReport(f.ctx, host, r, f.now)
	ok(f.t, err)
}

func (f *fixture) events() []telemetry.Event {
	f.t.Helper()
	ok(f.t, f.cfg.Telemetry.Flush(f.ctx))
	return f.capture.Events()
}

func (f *fixture) observeEventFormation(verify func()) func() {
	f.t.Helper()
	observe := true
	w := telemetry.New(telemetry.Config{Service: "accounts", Sink: f.capture, Stderr: io.Discard,
		Now: func() time.Time {
			if observe {
				verify()
			}
			return f.clock()
		},
	})
	f.t.Cleanup(func() {
		observe = false
		w.Shutdown(f.ctx, "done")
	})
	f.cfg.Telemetry = w
	return func() { observe = false }
}

func cloudErr(class cloud.Class) error {
	return &cloud.Error{Service: "ec2", Operation: "fixture", Code: "fixture-code", Class: class}
}

// R-MGI1-RGH5 R-MHPY-587U R-SE00-KES3 R-MIXU-IZYJ R-MK5Q-WRP8 R-UGS4-7D6X
func TestOperationSurface(t *testing.T) {
	got := []string{
		ops.StepAddress, ops.StepLaunch, ops.StepWaitRunning, ops.StepVolume, ops.StepWaitVolume,
		ops.StepAttach, ops.StepWaitAttached, ops.StepAssociate, ops.StepStop, ops.StepWaitStopped,
		ops.StepStart, ops.StepGrowRoot, ops.StepWaitRoot, ops.StepGrowData, ops.StepWaitData, ops.StepSetType,
		ops.StepUnrecord, ops.StepWaitUnrecord, ops.StepTerminate, ops.StepWaitTerminated, ops.StepRelease, ops.StepOrphan,
		ops.StepWaitAddress, ops.StepRecord, ops.StepWaitRecord, ops.StepWaitApplied,
	}
	want := strings.Fields("address launch wait_running volume wait_volume attach wait_attached associate stop wait_stopped start grow_root wait_root grow_data wait_data set_type unrecord wait_unrecord terminate wait_terminated release orphan wait_address record wait_record wait_applied")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("steps %v, want %v", got, want)
	}
	equal(t, ops.PollInterval, 5*time.Second)
	equal(t, ops.ReconcileInterval, time.Minute)
	equal(t, ops.ScanInterval, time.Hour)
	equal(t, ops.StepTimeout, 30*time.Minute)
	equal(t, ops.MaxBackoff, 5*time.Minute)
	equal(t, ops.RecordSettle, time.Minute)
	equal(t, ops.MaxGenerations, 3)
	equal(t, ops.DataDevice, "/dev/sdf")
	equal(t, ops.RecordTTL, 300)
	reasons := map[string]bool{}
	for _, value := range []string{
		ops.ReasonTerminated, ops.ReasonTimedOut, ops.ReasonRecordConflict, ops.ReasonMissing,
		ops.ReasonNoAccountsURL, ops.ReasonModifyFailed, ops.ReasonVolumeFailed, ops.ReasonBadSize,
	} {
		if value == "" || strings.ContainsAny(value, "\r\n") || reasons[value] {
			t.Fatalf("invalid reason %q", value)
		}
		reasons[value] = true
	}
	f := setup(t)
	e := ops.New(ops.Config{
		Store: f.st, Cloud: f.memory, Hosts: f.cfg.Hosts, AccountsURL: f.cfg.AccountsURL,
		Telemetry: f.cfg.Telemetry, Now: f.clock, After: f.cfg.After,
	})
	e.Wake()
	ok(t, e.Pass(f.ctx))
	ok(t, e.Scan(f.ctx))
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	ok(t, e.Run(ctx))
}

// R-9D52-0H9Z
func TestHostRecordStepConstants(t *testing.T) {
	type stepName string
	const hostRecord stepName = ops.StepHostRecord
	const waitHostRecord stepName = ops.StepWaitHostRecord
	equal(t, hostRecord, stepName("host_record"))
	equal(t, waitHostRecord, stepName("wait_host_record"))
}

// R-968B-Q7DG
func TestDataDeletionStepConstants(t *testing.T) {
	type stepName string
	const deleteData stepName = ops.StepDeleteData
	const waitDataDeleted stepName = ops.StepWaitDataDeleted
	equal(t, deleteData, stepName("delete_data"))
	equal(t, waitDataDeleted, stepName("wait_data_deleted"))
}

// R-MLDN-AJFX R-NUHX-G1VM
func TestPassPagesOperationsAndSilentHosts(t *testing.T) {
	f := setup(t)
	e := ops.New(f.cfg)
	names := make(map[string]string)
	for range 120 {
		h, op := f.create()
		names[op.ID] = h.Name
	}
	ok(t, e.Pass(f.ctx))
	for id, name := range names {
		op := f.operation(id)
		equal(t, op.Step, ops.StepHostRecord)
		if f.host(name).AllocationID == "" {
			t.Fatalf("host %s has no allocation", name)
		}
		ok(t, f.st.Finish(f.ctx, id, store.Update{}))
	}

	f.now = f.now.Add(store.SilentAfter + time.Second)
	callerCtx := identity.NewContext(f.ctx, author)
	ok(t, e.Pass(callerCtx))
	ok(t, e.Pass(callerCtx))
	seen := map[string]bool{}
	for _, event := range f.events() {
		equal(t, event.Name, trail.EventHostSilent)
		equal(t, event.RequestID, "")
		equal(t, event.User, "")
		host, valid := event.Attrs["host"].(string)
		if !valid || seen[host] {
			t.Fatalf("bad silent host %+v", event)
		}
		seen[host] = true
		equal(t, event.Attrs["applied"], any(f.host(host).Applied))
		equal(t, f.host(host).Alarm, store.AlarmSilent)
	}
	equal(t, len(seen), 120)
}

// R-NY5M-LD3P R-O1TB-QOBS R-NZDI-Z4UE
func TestRunSchedulingAndStoreRecovery(t *testing.T) {
	f := setup(t)
	_, op := f.create()
	ok(t, f.st.RecordStep(f.ctx, op.ID, ops.StepAddress, 1, f.now))
	ok(t, f.st.Defer(f.ctx, op.ID, time.Unix(math.MaxInt64, 0), 0))
	equal(t, f.operation(op.ID).NextTry.Unix(), int64(math.MaxInt64))
	times := make(chan time.Duration)
	tick := make(chan time.Time)
	f.cfg.After = func(d time.Duration) <-chan time.Time {
		times <- d
		return tick
	}
	cl := &scanCloud{Cloud: f.memory}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	defer cancel()
	waitFor := func(want time.Duration) {
		t.Helper()
		select {
		case got := <-times:
			equal(t, got, want)
		case <-time.After(5 * time.Second):
			t.Fatal("engine never scheduled next pass")
		}
	}
	waitFor(ops.PollInterval)
	equal(t, f.operation(op.ID).Step, ops.StepHostRecord)
	equal(t, cl.scans, 1)
	f.db.SetFailing(true)
	e.Wake()
	waitFor(ops.ReconcileInterval)
	f.db.SetFailing(false)
	equal(t, f.operation(op.ID).Step, ops.StepHostRecord)
	ok(t, f.st.Defer(f.ctx, op.ID, f.now.Add(17*time.Second), 0))
	e.Wake()
	waitFor(17 * time.Second)
	equal(t, f.operation(op.ID).Step, ops.StepHostRecord)
	f.now = f.now.Add(17 * time.Second)
	tick <- f.now
	waitFor(ops.PollInterval)
	equal(t, f.operation(op.ID).Step, ops.StepWaitHostRecord)
	for range 2 {
		f.now = f.now.Add(ReconcileAdvance)
		tick <- f.now
		waitFor(ops.PollInterval)
	}
	equal(t, f.operation(op.ID).Step, ops.StepWaitRunning)

	f.now = f.now.Add(ops.ScanInterval)
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepWaitRunning}))
	tick <- f.now
	waitFor(ops.PollInterval)
	equal(t, cl.scans, 2)
	cancel()
	ok(t, <-done)

	f.db.SetFailing(true)
	err := e.Pass(f.ctx)
	if !errors.Is(err, store.ErrUnreachable) {
		t.Fatalf("pass with failing store: %v", err)
	}
	f.db.SetFailing(false)
	equal(t, f.operation(op.ID).Step, ops.StepVolume)
}

type scanCloud struct {
	cloud.Cloud
	scans int
}

func (c *scanCloud) FindInstances(ctx context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	if tags[cloud.TagOperation] == "" {
		c.scans++
	}
	return c.Cloud.FindInstances(ctx, tags, each)
}

// R-0DM7-JP72 R-NWXQ-7LD0
func TestScanNotesEveryPageAndPreservesResources(t *testing.T) {
	f := setup(t)
	scanTime := f.now.Add(2 * time.Hour)
	f.cfg.Now = func() time.Time { return scanTime }
	// Reject stray notes stamped with anything other than the scan's clock.
	ok(t, f.db.Write(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, fmt.Sprintf(`CREATE TRIGGER require_scan_time BEFORE INSERT ON strays
			WHEN NEW.noted IS NOT %d BEGIN SELECT RAISE(ABORT, 'unexpected stray timestamp'); END`, scanTime.Unix()))
		return err
	}))
	h, op := f.create()
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{InstanceID: "i-held"}))
	cl := &pagedStrays{st: f.st, t: t, runningOp: op.ID, now: f.cfg.Now}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	ctx := identity.NewContext(f.ctx, author)
	ok(t, e.Scan(ctx))
	eventWith(t, f.events(), trail.EventResourceUnexplained, telemetry.Attrs{
		"resource": "i-unknown-op", "kind": trail.ResourceInstance, "host": "",
	}, identity.Caller{})
	ok(t, e.Scan(ctx))
	if err := f.st.Finish(f.ctx, op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	e = ops.New(f.cfg)
	ok(t, e.Scan(ctx))
	want := map[string][2]string{
		"i-stray1": {trail.ResourceInstance, "retired"}, "i-stray2": {trail.ResourceInstance, ""},
		"vol-root": {trail.ResourceVolume, ""}, "vol-data": {trail.ResourceVolume, ""},
		"eip-stray": {trail.ResourceAddress, ""}, "i-running": {trail.ResourceInstance, ""},
		"i-unknown-op": {trail.ResourceInstance, ""},
	}
	for _, ev := range f.events() {
		equal(t, ev.Name, trail.EventResourceUnexplained)
		equal(t, ev.User, "")
		equal(t, ev.RequestID, "")
		id, valid := ev.Attrs["resource"].(string)
		if !valid || !reflect.DeepEqual(ev.Attrs, telemetry.Attrs{"resource": id, "kind": want[id][0], "host": want[id][1]}) {
			t.Fatalf("unexpected event %+v", ev)
		}
		if _, found := want[id]; !found {
			t.Fatalf("unexpected resource %q", id)
		}
		delete(want, id)
	}
	equal(t, len(want), 0)
	equal(t, f.host(h.Name).InstanceID, "i-held")
}

type pagedStrays struct {
	cloud.Cloud
	st        *store.Store
	t         *testing.T
	runningOp string
	now       func() time.Time
}

func (c *pagedStrays) FindInstances(ctx context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	c.t.Helper()
	if !reflect.DeepEqual(tags, cloud.Tags{cloud.TagKind: cloud.KindHost}) {
		c.t.Fatalf("instance tags %v", tags)
	}
	if !each([]cloud.Instance{{ID: "i-stray1", Tags: cloud.Tags{cloud.TagHost: "retired"}}}) {
		return nil
	}
	added, err := c.st.NoteStray(ctx, "i-stray1", trail.ResourceInstance, "retired", c.now())
	ok(c.t, err)
	equal(c.t, added, false)
	each([]cloud.Instance{
		{ID: "i-stray2"},
		{ID: "i-held"},
		{ID: "i-running", Tags: cloud.Tags{cloud.TagOperation: c.runningOp}},
		{ID: "i-unknown-op", Tags: cloud.Tags{cloud.TagOperation: "ffffffffffffffff"}},
	})
	return nil
}

func (c *pagedStrays) FindAddresses(_ context.Context, tags cloud.Tags, each func([]cloud.Address) bool) error {
	equal(c.t, tags[cloud.TagKind], cloud.KindHost)
	each([]cloud.Address{{AllocationID: "eip-stray"}})
	return nil
}

func (c *pagedStrays) FindVolumes(_ context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	switch tags[cloud.TagKind] {
	case cloud.KindHost:
		each([]cloud.Volume{{ID: "vol-root"}})
	case cloud.KindHostData:
		each([]cloud.Volume{{ID: "vol-data"}})
	default:
		return fmt.Errorf("unexpected volume tags: %v", tags)
	}
	return nil
}
