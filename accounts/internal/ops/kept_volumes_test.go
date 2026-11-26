package ops_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type keptVolumeCloud struct {
	cloud.Cloud
	describe func(context.Context, string) (cloud.Volume, error)
	delete   func(context.Context, string) error
}

func (c keptVolumeCloud) DescribeVolume(ctx context.Context, id string) (cloud.Volume, error) {
	return c.describe(ctx, id)
}

func (c keptVolumeCloud) DeleteVolume(ctx context.Context, id string) error {
	return c.delete(ctx, id)
}

func (f *fixture) keptVolume(id string) store.KeptVolume {
	f.t.Helper()
	v, err := f.st.KeptVolume(f.ctx, id)
	ok(f.t, err)
	return v
}

func (f *fixture) keepVolume(id string) {
	f.t.Helper()
	h, create := f.create()
	ok(f.t, f.st.Finish(f.ctx, create.ID, store.Update{DataVolume: id}))
	op := f.deleteHost(h, true, author)
	ok(f.t, f.st.Finish(f.ctx, op.ID, store.Update{}))
}

func (f *fixture) scheduleKeptVolume(id string, caller identity.Caller) {
	f.t.Helper()
	_, err := f.st.DeleteKeptVolume(f.ctx, id, caller)
	ok(f.t, err)
}

// R-9DJQ-0TTM R-9JN7-XOJ3
func TestPassWorksEachDueKeptVolumeAfterItsOperations(t *testing.T) {
	f := setup(t)
	for i := range 120 {
		id := fmt.Sprintf("vol-%03d", i)
		f.keepVolume(id)
		f.scheduleKeptVolume(id, author)
	}
	below := fmt.Sprintf("vol-%03d-new", store.BatchSize-2)
	above := fmt.Sprintf("vol-%03d-new", store.BatchSize-1)
	for _, id := range []string{"later-id", "kept-id", "deleted-id", below, above} {
		f.keepVolume(id)
	}
	f.scheduleKeptVolume("later-id", author)
	ok(t, f.st.DeferVolumeDelete(f.ctx, "later-id", f.now.Add(time.Minute), 0))
	f.scheduleKeptVolume("deleted-id", author)
	ok(t, f.st.FinishVolumeDelete(f.ctx, "deleted-id"))
	_, create := f.create()
	worked := make([]string, 0, 121)
	f.cfg.Cloud = keptVolumeCloud{Cloud: f.memory,
		describe: func(_ context.Context, id string) (cloud.Volume, error) {
			equal(t, f.operation(create.ID).Step, ops.StepHostRecord)
			return cloud.Volume{ID: id, State: cloud.VolumeAvailable}, nil
		},
		delete: func(_ context.Context, id string) error {
			worked = append(worked, id)
			if len(worked) == 1 {
				f.scheduleKeptVolume(below, author)
				f.scheduleKeptVolume(above, author)
			}
			return nil
		},
	}
	e := ops.New(f.cfg)
	ok(t, e.Pass(f.ctx))
	want := make([]string, 0, 121)
	for i := range 120 {
		if i == store.BatchSize {
			want = append(want, above)
		}
		want = append(want, fmt.Sprintf("vol-%03d", i))
	}
	checkIDs(t, worked, want)
	equal(t, len(f.events()), 0)
}

