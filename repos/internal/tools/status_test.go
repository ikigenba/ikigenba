package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

// R-9B5A-8V19 R-9CD6-MMRY R-9G0V-RY01
func TestStatusArgumentsAndEmptyOwner(t *testing.T) {
	f := newToolsFixture(t)
	f.create(t, "other", "private")
	refusal(t, f.call(t, "status", `{"repo":"notes"}`), "invalid arguments:\nrepo: unknown field")
	refusal(t, f.call(t, "status", `{"second":2,"first":1}`), "invalid arguments:\nsecond: unknown field\nfirst: unknown field")
	p := f.Limits.Pressure()
	want := fmt.Sprintf(`{"read":{"slots":%d,"active":0,"queued":0},"write":{"slots":%d,"active":0,"queued":0},"repos":[]}`, p.Read.Slots, p.Write.Slots)
	toolsEqual(t, string(successObject(t, f.call(t, "status", ""))), want)
	toolsEqual(t, string(successObject(t, f.call(t, "status", "{}"))), want)
	assertOnlyToolCalls(t, f, 0, "invalid_arguments", "invalid_arguments", "ok", "ok")
}

func statusRepository(t *testing.T, f *toolsFixture, r store.Repo, limit int64, busy bool) string {
	t.Helper()
	size, err := f.Store.Size(toolsContext(t), r.ID)
	toolsMust(t, err)
	return fmt.Sprintf(`{"id":%q,"name":%q,"size_bytes":%d,"limit_bytes":%d,"available":%t,"busy":%t}`, r.ID, r.Name, size, limit, r.Available, busy)
}

// R-9CD6-MMRY R-9ESZ-E69C R-9G0V-RY01
func TestStatusReadsPressureAndBusyWithoutTakingResources(t *testing.T) {
	f := newToolsFixture(t)
	zeta := f.create(t, f.Caller.UserID, "zeta")
	alpha := f.create(t, f.Caller.UserID, "alpha")
	beta := f.create(t, f.Caller.UserID, "beta")
	other := f.create(t, "other", "private")
	toolsMust(t, os.WriteFile(filepath.Join(f.Store.Dir(alpha.ID), "measured"), []byte("current size"), 0600))
	toolsMust(t, os.Remove(filepath.Join(f.Store.Dir(zeta.ID), "HEAD")))
	toolsMust(t, f.Store.Verify(toolsContext(t), f.Writer))
	zeta, err := f.Store.Find(toolsContext(t), f.Caller.UserID, zeta.ID)
	toolsMust(t, err)
	toolsEqual(t, zeta.Available, false)
	s := settings.Defaults()
	s.ReadSlots, s.WriteSlots, s.QueueLength, s.RepoMaxBytes = 1, 1, 1, 12345
	armed := make(chan struct{}, 2)
	var afterCalls atomic.Int64
	l := limits.New(s, limits.Clock{Now: f.StoreConfig.Now, After: func(time.Duration) <-chan time.Time {
		afterCalls.Add(1)
		armed <- struct{}{}
		return make(chan time.Time)
	}})
	f.Limits = l
	f.Client = serveTools(t, tools.Config{Store: f.Store, Limits: l, Telemetry: f.Writer}, f.Base, false)
	read, err := l.Acquire(toolsContext(t), alpha.ID, limits.Fetch, false)
	toolsMust(t, err)
	t.Cleanup(read.Release)
	write, err := l.Acquire(toolsContext(t), other.ID, limits.Push, true)
	toolsMust(t, err)
	t.Cleanup(write.Release)
	release, ok := l.TryHold(beta.ID)
	toolsEqual(t, ok, true)
	t.Cleanup(release)
	readCancel, readDone := statusQueue(t, l, zeta.ID, limits.Fetch)
	writeCancel, writeDone := statusQueue(t, l, zeta.ID, limits.Push)
	for range 2 {
		select {
		case <-armed:
		case <-toolsContext(t).Done():
			t.Fatal("queue did not arm its injected timer")
		}
	}
	before := l.Pressure()
	toolsEqual(t, before, limits.Pressure{Read: limits.Usage{Slots: 1, Active: 1, Queued: 1}, Write: limits.Usage{Slots: 1, Active: 1, Queued: 1}})
	catalog, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	offset := len(f.events(t))
	want := `{"read":{"slots":1,"active":1,"queued":1},"write":{"slots":1,"active":1,"queued":1},"repos":[` + statusRepository(t, f, alpha, s.RepoMaxBytes, true) + `,` + statusRepository(t, f, beta, s.RepoMaxBytes, true) + `,` + statusRepository(t, f, zeta, s.RepoMaxBytes, false) + `]}`
	toolsEqual(t, string(successObject(t, f.call(t, "status", "{}"))), want)
	toolsEqual(t, l.Pressure(), before)
	toolsEqual(t, afterCalls.Load(), int64(2))
	toolsEqual(t, l.Busy(alpha.ID), true)
	toolsEqual(t, l.Busy(beta.ID), true)
	toolsEqual(t, l.Busy(zeta.ID), false)
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, catalog)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	assertOnlyToolCalls(t, f, offset, "ok")
	result, err := f.Client.CallTool(toolsContext(t), identity.Caller{UserID: "empty", RequestID: "empty-status"}, "status", json.RawMessage(`{}`))
	toolsMust(t, err)
	toolsEqual(t, string(successObject(t, result)), `{"read":{"slots":1,"active":1,"queued":1},"write":{"slots":1,"active":1,"queued":1},"repos":[]}`)
	// Cancellation removes both waiters before grants or the hold are released.
	readCancel()
	writeCancel()
	statusWaitCanceled(t, readDone)
	statusWaitCanceled(t, writeDone)
	toolsEqual(t, l.Pressure().Read.Queued, int64(0))
	toolsEqual(t, l.Pressure().Write.Queued, int64(0))
}

