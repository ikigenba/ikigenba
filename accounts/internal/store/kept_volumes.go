package store

import (
	"context"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

const keptVolumeFields = `volume, host, size_gb, deleted, state, reason, attempts, next_try, caller_user, caller_email, caller_request`
const keptVolumeSelect = "SELECT " + keptVolumeFields +
	", coalesce((SELECT cause FROM kept_causes WHERE kept_causes.volume = kept_volumes.volume), 'deleted') FROM kept_volumes"
const keptVolumeListed = "state <> 'deleted'"

func scanKeptVolume(r scanner) (v KeptVolume, err error) {
	err = r.Scan(&v.Volume, &v.Host, &v.SizeGB, storedTime{&v.Deleted}, &v.State, &v.Reason, &v.Attempts,
		storedTime{&v.NextTry}, &v.Caller.UserID, &v.Caller.Email, &v.Caller.RequestID, &v.Cause)
	return v, err
}

func getKeptVolume(q query, volume string) (KeptVolume, error) {
	return scanKeptVolume(q.row(keptVolumeSelect+" WHERE volume = ? AND "+keptVolumeListed, volume))
}

// KeptVolume reads a volume that is kept or whose deletion is in progress.
func (s *recordReader) KeptVolume(ctx context.Context, volume string) (KeptVolume, error) {
	return read(ctx, s, volume != "", func(q query) (KeptVolume, error) {
		return getKeptVolume(q, volume)
	})
}

func (s *recordReader) keptVolumePage(ctx context.Context, limit int, reverse bool, stmt string, args ...any) ([]KeptVolume, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]KeptVolume, error) {
		out, err := collect(q, scanKeptVolume, keptVolumeSelect+" WHERE "+keptVolumeListed+stmt, append(args, limit)...)
		if reverse {
			slices.Reverse(out)
		}
		return out, err
	})
}

// ListKeptVolumes reads a descending page beginning after the supplied cursor.
func (s *recordReader) ListKeptVolumes(ctx context.Context, afterDeleted time.Time, afterVolume string, limit int) ([]KeptVolume, error) {
	const order = " ORDER BY deleted DESC, volume DESC LIMIT ?"
	if afterDeleted.IsZero() {
		return s.keptVolumePage(ctx, limit, false, order)
	}
	if afterDeleted.Nanosecond() != 0 {
		return s.keptVolumePage(ctx, limit, false, " AND deleted <= ?"+order, afterDeleted.Unix())
	}
	return s.keptVolumePage(ctx, limit, false, " AND (deleted, volume) < (?, ?)"+order, afterDeleted.Unix(), afterVolume)
}

// ListKeptVolumesBefore reads the descending page immediately before the cursor.
func (s *recordReader) ListKeptVolumesBefore(ctx context.Context, beforeDeleted time.Time, beforeVolume string, limit int) ([]KeptVolume, error) {
	const order = " ORDER BY deleted, volume LIMIT ?"
	if beforeDeleted.Nanosecond() != 0 {
		return s.keptVolumePage(ctx, limit, true, " AND deleted > ?"+order, beforeDeleted.Unix())
	}
	return s.keptVolumePage(ctx, limit, true, " AND (deleted, volume) > (?, ?)"+order, beforeDeleted.Unix(), beforeVolume)
}

// LastKeptVolumes reads the final page in descending list order.
func (s *recordReader) LastKeptVolumes(ctx context.Context, limit int) ([]KeptVolume, error) {
	return s.keptVolumePage(ctx, limit, true, " ORDER BY deleted, volume LIMIT ?")
}

// DeleteKeptVolume schedules deletion and persists the caller for deferred work.
func (s *Store) DeleteKeptVolume(ctx context.Context, volume string, c identity.Caller) (KeptVolume, error) {
	return write(ctx, s, volume != "" && validCaller(c), func(w *writing) (KeptVolume, error) {
		v, err := getKeptVolume(w.query, volume)
		if err != nil {
			return KeptVolume{}, err
		}
		if v.State == VolumeDeleting {
			return KeptVolume{}, ErrVolumeDeleting
		}
		v.State, v.Reason, v.Attempts, v.NextTry, v.Caller = VolumeDeleting, "", 0, s.stamp(), c
		return v, w.exec(`UPDATE kept_volumes SET state = ?, reason = '', attempts = 0, next_try = ?,
			caller_user = ?, caller_email = ?, caller_request = ? WHERE volume = ?`,
			v.State, v.NextTry.Unix(), c.UserID, c.Email, c.RequestID, volume)
	})
}

