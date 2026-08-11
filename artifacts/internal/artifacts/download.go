package artifacts

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DownloadRouter is the chassis surface needed to mount the public and private
// download tiers.
type DownloadRouter interface {
	Handle(pattern string, h http.Handler)
	RequireIdentity(h http.Handler) http.Handler
}

// MountDownloads mounts the anonymous public tier and identity-gated private tier.
func (s *Service) MountDownloads(rt DownloadRouter) {
	rt.Handle("/f/", s.PublicDownloadHandler())
	rt.Handle("/p/", rt.RequireIdentity(s.PrivateDownloadHandler()))
}

// PublicDownloadHandler serves public artifacts and rejects identity-bearing requests.
func (s *Service) PublicDownloadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"X-Owner-Id", "X-Owner-Email", "X-Client-Id"} {
			if _, present := r.Header[http.CanonicalHeaderKey(name)]; present {
				http.NotFound(w, r)
				return
			}
		}
		s.serveArtifact(w, r, "public", "/f/")
	})
}

// PrivateDownloadHandler serves private artifacts after the chassis identity gate.
func (s *Service) PrivateDownloadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveArtifact(w, r, "private", "/p/")
	})
}

func (s *Service) serveArtifact(w http.ResponseWriter, r *http.Request, wantVisibility, prefix string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, filename, ok := downloadPath(r, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	artifact, err := s.Store.GetArtifact(r.Context(), id)
	if err != nil || artifact.Filename != filename || artifact.Visibility != wantVisibility {
		http.NotFound(w, r)
		return
	}
	blob, err := s.Blobs.Open(id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer blob.Close()

	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Length", stringInt64(artifact.Size))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", attachmentDisposition(filename))
	if _, err := io.Copy(w, blob); err != nil {
		return
	}
	_, _ = s.Store.IncrementDownloadCount(r.Context(), id)
}

func downloadPath(r *http.Request, prefix string) (string, string, bool) {
	escaped := r.URL.EscapedPath()
	if !strings.HasPrefix(escaped, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(escaped, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil || id == "" || strings.ContainsAny(id, `/\`) {
		return "", "", false
	}
	filename, err := url.PathUnescape(parts[1])
	if err != nil || filename == "" || strings.Contains(filename, "/") {
		return "", "", false
	}
	return id, filename, true
}

func attachmentDisposition(filename string) string {
	if isASCII(filename) {
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filename)
		return `attachment; filename="` + escaped + `"`
	}
	return `attachment; filename="download"; filename*=UTF-8''` + url.PathEscape(filename)
}

func isASCII(value string) bool {
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r > 127 {
			return false
		}
		value = value[size:]
	}
	return true
}

func stringInt64(value int64) string { return strconv.FormatInt(value, 10) }
