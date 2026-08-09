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

func TestRunTokenMintHonorsRequestedTTLAndStoresOnlyDigest(t *testing.T) {
	// R-36US-ASST
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

	first, response := mintRunTokenRequest(t, service, runTokenRequest{Kind: "code", Name: "demo", TTL: "40m"})
	if response.Code != http.StatusOK {
		t.Fatalf("mint status=%d body=%q, want 200", response.Code, response.Body.String())
	}
	if len(first.Token) != 43 {
		t.Fatalf("token length=%d, want 43", len(first.Token))
	}
	if want := clock.now.Add(40 * time.Minute).Format(time.RFC3339Nano); first.ExpiresAt != want {
		t.Fatalf("40m expires_at=%q, want %q", first.ExpiresAt, want)
	}
	wantCloneURL := registry.BaseURL("repos") + "/git/code/demo.git"
	if first.CloneURL != wantCloneURL {
		t.Fatalf("clone_url=%q, want %q", first.CloneURL, wantCloneURL)
	}
	second, response := mintRunTokenRequest(t, service, runTokenRequest{Kind: "code", Name: "demo", TTL: "3h"})
	if response.Code != http.StatusOK {
		t.Fatalf("second mint status=%d body=%q, want 200", response.Code, response.Body.String())
	}
	if want := clock.now.Add(3 * time.Hour).Format(time.RFC3339Nano); second.ExpiresAt != want {
		t.Fatalf("3h expires_at=%q, want %q", second.ExpiresAt, want)
	}

	for _, result := range []runTokenResponse{first, second} {
		digest := sha256.Sum256([]byte(result.Token))
		wantHash := hex.EncodeToString(digest[:])
		var columns [5]string
		if err := conn.QueryRow(`SELECT token_sha256, kind, name, expires_at, created_at FROM run_tokens WHERE token_sha256 = ?`, wantHash).Scan(
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
	}

	_, missing := mintRunTokenRequest(t, service, runTokenRequest{Kind: "code", Name: "missing", TTL: "40m"})
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
	handler := appserver.LoopbackOnly(RunTokenHandler(service))

	request := httptest.NewRequest(http.MethodPost, "/run-token", bytes.NewBufferString(`{"kind":"scripts","name":"worker","ttl":"1h"}`))
	request.Header.Set("X-Forwarded-Proto", "https")
	crossed := httptest.NewRecorder()
	handler.ServeHTTP(crossed, request)
	if crossed.Code != http.StatusNotFound {
		t.Fatalf("crossed mint status=%d body=%q, want 404", crossed.Code, crossed.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/run-token", bytes.NewBufferString(`{"kind":"scripts","name":"worker","ttl":"1h"}`))
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

func TestRunTokenRejectsInvalidNonPositiveAndOmittedTTLWithoutMinting(t *testing.T) {
	// R-382O-OKJI
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

	for _, ttl := range []string{"banana", "-5m", "0s", ""} {
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
	return mintRunTokenRequest(t, service, runTokenRequest{Kind: kind, Name: name, TTL: "1h"})
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
