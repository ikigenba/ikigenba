// Package ops advances recorded host and account operations against the cloud.
package ops

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Operation scheduling and cloud settings.
const (
	PollInterval      = 5 * time.Second
	ReconcileInterval = time.Minute
	ScanInterval      = time.Hour
	StepTimeout       = 30 * time.Minute
	MaxBackoff        = 5 * time.Minute
	MaxGenerations    = 3
	DataDevice        = "/dev/sdf"
	RecordTTL         = 300
	RecordSettle      = time.Minute
)

// Operation steps.
const (
	StepAddress        = "address"
	StepLaunch         = "launch"
	StepWaitRunning    = "wait_running"
	StepVolume         = "volume"
	StepWaitVolume     = "wait_volume"
	StepAttach         = "attach"
	StepWaitAttached   = "wait_attached"
	StepAssociate      = "associate"
	StepStop           = "stop"
	StepWaitStopped    = "wait_stopped"
	StepStart          = "start"
	StepGrowRoot       = "grow_root"
	StepWaitRoot       = "wait_root"
	StepGrowData       = "grow_data"
	StepWaitData       = "wait_data"
	StepSetType        = "set_type"
	StepUnrecord       = "unrecord"
	StepWaitUnrecord   = "wait_unrecord"
	StepTerminate      = "terminate"
	StepWaitTerminated = "wait_terminated"
	StepRelease        = "release"
	StepOrphan         = "orphan"
	StepWaitAddress    = "wait_address"
	StepRecord         = "record"
	StepWaitRecord     = "wait_record"
	StepWaitApplied    = "wait_applied"
)

// Reasons recorded when a step cannot finish.
const (
	ReasonTerminated     = "The instance terminated."
	ReasonTimedOut       = "The operation step timed out."
	ReasonRecordConflict = "The account name has a conflicting DNS record."
	ReasonMissing        = "A required cloud resource is missing."
	ReasonNoAccountsURL  = "The accounts URL is unavailable."
	ReasonModifyFailed   = "The volume modification failed."
	ReasonVolumeFailed   = "The data volume failed."
	ReasonBadSize        = "The volume size is out of range."
)

// Config supplies the engine's store, cloud, and scheduling seams.
type Config struct {
	Store       *store.Store
	Cloud       cloud.Cloud
	Hosts       settings.Hosts
	AccountsURL func() (string, error)
	Telemetry   *telemetry.Writer
	Now         func() time.Time
	After       func(time.Duration) <-chan time.Time
}

// Engine runs the operation loop. Wake may be called by a tool while Run is active.
type Engine struct {
	cfg        Config
	wake       chan struct{}
	lastScan   time.Time
	hasScanned bool
}

// New creates an operation engine.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	return &Engine{cfg: cfg, wake: make(chan struct{}, 1)}
}

// Wake requests another pass, coalescing pending requests.
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Run resumes recorded work immediately, then advances it until cancelled.
func (e *Engine) Run(ctx context.Context) error {
	first := true
	for ctx.Err() == nil {
		// A wake from before this pass is already accounted for. A wake sent
		// during the pass remains queued until the next wait.
		select {
		case <-e.wake:
		default:
		}
		_ = e.pass(ctx, first)
		first = false
		if ctx.Err() != nil {
			break
		}
		now := e.cfg.Now()
		next, err := e.cfg.Store.NextTry(ctx)
		delay := ReconcileInterval
		if err == nil && !next.IsZero() {
			delay = min(max(next.Sub(now), PollInterval), ReconcileInterval)
		}
		select {
		case <-ctx.Done():
		case <-e.wake:
		case <-e.cfg.After(delay):
		}
	}
	return nil
}

// Pass takes one step of each due operation, then runs the due sweeps.
func (e *Engine) Pass(ctx context.Context) error {
	return e.pass(ctx, false)
}

func (e *Engine) pass(ctx context.Context, first bool) error {
	now := e.cfg.Now()
	if err := e.steps(ctx, now, first); err != nil {
		return err
	}
	if err := e.silence(ctx, now); err != nil {
		return err
	}
	if first || !e.hasScanned || now.Sub(e.lastScan) >= ScanInterval {
		return e.Scan(ctx)
	}
	return nil
}

