// Package store keeps webhooks' records, their deliveries and their rules.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Identifier and secret prefixes, the two schemes, the body cap and the
// shared unreachable line.
const (
	IDPrefix                 = "whk_"
	DeliveryPrefix           = "whd_"
	SecretPrefix             = "whs_"
	Bearer                   = "bearer"
	GitHubHMAC               = "github-hmac"
	MaxBody                  = 1 << 20
	Unreachable       string = "cannot reach the database; try again later"
	secretBytes              = 32
	idBytes                  = 8
	timeLayout               = "2006-01-02T15:04:05Z"
	crockfordAlphabet        = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// Errors distinguish record refusals from unavailable storage.
var (
	ErrNotFound    = errors.New("webhook not found")
	ErrSlugTaken   = errors.New("webhook slug taken")
	ErrInvalid     = errors.New("invalid webhook")
	ErrUnreachable = errors.New(Unreachable)
)

var secretEncoding = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)

// Config supplies the clock and the source ids and secrets are drawn from.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Webhook is the complete persisted record of a webhook.
type Webhook struct {
	ID, Slug, Scheme, OwnerID, OwnerEmail string
	SecretSHA256, SecretPlain             string
	Created, LastReceived                 time.Time
}

// Draft contains the caller-provided fields of a new webhook.
type Draft struct{ Slug, Scheme, OwnerID, OwnerEmail string }

// Arrival is what the ingress keeps of an accepted delivery.
type Arrival struct {
	ContentType, GitHubEvent, GitHubDelivery string
	Body                                     []byte
}

// Delivery is the complete persisted record of an accepted delivery.
type Delivery struct {
	ID, HookID                               string
	Received                                 time.Time
	ContentType, GitHubEvent, GitHubDelivery string
	Body                                     []byte
}

// Store operates over an appkit database handle.
type Store struct {
	db  *db.DB
	cfg Config
}

// New builds a store without opening or closing its handle.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{db: d, cfg: cfg}
}

func validHex(s, prefix string) bool {
	if len(s) != len(prefix)+2*idBytes || !strings.HasPrefix(s, prefix) {
		return false
	}
	for _, b := range []byte(s[len(prefix):]) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidID reports whether s is a webhook id.
func ValidID(s string) bool { return validHex(s, IDPrefix) }

// ValidDeliveryID reports whether s is a delivery id.
func ValidDeliveryID(s string) bool { return validHex(s, DeliveryPrefix) }

// ValidSlug reports whether s follows the webhook slug grammar.
func ValidSlug(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' || s[len(s)-1] == '_' {
		return false
	}
	for i, b := range []byte(s) {
		if ((b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '_') || (b == '_' && i > 0 && s[i-1] == '_') {
			return false
		}
	}
	return true
}

// ValidScheme reports whether s names one of the two schemes.
func ValidScheme(s string) bool { return s == Bearer || s == GitHubHMAC }

// HashSecret returns the lowercase-hex SHA-256 of secret.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

const columns = "id, slug, scheme, secret_sha256, secret_plain, owner_id, owner_email, created, last_received"

type scanner interface{ Scan(...any) error }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	return t.UTC(), err
}

func scan(row scanner) (Webhook, error) {
	var w Webhook
	var created string
	var hash, plain, received sql.NullString
	if err := row.Scan(&w.ID, &w.Slug, &w.Scheme, &hash, &plain, &w.OwnerID, &w.OwnerEmail, &created, &received); err != nil {
		return Webhook{}, err
	}
	w.SecretSHA256, w.SecretPlain = hash.String, plain.String
	var err error
	if w.Created, err = parseTime(created); err != nil {
		return Webhook{}, err
	}
	if received.Valid {
		w.LastReceived, err = parseTime(received.String)
	}
	return w, err
}

func classify(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrSlugTaken) || errors.Is(err, ErrUnreachable) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}

func (s *Store) transaction(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return classify(err)
	}
	if write {
		return classify(s.db.Write(ctx, fn))
	}
	return classify(s.db.Read(ctx, fn))
}

func (s *Store) draw(ctx context.Context, tx *sql.Tx, table, prefix string) (string, error) {
	for {
		var b [idBytes]byte
		if _, err := io.ReadFull(s.cfg.Rand, b[:]); err != nil {
			return "", fmt.Errorf("%w: random source: %s", ErrUnreachable, err.Error())
		}
		id := prefix + hex.EncodeToString(b[:])
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id = ?", id).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return id, nil
		}
	}
}

func (s *Store) mint() (string, error) {
	var b [secretBytes]byte
	if _, err := io.ReadFull(s.cfg.Rand, b[:]); err != nil {
		return "", fmt.Errorf("%w: random source: %s", ErrUnreachable, err.Error())
	}
	return SecretPrefix + secretEncoding.EncodeToString(b[:]), nil
}

