package store

import (
	"context"
	"database/sql"
	"errors"
)

type adminResult struct {
	user    User
	changed bool
}

func setAdmin(q query, u User, admin bool) (adminResult, error) {
	if u.State == UserRemoving {
		return adminResult{}, ErrUserRemoving
	}
	if u.Admin == admin {
		return adminResult{user: u}, nil
	}
	var err error
	if admin {
		err = q.exec("INSERT INTO user_admins (uid) VALUES (?)", u.UID)
	} else {
		err = q.exec("DELETE FROM user_admins WHERE uid = ?", u.UID)
	}
	u.Admin = admin
	return adminResult{u, true}, err
}

// SetAdmin changes a user's admin status without changing desired state.
func (s *Store) SetAdmin(ctx context.Context, account, name string, admin bool) (User, bool, error) {
	out, err := write(ctx, s, allGiven(account, name), func(w *writing) (adminResult, error) {
		u, err := getUser(w.query, account, name)
		if err != nil {
			return adminResult{}, err
		}
		return setAdmin(w.query, u, admin)
	})
	return out.user, out.changed, err
}

// UserNamed reads a user from the shared namespace.
func (s *Store) UserNamed(ctx context.Context, name string) (User, error) {
	return read(ctx, &s.recordReader, name != "", func(q query) (User, error) {
		u, err := scanUser(q.row(userSelect+" WHERE u.name = ?", name))
		if err != nil {
			return User{}, err
		}
		err = loadKeys(q, &u)
		return u, err
	})
}

const activeUser = " WHERE u.state = ? AND a.state <> ?"

// ActiveUserByUID resolves an active user on the authenticated host.
func (s *Store) ActiveUserByUID(ctx context.Context, host string, uid int64) (User, error) {
	return read(ctx, &s.recordReader, host != "", func(q query) (User, error) {
		u, err := scanUser(q.row(userSelect+activeUser+" AND h.name = ? AND u.uid = ?", UserActive, AccountRemoving, host, uid))
		if err != nil {
			return User{}, err
		}
		err = loadKeys(q, &u)
		return u, err
	})
}

func readActor(q query, name string, admin bool) (User, error) {
	u, err := scanUser(q.row(userSelect+activeUser+" AND u.name = ?", UserActive, AccountRemoving, name))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	if admin && (err != nil || !u.Admin) {
		return User{}, ErrNotAdmin
	}
	if err != nil {
		return User{}, ErrNotActive
	}
	return u, nil
}

// AdminCheck determines whether the actor is an active admin.
func (s *Store) AdminCheck(ctx context.Context, actor string) error {
	_, err := read(ctx, &s.recordReader, actor != "", func(q query) (User, error) {
		return readActor(q, actor, true)
	})
	return err
}

// UsersAs lists the users of the active admin's account.
func (s *Store) UsersAs(ctx context.Context, actor string) ([]User, error) {
	return read(ctx, &s.recordReader, actor != "", func(q query) ([]User, error) {
		u, err := readActor(q, actor, true)
		if err != nil {
			return nil, err
		}
		return usersWithKeys(q, userSelect+" WHERE u.account = ? ORDER BY u.name", u.Account)
	})
}

func (s *Store) changeOwnKeys(ctx context.Context, actor string, k Key, change userChange) (User, error) {
	return write(ctx, s, allGiven(actor, k.Type, k.Body, k.Line), func(w *writing) (User, error) {
		u, err := readActor(w.query, actor, false)
		if err != nil {
			return User{}, err
		}
		if err := loadKeys(w.query, &u); err != nil {
			return User{}, err
		}
		return applyUserChange(w, u, change)
	})
}

// AddKeyAs adds a key to the active user's own account.
func (s *Store) AddKeyAs(ctx context.Context, actor string, k Key) (User, error) {
	return s.changeOwnKeys(ctx, actor, k, addKey(k))
}

// RemoveKeyAs removes one of the active user's keys while retaining at least one.
func (s *Store) RemoveKeyAs(ctx context.Context, actor string, k Key) (User, int, error) {
	u, err := s.changeOwnKeys(ctx, actor, k, removeKey(k, true))
	return u, len(u.Keys), err
}

func adminTarget(q query, actor, name string, forbidSelf bool) (User, error) {
	a, err := readActor(q, actor, true)
	if err != nil {
		return User{}, err
	}
	u, err := getUser(q, a.Account, name)
	switch {
	case errors.Is(err, ErrNotInAccount):
		return User{}, ErrNotFound
	case err != nil:
		return User{}, err
	case forbidSelf && actor == name:
		return User{}, ErrSelf
	default:
		return u, nil
	}
}

func (s *Store) changeUserAs(ctx context.Context, actor, name, state string) (User, error) {
	return write(ctx, s, actor != "", func(w *writing) (User, error) {
		u, err := adminTarget(w.query, actor, name, state != UserActive)
		if err != nil {
			return User{}, err
		}
		return applyUserChange(w, u, changeUserState(state))
	})
}

// LockAs locks another user of the active admin's account.
func (s *Store) LockAs(ctx context.Context, actor, name string) (User, error) {
	return s.changeUserAs(ctx, actor, name, UserLocked)
}

// UnlockAs unlocks a user of the active admin's account.
func (s *Store) UnlockAs(ctx context.Context, actor, name string) (User, error) {
	return s.changeUserAs(ctx, actor, name, UserActive)
}

// DeleteAs requests removal of another locked user of the active admin's account.
func (s *Store) DeleteAs(ctx context.Context, actor, name string) (User, error) {
	return s.changeUserAs(ctx, actor, name, UserRemoving)
}

// SetAdminAs changes the admin status of a user of the active admin's account.
func (s *Store) SetAdminAs(ctx context.Context, actor, name string, admin bool) (User, bool, error) {
	out, err := write(ctx, s, actor != "", func(w *writing) (adminResult, error) {
		u, err := adminTarget(w.query, actor, name, !admin)
		if err != nil {
			return adminResult{}, err
		}
		return setAdmin(w.query, u, admin)
	})
	return out.user, out.changed, err
}
