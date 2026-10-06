// Package events carries facts between services through the event bus.
package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

// Attrs holds flat event metadata.
type Attrs map[string]any

// Event is the envelope shared by producers, the broker, and consumers.
type Event struct {
	ID                             string
	Time                           time.Time
	Service, Name, RequestID, User string
	Attrs                          Attrs
	Cause                          string
	Depth                          int
	Seq                            int64
	Received                       time.Time
}

const eventTimeLayout = "2006-01-02T15:04:05.000000Z"

var eventNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
var attributeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
var eventIDPattern = regexp.MustCompile(`^evt_[0-9a-f]{16}$`)

func validEventName(s string) bool    { return eventNamePattern.MatchString(s) }
func validAttributeKey(s string) bool { return attributeKeyPattern.MatchString(s) }
func validEventID(s string) bool      { return eventIDPattern.MatchString(s) }
func basicAttribute(value any) (any, error) {
	if value != nil {
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
			f := v.Float()
			if !math.IsNaN(f) && !math.IsInf(f, 0) {
				return f, nil
			}
		}
	}
	return nil, errors.New("invalid attribute value")
}
func validEmitted(e Event) bool {
	if !validEventID(e.ID) || e.Time.UTC().Year() < 0 || e.Time.UTC().Year() > 9999 || e.Service == "" || !validEventName(e.Name) || e.Seq != 0 || !e.Received.IsZero() {
		return false
	}
	if (e.Cause == "" && e.Depth != 0) || (e.Cause != "" && (!validEventID(e.Cause) || e.Depth < 1)) {
		return false
	}
	for k, v := range e.Attrs {
		if !validAttributeKey(k) {
			return false
		}
		if _, err := basicAttribute(v); err != nil {
			return false
		}
	}
	return true
}
func validDelivered(e Event) bool {
	if e.Seq < 1 || e.Received.UTC().Year() < 0 || e.Received.UTC().Year() > 9999 || e.Received.UTC().Format(eventTimeLayout) == "0001-01-01T00:00:00.000000Z" {
		return false
	}
	e.Seq = 0
	e.Received = time.Time{}
	return validEmitted(e)
}

type eventEnvelope struct {
	ID        string         `json:"id"`
	Time      string         `json:"time"`
	Service   string         `json:"service"`
	Event     string         `json:"event"`
	RequestID string         `json:"request_id"`
	User      string         `json:"user"`
	Attrs     map[string]any `json:"attrs"`
	Cause     string         `json:"cause"`
	Depth     int            `json:"depth"`
}

func envelope(e Event) eventEnvelope {
	attrs := make(map[string]any, len(e.Attrs))
	for k, v := range e.Attrs {
		basic, err := basicAttribute(v)
		if err != nil {
			basic = v
		}
		attrs[k] = basic
	}
	return eventEnvelope{e.ID, e.Time.UTC().Format(eventTimeLayout), e.Service, e.Name, e.RequestID, e.User, attrs, e.Cause, e.Depth}
}

// marshalEnvelope also serializes diagnostic envelopes before validation.
func marshalEnvelope(e Event) ([]byte, error) { return json.Marshal(envelope(e)) }

// MarshalJSON returns the canonical emitted or delivered event text.
func (e Event) MarshalJSON() ([]byte, error) {
	if validEmitted(e) {
		return marshalEnvelope(e)
	}
	if validDelivered(e) {
		return json.Marshal(struct {
			eventEnvelope
			Seq      int64  `json:"seq"`
			Received string `json:"received"`
		}{envelope(e), e.Seq, e.Received.UTC().Format(eventTimeLayout)})
	}
	return nil, errors.New("invalid event envelope")
}
func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	result := make(map[string]json.RawMessage)
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid object key")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate member %q", key)
		}
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return nil, err
		}
		result[key] = raw
	}
	if _, err = d.Token(); err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return result, nil
}
func decodeString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", errors.New("expected string")
	}
	var s string
	err := json.Unmarshal(raw, &s)
	return s, err
}
func decodeTime(raw json.RawMessage) (time.Time, error) {
	s, err := decodeString(raw)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(eventTimeLayout, s)
	if err != nil || t.Format(eventTimeLayout) != s {
		return time.Time{}, errors.New("invalid event time")
	}
	return t.UTC(), nil
}
func decodeAttribute(raw json.RawMessage) (any, error) {
	if len(raw) > 0 && raw[0] == '"' {
		return decodeString(raw)
	}
	s := string(raw)
	if s == "true" {
		return true, nil
	}
	if s == "false" {
		return false, nil
	}
	if s == "-0" {
		return math.Copysign(0, -1), nil
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, nil
	}
	if v, err := strconv.ParseUint(s, 10, 64); err == nil {
		return v, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// UnmarshalJSON accepts exactly one valid event text and preserves e on failure.
func (e *Event) UnmarshalJSON(data []byte) error {
	fields, err := decodeObject(data)
	if err != nil {
		return err
	}
	if len(fields) != 9 && len(fields) != 11 {
		return errors.New("invalid event members")
	}
	var next Event
	strings := []struct {
		key string
		dst *string
	}{{"id", &next.ID}, {"service", &next.Service}, {"event", &next.Name}, {"request_id", &next.RequestID}, {"user", &next.User}, {"cause", &next.Cause}}
	for _, f := range strings {
		*f.dst, err = decodeString(fields[f.key])
		if err != nil {
			return err
		}
	}
	next.Time, err = decodeTime(fields["time"])
	if err != nil {
		return err
	}
	next.Depth, err = strconv.Atoi(string(fields["depth"]))
	if err != nil {
		return err
	}
	attrs, err := decodeObject(fields["attrs"])
	if err != nil {
		return err
	}
	next.Attrs = make(Attrs, len(attrs))
	for k, raw := range attrs {
		next.Attrs[k], err = decodeAttribute(raw)
		if err != nil {
			return err
		}
	}
	if len(fields) == 11 {
		next.Seq, err = strconv.ParseInt(string(fields["seq"]), 10, 64)
		if err != nil {
			return err
		}
		next.Received, err = decodeTime(fields["received"])
		if err != nil {
			return err
		}
		if !validDelivered(next) {
			return errors.New("invalid delivered event")
		}
	} else if !validEmitted(next) {
		return errors.New("invalid emitted event")
	}
	*e = next
	return nil
}
