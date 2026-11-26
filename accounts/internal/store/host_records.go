package store

import "context"

// DeletedHosts reads a page of deleted hosts whose DNS records have not been settled.
func (s *Store) DeletedHosts(ctx context.Context, after int64, limit int) ([]Host, error) {
	return s.hostPage(ctx, limit, false, ` WHERE state = ? AND seq > ? AND NOT EXISTS (
		SELECT 1 FROM host_records WHERE host_records.host_name = hosts.name AND host_records.settled = 1
	) ORDER BY seq LIMIT ?`, HostDeleted, after)
}

// SettleDeleted records that the scan checked a deleted host's DNS record.
func (s *Store) SettleDeleted(ctx context.Context, host string) error {
	return change(ctx, s, host != "", func(w *writing) error {
		var state string
		if err := w.row("SELECT state FROM hosts WHERE name = ?", host).Scan(&state); err != nil {
			return err
		}
		if state != HostDeleted {
			return ErrConflict
		}
		return settleDeleted(w.query, host)
	})
}

func settleDeleted(q query, host string) error {
	return q.exec(`INSERT INTO host_records (host_name, record_change, record_reason, settled)
		VALUES (?, '', '', 1)
		ON CONFLICT (host_name) DO UPDATE SET settled = excluded.settled`, host)
}

// RecordHostChange checkpoints a host's Route 53 change for a running operation.
func (s *Store) RecordHostChange(ctx context.Context, opID, host, changeID string) error {
	return change(ctx, s, allGiven(host, changeID), func(w *writing) error {
		op, err := runningOperation(w.query, opID)
		if err != nil {
			return err
		}
		if op.Host != host {
			return ErrNotFound
		}
		var name string
		return w.row(`INSERT INTO host_records (host_name, record_change, record_reason)
			SELECT name, ?, '' FROM hosts WHERE name = ?
			ON CONFLICT (host_name) DO UPDATE SET record_change = excluded.record_change
			RETURNING host_name`, changeID, host).Scan(&name)
	})
}

// NoteHostRecord records the scan's latest reason and, if supplied, change.
func (s *Store) NoteHostRecord(ctx context.Context, host, changeID, reason string) error {
	return change(ctx, s, host != "", func(w *writing) error {
		var name string
		return w.row(`INSERT INTO host_records (host_name, record_change, record_reason)
			SELECT name, ?, ? FROM hosts WHERE name = ? AND state <> ?
			ON CONFLICT (host_name) DO UPDATE SET
				record_change = coalesce(nullif(excluded.record_change, ''), record_change),
				record_reason = excluded.record_reason
			RETURNING host_name`, changeID, reason, host, HostDeleted).Scan(&name)
	})
}
