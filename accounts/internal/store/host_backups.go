package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func setClose(q query, h Host, closed time.Time) error {
	return q.exec(`INSERT INTO host_closes (host_seq, host, closed) VALUES (?, ?, ?)
		ON CONFLICT (host_seq) DO UPDATE SET closed = excluded.closed`, h.Seq, h.Name, closed.Unix())
}

func lockedOver(q query, h Host) (bool, error) {
	var locked bool
	err := q.row("SELECT EXISTS (SELECT 1 FROM host_closes WHERE host_seq = ?)", h.Seq).Scan(&locked)
	return locked, err
}

func newHostBackupOp(s *Store, w *writing, h Host, op Operation, stamp time.Time) (hostOp, error) {
	op.Host, op.ResumeState = h.Name, h.State
	op, err := s.newOperation(w.query, op, stamp)
	if err != nil {
		return hostOp{}, err
	}
	h.State, h.Reason, h.Operation, h.OnDelete, h.Replace = transitional[op.Kind], "", op.ID, "", ""
	return hostOp{h, op}, saveHostState(w.query, h)
}

func startBackupHost(q query, name string, allowClose bool) (Host, error) {
	h, err := getHost(q, name)
	if err != nil {
		return Host{}, err
	}
	if h.State == HostDeleted {
		return Host{}, ErrHostGone
	}
	busy, err := hostBusy(q, h, allowClose)
	if err != nil {
		return Host{}, err
	}
	if busy {
		return Host{}, ErrBusy
	}
	return h, nil
}

func addBackupSteps(q query, h Host, opID string, at time.Time) error {
	return q.exec(`INSERT INTO backup_steps (operation_id, account, state, at, code, reason)
		SELECT ?, slug, 'pending', ?, '', '' FROM accounts WHERE host = ? AND state <> 'removing'
		UNION ALL SELECT ?, '', 'pending', ?, '', ''`, opID, at.Unix(), h.Name, opID, at.Unix())
}

func resetFailedSteps(q query, opID string, at time.Time) error {
	// Pending retry rows keep a private marker so an older failed report
	// cannot re-fail them. Their public code remains empty.
	return q.exec(`UPDATE backup_steps SET state = 'pending', at = ?, code = 'retry', reason = ''
		WHERE operation_id = ? AND state = 'failed'`, at.Unix(), opID)
}

// CloseHost requests final backups and overlays locked users on desired state.
func (s *Store) CloseHost(ctx context.Context, name string, c identity.Caller) (Host, Operation, error) {
	out, err := write(ctx, s, name != "" && validCaller(c), func(w *writing) (hostOp, error) {
		h, err := startBackupHost(w.query, name, false)
		if err != nil {
			return hostOp{}, err
		}
		switch {
		case h.State == HostFailed:
			return hostOp{}, ErrHostFailed
		case !h.Closed.IsZero():
			return hostOp{host: h}, nil
		case h.State != HostRunning:
			return hostOp{}, ErrNotRunning
		}
		stamp := s.stamp()
		out, err := newHostBackupOp(s, w, h, Operation{Kind: OpClose, Caller: c}, stamp)
		if err != nil {
			return hostOp{}, err
		}
		if err := setClose(w.query, h, time.Time{}); err != nil {
			return hostOp{}, err
		}
		// Random operation IDs do not order closes created in the same second.
		// Mark the latest internally, keeping the public Supersedes field empty.
		if err := w.exec("UPDATE backup_operations SET supersedes = '' WHERE host_seq = ? AND kind = 'close' AND supersedes = 'latest'", h.Seq); err != nil {
			return hostOp{}, err
		}
		if err := w.exec("UPDATE backup_operations SET supersedes = 'latest' WHERE id = ?", out.op.ID); err != nil {
			return hostOp{}, err
		}
		if err := addBackupSteps(w.query, h, out.op.ID, stamp); err != nil {
			return hostOp{}, err
		}
		out.host.Version, err = w.bump(name)
		return out, err
	})
	return out.host, out.op, err
}

