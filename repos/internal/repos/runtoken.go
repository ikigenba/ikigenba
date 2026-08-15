package repos

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"registry"
	"time"
)

type runTokenRequest struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	TTL  string `json:"ttl,omitempty"`
}

type runTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	CloneURL  string `json:"clone_url"`
}

// RunTokenHandler mints a short-lived credential scoped to one live repository.
func RunTokenHandler(service *Service) http.Handler {
	return http.HandlerFunc(service.serveRunToken)
}

func (s *Service) serveRunToken(w http.ResponseWriter, request *http.Request) {
	if s == nil || s.store == nil || s.custody == nil {
		http.Error(w, "run token dependencies are not configured", http.StatusInternalServerError)
		return
	}
	key, requestedTTL, ok := decodeRunTokenRequest(w, request)
	if !ok || !s.runTokenRepositoryAvailable(w, request, key) {
		return
	}
	response, ok := s.mintRunToken(w, request, key, requestedTTL)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func decodeRunTokenRequest(w http.ResponseWriter, request *http.Request) (runTokenRequest, time.Duration, bool) {
	var key runTokenRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&key); err != nil {
		http.Error(w, fmt.Sprintf("%s: invalid run token body", ErrValidation), http.StatusBadRequest)
		return runTokenRequest{}, 0, false
	}
	requestedTTL, err := time.ParseDuration(key.TTL)
	if err != nil || requestedTTL <= 0 {
		http.Error(w, fmt.Sprintf("%s: invalid run token ttl", ErrValidation), http.StatusBadRequest)
		return runTokenRequest{}, 0, false
	}
	return key, requestedTTL, true
}

func (s *Service) runTokenRepositoryAvailable(w http.ResponseWriter, request *http.Request, key runTokenRequest) bool {
	if _, err := s.custody.Path(key.Kind, key.Name); err != nil {
		writeReadError(w, err)
		return false
	}
	repository, err := s.store.GetRepository(request.Context(), key.Kind, key.Name)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (repository.ArchivedAt != nil || !s.custody.Exists(key.Kind, key.Name))) {
		http.NotFound(w, request)
		return false
	}
	if err != nil {
		http.Error(w, "look up repository", http.StatusInternalServerError)
		return false
	}
	return true
}

func (s *Service) mintRunToken(w http.ResponseWriter, request *http.Request, key runTokenRequest, requestedTTL time.Duration) (runTokenResponse, bool) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "mint run token", http.StatusInternalServerError)
		return runTokenResponse{}, false
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	now := s.custody.Now()
	expiresAt := now.Add(requestedTTL)
	tx, err := s.store.BeginTx(request.Context())
	if err != nil {
		http.Error(w, "begin run token transaction", http.StatusInternalServerError)
		return runTokenResponse{}, false
	}
	defer tx.Rollback()
	if err := s.store.InsertRunToken(request.Context(), tx, RunToken{
		TokenSHA256: hex.EncodeToString(digest[:]), Kind: key.Kind, Name: key.Name,
		ExpiresAt: expiresAt, CreatedAt: now,
	}); err != nil {
		http.Error(w, "store run token", http.StatusInternalServerError)
		return runTokenResponse{}, false
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "commit run token", http.StatusInternalServerError)
		return runTokenResponse{}, false
	}
	return runTokenResponse{
		Token: token, ExpiresAt: expiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		CloneURL: registry.BaseURL("repos") + "/git/" + key.Kind + "/" + key.Name + ".git",
	}, true
}
