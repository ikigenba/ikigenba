// Package candidates reads businesses through the candidates service's MCP tools.
package candidates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// ClientConfig supplies the sibling service and event writer.
type ClientConfig struct {
	Service   services.Entry
	Telemetry *telemetry.Writer
}

// Client makes independent calls and is safe for concurrent use.
type Client struct{ mcp *mcp.Client }

// Business is the resolved business exactly as the service answered it.
type Business struct {
	ID, Status, MergedInto, Name                   string
	Names                                          []string
	Street, City, State, Zip, County, Phone, Email string
	People                                         []Person
}

// Person is a business contact.
type Person struct{ Name, Role string }

// Member holds a prospect list member, including its exclusion state.
type Member struct {
	Business Business
	Excluded bool
}

// Sentinel errors identify the refusals a reader can act on.
var (
	// ErrNotFound means the prospect list, cohort or candidate cannot be found.
	ErrNotFound = errors.New("candidates: not found")
	// ErrUnavailable means the store cannot be read now.
	ErrUnavailable = errors.New("candidates: unavailable")
	// ErrStopping means the service is shutting down.
	ErrStopping = errors.New("candidates: stopping")
)

// Refusal carries the tool's refusal text without interpreting it.
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return "candidates: " + r.Text }

// HTTPError retains an unexpected response's status and bounded body prefix.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("candidates: unexpected HTTP response (status %d)", e.StatusCode)
}

// NewClient constructs a client without contacting the service.
func NewClient(cfg ClientConfig) *Client {
	client := telemetry.SiblingClient(cfg.Telemetry, cfg.Service.Name, telemetry.SocketTransport(cfg.Service.Socket))
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	endpoint := (&url.URL{Scheme: "http", Host: cfg.Service.Name, Path: "/mcp"}).String()
	return &Client{mcp: mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint, HTTPClient: client})}
}

// ProspectList reads every member in the prospect list's order, including excluded members.
func (c *Client) ProspectList(ctx context.Context, id string) ([]Member, error) {
	fields, err := c.call(ctx, "prospect_list", map[string]any{"prospect_list": id})
	var rpc *mcp.RPCError
	if errors.As(err, &rpc) && rpc.Code == mcp.CodeInvalidParams && rpc.Message == "Unknown tool: prospect_list" {
		cohort := id
		if strings.HasPrefix(id, "pl_") {
			cohort = "coh_" + id[3:]
		}
		return c.Members(ctx, cohort)
	}
	if err != nil {
		return nil, err
	}
	return decodeMembers(ctx, fields)
}

// Members reads every member in the cohort's order, including excluded members.
func (c *Client) Members(ctx context.Context, cohort string) ([]Member, error) {
	fields, err := c.call(ctx, "cohort", map[string]any{"cohort": cohort})
	if err != nil {
		return nil, err
	}
	return decodeMembers(ctx, fields)
}

func decodeMembers(ctx context.Context, fields map[string]json.RawMessage) ([]Member, error) {
	entries, err := array(fields["members"])
	if err != nil {
		return nil, outputError(ctx, err)
	}
	members := make([]Member, len(entries))
	for i, raw := range entries {
		entry, err := object(raw)
		if err != nil {
			return nil, outputError(ctx, err)
		}
		members[i].Business, err = business(entry["candidate"])
		if err != nil {
			return nil, outputError(ctx, err)
		}
		if raw, ok := entry["excluded"]; ok {
			raw = bytes.TrimSpace(raw)
			if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
				return nil, outputError(ctx, errors.New("invalid excluded"))
			}
			members[i].Excluded = bytes.Equal(raw, []byte("true"))
		}
	}
	return members, nil
}

// Candidate reads one business without following its merge pointer.
func (c *Client) Candidate(ctx context.Context, candidate string) (Business, error) {
	fields, err := c.call(ctx, "candidate", map[string]any{"candidate": candidate})
	if err != nil {
		return Business{}, err
	}
	value, err := business(fields["candidate"])
	if err != nil {
		return Business{}, outputError(ctx, err)
	}
	return value, nil
}

