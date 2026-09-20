package store

import "time"

type User struct {
	ID              string
	Issuer          string
	Subject         string
	Email           string
	LastGoogleLogin time.Time
}

type Session struct {
	ID         string
	UserID     string
	LoginAt    time.Time
	LastUsedAt time.Time
}

type LoginState struct {
	State     string
	Verifier  string
	ReturnURL string
}

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

type Identity struct {
	UserID string
	Email  string
}

type Expiry string

const (
	ExpiryNever Expiry = "never"
	Expiry30d   Expiry = "30d"
	Expiry90d   Expiry = "90d"
	Expiry365d  Expiry = "365d"
)

const (
	SessionIdle      = 15 * time.Minute
	SessionMax       = 18 * time.Hour
	TokenLoginWindow = 30 * 24 * time.Hour
)
