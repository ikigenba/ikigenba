package ops

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (e *Engine) recordName(label string) string { return label + "." + e.cfg.Hosts.Domain }

func pointsAt(record cloud.Record, ip string) bool {
	return ip != "" && record.Type == "A" && len(record.Values) == 1 && record.Values[0] == ip
}

func (e *Engine) findRecord(ctx context.Context, name, ip string) (present, matches bool, err error) {
	records, err := e.cfg.Cloud.FindRecords(ctx, e.cfg.Hosts.ZoneID, name)
	return len(records) != 0, len(records) == 1 && pointsAt(records[0], ip), err
}

func (e *Engine) createRecord(ctx context.Context, name, ip string) (string, error) {
	return e.cfg.Cloud.ChangeRecord(ctx, e.cfg.Hosts.ZoneID, cloud.RecordChange{
		Action: cloud.RecordCreate, Name: name, IP: ip, TTL: RecordTTL,
	})
}

func recordRefused(err error) bool {
	class := cloud.ClassOf(err)
	return class == cloud.ClassRefused || class == cloud.ClassMismatch
}

func (s step) record(slug, next, waitStep string) error {
	label := slug
	if label == "" {
		label = s.h.Name
	}
	name := s.recordName(label)
	present, matches, err := s.findRecord(s.ctx, name, s.h.EIP)
	if err != nil {
		return err
	}
	switch {
	case matches:
		return s.next(next)
	case present:
		return s.fail(ReasonRecordConflict)
	}
	change, err := s.createRecord(s.ctx, name, s.h.EIP)
	if err != nil {
		if recordRefused(err) {
			return s.fail(ReasonRecordConflict)
		}
		return err
	}
	if err := s.saveChange(slug, change); err != nil {
		return err
	}
	return s.next(waitStep)
}

func (s step) saveChange(slug, change string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if slug == "" {
		return s.cfg.Store.RecordHostChange(s.ctx, s.op.ID, s.h.Name, change)
	}
	return s.cfg.Store.RecordChange(s.ctx, s.op.ID, slug, change)
}

func (s step) inSync(change string) (bool, error) {
	done, err := s.cfg.Cloud.ChangeInsync(s.ctx, change)
	if err != nil && cloud.ClassOf(err) == cloud.ClassNotFound {
		return true, nil
	}
	return done, err
}

func (s step) waitRecord(change, next string) error {
	done, err := s.inSync(change)
	if err != nil {
		return err
	}
	if done {
		return s.next(next)
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
		if err := s.deleteRecord(a.Slug, a.Slug); err != nil {
			return err
		}
	}
	if s.op.Kind == store.OpDelete && s.h.EIP != "" {
		if err := s.deleteRecord(s.h.Name, ""); err != nil {
			return err
		}
	}
	return s.next(StepWaitUnrecord)
}

func (s step) deleteRecord(label, slug string) error {
	name, zone := s.recordName(label), s.cfg.Hosts.ZoneID
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
		if err := s.saveChange(slug, change); err != nil {
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
		done, err := s.inSync(a.Change)
		if err != nil {
			return err
		}
		if !done {
			return s.wait()
		}
	}
	if s.op.Kind == store.OpDelete {
		if s.h.Change != "" {
			done, err := s.inSync(s.h.Change)
			if err != nil {
				return err
			}
			if !done {
				return s.wait()
			}
		}
		return s.next(StepTerminate)
	}
	return s.next(StepWaitApplied)
}

func (e *Engine) checkRecords(ctx context.Context) error {
	for after := int64(0); ; {
		hosts, err := e.cfg.Store.ListHosts(ctx, after, store.BatchSize)
		if err != nil || len(hosts) == 0 {
			return err
		}
		for _, host := range hosts {
			if host.EIP == "" {
				continue
			}
			switch host.State {
			case store.HostRunning, store.HostStopping, store.HostStopped, store.HostStarting, store.HostResizing:
				if err := e.checkHostRecord(ctx, host); err != nil {
					return err
				}
			}
		}
		if len(hosts) < store.BatchSize {
			return nil
		}
		after = hosts[len(hosts)-1].Seq
	}
}

func (e *Engine) checkHostRecord(ctx context.Context, host store.Host) error {
	name := e.recordName(host.Name)
	present, matches, err := e.findRecord(ctx, name, host.EIP)
	if err != nil {
		return err
	}
	var change, reason string
	switch {
	case matches:
		if host.RecordReason == "" {
			return nil
		}
	case present:
		reason = ReasonRecordConflict
	default:
		change, err = e.createRecord(ctx, name, host.EIP)
		if err != nil {
			if !recordRefused(err) {
				return err
			}
			change, reason = "", ReasonRecordConflict
		}
	}
	err = e.cfg.Store.NoteHostRecord(ctx, host.Name, change, reason)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}
