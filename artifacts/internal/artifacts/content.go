package artifacts

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"

	"registry"
)

// ContentRouter is the chassis surface needed to mount the private content plane.
type ContentRouter interface {
	HandleLoopback(pattern string, h http.Handler)
}

// Reference identifies immutable artifact bytes on the content plane.
type Reference struct {
	ContentURL  string `json:"content_url"`
	ContentHash string `json:"content_hash"`
	Size        int64  `json:"size"`
}

// Reference returns the content-plane reference for an artifact.
func (s *Service) Reference(artifact Artifact) Reference {
	return Reference{
		ContentURL:  registry.BaseURL("artifacts") + "/content?id=" + artifact.ID,
		ContentHash: artifact.ContentHash,
		Size:        artifact.Size,
	}
}

// MountContent mounts the holder endpoint behind the chassis loopback guard.
func (s *Service) MountContent(rt ContentRouter) {
	rt.HandleLoopback("GET /content", s.ContentHandler())
}

// ContentHandler streams immutable bytes without applying human-surface visibility.
func (s *Service) ContentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		artifact, err := s.Store.GetArtifact(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		blob, err := s.Blobs.Open(id)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer blob.Close()

		contentType := mime.TypeByExtension(filepath.Ext(artifact.Filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", stringInt64(artifact.Size))
		_, _ = io.Copy(w, blob)
	})
}