// R-9ERM-ELKB R-9H7F-651P
func TestKeptVolumeOutcomesAndStoredCaller(t *testing.T) {
	notFound := cloudErr(cloud.ClassNotFound)
	transient := cloudErr(cloud.ClassTransient)
	refused := &cloud.Error{Service: "ec2", Operation: "DeleteVolume", Code: "VolumeInUse", Class: cloud.ClassRefused}
	mismatch := cloudErr(cloud.ClassMismatch)
	for _, tc := range []struct {
		name, state, outcome string
		readErr, deleteErr   error
		attempts, added      int
		delay                time.Duration
		deletes              int
	}{
		{name: "already missing", readErr: notFound, outcome: store.VolumeDeleted},
		{name: "already deleted", state: cloud.VolumeDeleted, outcome: store.VolumeDeleted},
		{name: "deleting", state: cloud.VolumeDeleting, outcome: store.VolumeDeleting, attempts: 3, delay: ops.PollInterval},
		{name: "available", state: cloud.VolumeAvailable, outcome: store.VolumeDeleting, attempts: 1, delay: ops.PollInterval, deletes: 1},
		{name: "gone during delete", state: cloud.VolumeAvailable, deleteErr: notFound, outcome: store.VolumeDeleted, deletes: 1},
		{name: "throttled read", readErr: transient, outcome: store.VolumeDeleting, attempts: 2, added: 1, delay: 4 * ops.PollInterval},
		{name: "large attempt count", state: cloud.VolumeAvailable, deleteErr: transient, outcome: store.VolumeDeleting,
			attempts: math.MaxInt / 2, added: 1, delay: ops.MaxBackoff, deletes: 1},
		{name: "attached", state: cloud.VolumeInUse, deleteErr: refused, outcome: store.VolumeKept, deletes: 1},
		{name: "mismatched read", readErr: mismatch, outcome: store.VolumeKept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			const id = "vol-kept"
			f.keepVolume(id)
			caller := identity.Caller{UserID: "original-deleter", RequestID: "original-delete-request"}
			f.scheduleKeptVolume(id, caller)
			ok(t, f.st.DeferVolumeDelete(f.ctx, id, f.now, tc.attempts))
			f.now = f.now.Add(24 * time.Hour)
			reads, deletes := 0, 0
			cl := &keptVolumeCloud{Cloud: f.memory,
				describe: func(_ context.Context, got string) (cloud.Volume, error) {
					equal(t, got, id)
					reads++
					return cloud.Volume{ID: got, State: tc.state}, tc.readErr
				},
				delete: func(_ context.Context, got string) error {
					equal(t, got, id)
					deletes++
					return tc.deleteErr
				},
			}
			f.cfg.Cloud = cl
			before := f.keptVolume(id)
			formations := 0
			stopObserving := f.observeEventFormation(func() {
				formations++
				got, err := f.st.KeptVolume(f.ctx, id)
				switch tc.outcome {
				case store.VolumeDeleted:
					equal(t, errors.Is(err, store.ErrNotFound), true)
				case store.VolumeKept:
					ok(t, err)
					equal(t, got.State, store.VolumeKept)
				default:
					t.Fatal("event emitted for a deferred deletion")
				}
			})
			defer stopObserving()
			e := ops.New(f.cfg)
			ok(t, e.Scan(f.ctx))
			// Only DescribeVolume and DeleteVolume are allowed in the
			// pass, since the scan was already completed.
			cl.Cloud = nil
			other := identity.NewContext(f.ctx, identity.Caller{UserID: "another-user"})
			ok(t, e.Pass(other))
			equal(t, reads, 1)
			equal(t, deletes, tc.deletes)
			events := f.events()
			equal(t, formations, len(events))
			var reason string
			switch tc.outcome {
			case store.VolumeDeleting:
				after := f.keptVolume(id)
				equal(t, after.State, tc.outcome)
				equal(t, after.Reason, "")
				equal(t, after.NextTry, f.now.Add(tc.delay))
				equal(t, after.Attempts, tc.attempts+tc.added)
				equal(t, len(events), 0)
				return
			case store.VolumeDeleted:
				_, err := f.st.KeptVolume(f.ctx, id)
				equal(t, errors.Is(err, store.ErrNotFound), true)
			case store.VolumeKept:
				reason = errors.Join(tc.readErr, tc.deleteErr).Error()
				after := f.keptVolume(id)
				equal(t, after.State, tc.outcome)
				equal(t, after.Reason, reason)
			}
			equal(t, len(events), 1)
			eventWith(t, events, trail.EventVolumeChanged, telemetry.Attrs{
				"volume": id, "host": before.Host, "from": store.VolumeDeleting, "to": tc.outcome, "reason": reason,
			}, caller)
		})
	}
}

