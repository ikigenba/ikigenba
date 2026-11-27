package ops

import (
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (s step) creates(each func(string) error) error {
	for after := ""; ; {
		page, err := s.cfg.Store.OperationsOf(s.ctx, s.h.Name, after, store.BatchSize)
		if err != nil {
			return err
		}
		for _, op := range page {
			if op.Kind == store.OpCreate {
				if err := each(op.ID); err != nil {
					return err
				}
			}
			after = op.ID
		}
		if len(page) < store.BatchSize {
			return nil
		}
	}
}

func absentOK(err error) error {
	if err != nil && cloud.ClassOf(err) == cloud.ClassNotFound {
		return nil
	}
	return err
}

func (s step) terminate() error {
	cl := s.cfg.Cloud
	terminate := func(id string) error { return absentOK(cl.TerminateInstance(s.ctx, id)) }
	if s.h.InstanceID != "" {
		if err := terminate(s.h.InstanceID); err != nil {
			return err
		}
	}
	err := s.creates(func(id string) error {
		return eachResource(s.ctx, lookup(cloud.KindHost, id), cl.FindInstances, func(i cloud.Instance) error {
			if i.ID == s.h.InstanceID {
				return nil
			}
			return terminate(i.ID)
		})
	})
	if err != nil {
		return err
	}
	return s.next(StepWaitTerminated)
}

func (s step) waitTerminated() error {
	cl := s.cfg.Cloud
	pending := false
	if s.h.InstanceID != "" {
		i, err := cl.DescribeInstance(s.ctx, s.h.InstanceID)
		if err != nil {
			if absentOK(err) != nil {
				return err
			}
		} else {
			pending = i.State != cloud.InstanceTerminated
		}
	}
	err := s.creates(func(id string) error {
		return eachResource(s.ctx, lookup(cloud.KindHost, id), cl.FindInstances, func(i cloud.Instance) error {
			pending = pending || i.State != cloud.InstanceTerminated
			return nil
		})
	})
	if err != nil {
		return err
	}
	if pending {
		return s.wait()
	}
	return s.next(StepRelease)
}

func (s step) release() error {
	cl := s.cfg.Cloud
	release := func(a cloud.Address) error {
		if a.AssociationID != "" {
			if err := absentOK(cl.DisassociateAddress(s.ctx, a.AssociationID)); err != nil {
				return err
			}
		}
		return absentOK(cl.ReleaseAddress(s.ctx, a.AllocationID))
	}
	if s.h.AllocationID != "" {
		a, err := cl.DescribeAddress(s.ctx, s.h.AllocationID)
		if err != nil {
			if absentOK(err) != nil {
				return err
			}
		} else if err := release(a); err != nil {
			return err
		}
	}
	err := s.creates(func(id string) error {
		return eachResource(s.ctx, lookup(cloud.KindHost, id), cl.FindAddresses, func(a cloud.Address) error {
			if a.AllocationID == s.h.AllocationID {
				return nil
			}
			return release(a)
		})
	})
	if err != nil {
		return err
	}
	if s.deletesData() {
		return s.next(StepDeleteData)
	}
	return s.next(StepOrphan)
}

func (s step) deletesData() bool {
	return s.op.Kind == store.OpDelete && s.h.OnDelete == store.OnDeleteDelete
}

func (s step) orphan() error {
	cl := s.cfg.Cloud
	orphan := func(id string) error {
		return absentOK(cl.TagResource(s.ctx, id, cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue}))
	}
	if s.h.DataVolume != "" {
		if err := orphan(s.h.DataVolume); err != nil {
			return err
		}
	}
	err := s.creates(func(id string) error {
		return eachResource(s.ctx, lookup(cloud.KindHostData, id), cl.FindVolumes, func(v cloud.Volume) error {
			return s.useOtherDataVolume(v, func(id, _ string) error { return orphan(id) })
		})
	})
	if err != nil {
		return err
	}
	return s.finish(store.Update{})
}

func (s step) useOtherDataVolume(v cloud.Volume, use func(id, state string) error) error {
	if v.ID == s.h.DataVolume {
		return nil
	}
	_, err := s.cfg.Store.KeptVolume(s.ctx, v.ID)
	if errors.Is(err, store.ErrNotFound) {
		return use(v.ID, v.State)
	}
	return err
}

func (s step) findDataVolumes(use func(id, state string) error) error {
	cl := s.cfg.Cloud
	if s.h.DataVolume != "" {
		v, err := cl.DescribeVolume(s.ctx, s.h.DataVolume)
		if absentOK(err) != nil {
			return err
		}
		if err == nil {
			if err := use(s.h.DataVolume, v.State); err != nil {
				return err
			}
		}
	}
	return s.creates(func(id string) error {
		return eachResource(s.ctx, lookup(cloud.KindHostData, id), cl.FindVolumes, func(v cloud.Volume) error {
			return s.useOtherDataVolume(v, use)
		})
	})
}

func (s step) deleteData() error {
	if err := s.findDataVolumes(func(id, state string) error {
		if state == cloud.VolumeDeleting || state == cloud.VolumeDeleted {
			return nil
		}
		return absentOK(s.cfg.Cloud.DeleteVolume(s.ctx, id))
	}); err != nil {
		return err
	}
	return s.next(StepWaitDataDeleted)
}

func (s step) waitDataDeleted() error {
	pending := false
	if err := s.findDataVolumes(func(_, state string) error {
		pending = pending || state != cloud.VolumeDeleted
		return nil
	}); err != nil {
		return err
	}
	if pending {
		return s.wait()
	}
	return s.next(StepDeleteBackups)
}
