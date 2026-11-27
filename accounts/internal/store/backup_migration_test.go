package store_test

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-6CTF-3G33 R-6E1B-H7TS
func TestBackupMigrationAddsOnlyTheDeclaredTables(t *testing.T) {
	migrations := accounts.Migrations()
	body := value(t, result(fs.ReadFile(migrations, "0007_host_backups.sql")))
	eq(t, string(body), backupMigration)
	_, schema := earlierMigrations(t)
	kept := value(t, result(fs.ReadFile(migrations, "0006_kept_volumes.sql")))
	oldTable := regexp.MustCompile(`(?i)\b(` + strings.Join(tableNames(schema+string(kept)), "|") + `)\b`)
	create := regexp.MustCompile(`^CREATE (TABLE|INDEX|UNIQUE INDEX)\b`)
	foreign := regexp.MustCompile(`(?i)\b(FOREIGN\s+KEY|REFERENCES)\b`)
	for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(string(body)), ";"), ";") {
		stmt = strings.TrimSpace(stmt)
		if !create.MatchString(stmt) || oldTable.MatchString(stmt) || foreign.MatchString(stmt) {
			t.Fatalf("migration statement is not additive: %q", stmt)
		}
	}
}

const backupMigration = `CREATE TABLE backup_operations (id TEXT PRIMARY KEY, host TEXT NOT NULL, account TEXT NOT NULL, kind TEXT NOT NULL, step TEXT NOT NULL, status TEXT NOT NULL, generation INTEGER NOT NULL, target_type TEXT NOT NULL, target_root_gb INTEGER NOT NULL, target_data_gb INTEGER NOT NULL, resume_state TEXT NOT NULL, error TEXT NOT NULL, attempts INTEGER NOT NULL, started INTEGER NOT NULL, step_started INTEGER NOT NULL, next_try INTEGER NOT NULL, finished INTEGER NOT NULL, supersedes TEXT NOT NULL, caller_user TEXT NOT NULL, caller_email TEXT NOT NULL, caller_request TEXT NOT NULL, host_seq INTEGER NOT NULL) WITHOUT ROWID;
CREATE INDEX backup_operations_status_next_try ON backup_operations(status, next_try, id);
CREATE INDEX backup_operations_status_id ON backup_operations(status, id);
CREATE INDEX backup_operations_host_id ON backup_operations(host, id);
CREATE INDEX backup_operations_account_kind_status ON backup_operations(account, kind, status);
CREATE TABLE backup_settings (scope TEXT PRIMARY KEY, every_minutes INTEGER NOT NULL, hourly INTEGER NOT NULL, daily INTEGER NOT NULL, monthly INTEGER NOT NULL, window_days INTEGER NOT NULL, files_stale_minutes INTEGER NOT NULL, database_stale_minutes INTEGER NOT NULL, check_days INTEGER NOT NULL) WITHOUT ROWID;
CREATE TABLE host_closes (host_seq INTEGER PRIMARY KEY, host TEXT NOT NULL, closed INTEGER NOT NULL) WITHOUT ROWID;
CREATE TABLE replacements (operation_id TEXT PRIMARY KEY, mode TEXT NOT NULL, old_instance TEXT NOT NULL, old_volume TEXT NOT NULL, retired INTEGER NOT NULL, admitted INTEGER NOT NULL) WITHOUT ROWID;
CREATE INDEX replacements_old_instance ON replacements(old_instance);
CREATE INDEX replacements_old_volume ON replacements(old_volume);
CREATE TABLE restore_databases (operation_id TEXT NOT NULL, account TEXT NOT NULL, path TEXT NOT NULL, PRIMARY KEY (operation_id, account, path)) WITHOUT ROWID;
CREATE TABLE backup_steps (operation_id TEXT NOT NULL, account TEXT NOT NULL, state TEXT NOT NULL, at INTEGER NOT NULL, code TEXT NOT NULL, reason TEXT NOT NULL, PRIMARY KEY (operation_id, account)) WITHOUT ROWID;
CREATE TABLE account_backups (account TEXT PRIMARY KEY, gid INTEGER NOT NULL, host TEXT NOT NULL, snapshot INTEGER NOT NULL, checked INTEGER NOT NULL, error TEXT NOT NULL, databases INTEGER NOT NULL, database_errors INTEGER NOT NULL, synced INTEGER NOT NULL, written INTEGER NOT NULL, reported INTEGER NOT NULL) WITHOUT ROWID;
CREATE TABLE removed_backups (account TEXT PRIMARY KEY, host TEXT NOT NULL, host_seq INTEGER NOT NULL, removed INTEGER NOT NULL, snapshot INTEGER NOT NULL, checked INTEGER NOT NULL, error TEXT NOT NULL, databases INTEGER NOT NULL, database_errors INTEGER NOT NULL, synced INTEGER NOT NULL, written INTEGER NOT NULL, reported INTEGER NOT NULL) WITHOUT ROWID;
CREATE INDEX removed_backups_host ON removed_backups(host, account);
CREATE TABLE restores (id TEXT PRIMARY KEY, host TEXT NOT NULL, host_seq INTEGER NOT NULL, account TEXT NOT NULL, kind TEXT NOT NULL, path TEXT NOT NULL, user_name TEXT NOT NULL, as_of INTEGER NOT NULL, copy TEXT NOT NULL, state TEXT NOT NULL, reason TEXT NOT NULL, asked INTEGER NOT NULL, created_by TEXT NOT NULL) WITHOUT ROWID;
CREATE INDEX restores_host_asked ON restores(host, asked, id);
CREATE INDEX restores_pending ON restores(host, id) WHERE state = 'pending';
CREATE TABLE backups_deletes (account TEXT PRIMARY KEY, state TEXT NOT NULL, reason TEXT NOT NULL, attempts INTEGER NOT NULL, next_try INTEGER NOT NULL, caller_user TEXT NOT NULL, caller_email TEXT NOT NULL, caller_request TEXT NOT NULL) WITHOUT ROWID;
CREATE INDEX backups_deletes_due ON backups_deletes(next_try, account) WHERE state = 'deleting';
CREATE TABLE kept_causes (volume TEXT PRIMARY KEY, cause TEXT NOT NULL) WITHOUT ROWID;
`