// R-9IFB-JWSE
func TestKeptVolumePassDropsEndedAndMissingWork(t *testing.T) {
	f := setup(t)
	for _, id := range []string{"already-ended", "record-missing", "remaining"} {
		f.keepVolume(id)
		f.scheduleKeptVolume(id, author)
	}
	f.cfg.Cloud = keptVolumeCloud{Cloud: f.memory,
		describe: func(_ context.Context, id string) (cloud.Volume, error) {
			switch id {
			case "already-ended":
				ok(t, f.st.FailVolumeDelete(f.ctx, id, "another worker finished"))
			case "record-missing":
				// The store has no public API for removing a row. Force
				// the missing-record result after the pass read its page.
				ok(t, f.db.Write(f.ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(f.ctx, "DELETE FROM kept_volumes WHERE volume = ?", id)
					return err
				}))
			}
			return cloud.Volume{ID: id, State: cloud.VolumeDeleted}, nil
		},
	}
	ok(t, ops.New(f.cfg).Pass(f.ctx))
	equal(t, f.keptVolume("already-ended").Reason, "another worker finished")
	_, err := f.st.KeptVolume(f.ctx, "remaining")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	events := f.events()
	equal(t, len(events), 1)
	equal(t, events[0].Attrs["volume"], any("remaining"))
}

// R-9IFB-JWSE R-9H7F-651P
func TestKeptVolumePassRestartsInterruptedWork(t *testing.T) {
	f := setup(t)
	const id = "interrupted-volume"
	f.keepVolume(id)
	f.scheduleKeptVolume(id, author)
	before := f.keptVolume(id)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	f.cfg.Cloud = keptVolumeCloud{Cloud: f.memory,
		describe: func(_ context.Context, _ string) (cloud.Volume, error) {
			return cloud.Volume{State: cloud.VolumeAvailable}, nil
		},
		delete: func(ctx context.Context, _ string) error {
			cancel()
			return ctx.Err()
		},
	}
	equal(t, errors.Is(ops.New(f.cfg).Pass(ctx), context.Canceled), true)
	equal(t, f.keptVolume(id), before)
	equal(t, len(f.events()), 0)

	f.cfg.Cloud = keptVolumeCloud{Cloud: f.memory,
		describe: func(_ context.Context, _ string) (cloud.Volume, error) {
			f.db.SetFailing(true)
			return cloud.Volume{State: cloud.VolumeDeleted}, nil
		},
	}
	err := ops.New(f.cfg).Pass(f.ctx)
	f.db.SetFailing(false)
	equal(t, errors.Is(err, store.ErrUnreachable), true)
	equal(t, f.keptVolume(id), before)
	equal(t, len(f.events()), 0)

	f.cfg.Cloud = keptVolumeCloud{Cloud: f.memory,
		describe: func(_ context.Context, _ string) (cloud.Volume, error) {
			return cloud.Volume{State: cloud.VolumeDeleted}, nil
		},
	}
	restarted, stop := context.WithCancel(f.ctx)
	defer stop()
	f.cfg.After = func(_ time.Duration) <-chan time.Time {
		stop()
		return nil
	}
	ok(t, ops.New(f.cfg).Run(restarted))
	_, err = f.st.KeptVolume(f.ctx, id)
	equal(t, errors.Is(err, store.ErrNotFound), true)
}

func (f *fixture) legacyDelete(h store.Host, create store.Operation, id string) {
	f.t.Helper()
	ok(f.t, f.st.Finish(f.ctx, create.ID, store.Update{}))
	op := f.deleteHost(h, true, author)
	ok(f.t, f.st.Finish(f.ctx, op.ID, store.Update{}))
	// Supply data retained by an earlier release, which did not have a
	// kept-volume record when it finished the delete.
	ok(f.t, f.db.Write(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, "UPDATE hosts SET data_volume = ? WHERE name = ?", id, h.Name)
		return err
	}))
}

