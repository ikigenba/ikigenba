package store

import "time"

// User is a persisted Google account.
type User struct {
	ID              string
	Issuer          string
	Subject         string
	Email           string
	LastGoogleLogin time.Time
}

// Session is a persisted browser session.
type Session struct {
	ID         string
	UserID     string
	LoginAt    time.Time
	LastUsedAt time.Time
}

// LoginState is a persisted OAuth login state.
type LoginState struct {
	State     string
	Verifier  string
	ReturnURL string
}

// Token is a persisted API token. Its plaintext secret is not stored.
type Token struct {
	ID         string
	UserID     string
	Name       string
	Hash       string
	Enabled    bool
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// Identity is the user id and email returned by a successful lookup.
type Identity struct {
	UserID string
	Email  string
}

// Expiry names how long a created token stays valid.
type Expiry string

// Token lifetimes accepted by CreateToken.
const (
	ExpiryNever Expiry = "never"
	Expiry30d   Expiry = "30d"
	Expiry90d   Expiry = "90d"
	Expiry365d  Expiry = "365d"
)

// Bounds applied by session and token identity lookups.
const (
	SessionIdle      = 15 * time.Minute
	SessionMax       = 18 * time.Hour
	TokenLoginWindow = 30 * 24 * time.Hour
)
