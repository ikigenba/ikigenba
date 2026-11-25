package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"time"
)

const operationFields = `id, host, account, kind, step, status, generation, target_type, target_root_gb, target_data_gb,
	resume_state, error, attempts, started, step_started, next_try, finished, supersedes, caller_user, caller_email, caller_request`

const operationSelect = "SELECT " + operationFields + " FROM operations"

func scanOperation(r scanner) (op Operation, err error) {
	err = r.Scan(&op.ID, &op.Host, &op.Account, &op.Kind, &op.Step, &op.Status, &op.Generation,
		&op.Target.Type, &op.Target.RootGB, &op.Target.DataGB, &op.ResumeState, &op.Error, &op.Attempts,
		storedTime{&op.Started}, storedTime{&op.StepStarted}, storedTime{&op.NextTry}, storedTime{&op.Finished},
		&op.Supersedes, &op.Caller.UserID, &op.Caller.Email, &op.Caller.RequestID)
	return op, err
}

func getOperation(q query, id string) (Operation, error) {
	return scanOperation(q.row(operationSelect+" WHERE id = ?", id))
}

func saveOperation(q query, op Operation) error {
	return q.exec(`UPDATE operations SET step = ?, status = ?, generation = ?, error = ?, attempts = ?,
		step_started = ?, next_try = ?, finished = ?, caller_user = ?, caller_email = ?, caller_request = ? WHERE id = ?`,
		op.Step, op.Status, op.Generation, op.Error, op.Attempts, op.StepStarted.Unix(), op.NextTry.Unix(),
		op.Finished.Unix(), op.Caller.UserID, op.Caller.Email, op.Caller.RequestID, op.ID)
}

func (s *Store) newOperation(q query, op Operation, stamp time.Time) (Operation, error) {
	op.Status, op.Generation = OpRunning, 1
	op.Started, op.StepStarted, op.NextTry = stamp, stamp, stamp
	for range 8 {
		var buf [8]byte
		if _, err := io.ReadFull(s.cfg.Rand, buf[:]); err != nil {
			return Operation{}, err
		}
		op.ID = "op_" + hex.EncodeToString(buf[:])
		var id string
		err := q.row(`INSERT INTO operations (`+operationFields+`) VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO NOTHING RETURNING id`, op.ID, op.Host, op.Account, op.Kind, op.Step, op.Status,
			op.Generation, op.Target.Type, op.Target.RootGB, op.Target.DataGB, op.ResumeState, op.Error, op.Attempts,
			op.Started.Unix(), op.StepStarted.Unix(), op.NextTry.Unix(), op.Finished.Unix(), op.Supersedes,
			op.Caller.UserID, op.Caller.Email, op.Caller.RequestID).Scan(&id)
		if !errors.Is(err, sql.ErrNoRows) {
			return op, err
		}
	}
	return Operation{}, ErrUnreachable
}

// Operation reads a persisted operation by id.
func (s *recordReader) Operation(ctx context.Context, id string) (Operation, error) {
	return read(ctx, s, id != "", func(q query) (Operation, error) { return getOperation(q, id) })
}

// OperationsOf reads a page of one host's operations.
func (s *recordReader) OperationsOf(ctx context.Context, host, after string, limit int) ([]Operation, error) {
	return read(ctx, s, host != "" && validLimit(limit), func(q query) ([]Operation, error) {
		return collect(q, scanOperation, operationSelect+" WHERE host = ? AND id > ? ORDER BY id LIMIT ?", host, after, limit)
	})
}

// Runnable reads a page of operations due at now.
func (s *recordReader) Runnable(ctx context.Context, now time.Time, after string, limit int) ([]Operation, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]Operation, error) {
		return collect(q, scanOperation, operationSelect+" WHERE status = ? AND id > ? AND next_try <= ? ORDER BY id LIMIT ?",
			OpRunning, after, now.Unix(), limit)
	})
}

