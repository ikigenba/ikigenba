package repos

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func PutContentHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := readCommitBody(w, request, service.maxCommitBytes)
		if err != nil {
			writeReadError(w, err)
			return
		}
		query := request.URL.Query()
		result, err := service.PutContent(request.Context(), query.Get("kind"), query.Get("name"), query.Get("path"), query.Get("message"), query.Get("actor"), query.Get("rev"), body)
		if err != nil {
			writeReadError(w, err)
			return
		}
		writeJSON(w, result)
	})
}

func DeleteContentHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		result, err := service.DeleteContent(request.Context(), query.Get("kind"), query.Get("name"), query.Get("path"), query.Get("message"), query.Get("actor"))
		if err != nil {
			writeReadError(w, err)
			return
		}
		writeJSON(w, result)
	})
}

func CommitHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := readCommitBody(w, request, service.maxCommitBytes)
		if err != nil {
			writeReadError(w, err)
			return
		}
		var input CommitBatchRequest
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeReadError(w, fmt.Errorf("%w: invalid commit body: %v", ErrValidation, err))
			return
		}
		changes := make([]Change, 0, len(input.Changes))
		for _, change := range input.Changes {
			decoded := []byte(nil)
			if change.Op == "put" {
				decoded, err = base64.StdEncoding.DecodeString(change.ContentB64)
				if err != nil {
					writeReadError(w, fmt.Errorf("%w: invalid base64 for %q", ErrValidation, change.Path))
					return
				}
			}
			changes = append(changes, Change{Op: change.Op, Path: change.Path, Content: decoded})
		}
		result, err := service.CommitBatch(request.Context(), input.Kind, input.Name, input.Message, input.Actor, input.ParentRev, changes)
		if err != nil {
			writeReadError(w, err)
			return
		}
		writeJSON(w, result)
	})
}

func readCommitBody(w http.ResponseWriter, request *http.Request, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("repos: max commit bytes is not configured")
	}
	reader := http.MaxBytesReader(w, request.Body, limit)
	body, err := io.ReadAll(reader)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, fmt.Errorf("%w: request body exceeds %d bytes", ErrTooLarge, limit)
		}
		return nil, err
	}
	return body, nil
}
