package artifacts

import (
	"appkit"
	"artifacts/internal/db"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"registry"
	"strconv"
	"time"
)

const (
	ImportSourceUnavailable = "source_unavailable"
	ImportTooLarge          = "too_large"
)

// Artifact is the stored artifact returned by service operations.
type Artifact = db.Artifact

// ImportError reports a stable import failure category.
type ImportError struct {
	Code string
	Err  error
}

func (e *ImportError) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *ImportError) Unwrap() error { return e.Err }

// Import confines a source URL to the registered loopback plane and stores its bytes.
func (s *Service) Import(ctx context.Context, identity appkit.Identity, sourceURL, filename, visibility, description string) (Artifact, error) {
	if err := ValidateFilename(filename); err != nil {
		return Artifact{}, &ValidationError{Message: err.Error()}
	}
	if visibility != "public" && visibility != "private" {
		return Artifact{}, &ValidationError{Message: "visibility must be public or private"}
	}
	parsed, err := validateImportURL(sourceURL)
	if err != nil {
		return Artifact{}, &ValidationError{Message: err.Error()}
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			_, err := validateImportURL(req.URL.String())
			return err
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), http.NoBody)
	if err != nil {
		return Artifact{}, &ImportError{Code: ImportSourceUnavailable, Err: err}
	}
	response, err := client.Do(request)
	if err != nil {
		return Artifact{}, &ImportError{Code: ImportSourceUnavailable, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Artifact{}, &ImportError{Code: ImportSourceUnavailable, Err: fmt.Errorf("source returned %s", response.Status)}
	}

	artifactID := NewToken()
	written, digest, err := s.storeImportBody(response.Body, artifactID)
	if err != nil {
		return Artifact{}, err
	}
	artifact, err := s.Store.CreateArtifactWithEvent(ctx, db.CreateArtifactParams{
		ID: artifactID, OwnerID: identity.OwnerID, OwnerEmail: identity.OwnerEmail,
		Filename: filename, Description: description, Visibility: visibility,
		Size: written, ContentHash: digest, CreatedAt: s.now(),
	}, s.createdEvent)
	if err != nil {
		_ = s.Blobs.Remove(artifactID)
		return Artifact{}, err
	}
	if s.Outbox != nil {
		s.Outbox.Ring()
	}
	return artifact, nil
}

func (s *Service) storeImportBody(body io.Reader, artifactID string) (written int64, digest string, err error) {
	writer, err := s.Blobs.Create(artifactID)
	if err != nil {
		return 0, "", err
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
	limited := http.MaxBytesReader(nil, io.NopCloser(body), s.MaxUploadBytes)
	written, copyErr := io.Copy(io.MultiWriter(writer, hasher), limited)
	if copyErr != nil {
		abort()
		var tooLarge *http.MaxBytesError
		if errors.As(copyErr, &tooLarge) {
			return 0, "", &ImportError{Code: ImportTooLarge, Err: copyErr}
		}
		return 0, "", &ImportError{Code: ImportSourceUnavailable, Err: copyErr}
	}
	if err := writer.Close(); err != nil {
		return 0, "", err
	}
	return written, hex.EncodeToString(hasher.Sum(nil)), nil
}

func validateImportURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "http" {
		return nil, fmt.Errorf("source_url must be an absolute http URL")
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		return nil, fmt.Errorf("source_url host must be loopback")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || !isRegistryPort(port) {
		return nil, fmt.Errorf("source_url port must be assigned by the registry")
	}
	return parsed, nil
}

func isRegistryPort(port int) bool {
	for _, service := range registry.Services {
		value := reflect.ValueOf(service)
		if value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		field := value.FieldByName("Port")
		if field.IsValid() && field.CanInt() && int(field.Int()) == port {
			return true
		}
	}
	return false
}
