package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// R-EUTH-BQ33
var _ func(*Store, string, []string, time.Time) (Client, error) = (*Store).RegisterClient

// R-EW1D-PHTS
var _ func(*Store, string, time.Time) (Client, error) = (*Store).LookupClient

// R-EX9A-39KH
var _ func(*Store, string, string, string, string, string, time.Time) (AuthCode, error) = (*Store).CreateAuthCode

// R-EYH6-H1B6
var _ func(*Store, string, time.Time) (AuthCode, error) = (*Store).ConsumeAuthCode

// R-EZP2-UT1V
var _ func(*Store, AuthCode, string, time.Time) (Token, string, error) = (*Store).CreateClientToken

// R-F0WZ-8KSK
var _ func(*Store, string, string) error = (*Store).RevokeToken

func TestClientContract(t *testing.T) {
	// R-ENI3-13MX
	_ = Client(struct {
		ID           string
		Name         string
		RedirectURIs []string
		CreatedAt    time.Time
	}{})
	// R-EOPZ-EVDM
	_ = AuthCode(struct {
		Code        string
		ClientID    string
		UserID      string
		RedirectURI string
		Challenge   string
		Resource    string
		IssuedAt    time.Time
	}{})
	// R-EL2A-9K5J
	func(*TokenKind) {}(declaredType(TokenPersonal))
	func(*TokenKind) {}(declaredType(TokenClient))
	const personal TokenKind = TokenPersonal
	const client TokenKind = TokenClient
	if string(personal) != "personal" || string(client) != "client" {
		t.Fatal("token kinds")
	}
	// R-EPXV-SN4B
	func(*time.Duration) {}(declaredType(AuthCodeTTL))
	const codeTTL time.Duration = AuthCodeTTL
	// R-ER5S-6EV0
	func(*time.Duration) {}(declaredType(UnusedClientTTL))
	const unusedTTL time.Duration = UnusedClientTTL
	// R-ESDO-K6LP
	func(*time.Duration) {}(declaredType(ClientTokenTTL))
	const tokenTTL time.Duration = ClientTokenTTL
	if codeTTL != 10*time.Minute || unusedTTL != 24*time.Hour || tokenTTL != 90*24*time.Hour {
		t.Fatal("lifetimes")
	}
	// R-EJUD-VSEU
	type entityPrefix string
	const prefix entityPrefix = idcodec.ClientIDPrefix
	if prefix != "cli_" {
		t.Fatal(prefix)
	}
}

func clientFixture(t *testing.T) (*Store, Client, AuthCode, Token, string) {
	t.Helper()
	st := openTokenTestStore(t, bytes.NewReader(tokenSequentialBytes(4096)))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	c, err := st.RegisterClient("Agent", []string{"https://client.test/callback", "http://127.0.0.1/callback"}, now)
	if err != nil {
		t.Fatal(err)
	}
	code, err := st.CreateAuthCode(c.ID, "owner", c.RedirectURIs[0], "challenge", "https://mcp.test/mcp", now)
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := st.CreateClientToken(code, "mcp.test", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return st, c, code, tok, secret
}

func rowCount(t *testing.T, st *Store, table, column, value string) int {
	t.Helper()
	var count int
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table+" WHERE "+column+" = ?", value).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestClientAndCodeExactRecordsAndPersistence(t *testing.T) {
	// R-FQIV-9RD5
	// R-FU6K-F2L8
	// R-FEBV-G1Y7
	// R-FFJR-TTOW
	now := tokenTestNow()
	random := tokenSequentialBytes(32)
	path := filepath.Join(t.TempDir(), "auth.db")
	st := openTokenTestStoreAt(t, path, bytes.NewReader(random))
	redirects := []string{"https://second.test/cb", "https://first.test/cb"}
	client, err := st.RegisterClient("Agent", redirects, now)
	want := Client{ID: idcodec.ClientIDPrefix + idcodec.Encode(random[:16]), Name: "Agent", RedirectURIs: append([]string(nil), redirects...), CreatedAt: now}
	if err != nil || !reflect.DeepEqual(client, want) {
		t.Fatalf("client: %#v %v want %#v", client, err, want)
	}
	redirects[0] = "mutated"
	code, err := st.CreateAuthCode(client.ID, "owner", "https://second.test/cb", "challenge", "resource", now)
	wantCode := AuthCode{Code: idcodec.Encode(random[16:]), ClientID: client.ID, UserID: "owner", RedirectURI: "https://second.test/cb", Challenge: "challenge", Resource: "resource", IssuedAt: now}
	if err != nil || code != wantCode {
		t.Fatalf("code: %#v %v", code, err)
	}
	if rowCount(t, st, "clients", "id", client.ID) != 1 || rowCount(t, st, "auth_codes", "code", code.Code) != 1 {
		t.Fatal("missing unique rows")
	}
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	later := openTokenTestStoreAt(t, path, bytes.NewReader(nil))
	got, err := later.LookupClient(client.ID, now.Add(AuthCodeTTL))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted client: %#v %v", got, err)
	}
	gotCode, err := later.ConsumeAuthCode(code.Code, now.Add(AuthCodeTTL))
	if err != nil || gotCode != code {
		t.Fatalf("persisted code: %#v %v", gotCode, err)
	}
}

