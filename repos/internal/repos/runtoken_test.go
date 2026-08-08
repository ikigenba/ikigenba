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
	body, err := json.Marshal(runTokenRequest{Kind: kind, Name: name})
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
