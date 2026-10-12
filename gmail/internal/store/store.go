// Package store persists transfers and drafts.
package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

// Config supplies the clock and random source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Store holds the database and serializes record operations.
type Store struct {
	db  *db.DB
	cfg Config
	mu  sync.Mutex
}

// New constructs a store over an existing database.
func New(d *db.DB, cfg Config) *Store { return &Store{db: d, cfg: cfg} }

// Transfer describes a recorded byte transfer.
type Transfer struct {
	ID, Direction, Status, Reason, Message, Attachment, Draft, Name, Path, ContentType string
	Bytes                                                                              int64
	Started, Finished                                                                  time.Time
	URL, Link, ArtifactID, UserID, UserEmail, RequestID                                string
}

// Ending describes the final outcome of a transfer.
type Ending struct {
	Status, Reason                 string
	Bytes                          int64
	Message, URL, Link, ArtifactID string
}

// Attachment describes an artifact attached to a draft.
type Attachment struct {
	Path, Name, ContentType string
	Bytes                   int64
}

// Draft holds a draft and its composition.
type Draft struct {
	ID, GmailDraft, Message, Thread, Status                   string
	To, Cc, Bcc                                               []string
	Subject, Body, ReplyTo, HeaderInReplyTo, HeaderReferences string
	Attachments                                               []Attachment
	Link                                                      string
	Created, Modified, Sent                                   time.Time
	UserID                                                    string
}

// Record vocabulary and identifier prefixes.
const (
	TransferIDPrefix  = "xfr_"
	DraftIDPrefix     = "drf_"
	DirectionPull     = "pull"
	DirectionPush     = "push"
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	DraftOpen         = "open"
	DraftSent         = "sent"
	DraftDiscarded    = "discarded"
	ReasonNotFound    = "not_found"
	ReasonForbidden   = "forbidden"
	ReasonTooLarge    = "too_large"
	ReasonReconnect   = "reconnect"
	ReasonProvider    = "provider"
	ReasonArtifacts   = "artifacts"
	ReasonInterrupted = "interrupted"
	Unreachable       = "The Gmail database is unavailable."
)

// Store errors distinguish record state from storage failures.
var (
	ErrNotFound    = errors.New("record not found")
	ErrEnded       = errors.New("transfer ended")
	ErrNotOpen     = errors.New("draft not open")
	ErrUnreachable = errors.New("database unreachable")
)

func validID(s, p string) bool {
	if len(s) != len(p)+16 || s[:len(p)] != p {
		return false
	}
	for _, c := range s[len(p):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidTransferID reports whether s is a connector transfer identifier.
func ValidTransferID(s string) bool { return validID(s, TransferIDPrefix) }

// ValidDraftID reports whether s is a connector draft identifier.
func ValidDraftID(s string) bool  { return validID(s, DraftIDPrefix) }
func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func encodeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return stamp(t).Format(time.RFC3339)
}
func decodeTime(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t.UTC() }
func normalizeErr(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrEnded) || errors.Is(err, ErrNotOpen) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) transaction(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	if write {
		return normalizeErr(s.db.Write(ctx, fn))
	}
	return normalizeErr(s.db.Read(ctx, fn))
}
func (s *Store) mint(ctx context.Context, tx *sql.Tx, table, prefix string) (string, error) {
	for range 8 {
		b := make([]byte, 8)
		if _, err := io.ReadFull(s.cfg.Rand, b); err != nil {
			return "", errors.Join(ErrUnreachable, err)
		}
		id := prefix + hex.EncodeToString(b)
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=?", id).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return id, nil
		}
	}
	return "", ErrUnreachable
}

const transferCols = "id,direction,status,reason,message,attachment,draft,name,path,content_type,bytes,started,finished,url,link,artifact_id,user_id,user_email,request_id"

type scanner interface{ Scan(...any) error }

func scanTransfer(r scanner) (Transfer, error) {
	var t Transfer
	var a, b string
	err := r.Scan(&t.ID, &t.Direction, &t.Status, &t.Reason, &t.Message, &t.Attachment, &t.Draft, &t.Name, &t.Path, &t.ContentType, &t.Bytes, &a, &b, &t.URL, &t.Link, &t.ArtifactID, &t.UserID, &t.UserEmail, &t.RequestID)
	t.Started = decodeTime(a)
	t.Finished = decodeTime(b)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return t, err
}
func readTransfer(ctx context.Context, tx *sql.Tx, id string) (Transfer, error) {
	return scanTransfer(tx.QueryRowContext(ctx, "SELECT "+transferCols+" FROM transfers WHERE id=?", id))
}
func putTransfer(ctx context.Context, tx *sql.Tx, t Transfer) error {
	_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO transfers ("+transferCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", t.ID, t.Direction, t.Status, t.Reason, t.Message, t.Attachment, t.Draft, t.Name, t.Path, t.ContentType, t.Bytes, encodeTime(t.Started), encodeTime(t.Finished), t.URL, t.Link, t.ArtifactID, t.UserID, t.UserEmail, t.RequestID)
	return err
}

