package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Desired builds the host's versioned reconciliation document.
func (s *recordReader) Desired(ctx context.Context, host string) (protocol.DesiredState, error) {
	return read(ctx, s, host != "", func(q query) (protocol.DesiredState, error) {
		h, err := liveHost(q, host)
		if err != nil {
			return protocol.DesiredState{}, err
		}
		unban, err := collect(q, scanString, "SELECT address FROM host_unbans WHERE host = ? ORDER BY address", host)
		if err != nil {
			return protocol.DesiredState{}, err
		}
		d := protocol.DesiredState{Protocol: protocol.Version, Host: h.Name, Version: h.Version,
			DataVolume: h.DataVolume, RootGB: h.RootGB, DataGB: h.DataGB,
			Allowlist: h.Allowlist, Unban: unban, Accounts: []protocol.Account{}, Users: []protocol.User{}}
		d.Backup = &protocol.Backup{HostPrefix: protocol.HostPrefix(h.Name, h.Seq)}
		d.Backup.Restores, err = pendingRestores(q, h)
		if err != nil {
			return protocol.DesiredState{}, err
		}
		current, err := getOperation(q, h.Operation)
		if err != nil {
			return protocol.DesiredState{}, err
		}
		if current.Kind == OpClose && current.Status == OpRunning {
			d.Backup.Close = current.ID
		}
		withheld, restoring, err := replacementState(q, h)
		if err != nil {
			return protocol.DesiredState{}, err
		}
		if withheld {
			if restoring {
				d.Backup.Restore, err = desiredHostRestore(q, h, current.ID)
			}
			return d, err
		}
		if err := desiredAccountsAndUsers(q, h, &d); err != nil {
			return protocol.DesiredState{}, err
		}
		return d, nil
	})
}

func desiredAccountsAndUsers(q query, h Host, d *protocol.DesiredState) error {
	accounts, err := hostAccounts(q, h.Name)
	if err != nil {
		return err
	}
	users, err := usersWithKeys(q, userSelect+" WHERE a.host = ? ORDER BY u.name", h.Name)
	if err != nil {
		return err
	}
	locked, err := lockedOver(q, h)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		state := protocol.AccountActive
		if a.State == AccountRemoving {
			state = protocol.AccountRemoved
		}
		d.Accounts = append(d.Accounts, protocol.Account{Slug: a.Slug, GID: a.GID, State: state})
	}
	for _, u := range users {
		state := u.State
		switch {
		case u.State == UserRemoving:
			state = protocol.UserDeleted
		case locked && u.State == UserActive:
			state = protocol.UserLocked
		}
		keys := make([]string, 0, len(u.Keys))
		for _, k := range u.Keys {
			keys = append(keys, k.Line)
		}
		d.Users = append(d.Users, protocol.User{Name: u.Name, Account: u.Account, UID: u.UID, State: state, Keys: keys})
	}
	return nil
}

func desiredHostRestore(q query, h Host, opID string) (*protocol.HostRestore, error) {
	accounts, err := hostAccounts(q, h.Name)
	if err != nil {
		return nil, err
	}
	r := &protocol.HostRestore{ID: opID, Accounts: make([]protocol.RestoreAccount, 0, len(accounts))}
	for _, a := range accounts {
		if a.State == AccountRemoving {
			continue
		}
		paths, err := collect(q, scanString, "SELECT path FROM restore_databases WHERE operation_id = ? AND account = ? ORDER BY path",
			opID, a.Slug)
		if err != nil {
			return nil, err
		}
		r.Accounts = append(r.Accounts, protocol.RestoreAccount{Slug: a.Slug, GID: a.GID, Databases: paths})
	}
	return r, nil
}

