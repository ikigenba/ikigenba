// Package tools exposes artifact metadata and sibling imports over MCP.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/fetch"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

// Config describes the artifact service contract.
type Config struct {
	Base      string
	Store     *store.Store
	Telemetry *telemetry.Writer
	Events    *events.Emitter
	Max       int64
	Now       func() time.Time
	Fetch     fetch.Config
}

// Entry describes the artifact service contract.
type Entry struct{ Name, Description string }

// BytesGuide explains how callers put and fetch artifact bytes over HTTP.
func BytesGuide(base string) string {
	return "No tool carries bytes. To put a file, send an HTTP PUT with the file as the body to " + base + "/files/<path>, with your bearer token in the Authorization header: 201 means created, 200 means replaced. The Content-Type header is stored and served back, and an optional X-Artifact-Expires header holding an RFC 3339 instant sets when the artifact expires. To fetch the bytes, send an authenticated GET to the same URL, which is the url field that show and list answer. Use import only for a file another service on this space already serves at a URL."
}

// ListArgs describes the artifact service contract.
type ListArgs struct{}

// PathArgs describes the artifact service contract.
type PathArgs struct {
	Path string `json:"path" mcp:"required"`
}

// ImportArgs describes the artifact service contract.
type ImportArgs struct {
	URL     string  `json:"url" mcp:"required"`
	Path    string  `json:"path" mcp:"required"`
	Expires *string `json:"expires"`
}

// Listed describes the artifact service contract.
type Listed struct {
	ID          string  `json:"id"`
	Path        string  `json:"path" mcp:"required"`
	Bytes       int64   `json:"bytes"`
	ContentType string  `json:"content_type"`
	SHA256      string  `json:"sha256"`
	Owner       string  `json:"owner"`
	Modified    string  `json:"modified"`
	Expires     *string `json:"expires,omitempty"`
	URL         string  `json:"url"`
}

// ArtifactList describes the artifact service contract.
type ArtifactList struct {
	Artifacts []Listed `json:"artifacts"`
}

// Deleted describes the artifact service contract.
type Deleted struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

// NoArtifact implements the artifact service contract.
func NoArtifact(p string) string { return fmt.Sprintf("No artifact at %s", p) }

// InvalidPath implements the artifact service contract.
func InvalidPath(p string) string { return fmt.Sprintf("Invalid artifact path: %s", p) }

// NotOwner implements the artifact service contract.
func NotOwner(p string) string { return fmt.Sprintf("You do not own %s", p) }

// InvalidExpiry implements the artifact service contract.
func InvalidExpiry(s string) string { return fmt.Sprintf("Invalid expiry: %s", s) }

// PastExpiry implements the artifact service contract.
func PastExpiry(s string) string { return fmt.Sprintf("Expiry is not in the future: %s", s) }

// NotOnSpace implements the artifact service contract.
func NotOnSpace(s string) string { return fmt.Sprintf("URL is not on this space: %s", s) }

// FetchUnanswered implements the artifact service contract.
func FetchUnanswered(s string) string { return fmt.Sprintf("Sibling did not answer: %s", s) }

// FetchTimedOut implements the artifact service contract.
func FetchTimedOut(s string) string { return fmt.Sprintf("Fetch timed out: %s", s) }

// FetchCutOff implements the artifact service contract.
func FetchCutOff(s string) string { return fmt.Sprintf("Sibling body was cut off: %s", s) }

// FetchFailed implements the artifact service contract.
func FetchFailed(n int) string { return fmt.Sprintf("Sibling answered status %d", n) }

// TooLarge implements the artifact service contract.
func TooLarge(n int64) string { return fmt.Sprintf("Artifact exceeds %d bytes", n) }

// Catalog implements the artifact service contract.
func Catalog(base string) []Entry {
	return []Entry{
		{"list", "List every artifact's metadata." + "\n\n" + BytesGuide(base)},
		{"show", "Show an artifact's metadata." + "\n\n" + BytesGuide(base)},
		{"delete", "Delete an artifact you own." + "\n\n" + BytesGuide(base)},
		{"import", "Import a sibling service's file." + "\n\n" + BytesGuide(base)},
	}
}

// FirstLine implements the artifact service contract.
func FirstLine(s string) string { line, _, _ := strings.Cut(s, "\n"); return line }
func refusal(err error, p string) error {
	switch {
	case errors.Is(err, store.ErrInvalid):
		return errors.New(InvalidPath(p))
	case errors.Is(err, store.ErrNotFound):
		return errors.New(NoArtifact(p))
	case errors.Is(err, store.ErrForbidden):
		return errors.New(NotOwner(p))
	default:
		return errors.New(store.Unreachable)
	}
}