func TestClientTokenExactValuesAndMissingClient(t *testing.T) {
	// R-G0A2-BXAP
	// R-G2PV-3GS3
	st, c, code, tok, secret := clientFixture(t)
	now := tokenTestNow().Add(time.Minute)
	random := tokenSequentialBytes(112)
	expiry := code.IssuedAt.Add(ClientTokenTTL)
	want := Token{ID: idcodec.TokenIDPrefix + idcodec.Encode(random[32:48]), UserID: "owner", Name: c.Name, Hash: idcodec.HashSecret(idcodec.SecretPrefix + idcodec.Encode(random[48:80])), Kind: TokenClient, Host: "mcp.test", Enabled: true, CreatedAt: now, ExpiresAt: &expiry}
	if secret != idcodec.SecretPrefix+idcodec.Encode(random[48:80]) || !reflect.DeepEqual(tok, want) || !reflect.DeepEqual(readToken(t, st, tok.ID), want) {
		t.Fatalf("token: %#v secret %q want %#v", tok, secret, want)
	}
	before := allTokenStates(t, st)
	code.ClientID = "missing"
	if got, secret, err := st.CreateClientToken(code, "host", now); !errors.Is(err, ErrNotFound) || got != (Token{}) || secret != "" {
		t.Fatalf("missing client: %#v %q %v", got, secret, err)
	}
	if !reflect.DeepEqual(before, allTokenStates(t, st)) {
		t.Fatal("missing client created token")
	}
}