// DueVolumeDeletes reads the due deletions after a volume id.
func (s *recordReader) DueVolumeDeletes(ctx context.Context, now time.Time, after string, limit int) ([]KeptVolume, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]KeptVolume, error) {
		return collect(q, scanKeptVolume, keptVolumeSelect+
			" WHERE state = 'deleting' AND next_try <= ? AND volume > ? ORDER BY volume LIMIT ?", now.Unix(), after, limit)
	})
}

func updateVolumeDelete(ctx context.Context, s *Store, volume, statement string, args ...any) error {
	return change(ctx, s, volume != "", func(w *writing) error {
		var state string
		if err := w.row("SELECT state FROM kept_volumes WHERE volume = ?", volume).Scan(&state); err != nil {
			return err
		}
		if state != VolumeDeleting {
			return ErrConflict
		}
		return w.exec(statement, append(args, volume)...)
	})
}

// DeferVolumeDelete records the next try and attempt count.
func (s *Store) DeferVolumeDelete(ctx context.Context, volume string, next time.Time, attempts int) error {
	return updateVolumeDelete(ctx, s, volume, "UPDATE kept_volumes SET next_try = ?, attempts = ? WHERE volume = ?", next.Unix(), attempts)
}

// FinishVolumeDelete records the deletion as completed.
func (s *Store) FinishVolumeDelete(ctx context.Context, volume string) error {
	return updateVolumeDelete(ctx, s, volume, "UPDATE kept_volumes SET state = ?, reason = '' WHERE volume = ?", VolumeDeleted)
}

// FailVolumeDelete returns the volume to the kept state with the reason.
func (s *Store) FailVolumeDelete(ctx context.Context, volume, reason string) error {
	return updateVolumeDelete(ctx, s, volume, "UPDATE kept_volumes SET state = ?, reason = ? WHERE volume = ?", VolumeKept, reason)
}

const insertKeptVolume = "INSERT INTO kept_volumes (" + keptVolumeFields + ")"

func recordDeletedHostVolume(q query, h Host, stamp time.Time) error {
	if h.DataVolume == "" {
		return nil
	}
	state := VolumeKept
	if h.OnDelete == OnDeleteDelete {
		state = VolumeDeleted
	}
	return q.exec(insertKeptVolume+` VALUES (?, ?, ?, ?, ?, '', 0, ?, '', '', '') ON CONFLICT (volume) DO NOTHING`,
		h.DataVolume, h.Name, h.DataGB, stamp.Unix(), state, time.Time{}.Unix())
}

func recordReplacedVolume(q query, h Host, stamp time.Time) error {
	res, err := q.ExecContext(q.Context, insertKeptVolume+` VALUES (?, ?, ?, ?, 'kept', '', 0, ?, '', '', '')
		ON CONFLICT (volume) DO NOTHING`, h.DataVolume, h.Name, h.DataGB, stamp.Unix(), time.Time{}.Unix())
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	return q.exec("INSERT INTO kept_causes (volume, cause) VALUES (?, ?)", h.DataVolume, CauseReplaced)
}

const adoptKeptVolume = insertKeptVolume + `
	SELECT h.data_volume, h.name, h.data_gb, op.finished, ?, '', 0, ?, '', '', ''
	FROM hosts AS h JOIN operations AS op ON op.id = h.operation
	WHERE h.state = 'deleted' AND h.data_volume <> '' AND NOT EXISTS (
		SELECT 1 FROM kept_volumes AS kv WHERE kv.volume = h.data_volume
	)`

func adoptVacatedVolume(q query, name string) error {
	return q.exec(adoptKeptVolume+" AND h.name = ?", VolumeKept, time.Time{}.Unix(), name)
}

// AdoptKept records a bounded batch of earlier deletes with unrecorded volumes.
func (s *Store) AdoptKept(ctx context.Context, limit int) (int, error) {
	return write(ctx, s, validLimit(limit), func(w *writing) (int, error) {
		result, err := w.ExecContext(ctx, adoptKeptVolume+`
			AND NOT EXISTS (SELECT 1 FROM hosts AS prior WHERE prior.data_volume = h.data_volume
				AND prior.state = 'deleted' AND prior.seq < h.seq)
			ORDER BY h.seq LIMIT ?`, VolumeKept, time.Time{}.Unix(), limit)
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		// The query inserts at most limit rows, which is already an int.
		return int(n), err
	})
}