// NextTry reads the earliest due time among running operations.
func (s *recordReader) NextTry(ctx context.Context) (time.Time, error) {
	return read(ctx, s, true, func(q query) (next time.Time, err error) {
		err = q.row("SELECT next_try FROM operations WHERE status = ? ORDER BY next_try LIMIT 1", OpRunning).Scan(storedTime{&next})
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return next, err
	})
}

func runningOperation(q query, id string) (Operation, error) {
	op, err := getOperation(q, id)
	if err != nil {
		return Operation{}, err
	}
	if op.Status != OpRunning {
		return Operation{}, ErrConflict
	}
	return op, nil
}

func modifyOperation(ctx context.Context, s *Store, id string, valid bool, fn func(*writing, *Operation) error) error {
	return change(ctx, s, valid && id != "", func(w *writing) error {
		op, err := runningOperation(w.query, id)
		if err != nil {
			return err
		}
		before := op
		if err := fn(w, &op); err != nil {
			return err
		}
		if op == before {
			return nil
		}
		return saveOperation(w.query, op)
	})
}

// RecordStep checkpoints the step about to run.
func (s *Store) RecordStep(ctx context.Context, opID, step string, generation int, now time.Time) error {
	return modifyOperation(ctx, s, opID, step != "" && generation > 0, func(_ *writing, op *Operation) error {
		if op.Step != step || op.Generation != generation {
			op.Step, op.Generation, op.StepStarted = step, generation, cleanTime(now)
		}
		return nil
	})
}

// Defer records a retry time and attempt count.
func (s *Store) Defer(ctx context.Context, opID string, next time.Time, attempts int) error {
	return modifyOperation(ctx, s, opID, true, func(_ *writing, op *Operation) error {
		op.NextTry, op.Attempts = cleanTime(next), attempts
		return nil
	})
}

// RecordChange records an account's Route 53 change.
func (s *Store) RecordChange(ctx context.Context, opID, slug, changeID string) error {
	return modifyOperation(ctx, s, opID, allGiven(slug, changeID), func(w *writing, op *Operation) error {
		a, err := getAccount(w.query, slug)
		if err != nil {
			return err
		}
		if a.Host != op.Host {
			return ErrNotFound
		}
		return w.exec("UPDATE accounts SET change_id = ? WHERE slug = ?", changeID, slug)
	})
}

func assign[T comparable](dest *T, src T) {
	var zero T
	if src != zero {
		*dest = src
	}
}

func applyHost(w *writing, h *Host, u Update) error {
	if u.InstanceID != "" && u.InstanceID != h.InstanceID {
		var taken bool
		if err := w.row("SELECT EXISTS (SELECT 1 FROM hosts WHERE instance_id = ? AND name <> ? AND state <> ?)",
			u.InstanceID, h.Name, HostDeleted).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return ErrConflict
		}
	}
	before := *h
	assign(&h.InstanceID, u.InstanceID)
	assign(&h.RootVolume, u.RootVolume)
	assign(&h.DataVolume, u.DataVolume)
	assign(&h.AllocationID, u.AllocationID)
	assign(&h.AssociationID, u.AssociationID)
	assign(&h.EIP, u.EIP)
	assign(&h.AZ, u.AZ)
	assign(&h.Type, u.Type)
	assign(&h.RootGB, u.RootGB)
	assign(&h.DataGB, u.DataGB)
	if h.InstanceID == before.InstanceID && h.RootVolume == before.RootVolume && h.DataVolume == before.DataVolume &&
		h.AllocationID == before.AllocationID && h.AssociationID == before.AssociationID && h.EIP == before.EIP &&
		h.AZ == before.AZ && h.Type == before.Type && h.RootGB == before.RootGB && h.DataGB == before.DataGB {
		return nil
	}
	if h.DataVolume != before.DataVolume || h.RootGB != before.RootGB || h.DataGB != before.DataGB {
		h.Version++
		w.noteVersionChange(h.Name)
	}
	return w.exec(`UPDATE hosts SET instance_id = ?, root_volume = ?, data_volume = ?, allocation_id = ?, association_id = ?,
		eip = ?, az = ?, instance_type = ?, root_gb = ?, data_gb = ?, version = ? WHERE name = ?`,
		h.InstanceID, h.RootVolume, h.DataVolume, h.AllocationID, h.AssociationID, h.EIP, h.AZ, h.Type, h.RootGB, h.DataGB, h.Version, h.Name)
}

