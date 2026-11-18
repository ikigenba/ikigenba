package ops

import (
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (s step) name(a store.Account) string { return a.Slug + "." + s.cfg.Hosts.Domain }

func pointsAt(record cloud.Record, ip string) bool {
	return ip != "" && record.Type == "A" && len(record.Values) == 1 && record.Values[0] == ip
}

func (s step) record() error {
	cl := s.cfg.Cloud
	name, zone := s.name(s.a), s.cfg.Hosts.ZoneID
	records, err := cl.FindRecords(s.ctx, zone, name)
	if err != nil {
		return err
	}
	switch {
	case len(records) == 1 && pointsAt(records[0], s.h.EIP):
		return s.next(StepWaitApplied)
	case len(records) != 0:
		return s.fail(ReasonRecordConflict)
	}
	change, err := cl.ChangeRecord(s.ctx, zone, cloud.RecordChange{
		Action: cloud.RecordCreate, Name: name, IP: s.h.EIP, TTL: RecordTTL,
	})
	if err != nil {
		switch cloud.ClassOf(err) {
		case cloud.ClassRefused, cloud.ClassMismatch:
			return s.fail(ReasonRecordConflict)
		default:
			return err
		}
	}
	if err := s.saveChange(s.a.Slug, change); err != nil {
		return err
	}
	return s.next(StepWaitRecord)
}

func (s step) saveChange(slug, change string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.cfg.Store.RecordChange(s.ctx, s.op.ID, slug, change)
}

func (s step) inSync(a store.Account) (bool, error) {
	done, err := s.cfg.Cloud.ChangeInsync(s.ctx, a.Change)
	if err != nil && cloud.ClassOf(err) == cloud.ClassNotFound {
		return true, nil
	}
	return done, err
}

func (s step) waitRecord() error {
	done, err := s.inSync(s.a)
	if err != nil {
		return err
	}
	if done {
		return s.next(StepWaitApplied)
	}
	return s.wait()
}

func (s step) waitApplied() error {
	ready := s.h.Applied >= s.a.Version
	if s.op.Kind == store.OpUnplace {
		ready = (s.h.Applied < s.a.Version && s.h.Attempted < s.a.Version) || s.h.Applied >= s.a.RemovedVersion
	}
	if ready {
		return s.finish(store.Update{})
	}
	return s.wait()
}

func (s step) removing() ([]store.Account, error) {
	if s.op.Kind == store.OpUnplace {
		return []store.Account{s.a}, nil
	}
	return s.cfg.Store.AccountsOn(s.ctx, s.h.Name)
}

func (s step) unrecord() error {
	accounts, err := s.removing()
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if err := s.deleteRecord(a); err != nil {
			return err
		}
	}
	return s.next(StepWaitUnrecord)
}

func (s step) deleteRecord(a store.Account) error {
	name, zone := s.name(a), s.cfg.Hosts.ZoneID
	records, err := s.cfg.Cloud.FindRecords(s.ctx, zone, name)
	if err != nil {
		return err
	}
	for _, r := range records {
		if !pointsAt(r, s.h.EIP) {
			continue
		}
		change, err := s.cfg.Cloud.ChangeRecord(s.ctx, zone, cloud.RecordChange{
			Action: cloud.RecordDelete, Name: name, IP: s.h.EIP, TTL: r.TTL,
		})
		if err != nil {
			return err
		}
		if err := s.saveChange(a.Slug, change); err != nil {
			return err
		}
	}
	return nil
}

func (s step) waitUnrecord() error {
	if s.now.Sub(s.op.StepStarted) < RecordSettle {
		return s.wait()
	}
	accounts, err := s.removing()
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if a.Change == "" {
			continue
		}
		done, err := s.inSync(a)
		if err != nil {
			return err
		}
		if !done {
			return s.wait()
		}
	}
	if s.op.Kind == store.OpDelete {
		return s.next(StepTerminate)
	}
	return s.next(StepWaitApplied)
}