func statusQueue(t *testing.T, l *limits.Limits, repo string, op limits.Op) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(toolsContext(t))
	done := make(chan error, 1)
	go func() {
		g, err := l.Acquire(ctx, repo, op, op != limits.Fetch)
		if g != nil {
			g.Release()
		}
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-ctx.Done():
		}
	})
	return cancel, done
}

func statusWaitCanceled(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued operation returned %v, want cancellation", err)
		}
	case <-toolsContext(t).Done():
		t.Fatal("queued operation did not return after cancellation")
	}
}

// R-9DL3-0EIN
func TestStatusRefusesUnreachableList(t *testing.T) {
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	f.DB.SetFailing(true)
	refusal(t, f.call(t, "status", "{}"), "cannot reach the repositories; try again later")
	assertOnlyToolCalls(t, f, 0, "error")
}

type statusCancelOnAccess struct {
	context.Context
	cancel    context.CancelFunc
	threshold int64
	calls     atomic.Int64
}

func (c *statusCancelOnAccess) Err() error {
	if calls := c.calls.Add(1); c.threshold > 0 && calls >= c.threshold {
		c.cancel()
	}
	return c.Context.Err()
}

// R-9DL3-0EIN
func TestStatusRefusesSizeFailureAfterSuccessfulList(t *testing.T) {
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	f.create(t, f.Caller.UserID, "zeta")
	for completedSizes := range 2 {
		t.Run(fmt.Sprintf("completed_sizes_%d", completedSizes), func(t *testing.T) {
			// Measure successful reads through the public context seam, then
			// cancel when the next Size checks that context.
			ctx, cancel := context.WithCancel(toolsContext(t))
			defer cancel()
			probe := &statusCancelOnAccess{Context: ctx, cancel: cancel}
			repos, err := f.Store.List(probe, f.Caller.UserID)
			toolsMust(t, err)
			toolsEqual(t, len(repos), 2)
			for i := range completedSizes {
				_, err = f.Store.Size(probe, repos[i].ID)
				toolsMust(t, err)
			}
			threshold := probe.calls.Load() + 1
			decorate := func(ctx context.Context) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(ctx)
				return &statusCancelOnAccess{Context: ctx, cancel: cancel, threshold: threshold}, cancel
			}
			// Prove the injected context permits List and earlier Size calls
			// but makes this particular Size fail before using it over MCP.
			check, stop := decorate(toolsContext(t))
			defer stop()
			_, err = f.Store.List(check, f.Caller.UserID)
			toolsMust(t, err)
			for i := range completedSizes {
				_, err = f.Store.Size(check, repos[i].ID)
				toolsMust(t, err)
			}
			_, err = f.Store.Size(check, repos[completedSizes].ID)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("injected Size error = %v, want cancellation", err)
			}
			f.Client = queryContextClient(t, f, decorate)
			offset := len(f.events(t))
			// With no Size to make, the same context permits success.
			result, err := f.Client.CallTool(toolsContext(t), identity.Caller{UserID: "empty", RequestID: "empty-control"}, "status", json.RawMessage(`{}`))
			toolsMust(t, err)
			successObject(t, result)
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			refusal(t, f.call(t, "status", "{}"), "cannot reach the repositories; try again later")
			after, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, after, before)
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			assertOnlyToolCalls(t, f, offset, "ok", "error")
		})
	}
}
