package store

import (
	"context"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

const userFields = `u.name, u.account, u.state, u.uid, u.version, u.last_sign_in, u.created, u.created_by`
const userSelect = `SELECT ` + userFields + `, u.version > h.applied, EXISTS (SELECT 1 FROM user_admins WHERE uid = u.uid)
	FROM ssh_users u JOIN accounts a ON a.slug = u.account JOIN hosts h ON h.name = a.host`

func scanUser(r scanner) (u User, err error) {
	err = r.Scan(&u.Name, &u.Account, &u.State, &u.UID, &u.Version, storedTime{&u.LastSignIn},
		storedTime{&u.Created}, &u.CreatedBy, &u.Pending, &u.Admin)
	return u, err
}

func scanKey(r scanner) (k Key, err error) {
	err = r.Scan(&k.Type, &k.Body, &k.Line)
	return k, err
}

func loadKeys(q query, u *User) (err error) {
	u.Keys, err = collect(q, scanKey, "SELECT key_type, body, line FROM ssh_keys WHERE name = ? ORDER BY line", u.Name)
	return err
}

func usersWithKeys(q query, stmt string, args ...any) ([]User, error) {
	out, err := collect(q, scanUser, stmt, args...)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := loadKeys(q, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func getUser(q query, account, name string) (User, error) {
	u, err := scanUser(q.row(userSelect+" WHERE u.name = ?", name))
	if err != nil {
		return User{}, err
	}
	if u.Account != account {
		return User{}, ErrNotInAccount
	}
	err = loadKeys(q, &u)
	return u, err
}

func insertKey(q query, name string, k Key) error {
	return q.exec("INSERT INTO ssh_keys (name, key_type, body, line) VALUES (?, ?, ?, ?)", name, k.Type, k.Body, k.Line)
}

// AddUser creates a user and its first key on the account's host.
func (s *Store) AddUser(ctx context.Context, account, name string, k Key, c identity.Caller) (User, error) {
	return write(ctx, s, allGiven(account, name, k.Type, k.Body, k.Line) && validCaller(c), func(w *writing) (User, error) {
		a, err := getAccount(w.query, account)
		if err != nil {
			return User{}, err
		}
		if a.State == AccountRemoving {
			return User{}, ErrAccountRemoving
		}
		return addUser(w, a, name, k, false, c.UserID, s.stamp())
	})
}

func addUser(w *writing, a Account, name string, k Key, admin bool, createdBy string, stamp time.Time) (User, error) {
	if err := claimName(w.query, name); err != nil {
		return User{}, err
	}
	var count int
	if err := w.row(`SELECT count(*) FROM accounts a JOIN ssh_users u ON u.account = a.slug WHERE a.host = ?`, a.Host).Scan(&count); err != nil {
		return User{}, err
	}
	if count >= valid.MaxUsersPerHost {
		return User{}, ErrAccountFull
	}
	uid, err := allocate(w.query, "uid_gid", FirstID)
	if err != nil {
		return User{}, err
	}
	version, err := w.bump(a.Host)
	if err != nil {
		return User{}, err
	}
	u := User{Name: name, Account: a.Slug, Admin: admin, State: UserActive, UID: uid, Version: version, Pending: true,
		Keys: []Key{k}, Created: stamp, CreatedBy: createdBy}
	if err := w.exec(`INSERT INTO ssh_users (name, account, state, uid, version, last_sign_in, created, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, name, a.Slug, u.State, uid, version, u.LastSignIn.Unix(), u.Created.Unix(), u.CreatedBy); err != nil {
		return User{}, err
	}
	if admin {
		if err := w.exec("INSERT INTO user_admins (uid) VALUES (?)", uid); err != nil {
			return User{}, err
		}
	}
	return u, insertKey(w.query, name, k)
}

// User reads a single user and verifies its account.
func (s *recordReader) User(ctx context.Context, account, name string) (User, error) {
	return read(ctx, s, true, func(q query) (User, error) { return getUser(q, account, name) })
}

type userChange func(*writing, *User) (bool, error)

func modifyUser(ctx context.Context, s *Store, account, name string, valid bool, fn userChange) (User, error) {
	return write(ctx, s, valid && allGiven(account, name), func(w *writing) (User, error) {
		u, err := getUser(w.query, account, name)
		if err != nil {
			return User{}, err
		}
		return applyUserChange(w, u, fn)
	})
}

func applyUserChange(w *writing, u User, fn userChange) (User, error) {
	changed, err := fn(w, &u)
	if err != nil || !changed {
		return u, err
	}
	var host string
	if err := w.row("SELECT host FROM accounts WHERE slug = ?", u.Account).Scan(&host); err != nil {
		return User{}, err
	}
	u.Version, err = w.bump(host)
	if err != nil {
		return User{}, err
	}
	u.Pending = true
	if err := w.exec("UPDATE ssh_users SET state = ?, version = ? WHERE name = ?", u.State, u.Version, u.Name); err != nil {
		return User{}, err
	}
	return u, nil
}

func keyMatch(keys []Key, k Key) int {
	return slices.IndexFunc(keys, func(held Key) bool { return held.Type == k.Type && held.Body == k.Body })
}

func addKey(k Key) userChange {
	return func(w *writing, u *User) (bool, error) {
		switch {
		case u.State == UserRemoving:
			return false, ErrUserRemoving
		case keyMatch(u.Keys, k) >= 0:
			return false, nil
		case len(u.Keys) >= valid.MaxKeysPerUser:
			return false, ErrTooManyKeys
		}
		if err := insertKey(w.query, u.Name, k); err != nil {
			return false, err
		}
		return true, loadKeys(w.query, u)
	}
}

// AddKey keeps an existing line for an already held key.
func (s *Store) AddKey(ctx context.Context, account, name string, k Key) (User, error) {
	return modifyUser(ctx, s, account, name, allGiven(k.Type, k.Body, k.Line), addKey(k))
}

func removeKey(k Key, keepLast bool) userChange {
	return func(w *writing, u *User) (bool, error) {
		switch {
		case u.State == UserRemoving:
			return false, ErrUserRemoving
		case keyMatch(u.Keys, k) < 0:
			return false, ErrKeyNotHeld
		case keepLast && len(u.Keys) == 1:
			return false, ErrLastKey
		}
		if err := w.exec("DELETE FROM ssh_keys WHERE name = ? AND key_type = ? AND body = ?", u.Name, k.Type, k.Body); err != nil {
			return false, err
		}
		return true, loadKeys(w.query, u)
	}
}

// RemoveKey deletes a held key by type and body, and returns the remaining count.
func (s *Store) RemoveKey(ctx context.Context, account, name string, k Key) (User, int, error) {
	u, err := modifyUser(ctx, s, account, name, allGiven(k.Type, k.Body), removeKey(k, false))
	return u, len(u.Keys), err
}

func changeUserState(state string) userChange {
	return func(_ *writing, u *User) (bool, error) {
		switch {
		case u.State == state:
			return false, nil
		case u.State == UserRemoving:
			return false, ErrUserRemoving
		case state == UserRemoving && u.State != UserLocked:
			return false, ErrNotLocked
		}
		u.State = state
		return true, nil
	}
}

// Lock disables a user's sign-in.
func (s *Store) Lock(ctx context.Context, account, name string) (User, error) {
	return modifyUser(ctx, s, account, name, true, changeUserState(UserLocked))
}

// Unlock restores a locked user's sign-in.
func (s *Store) Unlock(ctx context.Context, account, name string) (User, error) {
	return modifyUser(ctx, s, account, name, true, changeUserState(UserActive))
}

// Delete requests removal of a locked user.
func (s *Store) Delete(ctx context.Context, account, name string) (User, error) {
	return modifyUser(ctx, s, account, name, true, changeUserState(UserRemoving))
}

func (s *recordReader) userPage(ctx context.Context, account string, limit int, reverse bool, cursor string, args ...any) ([]User, error) {
	queryText := userSelect + " WHERE TRUE"
	if account != "" {
		queryText += " AND u.account = ?"
		args = append([]any{account}, args...)
	}
	queryText += cursor
	order := " ORDER BY u.account, u.name LIMIT ?"
	if reverse {
		order = " ORDER BY u.account DESC, u.name DESC LIMIT ?"
	}
	return read(ctx, s, validLimit(limit), func(q query) ([]User, error) {
		out, err := usersWithKeys(q, queryText+order, append(args, limit)...)
		if reverse {
			slices.Reverse(out)
		}
		return out, err
	})
}

// ListUsers reads the page after the account and name cursor.
func (s *recordReader) ListUsers(ctx context.Context, account, afterAccount, afterName string, limit int) ([]User, error) {
	return s.userPage(ctx, account, limit, false, " AND (u.account, u.name) > (?, ?)", afterAccount, afterName)
}

// ListUsersBefore reads the page before the account and name cursor.
func (s *recordReader) ListUsersBefore(ctx context.Context, account, beforeAccount, beforeName string, limit int) ([]User, error) {
	return s.userPage(ctx, account, limit, true, " AND (u.account, u.name) < (?, ?)", beforeAccount, beforeName)
}

// LastUsers reads the final page in ascending account and name order.
func (s *recordReader) LastUsers(ctx context.Context, account string, limit int) ([]User, error) {
	return s.userPage(ctx, account, limit, true, "")
}

// UsersOf reads all the users of an account, in name order.
func (s *recordReader) UsersOf(ctx context.Context, account string) ([]User, error) {
	return read(ctx, s, account != "", func(q query) ([]User, error) {
		if _, err := getAccount(q, account); err != nil {
			return nil, err
		}
		return usersWithKeys(q, userSelect+" WHERE u.account = ? ORDER BY u.name", account)
	})
}
