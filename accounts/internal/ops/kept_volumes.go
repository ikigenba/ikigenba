package ops

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (e *Engine) adoptKeptVolumes(ctx context.Context) error {
	for {
		n, err := e.cfg.Store.AdoptKept(ctx, store.BatchSize)
		if err != nil || n < store.BatchSize {
			return err
		}
	}
}

func (e *Engine) deleteKeptVolumes(ctx context.Context, now time.Time) error {
	if e.cfg.Cloud == nil {
		return nil
	}
	for after := ""; ; {
		page, err := e.cfg.Store.DueVolumeDeletes(ctx, now, after, store.BatchSize)
		if err != nil {
			return err
		}
		for _, v := range page {
			if err := stepStoreError(e.deleteKeptVolume(ctx, now, v)); err != nil {
				return err
			}
			after = v.Volume
		}
		if len(page) < store.BatchSize {
			return nil
		}
	}
}

func (e *Engine) deleteKeptVolume(ctx context.Context, now time.Time, v store.KeptVolume) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	volume, err := e.cfg.Cloud.DescribeVolume(ctx, v.Volume)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil && volume.State != cloud.VolumeDeleted && volume.State != cloud.VolumeDeleting {
		err = e.cfg.Cloud.DeleteVolume(ctx, v.Volume)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	class := cloud.ClassOf(err)
	switch {
	case err == nil && volume.State != cloud.VolumeDeleted:
		return e.cfg.Store.DeferVolumeDelete(ctx, v.Volume, now.Add(PollInterval), v.Attempts)
	case err == nil || class == cloud.ClassNotFound:
		return e.endKeptVolumeDelete(ctx, v, store.VolumeDeleted, "")
	case class == cloud.ClassRefused || class == cloud.ClassMismatch:
		return e.endKeptVolumeDelete(ctx, v, store.VolumeKept, err.Error())
	default:
		return e.cfg.Store.DeferVolumeDelete(ctx, v.Volume, now.Add(retryDelay(v.Attempts)), v.Attempts+1)
	}
}

func (e *Engine) endKeptVolumeDelete(ctx context.Context, v store.KeptVolume, state, reason string) error {
	var err error
	if state == store.VolumeDeleted {
		err = e.cfg.Store.FinishVolumeDelete(ctx, v.Volume)
	} else {
		err = e.cfg.Store.FailVolumeDelete(ctx, v.Volume, reason)
	}
	if err == nil && e.cfg.Telemetry != nil {
		caller := identity.NewContext(ctx, v.Caller)
		trail.VolumeChanged(caller, e.cfg.Telemetry, v.Volume, v.Host, store.VolumeDeleting, state, reason)
	}
	return err
}