func reportSignIns(q query, h Host, entries []protocol.SignIn, now time.Time) ([]protocol.SignIn, error) {
	var out []protocol.SignIn
	if entries != nil {
		out = make([]protocol.SignIn, 0, len(entries))
	}
	for _, entry := range entries {
		at := entry.At
		if at.After(now) {
			at = now
		}
		var name string
		err := q.row(`UPDATE ssh_users SET last_sign_in = max(last_sign_in, ?) WHERE name = ?
			AND account IN (SELECT slug FROM accounts WHERE host = ?) RETURNING name`, at.Unix(), entry.User, h.Name).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func reportSlices(q query, host string, entries []protocol.Slice) ([]protocol.Slice, error) {
	var out []protocol.Slice
	if entries != nil {
		out = make([]protocol.Slice, 0, len(entries))
	}
	for _, entry := range entries {
		var held bool
		if err := q.row("SELECT EXISTS (SELECT 1 FROM accounts WHERE slug = ? AND host = ?)", entry.Account, host).Scan(&held); err != nil {
			return nil, err
		}
		if held {
			out = append(out, entry)
		}
	}
	return out, nil
}

func purgeUsers(q query, host string, applied int64) error {
	if err := q.exec(`DELETE FROM user_admins WHERE uid IN (SELECT uid FROM ssh_users WHERE state = ?
		AND version <= ? AND account IN (SELECT slug FROM accounts WHERE host = ?))`, UserRemoving, applied, host); err != nil {
		return err
	}
	names, err := collect(q, func(r scanner) (name string, err error) {
		err = r.Scan(&name)
		return name, err
	}, `DELETE FROM ssh_users WHERE state = ? AND version <= ? AND account IN
		(SELECT slug FROM accounts WHERE host = ?) RETURNING name`, UserRemoving, applied, host)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := q.exec("DELETE FROM names WHERE name = ?", name); err != nil {
			return err
		}
	}
	return nil
}

func foldReport(h *Host, r protocol.Report, now time.Time) Transition {
	tr := Transition{Before: Health(*h, now)}
	h.Reported, h.ReportError, h.Attempted = cleanTime(now), r.Error != "", r.Attempted
	if h.ReportError {
		tr.ApplyFailed = h.Alarm != AlarmError
		h.Alarm = AlarmError
	} else {
		h.Applied = max(h.Applied, min(r.Applied, h.Version))
		if h.Applied == h.Version && (h.Alarm == AlarmSilent || h.Alarm == AlarmError) {
			tr.Synced = true
			h.Alarm = AlarmNone
		}
	}
	tr.After, tr.Applied, tr.Attempted = Health(*h, now), h.Applied, h.Attempted
	return tr
}

func recordBackupReport(q query, h Host, r *protocol.Report) error {
	op, err := getOperation(q, h.Operation)
	if err != nil {
		return err
	}
	closeRunning := op.Kind == OpClose && op.Status == OpRunning
	replaceRunning := op.Kind == OpReplace && op.Status == OpRunning
	if closeRunning || replaceRunning {
		step := r.HostBackup.Final
		if replaceRunning {
			step = r.HostBackup.Restored
		}
		if err := settleBackupStep(q, op.ID, "", step, r.HostBackup.Error); err != nil {
			return err
		}
	}
	var kept []protocol.AccountBackup
	if r.Backups != nil {
		kept = make([]protocol.AccountBackup, 0, min(len(r.Backups), protocol.MaxBackupAccounts))
	}
	for _, entry := range r.Backups {
		account, err := getAccount(q, entry.Account)
		if errors.Is(err, sql.ErrNoRows) || err == nil && account.Host != h.Name {
			continue
		}
		if err != nil {
			return err
		}
		kept = append(kept, entry)
		if err := writeAccountBackup(q, account, entry, h.Reported); err != nil {
			return err
		}
		if account.State == AccountRemoving || !closeRunning && !replaceRunning {
			continue
		}
		step := entry.Final
		if replaceRunning {
			step = entry.Restored
		}
		if err := settleBackupStep(q, op.ID, account.Slug, step, entry.Error); err != nil {
			return err
		}
	}
	r.Backups = kept
	return recordRestoreResults(q, h, r.Restores)
}

// RecordReport replaces the host's report and purges applied user removals and unbans.
func (s *Store) RecordReport(ctx context.Context, host string, r protocol.Report, now time.Time) (Transition, error) {
	valid := host != "" && len(r.SignIns) <= protocol.MaxSignIns && len(r.Slices) <= protocol.MaxSlices
	return write(ctx, s, valid, func(w *writing) (Transition, error) {
		h, err := liveHost(w.query, host)
		if err != nil {
			return Transition{}, err
		}
		tr := foldReport(&h, r, now)
		r.SignIns, err = reportSignIns(w.query, h, r.SignIns, now)
		if err != nil {
			return Transition{}, err
		}
		r.Slices, err = reportSlices(w.query, host, r.Slices)
		if err != nil {
			return Transition{}, err
		}
		if err := recordBackupReport(w.query, h, &r); err != nil {
			return Transition{}, err
		}
		body, err := json.Marshal(r)
		if err != nil {
			return Transition{}, err
		}
		if len(body) > protocol.MaxReportBytes {
			return Transition{}, ErrUnreachable
		}
		if err := w.exec(`INSERT INTO reports (host, received, body) VALUES (?, ?, ?)
			ON CONFLICT (host) DO UPDATE SET received = excluded.received, body = excluded.body`, host, h.Reported.Unix(), body); err != nil {
			return Transition{}, err
		}
		if err := w.exec("UPDATE hosts SET reported = ?, report_error = ?, attempted = ?, applied = ?, alarm = ? WHERE name = ?",
			h.Reported.Unix(), h.ReportError, h.Attempted, h.Applied, h.Alarm, host); err != nil {
			return Transition{}, err
		}
		if err := purgeUsers(w.query, host, h.Applied); err != nil {
			return Transition{}, err
		}
		return tr, w.exec("DELETE FROM host_unbans WHERE host = ? AND version <= ?", host, h.Applied)
	})
}

// Report reads the last report of a live host, if one has arrived.
func (s *recordReader) Report(ctx context.Context, host string) (*Report, error) {
	return read(ctx, s, host != "", func(q query) (*Report, error) {
		if _, err := liveHost(q, host); err != nil {
			return nil, err
		}
		return latestReport(q, host)
	})
}

func latestReport(q query, host string) (*Report, error) {
	r := Report{Host: host}
	var body []byte
	err := q.row("SELECT received, body FROM reports WHERE host = ?", host).Scan(storedTime{&r.Received}, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, json.Unmarshal(body, &r.Body)
}

// Health classifies the host's last reconciliation result at now.
func Health(h Host, now time.Time) string {
	last := h.Reported
	if h.RunningSince.After(last) {
		last = h.RunningSince
	}
	switch {
	case h.State == HostRunning && last.Before(now.Add(-SilentAfter)):
		return HealthSilent
	case h.Reported.IsZero():
		return HealthNone
	case h.ReportError:
		return HealthError
	case h.Applied < h.Version:
		return HealthPending
	default:
		return HealthSync
	}
}

// MarkSilent records each newly silent running host once per alarm episode.
func (s *Store) MarkSilent(ctx context.Context, now time.Time, after string, limit int) ([]Host, error) {
	return write(ctx, s, validLimit(limit), func(w *writing) ([]Host, error) {
		cutoff := now.Add(-SilentAfter)
		// Stored times have whole-second precision. A fractional cutoff
		// includes its own second, while an exact cutoff excludes it.
		second := cutoff.Unix()
		if cutoff.Nanosecond() != 0 {
			second++
		}
		hs, err := collectHosts(w.query, ` WHERE name > ? AND state = ? AND alarm <> ?
			AND reported < ? AND running_since < ? ORDER BY name LIMIT ?`, after, HostRunning, AlarmSilent, second, second, limit)
		if err != nil {
			return nil, err
		}
		for i := range hs {
			if err := w.exec("UPDATE hosts SET alarm = ? WHERE name = ?", AlarmSilent, hs[i].Name); err != nil {
				return nil, err
			}
			hs[i].Alarm = AlarmSilent
		}
		return hs, nil
	})
}
