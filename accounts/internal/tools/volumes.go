package tools

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

func keptVolumeAnswer(volume store.KeptVolume) KeptVolume {
	return KeptVolume{
		Volume: volume.Volume, Host: volume.Host, Cause: volume.Cause, SizeGB: volume.SizeGB,
		Deleted: stamp(volume.Deleted), State: volume.State, Reason: volume.Reason,
	}
}

func (cfg Config) keptVolumes(ctx context.Context, args KeptVolumesArgs) (KeptVolumeList, error) {
	token := paging.Token{Tool: ToolKeptVolumes}
	var deletedNano int64
	var afterVolume string
	size, err := window(token, args.Page, args.Limit, &deletedNano, &afterVolume)
	if err != nil {
		return KeptVolumeList{}, err
	}
	var afterDeleted time.Time
	if value(args.Page, "") != "" {
		afterDeleted = time.Unix(0, deletedNano).UTC()
	}
	rows, err := cfg.Store.ListKeptVolumes(ctx, afterDeleted, afterVolume, size+1)
	if err != nil {
		return KeptVolumeList{}, storeError(err, NoKeptVolume)
	}
	var result KeptVolumeList
	if len(rows) > size {
		last := rows[size-1]
		next := token.Encode(last.Deleted.UnixNano(), last.Volume)
		result.Next, rows = &next, rows[:size]
	}
	result.KeptVolumes = make([]KeptVolume, 0, len(rows))
	for _, volume := range rows {
		result.KeptVolumes = append(result.KeptVolumes, keptVolumeAnswer(volume))
	}
	return result, nil
}

func (cfg Config) deleteKeptVolume(ctx context.Context, c identity.Caller, args KeptVolumeArgs, r *changeRecord) (KeptVolume, error) {
	if args.Volume == "" {
		// An empty ID cannot name a kept volume. Still honor store failures.
		err := store.ReadSnapshot(ctx, cfg.Store, func(*store.Snapshot) error { return store.ErrNotFound })
		return KeptVolume{}, storeError(err, NoKeptVolume)
	}
	volume, err := cfg.Store.DeleteKeptVolume(ctx, args.Volume, c)
	if err != nil {
		if existing, readErr := cfg.Store.KeptVolume(ctx, args.Volume); readErr == nil {
			r.host = existing.Host
		}
		return KeptVolume{}, storeError(err, NoKeptVolume)
	}
	r.host, r.volume = volume.Host, volume.Volume
	return keptVolumeAnswer(volume), nil
}
