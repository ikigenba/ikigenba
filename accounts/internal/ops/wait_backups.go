package ops

import (
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (s step) runBackupWait() error {
	switch {
	case s.op.Kind == store.OpClose && s.op.Step == StepWaitFinal:
		done, reason, err := s.backupSteps(ReasonFinalFailed)
		switch {
		case err != nil:
			return err
		case reason != "":
			return s.fail(reason)
		case done:
			return s.finish(store.Update{})
		default:
			return s.wait()
		}
	case s.op.Kind == store.OpOpen && s.op.Step == StepWaitOpened:
		if s.h.Applied >= s.h.Version {
			return s.finish(store.Update{})
		}
		return s.wait()
	case s.op.Kind == store.OpRestore && s.op.Step == StepWaitRestored:
		r, err := s.cfg.Store.Restore(s.ctx, s.op.ID)
		if err != nil {
			return err
		}
		switch r.State {
		case protocol.StepDone:
			return s.finish(store.Update{})
		case protocol.StepFailed:
			return s.fail(ReasonRestoreFailed)
		default:
			return s.wait()
		}
	}
	return s.unknownStep()
}

func (s step) waitWholeRestore() error {
	if s.h.State != store.HostRestoring {
		if err := s.advance(store.Update{HostState: store.HostRestoring}); err != nil {
			return err
		}
		s.h.State = store.HostRestoring
	}
	reason, err := s.checkReplacementHost()
	if err != nil {
		return s.cloudError(err)
	}
	if reason != "" {
		return s.fail(reason)
	}
	done, reason, err := s.backupSteps(ReasonRestoreFailed)
	switch {
	case err != nil:
		return err
	case reason != "":
		return s.fail(reason)
	case done:
		return s.next(StepAdmit)
	default:
		return s.wait()
	}
}

func (s step) backupSteps(failure string) (done bool, reason string, err error) {
	st := s.cfg.Store
	rows, err := st.BackupSteps(s.ctx, s.op.ID)
	if err != nil {
		return false, "", err
	}
	accounts, err := st.AccountsOn(s.ctx, s.h.Name)
	if err != nil {
		return false, "", err
	}
	next := 0
	done = true
	member := func(account string) {
		for next < len(rows) && rows[next].Account < account {
			next++
		}
		if next == len(rows) || rows[next].Account != account {
			done = false
			return
		}
		row := rows[next]
		next++
		done = done && row.State == protocol.StepDone
		if row.State != protocol.StepFailed {
			return
		}
		if failure == ReasonRestoreFailed && row.Code == protocol.CodeNotEmpty {
			reason = ReasonNotEmpty
		} else if reason == "" {
			reason = failure
		}
	}
	member("")
	for _, a := range accounts {
		switch a.State {
		case store.AccountPending, store.AccountActive, store.AccountFailed:
			member(a.Slug)
		}
	}
	return done, reason, nil
}
