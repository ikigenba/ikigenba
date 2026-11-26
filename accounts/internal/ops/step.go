package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

type step struct {
	*Engine
	ctx context.Context
	now time.Time
	op  store.Operation
	h   store.Host
	a   store.Account
}

func (e *Engine) take(ctx context.Context, now time.Time, op store.Operation) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if op.Step == "" {
		op.Step, op.StepStarted = firstStep(op), now
	}
	st := e.cfg.Store
	if err := st.RecordStep(ctx, op.ID, op.Step, op.Generation, now); err != nil {
		return stepStoreError(err)
	}
	h, err := st.Host(ctx, op.Host)
	if err != nil {
		return stepStoreError(err)
	}
	s := step{Engine: e, ctx: ctx, now: now, op: op, h: h}
	if op.Account != "" {
		s.a, err = st.Account(ctx, op.Account)
		if err != nil {
			return stepStoreError(err)
		}
	}
	err = s.run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil || errors.Is(err, store.ErrUnreachable) {
		return err
	}
	if ended(err) {
		return nil
	}
	return stepStoreError(s.cloudError(err))
}

func ended(err error) bool {
	return errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound)
}

func stepStoreError(err error) error {
	if ended(err) {
		return nil
	}
	return err
}

func (s step) run() error {
	if s.expired() {
		switch s.op.Step {
		case StepWaitAddress, StepWaitApplied, StepGrowRoot, StepGrowData, StepWaitRoot, StepWaitData:
			// EBS steps determine their exceptions using the volume's modification.
		default:
			return s.fail(ReasonTimedOut)
		}
	}
	// A rollback may leave a delete at a step for the other data choice.
	switch s.op.Step {
	case StepOrphan:
		if s.deletesData() {
			return s.next(StepDeleteData)
		}
	case StepDeleteData, StepWaitDataDeleted:
		if s.op.Kind == store.OpDelete && !s.deletesData() {
			return s.next(StepOrphan)
		}
	}
	switch s.op.Step {
	case StepAddress:
		return s.allocate()
	case StepHostRecord:
		return s.record("", StepLaunch, StepWaitHostRecord)
	case StepWaitHostRecord:
		return s.waitRecord(s.h.Change, StepLaunch)
	case StepLaunch:
		return s.launch()
	case StepWaitRunning:
		return s.waitRunning()
	case StepVolume:
		return s.createVolume()
	case StepWaitVolume:
		return s.waitVolume()
	case StepAttach:
		return s.attach()
	case StepWaitAttached:
		return s.waitAttached()
	case StepAssociate:
		return s.associate()
	case StepStop:
		return s.stop()
	case StepWaitStopped:
		return s.waitStopped()
	case StepStart:
		return s.start()
	case StepGrowRoot, StepGrowData:
		return s.grow()
	case StepWaitRoot, StepWaitData:
		return s.waitGrow()
	case StepSetType:
		return s.setType()
	case StepWaitAddress:
		if s.h.EIP != "" {
			return s.next(StepRecord)
		}
		return s.wait()
	case StepRecord:
		return s.record(s.a.Slug, StepWaitApplied, StepWaitRecord)
	case StepWaitRecord:
		return s.waitRecord(s.a.Change, StepWaitApplied)
	case StepWaitApplied:
		return s.waitApplied()
	case StepUnrecord:
		return s.unrecord()
	case StepWaitUnrecord:
		return s.waitUnrecord()
	case StepTerminate:
		return s.terminate()
	case StepWaitTerminated:
		return s.waitTerminated()
	case StepRelease:
		return s.release()
	case StepOrphan:
		return s.orphan()
	case StepDeleteData:
		if s.deletesData() {
			return s.deleteData()
		}
	case StepWaitDataDeleted:
		if s.deletesData() {
			return s.waitDataDeleted()
		}
	}
	return fmt.Errorf("%w: unknown operation step %q for %q", store.ErrUnreachable, s.op.Step, s.op.Kind)
}

func (s step) expired() bool { return s.now.Sub(s.op.StepStarted) > StepTimeout }

func (s step) token() string {
	return fmt.Sprintf("%s-%s-%d", s.op.ID, s.op.Step, s.op.Generation)
}

