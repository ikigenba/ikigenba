package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"appkit"
	appkitdb "appkit/db"
	domainDB "artifacts/internal/db"
)

// R-3MZ0-3QNE
func TestMintUploadValidatesAndReturnsTwentyFourHourLink(t *testing.T) {
	fx := newUploadFixture(t, 64)
	upload, err := fx.service.MintUpload(context.Background(), testIdentity(), "report.txt", "private", "quarterly")
	if err != nil {
		t.Fatal(err)
	}
	if upload.Token == "" || !upload.ExpiresAt.Equal(fx.now.Add(24*time.Hour)) {
		t.Fatalf("minted upload = %#v; want token and exact 24h expiry", upload)
	}
	wantSuffix := "/srv/artifacts/u/" + upload.Token
	if got := upload.URL[len(upload.URL)-len(wantSuffix):]; got != wantSuffix {
		t.Fatalf("upload URL = %q, want suffix %q", upload.URL, wantSuffix)
	}
	before := rowCount(t, fx.conn, "uploads")
	for _, input := range []struct{ filename, visibility string }{
		{filename: string(bytes.Repeat([]byte("x"), 129)), visibility: "private"},
		{filename: "valid.txt", visibility: "shared"},
	} {
		_, err := fx.service.MintUpload(context.Background(), testIdentity(), input.filename, input.visibility, "")
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("MintUpload(%d-byte name, %q) error = %v, want ValidationError", len(input.filename), input.visibility, err)
		}
	}
	if got := rowCount(t, fx.conn, "uploads"); got != before {
		t.Fatalf("invalid mints changed upload rows from %d to %d", before, got)
	}
}

