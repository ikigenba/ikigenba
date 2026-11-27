package ops

import (
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (s step) runReplace() error {
	r, err := s.cfg.Store.Replacement(s.ctx, s.op.ID)
	if err != nil {
		return err
	}
	switch s.op.Step {
	case StepRetire:
		return s.retire(r)
	case StepWaitRetired:
		return s.waitRetired(r)
	}

	// The old daemon must be gone before any part of the replacement is made.
	if !r.Retired {
		return s.next(StepRetire)
	}
	switch s.op.Step {
	case StepRelaunch:
		return s.launch()
	case StepWaitRelaunched:
		return s.waitRelaunched(r.Mode)
	case StepNewVolume:
		return s.createVolume()
	case StepWaitNewVolume:
		return s.waitVolume()
	case StepReattach:
		return s.reattach()
	case StepWaitReattached:
		return s.waitReattached(r.Mode)
	case StepTagOld:
		if r.OldVolume != "" {
			err := s.cfg.Cloud.TagResource(s.ctx, r.OldVolume, cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue})
			if err := absentOK(err); err != nil {
				return err
			}
		}
		return s.next(StepReassociate)
	case StepReassociate:
		return s.reassociate(r.Mode)
	case StepRestore:
		return s.waitWholeRestore()
	case StepAdmit:
		if err := s.cfg.Store.Admit(s.ctx, s.op.ID); err != nil {
			return err
		}
		return s.next(StepWaitAdmitted)
	case StepWaitAdmitted:
		return s.waitAdmitted()
	default:
		return s.unknownStep()
	}
}

func (s step) retire(r store.Replacement) error {
	if r.OldInstance != "" {
		cl := s.cfg.Cloud
		i, err := cl.DescribeInstance(s.ctx, r.OldInstance)
		if absentOK(err) != nil {
			return err
		}
		if err == nil && !instanceGone(i) {
			if err := absentOK(cl.TerminateInstance(s.ctx, r.OldInstance)); err != nil {
				return err
			}
		}
	}
	return s.next(StepWaitRetired)
}

func (s step) waitRetired(r store.Replacement) error {
	if r.OldInstance != "" {
		i, err := s.cfg.Cloud.DescribeInstance(s.ctx, r.OldInstance)
		if absentOK(err) != nil {
			return err
		}
		if err == nil && i.State != cloud.InstanceTerminated {
			return s.wait()
		}
	}
	if err := s.cfg.Store.Retire(s.ctx, s.op.ID); err != nil {
		return err
	}
	return s.next(StepRelaunch)
}

func (s step) replacementTarget() (typ string, rootGB, dataGB int) {
	target := s.op.Target
	typ, rootGB, dataGB = s.h.Type, s.h.RootGB, s.h.DataGB
	if target.Type != "" {
		typ = target.Type
	}
	if target.RootGB != 0 {
		rootGB = target.RootGB
	}
	if target.DataGB != 0 {
		dataGB = target.DataGB
	}
	return typ, rootGB, dataGB
}

func (s step) waitRelaunched(mode string) error {
	i, err := s.cfg.Cloud.DescribeInstance(s.ctx, s.h.InstanceID)
	if cloud.ClassOf(err) == cloud.ClassNotFound {
		return s.wait()
	}
	if err != nil {
		return err
	}
	if instanceGone(i) {
		return s.newGeneration()
	}
	if i.State != cloud.InstanceRunning {
		return s.wait()
	}
	next := StepReattach
	if mode == store.ReplaceBackup {
		next = StepNewVolume
	}
	typ, rootGB, _ := s.replacementTarget()
	return s.advance(store.Update{Step: next, AZ: i.AZ, RootVolume: i.RootVolume, Type: typ, RootGB: rootGB})
}

func (s step) reattach() error {
	cl := s.cfg.Cloud
	v, err := cl.DescribeVolume(s.ctx, s.h.DataVolume)
	if err != nil {
		return err
	}
	if v.Attachment.InstanceID == s.h.InstanceID {
		return s.next(StepWaitReattached)
	}
	if v.State != cloud.VolumeAvailable {
		return s.wait()
	}
	if err := cl.AttachVolume(s.ctx, s.h.DataVolume, s.h.InstanceID, DataDevice); err != nil {
		return err
	}
	return s.next(StepWaitReattached)
}

func (s step) waitReattached(mode string) error {
	v, err := s.cfg.Cloud.DescribeVolume(s.ctx, s.h.DataVolume)
	if err != nil {
		return err
	}
	if v.Attachment.InstanceID != s.h.InstanceID || v.Attachment.State != cloud.AttachmentAttached {
		return s.wait()
	}
	if mode == store.ReplaceBackup {
		return s.next(StepTagOld)
	}
	return s.next(StepReassociate)
}

func (s step) reassociate(mode string) error {
	id, err := s.cfg.Cloud.AssociateAddress(s.ctx, s.h.AllocationID, s.h.InstanceID)
	if err != nil {
		return err
	}
	update := store.Update{AssociationID: id, Step: StepAdmit}
	if mode == store.ReplaceBackup {
		update.Step = StepRestore
		update.HostState = store.HostRestoring
	}
	return s.advance(update)
}

func (s step) checkReplacementHost() (string, error) {
	i, err := s.cfg.Cloud.DescribeInstance(s.ctx, s.h.InstanceID)
	if cloud.ClassOf(err) == cloud.ClassNotFound {
		return ReasonMissing, nil
	}
	if err != nil {
		return "", err
	}
	if i.State != cloud.InstanceRunning {
		return ReasonTerminated, nil
	}
	if s.now.Sub(s.op.StepStarted) > StepTimeout && s.now.Sub(s.h.Reported) > StepTimeout {
		return ReasonTimedOut, nil
	}
	return "", nil
}

func (s step) waitAdmitted() error {
	reason, err := s.checkReplacementHost()
	if err != nil {
		return err
	}
	if reason != "" {
		return s.fail(reason)
	}
	if s.h.Applied < s.h.Version {
		return s.wait()
	}
	typ, rootGB, dataGB := s.replacementTarget()
	return s.finish(store.Update{Type: typ, RootGB: rootGB, DataGB: dataGB})
}
