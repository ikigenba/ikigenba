package store

import (
	"context"
	"time"
)

// NoteStray records the first discovery of an unexplained resource.
func (s *Store) NoteStray(ctx context.Context, resource, kind, hostTag string, now time.Time) (bool, error) {
	return write(ctx, s, allGiven(resource, kind), func(w *writing) (bool, error) {
		r, err := w.ExecContext(ctx, "INSERT INTO strays (resource, kind, host_tag, noted) VALUES (?, ?, ?, ?) ON CONFLICT (resource) DO NOTHING",
			resource, kind, hostTag, now.Unix())
		if err != nil {
			return false, err
		}
		n, err := r.RowsAffected()
		return n > 0, err
	})
}

// Explains checks whether a resource is recorded on a host, even a deleted one.
func (s *recordReader) Explains(ctx context.Context, resource string) (bool, error) {
	return read(ctx, s, resource != "", func(q query) (bool, error) {
		var found bool
		err := q.row(`SELECT EXISTS (SELECT 1 FROM hosts WHERE instance_id = ? OR root_volume = ? OR data_volume = ? OR allocation_id = ?
			UNION ALL SELECT 1 FROM kept_volumes WHERE volume = ?
			UNION ALL SELECT 1 FROM replacements WHERE old_instance = ? OR old_volume = ?)`,
			resource, resource, resource, resource, resource, resource, resource).Scan(&found)
		return found, err
	})
}

// NoteGrow records a volume modification and prunes expired notes for that volume.
func (s *Store) NoteGrow(ctx context.Context, volume string, now time.Time) error {
	return change(ctx, s, volume != "", func(w *writing) error {
		if err := w.exec("DELETE FROM volume_grows WHERE volume = ? AND noted <= ?", volume, now.Add(-GrowWindow).Unix()); err != nil {
			return err
		}
		return w.exec("INSERT INTO volume_grows (volume, noted) VALUES (?, ?)", volume, now.Unix())
	})
}

// PruneGrows removes one bounded page of expired volume modification notes.
func (s *Store) PruneGrows(ctx context.Context, now time.Time, limit int) (int, error) {
	return write(ctx, s, validLimit(limit), func(w *writing) (int, error) {
		r, err := w.ExecContext(ctx, `DELETE FROM volume_grows WHERE id IN
			(SELECT id FROM volume_grows WHERE noted <= ? ORDER BY noted, id LIMIT ?)`, now.Add(-GrowWindow).Unix(), limit)
		if err != nil {
			return 0, err
		}
		n, err := r.RowsAffected()
		return int(n), err
	})
}