// AddTransfer records a new transfer.
func (s *Store) AddTransfer(ctx context.Context, t Transfer) (Transfer, error) {
	var out Transfer
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if (t.Direction != DirectionPull && t.Direction != DirectionPush) || (t.Status != StatusQueued && t.Status != StatusRunning) || t.UserID == "" || t.Path == "" || t.Message == "" {
			return ErrUnreachable
		}
		id, err := s.mint(ctx, tx, "transfers", TransferIDPrefix)
		if err != nil {
			return err
		}
		t.ID = id
		t.Bytes = 0
		t.Started = stamp(s.cfg.Now())
		t.Finished = time.Time{}
		t.Reason = ""
		t.URL = ""
		t.Link = ""
		t.ArtifactID = ""
		if err = putTransfer(ctx, tx, t); err == nil {
			out = t
		}
		return err
	})
	if err != nil {
		return Transfer{}, err
	}
	return out, nil
}
func (s *Store) mutateTransfer(ctx context.Context, id string, valid bool, fn func(*Transfer) error) (Transfer, error) {
	var out Transfer
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !valid {
			return ErrUnreachable
		}
		t, err := readTransfer(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = fn(&t); err != nil {
			return err
		}
		if err = putTransfer(ctx, tx, t); err == nil {
			out = t
		}
		return err
	})
	if err != nil {
		return Transfer{}, err
	}
	return out, nil
}

// StartTransfer moves a queued transfer to running.
func (s *Store) StartTransfer(ctx context.Context, id string) (Transfer, error) {
	return s.mutateTransfer(ctx, id, true, func(t *Transfer) error {
		if t.Status != StatusQueued {
			return ErrEnded
		}
		t.Status = StatusRunning
		return nil
	})
}

// SetTransferBytes updates a running transfer's byte count.
func (s *Store) SetTransferBytes(ctx context.Context, id string, n int64) (Transfer, error) {
	return s.mutateTransfer(ctx, id, n >= 0, func(t *Transfer) error {
		if t.Status != StatusRunning {
			return ErrEnded
		}
		t.Bytes = n
		return nil
	})
}
func validReason(r string) bool {
	switch r {
	case ReasonNotFound, ReasonForbidden, ReasonTooLarge, ReasonReconnect, ReasonProvider, ReasonArtifacts, ReasonInterrupted:
		return true
	}
	return false
}
func validEnding(e Ending) bool {
	return e.Bytes >= 0 && (e.Status == StatusDone && e.Reason == "" || e.Status == StatusFailed && validReason(e.Reason) && e.Message == "" && e.URL == "" && e.Link == "" && e.ArtifactID == "")
}

// FinishTransfer records a transfer's final outcome.
func (s *Store) FinishTransfer(ctx context.Context, id string, e Ending) (Transfer, error) {
	return s.mutateTransfer(ctx, id, validEnding(e), func(t *Transfer) error {
		if t.Status == StatusDone || t.Status == StatusFailed {
			return ErrEnded
		}
		if t.Status == StatusQueued && e.Status == StatusDone {
			return ErrUnreachable
		}
		t.Status = e.Status
		t.Reason = e.Reason
		t.Bytes = e.Bytes
		t.URL = e.URL
		t.Link = e.Link
		t.ArtifactID = e.ArtifactID
		if e.Message != "" {
			t.Message = e.Message
		}
		t.Finished = stamp(s.cfg.Now())
		return nil
	})
}

// GetTransfer reads one transfer owned by the user.
func (s *Store) GetTransfer(ctx context.Context, userID, id string) (Transfer, error) {
	var out Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		t, err := readTransfer(ctx, tx, id)
		if err != nil {
			return err
		}
		if t.UserID != userID {
			return ErrNotFound
		}
		out = t
		return nil
	})
	if err != nil {
		return Transfer{}, err
	}
	return out, nil
}
func queryTransfers(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Transfer, error) {
	out := []Transfer{}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTransfers reads the user's transfers newest first.
func (s *Store) ListTransfers(ctx context.Context, userID string) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		out, err = queryTransfers(ctx, tx, "SELECT "+transferCols+" FROM transfers WHERE user_id=? ORDER BY started DESC,id DESC", userID)
		return err
	})
	return out, err
}

