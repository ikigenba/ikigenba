package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

const restoreFields = "id, host, host_seq, account, kind, path, user_name, as_of, copy, state, reason, asked, created_by"

func scanRestore(r scanner) (v Restore, err error) {
	err = r.Scan(&v.ID, &v.Host, &v.Account, &v.Kind, &v.Path, &v.User, storedTime{&v.AsOf}, &v.Copy,
		&v.State, &v.Reason, storedTime{&v.Asked}, &v.CreatedBy)
	return v, err
}

const restoreSelect = `SELECT id, (SELECT name FROM hosts WHERE seq = host_seq) AS host, account, kind, path, user_name,
	as_of, copy, state, reason, asked, created_by FROM restores`

type restoreOp struct {
	restore Restore
	op      Operation
}

func validRestore(r RestoreRequest) bool {
	if r.Account == "" || r.AsOf.IsZero() {
		return false
	}
	switch r.Kind {
	case protocol.RestoreDatabase:
		return r.User == "" && valid.DatabasePath(r.Path) == nil
	case protocol.RestoreFiles:
		return valid.RestorePath(r.Path) == nil
	default:
		return false
	}
}

func checkRestore(q query, r RestoreRequest, stamp time.Time) (Host, error) {
	a, err := getAccount(q, r.Account)
	if err != nil {
		return Host{}, err
	}
	if r.Kind == protocol.RestoreFiles && r.User != "" {
		var account string
		err := q.row("SELECT account FROM ssh_users WHERE name = ?", r.User).Scan(&account)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return Host{}, ErrNoUser
		case err != nil:
			return Host{}, err
		case account != a.Slug:
			return Host{}, ErrNotInAccount
		}
	}
	if a.State != AccountActive {
		return Host{}, ErrAccountNotActive
	}
	h, err := getHost(q, a.Host)
	if err != nil {
		return Host{}, err
	}
	if h.State != HostRunning {
		return Host{}, ErrNotRunning
	}
	report, err := latestReport(q, h.Name)
	if err != nil {
		return Host{}, err
	}
	var entries []protocol.AccountBackup
	if report != nil {
		entries = report.Body.Backups
	}
	if err := checkRestoreTime(r, entries, stamp); err != nil {
		return Host{}, err
	}
	var unfinished int
	err = q.row(`SELECT count(*) FROM (
		SELECT id FROM backup_operations WHERE host = ? AND host_seq = ? AND kind = 'restore' AND status = 'running'
		UNION ALL
		SELECT r.id FROM restores r WHERE r.host = ? AND r.host_seq = ? AND r.state = 'pending'
			AND NOT EXISTS (SELECT 1 FROM backup_operations b WHERE b.id = r.id AND b.status = 'running')
		LIMIT ?)`, h.Name, h.Seq, h.Name, h.Seq, valid.MaxRestoresPerHost).Scan(&unfinished)
	if err == nil && unfinished >= valid.MaxRestoresPerHost {
		return Host{}, ErrTooManyRestores
	}
	return h, err
}

func checkRestoreTime(r RestoreRequest, entries []protocol.AccountBackup, stamp time.Time) error {
	for _, entry := range entries {
		if entry.Account != r.Account {
			continue
		}
		if r.Kind == protocol.RestoreFiles {
			if slices.Contains(entry.Snapshots, r.AsOf.Unix()) {
				return nil
			}
			continue
		}
		for _, d := range entry.Databases {
			if d.Path == r.Path {
				if d.Oldest.IsZero() || d.Oldest.After(r.AsOf) || r.AsOf.After(stamp) {
					return ErrNotRestorable
				}
				return nil
			}
		}
	}
	if r.Kind == protocol.RestoreFiles {
		return ErrNoSnapshot
	}
	return ErrNoDatabase
}

// RequestRestore checks the latest report and records a pending restore operation.
func (s *Store) RequestRestore(ctx context.Context, r RestoreRequest, c identity.Caller) (Restore, Operation, error) {
	out, err := write(ctx, s, validCaller(c) && validRestore(r), func(w *writing) (restoreOp, error) {
		stamp := s.stamp()
		r.AsOf = cleanTime(r.AsOf)
		h, err := checkRestore(w.query, r, stamp)
		if err != nil {
			return restoreOp{}, err
		}
		op, err := s.newOperation(w.query, Operation{Host: h.Name, Account: r.Account, Kind: OpRestore, Caller: c}, stamp)
		if err != nil {
			return restoreOp{}, err
		}
		restore := Restore{ID: op.ID, Host: h.Name, Account: r.Account, Kind: r.Kind, Path: r.Path, User: r.User,
			AsOf: r.AsOf, State: protocol.StepPending, Asked: stamp, CreatedBy: c.UserID}
		if err := w.exec("INSERT INTO restores ("+restoreFields+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, '', ?, ?)",
			op.ID, h.Name, h.Seq, r.Account, r.Kind, r.Path, r.User, r.AsOf.Unix(),
			protocol.StepPending, stamp.Unix(), c.UserID); err != nil {
			return restoreOp{}, err
		}
		_, err = w.bump(h.Name)
		return restoreOp{restore, op}, err
	})
	return out.restore, out.op, err
}

