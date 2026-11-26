package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

const hostFields = `name, seq, state, reason, instance_type, root_gb, data_gb, instance_id, root_volume, data_volume,
	allocation_id, association_id, eip, az, version, applied, attempted, report_error, alarm, running_since,
	reported, operation, created, created_by`

const hostSelect = `SELECT ` + hostFields +
	`, (SELECT count(*) FROM accounts WHERE host = hosts.name),
	coalesce((SELECT host_records.record_change FROM host_records WHERE host_records.host_name = hosts.name), ''),
	coalesce((SELECT host_records.record_reason FROM host_records WHERE host_records.host_name = hosts.name), ''),
	CASE WHEN hosts.state IN ('deleting', 'failed') THEN coalesce((
		SELECT CASE WHEN delete_choices.keep_data = 0 THEN 'delete' ELSE 'keep' END
		FROM operations LEFT JOIN delete_choices ON delete_choices.operation_id = operations.id
		WHERE operations.id = hosts.operation AND operations.kind = 'delete'
	), '') ELSE '' END
	FROM hosts`

func scanHost(q query, r scanner) (h Host, err error) {
	if err := r.Scan(&h.Name, &h.Seq, &h.State, &h.Reason, &h.Type, &h.RootGB, &h.DataGB,
		&h.InstanceID, &h.RootVolume, &h.DataVolume, &h.AllocationID, &h.AssociationID, &h.EIP, &h.AZ,
		&h.Version, &h.Applied, &h.Attempted, &h.ReportError, &h.Alarm, storedTime{&h.RunningSince},
		storedTime{&h.Reported}, &h.Operation, storedTime{&h.Created}, &h.CreatedBy, &h.Accounts, &h.Change, &h.RecordReason,
		&h.OnDelete); err != nil {
		return Host{}, err
	}
	h.Allowlist, err = allowlist(q, h.Name)
	return h, err
}

func getHost(q query, name string) (Host, error) {
	return scanHost(q, q.row(hostSelect+" WHERE name = ?", name))
}

func liveHost(q query, name string) (Host, error) {
	return scanHost(q, q.row(hostSelect+" WHERE name = ? AND state <> ?", name, HostDeleted))
}

func collectHosts(q query, stmt string, args ...any) ([]Host, error) {
	return collect(q, func(r scanner) (Host, error) {
		return scanHost(q, r)
	}, hostSelect+stmt, args...)
}

