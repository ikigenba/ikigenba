package ops

import (
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (s step) volumeTarget() (string, int, string) {
	if s.op.Step == StepGrowRoot || s.op.Step == StepWaitRoot {
		return s.h.RootVolume, s.op.Target.RootGB, StepWaitRoot
	}
	return s.h.DataVolume, s.op.Target.DataGB, StepWaitData
}

func (s step) modification(id string) (cloud.Volume, cloud.Modification, error) {
	v, err := s.cfg.Cloud.DescribeVolume(s.ctx, id)
	if err != nil {
		return v, cloud.Modification{}, err
	}
	m, err := s.cfg.Cloud.VolumeModification(s.ctx, id)
	return v, m, err
}

func (s step) grow() error {
	id, target, next := s.volumeTarget()
	size, ok := checkedSize(target)
	if !ok {
		return s.fail(ReasonBadSize)
	}
	v, m, err := s.modification(id)
	if err != nil {
		return err
	}
	busy := m.State == cloud.ModificationModifying || m.State == cloud.ModificationOptimizing
	otherBusy := busy && m.TargetGB != size
	if s.expired() && s.op.Attempts == 0 && !otherBusy {
		return s.fail(ReasonTimedOut)
	}
	if int(v.SizeGB) >= target || (m.TargetGB == size && m.State != cloud.ModificationFailed) {
		return s.next(next)
	}
	if otherBusy {
		return s.wait()
	}
	if s.op.Attempts == 0 {
		if err := s.cfg.Store.NoteGrow(s.ctx, id, s.now); err != nil {
			return err
		}
	}
	if err := s.cfg.Cloud.ModifyVolume(s.ctx, id, size); err != nil {
		return err
	}
	return s.next(next)
}

func (s step) waitGrow() error {
	id, target, _ := s.volumeTarget()
	v, m, err := s.modification(id)
	if err != nil {
		return err
	}
	if s.expired() && m.State != cloud.ModificationModifying {
		return s.fail(ReasonTimedOut)
	}
	sameTarget := int(m.TargetGB) == target
	switch {
	case int(v.SizeGB) >= target || (sameTarget && (m.State == cloud.ModificationOptimizing || m.State == cloud.ModificationCompleted)):
		u := store.Update{Step: resizeAfter(s.op, s.op.Step)}
		if s.op.Step == StepWaitRoot {
			u.RootGB = target
		} else {
			u.DataGB = target
		}
		if u.Step == "" {
			return s.finish(u)
		}
		return s.advance(u)
	case sameTarget && m.State == cloud.ModificationFailed:
		return s.fail(ReasonModifyFailed)
	default:
		return s.wait()
	}
}

func (s step) setType() error {
	if err := s.cfg.Cloud.SetInstanceType(s.ctx, s.h.InstanceID, s.op.Target.Type); err != nil {
		return err
	}
	u := store.Update{Type: s.op.Target.Type}
	if s.op.ResumeState == store.HostRunning {
		u.Step = StepStart
		return s.advance(u)
	}
	return s.finish(u)
}
