package repos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	appserver "appkit/server"
	"registry"
)

func TestRunTokenMintStoresOnlyDigestAndRejectsUnknownRepository(t *testing.T) {
	// R-K96Y-3ON9
	ctx := context.Background()
	conn, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 12, 0, 0, 123, time.UTC)}
	custody := testCustodyWithClock(t, clock)
	if err := custody.Init(ctx, "code", "demo"); err != nil {
		t.Fatal(err)
	}
	created := clock.now.Add(-time.Hour)
	inTx(t, conn, func(tx *sql.Tx) error {
		return store.InsertRepository(ctx, tx, Repository{Kind: "code", Name: "demo", OwnerID: "owner", OwnerEmail: "owner@example.test", DefaultBranch: "main", CreatedAt: created})
	})
	service := NewService(store)
	service.SetCustody(custody)
	service.SetRunTokenTTL(90 * time.Minute)

	result, response := mintRunToken(t, service, "code", "demo")
	if response.Code != http.StatusOK {
		t.Fatalf("mint status=%d body=%q, want 200", response.Code, response.Body.String())
	}
	if len(result.Token) != 43 {
		t.Fatalf("token length=%d, want 43", len(result.Token))
	}
	if result.ExpiresAt != clock.now.Add(90*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("expires_at=%q, want %q", result.ExpiresAt, clock.now.Add(90*time.Minute).Format(time.RFC3339Nano))
	}
	wantCloneURL := registry.BaseURL("repos") + "/git/code/demo.git"
	if result.CloneURL != wantCloneURL {
		t.Fatalf("clone_url=%q, want %q", result.CloneURL, wantCloneURL)
	}
	digest := sha256.Sum256([]byte(result.Token))
	wantHash := hex.EncodeToString(digest[:])
	var columns [5]string
	if err := conn.QueryRow(`SELECT token_sha256, kind, name, expires_at, created_at FROM run_tokens`).Scan(
		&columns[0], &columns[1], &columns[2], &columns[3], &columns[4]); err != nil {
		t.Fatal(err)
	}
	if columns[0] != wantHash {
		t.Fatalf("stored token_sha256=%q, want %q", columns[0], wantHash)
	}
	for index, value := range columns {
		if value == result.Token {
			t.Fatalf("raw token stored in column %d", index)
		}
	}

	_, missing := mintRunToken(t, service, "code", "missing")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown repository status=%d body=%q, want 404", missing.Code, missing.Body.String())
	}
}

func TestRunTokenRouteIsLoopbackOnlyAndSweepDeletesOnlyExpiredRows(t *testing.T) {
	// R-KE2J-MRM1
	ctx := context.Background()
	conn, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 15, 0, 0, 0, time.UTC)}
	custody := testCustodyWithClock(t, clock)
	if err := custody.Init(ctx, "scripts", "worker"); err != nil {
		t.Fatal(err)
	}
	inTx(t, conn, func(tx *sql.Tx) error {
		return store.InsertRepository(ctx, tx, Repository{Kind: "scripts", Name: "worker", OwnerID: "owner", OwnerEmail: "owner@example.test", DefaultBranch: "main", CreatedAt: clock.now})
	})
	service := NewService(store)
	service.SetCustody(custody)
	service.SetRunTokenTTL(time.Hour)
	handler := appserver.LoopbackOnly(RunTokenHandler(service))

	request := httptest.NewRequest(http.MethodPost, "/run-token", bytes.NewBufferString(`{"kind":"scripts","name":"worker"}`))
	request.Header.Set("X-Forwarded-Proto", "https")
	crossed := httptest.NewRecorder()
	handler.ServeHTTP(crossed, request)
	if crossed.Code != http.StatusNotFound {
		t.Fatalf("crossed mint status=%d body=%q, want 404", crossed.Code, crossed.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/run-token", bytes.NewBufferString(`{"kind":"scripts","name":"worker"}`))
	loopback := httptest.NewRecorder()
	handler.ServeHTTP(loopback, request)
	if loopback.Code != http.StatusOK {
		t.Fatalf("loopback mint status=%d body=%q, want 200", loopback.Code, loopback.Body.String())
	}

	now := clock.now.Add(2 * time.Hour)
	expired := RunToken{TokenSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "scripts", Name: "worker", ExpiresAt: now.Add(-time.Nanosecond), CreatedAt: clock.now}
	equal := RunToken{TokenSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "scripts", Name: "worker", ExpiresAt: now, CreatedAt: clock.now}
	live := RunToken{TokenSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Kind: "scripts", Name: "worker", ExpiresAt: now.Add(time.Nanosecond), CreatedAt: clock.now}
	inTx(t, conn, func(tx *sql.Tx) error {
		for _, token := range []RunToken{expired, equal, live} {
			if err := store.InsertRunToken(ctx, tx, token); err != nil {
				return err
			}
		}
		return nil
	})
	inTx(t, conn, func(tx *sql.Tx) error {
		deleted, err := store.SweepExpiredTokens(ctx, tx, now)
		if err == nil && deleted != 2 { // the minted token and the explicitly expired row
			t.Errorf("deleted=%d, want 2", deleted)
		}
		return err
	})
	var hashes []string
	rows, err := conn.Query(`SELECT token_sha256 FROM run_tokens ORDER BY token_sha256`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{equal.TokenSHA256, live.TokenSHA256}; !reflect.DeepEqual(hashes, want) {
		t.Fatalf("surviving hashes=%v, want %v", hashes, want)
	}
}

func TestRunTokenRequestedTTLShortensLifetimeAndCannotExceedConfiguredCap(t *testing.T) {
	// R-II0Q-VTSI
	ctx := context.Background()
	conn, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC)}
	custody := testCustodyWithClock(t, clock)
	if err := custody.Init(ctx, "code", "demo"); err != nil {
		t.Fatal(err)
	}
	inTx(t, conn, func(tx *sql.Tx) error {
		return store.InsertRepository(ctx, tx, Repository{Kind: "code", Name: "demo", OwnerID: "owner", OwnerEmail: "owner@example.test", DefaultBranch: "main", CreatedAt: clock.now})
	})
	service := NewService(store)
	service.SetCustody(custody)
	service.SetRunTokenTTL(2 * time.Hour)

	shortened, response := mintRunTokenRequest(t, service, runTokenRequest{Kind: "code", Name: "demo", TTL: "35m"})
	if response.Code != http.StatusOK {
		t.Fatalf("shortened mint status=%d body=%q, want 200", response.Code, response.Body.String())
	}
	if want := clock.now.Add(35 * time.Minute).Format(time.RFC3339Nano); shortened.ExpiresAt != want {
		t.Fatalf("shortened expires_at=%q, want %q", shortened.ExpiresAt, want)
	}

	capped, response := mintRunTokenRequest(t, service, runTokenRequest{Kind: "code", Name: "demo", TTL: "12h"})
	if response.Code != http.StatusOK {
		t.Fatalf("capped mint status=%d body=%q, want 200", response.Code, response.Body.String())
	}
	if want := clock.now.Add(2 * time.Hour).Format(time.RFC3339Nano); capped.ExpiresAt != want {
		t.Fatalf("capped expires_at=%q, want %q", capped.ExpiresAt, want)
	}
}