func insertHost(q query, h Host) error {
	return q.exec(`INSERT INTO hosts (`+hostFields+`) VALUES
		(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.Seq, h.State, h.Reason, h.Type, h.RootGB, h.DataGB, h.InstanceID, h.RootVolume, h.DataVolume,
		h.AllocationID, h.AssociationID, h.EIP, h.AZ, h.Version, h.Applied, h.Attempted, h.ReportError, h.Alarm,
		h.RunningSince.Unix(), h.Reported.Unix(), h.Operation, h.Created.Unix(), h.CreatedBy)
}

type hostOp struct {
	host Host
	op   Operation
}

// CreateHost allocates a new host and records its provisioning operation.
func (s *Store) CreateHost(ctx context.Context, spec HostSpec, c identity.Caller) (Host, Operation, error) {
	out, err := write(ctx, s, spec.Type != "" && validCaller(c) && spec.RootGB > 0 && spec.DataGB > 0, func(w *writing) (hostOp, error) {
		stamp := s.stamp()
		seq, err := allocate(w.query, "host", 1)
		if err != nil {
			return hostOp{}, err
		}
		name, err := nextHostName(w.query)
		if err != nil {
			return hostOp{}, err
		}
		h := Host{Name: name, Seq: seq, State: HostProvisioning, Type: spec.Type,
			RootGB: spec.RootGB, DataGB: spec.DataGB, Version: 1, Created: stamp, CreatedBy: c.UserID, Allowlist: []string{}}
		op, err := s.newOperation(w.query, Operation{Host: h.Name, Kind: OpCreate, Caller: c}, stamp)
		if err != nil {
			return hostOp{}, err
		}
		h.Operation = op.ID
		return hostOp{h, op}, insertHost(w.query, h)
	})
	return out.host, out.op, err
}

func nextHostName(q query) (string, error) {
	for number := int64(1); ; number++ {
		name := fmt.Sprintf("host%02d", number)
		var seq int64
		var state string
		err := q.row("SELECT seq, state FROM hosts WHERE name = ?", name).Scan(&seq, &state)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return name, nil
		case err != nil:
			return "", err
		case state == HostDeleted:
			return name, vacateHost(q, name, seq)
		}
	}
}

func vacateHost(q query, name string, seq int64) error {
	key := fmt.Sprintf("%s#%d", name, seq)
	// Adopt before the old host's name changes. The report has an immediate
	// foreign key, while the moved operations' foreign key is deferred.
	if err := adoptVacatedVolume(q, name); err != nil {
		return err
	}
	for _, stmt := range []string{"DELETE FROM reports WHERE host = ?",
		"DELETE FROM host_allowlist WHERE host = ?", "DELETE FROM host_unbans WHERE host = ?"} {
		if err := q.exec(stmt, name); err != nil {
			return err
		}
	}
	if err := q.exec("UPDATE hosts SET name = ? WHERE name = ?", key, name); err != nil {
		return err
	}
	if err := q.exec("UPDATE operations SET host = ? WHERE host = ?", key, name); err != nil {
		return err
	}
	if err := q.exec("UPDATE host_records SET host_name = ? WHERE host_name = ?", key, name); err != nil {
		return err
	}
	return settleDeleted(q, key)
}

// Host reads a live host by name.
func (s *recordReader) Host(ctx context.Context, name string) (Host, error) {
	return read(ctx, s, true, func(q query) (Host, error) { return liveHost(q, name) })
}

// HostByInstance reads the live host that owns an instance.
func (s *recordReader) HostByInstance(ctx context.Context, id string) (Host, error) {
	return read(ctx, s, true, func(q query) (Host, error) {
		return scanHost(q, q.row(hostSelect+" WHERE instance_id = ? AND instance_id <> '' AND state <> ?", id, HostDeleted))
	})
}

func (s *recordReader) hostPage(ctx context.Context, limit int, reverse bool, stmt string, args ...any) ([]Host, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]Host, error) {
		out, err := collectHosts(q, stmt, append(args, limit)...)
		if reverse {
			slices.Reverse(out)
		}
		return out, err
	})
}

// ListHosts reads a page following a sequence cursor.
func (s *recordReader) ListHosts(ctx context.Context, after int64, limit int) ([]Host, error) {
	return s.hostPage(ctx, limit, false, " WHERE state <> ? AND seq > ? ORDER BY seq LIMIT ?", HostDeleted, after)
}

// ListHostsBefore reads the page immediately preceding a sequence cursor.
func (s *recordReader) ListHostsBefore(ctx context.Context, before int64, limit int) ([]Host, error) {
	return s.hostPage(ctx, limit, true, " WHERE state <> ? AND seq < ? ORDER BY seq DESC LIMIT ?", HostDeleted, before)
}

// LastHosts reads the final page in ascending sequence order.
func (s *recordReader) LastHosts(ctx context.Context, limit int) ([]Host, error) {
	return s.hostPage(ctx, limit, true, " WHERE state <> ? ORDER BY seq DESC LIMIT ?", HostDeleted)
}

func hostGuard(q query, h Host, kind string) error {
	var busy bool
	err := q.row(`SELECT EXISTS (SELECT 1 FROM operations WHERE host = ? AND status = ?
		AND kind IN (?, ?, ?, ?, ?))`, h.Name, OpRunning, OpCreate, OpStop, OpStart, OpResize, OpDelete).Scan(&busy)
	if err != nil {
		return err
	}
	switch {
	case busy:
		return ErrBusy
	case h.State == HostFailed && kind != OpDelete:
		return ErrHostFailed
	case kind == OpStop && h.State != HostRunning:
		return ErrNotRunning
	case kind == OpStart && h.State != HostStopped:
		return ErrNotStopped
	case kind == OpDelete:
		var blocked bool
		if err := q.row("SELECT EXISTS (SELECT 1 FROM accounts WHERE host = ? AND state <> ?)", h.Name, AccountRemoving).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return ErrHasAccounts
		}
	}
	return nil
}

func resizeTarget(q query, h Host, target Target, cutoff int64) (Target, error) {
	if target.RootGB != 0 && target.RootGB < h.RootGB || target.DataGB != 0 && target.DataGB < h.DataGB {
		return Target{}, ErrShrinks
	}
	if target.Type == h.Type {
		target.Type = ""
	}
	if target.RootGB == h.RootGB {
		target.RootGB = 0
	}
	if target.DataGB == h.DataGB {
		target.DataGB = 0
	}
	if target == (Target{}) {
		return Target{}, ErrNothingToChange
	}
	for _, volume := range []struct {
		size int
		id   string
	}{{target.RootGB, h.RootVolume}, {target.DataGB, h.DataVolume}} {
		if volume.size == 0 {
			continue
		}
		var count int
		if err := q.row(`SELECT count(*) FROM (SELECT 1 FROM volume_grows WHERE volume = ? AND noted > ? LIMIT ?)`,
			volume.id, cutoff, MaxGrows).Scan(&count); err != nil {
			return Target{}, err
		}
		if count >= MaxGrows {
			return Target{}, ErrGrowLimit
		}
	}
	return target, nil
}

var transitional = map[string]string{OpCreate: HostProvisioning, OpStop: HostStopping, OpStart: HostStarting,
	OpResize: HostResizing, OpDelete: HostDeleting}

// StartHostOp checks the host's guards and records a new host operation.
func (s *Store) StartHostOp(ctx context.Context, name, kind string, target Target, c identity.Caller) (Host, Operation, error) {
	valid := kind == OpStop || kind == OpStart || kind == OpResize && target.RootGB >= 0 && target.DataGB >= 0
	return s.startHostOp(ctx, name, kind, target, c, valid, true)
}

// DeleteHost records a host deletion and its choice about the data volume.
func (s *Store) DeleteHost(ctx context.Context, name string, keepData bool, c identity.Caller) (Host, Operation, error) {
	return s.startHostOp(ctx, name, OpDelete, Target{}, c, true, keepData)
}

func (s *Store) startHostOp(ctx context.Context, name, kind string, target Target, c identity.Caller, valid, keepData bool) (Host, Operation, error) {
	valid = valid && name != "" && validCaller(c)
	out, err := write(ctx, s, valid, func(w *writing) (hostOp, error) {
		h, err := getHost(w.query, name)
		if err != nil {
			return hostOp{}, err
		}
		if h.State == HostDeleted {
			return hostOp{}, ErrHostGone
		}
		if err := hostGuard(w.query, h, kind); err != nil {
			return hostOp{}, err
		}
		stamp := s.stamp()
		computed := Target{}
		if kind == OpResize {
			computed, err = resizeTarget(w.query, h, target, stamp.Add(-GrowWindow).Unix())
			if err != nil {
				return hostOp{}, err
			}
		}
		op, err := s.newOperation(w.query, Operation{Host: h.Name, Kind: kind, Target: computed, ResumeState: h.State, Caller: c}, stamp)
		if err != nil {
			return hostOp{}, err
		}
		h.State, h.Reason, h.Operation = transitional[kind], "", op.ID
		if kind == OpDelete {
			if err := w.exec("INSERT INTO delete_choices (operation_id, keep_data) VALUES (?, ?)", op.ID, keepData); err != nil {
				return hostOp{}, err
			}
			h.OnDelete = OnDeleteKeep
			if !keepData {
				h.OnDelete = OnDeleteDelete
			}
		}
		return hostOp{h, op}, saveHostState(w.query, h)
	})
	return out.host, out.op, err
}

// RetryHost resumes the failed operation of a live host.
func (s *Store) RetryHost(ctx context.Context, name string, c identity.Caller) (Host, Operation, error) {
	out, err := write(ctx, s, name != "" && validCaller(c), func(w *writing) (hostOp, error) {
		h, err := getHost(w.query, name)
		if err != nil {
			return hostOp{}, err
		}
		switch h.State {
		case HostDeleted:
			return hostOp{}, ErrHostGone
		case HostFailed:
		default:
			return hostOp{}, ErrNotFailed
		}
		op, err := getOperation(w.query, h.Operation)
		if err != nil {
			return hostOp{}, err
		}
		stamp := s.stamp()
		op.Status, op.Attempts, op.Error = OpRunning, 0, ""
		op.Finished = time.Time{}
		op.StepStarted, op.NextTry, op.Caller = stamp, stamp, c
		if err := saveOperation(w.query, op); err != nil {
			return hostOp{}, err
		}
		h.State, h.Reason = transitional[op.Kind], ""
		return hostOp{h, op}, saveHostState(w.query, h)
	})
	return out.host, out.op, err
}

func saveHostState(q query, h Host) error {
	return q.exec("UPDATE hosts SET state = ?, reason = ?, operation = ?, running_since = ? WHERE name = ?",
		h.State, h.Reason, h.Operation, h.RunningSince.Unix(), h.Name)
}
