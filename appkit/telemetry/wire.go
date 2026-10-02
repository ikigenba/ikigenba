package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// ServiceName is the services-file entry for the event store.
const ServiceName = "telemetry"

// IngestPath is the event delivery endpoint.
const IngestPath = "/ingest"

// MaxEventBytes bounds an ingest request body.
const MaxEventBytes = 65536

// NewSocketSink returns a sink that discovers telemetry on each delivery.
func NewSocketSink() Sink { return socketSink{} }

type socketSink struct{}

func (socketSink) Deliver(ctx context.Context, e Event) error {
	body, err := e.MarshalJSON()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRejected, err)
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	path := os.Getenv(services.Variable)
	if path == "" {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return errors.New("telemetry services path is empty")
	}
	list, err := services.Read(path)
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	entry, ok := list.Find(ServiceName)
	if !ok {
		return errors.New("telemetry service is absent")
	}
	transport := SocketTransport(entry.Socket)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://telemetry"+IngestPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode >= 400 && response.StatusCode <= 499 {
		return fmt.Errorf("%w: HTTP %d", ErrRejected, response.StatusCode)
	}
	return fmt.Errorf("telemetry: HTTP %d", response.StatusCode)
}

// IngestHandler validates an event before handing it to the store sink.
func IngestHandler(sink Sink) http.Handler {
	if sink == nil {
		panic("telemetry: sink is nil")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxEventBytes+1))
		if len(body) > MaxEventBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		event, err := decodeWireEvent(body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := sink.Deliver(r.Context(), event); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

const wireTimeLayout = "2006-01-02T15:04:05.000000Z"

func decodeWireEvent(body []byte) (Event, error) {
	if !utf8.Valid(body) {
		return Event{}, errors.New("invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	members, err := decodeWireObject(decoder)
	if err != nil {
		return Event{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Event{}, errors.New("extra JSON text")
	}
	if len(members) != 6 {
		return Event{}, errors.New("wrong event members")
	}
	var event Event
	timestamp, ok := members["time"].(string)
	if !ok {
		return Event{}, errors.New("invalid time")
	}
	event.Time, err = time.Parse(wireTimeLayout, timestamp)
	if err != nil || event.Time.Format(wireTimeLayout) != timestamp {
		return Event{}, errors.New("invalid time")
	}
	event.Time = event.Time.UTC()
	event.Service, ok = members["service"].(string)
	if !ok || event.Service == "" {
		return Event{}, errors.New("invalid service")
	}
	event.Name, ok = members["event"].(string)
	if !ok || !validEventName(event.Name) {
		return Event{}, errors.New("invalid event name")
	}
	event.RequestID, ok = members["request_id"].(string)
	if !ok {
		return Event{}, errors.New("invalid request id")
	}
	event.User, ok = members["user"].(string)
	if !ok {
		return Event{}, errors.New("invalid user")
	}
	attrs, ok := members["attrs"].(map[string]any)
	if !ok {
		return Event{}, errors.New("invalid attrs")
	}
	event.Attrs = make(Attrs, len(attrs))
	for key, value := range attrs {
		if !validAttributeKey(key) {
			return Event{}, errors.New("invalid attribute key")
		}
		switch value := value.(type) {
		case string:
			event.Attrs[key] = value
		case bool:
			event.Attrs[key] = value
		case json.Number:
			decoded, err := decodeWireNumber(string(value))
			if err != nil {
				return Event{}, err
			}
			event.Attrs[key] = decoded
		default:
			return Event{}, errors.New("invalid attribute value")
		}
	}
	return event, nil
}

// Tokens preserve decoded key identity and detect duplicates before a map
// can overwrite them. Nested objects are decoded for attrs and rejected as values.
func decodeWireObject(decoder *json.Decoder) (map[string]any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("expected object")
	}
	members := make(map[string]any)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("expected member name")
		}
		if _, exists := members[key]; exists {
			return nil, errors.New("duplicate member")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		nested := json.NewDecoder(bytes.NewReader(raw))
		nested.UseNumber()
		var value any
		if len(raw) > 0 && raw[0] == '{' {
			value, err = decodeWireObject(nested)
		} else {
			err = nested.Decode(&value)
		}
		if err != nil {
			return nil, err
		}
		members[key] = value
	}
	_, err = decoder.Token()
	return members, err
}

func decodeWireNumber(text string) (any, error) {
	if text == "-0" {
		return math.Copysign(0, -1), nil
	}
	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		return value, nil
	}
	if value, err := strconv.ParseUint(text, 10, 64); err == nil {
		return value, nil
	}
	return strconv.ParseFloat(text, 64)
}
