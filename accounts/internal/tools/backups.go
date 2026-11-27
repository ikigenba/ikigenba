package tools

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

func (cfg Config) backupHostOperation(start func(context.Context, string, identity.Caller) (store.Host, store.Operation, error)) func(context.Context, identity.Caller, HostArgs, *changeRecord) (HostChange, error) {
	return func(ctx context.Context, c identity.Caller, a HostArgs, r *changeRecord) (HostChange, error) {
		if _, err := cfg.Store.Host(ctx, a.Host); err != nil {
			return HostChange{}, storeError(err, NoHost)
		}
		h, op, err := start(ctx, a.Host, c)
		if err != nil {
			return HostChange{}, backupHostError(err)
		}
		return r.hostResult(h, op, op.ResumeState), nil
	}
}

func backupHostError(err error) error {
	if errors.Is(err, store.ErrHostGone) {
		return errors.New(NoHost)
	}
	return storeError(err, NoHost)
}

func (cfg Config) replaceHost(ctx context.Context, c identity.Caller, a ReplaceHostArgs, r *changeRecord) (HostChange, error) {
	if a.From != store.ReplaceVolume && a.From != store.ReplaceBackup {
		return HostChange{}, errors.New(BadReplace)
	}
	if err := checkSize(a.Type, a.RootGB, a.DataGB); err != nil {
		return HostChange{}, err
	}
	if a.From == store.ReplaceVolume && a.DataGB != nil {
		return HostChange{}, errors.New(KeepsVolumeSize)
	}
	if _, err := cfg.Store.Host(ctx, a.Host); err != nil {
		return HostChange{}, storeError(err, NoHost)
	}
	target := store.Target{Type: value(a.Type, ""), RootGB: value(a.RootGB, 0), DataGB: value(a.DataGB, 0)}
	h, op, cancelled, err := cfg.Store.ReplaceHost(ctx, a.Host, a.From, target, c)
	if err != nil {
		return HostChange{}, backupHostError(err)
	}
	r.cancelledRestores = cancelled
	return r.hostResult(h, op, op.ResumeState), nil
}

func backupAnswer(b store.AccountBackup, settings store.BackupSettings, now time.Time) AccountBackups {
	return AccountBackups{
		Account: b.Account, Host: b.Host, State: b.State, Health: store.BackupHealth(b, settings, now),
		Snapshot: stamp(b.Snapshot), Checked: stamp(b.Checked), Error: b.Error,
		Databases: b.Databases, DatabaseErrors: b.DatabaseErrors, Synced: stamp(b.Synced), Written: stamp(b.Written),
		Final: b.Final, FinalAt: stamp(b.FinalAt), Restore: b.Restore, Reported: stamp(b.Reported),
		Delete: b.Delete, DeleteReason: b.DeleteReason,
	}
}

func (cfg Config) backupStatus(ctx context.Context, a BackupStatusArgs) (BackupStatusList, error) {
	token := paging.Token{Tool: ToolBackupStatus}
	var after string
	size, err := window(token, a.Page, a.Limit, &after)
	if err != nil {
		return BackupStatusList{}, err
	}
	host := value(a.Host, "")
	if host != "" {
		if _, err := cfg.Store.Host(ctx, host); err != nil {
			return BackupStatusList{}, storeError(err, NoHost)
		}
	}
	rows, err := cfg.Store.ListBackups(ctx, host, after, size+1)
	if err != nil {
		return BackupStatusList{}, storeError(err, NoHost)
	}
	settings, err := cfg.Store.BackupSettings(ctx)
	if err != nil {
		return BackupStatusList{}, errors.New(store.Unreachable)
	}
	var out BackupStatusList
	if len(rows) > size {
		next := token.Encode(rows[size-1].Account)
		out.Next, rows = &next, rows[:size]
	}
	out.Accounts = make([]AccountBackups, 0, len(rows))
	now := cfg.Telemetry.Now()
	for _, b := range rows {
		out.Accounts = append(out.Accounts, backupAnswer(b, settings, now))
	}
	return out, nil
}