func applyOperation(op *Operation, u Update, stamp time.Time) {
	assign(&op.Step, u.Step)
	assign(&op.Generation, u.Generation)
	op.Attempts, op.StepStarted, op.NextTry = 0, stamp, stamp
}

// Advance applies an engine checkpoint and its learned host fields.
func (s *Store) Advance(ctx context.Context, opID string, u Update) error {
	return modifyOperation(ctx, s, opID, u.Generation >= 0 && u.RootGB >= 0 && u.DataGB >= 0, func(w *writing, op *Operation) error {
		applyOperation(op, u, s.stamp())
		if transitional[op.Kind] == "" {
			return nil
		}
		h, err := getHost(w.query, op.Host)
		if err != nil {
			return err
		}
		return applyHost(w, &h, u)
	})
}

// Fail records an operation failure and the state it causes.
func (s *Store) Fail(ctx context.Context, opID, reason string) error {
	return modifyOperation(ctx, s, opID, true, func(w *writing, op *Operation) error {
		op.Status, op.Error, op.Finished = OpFailed, reason, s.stamp()
		switch op.Kind {
		case OpPlace:
			return w.exec("UPDATE accounts SET state = ?, reason = ? WHERE slug = ?", AccountFailed, reason, op.Account)
		case OpUnplace:
			return nil
		default:
			return w.exec("UPDATE hosts SET state = ?, reason = ? WHERE name = ?", HostFailed, reason, op.Host)
		}
	})
}

// Finish applies the final update and completes an operation.
func (s *Store) Finish(ctx context.Context, opID string, u Update) error {
	_, err := FinishWithRemoved(ctx, s, opID, u)
	return err
}

// FinishWithRemoved completes an operation and returns the latest unplace for
// each account purged by a host deletion, in account slug order.
func FinishWithRemoved(ctx context.Context, s *Store, opID string, u Update) ([]Operation, error) {
	var removed []Operation
	err := modifyOperation(ctx, s, opID, u.Generation >= 0 && u.RootGB >= 0 && u.DataGB >= 0, func(w *writing, op *Operation) error {
		stamp := s.stamp()
		applyOperation(op, u, stamp)
		op.Status, op.Finished = OpDone, stamp
		switch op.Kind {
		case OpPlace:
			return w.exec("UPDATE accounts SET state = ? WHERE slug = ?", AccountActive, op.Account)
		case OpUnplace:
			return purgeAccount(w.query, op.Account)
		}
		h, err := getHost(w.query, op.Host)
		if err != nil {
			return err
		}
		if err := applyHost(w, &h, u); err != nil {
			return err
		}
		switch op.Kind {
		case OpCreate, OpStart:
			h.State = HostRunning
		case OpStop:
			h.State = HostStopped
		case OpResize:
			h.State = op.ResumeState
		case OpDelete:
			h.State = HostDeleted
			removed, err = deleteHostAccounts(w.query, h.Name, stamp)
			if err != nil {
				return err
			}
		}
		if h.State == HostRunning {
			h.RunningSince = stamp
		}
		return saveHostState(w.query, h)
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

func deleteHostAccounts(q query, name string, stamp time.Time) ([]Operation, error) {
	as, err := hostAccounts(q, name)
	if err != nil {
		return nil, err
	}
	var removed []Operation
	for _, a := range as {
		if err := q.exec(`UPDATE operations SET status = ?, finished = ?
			WHERE host = ? AND account = ? AND kind = ? AND status IN (?, ?)`,
			OpDone, stamp.Unix(), name, a.Slug, OpUnplace, OpRunning, OpFailed); err != nil {
			return nil, err
		}
		op, err := getOperation(q, a.Operation)
		if err != nil {
			return nil, err
		}
		removed = append(removed, op)
		if err := purgeAccount(q, a.Slug); err != nil {
			return nil, err
		}
	}
	return removed, nil
}
