// Package ingest receives telemetry batches from loopback reporters.
package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"appkit"
	"telemetry/internal/db"
	"telemetry/internal/record"
	telemetrytime "telemetry/internal/telemetry"
)

const (
	// Path is the loopback ingest route and the exact path excluded from request telemetry.
	Path      = "/ingest"
	bodyLimit = 8 << 20
)

// Request is the reporter's ingest envelope.
type Request struct {
	Records []record.Record `json:"records"`
	Dropped int64           `json:"dropped,omitempty"`
}

// Response reports the independently accepted and rejected records.
type Response struct {
	Stored   int `json:"stored"`
	Rejected int `json:"rejected"`
}

// Mount registers the loopback-only ingest route on the chassis router.
func Mount(rt *appkit.Router, store *db.Store, clock telemetrytime.Clock) {
	handler := &handler{store: store, clock: clock, logger: rt.Logger()}
	rt.HandleLoopback("POST "+Path, http.HandlerFunc(handler.serveHTTP))
}

type handler struct {
	store  *db.Store
	clock  telemetrytime.Clock
	logger interface {
		Warn(msg string, args ...any)
		Error(msg string, args ...any)
	}
}

type wireRequest struct {
	Records json.RawMessage `json:"records"`
	Dropped int64           `json:"dropped,omitempty"`
}

func (h *handler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	wire, err := decodeEnvelope(w, r)
	if err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return
	}

	var elements []json.RawMessage
	if err := json.Unmarshal(wire.Records, &elements); err != nil || elements == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	valid := make([]record.Record, 0, len(elements))
	rejected := 0
	service := ""
	for index, raw := range elements {
		var item record.Record
		if err := json.Unmarshal(raw, &item); err != nil {
			rejected++
			h.warnRejected(recordID(raw), validationField(err))
			continue
		}
		if index == 0 {
			service = item.Service
		}
		if err := item.Validate(); err != nil {
			rejected++
			h.warnRejected(item.ID, validationField(err))
			continue
		}
		valid = append(valid, item)
	}

	now := h.clock.Now()
	stored := 0
	if len(valid) != 0 {
		stored, err = h.store.InsertRecords(r.Context(), valid, now)
		if err != nil {
			h.logger.Error("store telemetry batch", "error", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
	if err := h.store.NoteDropped(r.Context(), service, wire.Dropped, now); err != nil {
		h.logger.Error("store telemetry dropped count", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(Response{Stored: stored, Rejected: rejected})
}

func decodeEnvelope(w http.ResponseWriter, r *http.Request) (wireRequest, error) {
	reader := http.MaxBytesReader(w, r.Body, bodyLimit)
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	var wire wireRequest
	if err := decoder.Decode(&wire); err != nil {
		return wireRequest{}, err
	}
	if len(wire.Records) == 0 {
		return wireRequest{}, errors.New("records is required")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return wireRequest{}, errors.New("multiple JSON values")
		}
		return wireRequest{}, err
	}
	trimmed := bytes.TrimSpace(wire.Records)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return wireRequest{}, errors.New("records must be an array")
	}
	return wire, nil
}

func (h *handler) warnRejected(id, field string) {
	h.logger.Warn("reject telemetry record", "id", id, "field", field)
}

func recordID(raw json.RawMessage) string {
	var identity struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &identity)
	return identity.ID
}

func validationField(err error) string {
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) && typeError.Field != "" {
		return typeError.Field
	}
	message := err.Error()
	for _, field := range []string{"id", "time", "correlation_id", "service", "kind", "op", "params", "detail"} {
		if strings.HasPrefix(message, field+" ") || strings.HasPrefix(message, field+" must") {
			return field
		}
	}
	if strings.HasPrefix(message, "unknown kind") {
		return "kind"
	}
	return "record"
}
