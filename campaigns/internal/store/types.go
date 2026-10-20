// Package store owns campaigns records and state transitions.
package store

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Config supplies the clock and randomness; neither has a default.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Store owns the records over an existing database handle.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New builds a store without opening a database.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, cfg: cfg} }

// Record constants define ids, channels, states and delivery methods.
const (
	CampaignIDPrefix = "camp_"
	PieceIDPrefix    = "piece_"
	ChannelMail      = "mail"
	ChannelEmail     = "email"
	StatePlanned     = "planned"
	StateDelivered   = "delivered"
	StateDropped     = "dropped"
	HowHand          = "hand"
	HowSystem        = "system"
)

// Unreachable is the public response when campaign records cannot be reached.
const Unreachable string = "Campaign records are unavailable."

// Store errors distinguish missing records, refused values and database failures.
var (
	ErrNotFound    = errors.New("record not found")
	ErrInvalid     = errors.New("invalid change")
	ErrUnreachable = errors.New("database unreachable")
)

// Campaign is a campaign, including its deletion mark.
type Campaign struct {
	ID, Name, Description           string
	Channels                        []string
	Destination                     string
	Created                         time.Time
	CreatedUserID, CreatedUserEmail string
	Deleted                         bool
}

// CampaignEdit names the editable campaign fields; nil leaves a field unchanged.
type CampaignEdit struct {
	ID                             string
	Name, Description, Destination *string
}

// Member is a candidate in one campaign, including its rejection.
type Member struct {
	Campaign, Candidate, Cohort       string
	Added                             time.Time
	AddedUserID, AddedUserEmail       string
	Rejected                          bool
	RejectedUserID, RejectedUserEmail string
	RejectedAt                        time.Time
	Why                               string
}

// MemberInput supplies a candidate and the channels for its pieces.
type MemberInput struct {
	Campaign, Candidate, Cohort string
	Channels                    []string
	UserID, UserEmail           string
}

// Rejection records who rejected a member and why.
type Rejection struct{ Campaign, Candidate, Why, UserID, UserEmail string }

// Piece is one channel of outreach to one member.
type Piece struct {
	ID, Campaign, Candidate, Channel, State, Token string
	Created, Delivered                             time.Time
	DeliveredUserID, DeliveredUserEmail, How       string
	Dropped                                        time.Time
	DroppedUserID, DroppedUserEmail                string
}

// PieceAction names a piece and the caller changing it.
type PieceAction struct{ Piece, UserID, UserEmail string }

// ChannelDelivery names a channel to deliver and its caller.
type ChannelDelivery struct{ Campaign, Channel, UserID, UserEmail string }

// Material is a labelled reference to an artifact.
type Material struct {
	Campaign, Path, Label, Artifact string
	Added                           time.Time
	AddedUserID, AddedUserEmail     string
}

// Visit records a visit and reads its current destination.
type Visit struct {
	Seq                          int64
	Piece                        string
	At                           time.Time
	UserAgent, Referrer, Address string
}

// VisitInput holds request data preserved verbatim.
type VisitInput struct{ UserAgent, Referrer, Address string }

// Visited identifies the visited piece and current destination.
type Visited struct{ Campaign, Piece, Destination string }

// PieceEngagement summarizes visits to one piece.
type PieceEngagement struct {
	Opens       int
	First, Last time.Time
}

// MemberEngagement summarizes visits across a member’s pieces.
type MemberEngagement struct {
	Opened      bool
	Opens       int
	First, Last time.Time
}

// Counts summarizes campaign membership, delivery and engagement.
type Counts struct{ Members, Delivered, Opened int }

// CampaignRow combines a campaign and its counts.
type CampaignRow struct {
	Campaign Campaign
	Counts   Counts
}

// PieceRow combines a piece and its engagement.
type PieceRow struct {
	Piece      Piece
	Engagement PieceEngagement
}

// MemberRow combines a member, its pieces and engagement.
type MemberRow struct {
	Member     Member
	Pieces     []PieceRow
	Engagement MemberEngagement
}

// MergeRow combines a piece and its member’s rejection mark.
type MergeRow struct {
	Piece    Piece
	Rejected bool
}

func (s *Store) run(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	var err error
	if write {
		err = s.d.Write(ctx, fn)
	} else {
		err = s.d.Read(ctx, fn)
	}
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) stamp() time.Time { return s.cfg.Now().UTC().Truncate(time.Second) }
