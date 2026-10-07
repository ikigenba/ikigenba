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

// TokenKind distinguishes personal and MCP client tokens.
type TokenKind string

// Token kinds accepted by the store.
const (
	TokenPersonal TokenKind = "personal"
	TokenClient   TokenKind = "client"
)

// Client is a persisted MCP client registration.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	CreatedAt    time.Time
}

// AuthCode is a short-lived, single-use authorization grant.
type AuthCode struct {
	Code        string
	ClientID    string
	UserID      string
	RedirectURI string
	Challenge   string
	Resource    string
	IssuedAt    time.Time
}

// Token is a persisted API token. Its plaintext secret is not stored.
type Token struct {
	ID         string
	UserID     string
	Name       string
	Hash       string
	Kind       TokenKind
	Host       string
	Enabled    bool
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// Identity is the user and, for token authentication, the honored token.
type Identity struct {
	UserID  string
	Email   string
	TokenID string
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
	AuthCodeTTL      time.Duration = 10 * time.Minute
	UnusedClientTTL  time.Duration = 24 * time.Hour
	ClientTokenTTL   time.Duration = 90 * 24 * time.Hour
	SessionIdle      time.Duration = 15 * time.Minute
	SessionMax       time.Duration = 18 * time.Hour
	TokenLoginWindow time.Duration = 30 * 24 * time.Hour
)