func (s *recordReader) Restore(ctx context.Context, id string) (Restore, error) {
	return read(ctx, s, id != "", func(q query) (Restore, error) { return scanRestore(q.row(restoreSelect+" WHERE id = ?", id)) })
}

type restorePage struct {
	rows  []Restore
	total int
}

func (s *recordReader) RestoresOn(ctx context.Context, host string, limit int) ([]Restore, int, error) {
	out, err := read(ctx, s, host != "" && validLimit(limit), func(q query) (restorePage, error) {
		h, err := liveHost(q, host)
		if err != nil {
			return restorePage{}, err
		}
		var total int
		if err := q.row("SELECT count(*) FROM restores WHERE host = ? AND host_seq = ?", host, h.Seq).Scan(&total); err != nil {
			return restorePage{}, err
		}
		rows, err := collect(q, scanRestore, restoreSelect+" WHERE host = ? AND host_seq = ? ORDER BY asked DESC, id DESC LIMIT ?",
			host, h.Seq, limit)
		return restorePage{rows, total}, err
	})
	return out.rows, out.total, err
}

func pendingRestores(q query, h Host) ([]protocol.RestoreRequest, error) {
	return collect(q, func(r scanner) (v protocol.RestoreRequest, err error) {
		err = r.Scan(&v.ID, &v.Account, &v.Kind, &v.Path, &v.User, storedTime{&v.AsOf})
		return v, err
	}, `SELECT id, account, kind, path, user_name, as_of FROM restores
		WHERE host = ? AND host_seq = ? AND state = 'pending' ORDER BY id LIMIT ?`, h.Name, h.Seq, valid.MaxRestoresPerHost)
}

func cancelRestores(q query, seq int64, account string, stamp time.Time) ([]string, error) {
	predicate := " WHERE host_seq = ?"
	args := []any{seq}
	if account != "" {
		predicate += " AND account = ?"
		args = append(args, account)
	}
	// Keep the result bounded even if a database predates the unfinished-restore cap.
	ids, err := collect(q, scanString, "SELECT id FROM backup_operations"+
		predicate+" AND kind = 'restore' AND status = 'running' ORDER BY id LIMIT ?",
		append(args, valid.MaxRestoresPerHost+1)...)
	if err != nil {
		return nil, err
	}
	if len(ids) > valid.MaxRestoresPerHost {
		return nil, ErrTooManyRestores
	}
	if err := q.exec("UPDATE backup_operations SET status = 'cancelled', finished = ?"+
		predicate+" AND kind = 'restore' AND status = 'running'", append([]any{stamp.Unix()}, args...)...); err != nil {
		return nil, err
	}
	if err := q.exec("UPDATE restores SET state = 'failed', reason = ''"+predicate+" AND state = 'pending'", args...); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}

func recordRestoreResults(q query, h Host, results []protocol.RestoreResult) error {
	for _, r := range results {
		if r.State != protocol.StepDone && r.State != protocol.StepFailed {
			continue
		}
		copyPath := r.Copy
		if r.State != protocol.StepDone {
			copyPath = ""
		}
		if err := q.exec(`UPDATE restores SET state = ?, copy = ?, reason = ?
			WHERE id = ? AND host_seq = ? AND state = 'pending'`,
			r.State, copyPath, boundedBackupError(r.Error), r.ID, h.Seq); err != nil {
			return err
		}
	}
	return nil
}

type restorePath struct {
	account, path string
}

func compareRestorePaths(a, b restorePath) int {
	return cmp.Or(cmp.Compare(a.account, b.account), cmp.Compare(a.path, b.path))
}

func eligibleRestorePaths(q query, h Host, entries []protocol.AccountBackup) ([]restorePath, error) {
	// Keep only the first distinct account and path pairs, even when a report
	// lists one account in several entries.
	paths := make([]restorePath, 0, protocol.MaxDatabases)
	for _, a := range entries {
		var eligible bool
		if err := q.row("SELECT EXISTS (SELECT 1 FROM accounts WHERE slug = ? AND host = ? AND state <> 'removing')",
			a.Account, h.Name).Scan(&eligible); err != nil {
			return nil, err
		}
		if !eligible {
			continue
		}
		for _, d := range a.Databases {
			path := restorePath{account: a.Account, path: d.Path}
			i, duplicate := slices.BinarySearchFunc(paths, path, compareRestorePaths)
			if duplicate || i == protocol.MaxDatabases {
				continue
			}
			if len(paths) < protocol.MaxDatabases {
				paths = append(paths, restorePath{})
			}
			copy(paths[i+1:], paths[i:])
			paths[i] = path
		}
	}
	return paths, nil
}

func copyRestorePaths(q query, h Host, opID string) error {
	report, err := latestReport(q, h.Name)
	if report == nil || err != nil {
		return err
	}
	paths, err := eligibleRestorePaths(q, h, report.Body.Backups)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := q.exec("INSERT INTO restore_databases (operation_id, account, path) VALUES (?, ?, ?)",
			opID, path.account, path.path); err != nil {
			return err
		}
	}
	return nil
}
