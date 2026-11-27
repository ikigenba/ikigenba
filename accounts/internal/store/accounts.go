package store

import (
	"context"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

const accountFields = `slug, host, state, reason, gid, version, removed_version, change_id, operation, created, created_by`
const accountSelect = `SELECT ` + accountFields +
	`, (SELECT count(*) FROM ssh_users WHERE account = accounts.slug) FROM accounts`

const accountSelectWithInvites = `SELECT ` + accountFields +
	`, (SELECT count(*) FROM ssh_users WHERE account = accounts.slug),
	(SELECT count(*) FROM invites WHERE account = accounts.slug AND gid = accounts.gid AND expires > ?)
	FROM accounts`

func scanAccount(r scanner, withInvites bool) (a Account, err error) {
	fields := []any{&a.Slug, &a.Host, &a.State, &a.Reason, &a.GID, &a.Version, &a.RemovedVersion,
		&a.Change, &a.Operation, storedTime{&a.Created}, &a.CreatedBy, &a.Users}
	if withInvites {
		fields = append(fields, &a.Invites)
	}
	err = r.Scan(fields...)
	return a, err
}

func getAccount(q query, slug string) (Account, error) {
	return scanAccount(q.row(accountSelect+" WHERE slug = ?", slug), false)
}

func hostAccounts(q query, host string) ([]Account, error) {
	return collect(q, func(r scanner) (Account, error) { return scanAccount(r, false) }, accountSelect+" WHERE host = ? ORDER BY slug", host)
}

func collectAccounts(q query, stamp time.Time, stmt string, args ...any) ([]Account, error) {
	return collect(q, func(r scanner) (Account, error) { return scanAccount(r, true) },
		accountSelectWithInvites+stmt, append([]any{stamp.Unix()}, args...)...)
}

// NameHeld reports whether a user or account currently holds the name.
func (s *Store) NameHeld(ctx context.Context, name string) (bool, error) {
	return read(ctx, &s.recordReader, name != "", func(q query) (held bool, err error) {
		err = q.row("SELECT EXISTS (SELECT 1 FROM names WHERE name = ?)", name).Scan(&held)
		return held, err
	})
}

func claimName(q query, name string) error {
	r, err := q.ExecContext(q.Context, "INSERT INTO names (name) VALUES (?) ON CONFLICT (name) DO NOTHING", name)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return ErrTaken
	}
	return err
}

func purgeAccount(q query, slug string, stamp time.Time) error {
	if err := moveRemovedBackup(q, slug, stamp); err != nil {
		return err
	}
	if err := deleteAccountInvites(q, slug); err != nil {
		return err
	}
	if err := q.exec("DELETE FROM accounts WHERE slug = ?", slug); err != nil {
		return err
	}
	return q.exec("DELETE FROM names WHERE name = ?", slug)
}

type accountOp struct {
	account   Account
	op        Operation
	cancelled []string
}

// PlaceAccount adds a business to one host and starts its placement.
func (s *Store) PlaceAccount(ctx context.Context, slug, host string, c identity.Caller) (Account, Operation, error) {
	out, err := write(ctx, s, allGiven(slug, host) && validCaller(c), func(w *writing) (accountOp, error) {
		h, err := getHost(w.query, host)
		if err != nil {
			return accountOp{}, err
		}
		if h.State == HostDeleting || h.State == HostDeleted {
			return accountOp{}, ErrHostGone
		}
		if err := claimName(w.query, slug); err != nil {
			return accountOp{}, err
		}
		if h.Accounts >= valid.MaxAccountsPerHost {
			return accountOp{}, ErrHostFull
		}
		var deleting bool
		if err := w.row("SELECT EXISTS (SELECT 1 FROM backups_deletes WHERE account = ? AND state = 'deleting')",
			slug).Scan(&deleting); err != nil {
			return accountOp{}, err
		}
		if deleting {
			return accountOp{}, ErrBackupsDeleting
		}
		stamp := s.stamp()
		gid, err := allocate(w.query, "uid_gid", FirstID)
		if err != nil {
			return accountOp{}, err
		}
		version, err := w.bump(host)
		if err != nil {
			return accountOp{}, err
		}
		op, err := s.newOperation(w.query, Operation{Host: host, Account: slug, Kind: OpPlace, Caller: c}, stamp)
		if err != nil {
			return accountOp{}, err
		}
		a := Account{Slug: slug, Host: host, State: AccountPending, GID: gid, Version: version, Operation: op.ID,
			Created: stamp, CreatedBy: c.UserID}
		return accountOp{account: a, op: op}, w.exec("INSERT INTO accounts ("+accountFields+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			a.Slug, a.Host, a.State, a.Reason, a.GID, a.Version, a.RemovedVersion, a.Change, a.Operation, a.Created.Unix(), a.CreatedBy)
	})
	return out.account, out.op, err
}