// UnfinishedTransfers reads all unfinished transfers oldest first.
func (s *Store) UnfinishedTransfers(ctx context.Context) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		out, err = queryTransfers(ctx, tx, "SELECT "+transferCols+" FROM transfers WHERE status IN (?,?) ORDER BY started,id", StatusQueued, StatusRunning)
		return err
	})
	return out, err
}
func oldEnough(t, now time.Time, days int64) bool {
	// Stored timestamps have second precision. Compare exact seconds instead
	// of durations, whose nanosecond representation saturates after 292 years.
	age := new(big.Int).Sub(big.NewInt(now.Unix()), big.NewInt(t.Unix()))
	limit := new(big.Int).Mul(big.NewInt(days), big.NewInt(86400))
	switch age.Cmp(limit) {
	case 1:
		return true
	case 0:
		return now.Nanosecond() > t.Nanosecond()
	default:
		return false
	}
}

// PruneTransfers removes old transfers outside each user's retained count.
func (s *Store) PruneTransfers(ctx context.Context, now time.Time, keepDays, keepCount int64) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if keepDays < 1 || keepCount < 1 {
			return ErrUnreachable
		}
		all, err := queryTransfers(ctx, tx, "SELECT "+transferCols+" FROM transfers ORDER BY user_id,started DESC,id DESC")
		if err != nil {
			return err
		}
		out = []Transfer{}
		ranks := map[string]int64{}
		for _, t := range all {
			rank := ranks[t.UserID]
			ranks[t.UserID]++
			if rank >= keepCount && (t.Status == StatusDone || t.Status == StatusFailed) && oldEnough(t.Started, now, keepDays) {
				if _, err = tx.ExecContext(ctx, "DELETE FROM transfers WHERE id=?", t.ID); err != nil {
					return err
				}
				out = append(out, t)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const draftCols = "id,gmail_draft,message,thread,status,to_addrs,cc_addrs,bcc_addrs,subject,body,reply_to,in_reply_to,references_header,attachments,created,modified,sent,user_id"

func scanDraft(r scanner) (Draft, error) {
	var d Draft
	var to, cc, bcc, a, c, m, sent string
	err := r.Scan(&d.ID, &d.GmailDraft, &d.Message, &d.Thread, &d.Status, &to, &cc, &bcc, &d.Subject, &d.Body, &d.ReplyTo, &d.HeaderInReplyTo, &d.HeaderReferences, &a, &c, &m, &sent, &d.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	if err != nil {
		return Draft{}, err
	}
	for _, v := range []struct {
		raw  string
		dest any
	}{{to, &d.To}, {cc, &d.Cc}, {bcc, &d.Bcc}, {a, &d.Attachments}} {
		if err = json.Unmarshal([]byte(v.raw), v.dest); err != nil {
			return Draft{}, err
		}
	}
	d.Created = decodeTime(c)
	d.Modified = decodeTime(m)
	d.Sent = decodeTime(sent)
	switch d.Status {
	case DraftOpen:
		d.Link = urls.DraftLink(d.Message)
	case DraftSent:
		d.Link = urls.MessageLink(d.Message)
	}
	return d, nil
}
func readDraft(ctx context.Context, tx *sql.Tx, id string) (Draft, error) {
	return scanDraft(tx.QueryRowContext(ctx, "SELECT "+draftCols+" FROM drafts WHERE id=?", id))
}
func jsonArray(v any) string { b, _ := json.Marshal(v); return string(b) }
func putDraft(ctx context.Context, tx *sql.Tx, d Draft) error {
	_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO drafts ("+draftCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", d.ID, d.GmailDraft, d.Message, d.Thread, d.Status, jsonArray(d.To), jsonArray(d.Cc), jsonArray(d.Bcc), d.Subject, d.Body, d.ReplyTo, d.HeaderInReplyTo, d.HeaderReferences, jsonArray(d.Attachments), encodeTime(d.Created), encodeTime(d.Modified), encodeTime(d.Sent), d.UserID)
	return err
}

// AddDraft records a new open draft.
func (s *Store) AddDraft(ctx context.Context, d Draft) (Draft, error) {
	var out Draft
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if d.UserID == "" || d.GmailDraft == "" || d.Message == "" {
			return ErrUnreachable
		}
		id, err := s.mint(ctx, tx, "drafts", DraftIDPrefix)
		if err != nil {
			return err
		}
		d.ID = id
		d.Status = DraftOpen
		d.To = append([]string{}, d.To...)
		d.Cc = append([]string{}, d.Cc...)
		d.Bcc = append([]string{}, d.Bcc...)
		d.Attachments = []Attachment{}
		d.Created = stamp(s.cfg.Now())
		d.Modified = d.Created
		d.Sent = time.Time{}
		d.Link = urls.DraftLink(d.Message)
		if err = putDraft(ctx, tx, d); err == nil {
			out = d
		}
		return err
	})
	if err != nil {
		return Draft{}, err
	}
	return out, nil
}
func (s *Store) mutateDraft(ctx context.Context, id string, valid bool, fn func(*Draft)) (Draft, error) {
	var out Draft
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !valid {
			return ErrUnreachable
		}
		d, err := readDraft(ctx, tx, id)
		if err != nil {
			return err
		}
		if d.Status != DraftOpen {
			return ErrNotOpen
		}
		fn(&d)
		d.Link = ""
		switch d.Status {
		case DraftOpen:
			d.Link = urls.DraftLink(d.Message)
		case DraftSent:
			d.Link = urls.MessageLink(d.Message)
		}
		if err = putDraft(ctx, tx, d); err == nil {
			out = d
		}
		return err
	})
	if err != nil {
		return Draft{}, err
	}
	return out, nil
}
func updateMessage(d *Draft, message, thread string) {
	d.Message = message
	if thread != "" {
		d.Thread = thread
	}
}

