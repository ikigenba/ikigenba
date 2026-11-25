package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

// Invite is a recorded offer to join an account, without its secret token.
type Invite struct {
	ID, Account      string
	Admin            bool
	Created, Expires time.Time
	CreatedBy        string
}

// Invite limits and identifier prefixes.
const (
	InviteLife   = 72 * time.Hour
	MaxInvites   = 20
	InvitePrefix = "iv_"
	TokenPrefix  = "inv_"
)

const tokenAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var tokenEncoding = base32.NewEncoding(tokenAlphabet).WithPadding(base32.NoPadding)

func validText(s, prefix, alphabet string, size int) bool {
	if len(s) != len(prefix)+size || !strings.HasPrefix(s, prefix) {
		return false
	}
	for i := len(prefix); i < len(s); i++ {
		if !strings.ContainsRune(alphabet, rune(s[i])) {
			return false
		}
	}
	return true
}

// ValidInviteID reports whether s has the invite identifier format.
func ValidInviteID(s string) bool { return validText(s, InvitePrefix, "0123456789abcdef", 16) }

// ValidToken reports whether s has the enrollment token format.
func ValidToken(s string) bool { return validText(s, TokenPrefix, tokenAlphabet, 52) }

const inviteFields = "i.id, i.account, i.admin, i.created, i.expires, i.created_by"
const liveInvites = " FROM invites i JOIN accounts a ON a.slug = i.account AND a.gid = i.gid WHERE i.expires > ?"

func inviteDest(i *Invite) []any {
	return []any{&i.ID, &i.Account, &i.Admin, storedTime{&i.Created}, storedTime{&i.Expires}, &i.CreatedBy}
}

func scanInvite(r scanner) (i Invite, err error) {
	err = r.Scan(inviteDest(&i)...)
	return i, err
}

func deleteAccountInvites(q query, slug string) error {
	return q.exec("DELETE FROM invites WHERE account = ?", slug)
}

type invited struct {
	invite Invite
	token  string
}

func (s *Store) inviteID(q query) (string, error) {
	for range 8 {
		var data [8]byte
		if _, err := io.ReadFull(s.cfg.Rand, data[:]); err != nil {
			return "", ErrUnreachable
		}
		id := InvitePrefix + hex.EncodeToString(data[:])
		var held bool
		if err := q.row("SELECT EXISTS (SELECT 1 FROM invites WHERE id = ?)", id).Scan(&held); err != nil {
			return "", err
		}
		if !held {
			return id, nil
		}
	}
	return "", ErrUnreachable
}

