package ops

import (
	"context"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (e *Engine) deleteBackupPage(ctx context.Context, prefix string) (bool, error) {
	bucket := e.cfg.Hosts.BackupBucket()
	cl := e.cfg.Cloud
	var keys []string
	listed := false
	err := cl.ListKeys(ctx, bucket, prefix, func(page []string) bool {
		if !listed {
			keys, listed = page, true
		}
		return false
	})
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil || len(keys) == 0 {
		return false, err
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			return false, &cloud.Error{Service: "s3", Operation: "ListObjectsV2", Subject: bucket, Code: "UnexpectedKey", Class: cloud.ClassMismatch}
		}
	}
	return true, cl.DeleteKeys(ctx, bucket, keys)
}

func (s step) deleteHostBackups() error {
	deleted, err := s.deleteBackupPage(s.ctx, protocol.HostPrefix(s.h.Name, s.h.Seq))
	if absentOK(err) != nil {
		return err
	}
	if err != nil || !deleted {
		return s.finish(store.Update{})
	}
	return s.deferFor(0, s.op.Attempts)
}

func (e *Engine) deleteAccountBackups(ctx context.Context, now time.Time) error {
	if e.cfg.Cloud == nil {
		return nil
	}
	for after := ""; ; {
		page, err := e.cfg.Store.DueBackupDeletes(ctx, now, after, store.BatchSize)
		if err != nil {
			return err
		}
		for _, row := range page {
			if err := stepStoreError(e.deleteRemovedBackup(ctx, now, row)); err != nil {
				return err
			}
			after = row.Account
		}
		if len(page) < store.BatchSize {
			return nil
		}
	}
}

func (e *Engine) deleteRemovedBackup(ctx context.Context, now time.Time, row store.BackupsDelete) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deleted, err := e.deleteBackupPage(ctx, protocol.AccountPrefix(row.Account))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	st := e.cfg.Store
	if err == nil {
		if deleted {
			return st.DeferBackupDelete(ctx, row.Account, now, row.Attempts)
		}
		return st.FinishBackupDelete(ctx, row.Account)
	}
	switch cloud.ClassOf(err) {
	case cloud.ClassNotFound:
		return st.FinishBackupDelete(ctx, row.Account)
	case cloud.ClassRefused, cloud.ClassMismatch:
		return st.FailBackupDelete(ctx, row.Account, err.Error())
	default:
		return st.DeferBackupDelete(ctx, row.Account, now.Add(retryDelay(row.Attempts)), row.Attempts+1)
	}
}
