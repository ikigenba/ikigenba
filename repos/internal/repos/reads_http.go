package repos

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
)

func ContentHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		result, err := service.ReadContent(request.Context(), query.Get("kind"), query.Get("name"), query.Get("path"), query.Get("ref"), query.Get("rev"))
		if err != nil {
			writeReadError(w, err)
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(query.Get("path")))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", fmt.Sprint(len(result.Bytes)))
		w.Header().Set("X-Repos-Rev", result.Rev)
		w.Header().Set("X-Repos-Blob", result.BlobSHA)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Bytes)
	})
}

func ListHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		result, err := service.List(request.Context(), query.Get("kind"), query.Get("name"), query.Get("ref"), query.Get("path"))
		if err != nil {
			writeReadError(w, err)
			return
		}
		writeJSON(w, result)
	})
}

func StatHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		entry, rev, err := service.Stat(request.Context(), query.Get("kind"), query.Get("name"), query.Get("path"), query.Get("ref"))
		if err != nil {
			writeReadError(w, err)
			return
		}
		w.Header().Set("X-Repos-Rev", rev)
		writeJSON(w, entry)
	})
}

func ArchiveHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		result, err := service.ReadArchive(request.Context(), query.Get("kind"), query.Get("name"), query.Get("ref"))
		if err != nil {
			writeReadError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Content-Length", fmt.Sprint(len(result.Bytes)))
		w.Header().Set("X-Repos-Rev", result.Rev)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Bytes)
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func writeReadError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrConflict), errors.Is(err, ErrForcePush):
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}

func makeURLValues(kind, name, filePath, ref string) url.Values {
	return url.Values{"kind": {kind}, "name": {name}, "path": {filePath}, "ref": {ref}}
}