// R-3O6W-HIE3
func TestUploadIngressPersistsExactBytesAndArtifactFacts(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	pending := fx.mint(t, "evidence.bin", "public")
	body := []byte{0, 1, 2, 3, 0xff, 'x', 'y'}
	response := fx.put(t, pending.Token, body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d, body %q", response.Code, response.Body.String())
	}
	var got uploadResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(body)
	if got.ID == "" || got.Filename != "evidence.bin" || got.Size != int64(len(body)) || got.ContentHash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("response = %#v", got)
	}
	artifact, err := fx.store.GetArtifact(context.Background(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.OwnerID != "owner-1" || artifact.OwnerEmail != "owner@example.com" || artifact.Filename != "evidence.bin" || artifact.Visibility != "public" || artifact.Size != int64(len(body)) || artifact.ContentHash != got.ContentHash {
		t.Fatalf("artifact row = %#v", artifact)
	}
	file, err := fx.blobs.Open(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, readErr := io.ReadAll(file)
	_ = file.Close()
	if readErr != nil || !bytes.Equal(stored, body) {
		t.Fatalf("blob = %v, %v; want %v", stored, readErr, body)
	}
}

// R-3PES-VA4S
func TestUploadTokenFailuresAreUniformAndCreateNothing(t *testing.T) {
	fx := newUploadFixture(t, 64)
	consumed := fx.mint(t, "once.txt", "private")
	if got := fx.put(t, consumed.Token, []byte("first"), nil); got.Code != http.StatusCreated {
		t.Fatalf("initial PUT = %d: %s", got.Code, got.Body.String())
	}
	expired := fx.mint(t, "expired.txt", "private")
	fx.now = fx.now.Add(25 * time.Hour)
	before := rowCount(t, fx.conn, "artifacts")
	responses := []*httptest.ResponseRecorder{
		fx.put(t, consumed.Token, []byte("again"), nil),
		fx.put(t, expired.Token, []byte("late"), nil),
		fx.put(t, "does-not-exist", []byte("probe"), nil),
	}
	for i, response := range responses {
		if response.Code != http.StatusNotFound {
			t.Errorf("failure %d status = %d, want 404", i, response.Code)
		}
		if response.Body.String() != responses[0].Body.String() {
			t.Errorf("failure %d body = %q, want byte-identical %q", i, response.Body.String(), responses[0].Body.String())
		}
	}
	if got := rowCount(t, fx.conn, "artifacts"); got != before {
		t.Fatalf("failed token attempts changed artifact count from %d to %d", before, got)
	}
}

// R-3QMP-91VH
func TestUploadCapRejectsWithoutConsumingAndIncludesBoundary(t *testing.T) {
	const capBytes = 8
	fx := newUploadFixture(t, capBytes)
	oversized := fx.mint(t, "retry.bin", "private")
	response := fx.put(t, oversized.Token, bytes.Repeat([]byte("a"), capBytes+1), nil)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PUT = %d, body %q", response.Code, response.Body.String())
	}
	if got := rowCount(t, fx.conn, "artifacts"); got != 0 {
		t.Fatalf("oversized PUT left %d artifact rows", got)
	}
	entries, err := os.ReadDir(filepath.Join(fx.blobs.Root, "blobs"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized PUT left blob files: %v", entries)
	}
	if retry := fx.put(t, oversized.Token, []byte("small"), nil); retry.Code != http.StatusCreated {
		t.Fatalf("retry PUT = %d: %s", retry.Code, retry.Body.String())
	}
	exact := fx.mint(t, "exact.bin", "private")
	if boundary := fx.put(t, exact.Token, bytes.Repeat([]byte("b"), capBytes), nil); boundary.Code != http.StatusCreated {
		t.Fatalf("exact-cap PUT = %d: %s", boundary.Code, boundary.Body.String())
	}
}

// R-3RUL-MTM6
func TestUploadIngressRejectsNonPutRegardlessOfToken(t *testing.T) {
	fx := newUploadFixture(t, 64)
	valid := fx.mint(t, "method.txt", "private")
	for _, token := range []string{valid.Token, "unknown"} {
		request := httptest.NewRequest(http.MethodPost, "/u/"+token, nil)
		response := httptest.NewRecorder()
		fx.service.UploadHandler().ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST token %q status = %d, want 405", token, response.Code)
		}
	}
}

// R-3T2I-0LCV
func TestUploadIngressRejectsIdentityHeadersButAllowsForwardedProto(t *testing.T) {
	for _, header := range []string{"X-Owner-Id", "X-Owner-Email", "X-Client-Id"} {
		t.Run(header, func(t *testing.T) {
			fx := newUploadFixture(t, 64)
			pending := fx.mint(t, "headers.txt", "private")
			rejected := fx.put(t, pending.Token, []byte("blocked"), map[string]string{header: ""})
			if rejected.Code != http.StatusNotFound {
				t.Fatalf("identity-bearing PUT = %d, want 404", rejected.Code)
			}
			accepted := fx.put(t, pending.Token, []byte("accepted"), map[string]string{"X-Forwarded-Proto": "https"})
			if accepted.Code != http.StatusCreated {
				t.Fatalf("forwarded-proto-only PUT = %d: %s", accepted.Code, accepted.Body.String())
			}
		})
	}
}

// R-3UAE-ED3K
func TestConcurrentUploadsHaveExactlyOneWinner(t *testing.T) {
	fx := newUploadFixture(t, 1024)
	pending := fx.mint(t, "race.bin", "private")
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, body := range [][]byte{[]byte("contender one"), []byte("contender two")} {
		wg.Add(1)
		go func(body []byte) {
			defer wg.Done()
			<-start
			response := fx.put(t, pending.Token, body, nil)
			statuses <- response.Code
		}(body)
	}
	close(start)
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusNotFound] != 1 {
		t.Fatalf("concurrent statuses = %v, want one 201 and one 404", counts)
	}
	if got := rowCount(t, fx.conn, "artifacts"); got != 1 {
		t.Fatalf("artifact rows = %d, want 1", got)
	}
	entries, err := os.ReadDir(filepath.Join(fx.blobs.Root, "blobs"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("blob entries = %v, %v; want exactly one", entries, err)
	}
}

// R-3VIA-S4U9
func TestMintPurgesOnlyExpiredUnconsumedUpload(t *testing.T) {
	fx := newUploadFixture(t, 64)
	expired := fx.mint(t, "old.txt", "private")
	fx.now = fx.now.Add(time.Hour)
	future := fx.mint(t, "future.txt", "private")
	consumed := fx.mint(t, "audit.txt", "private")
	if won, err := fx.store.ConsumeUpload(context.Background(), consumed.Token, "audit-artifact", fx.now); err != nil || !won {
		t.Fatalf("seed consumed upload = %v, %v", won, err)
	}
	fx.now = expired.ExpiresAt
	created := fx.mint(t, "new.txt", "public")
	if _, err := fx.store.GetUpload(context.Background(), expired.Token); err != sql.ErrNoRows {
		t.Fatalf("expired upload lookup = %v, want sql.ErrNoRows", err)
	}
	for _, token := range []string{future.Token, consumed.Token, created.Token} {
		if _, err := fx.store.GetUpload(context.Background(), token); err != nil {
			t.Errorf("preserved/created token %q missing: %v", token, err)
		}
	}
}

type uploadFixture struct {
	conn    *sql.DB
	store   *domainDB.Store
	blobs   *BlobStore
	service *Service
	now     time.Time
	router  *downloadRouter
}

type uploadResponse struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
	URL         string `json:"url"`
}

func newUploadFixture(t *testing.T, capBytes int64) *uploadFixture {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(domainDB.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	n := 0
	store := domainDB.NewStore(conn, func() string {
		n++
		return fmt.Sprintf("generated-%d", n)
	})
	fx := &uploadFixture{conn: conn, store: store, blobs: &BlobStore{Root: t.TempDir()}, now: time.Date(2026, 8, 10, 12, 0, 0, 123, time.UTC)}
	fx.service = NewService(store, fx.blobs, func() time.Time { return fx.now }, capBytes)
	fx.service.UploadOrigin = "https://files.example.test"
	return fx
}

func (fx *uploadFixture) mint(t *testing.T, filename, visibility string) Upload {
	t.Helper()
	upload, err := fx.service.MintUpload(context.Background(), testIdentity(), filename, visibility, "description")
	if err != nil {
		t.Fatal(err)
	}
	return upload
}

func (fx *uploadFixture) put(t *testing.T, token string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/u/"+token, bytes.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	fx.service.UploadHandler().ServeHTTP(response, request)
	return response
}

func rowCount(t *testing.T, conn *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func testIdentity() appkit.Identity {
	return appkit.Identity{OwnerID: "owner-1", OwnerEmail: "owner@example.com"}
}
