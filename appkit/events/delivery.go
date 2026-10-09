package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Handler handles one delivery.
type Handler func(ctx context.Context, d Delivery) Outcome

// Handlers registers named or wildcard handlers.
type Handlers map[string]Handler

// Delivery is a broker event and its delivery attempt.
type Delivery struct {
	Event   Event
	Attempt int
}

// Outcome is a comparable handler result.
type Outcome struct {
	kind    string
	message string
}

// Outcome spellings and delivery limits.
const (
	OutcomeOK        = "ok"
	OutcomeSkip      = "skip"
	OutcomeError     = "error"
	MaxDeliveryBytes = 131072
)

// PanicMessage is the public copy for a recovered handler panic.
const PanicMessage string = "event handler panicked"

// OK marks a handled delivery.
func OK() Outcome { return Outcome{kind: OutcomeOK} }

// Skip marks an irrelevant delivery.
func Skip() Outcome { return Outcome{kind: OutcomeSkip} }

// Fail marks a failed delivery.
func Fail(msg string) Outcome { return Outcome{message: msg} }

// Kind reports the outcome wire spelling.
func (o Outcome) Kind() string {
	if o.kind == "" {
		return OutcomeError
	}
	return o.kind
}

// Message reports the failure message.
func (o Outcome) Message() string { return o.message }

func copyHandlers(h Handlers) Handlers {
	c := make(Handlers, len(h))
	for k, v := range h {
		if (k != "*" && !validEventPattern(k)) || v == nil {
			panic(fmt.Sprintf("events: invalid handler for %s", strconv.Quote(k)))
		}
		c[k] = v
	}
	return c
}

func selectHandler(handlers Handlers, name string) Handler {
	if h, ok := handlers[name]; ok {
		return h
	}
	best := ""
	stars := 0
	for key := range handlers {
		if !Match(key, name) {
			continue
		}
		n := strings.Count(key, "*")
		if best == "" || n < stars || n == stars && key < best {
			best, stars = key, n
		}
	}
	if best != "" {
		return handlers[best]
	}
	return handlers["*"]
}

func decodeDelivery(body []byte) (Delivery, error) {
	m, err := decodeObject(body)
	if err != nil {
		return Delivery{}, err
	}
	raw, ok := m["attempt"]
	if !ok {
		return Delivery{}, fmt.Errorf("events: missing attempt")
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < 1 {
		return Delivery{}, fmt.Errorf("events: invalid attempt")
	}
	delete(m, "attempt")
	b, err := json.Marshal(m)
	if err != nil {
		return Delivery{}, err
	}
	var e Event
	if err := e.UnmarshalJSON(b); err != nil {
		return Delivery{}, err
	}
	if !validDelivered(e) {
		return Delivery{}, fmt.Errorf("events: expected delivered event")
	}
	return Delivery{e, n}, nil
}
func callHandler(ctx context.Context, h Handler, d Delivery) (o Outcome) {
	defer func() {
		if recover() != nil {
			o = Fail(PanicMessage)
		}
	}()
	return h(ctx, d)
}
func writeOutcome(w http.ResponseWriter, o Outcome) {
	w.Header().Set("Content-Type", "application/json")
	if o.Kind() != OutcomeError {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"outcome":"`+o.Kind()+`"}`)
		return
	}
	msg := o.Message()
	if len(msg) > MaxEventBytes/8 {
		n := MaxEventBytes / 8
		for n > 0 && !utf8.RuneStart(msg[n]) {
			n--
		}
		msg = msg[:n]
	}
	b, _ := json.Marshal(struct {
		Outcome string `json:"outcome"`
		Error   string `json:"error"`
	}{OutcomeError, msg})
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(b)
}

// DeliveryHandler validates deliveries and invokes the selected handler.
func DeliveryHandler(h Handlers) http.Handler {
	handlers := copyHandlers(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, MaxDeliveryBytes+1))
		if len(b) > MaxDeliveryBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		d, err := decodeDelivery(b)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		handler := selectHandler(handlers, d.Event.Name)
		if handler == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ctx := NewContext(r.Context(), Cause{ID: d.Event.ID, Depth: d.Event.Depth})
		writeOutcome(w, callHandler(ctx, handler, d))
	})
}

// DeclarationsHandler publishes the service's emissions and acceptances.
func DeclarationsHandler(em *Emitter, h Handlers) http.Handler {
	handlers := copyHandlers(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		type emission struct {
			Event string   `json:"event"`
			Attrs []string `json:"attrs"`
		}
		out := struct {
			Emits   []emission `json:"emits"`
			Accepts []string   `json:"accepts"`
		}{make([]emission, 0), make([]string, 0, len(handlers))}
		if em != nil {
			for _, e := range em.Emits() {
				out.Emits = append(out.Emits, emission{e.Event, append([]string{}, e.Attrs...)})
			}
		}
		sort.SliceStable(out.Emits, func(i, j int) bool { return out.Emits[i].Event < out.Emits[j].Event })
		for name := range handlers {
			out.Accepts = append(out.Accepts, name)
		}
		sort.Strings(out.Accepts)
		b, _ := json.Marshal(out)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})
}
