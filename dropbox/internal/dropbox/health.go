package dropbox

import (
	"context"
	"database/sql"
)

// Health returns the dropbox_health payload (PLAN.md §3): the caller's identity
// plus the mirror/disk telemetry —
//
//   - MirrorBytes:    SUM(size) over the files index — the indexed LOGICAL size,
//     not a `du` walk of the mirror directory.
//   - DiskFreeBytes / DiskTotalBytes: a statfs on the mirror filesystem.
//   - FailedFiles:    count of index rows with a non-null `error` — the poison
//     entries the sync engine advanced past (PLAN.md §2/§6).
//
// The telemetry is best-effort: a DB read or a statfs failure leaves the
// corresponding fields zero rather than failing the whole probe (the identity
// answer — the end-to-end auth proof — must always come back). dropbox is a
// single-box, single-owner service: identity is reported verbatim, never used to
// scope domain data (PLAN.md §6 — no owner column).
func (s *Service) Health(ownerEmail, clientID string) (HealthInfo, error) {
	info := HealthInfo{OwnerEmail: ownerEmail, ClientID: clientID}

	if s.DB != nil && s.Store != nil {
		if tx, err := s.DB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true}); err == nil {
			if total, e := s.Store.TotalSize(tx); e == nil {
				info.MirrorBytes = total
			}
			if n, e := s.Store.FailedFiles(tx); e == nil {
				info.FailedFiles = n
			}
			_ = tx.Rollback()
		}
	}

	if s.Mirror != nil {
		if free, total, err := s.Mirror.StatFS(); err == nil {
			info.DiskFreeBytes = free
			info.DiskTotalBytes = total
		}
	}

	return info, nil
}
