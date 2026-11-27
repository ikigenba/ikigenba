package pages

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func landingBackupRows(ctx context.Context, snapshot *store.Snapshot, data *LandingData, hosts []store.Host, accounts []store.Account, settings store.BackupSettings, now time.Time) error {
	data.Hosts = make([]HostRow, 0, len(hosts))
	for _, host := range hosts {
		facts, err := snapshot.BackupsOn(ctx, host.Name)
		if err != nil {
			return err
		}
		data.Hosts = append(data.Hosts, hostRow(host, data.Domain, now, flaggedBackups(facts, settings, now)))
	}
	data.Accounts = make([]AccountRow, 0, len(accounts))
	for _, account := range accounts {
		facts, err := snapshot.BackupOf(ctx, account.Slug)
		if err != nil {
			return err
		}
		data.Accounts = append(data.Accounts, accountRow(account, data.Domain, facts, settings, now))
	}
	return nil
}

func flaggedBackups(facts []store.AccountBackup, settings store.BackupSettings, now time.Time) int {
	count := 0
	for _, backup := range facts {
		switch store.BackupHealth(backup, settings, now) {
		case store.BackupStale, store.BackupError:
			count++
		}
	}
	return count
}

func backupRows(facts []store.AccountBackup, accounts []store.Account, report *store.Report, settings store.BackupSettings, now time.Time) ([]BackupRow, int) {
	entries := make(map[string]protocol.AccountBackup)
	if report != nil {
		for _, entry := range report.Body.Backups {
			if _, exists := entries[entry.Account]; !exists {
				entries[entry.Account] = entry
			}
		}
		for _, account := range accounts {
			if report.Received.Before(account.Created) {
				delete(entries, account.Slug)
			}
		}
	}
	rows := make([]BackupRow, 0, len(facts))
	count := 0
	for _, backup := range facts {
		entry := entries[backup.Account]
		row := BackupRow{
			Account: backup.Account, Health: store.BackupHealth(backup, settings, now),
			FilesStale: store.FilesStale(backup, settings, now),
			Error:      backup.Error, Final: backup.Final, Restore: backup.Restore,
			Databases:     databaseRows(entry.Databases, settings, now),
			DatabaseTotal: max(entry.DatabaseTotal, len(entry.Databases)),
		}
		row.Snapshot, row.SnapshotText = optionalStamps(backup.Snapshot, SecondLayout)
		row.Checked, row.CheckedText = optionalStamps(backup.Checked, SecondLayout)
		if backup.Final == protocol.StepDone {
			row.FinalAt, row.FinalAtText = optionalStamps(backup.FinalAt, SecondLayout)
		}
		count += len(row.Databases)
		rows = append(rows, row)
	}
	return rows, count
}

func databaseRows(databases []protocol.Database, settings store.BackupSettings, now time.Time) []DatabaseRow {
	sorted := slices.Clone(databases)
	slices.SortStableFunc(sorted, func(a, b protocol.Database) int { return strings.Compare(a.Path, b.Path) })
	limit := min(MaxDatabaseRows, len(sorted))
	rows := make([]DatabaseRow, 0, limit)
	for _, database := range sorted[:limit] {
		written, writtenText := optionalStamps(database.Written, SecondLayout)
		rows = append(rows, DatabaseRow{
			Path: database.Path, Health: store.DatabaseHealth(database, settings, now),
			Written: written, WrittenText: writtenText, Error: database.Error,
		})
	}
	return rows
}

func restoreRows(restores []store.Restore) []RestoreRow {
	rows := make([]RestoreRow, 0, len(restores))
	for _, restore := range restores {
		asOf, asOfText := stamps(restore.AsOf, SecondLayout)
		asked, askedText := stamps(restore.Asked, SecondLayout)
		rows = append(rows, RestoreRow{
			Account: restore.Account, Kind: restore.Kind, Path: restore.Path, AsOf: asOf, AsOfText: asOfText,
			Copy: restore.Copy, State: restore.State, Reason: restore.Reason, Asked: asked, AskedText: askedText,
		})
	}
	return rows
}
