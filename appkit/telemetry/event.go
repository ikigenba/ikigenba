// Package telemetry records the suite's event trail.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"time"
)

// Attrs holds an event's flat metadata.
type Attrs map[string]any

// Event is the shared envelope of one trail event.
type Event struct {
	Time                           time.Time
	Service, Name, RequestID, User string
	Attrs                          Attrs
}

var (
	eventNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*(\.[a-z][a-z0-9]*(_[a-z0-9]+)*)+$`)
	attributeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
)

func validEventName(name string) bool   { return eventNamePattern.MatchString(name) }
func validAttributeKey(key string) bool { return attributeKeyPattern.MatchString(key) }

func normalizeAttrs(attrs Attrs) (Attrs, error) {
	normalized := make(Attrs, len(attrs))
	for key, value := range attrs {
		if !validAttributeKey(key) {
			return nil, fmt.Errorf("invalid attribute key %q", key)
		}
		basic, err := basicAttribute(value)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", key, err)
		}
		normalized[key] = basic
	}
	return normalized, nil
}

func basicAttribute(value any) (any, error) {
	if value == nil {
		return nil, errors.New("nil attribute value")
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		number := v.Float()
		if !math.IsNaN(number) && !math.IsInf(number, 0) {
			return number, nil
		}
	}
	return nil, errors.New("invalid attribute value")
}

// MarshalJSON encodes the canonical event envelope, rejecting malformed events.
func (e Event) MarshalJSON() ([]byte, error) {
	if e.Service == "" {
		return nil, errors.New("empty event service")
	}
	if !validEventName(e.Name) {
		return nil, errors.New("invalid event name")
	}
	utc := e.Time.UTC()
	if utc.Year() < 0 || utc.Year() > 9999 {
		return nil, errors.New("event time outside supported years")
	}
	attrs, err := normalizeAttrs(e.Attrs)
	if err != nil {
		return nil, err
	}
	e.Attrs = attrs
	return marshalEnvelope(e)
}

func marshalEnvelope(e Event) ([]byte, error) {
	attrs := e.Attrs
	if attrs == nil {
		attrs = make(Attrs)
	}
	envelope := struct {
		Time      string         `json:"time"`
		Service   string         `json:"service"`
		Event     string         `json:"event"`
		RequestID string         `json:"request_id"`
		User      string         `json:"user"`
		Attrs     map[string]any `json:"attrs"`
	}{e.Time.UTC().Format("2006-01-02T15:04:05.000000Z"), e.Service, e.Name, e.RequestID, e.User, attrs}
	return json.Marshal(envelope)
}