func (cfg Config) restorePoints(ctx context.Context, args AccountArgs) (RestorePoints, error) {
	a, err := cfg.Store.Account(ctx, args.Account)
	if err != nil {
		return RestorePoints{}, storeError(err, NoAccount)
	}
	out := RestorePoints{Account: a.Slug, Host: a.Host, Snapshots: []string{}, Databases: []DatabasePoint{}}
	report, err := cfg.Store.Report(ctx, a.Host)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return RestorePoints{}, errors.New(store.Unreachable)
	}
	if report == nil {
		return out, nil
	}
	out.Reported = stamp(report.Received)
	for _, entry := range report.Body.Backups {
		if entry.Account != a.Slug {
			continue
		}
		settings, err := cfg.Store.BackupSettings(ctx)
		if err != nil {
			return RestorePoints{}, errors.New(store.Unreachable)
		}
		out.DatabaseTotal = entry.DatabaseTotal
		slices.SortFunc(entry.Snapshots, func(a, b int64) int { return cmp.Compare(b, a) })
		for _, unix := range entry.Snapshots[:min(len(entry.Snapshots), protocol.MaxSnapshots)] {
			out.Snapshots = append(out.Snapshots, stamp(time.Unix(unix, 0)))
		}
		slices.SortFunc(entry.Databases, func(a, b protocol.Database) int { return cmp.Compare(a.Path, b.Path) })
		now := cfg.Telemetry.Now()
		for _, d := range entry.Databases[:min(len(entry.Databases), protocol.MaxDatabases)] {
			out.Databases = append(out.Databases, DatabasePoint{
				Path: d.Path, Health: store.DatabaseHealth(d, settings, now),
				Oldest: stamp(d.Oldest), Written: stamp(d.Written), Synced: stamp(d.Synced), Error: d.Error,
			})
		}
		break
	}
	return out, nil
}

func (cfg Config) restoreDatabase(ctx context.Context, c identity.Caller, a RestoreDatabaseArgs, r *changeRecord) (RestoreMade, error) {
	if err := valid.DatabasePath(a.Path); err != nil {
		return RestoreMade{}, errors.New(InvalidPath)
	}
	asOf, err := time.Parse(time.RFC3339, a.AsOf)
	if err != nil {
		return RestoreMade{}, errors.New(BadTime)
	}
	return cfg.requestRestore(ctx, c, store.RestoreRequest{
		Account: a.Account, Kind: protocol.RestoreDatabase, Path: a.Path, AsOf: asOf.UTC().Truncate(time.Second),
	}, r)
}

func (cfg Config) restoreFiles(ctx context.Context, c identity.Caller, a RestoreFilesArgs, r *changeRecord) (RestoreMade, error) {
	path := value(a.Path, "")
	if err := valid.RestorePath(path); err != nil {
		return RestoreMade{}, errors.New(InvalidPath)
	}
	snapshot, err := time.Parse(time.RFC3339, a.Snapshot)
	if err != nil {
		return RestoreMade{}, errors.New(BadTime)
	}
	return cfg.requestRestore(ctx, c, store.RestoreRequest{
		Account: a.Account, Kind: protocol.RestoreFiles, Path: path, User: value(a.User, ""), AsOf: snapshot,
	}, r)
}

func (cfg Config) requestRestore(ctx context.Context, c identity.Caller, req store.RestoreRequest, r *changeRecord) (RestoreMade, error) {
	account, err := cfg.Store.Account(ctx, req.Account)
	if err != nil {
		return RestoreMade{}, storeError(err, NoAccount)
	}
	r.host = account.Host
	if req.AsOf.IsZero() {
		// A parsed timestamp in the zero second is present. The store treats
		// the zero Time as absent and truncates accepted values to the second.
		req.AsOf = req.AsOf.Add(time.Nanosecond)
	}
	restore, op, err := cfg.Store.RequestRestore(ctx, req, c)
	if err != nil {
		return RestoreMade{}, storeError(err, NoAccount)
	}
	r.host, r.operation = restore.Host, op
	return RestoreMade{
		Restore: restore.ID, Account: restore.Account, Host: restore.Host, Kind: restore.Kind, Path: restore.Path,
		User: restore.User, AsOf: stamp(restore.AsOf), State: restore.State,
	}, nil
}

func (cfg Config) deleteAccountBackups(ctx context.Context, c identity.Caller, a AccountArgs, r *changeRecord) (BackupsDeletion, error) {
	if err := valid.Slug(a.Account); err != nil {
		return BackupsDeletion{}, nameError(err)
	}
	deletion, started, err := cfg.Store.RequestBackupDelete(ctx, a.Account, c)
	if err != nil {
		return BackupsDeletion{}, storeError(err, NoAccount)
	}
	r.backupDeleteStarted = started
	return BackupsDeletion{Account: deletion.Account, State: deletion.State}, nil
}