func TestRunTokenInvalidRequestedTTLDoesNotMintAndOmittedTTLUsesDefault(t *testing.T) {
	// R-IJ8N-9LJ7
	ctx := context.Background()
	conn, store := newTestStore(t)
	clock := &sequenceClock{now: time.Date(2026, 8, 8, 17, 0, 0, 0, time.UTC)}
	custody := testCustodyWithClock(t, clock)
	if err := custody.Init(ctx, "code", "demo"); err != nil {
		t.Fatal(err)
	}
	inTx(t, conn, func(tx *sql.Tx) error {
		return store.InsertRepository(ctx, tx, Repository{Kind: "code", Name: "demo", OwnerID: "owner", OwnerEmail: "owner@example.test", DefaultBranch: "main", CreatedAt: clock.now})
	})
	service := NewService(store)
	service.SetCustody(custody)
	service.SetRunTokenTTL(90 * time.Minute)

	for _, ttl := range []string{"banana", "-5m"} {
		_, response := mintRunTokenRequest(t, service, runTokenRequest{Kind: "code", Name: "demo", TTL: ttl})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ttl %q status=%d body=%q, want 400", ttl, response.Code, response.Body.String())
		}
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM run_tokens`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("ttl %q inserted %d run token rows, want 0", ttl, count)
		}
	}

	result, response := mintRunToken(t, service, "code", "demo")
	if response.Code != http.StatusOK {
		t.Fatalf("omitted ttl status=%d body=%q, want 200", response.Code, response.Body.String())
	}
	if want := clock.now.Add(90 * time.Minute).Format(time.RFC3339Nano); result.ExpiresAt != want {
		t.Fatalf("omitted ttl expires_at=%q, want %q", result.ExpiresAt, want)
	}
}

func testCustodyWithClock(t *testing.T, clock Clock) *Custody {
	t.Helper()
	root := t.TempDir()
	custody, err := NewCustody(root, NewCommandGit("git", root), clock)
	if err != nil {
		t.Fatal(err)
	}
	return custody
}

func mintRunToken(t *testing.T, service *Service, kind, name string) (runTokenResponse, *httptest.ResponseRecorder) {
	t.Helper()
	return mintRunTokenRequest(t, service, runTokenRequest{Kind: kind, Name: name})
}

func mintRunTokenRequest(t *testing.T, service *Service, key runTokenRequest) (runTokenResponse, *httptest.ResponseRecorder) {
	t.Helper()
	body, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/run-token", bytes.NewReader(body))
	response := httptest.NewRecorder()
	RunTokenHandler(service).ServeHTTP(response, request)
	var result runTokenResponse
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return result, response
}
