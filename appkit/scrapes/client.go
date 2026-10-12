// Package scrapes streams public-records runs and rows from the scrapes service.
package scrapes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// ClientConfig supplies the sibling service and event writer.
type ClientConfig struct {
	Service   services.Entry
	Telemetry *telemetry.Writer
}

// Client reads runs and streams rows, and is safe for concurrent use.
type Client struct {
	name string
	http *http.Client
}

// Run describes one scraper's run and its collection counts.
type Run struct {
	ID, Scraper, Status, Reason         string
	Started, Finished                   time.Time
	Rows, New, Changed, Unchanged, Gone int64
	User, Email, Trigger                string
}

// Row holds the common envelope and the source record's JSON bytes.
type Row struct {
	Scraper, Run, SourceID string
	Collected              time.Time
	Digest, Name           string
	Names                  []string
	Address                Address
	Lat, Lon               *float64
	People                 []Person
	Facts                  map[string]json.RawMessage
	Raw                    json.RawMessage
}

// Address is a row's postal address.
type Address struct{ Street, City, State, Zip, County string }

// Person is a person named in a source record.
type Person struct{ Name, Role string }

// The service's refusals have distinct identities for errors.Is.
var (
	ErrNotFound    = errors.New("scrapes: not found")
	ErrUnavailable = errors.New("scrapes: unavailable")
)

// HTTPError retains an unexpected response's status and bounded body prefix.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("scrapes: unexpected HTTP response (status %d)", e.StatusCode)
}

// NewClient constructs a client without contacting the service.
func NewClient(cfg ClientConfig) *Client {
	client := telemetry.SiblingClient(cfg.Telemetry, cfg.Service.Name, telemetry.SocketTransport(cfg.Service.Socket))
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{name: cfg.Service.Name, http: client}
}

func (c *Client) request(ctx context.Context, path string) (*http.Response, error) {
	u := &url.URL{Scheme: "http", Host: c.name, Path: path}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("scrapes: request: %w", err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("scrapes: request: %w", err)
	}
	return response, nil
}

func responseError(response *http.Response) error {
	switch response.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusServiceUnavailable:
		return ErrUnavailable
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return &HTTPError{StatusCode: response.StatusCode, Body: string(body)}
}

// Current reads the scraper's current run.
func (c *Client) Current(ctx context.Context, scraper string) (Run, error) {
	return c.run(ctx, "/scrapers/"+scraper+"/current")
}

// Record reads one run's record.
func (c *Client) Record(ctx context.Context, run string) (Run, error) {
	return c.run(ctx, "/runs/"+run+"/record")
}

func (c *Client) run(ctx context.Context, path string) (Run, error) {
	response, err := c.request(ctx, path)
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Run{}, responseError(response)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return Run{}, fmt.Errorf("scrapes: read run: %w", err)
	}
	run, err := decodeRun(data)
	if err != nil {
		return Run{}, fmt.Errorf("scrapes: invalid run response: %w", err)
	}
	return run, nil
}

// Rows hands each complete row to fn and returns fn's own error unchanged.
func (c *Client) Rows(ctx context.Context, run string, fn func(Row) error) error {
	response, err := c.request(ctx, "/runs/"+run+"/rows")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return fmt.Errorf("scrapes: read rows: %w", err)
			}
			if len(line) != 0 {
				return errors.New("scrapes: incomplete row line")
			}
			return nil
		}
		row, err := decodeRow(line[:len(line)-1])
		if err != nil {
			return fmt.Errorf("scrapes: invalid row: %w", err)
		}
		if err := fn(row); err != nil {
			return err
		}
	}
}

func object(data []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid JSON encoding")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("expected object")
	}
	return fields, nil
}

func text(fields map[string]json.RawMessage, key string, optional bool) (string, error) {
	raw, ok := fields[key]
	if !ok && optional {
		return "", nil
	}
	var value any
	if json.Unmarshal(raw, &value) == nil {
		if result, ok := value.(string); ok {
			return result, nil
		}
	}
	return "", fmt.Errorf("invalid %s", key)
}