func (e *Engine) steps(ctx context.Context, now time.Time, first bool) error {
	if e.cfg.Cloud == nil {
		return nil
	}
	due := now
	if first {
		// Runnable compares the persisted Unix seconds as signed integers.
		due = time.Unix(math.MaxInt64, 0)
	}
	for after := ""; ; {
		page, err := e.cfg.Store.Runnable(ctx, due, after, store.BatchSize)
		if err != nil {
			return err
		}
		for _, op := range page {
			if err := e.take(ctx, now, op); err != nil {
				return err
			}
			after = op.ID
		}
		if len(page) < store.BatchSize {
			return nil
		}
	}
}

func (e *Engine) silence(ctx context.Context, now time.Time) error {
	guest := identity.NewContext(ctx, identity.Caller{})
	for after := ""; ; {
		page, err := e.cfg.Store.MarkSilent(ctx, now, after, store.BatchSize)
		if err != nil {
			return err
		}
		for _, h := range page {
			if e.cfg.Telemetry != nil {
				trail.HostSilent(guest, e.cfg.Telemetry, h.Name, h.Applied)
			}
			after = h.Name
		}
		if len(page) < store.BatchSize {
			return nil
		}
	}
}

// Scan notes unexplained resources without modifying them and prunes expired grows.
func (e *Engine) Scan(ctx context.Context) error {
	return e.scan(ctx, e.cfg.Now())
}

func (e *Engine) scan(ctx context.Context, now time.Time) error {
	e.lastScan, e.hasScanned = now, true
	if e.cfg.Cloud != nil {
		if err := e.scanResources(ctx, now); err != nil {
			return err
		}
	}
	for {
		n, err := e.cfg.Store.PruneGrows(ctx, now, store.BatchSize)
		if err != nil || n < store.BatchSize {
			return err
		}
	}
}

func (e *Engine) scanResources(ctx context.Context, now time.Time) error {
	guest := identity.NewContext(ctx, identity.Caller{})
	note := func(id, kind string, tags cloud.Tags) error {
		explained, err := e.cfg.Store.Explains(ctx, id)
		if err != nil || explained {
			return err
		}
		if id := tags[cloud.TagOperation]; id != "" {
			op, err := e.cfg.Store.Operation(ctx, id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if op.Status == store.OpRunning {
				return nil
			}
		}
		host := tags[cloud.TagHost]
		added, err := e.cfg.Store.NoteStray(ctx, id, kind, host, now)
		if err == nil && added && e.cfg.Telemetry != nil {
			trail.Unexplained(guest, e.cfg.Telemetry, id, kind, host)
		}
		return err
	}
	cl := e.cfg.Cloud
	tags := cloud.Tags{cloud.TagKind: cloud.KindHost}
	if err := eachResource(ctx, tags, cl.FindInstances, func(i cloud.Instance) error {
		return note(i.ID, trail.ResourceInstance, i.Tags)
	}); err != nil {
		return err
	}
	if err := eachResource(ctx, tags, cl.FindAddresses, func(a cloud.Address) error {
		return note(a.AllocationID, trail.ResourceAddress, a.Tags)
	}); err != nil {
		return err
	}
	for _, kind := range []string{cloud.KindHost, cloud.KindHostData} {
		if err := eachResource(ctx, cloud.Tags{cloud.TagKind: kind}, cl.FindVolumes, func(v cloud.Volume) error {
			return note(v.ID, trail.ResourceVolume, v.Tags)
		}); err != nil {
			return err
		}
	}
	return nil
}

func eachResource[T any](ctx context.Context, tags cloud.Tags, find func(context.Context, cloud.Tags, func([]T) bool) error, use func(T) error) error {
	var useErr error
	err := find(ctx, tags, func(page []T) bool {
		for _, item := range page {
			if useErr = use(item); useErr != nil {
				return false
			}
		}
		return true
	})
	if useErr != nil {
		return useErr
	}
	return err
}

func firstStep(op store.Operation) string {
	switch op.Kind {
	case store.OpCreate:
		return StepAddress
	case store.OpStop:
		return StepStop
	case store.OpStart:
		return StepStart
	case store.OpPlace:
		return StepWaitAddress
	case store.OpUnplace, store.OpDelete:
		return StepUnrecord
	case store.OpResize:
		return resizeAfter(op, "")
	default:
		return ""
	}
}

func resizeAfter(op store.Operation, step string) string {
	switch {
	case step == "" && op.Target.RootGB != 0:
		return StepGrowRoot
	case step != StepWaitData && op.Target.DataGB != 0:
		return StepGrowData
	case op.Target.Type != "" && op.ResumeState == store.HostRunning:
		return StepStop
	case op.Target.Type != "" || step == "":
		return StepSetType
	default:
		return ""
	}
}