// AttachToDraft appends an attachment and updates Gmail identifiers.
func (s *Store) AttachToDraft(ctx context.Context, id string, a Attachment, message, thread string) (Draft, error) {
	return s.mutateDraft(ctx, id, a.Path != "" && a.Bytes >= 0 && message != "", func(d *Draft) {
		d.Attachments = append(d.Attachments, a)
		updateMessage(d, message, thread)
		d.Modified = stamp(s.cfg.Now())
	})
}

// MarkDraftSent records that an open draft was sent.
func (s *Store) MarkDraftSent(ctx context.Context, id, message, thread string, sent time.Time) (Draft, error) {
	return s.mutateDraft(ctx, id, message != "", func(d *Draft) {
		updateMessage(d, message, thread)
		d.Status = DraftSent
		d.GmailDraft = ""
		d.Modified = stamp(s.cfg.Now())
		if sent.IsZero() {
			sent = d.Modified
		}
		d.Sent = stamp(sent)
	})
}

// MarkDraftDiscarded records that an open draft was discarded.
func (s *Store) MarkDraftDiscarded(ctx context.Context, id string) (Draft, error) {
	return s.mutateDraft(ctx, id, true, func(d *Draft) { d.Status = DraftDiscarded; d.GmailDraft = ""; d.Modified = stamp(s.cfg.Now()) })
}

// SetDraftMessage refreshes an open draft's Gmail identifiers.
func (s *Store) SetDraftMessage(ctx context.Context, id, message, thread string) (Draft, error) {
	return s.mutateDraft(ctx, id, message != "", func(d *Draft) { updateMessage(d, message, thread) })
}

// GetDraft reads one draft owned by the user.
func (s *Store) GetDraft(ctx context.Context, userID, id string) (Draft, error) {
	var out Draft
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		d, err := readDraft(ctx, tx, id)
		if err != nil {
			return err
		}
		if d.UserID != userID {
			return ErrNotFound
		}
		out = d
		return nil
	})
	if err != nil {
		return Draft{}, err
	}
	return out, nil
}
func queryDrafts(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Draft, error) {
	out := []Draft{}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDrafts reads the user's drafts newest first.
func (s *Store) ListDrafts(ctx context.Context, userID string) ([]Draft, error) {
	var out []Draft
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		out, err = queryDrafts(ctx, tx, "SELECT "+draftCols+" FROM drafts WHERE user_id=? ORDER BY created DESC,id DESC", userID)
		return err
	})
	return out, err
}

// PruneDrafts removes old finished drafts outside the retained count.
func (s *Store) PruneDrafts(ctx context.Context, now time.Time, keepDays, keepCount int64) ([]Draft, error) {
	var out []Draft
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if keepDays < 1 || keepCount < 1 {
			return ErrUnreachable
		}
		all, err := queryDrafts(ctx, tx, "SELECT "+draftCols+" FROM drafts ORDER BY user_id,created DESC,id DESC")
		if err != nil {
			return err
		}
		out = []Draft{}
		ranks := map[string]int64{}
		for _, d := range all {
			if d.Status == DraftOpen {
				continue
			}
			rank := ranks[d.UserID]
			ranks[d.UserID]++
			if rank >= keepCount && oldEnough(d.Modified, now, keepDays) {
				if _, err = tx.ExecContext(ctx, "DELETE FROM drafts WHERE id=?", d.ID); err != nil {
					return err
				}
				out = append(out, d)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
