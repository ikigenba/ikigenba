package store_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

// R-WDHE-RPV7 R-VV6X-15QS R-VWET-EXHH
func TestBackupSettingsAreDefaultedAndReadOnly(t *testing.T) {
	d, s, _ := fixture(t)
	want := store.BackupSettings{EveryMinutes: 60, Hourly: 24, Daily: 7, Monthly: 1, WindowDays: 35,
		FilesStaleMinutes: 120, DatabaseStaleMinutes: 10, CheckDays: 7}
	first := store.DefaultBackupSettings()
	eq(t, first, want)
	first.EveryMinutes = 42
	eq(t, store.DefaultBackupSettings(), want)
	eq(t, value(t, result(s.BackupSettings(ctx))), want)
	h := readyHost(t, s)
	_, closing := closeHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, closing.ID, store.Update{}), nil)
	eq(t, value(t, result(s.BackupSettings(ctx))), want)
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO backup_settings
			(scope, every_minutes, hourly, daily, monthly, window_days, files_stale_minutes, database_stale_minutes, check_days)
			VALUES ('', 30, 16, 5, 2, 50, 90, 12, 6), ('elsewhere', 95, 1, 1, 1, 1, 1, 1, 1)`)
		return err
	}), nil)
	want = store.BackupSettings{EveryMinutes: 30, Hourly: 16, Daily: 5, Monthly: 2, WindowDays: 50,
		FilesStaleMinutes: 90, DatabaseStaleMinutes: 12, CheckDays: 6}
	eq(t, value(t, result(s.BackupSettings(ctx))), want)
	_, opening := openHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, opening.ID, store.Update{}), nil)
	_, replacing, _ := replaceHost(t, s, h.Name, store.ReplaceVolume, store.Target{})
	checkErr(t, s.Retire(ctx, replacing.ID), nil)
	checkErr(t, s.Admit(ctx, replacing.ID), nil)
	checkErr(t, s.Finish(ctx, replacing.ID, store.Update{}), nil)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{}, baseTime)))
	value(t, result(s.DeleteAccountBackups(ctx, "unplaced", caller)))
	checkErr(t, s.FinishBackupDelete(ctx, "unplaced"), nil)
	eq(t, value(t, result(s.BackupSettings(ctx))), want)
}

// R-5V77-S6E9 R-5WF4-5Y4Y R-5XN0-JPVN
func TestBackupHealthUsesTheReaderClockAndConfiguredThresholds(t *testing.T) {
	now := baseTime
	cfg := store.DefaultBackupSettings()
	cfg.FilesStaleMinutes, cfg.DatabaseStaleMinutes = 72, 6
	freshFiles := now.Add(-72 * time.Minute)
	oldFiles := freshFiles.Add(-time.Second)
	freshDatabase := now.Add(-6 * time.Minute)
	oldDatabase := freshDatabase.Add(-time.Second)
	for _, tc := range []struct {
		name       string
		backup     store.AccountBackup
		health     string
		filesStale bool
	}{
		{"no backup", store.AccountBackup{}, store.BackupNone, false},
		{"snapshot exactly at threshold", store.AccountBackup{Snapshot: freshFiles}, store.BackupHealthy, false},
		{"older snapshot", store.AccountBackup{Snapshot: oldFiles}, store.BackupStale, true},
		{"only recent database", store.AccountBackup{Synced: freshDatabase}, store.BackupNone, false},
		{"older database", store.AccountBackup{Snapshot: freshFiles, Synced: oldDatabase}, store.BackupStale, false},
		{"error takes precedence", store.AccountBackup{Snapshot: oldFiles, Error: "reported error"}, store.BackupError, true},
		{"database errors take precedence", store.AccountBackup{Snapshot: oldFiles, DatabaseErrors: 1}, store.BackupError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eq(t, store.BackupHealth(tc.backup, cfg, now), tc.health)
			eq(t, store.FilesStale(tc.backup, cfg, now), tc.filesStale)
		})
	}
	for _, tc := range []struct {
		db     protocol.Database
		health string
	}{
		{protocol.Database{Error: "error"}, store.BackupError},
		{protocol.Database{Written: now}, store.BackupNone},
		{protocol.Database{Written: now, Synced: oldDatabase}, store.BackupStale},
		{protocol.Database{Written: oldFiles, Synced: freshDatabase}, store.BackupHealthy},
	} {
		eq(t, store.DatabaseHealth(tc.db, cfg, now), tc.health)
	}
}

// R-WC9I-DY4I R-75JE-BGKN
func TestEmptyBackupReadsAndInvalidPageLimits(t *testing.T) {
	_, s, now := fixture(t)
	const missing = "missing"
	eq(t, value(t, result(s.BackupSettings(ctx))), store.DefaultBackupSettings())
	for _, limit := range []int{1, paging.MaxLimit + 1} {
		eq(t, value(t, result(s.ListBackups(ctx, "", "", limit))), []store.AccountBackup{})
		eq(t, value(t, result(s.DueBackupDeletes(ctx, *now, "", limit))), []store.BackupsDelete{})
		observe(s.ListBackups(ctx, missing, "", limit)).assertZero(t, store.ErrNotFound)
		observe2(s.RestoresOn(ctx, missing, limit)).assertZero(t, store.ErrNotFound)
	}
	for _, limit := range []int{-1, 0, paging.MaxLimit + 2} {
		observe(s.ListBackups(ctx, "", "", limit)).assertZero(t, store.ErrUnreachable)
		observe(s.DueBackupDeletes(ctx, *now, "", limit)).assertZero(t, store.ErrUnreachable)
		observe2(s.RestoresOn(ctx, missing, limit)).assertZero(t, store.ErrUnreachable)
	}
	for _, call := range []observation{
		observe(s.Replacement(ctx, missing)),
		noOutput(s.Retire(ctx, missing)),
		noOutput(s.Admit(ctx, missing)),
		observe(s.BackupSteps(ctx, missing)),
		observe(s.Restore(ctx, missing)),
		observe(s.BackupOf(ctx, missing)),
		observe(s.BackupsOn(ctx, missing)),
		noOutput(s.DeferBackupDelete(ctx, missing, *now, 1)),
		noOutput(s.FinishBackupDelete(ctx, missing)),
		noOutput(s.FailBackupDelete(ctx, missing, "error")),
		observe2(s.CloseHost(ctx, missing, caller)),
		observe2(s.OpenHost(ctx, missing, caller)),
		observe3(s.ReplaceHost(ctx, missing, store.ReplaceBackup, store.Target{}, caller)),
		observe2(s.RequestRestore(ctx, store.RestoreRequest{Account: missing, Kind: protocol.RestoreFiles, AsOf: *now}, caller)),
	} {
		call.assertZero(t, store.ErrNotFound)
	}
}