func (c *Client) call(ctx context.Context, tool string, args map[string]any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("candidates: encode arguments: %w", err)
	}
	caller, _ := identity.FromContext(ctx)
	result, err := c.mcp.CallTool(ctx, caller, tool, encoded)
	if err != nil {
		var rpc *mcp.RPCError
		if errors.As(err, &rpc) {
			return nil, fmt.Errorf("candidates: call %s: %w", tool, err)
		}
		var he *mcp.HTTPError
		if errors.As(err, &he) {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("candidates: call %s: %w", tool, ctx.Err())
			}
			return nil, &HTTPError{StatusCode: he.StatusCode, Body: he.Body}
		}
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, fmt.Errorf("candidates: call %s: %w", tool, err)
	}
	raw, err := result.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("candidates: decode result: %w", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("candidates: decode result: %w", err)
	}
	if result.IsError() {
		text := refusalText(members["content"])
		for _, refusal := range []struct {
			prefix string
			err    error
		}{
			{"not_found: ", ErrNotFound}, {"unavailable: ", ErrUnavailable}, {"stopping: ", ErrStopping},
		} {
			if strings.HasPrefix(text, refusal.prefix) {
				return nil, refusal.err
			}
		}
		return nil, &Refusal{Text: text}
	}
	fields, err := object(members["structuredContent"])
	if err != nil {
		return nil, outputError(ctx, err)
	}
	return fields, nil
}

func outputError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return fmt.Errorf("candidates: invalid output: %w", err)
}

func refusalText(raw json.RawMessage) string {
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, block := range blocks {
		var fields map[string]json.RawMessage
		if json.Unmarshal(block, &fields) != nil {
			continue
		}
		var kind, text string
		if stringInto(fields["type"], &kind) && kind == "text" && stringInto(fields["text"], &text) {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("invalid JSON encoding")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("expected object")
	}
	return fields, nil
}

type stringField struct {
	name     string
	target   *string
	optional bool
}

func stringsInto(fields map[string]json.RawMessage, targets []stringField) bool {
	for _, target := range targets {
		raw, ok := fields[target.name]
		if !ok && target.optional {
			continue
		}
		if !stringInto(raw, target.target) {
			return false
		}
	}
	return true
}

func stringInto(raw []byte, target *string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] == '"' && json.Unmarshal(raw, target) == nil
}

func array(raw []byte) ([]json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	var elements []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &elements) != nil {
		return nil, errors.New("expected array")
	}
	return elements, nil
}

func business(raw []byte) (Business, error) {
	fields, err := object(raw)
	if err != nil {
		return Business{}, err
	}
	var b Business
	if !stringsInto(fields, []stringField{
		{"id", &b.ID, false}, {"status", &b.Status, false}, {"name", &b.Name, false},
		{"merged_into", &b.MergedInto, true}, {"street", &b.Street, true}, {"city", &b.City, true},
		{"state", &b.State, true}, {"zip", &b.Zip, true}, {"county", &b.County, true},
		{"phone", &b.Phone, true}, {"email", &b.Email, true},
	}) {
		return Business{}, errors.New("invalid business string")
	}
	b.Names = make([]string, 0)
	b.People = make([]Person, 0)
	if raw, ok := fields["names"]; ok {
		names, err := array(raw)
		if err != nil {
			return Business{}, err
		}
		for _, raw := range names {
			var name string
			if !stringInto(raw, &name) {
				return Business{}, errors.New("invalid name")
			}
			b.Names = append(b.Names, name)
		}
	}
	if raw, ok := fields["people"]; ok {
		people, err := array(raw)
		if err != nil {
			return Business{}, err
		}
		for _, raw := range people {
			fields, err := object(raw)
			if err != nil {
				return Business{}, err
			}
			var p Person
			if !stringsInto(fields, []stringField{{"name", &p.Name, true}, {"role", &p.Role, true}}) {
				return Business{}, errors.New("invalid person")
			}
			b.People = append(b.People, p)
		}
	}
	return b, nil
}