func instant(fields map[string]json.RawMessage, key string, optional bool) (time.Time, error) {
	if _, ok := fields[key]; !ok && optional {
		return time.Time{}, nil
	}
	value, err := text(fields, key, false)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func decodeRun(data []byte) (Run, error) {
	fields, err := object(data)
	if err != nil {
		return Run{}, err
	}
	var run Run
	for _, member := range []struct {
		name     string
		dest     *string
		optional bool
	}{
		{"id", &run.ID, false}, {"scraper", &run.Scraper, false}, {"status", &run.Status, false},
		{"reason", &run.Reason, true}, {"user", &run.User, true}, {"email", &run.Email, true}, {"trigger", &run.Trigger, true},
	} {
		*member.dest, err = text(fields, member.name, member.optional)
		if err != nil {
			return Run{}, err
		}
	}
	run.Started, err = instant(fields, "started", false)
	if err != nil {
		return Run{}, err
	}
	run.Finished, err = instant(fields, "finished", true)
	if err != nil {
		return Run{}, err
	}
	for _, member := range []struct {
		name string
		dest *int64
	}{
		{"rows", &run.Rows}, {"new", &run.New}, {"changed", &run.Changed}, {"unchanged", &run.Unchanged}, {"gone", &run.Gone},
	} {
		if raw := fields[member.name]; len(raw) == 0 || string(raw) == "null" {
			return Run{}, fmt.Errorf("invalid %s", member.name)
		}
		if err := json.Unmarshal(fields[member.name], member.dest); err != nil {
			return Run{}, err
		}
	}
	return run, nil
}

func array(fields map[string]json.RawMessage, key string) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(fields[key], &items); err != nil {
		return nil, err
	}
	if items == nil {
		return nil, fmt.Errorf("invalid %s", key)
	}
	return items, nil
}

func decodeRow(data []byte) (Row, error) {
	fields, err := object(data)
	if err != nil {
		return Row{}, err
	}
	var row Row
	for _, member := range []struct {
		name string
		dest *string
	}{
		{"scraper", &row.Scraper}, {"run", &row.Run}, {"source_id", &row.SourceID}, {"digest", &row.Digest}, {"name", &row.Name},
	} {
		*member.dest, err = text(fields, member.name, false)
		if err != nil {
			return Row{}, err
		}
	}
	row.Collected, err = instant(fields, "collected", false)
	if err != nil {
		return Row{}, err
	}
	names, err := array(fields, "names")
	if err != nil {
		return Row{}, err
	}
	row.Names = make([]string, len(names))
	for i, item := range names {
		row.Names[i], err = text(map[string]json.RawMessage{"name": item}, "name", false)
		if err != nil {
			return Row{}, err
		}
	}
	if raw, ok := fields["address"]; ok {
		address, err := object(raw)
		if err != nil {
			return Row{}, err
		}
		for _, member := range []struct {
			name string
			dest *string
		}{
			{"street", &row.Address.Street}, {"city", &row.Address.City}, {"state", &row.Address.State}, {"zip", &row.Address.Zip}, {"county", &row.Address.County},
		} {
			*member.dest, err = text(address, member.name, true)
			if err != nil {
				return Row{}, err
			}
		}
	}
	for _, member := range []struct {
		name string
		dest **float64
	}{{"lat", &row.Lat}, {"lon", &row.Lon}} {
		if raw, ok := fields[member.name]; ok {
			if err := json.Unmarshal(raw, member.dest); err != nil {
				return Row{}, err
			}
		}
	}
	people, err := array(fields, "people")
	if err != nil {
		return Row{}, err
	}
	row.People = make([]Person, len(people))
	for i, item := range people {
		person, err := object(item)
		if err != nil {
			return Row{}, err
		}
		row.People[i].Name, err = text(person, "name", true)
		if err != nil {
			return Row{}, err
		}
		row.People[i].Role, err = text(person, "role", true)
		if err != nil {
			return Row{}, err
		}
	}
	row.Facts, err = object(fields["facts"])
	if err != nil {
		return Row{}, err
	}
	row.Raw = fields["raw"]
	return row, nil
}
