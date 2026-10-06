package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// ServiceName identifies the broker in the services file.
const ServiceName = "events"

// EmitPath is the broker's ingest endpoint.
const EmitPath = "/emit"

// EventsPath is a consumer's delivery endpoint.
const EventsPath = "/events"

// DeclarationsPath is a service's declaration endpoint.
const DeclarationsPath = "/declarations"

// MaxEventBytes bounds canonical and incoming event bodies.
const MaxEventBytes = 65536

// NewSocketSink discovers the broker afresh for each delivery.
func NewSocketSink() Sink { return socketSink{} }

type socketSink struct{}

func (socketSink) Deliver(ctx context.Context, e Event) error {
	if !validEmitted(e) {
		return fmt.Errorf("%w: invalid emitted event", ErrRejected)
	}
	body, err := e.MarshalJSON()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRejected, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := os.Getenv(services.Variable)
	if path == "" {
		return errors.New("events: services path is empty")
	}
	list, err := services.Read(path)
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if err != nil {
		return err
	}
	entry, ok := list.Find(ServiceName)
	if !ok {
		return errors.New("events: service is absent")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", entry.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+ServiceName+EmitPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return nil
	}
	if response.StatusCode >= 400 && response.StatusCode <= 499 {
		return fmt.Errorf("%w: HTTP %d", ErrRejected, response.StatusCode)
	}
	return fmt.Errorf("events: HTTP %d", response.StatusCode)
}

// EmitHandler validates producer events before recording them with sink.
func EmitHandler(sink Sink) http.Handler {
	if sink == nil {
		panic("events: sink is nil")
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
		var event Event
		if err != nil || event.UnmarshalJSON(body) != nil || !validEmitted(event) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		canonical, err := event.MarshalJSON()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(canonical) > MaxEventBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		err = sink.Deliver(r.Context(), event)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrRejected):
			w.WriteHeader(http.StatusUnprocessableEntity)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
}