func (s step) tags(kind string) cloud.Tags {
	return cloud.Tags{cloud.TagKind: kind, cloud.TagHost: s.h.Name, cloud.TagOperation: s.op.ID}
}

func lookup(kind, operation string) cloud.Tags {
	return cloud.Tags{cloud.TagKind: kind, cloud.TagOperation: operation}
}

func (s step) advance(u store.Update) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.cfg.Store.Advance(s.ctx, s.op.ID, u)
}

func (s step) next(name string) error { return s.advance(store.Update{Step: name}) }

func (s step) wait() error { return s.deferFor(PollInterval, s.op.Attempts) }

func (s step) deferFor(delay time.Duration, attempts int) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.cfg.Store.Defer(s.ctx, s.op.ID, s.now.Add(delay), attempts)
}

func (s step) cloudError(err error) error {
	switch cloud.ClassOf(err) {
	case cloud.ClassNotFound:
		return s.fail(ReasonMissing)
	case cloud.ClassRefused, cloud.ClassMismatch:
		return s.fail(err.Error())
	default:
		delay := retryDelay(s.op.Attempts)
		var ce *cloud.Error
		if errors.As(err, &ce) && ce.Code == "VolumeModificationRateExceeded" {
			delay = MaxBackoff
		}
		return s.deferFor(delay, s.op.Attempts+1)
	}
}

func retryDelay(attempts int) time.Duration {
	delay := PollInterval
	for attempts > 0 && delay < MaxBackoff {
		attempts--
		delay = min(delay*2, MaxBackoff)
	}
	return delay
}

func (s step) fail(reason string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := s.cfg.Store.Fail(s.ctx, s.op.ID, reason); err != nil {
		return err
	}
	s.tell(store.OpFailed, reason)
	return nil
}

func (s step) finish(u store.Update) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	var removed []store.Operation
	var err error
	if s.op.Kind == store.OpDelete {
		removed, err = store.FinishWithRemoved(s.ctx, s.cfg.Store, s.op.ID, u)
	} else {
		err = s.cfg.Store.Finish(s.ctx, s.op.ID, u)
	}
	if err != nil {
		return err
	}
	s.tell(store.OpDone, "")
	s.tellRemoved(removed)
	return nil
}

func (s step) tell(status, reason string) {
	w := s.cfg.Telemetry
	if w == nil {
		return
	}
	op := s.op
	ctx := identity.NewContext(s.ctx, op.Caller)
	switch op.Kind {
	case store.OpPlace:
		state := store.AccountActive
		if status == store.OpFailed {
			state = store.AccountFailed
		}
		trail.AccountChanged(ctx, w, s.a.Slug, s.h.Name, store.AccountPending, state)
	case store.OpUnplace:
		if status == store.OpDone {
			trail.AccountChanged(ctx, w, s.a.Slug, s.h.Name, store.AccountRemoving, trail.AccountRemoved)
		}
	default:
		state := endState(op, status)
		if state != s.h.State {
			trail.HostChanged(ctx, w, s.h.Name, s.h.State, state, op.ID)
		}
	}
	var data string
	if op.Kind == store.OpDelete {
		data = s.h.OnDelete
	}
	trail.OperationFinished(ctx, w, op.ID, op.Kind, op.Host, op.Account, status, reason, data)
}

func endState(op store.Operation, status string) string {
	if status == store.OpFailed {
		return store.HostFailed
	}
	switch op.Kind {
	case store.OpStop:
		return store.HostStopped
	case store.OpResize:
		return op.ResumeState
	case store.OpDelete:
		return store.HostDeleted
	default:
		return store.HostRunning
	}
}

func (s step) tellRemoved(removed []store.Operation) {
	w := s.cfg.Telemetry
	if w == nil {
		return
	}
	ctx := identity.NewContext(s.ctx, s.op.Caller)
	for _, op := range removed {
		trail.AccountChanged(ctx, w, op.Account, op.Host, store.AccountRemoving, trail.AccountRemoved)
		unplace := identity.NewContext(s.ctx, op.Caller)
		trail.OperationFinished(unplace, w, op.ID, op.Kind, op.Host, op.Account, op.Status, "", "")
	}
}