func beginRemoval(w *writing, a *Account, stamp time.Time) (string, error) {
	if a.State == AccountRemoving {
		var busy bool
		err := w.row("SELECT EXISTS (SELECT 1 FROM operations WHERE account = ? AND kind = ? AND status = ?)",
			a.Slug, OpUnplace, OpRunning).Scan(&busy)
		if err == nil && busy {
			return "", ErrAccountRemoving
		}
		return "", err
	}
	var supersedes string
	place, err := getOperation(w.query, a.Operation)
	if err != nil {
		return "", err
	}
	if place.Status == OpRunning {
		supersedes = place.ID
	}
	if err := w.exec(`UPDATE operations SET status = ?, finished = ?
		WHERE account = ? AND kind = ? AND status = ?`, OpCancelled, stamp.Unix(), a.Slug, OpPlace, OpRunning); err != nil {
		return "", err
	}
	version, err := w.bump(a.Host)
	if err != nil {
		return "", err
	}
	a.State, a.Reason, a.RemovedVersion = AccountRemoving, "", version
	return supersedes, nil
}

// RemoveAccount starts removal or starts a new operation after a failed removal.
func (s *Store) RemoveAccount(ctx context.Context, slug string, c identity.Caller) (Account, Operation, []string, error) {
	out, err := write(ctx, s, slug != "" && validCaller(c), func(w *writing) (accountOp, error) {
		a, err := getAccount(w.query, slug)
		if err != nil {
			return accountOp{}, err
		}
		if a.Users != 0 {
			return accountOp{}, ErrHasUsers
		}
		previous := a.State
		stamp := s.stamp()
		supersedes, err := beginRemoval(w, &a, stamp)
		if err != nil {
			return accountOp{}, err
		}
		op, err := s.newOperation(w.query, Operation{Host: a.Host, Account: slug, Kind: OpUnplace,
			ResumeState: previous, Supersedes: supersedes, Caller: c}, stamp)
		if err != nil {
			return accountOp{}, err
		}
		if err := deleteAccountInvites(w.query, a.Slug); err != nil {
			return accountOp{}, err
		}
		a.Operation = op.ID
		var seq int64
		if err := w.row("SELECT seq FROM hosts WHERE name = ?", a.Host).Scan(&seq); err != nil {
			return accountOp{}, err
		}
		cancelled, err := cancelRestores(w.query, seq, slug, stamp)
		if err != nil {
			return accountOp{}, err
		}
		return accountOp{account: a, op: op, cancelled: cancelled}, w.exec("UPDATE accounts SET state = ?, reason = ?, removed_version = ?, operation = ? WHERE slug = ?",
			a.State, a.Reason, a.RemovedVersion, a.Operation, a.Slug)
	})
	return out.account, out.op, out.cancelled, err
}

// Account reads a business by slug.
func (s *recordReader) Account(ctx context.Context, slug string) (Account, error) {
	return read(ctx, s, true, func(q query) (Account, error) {
		return scanAccount(q.row(accountSelectWithInvites+" WHERE slug = ?", cleanTime(s.now()).Unix(), slug), true)
	})
}

func (s *recordReader) accountPage(ctx context.Context, limit int, reverse bool, stmt string, args ...any) ([]Account, error) {
	return read(ctx, s, validLimit(limit), func(q query) ([]Account, error) {
		out, err := collectAccounts(q, cleanTime(s.now()), stmt, append(args, limit)...)
		if reverse {
			slices.Reverse(out)
		}
		return out, err
	})
}

// ListAccounts reads the page after a slug cursor.
func (s *recordReader) ListAccounts(ctx context.Context, after string, limit int) ([]Account, error) {
	return s.accountPage(ctx, limit, false, " WHERE slug > ? ORDER BY slug LIMIT ?", after)
}

// ListAccountsBefore reads the page before a slug cursor.
func (s *recordReader) ListAccountsBefore(ctx context.Context, before string, limit int) ([]Account, error) {
	return s.accountPage(ctx, limit, true, " WHERE slug < ? ORDER BY slug DESC LIMIT ?", before)
}

// LastAccounts reads the final page in ascending slug order.
func (s *recordReader) LastAccounts(ctx context.Context, limit int) ([]Account, error) {
	return s.accountPage(ctx, limit, true, " ORDER BY slug DESC LIMIT ?")
}

// AccountsOn reads all accounts of a live host in slug order.
func (s *recordReader) AccountsOn(ctx context.Context, host string) ([]Account, error) {
	return read(ctx, s, host != "", func(q query) ([]Account, error) {
		if _, err := liveHost(q, host); err != nil {
			return nil, err
		}
		return collectAccounts(q, cleanTime(s.now()), " WHERE host = ? ORDER BY slug", host)
	})
}
