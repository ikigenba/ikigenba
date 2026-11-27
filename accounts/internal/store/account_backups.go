package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

const backupFactFields = "snapshot, checked, error, databases, database_errors, synced, written, reported"

func backupFactDest(b *AccountBackup) []any {
	return []any{storedTime{&b.Snapshot}, storedTime{&b.Checked}, &b.Error, &b.Databases, &b.DatabaseErrors,
		storedTime{&b.Synced}, storedTime{&b.Written}, storedTime{&b.Reported}}
}

func ignoreNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func boundedBackupError(reason string) string {
	// The bytes of the prefix before escaping cannot exceed the JSON budget.
	end := 0
	for end < len(reason) && end < protocol.MaxBackupError {
		_, width := utf8.DecodeRuneInString(reason[end:])
		encoded, _ := json.Marshal(reason[:end+width])
		if len(encoded) > protocol.MaxBackupError {
			break
		}
		end += width
	}
	return reason[:end]
}

func writeAccountBackup(q query, a Account, b protocol.AccountBackup, reported time.Time) error {
	return q.exec(`INSERT INTO account_backups (account, gid, host, `+backupFactFields+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account) DO UPDATE SET gid = excluded.gid, host = excluded.host, snapshot = excluded.snapshot,
		checked = excluded.checked, error = excluded.error, databases = excluded.databases, database_errors = excluded.database_errors,
		synced = excluded.synced, written = excluded.written, reported = excluded.reported`,
		a.Slug, a.GID, a.Host, b.Snapshot.Unix(), b.Checked.Unix(), b.Error, b.DatabaseTotal, b.DatabaseErrors,
		b.Synced.Unix(), b.Written.Unix(), reported.Unix())
}

func moveRemovedBackup(q query, slug string, removed time.Time) error {
	if err := q.exec(`INSERT INTO removed_backups (account, host, host_seq, removed, `+backupFactFields+`)
		SELECT b.account, b.host, h.seq, ?, b.snapshot, b.checked, b.error, b.databases, b.database_errors, b.synced, b.written, b.reported
		FROM account_backups b JOIN accounts a ON a.slug = b.account AND a.gid = b.gid
		JOIN hosts h ON h.name = a.host WHERE b.account = ?
		ON CONFLICT (account) DO UPDATE SET host = excluded.host, host_seq = excluded.host_seq, removed = excluded.removed,
		snapshot = excluded.snapshot, checked = excluded.checked, error = excluded.error, databases = excluded.databases,
		database_errors = excluded.database_errors, synced = excluded.synced, written = excluded.written, reported = excluded.reported`,
		removed.Unix(), slug); err != nil {
		return err
	}
	return q.exec("DELETE FROM account_backups WHERE account = ?", slug)
}

func accountBackup(q query, a Account, h Host) (AccountBackup, error) {
	b := AccountBackup{Account: a.Slug, Host: a.Host, State: a.State}
	err := q.row("SELECT "+backupFactFields+" FROM account_backups WHERE account = ? AND gid = ?", a.Slug, a.GID).
		Scan(backupFactDest(&b)...)
	if err := ignoreNoRows(err); err != nil {
		return AccountBackup{}, err
	}
	err = q.row(`SELECT s.state, s.at FROM backup_steps s JOIN backup_operations o ON o.id = s.operation_id
		WHERE o.kind = 'close' AND o.host_seq = ? AND o.supersedes = 'latest' AND s.account = ?`, h.Seq, a.Slug).
		Scan(&b.Final, storedTime{&b.FinalAt})
	if err := ignoreNoRows(err); err != nil {
		return AccountBackup{}, err
	}
	if h.Replace == ReplaceBackup {
		err = q.row("SELECT state FROM backup_steps WHERE operation_id = ? AND account = ?", h.Operation, a.Slug).Scan(&b.Restore)
	}
	return b, ignoreNoRows(err)
}

func removedBackup(q query, slug string) (AccountBackup, error) {
	b := AccountBackup{Account: slug, State: BackupsRemoved}
	err := q.row("SELECT host, "+backupFactFields+" FROM removed_backups WHERE account = ?", slug).
		Scan(append([]any{&b.Host}, backupFactDest(&b)...)...)
	if err != nil {
		return AccountBackup{}, err
	}
	err = q.row("SELECT state, CASE WHEN state = 'failed' THEN reason ELSE '' END FROM backups_deletes WHERE account = ?", slug).
		Scan(&b.Delete, &b.DeleteReason)
	return b, ignoreNoRows(err)
}

func (s *recordReader) BackupOf(ctx context.Context, account string) (AccountBackup, error) {
	return read(ctx, s, account != "", func(q query) (AccountBackup, error) {
		a, err := getAccount(q, account)
		if err != nil {
			return AccountBackup{}, err
		}
		h, err := getHost(q, a.Host)
		if err != nil {
			return AccountBackup{}, err
		}
		return accountBackup(q, a, h)
	})
}

func (s *recordReader) BackupsOn(ctx context.Context, host string) ([]AccountBackup, error) {
	return read(ctx, s, host != "", func(q query) ([]AccountBackup, error) {
		h, err := liveHost(q, host)
		if err != nil {
			return nil, err
		}
		accounts, err := hostAccounts(q, host)
		if err != nil {
			return nil, err
		}
		out := make([]AccountBackup, 0, len(accounts))
		for _, a := range accounts {
			b, err := accountBackup(q, a, h)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
		return out, nil
	})
}

type backupEntry struct {
	slug string
	live bool
}

func backupEntries(q query, host, after string, limit int) ([]backupEntry, error) {
	live := "SELECT slug, 1 FROM accounts WHERE slug > ?"
	removed := `SELECT account, 0 FROM removed_backups WHERE account > ?
		AND NOT EXISTS (SELECT 1 FROM accounts WHERE slug = removed_backups.account)`
	args := []any{after, after}
	if host != "" {
		h, err := liveHost(q, host)
		if err != nil {
			return nil, err
		}
		live += " AND host = ?"
		removed += " AND host = ? AND host_seq = ?"
		args = []any{after, host, after, host, h.Seq}
	}
	return collect(q, func(r scanner) (b backupEntry, err error) {
		err = r.Scan(&b.slug, &b.live)
		return b, err
	}, live+" UNION ALL "+removed+" ORDER BY 1 LIMIT ?", append(args, limit)...)
}

func listBackupEntry(q query, entry backupEntry) (AccountBackup, error) {
	if !entry.live {
		return removedBackup(q, entry.slug)
	}
	a, err := getAccount(q, entry.slug)
	if err != nil {
		return AccountBackup{}, err
	}
	h, err := getHost(q, a.Host)
	if err != nil {
		return AccountBackup{}, err
	}
	return accountBackup(q, a, h)
}

func (s *recordReader) ListBackups(ctx context.Context, host, after string, limit int) ([]AccountBackup, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]AccountBackup, error) {
		entries, err := backupEntries(q, host, after, limit)
		if err != nil {
			return nil, err
		}
		out := make([]AccountBackup, 0, len(entries))
		for _, entry := range entries {
			b, err := listBackupEntry(q, entry)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
		return out, nil
	})
}

const backupsDeleteFields = "account, state, reason, attempts, next_try, caller_user, caller_email, caller_request"
const backupsDeleteSelect = "SELECT " + backupsDeleteFields + " FROM backups_deletes"

func scanBackupsDelete(r scanner) (b BackupsDelete, err error) {
	err = r.Scan(&b.Account, &b.State, &b.Reason, &b.Attempts, storedTime{&b.NextTry}, &b.Caller.UserID, &b.Caller.Email, &b.Caller.RequestID)
	return b, err
}

func getBackupsDelete(q query, slug string) (BackupsDelete, error) {
	return scanBackupsDelete(q.row(backupsDeleteSelect+" WHERE account = ?", slug))
}

func (s *recordReader) BackupsDeleteOf(ctx context.Context, slug string) (BackupsDelete, error) {
	return read(ctx, s, slug != "", func(q query) (BackupsDelete, error) { return getBackupsDelete(q, slug) })
}

// DeleteAccountBackups requests deletion of a removed account's retained backups.
func (s *Store) DeleteAccountBackups(ctx context.Context, slug string, c identity.Caller) (BackupsDelete, error) {
	b, _, err := s.RequestBackupDelete(ctx, slug, c)
	return b, err
}

// RequestBackupDelete also reports whether this call recorded a new deletion.
func (s *Store) RequestBackupDelete(ctx context.Context, slug string, c identity.Caller) (BackupsDelete, bool, error) {
	type result struct {
		row      BackupsDelete
		recorded bool
	}
	out, err := write(ctx, s, slug != "" && validCaller(c), func(w *writing) (result, error) {
		var exists bool
		if err := w.row("SELECT EXISTS (SELECT 1 FROM accounts WHERE slug = ?)", slug).Scan(&exists); err != nil {
			return result{}, err
		}
		if exists {
			return result{}, ErrAccountExists
		}
		before, err := getBackupsDelete(w.query, slug)
		if before.State == DeleteBackupsDeleting || ignoreNoRows(err) != nil {
			return result{row: before}, err
		}
		b := BackupsDelete{Account: slug, State: DeleteBackupsDeleting, NextTry: s.stamp(), Caller: c}
		return result{row: b, recorded: true}, w.exec(`INSERT INTO backups_deletes (`+backupsDeleteFields+`) VALUES (?, ?, '', 0, ?, ?, ?, ?)
			ON CONFLICT (account) DO UPDATE SET state = excluded.state, reason = '', attempts = 0, next_try = excluded.next_try,
			caller_user = excluded.caller_user, caller_email = excluded.caller_email, caller_request = excluded.caller_request`,
			slug, b.State, b.NextTry.Unix(), c.UserID, c.Email, c.RequestID)
	})
	return out.row, out.recorded, err
}

func (s *recordReader) DueBackupDeletes(ctx context.Context, now time.Time, after string, limit int) ([]BackupsDelete, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]BackupsDelete, error) {
		return collect(q, scanBackupsDelete, backupsDeleteSelect+" WHERE state = 'deleting' AND next_try <= ? AND account > ? ORDER BY account LIMIT ?",
			now.Unix(), after, limit)
	})
}

func updateBackupDelete(ctx context.Context, s *Store, slug string, fn func(query) error) error {
	return change(ctx, s, slug != "", func(w *writing) error {
		b, err := getBackupsDelete(w.query, slug)
		if err != nil {
			return err
		}
		if b.State != DeleteBackupsDeleting {
			return ErrConflict
		}
		return fn(w.query)
	})
}

// DeferBackupDelete schedules another deletion attempt.
func (s *Store) DeferBackupDelete(ctx context.Context, slug string, next time.Time, attempts int) error {
	return updateBackupDelete(ctx, s, slug, func(q query) error {
		return q.exec("UPDATE backups_deletes SET next_try = ?, attempts = ? WHERE account = ?", next.Unix(), attempts, slug)
	})
}

// FinishBackupDelete removes the retained backup record after deletion succeeds.
func (s *Store) FinishBackupDelete(ctx context.Context, slug string) error {
	return updateBackupDelete(ctx, s, slug, func(q query) error {
		if err := q.exec("DELETE FROM removed_backups WHERE account = ?", slug); err != nil {
			return err
		}
		return q.exec("DELETE FROM backups_deletes WHERE account = ?", slug)
	})
}

// FailBackupDelete records a failed deletion and its reason.
func (s *Store) FailBackupDelete(ctx context.Context, slug, reason string) error {
	return updateBackupDelete(ctx, s, slug, func(q query) error {
		return q.exec("UPDATE backups_deletes SET state = 'failed', reason = ? WHERE account = ?", reason, slug)
	})
}