// OpenHost clears the close overlay and cancels a close still running.
func (s *Store) OpenHost(ctx context.Context, name string, c identity.Caller) (Host, Operation, error) {
	out, err := write(ctx, s, name != "" && validCaller(c), func(w *writing) (hostOp, error) {
		h, err := startBackupHost(w.query, name, true)
		if err != nil {
			return hostOp{}, err
		}
		locked, err := lockedOver(w.query, h)
		if err != nil {
			return hostOp{}, err
		}
		if !locked {
			return hostOp{}, ErrNotClosed
		}
		current, err := getOperation(w.query, h.Operation)
		if err != nil {
			return hostOp{}, err
		}
		closeCurrent := current.Kind == OpClose && (current.Status == OpRunning || current.Status == OpFailed)
		if !closeCurrent && h.State != HostRunning {
			return hostOp{}, ErrNotRunning
		}
		stamp := s.stamp()
		var supersedes string
		if closeCurrent && current.Status == OpRunning {
			current.Status, current.Finished = OpCancelled, stamp
			if err := saveOperation(w.query, current); err != nil {
				return hostOp{}, err
			}
			supersedes = current.ID
		}
		if err := w.exec("DELETE FROM host_closes WHERE host_seq = ?", h.Seq); err != nil {
			return hostOp{}, err
		}
		h.Closed = time.Time{}
		out, err := newHostBackupOp(s, w, h, Operation{Kind: OpOpen, Caller: c, Supersedes: supersedes}, stamp)
		if err != nil {
			return hostOp{}, err
		}
		out.host.Version, err = w.bump(name)
		return out, err
	})
	return out.host, out.op, err
}

type hostReplacement struct {
	hostOp
	cancelled []string
}

// ReplaceHost records a replacement and fixes its set of database restore paths.
func (s *Store) ReplaceHost(ctx context.Context, name, mode string, target Target, c identity.Caller) (Host, Operation, []string, error) {
	ok := name != "" && validCaller(c) && target.RootGB >= 0 && target.DataGB >= 0 &&
		(mode == ReplaceBackup || mode == ReplaceVolume && target.DataGB == 0)
	out, err := write(ctx, s, ok, func(w *writing) (hostReplacement, error) {
		h, err := startBackupHost(w.query, name, false)
		if err != nil {
			return hostReplacement{}, err
		}
		if h.State == HostFailed {
			op, err := getOperation(w.query, h.Operation)
			if err != nil {
				return hostReplacement{}, err
			}
			if op.Kind == OpDelete {
				return hostReplacement{}, ErrHostFailed
			}
		}
		withheld, restoring, err := replacementState(w.query, h)
		if err != nil {
			return hostReplacement{}, err
		}
		if mode == ReplaceVolume && withheld {
			return hostReplacement{}, ErrWithheld
		}
		if mode == ReplaceVolume && h.DataVolume == "" {
			return hostReplacement{}, ErrNoDataVolume
		}
		if h.AllocationID == "" {
			return hostReplacement{}, ErrNoAddress
		}
		if h.State != HostRunning && h.State != HostStopped && h.State != HostFailed {
			return hostReplacement{}, ErrNotRunning
		}
		computed := Target{Type: h.Type, RootGB: h.RootGB, DataGB: h.DataGB}
		assign(&computed.Type, target.Type)
		assign(&computed.RootGB, target.RootGB)
		assign(&computed.DataGB, target.DataGB)
		stamp := s.stamp()
		cancelled, err := cancelRestores(w.query, h.Seq, "", stamp)
		if err != nil {
			return hostReplacement{}, err
		}
		if restoring {
			h.Version, err = w.bump(h.Name)
			if err != nil {
				return hostReplacement{}, err
			}
		}
		started, err := newHostBackupOp(s, w, h, Operation{Kind: OpReplace, Target: computed, Caller: c}, stamp)
		if err != nil {
			return hostReplacement{}, err
		}
		started.host.Replace = mode
		opID := started.op.ID
		if err := w.exec("INSERT INTO replacements (operation_id, mode, old_instance, old_volume, retired, admitted) VALUES (?, ?, ?, ?, 0, 0)",
			opID, mode, h.InstanceID, h.DataVolume); err != nil {
			return hostReplacement{}, err
		}
		if mode == ReplaceBackup {
			if err := addBackupSteps(w.query, h, opID, stamp); err != nil {
				return hostReplacement{}, err
			}
			if err := copyRestorePaths(w.query, h, opID); err != nil {
				return hostReplacement{}, err
			}
		}
		return hostReplacement{started, cancelled}, nil
	})
	return out.host, out.op, out.cancelled, err
}

func getReplacement(q query, opID string) (r Replacement, err error) {
	err = q.row("SELECT operation_id, mode, old_instance, old_volume, retired, admitted FROM replacements WHERE operation_id = ?",
		opID).Scan(&r.Operation, &r.Mode, &r.OldInstance, &r.OldVolume, &r.Retired, &r.Admitted)
	return r, err
}

func retiredReplacementKey(seq int64) string {
	return "retired_replace:" + strconv.FormatInt(seq, 10)
}