func keep(w *Webhook, secret string) {
	w.SecretSHA256, w.SecretPlain = "", ""
	if w.Scheme == GitHubHMAC {
		w.SecretPlain = secret
	} else {
		w.SecretSHA256 = HashSecret(secret)
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func byID(ctx context.Context, tx *sql.Tx, id string) (Webhook, error) {
	w, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM webhooks WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	return w, err
}

// List returns every webhook in ascending slug order.
func (s *Store) List(ctx context.Context) ([]Webhook, error) {
	result := make([]Webhook, 0)
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM webhooks ORDER BY slug COLLATE BINARY")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			w, err := scan(rows)
			if err != nil {
				return err
			}
			result = append(result, w)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Get finds a webhook by its exact slug.
func (s *Store) Get(ctx context.Context, slug string) (Webhook, error) {
	var result Webhook
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM webhooks WHERE slug = ?", slug))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return Webhook{}, err
	}
	return result, nil
}

// Create validates and inserts a new webhook, answering its minted secret.
func (s *Store) Create(ctx context.Context, d Draft) (Webhook, string, error) {
	var result Webhook
	var secret string
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !ValidSlug(d.Slug) || !ValidScheme(d.Scheme) || d.OwnerID == "" {
			return ErrInvalid
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM webhooks WHERE slug = ?", d.Slug).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrSlugTaken
		}
		id, err := s.draw(ctx, tx, "webhooks", IDPrefix)
		if err != nil {
			return err
		}
		if secret, err = s.mint(); err != nil {
			return err
		}
		result = Webhook{ID: id, Slug: d.Slug, Scheme: d.Scheme, OwnerID: d.OwnerID, OwnerEmail: d.OwnerEmail, Created: s.cfg.Now().UTC().Truncate(time.Second)}
		keep(&result, secret)
		_, err = tx.ExecContext(ctx, "INSERT INTO webhooks ("+columns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)", result.ID, result.Slug, result.Scheme, nullable(result.SecretSHA256), nullable(result.SecretPlain), result.OwnerID, result.OwnerEmail, result.Created.Format(timeLayout))
		return err
	})
	if err != nil {
		return Webhook{}, "", err
	}
	return result, secret, nil
}

// Rotate replaces a webhook's secret, answering the new one.
func (s *Store) Rotate(ctx context.Context, id string) (Webhook, string, error) {
	var result Webhook
	var secret string
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		w, err := byID(ctx, tx, id)
		if err != nil {
			return err
		}
		if secret, err = s.mint(); err != nil {
			return err
		}
		keep(&w, secret)
		result = w
		_, err = tx.ExecContext(ctx, "UPDATE webhooks SET secret_sha256 = ?, secret_plain = ? WHERE id = ?", nullable(w.SecretSHA256), nullable(w.SecretPlain), id)
		return err
	})
	if err != nil {
		return Webhook{}, "", err
	}
	return result, secret, nil
}

// Delete removes a webhook and every delivery it holds, returning its former record.
func (s *Store) Delete(ctx context.Context, id string) (Webhook, error) {
	var result Webhook
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		w, err := byID(ctx, tx, id)
		if err != nil {
			return err
		}
		result = w
		if _, err = tx.ExecContext(ctx, "DELETE FROM deliveries WHERE hook_id = ?", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM webhooks WHERE id = ?", id)
		return err
	})
	if err != nil {
		return Webhook{}, err
	}
	return result, nil
}

// Receive records an accepted delivery and the webhook's latest receipt together.
func (s *Store) Receive(ctx context.Context, hookID string, a Arrival) (Delivery, error) {
	var result Delivery
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if len(a.Body) > MaxBody {
			return ErrInvalid
		}
		if _, err := byID(ctx, tx, hookID); err != nil {
			return err
		}
		id, err := s.draw(ctx, tx, "deliveries", DeliveryPrefix)
		if err != nil {
			return err
		}
		body := a.Body
		if body == nil {
			body = []byte{}
		}
		result = Delivery{ID: id, HookID: hookID, Received: s.cfg.Now().UTC().Truncate(time.Second), ContentType: a.ContentType, GitHubEvent: a.GitHubEvent, GitHubDelivery: a.GitHubDelivery, Body: append([]byte{}, body...)}
		stamp := result.Received.Format(timeLayout)
		if _, err = tx.ExecContext(ctx, "INSERT INTO deliveries (id, hook_id, received, content_type, github_event, github_delivery, body) VALUES (?, ?, ?, ?, ?, ?, ?)", result.ID, hookID, stamp, result.ContentType, result.GitHubEvent, result.GitHubDelivery, body); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE webhooks SET last_received = ? WHERE id = ?", stamp, hookID)
		return err
	})
	if err != nil {
		return Delivery{}, err
	}
	return result, nil
}

// Delivery finds a delivery by id, with the webhook it was made to.
func (s *Store) Delivery(ctx context.Context, id string) (Delivery, Webhook, error) {
	var d Delivery
	var w Webhook
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var received string
		err := tx.QueryRowContext(ctx, "SELECT id, hook_id, received, content_type, github_event, github_delivery, body FROM deliveries WHERE id = ?", id).Scan(&d.ID, &d.HookID, &received, &d.ContentType, &d.GitHubEvent, &d.GitHubDelivery, &d.Body)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if d.Body == nil {
			d.Body = []byte{}
		}
		if d.Received, err = parseTime(received); err != nil {
			return err
		}
		w, err = byID(ctx, tx, d.HookID)
		return err
	})
	if err != nil {
		return Delivery{}, Webhook{}, err
	}
	return d, w, nil
}

// Sweep removes every delivery received before the given time, answering how many.
func (s *Store) Sweep(ctx context.Context, before time.Time) (int64, error) {
	var removed int64
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM deliveries WHERE received < ?", before.UTC().Truncate(time.Second).Format(timeLayout))
		if err != nil {
			return err
		}
		removed, err = result.RowsAffected()
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
