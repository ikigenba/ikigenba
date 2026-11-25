package ops

import (
	"math"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func checkedSize(n int) (int32, bool) {
	if n < 1 || n > math.MaxInt32 {
		return 0, false
	}
	return int32(n), true
}

func (s step) allocate() error {
	cl := s.cfg.Cloud
	var next cloud.Address
	if err := eachResource(s.ctx, lookup(cloud.KindHost, s.op.ID), cl.FindAddresses, func(a cloud.Address) error {
		if next.AllocationID == "" || a.AllocationID < next.AllocationID {
			next = a
		}
		return nil
	}); err != nil {
		return err
	}
	if next.AllocationID == "" {
		var err error
		next, err = cl.AllocateAddress(s.ctx, s.tags(cloud.KindHost))
		if err != nil {
			return err
		}
	}
	return s.advance(store.Update{Step: StepHostRecord, AllocationID: next.AllocationID, EIP: next.IP})
}

func (s step) launch() error {
	if s.cfg.AccountsURL == nil {
		return s.fail(ReasonNoAccountsURL)
	}
	url, err := s.cfg.AccountsURL()
	if err != nil || url == "" {
		return s.fail(ReasonNoAccountsURL)
	}
	size, ok := checkedSize(s.h.RootGB)
	if !ok {
		return s.fail(ReasonBadSize)
	}
	cl := s.cfg.Cloud
	var instance cloud.Instance
	err = eachResource(s.ctx, lookup(cloud.KindHost, s.op.ID), cl.FindInstances, func(i cloud.Instance) error {
		switch i.State {
		case cloud.InstancePending, cloud.InstanceRunning, cloud.InstanceStopping, cloud.InstanceStopped:
			if instance.ID == "" || i.ID < instance.ID {
				instance = i
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if instance.ID == "" {
		instance, err = cl.RunInstance(s.ctx, cloud.Launch{
			Template: s.cfg.Hosts.LaunchTemplate, InstanceProfile: s.cfg.Hosts.InstanceProfile,
			Type: s.h.Type, RootGB: size, ClientToken: s.token(),
			Tags: s.tags(cloud.KindHost), InstanceTags: cloud.Tags{cloud.TagAccountsURL: url},
		})
		if err != nil {
			return err
		}
	}
	if instanceGone(instance) {
		return s.newGeneration()
	}
	return s.advance(store.Update{Step: StepWaitRunning, InstanceID: instance.ID})
}

func instanceGone(i cloud.Instance) bool {
	return i.State == cloud.InstanceShuttingDown || i.State == cloud.InstanceTerminated
}

func (s step) newGeneration() error {
	if s.op.Generation >= MaxGenerations {
		return s.fail(ReasonTerminated)
	}
	return s.advance(store.Update{Step: StepLaunch, Generation: s.op.Generation + 1})
}

func (s step) waitRunning() error {
	i, err := s.cfg.Cloud.DescribeInstance(s.ctx, s.h.InstanceID)
	if err != nil {
		if s.op.Kind == store.OpCreate && cloud.ClassOf(err) == cloud.ClassNotFound {
			return s.wait()
		}
		return err
	}
	switch {
	case i.State == cloud.InstanceRunning:
		next := StepAssociate
		if s.op.Kind == store.OpCreate {
			next = StepVolume
		}
		return s.advance(store.Update{Step: next, AZ: i.AZ, RootVolume: i.RootVolume})
	case instanceGone(i):
		if s.op.Kind == store.OpCreate {
			return s.newGeneration()
		}
		return s.fail(ReasonTerminated)
	default:
		return s.wait()
	}
}

func (s step) createVolume() error {
	size, ok := checkedSize(s.h.DataGB)
	if !ok {
		return s.fail(ReasonBadSize)
	}
	cl := s.cfg.Cloud
	var volume cloud.Volume
	if err := eachResource(s.ctx, lookup(cloud.KindHostData, s.op.ID), cl.FindVolumes, func(v cloud.Volume) error {
		if volume.ID == "" || v.ID < volume.ID {
			volume = v
		}
		return nil
	}); err != nil {
		return err
	}
	if volume.ID == "" {
		var err error
		volume, err = cl.CreateVolume(s.ctx, cloud.VolumeSpec{
			AZ: s.h.AZ, SizeGB: size, ClientToken: s.token(), Tags: s.tags(cloud.KindHostData),
		})
		if err != nil {
			return err
		}
	}
	return s.advance(store.Update{Step: StepWaitVolume, DataVolume: volume.ID})
}

func (s step) waitVolume() error {
	v, err := s.cfg.Cloud.DescribeVolume(s.ctx, s.h.DataVolume)
	if err != nil {
		if cloud.ClassOf(err) == cloud.ClassNotFound {
			return s.wait()
		}
		return err
	}
	switch v.State {
	case cloud.VolumeAvailable:
		return s.next(StepAttach)
	case cloud.VolumeInUse:
		if v.Attachment.InstanceID == s.h.InstanceID {
			return s.next(StepAssociate)
		}
	case cloud.VolumeError:
		return s.fail(ReasonVolumeFailed)
	}
	return s.wait()
}

func (s step) attach() error {
	cl := s.cfg.Cloud
	v, err := cl.DescribeVolume(s.ctx, s.h.DataVolume)
	if err != nil {
		return err
	}
	if v.Attachment.InstanceID != s.h.InstanceID {
		if err := cl.AttachVolume(s.ctx, s.h.DataVolume, s.h.InstanceID, DataDevice); err != nil {
			return err
		}
	}
	return s.next(StepWaitAttached)
}

func (s step) waitAttached() error {
	v, err := s.cfg.Cloud.DescribeVolume(s.ctx, s.h.DataVolume)
	if err != nil {
		return err
	}
	if v.Attachment.InstanceID == s.h.InstanceID && v.Attachment.State == cloud.AttachmentAttached {
		return s.next(StepAssociate)
	}
	return s.wait()
}

func (s step) associate() error {
	id, err := s.cfg.Cloud.AssociateAddress(s.ctx, s.h.AllocationID, s.h.InstanceID)
	if err != nil {
		return err
	}
	return s.finish(store.Update{AssociationID: id})
}

func (s step) stop() error {
	if err := s.cfg.Cloud.StopInstance(s.ctx, s.h.InstanceID); err != nil {
		return err
	}
	return s.next(StepWaitStopped)
}

func (s step) waitStopped() error {
	i, err := s.cfg.Cloud.DescribeInstance(s.ctx, s.h.InstanceID)
	if err != nil {
		return err
	}
	switch {
	case i.State == cloud.InstanceStopped:
		if s.op.Kind == store.OpResize {
			return s.next(StepSetType)
		}
		return s.finish(store.Update{})
	case instanceGone(i):
		return s.fail(ReasonTerminated)
	default:
		return s.wait()
	}
}

func (s step) start() error {
	if err := s.cfg.Cloud.StartInstance(s.ctx, s.h.InstanceID); err != nil {
		return err
	}
	return s.next(StepWaitRunning)
}