func (s *Store) createInvite(q query, a Account, admin bool, c identity.Caller) (invited, error) {
	if a.State == AccountRemoving {
		return invited{}, ErrAccountRemoving
	}
	stamp := s.stamp()
	if err := q.exec("DELETE FROM invites WHERE account = ? AND (gid <> ? OR expires <= ?)",
		a.Slug, a.GID, stamp.Unix()); err != nil {
		return invited{}, err
	}
	var count int
	if err := q.row("SELECT count(*) FROM invites WHERE account = ? AND gid = ?", a.Slug, a.GID).Scan(&count); err != nil {
		return invited{}, err
	}
	if count >= MaxInvites {
		return invited{}, ErrTooManyInvites
	}
	id, err := s.inviteID(q)
	if err != nil {
		return invited{}, err
	}
	var secret [32]byte
	if _, err := io.ReadFull(s.cfg.Rand, secret[:]); err != nil {
		return invited{}, ErrUnreachable
	}
	token := TokenPrefix + tokenEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(token))
	i := Invite{ID: id, Account: a.Slug, Admin: admin, Created: stamp, Expires: stamp.Add(InviteLife), CreatedBy: c.UserID}
	err = q.exec(`INSERT INTO invites (id, account, gid, admin, created, expires, created_by, token_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, i.ID, i.Account, a.GID, i.Admin, i.Created.Unix(), i.Expires.Unix(), i.CreatedBy, hash[:])
	return invited{i, token}, err
}

// CreateInvite creates a live invite and returns its token once.
func (s *Store) CreateInvite(ctx context.Context, account string, admin bool, c identity.Caller) (Invite, string, error) {
	out, err := write(ctx, s, account != "" && validCaller(c), func(w *writing) (invited, error) {
		a, err := getAccount(w.query, account)
		if err != nil {
			return invited{}, err
		}
		return s.createInvite(w.query, a, admin, c)
	})
	return out.invite, out.token, err
}

// CreateInviteAs grants an ordinary-user invite to the active admin's account.
func (s *Store) CreateInviteAs(ctx context.Context, actor string, c identity.Caller) (Invite, string, error) {
	out, err := write(ctx, s, actor != "" && validCaller(c), func(w *writing) (invited, error) {
		u, err := readActor(w.query, actor, true)
		if err != nil {
			return invited{}, err
		}
		a, err := getAccount(w.query, u.Account)
		if err != nil {
			return invited{}, err
		}
		return s.createInvite(w.query, a, false, c)
	})
	return out.invite, out.token, err
}

func listInvites(q query, account string, stamp time.Time) ([]Invite, error) {
	return collect(q, scanInvite, "SELECT "+inviteFields+liveInvites+" AND i.account = ? ORDER BY i.created, i.id",
		stamp.Unix(), account)
}

// Invites lists the account's live invites in creation order.
func (s *Store) Invites(ctx context.Context, account string) ([]Invite, error) {
	return read(ctx, &s.recordReader, account != "", func(q query) ([]Invite, error) {
		if _, err := getAccount(q, account); err != nil {
			return nil, err
		}
		return listInvites(q, account, s.stamp())
	})
}

// InvitesAs lists the active admin's account's live invites.
func (s *Store) InvitesAs(ctx context.Context, actor string) ([]Invite, error) {
	return read(ctx, &s.recordReader, actor != "", func(q query) ([]Invite, error) {
		u, err := readActor(q, actor, true)
		if err != nil {
			return nil, err
		}
		return listInvites(q, u.Account, s.stamp())
	})
}

func revokeInvite(q query, id, account string, stamp time.Time) (Invite, error) {
	if !ValidInviteID(id) {
		return Invite{}, ErrNotFound
	}
	i, err := scanInvite(q.row("SELECT "+inviteFields+liveInvites+" AND i.id = ?", stamp.Unix(), id))
	if err != nil {
		return Invite{}, err
	}
	if account != "" && i.Account != account {
		return Invite{}, ErrNotFound
	}
	return i, q.exec("DELETE FROM invites WHERE id = ?", i.ID)
}

// RevokeInvite deletes one live invite.
func (s *Store) RevokeInvite(ctx context.Context, id string) (Invite, error) {
	return write(ctx, s, true, func(w *writing) (Invite, error) {
		return revokeInvite(w.query, id, "", s.stamp())
	})
}

// RevokeInviteAs deletes one live invite of the active admin's account.
func (s *Store) RevokeInviteAs(ctx context.Context, actor, id string) (Invite, error) {
	return write(ctx, s, actor != "", func(w *writing) (Invite, error) {
		u, err := readActor(w.query, actor, true)
		if err != nil {
			return Invite{}, err
		}
		return revokeInvite(w.query, id, u.Account, s.stamp())
	})
}

func findInvite(q query, token string, stamp time.Time) (Invite, error) {
	if !ValidToken(token) {
		return Invite{}, ErrNotFound
	}
	hash := sha256.Sum256([]byte(token))
	var i Invite
	var stored []byte
	err := q.row("SELECT "+inviteFields+", i.token_hash"+liveInvites+" AND i.token_hash = ? AND a.state <> ?",
		stamp.Unix(), hash[:], AccountRemoving).Scan(append(inviteDest(&i), &stored)...)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Invite{}, err
	}
	if subtle.ConstantTimeCompare(hash[:], stored) != 1 {
		return Invite{}, ErrNotFound
	}
	return i, nil
}

// InviteByToken looks up a usable invite without consuming it.
func (s *Store) InviteByToken(ctx context.Context, token string) (Invite, error) {
	return read(ctx, &s.recordReader, true, func(q query) (Invite, error) {
		return findInvite(q, token, s.stamp())
	})
}

type enrolled struct {
	invite Invite
	user   User
}

// Enroll consumes a usable invite and creates the account user in one transaction.
func (s *Store) Enroll(ctx context.Context, token, name string, k Key) (Invite, User, error) {
	out, err := write(ctx, s, allGiven(name, k.Type, k.Body, k.Line), func(w *writing) (enrolled, error) {
		stamp := s.stamp()
		i, err := findInvite(w.query, token, stamp)
		if err != nil {
			return enrolled{}, err
		}
		a, err := getAccount(w.query, i.Account)
		if err != nil {
			return enrolled{}, err
		}
		u, err := addUser(w, a, name, k, i.Admin, i.CreatedBy, stamp)
		if err != nil {
			return enrolled{}, err
		}
		return enrolled{i, u}, w.exec("DELETE FROM invites WHERE id = ?", i.ID)
	})
	return out.invite, out.user, err
}