// replacementState reads the last retirement for this host incarnation, which
// survives operations that replace the host's current operation pointer.
func replacementState(q query, h Host) (withheld, restoring bool, err error) {
	err = q.row(`SELECT r.retired AND NOT r.admitted,
			r.retired AND NOT r.admitted AND r.operation_id = ? AND r.mode = ? AND b.status IN ('running', 'failed')
		FROM replacements r JOIN backup_operations b ON b.id = r.operation_id
		WHERE r.operation_id = (SELECT printf('op_%016x', value) FROM counters WHERE name = ?)
			AND b.host_seq = ? AND b.kind = 'replace'`, h.Operation, ReplaceBackup, retiredReplacementKey(h.Seq), h.Seq).
		Scan(&withheld, &restoring)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return withheld, restoring, err
}

func rememberRetirement(q query, opID string) error {
	// Operation IDs wrap eight random bytes. Store those same bits in the
	// integer value of the host's private counter to preserve the schema.
	id, err := strconv.ParseUint(strings.TrimPrefix(opID, "op_"), 16, 64)
	if err != nil {
		return err
	}
	var seq int64
	if err := q.row("SELECT host_seq FROM backup_operations WHERE id = ?", opID).Scan(&seq); err != nil {
		return err
	}
	value := int64(id & math.MaxInt64)
	if id > math.MaxInt64 {
		value += math.MinInt64
	}
	return q.exec("INSERT INTO counters (name, value) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET value = excluded.value",
		retiredReplacementKey(seq), value)
}

func (s *recordReader) Replacement(ctx context.Context, opID string) (Replacement, error) {
	return read(ctx, s, opID != "", func(q query) (Replacement, error) { return getReplacement(q, opID) })
}

func (s *Store) markReplacement(ctx context.Context, opID, column string) error {
	return change(ctx, s, opID != "", func(w *writing) error {
		op, err := getOperation(w.query, opID)
		if err != nil {
			return err
		}
		if op.Kind != OpReplace {
			return ErrNotFound
		}
		if op.Status != OpRunning {
			return ErrConflict
		}
		res, err := w.ExecContext(ctx, "UPDATE replacements SET "+column+" = 1 WHERE operation_id = ? AND "+column+" = 0", opID)
		if err != nil {
			return err
		}
		count, err := res.RowsAffected()
		if err != nil || count == 0 {
			return err
		}
		if column == "retired" {
			if err := rememberRetirement(w.query, opID); err != nil {
				return err
			}
		}
		_, err = w.bump(op.Host)
		return err
	})
}

// Retire withholds desired accounts from the replacement daemon.
func (s *Store) Retire(ctx context.Context, opID string) error {
	return s.markReplacement(ctx, opID, "retired")
}

// Admit restores desired accounts to the replacement daemon.
func (s *Store) Admit(ctx context.Context, opID string) error {
	return s.markReplacement(ctx, opID, "admitted")
}

func scanBackupStep(r scanner) (b BackupStep, err error) {
	err = r.Scan(&b.Account, &b.State, storedTime{&b.At}, &b.Code, &b.Reason)
	return b, err
}

func (s *recordReader) BackupSteps(ctx context.Context, opID string) ([]BackupStep, error) {
	return read(ctx, s, opID != "", func(q query) ([]BackupStep, error) {
		op, err := getOperation(q, opID)
		if err != nil {
			return nil, err
		}
		return collect(q, scanBackupStep, `SELECT account, state, at, CASE WHEN state = 'pending' THEN '' ELSE code END, reason
			FROM backup_steps WHERE operation_id = ? AND (account = '' OR account IN
				(SELECT slug FROM accounts WHERE host = ? AND state <> 'removing')) ORDER BY account`, opID, op.Host)
	})
}

func (s *recordReader) BackupSettings(ctx context.Context) (BackupSettings, error) {
	return read(ctx, s, true, func(q query) (BackupSettings, error) {
		b := DefaultBackupSettings()
		err := q.row(`SELECT every_minutes, hourly, daily, monthly, window_days, files_stale_minutes, database_stale_minutes, check_days
			FROM backup_settings WHERE scope = ''`).Scan(&b.EveryMinutes, &b.Hourly, &b.Daily, &b.Monthly, &b.WindowDays,
			&b.FilesStaleMinutes, &b.DatabaseStaleMinutes, &b.CheckDays)
		return b, ignoreNoRows(err)
	})
}

func settleBackupStep(q query, opID, account string, step protocol.Step, reason string) error {
	if step.ID != opID || step.State != protocol.StepDone && step.State != protocol.StepFailed {
		return nil
	}
	return q.exec(`INSERT INTO backup_steps (operation_id, account, state, at, code, reason) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (operation_id, account) DO UPDATE SET state = excluded.state, at = excluded.at, code = excluded.code,
		reason = excluded.reason WHERE backup_steps.state = 'pending' AND
			(excluded.state = 'done' OR backup_steps.code <> 'retry' OR backup_steps.at <= excluded.at)`,
		opID, account, step.State, step.At.Unix(), step.Code, boundedBackupError(reason))
}