// R-VTZ0-NE03 R-UM0R-2K4G
func TestBackupRowsStayWithTheHostSequenceDuringRollback(t *testing.T) {
	rand := new(counterRandom)
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: rand}
	path := filepath.Join(t.TempDir(), "record.db")
	migrations := accounts.Migrations()
	d, s := open(t, path, migrations, cfg)
	h, creation := create(t, s)
	checkErr(t, s.Finish(ctx, creation.ID, store.Update{InstanceID: "previous-instance", AllocationID: "previous-address", DataVolume: "previous-volume"}), nil)
	a, placing := place(t, s, "business", h.Name)
	checkErr(t, s.Finish(ctx, placing.ID, store.Update{}), nil)
	_, closing, err := s.CloseHost(ctx, h.Name, caller)
	checkErr(t, err, nil)
	checkErr(t, s.Finish(ctx, closing.ID, store.Update{}), nil)
	_, err = s.RecordReport(ctx, h.Name, protocol.Report{Backups: []protocol.AccountBackup{{
		Account: a.Slug, Snapshots: []int64{baseTime.Unix()},
	}}}, baseTime)
	checkErr(t, err, nil)
	request := store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreFiles, Path: "work", AsOf: baseTime}
	pending, operation, err := s.RequestRestore(ctx, request, caller)
	checkErr(t, err, nil)
	checkErr(t, d.Close(), nil)

	earlier, _ := earlierMigrations(t)
	kept := value(t, result(fs.ReadFile(migrations, "0006_kept_volumes.sql")))
	earlier["0006_kept_volumes.sql"] = &fstest.MapFile{Data: kept}
	d, _ = open(t, path, earlier, cfg)
	const movedName = "host01#prior"
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, statement := range []string{
			"DELETE FROM reports WHERE host = 'host01'",
			"DELETE FROM accounts WHERE slug = 'business'",
			"DELETE FROM names WHERE name = 'business'",
			"UPDATE hosts SET name = 'host01#prior', state = 'deleted' WHERE name = 'host01'",
			"UPDATE operations SET host = 'host01#prior' WHERE host = 'host01'",
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}), nil)
	fresh, freshOp := seedEarlierNamedCreate(t, d.Write, h.Name, h.Seq+1)
	checkErr(t, d.Close(), nil)

	_, s = open(t, path, migrations, cfg)
	eq(t, value(t, result(s.Host(ctx, fresh.Name))), fresh)
	acct, _ := place(t, s, "new-business", fresh.Name)
	value(t, result(s.AddUser(ctx, acct.Slug, "fresh-user", firstKey, caller)))
	desired := value(t, result(s.Desired(ctx, fresh.Name)))
	eq(t, desired.Users[0].State, protocol.UserActive)
	eq(t, desired.Backup.HostPrefix, protocol.HostPrefix(fresh.Name, fresh.Seq))
	eq(t, desired.Backup.Restores, []protocol.RestoreRequest{})
	eq(t, desired.Backup.Close, "")
	ops := value(t, result(s.OperationsOf(ctx, fresh.Name, "", 20)))
	eq(t, len(ops), 2)
	ids := []string{ops[0].ID, ops[1].ID}
	if !slices.Contains(ids, freshOp.ID) || slices.Contains(ids, closing.ID) || slices.Contains(ids, operation.ID) {
		t.Fatalf("unexpected operations for reused name: %v", ops)
	}
	closing.Host, operation.Host = movedName, movedName
	eq(t, value(t, result(s.Operation(ctx, closing.ID))).Host, closing.Host)
	eq(t, value(t, result(s.Operation(ctx, operation.ID))), operation)
	restores, count, err := s.RestoresOn(ctx, fresh.Name, 20)
	checkErr(t, err, nil)
	eq(t, restores, []store.Restore{})
	eq(t, count, 0)
	_, err = s.RecordReport(ctx, fresh.Name, protocol.Report{Restores: []protocol.RestoreResult{{
		ID: pending.ID, State: protocol.StepDone, Copy: "/incorrect/",
	}}}, baseTime)
	checkErr(t, err, nil)
	got := value(t, result(s.Restore(ctx, pending.ID)))
	eq(t, got.State, protocol.StepPending)
	eq(t, got.Copy, "")
}