// Register implements the artifact service contract.
func Register(srv *mcp.Server, cfg Config) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	c := Catalog(cfg.Base)
	addTool(srv, mcp.Tool[ListArgs, ArtifactList]{Name: c[0].Name, Description: c[0].Description, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, _ ListArgs) (ArtifactList, error) {
		out := ArtifactList{Artifacts: make([]Listed, 0)}
		all, err := cfg.Store.List(ctx)
		if err != nil {
			return out, refusal(err, "")
		}
		base, _ := urls.FromContext(ctx)
		for _, a := range all {
			m := files.Describe(a, base)
			out.Artifacts = append(out.Artifacts, Listed{ID: m.ID, Path: m.Path, Bytes: m.Bytes, ContentType: m.ContentType, SHA256: m.SHA256, Owner: m.Owner, Modified: m.Modified, Expires: m.Expires, URL: m.URL})
		}
		return out, nil
	}})
	addTool(srv, mcp.Tool[PathArgs, files.Metadata]{Name: c[1].Name, Description: c[1].Description, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, in PathArgs) (files.Metadata, error) {
		if !store.ValidPath(in.Path) {
			return files.Metadata{}, errors.New(InvalidPath(in.Path))
		}
		a, err := cfg.Store.Get(ctx, in.Path)
		if err != nil {
			return files.Metadata{}, refusal(err, in.Path)
		}
		base, _ := urls.FromContext(ctx)
		return files.Describe(a, base), nil
	}})
	mcp.AddTool(srv, mcp.Tool[PathArgs, Deleted]{Name: c[2].Name, Description: c[2].Description, Effect: mcp.Destructive, Handler: func(ctx context.Context, c identity.Caller, in PathArgs) (Deleted, error) {
		if !store.ValidPath(in.Path) {
			return Deleted{}, errors.New(InvalidPath(in.Path))
		}
		a, err := cfg.Store.Delete(ctx, in.Path, c.UserID)
		if err != nil {
			return Deleted{}, refusal(err, in.Path)
		}
		trail.Record(ctx, cfg.Telemetry, cfg.Events, trail.Deleted, a)
		return Deleted{Deleted: true, ID: a.ID}, nil
	}})
	addTool(srv, mcp.Tool[ImportArgs, files.Metadata]{Name: c[3].Name, Description: c[3].Description, Effect: mcp.Destructive, Handler: func(ctx context.Context, c identity.Caller, in ImportArgs) (files.Metadata, error) {
		return importArtifact(ctx, cfg, c, in)
	}})
}
func importArtifact(ctx context.Context, cfg Config, c identity.Caller, in ImportArgs) (files.Metadata, error) {
	fail := func(s string) (files.Metadata, error) { return files.Metadata{}, errors.New(s) }
	if !store.ValidPath(in.Path) {
		return fail(InvalidPath(in.Path))
	}
	var exp time.Time
	if in.Expires != nil {
		var err error
		exp, err = time.Parse(time.RFC3339, *in.Expires)
		if err != nil {
			return fail(InvalidExpiry(*in.Expires))
		}
		if !exp.Truncate(time.Second).After(cfg.Now().Truncate(time.Second)) {
			return fail(PastExpiry(*in.Expires))
		}
	}
	if _, err := fetch.Target(cfg.Fetch.ServicesPath, in.URL); err != nil {
		return fail(NotOnSpace(in.URL))
	}
	a, err := cfg.Store.Get(ctx, in.Path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fail(store.Unreachable)
	}
	if err == nil && a.OwnerID != c.UserID {
		return fail(NotOwner(in.Path))
	}
	res, err := fetch.Get(ctx, cfg.Fetch, in.URL)
	if err != nil {
		var status *fetch.StatusError
		switch {
		case errors.Is(err, fetch.ErrNotOnSpace):
			return fail(NotOnSpace(in.URL))
		case errors.Is(err, fetch.ErrTimedOut):
			return fail(FetchTimedOut(in.URL))
		case errors.As(err, &status):
			return fail(FetchFailed(status.Code))
		default:
			return fail(FetchUnanswered(in.URL))
		}
	}
	defer func() { _ = res.Body.Close() }()
	a, created, err := cfg.Store.Put(ctx, store.Draft{Path: in.Path, ContentType: res.ContentType, Expires: exp, OwnerID: c.UserID, OwnerEmail: c.Email, Max: cfg.Max}, res.Body)
	if err != nil {
		switch {
		case errors.Is(err, fetch.ErrTimedOut):
			return fail(FetchTimedOut(in.URL))
		case errors.Is(err, store.ErrTooLarge):
			return fail(TooLarge(cfg.Max))
		case errors.Is(err, store.ErrForbidden):
			return fail(NotOwner(in.Path))
		case errors.Is(err, store.ErrBody):
			return fail(FetchCutOff(in.URL))
		default:
			return fail(store.Unreachable)
		}
	}
	name := trail.Replaced
	if created {
		name = trail.Created
	}
	trail.Record(ctx, cfg.Telemetry, cfg.Events, name, a)
	base, _ := urls.FromContext(ctx)
	return files.Describe(a, base), nil
}

func addTool[In, Out any](srv *mcp.Server, tool mcp.Tool[In, Out]) {
	mcp.AddRawTool(srv, mcp.RawTool[In]{Name: tool.Name, Description: tool.Description, Effect: tool.Effect, Handler: func(ctx context.Context, c identity.Caller, in In) (mcp.Result, error) {
		out, err := tool.Handler(ctx, c, in)
		if err != nil {
			return mcp.Result{}, err
		}
		b, err := json.Marshal(out)
		if err != nil {
			return mcp.Result{}, err
		}
		wire, err := json.Marshal(struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Structured json.RawMessage `json:"structuredContent"`
		}{Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{{Type: "text", Text: string(b)}}, Structured: b})
		if err != nil {
			return mcp.Result{}, err
		}
		var result mcp.Result
		err = json.Unmarshal(wire, &result)
		return result, err
	}})
}
