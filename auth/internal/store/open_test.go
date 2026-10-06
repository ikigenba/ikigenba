package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestFoundationContract(t *testing.T) {
	// Each conversion from a struct of exactly the declared exported fields, in
	// the declared order, compiles only if the type has identical fields.
	// R-47ML-KDLX
	_ = User(struct {
		ID              string
		Issuer          string
		Subject         string
		Email           string
		LastGoogleLogin time.Time
	}{})

	// R-48UH-Y5CM
	_ = Session(struct {
		ID         string
		UserID     string
		LoginAt    time.Time
		LastUsedAt time.Time
	}{})

	// R-4A2E-BX3B
	_ = LoginState(struct {
		State     string
		Verifier  string
		ReturnURL string
	}{})

	// R-4CI7-3GKP
	_ = Token(struct {
		ID         string
		UserID     string
		Name       string
		Hash       string
		Enabled    bool
		CreatedAt  time.Time
		ExpiresAt  *time.Time
		LastUsedAt *time.Time
	}{})

	// R-SWQG-1MLW
	_ = Identity(struct {
		UserID  string
		Email   string
		TokenID string
	}{})

	if ExpiryNever != "never" || Expiry30d != "30d" || Expiry90d != "90d" || Expiry365d != "365d" {
		t.Fatalf("expiry constants = %q, %q, %q, %q", ExpiryNever, Expiry30d, Expiry90d, Expiry365d)
	}

	if ErrNotFound == nil || !errors.Is(ErrNotFound, ErrNotFound) || !errors.Is(errors.Join(errors.New("context"), ErrNotFound), ErrNotFound) {
		t.Fatalf("ErrNotFound is not a usable errors.Is sentinel")
	}
}

func TestSessionIdleIsDurationConstant(t *testing.T) {
	// R-J5A1-Z776
	const idle = SessionIdle
	value, ok := any(idle).(time.Duration)
	if !ok || value != 15*time.Minute {
		t.Fatalf("SessionIdle = %#v, want time.Duration constant 15 * time.Minute", any(idle))
	}
}

func TestSessionMaxIsDurationConstant(t *testing.T) {
	// R-J6HY-CYXV
	const sessionMax = SessionMax
	value, ok := any(sessionMax).(time.Duration)
	if !ok || value != 18*time.Hour {
		t.Fatalf("SessionMax = %#v, want time.Duration constant 18 * time.Hour", any(sessionMax))
	}
}

func TestTokenLoginWindowIsDurationConstant(t *testing.T) {
	// R-J7PU-QQOK
	const window = TokenLoginWindow
	value, ok := any(window).(time.Duration)
	if !ok || value != 30*24*time.Hour {
		t.Fatalf("TokenLoginWindow = %#v, want time.Duration constant 30 * 24 * time.Hour", any(window))
	}
}

func TestExpiryIsDefinedStringTypeWithTypedConstants(t *testing.T) {
	// R-4EXZ-V023
	const (
		_ = ExpiryNever
		_ = Expiry30d
		_ = Expiry90d
		_ = Expiry365d
	)

	// Expiry converts to and from string, so its underlying type is string.
	if got := Expiry(string(Expiry("30d"))); got != Expiry30d {
		t.Errorf("Expiry round trip through string = %q", got)
	}
	constants := []struct {
		name  string
		value any
		want  string
	}{
		{name: "ExpiryNever", value: ExpiryNever, want: "never"},
		{name: "Expiry30d", value: Expiry30d, want: "30d"},
		{name: "Expiry90d", value: Expiry90d, want: "90d"},
		{name: "Expiry365d", value: Expiry365d, want: "365d"},
	}
	seen := make(map[string]string, len(constants))
	for _, constant := range constants {
		// An untyped constant would box as a plain string, not an Expiry.
		typed, ok := constant.value.(Expiry)
		if !ok || string(typed) != constant.want {
			t.Errorf("%s = %#v, want Expiry %q", constant.name, constant.value, constant.want)
		}
		if previous, ok := seen[string(typed)]; ok {
			t.Errorf("%s duplicates %s value %q", constant.name, previous, typed)
		}
		seen[string(typed)] = constant.name
	}
}

func TestErrNotFoundForMissingAndOtherOwnerRows(t *testing.T) {
	// R-2SV9-DI1W
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	insertTokenUser(t, st, "other", "other@example.com", now)
	insertToken(t, st, Token{
		ID:        "owned",
		UserID:    "owner",
		Name:      "owned",
		Hash:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now,
	})
	insertSessionFixture(t, st, "kept-session", "owner", now, now)
	if err := st.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			context.Background(),
			`INSERT INTO login_states (state, verifier, return_url) VALUES (?, ?, ?)`,
			"kept-state",
			"verifier",
			"/return",
		)
		return err
	}); err != nil {
		t.Fatalf("insert login state: %v", err)
	}
	beforeTokens := allTokenStates(t, st)

	if got, err := st.LookupSessionIdentity("missing-session", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("LookupSessionIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.TouchSession("missing-session", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("TouchSession(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.ConsumeLoginState("missing-state"); err == nil || !errors.Is(err, ErrNotFound) || got != (LoginState{}) {
		t.Errorf("ConsumeLoginState(missing) = %#v, %v; want zero login state and ErrNotFound", got, err)
	}
	if got, err := st.LookupTokenIdentity("missing-secret", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("LookupTokenIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.TouchTokenIdentity("missing-secret", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("TouchTokenIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if err := st.SetTokenEnabled("owner", "missing-token", false); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTokenEnabled(missing) error = %v, want ErrNotFound", err)
	}
	if err := st.SetTokenEnabled("other", "owned", false); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTokenEnabled(other owner) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteToken("owner", "missing-token"); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(missing) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteToken("other", "owned"); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(other owner) error = %v, want ErrNotFound", err)
	}

	if after := allTokenStates(t, st); !reflect.DeepEqual(after, beforeTokens) {
		t.Errorf("missing and other-owner calls changed tokens: before %#v, after %#v", beforeTokens, after)
	}
	assertStoredSession(t, st, "kept-session", "owner", now.UnixNano(), now.UnixNano())
	var verifier, returnURL string
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(
			context.Background(),
			`SELECT verifier, return_url FROM login_states WHERE state = ?`,
			"kept-state",
		).Scan(&verifier, &returnURL)
	}); err != nil || verifier != "verifier" || returnURL != "/return" {
		t.Errorf("kept login state = (%q, %q, %v), want verifier and /return", verifier, returnURL, err)
	}
}
