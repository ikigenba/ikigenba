package artifacts

import (
	"appkit"
	"artifacts/internal/db"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const uploadTTL = 24 * time.Hour

// ValidationError reports caller input that cannot be used to mint an upload.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Upload is a pending upload together with the command-ready public link.
type Upload struct {
	db.Upload
	URL  string
	Curl string
}

// Service owns upload-link minting and the public upload ingress.
type Service struct {
	Store          *db.Store
	Blobs          *BlobStore
	Outbox         EventSink
	Clock          func() time.Time
	MaxUploadBytes int64
	BaseURL        string
}

// NewService builds the upload service with its real database and filesystem substrates.
func NewService(store *db.Store, blobs *BlobStore, clock func() time.Time, maxUploadBytes int64, baseURL string) *Service {
	if baseURL == "" {
		panic("artifacts: empty base URL")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		Store:          store,
		Blobs:          blobs,
		Clock:          clock,
		MaxUploadBytes: maxUploadBytes,
		BaseURL:        baseURL,
	}
}

// MintUpload creates a single-use upload capability with the contractual 24-hour lifetime.
func (s *Service) MintUpload(ctx context.Context, id appkit.Identity, filename, visibility, description string) (Upload, error) {
	if err := ValidateFilename(filename); err != nil {
		return Upload{}, &ValidationError{Message: err.Error()}
	}
	if visibility != "public" && visibility != "private" {
		return Upload{}, &ValidationError{Message: "visibility must be public or private"}
	}
	now := s.now()
	if _, err := s.Store.PurgeExpiredUploads(ctx, now); err != nil {
		return Upload{}, fmt.Errorf("purge expired uploads: %w", err)
	}
	pending, err := s.Store.CreateUpload(ctx, db.CreateUploadParams{
		OwnerID: id.OwnerID, OwnerEmail: id.OwnerEmail, Filename: filename,
		Description: description, Visibility: visibility, CreatedAt: now,
		ExpiresAt: now.Add(uploadTTL),
	})
	if err != nil {
		return Upload{}, err
	}
	link := s.BaseURL + "u/" + pending.Token
	return Upload{Upload: pending, URL: link, Curl: "curl -T " + shellQuote(filename) + " " + shellQuote(link)}, nil
}

// DownloadURL renders the configured front-door URL for an artifact's visibility tier.
func (s *Service) DownloadURL(id, filename, visibility string) string {
	tier := "p"
	if visibility == "public" {
		tier = "f"
	}
	return s.BaseURL + tier + "/" + url.PathEscape(id) + "/" + url.PathEscape(filename)
}

// UploadHandler returns the bare, token-authenticated PUT ingress.
func (s *Service) UploadHandler() http.Handler {
	return http.HandlerFunc(s.handleUpload)
}

func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	for _, name := range []string{"X-Owner-Id", "X-Owner-Email", "X-Client-Id"} {
		if _, present := r.Header[http.CanonicalHeaderKey(name)]; present {
			http.NotFound(w, r)
			return
		}
	}
	token := strings.TrimPrefix(r.URL.Path, "/u/")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	pending, err := s.Store.GetUpload(r.Context(), token)
	now := s.now()
	if err != nil || pending.ConsumedAt != nil || !pending.ExpiresAt.After(now) {
		http.NotFound(w, r)
		return
	}

	artifactID := NewToken()
	written, digest, status := s.storeBody(w, r, artifactID)
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	artifact, won, err := s.Store.CommitUploadWithEvent(r.Context(), token, artifactID, db.CreateArtifactParams{
		OwnerID: pending.OwnerID, OwnerEmail: pending.OwnerEmail, Filename: pending.Filename,
		Description: pending.Description, Visibility: pending.Visibility, Size: written,
		ContentHash: digest, CreatedAt: now,
	}, now, s.createdEvent)
	if err != nil || !won {
		_ = s.Blobs.Remove(artifactID)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	if s.Outbox != nil {
		s.Outbox.Ring()
	}

	response := struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		Size        int64  `json:"size"`
		ContentHash string `json:"content_hash"`
		URL         string `json:"url"`
	}{artifact.ID, pending.Filename, written, digest, s.DownloadURL(artifact.ID, pending.Filename, pending.Visibility)}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Service) storeBody(w http.ResponseWriter, r *http.Request, artifactID string) (written int64, digest string, status int) {
	writer, err := s.Blobs.Create(artifactID)
	if err != nil {
		return 0, "", http.StatusInternalServerError
	}
	abort := func() {
		if temporary, ok := writer.(*blobWriter); ok {
			temporary.abort()
		} else {
			_ = writer.Close()
			_ = s.Blobs.Remove(artifactID)
		}
	}
	hasher := sha256.New()
	limited := http.MaxBytesReader(w, r.Body, s.MaxUploadBytes)
	written, err = io.Copy(io.MultiWriter(writer, hasher), limited)
	if err != nil {
		abort()
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return 0, "", http.StatusRequestEntityTooLarge
		}
		return 0, "", http.StatusInternalServerError
	}
	if err := writer.Close(); err != nil {
		return 0, "", http.StatusInternalServerError
	}
	return written, hex.EncodeToString(hasher.Sum(nil)), 0
}

func (s *Service) now() time.Time { return s.Clock().UTC() }

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