// R-9M30-P80H
func TestScanAdoptsLegacyKeptVolumesAcrossBatches(t *testing.T) {
	f := setup(t)
	f.keepVolume("kept-before-scan")
	f.keepVolume("deleted-before-scan")
	f.scheduleKeptVolume("deleted-before-scan", author)
	ok(t, f.st.FinishVolumeDelete(f.ctx, "deleted-before-scan"))
	keptBefore := f.keptVolume("kept-before-scan")
	type legacy struct {
		host   store.Host
		create store.Operation
	}
	var old []legacy
	for range 120 {
		h, create := f.create()
		old = append(old, legacy{h, create})
	}
	for i, item := range old {
		f.legacyDelete(item.host, item.create, fmt.Sprintf("legacy-volume-%03d", i))
	}
	cloudScan := &adoptionListCloud{}
	f.cfg.Cloud = cloudScan
	e := ops.New(f.cfg)
	expectedListings := []string{"instances", "addresses", cloud.KindHost, cloud.KindHostData}
	slices.Sort(expectedListings)
	ok(t, e.Scan(f.ctx))
	slices.Sort(cloudScan.calls)
	checkIDs(t, cloudScan.calls, expectedListings)
	visible := make(map[string]store.KeptVolume, len(old)+1)
	var after time.Time
	var cursor string
	for {
		page, err := f.st.ListKeptVolumes(f.ctx, after, cursor, store.BatchSize)
		ok(t, err)
		for _, volume := range page {
			visible[volume.Volume] = volume
		}
		if len(page) < store.BatchSize {
			break
		}
		last := page[len(page)-1]
		after, cursor = last.Deleted, last.Volume
	}
	equal(t, len(visible), len(old)+1)
	for i, item := range old {
		volume := visible[fmt.Sprintf("legacy-volume-%03d", i)]
		equal(t, volume.State, store.VolumeKept)
		equal(t, volume.Host, item.host.Name)
	}
	equal(t, visible[keptBefore.Volume], keptBefore)
	_, deletedVisible := visible["deleted-before-scan"]
	equal(t, deletedVisible, false)
	cloudScan.calls = nil
	ok(t, e.Scan(f.ctx))
	slices.Sort(cloudScan.calls)
	checkIDs(t, cloudScan.calls, expectedListings)
	_, err := f.st.KeptVolume(f.ctx, "deleted-before-scan")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	equal(t, len(f.events()), 0)
}

// The rest of Scan lists possible strays. The embedded nil Cloud makes any
// other request unexpected, and each legitimate listing is counted.
type adoptionListCloud struct {
	cloud.Cloud
	calls []string
}

func (c *adoptionListCloud) FindInstances(context.Context, cloud.Tags, func([]cloud.Instance) bool) error {
	c.calls = append(c.calls, "instances")
	return nil
}

func (c *adoptionListCloud) FindAddresses(context.Context, cloud.Tags, func([]cloud.Address) bool) error {
	c.calls = append(c.calls, "addresses")
	return nil
}

func (c *adoptionListCloud) FindVolumes(_ context.Context, tags cloud.Tags, _ func([]cloud.Volume) bool) error {
	c.calls = append(c.calls, tags[cloud.TagKind])
	return nil
}

type adoptionScanCloud struct {
	*recordCloud
	beforeAdoption func()
}

func (c adoptionScanCloud) FindVolumes(ctx context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	err := c.Cloud.FindVolumes(ctx, tags, each)
	if tags[cloud.TagKind] == cloud.KindHostData {
		c.beforeAdoption()
	}
	return err
}

// R-9KV4-BG9S
func TestScanJoinsAdoptionFailureAndRetriesIt(t *testing.T) {
	f := setup(t)
	live := f.runningWithAddress("192.0.2.11")
	leftover := f.runningWithAddress("192.0.2.12")
	h, create := f.create()
	f.legacyDelete(h, create, "legacy-volume")
	op := f.deleteHost(leftover, true, author)
	ok(t, f.st.Finish(f.ctx, op.ID, store.Update{}))

	recordErr := errors.New("live DNS lookup failed")
	leftoverErr := errors.New("leftover DNS lookup failed")
	base := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, find: func(name string) error {
		switch name {
		case live.Name + "." + f.cfg.Hosts.Domain:
			return recordErr
		case leftover.Name + "." + f.cfg.Hosts.Domain:
			return leftoverErr
		default:
			return nil
		}
	}}
	f.cfg.Cloud = adoptionScanCloud{recordCloud: base, beforeAdoption: func() { f.db.SetFailing(true) }}
	err := ops.New(f.cfg).Scan(f.ctx)
	f.db.SetFailing(false)
	equal(t, errors.Is(err, recordErr), true)
	equal(t, errors.Is(err, leftoverErr), true)
	equal(t, errors.Is(err, store.ErrUnreachable), true)
	_, err = f.st.KeptVolume(f.ctx, "legacy-volume")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	f.cfg.Cloud = f.memory
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	equal(t, f.keptVolume("legacy-volume").State, store.VolumeKept)
}