func TestClientPruningBoundariesAndPermanentRegistration(t *testing.T) {
	// R-FPAY-VZMG
	// R-FRQR-NJ3U
	// R-FSYO-1AUJ
	// R-A4QA-XTHH
	st, used, _, tok, _ := clientFixture(t)
	now := tokenTestNow()
	stale, err := st.RegisterClient("stale", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := st.RegisterClient("boundary", nil, now.Add(time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	// Lookup deletes only its named stale client.
	if _, err = st.LookupClient(stale.ID, now.Add(UnusedClientTTL+time.Nanosecond)); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if rowCount(t, st, "clients", "id", stale.ID) != 0 || rowCount(t, st, "clients", "id", boundary.ID) != 1 {
		t.Fatal("lookup pruning scope")
	}
	if got, err := st.LookupClient(boundary.ID, now.Add(UnusedClientTTL+time.Nanosecond)); err != nil || !reflect.DeepEqual(got, boundary) {
		t.Fatalf("boundary: %#v %v", got, err)
	}
	_, err = st.RegisterClient("new", nil, now.Add(UnusedClientTTL+2*time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if rowCount(t, st, "clients", "id", boundary.ID) != 0 {
		t.Fatal("register failed pruning")
	}
	if _, err = st.LookupClient(boundary.ID, now.Add(UnusedClientTTL+2*time.Nanosecond)); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for _, later := range []time.Time{now.Add(ClientTokenTTL + time.Hour), now.Add(10 * ClientTokenTTL)} {
		got, err := st.LookupClient(used.ID, later)
		if err != nil || !reflect.DeepEqual(got, used) {
			t.Fatalf("used registration %#v %v", got, err)
		}
	}
	if err = st.RevokeToken("owner", tok.ID); err != nil {
		t.Fatal(err)
	}
	_, err = st.RegisterClient("later", nil, now.Add(20*ClientTokenTTL))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.LookupClient(used.ID, now.Add(20*ClientTokenTTL)); err != nil || !reflect.DeepEqual(got, used) {
		t.Fatalf("revoked client's registration %#v %v", got, err)
	}
}

func TestAuthorizationCodeCleanupAndConsumption(t *testing.T) {
	// R-FWMD-6M2M
	// R-FXU9-KDTB
	// R-FZ25-Y5K0
	for _, operation := range []string{"issue", "consume live", "consume expired", "consume unknown", "consume twice"} {
		t.Run(operation, func(t *testing.T) {
			st := openTokenTestStore(t, bytes.NewReader(tokenSequentialBytes(256)))
			now := tokenTestNow()
			old, err := st.CreateAuthCode("client", "user", "redirect", "challenge", "resource", now)
			if err != nil {
				t.Fatal(err)
			}
			live, err := st.CreateAuthCode("client", "user", "redirect", "challenge", "resource", now.Add(time.Nanosecond))
			if err != nil {
				t.Fatal(err)
			}
			at := now.Add(AuthCodeTTL + time.Nanosecond)
			switch operation {
			case "issue":
				if _, err := st.CreateAuthCode("client", "user", "redirect", "challenge", "resource", at); err != nil {
					t.Fatal(err)
				}
			case "consume live":
				got, err := st.ConsumeAuthCode(live.Code, at)
				if err != nil || got != live {
					t.Fatalf("consume %#v %v", got, err)
				}
			case "consume expired":
				if _, err := st.ConsumeAuthCode(old.Code, at); !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			case "consume unknown":
				if _, err := st.ConsumeAuthCode("unknown", at); !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			case "consume twice":
				if _, err := st.ConsumeAuthCode(live.Code, at); err != nil {
					t.Fatal(err)
				}
				if _, err := st.ConsumeAuthCode(live.Code, at); !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			}
			if rowCount(t, st, "auth_codes", "code", old.Code) != 0 {
				t.Fatal("expired code retained")
			}
			shouldRemain := operation == "issue" || operation == "consume expired" || operation == "consume unknown"
			if got := rowCount(t, st, "auth_codes", "code", live.Code); (got == 1) != shouldRemain {
				t.Fatalf("boundary live code rows %d", got)
			}
		})
	}
}

func TestClientTokenHostAndMutationScopes(t *testing.T) {
	// R-FJ7G-Z4WZ
	// R-FKFD-CWNO
	// R-G3XR-H8IS
	// R-FMRY-1QMC
	// R-FNZU-FID1
	// R-FO32-I7VR
	st, _, _, tok, secret := clientFixture(t)
	personal, personalSecret, err := st.CreateToken("owner", "personal", ExpiryNever, tokenTestNow())
	if err != nil {
		t.Fatal(err)
	}
	now := tokenTestNow().Add(time.Minute)
	if err := st.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE tokens SET host = 'ignored.test' WHERE id = ?", personal.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, op := range []func() error{func() error { return st.SetTokenEnabled("owner", tok.ID, false) }, func() error { return st.DeleteToken("owner", tok.ID) }, func() error { return st.RevokeToken("other", tok.ID) }, func() error { return st.RevokeToken("owner", personal.ID) }, func() error { return st.RevokeToken("owner", "unknown") }} {
		before := allTokenStates(t, st)
		if err := op(); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, allTokenStates(t, st)) {
			t.Fatal("wrong kind/owner changed tokens")
		}
	}
	for _, lookup := range []func(string, string, time.Time) (Identity, error){st.LookupTokenIdentity, st.TouchTokenIdentity} {
		for _, host := range []string{"", "other.test", "mcp.test:443", "MCP.TEST", "mcp.test"} {
			before := domainRows(t, st)
			got, err := lookup(secret, host, now)
			live := host == "MCP.TEST" || host == "mcp.test"
			if live {
				if err != nil || got != (Identity{UserID: "owner", Email: "owner@example.com", TokenID: tok.ID}) {
					t.Fatalf("host %q %#v %v", host, got, err)
				}
			} else {
				if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, domainRows(t, st)) {
					t.Fatalf("host %q accepted or changed row", host)
				}
			}
			if _, err := lookup(personalSecret, host, now); err != nil {
				t.Fatalf("personal host %q: %v", host, err)
			}
		}
	}
	// Empty bound host never authenticates, including against empty caller host.
	if err := st.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE tokens SET host = '' WHERE id = ?", tok.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"", "mcp.test"} {
		for _, lookup := range []func(string, string, time.Time) (Identity, error){st.LookupTokenIdentity, st.TouchTokenIdentity} {
			before := domainRows(t, st)
			if _, err := lookup(secret, host, now); !errors.Is(err, ErrNotFound) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, domainRows(t, st)) {
				t.Fatal("rejected touch changed row")
			}
		}
	}
	if err := st.RevokeToken("owner", tok.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := st.ListTokens("owner")
	if err != nil || len(listed) != 1 || listed[0].ID != personal.ID {
		t.Fatalf("revoke list %#v %v", listed, err)
	}
	for _, lookup := range []func(string, string, time.Time) (Identity, error){st.LookupTokenIdentity, st.TouchTokenIdentity} {
		if _, err := lookup(secret, "mcp.test", now); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
}

func rowCountAll(t *testing.T, st *Store, table string) int {
	t.Helper()
	var count int
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestClientTokenIdentityUseAndHashStorage(t *testing.T) {
	// R-G0A2-BXAP
	// R-G99G-TBAH
	// R-FNZU-FID1
	// R-FO32-I7VR
	// R-FMRY-1QMC
	st, _, code, tok, secret := clientFixture(t)
	now := tokenTestNow().Add(2 * time.Minute)
	before := domainRows(t, st)
	want := Identity{UserID: "owner", Email: "owner@example.com", TokenID: tok.ID}
	if got, err := st.LookupTokenIdentity(secret, "MCP.TEST", now); err != nil || got != want {
		t.Fatalf("lookup %#v %v", got, err)
	}
	if !reflect.DeepEqual(before, domainRows(t, st)) {
		t.Fatal("lookup changed row")
	}
	if got, err := st.TouchTokenIdentity(secret, "MCP.TEST", now); err != nil || got != want {
		t.Fatalf("touch %#v %v", got, err)
	}
	used := readToken(t, st, tok.ID)
	if used.LastUsedAt == nil || !used.LastUsedAt.Equal(now) {
		t.Fatalf("last use %#v", used.LastUsedAt)
	}
	tok.LastUsedAt = &now
	if !reflect.DeepEqual(tok, used) {
		t.Fatal("touch changed another token field")
	}
	// Reading every persisted column establishes that the secret is never stored.
	for _, rows := range domainRows(t, st) {
		for _, row := range rows {
			for _, value := range row {
				if value == secret {
					t.Fatal("plaintext persisted")
				}
			}
		}
	}
	if _, err := st.LookupTokenIdentity(used.Hash, "mcp.test", now); !errors.Is(err, ErrNotFound) {
		t.Fatal("hash honored as secret")
	}
	for _, change := range []string{"disabled", "expired", "login stale"} {
		t.Run(change, func(t *testing.T) {
			at := code.IssuedAt.Add(ClientTokenTTL)
			switch change {
			case "disabled":
				if err := st.db.Write(context.Background(), func(tx *sql.Tx) error {
					_, err := tx.ExecContext(context.Background(), "UPDATE tokens SET enabled = 0 WHERE id = ?", tok.ID)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				at = now
			case "expired":
				if _, _, err := st.UpsertUserOnLogin("issuer-owner", "subject-owner", "owner@example.com", at); err != nil {
					t.Fatal(err)
				}
			case "login stale":
				at = tokenTestNow().Add(TokenLoginWindow + time.Nanosecond)
				if _, _, err := st.UpsertUserOnLogin("issuer-owner", "subject-owner", "owner@example.com", tokenTestNow()); err != nil {
					t.Fatal(err)
				}
			}
			before := domainRows(t, st)
			for _, lookup := range []func(string, string, time.Time) (Identity, error){st.LookupTokenIdentity, st.TouchTokenIdentity} {
				if _, err := lookup(secret, "mcp.test", at); !errors.Is(err, ErrNotFound) {
					t.Fatalf("%s: %v", change, err)
				}
			}
			if !reflect.DeepEqual(before, domainRows(t, st)) {
				t.Fatal("failed lookup changed row")
			}
			if err := st.db.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.ExecContext(context.Background(), "UPDATE tokens SET enabled = 1 WHERE id = ?", tok.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func declaredType[T any](value T) *T { return &value }
