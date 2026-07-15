package dropbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// WriteHandler serves the loopback PUT and DELETE /content mutations.
func (s *Service) WriteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		switch r.Method {
		case http.MethodPut:
			row, err := s.Write(context.Background(), path, r.Body, r.Header.Get("X-Client-Id"))
			if err != nil {
				writeMutationError(w, s.Logger, "PUT /content", err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"path": row.Path, "size": row.Size, "content_hash": row.ContentHash, "rev": row.Rev})
		case http.MethodDelete:
			if _, err := s.Delete(context.Background(), path, r.Header.Get("X-Client-Id")); err != nil {
				writeMutationError(w, s.Logger, "DELETE /content", err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// MkdirHandler serves POST /mkdir.
func (s *Service) MkdirHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Mkdir(context.Background(), r.URL.Query().Get("path"), r.Header.Get("X-Client-Id")); err != nil {
			writeMutationError(w, s.Logger, "POST /mkdir", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// MoveHandler serves POST /move.
func (s *Service) MoveHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Move(context.Background(), r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.Header.Get("X-Client-Id")); err != nil {
			writeMutationError(w, s.Logger, "POST /move", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// StatHandler serves GET /stat for either indexed entry kind.
func (s *Service) StatHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry, err := s.Stat(r.URL.Query().Get("path"))
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.loopbackEntry(entry))
	})
}

// loopbackEntry renders the shared metadata shape for /stat and /list. Only
// loopback callers receive content_url; MCP deliberately renders its own shape.
func (s *Service) loopbackEntry(entry Entry) map[string]any {
	out := map[string]any{
		"path":         entry.Path,
		"kind":         entry.Kind,
		"size":         entry.Size,
		"content_hash": entry.ContentHash,
		"rev":          entry.Rev,
		"updated_at":   entry.UpdatedAt,
	}
	if entry.Kind == KindFile && s.ContentBase != "" {
		out["content_url"] = contentURL(s.ContentBase, entry.Path)
	}
	return out
}

func writeMutationError(w http.ResponseWriter, logger *slog.Logger, route string, err error) {
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrPathEscape) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if errors.Is(err, ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("dropbox mutation failed", "route", route, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
