// Package tools exposes artifact metadata and upload plans over MCP.
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
	Now       func() time.Time
}

// Entry describes the artifact service contract.
type Entry struct{ Name, Description string }

// BytesGuide explains how callers put and fetch artifact bytes over HTTP.
const BytesGuide string = "No tool carries bytes: import answers the URL and instructions to put a file, and show and list answer the url to fetch one."

// BearerPlaceholder stands for the caller's own bearer token.
const BearerPlaceholder string = "Bearer <your token>"

// ImportInstructions explains how to follow an upload plan.
const ImportInstructions string = "Send the file as the request body. 201 means created, 200 means replaced. GET the same URL to read it back."

// Plan describes the HTTP upload a caller performs.
type Plan struct {
	URL          string      `json:"url"`
	Method       string      `json:"method"`
	Headers      PlanHeaders `json:"headers"`
	Instructions string      `json:"instructions"`
}

// PlanHeaders names the HTTP headers to send with the file.
type PlanHeaders struct {
	Authorization string  `json:"Authorization"`
	ContentType   string  `json:"Content-Type"`
	Expires       *string `json:"X-Artifact-Expires,omitempty"`
}

// ListArgs describes the artifact service contract.
type ListArgs struct{}

// PathArgs describes the artifact service contract.
type PathArgs struct {
	Path string `json:"path" mcp:"required"`
}

// ImportArgs describes the artifact service contract.
type ImportArgs struct {
	Path        string  `json:"path" mcp:"required"`
	ContentType *string `json:"content_type"`
	Expires     *string `json:"expires"`
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

// Catalog implements the artifact service contract.
func Catalog() []Entry {
	return []Entry{
		{"list", "List every artifact's metadata." + "\n\n" + BytesGuide},
		{"show", "Show an artifact's metadata." + "\n\n" + BytesGuide},
		{"delete", "Delete an artifact you own." + "\n\n" + BytesGuide},
		{"import", "Plan an artifact upload." + "\n\n" + BytesGuide},
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
	c := Catalog()
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
	addTool(srv, mcp.Tool[ImportArgs, Plan]{Name: c[3].Name, Description: c[3].Description, Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, in ImportArgs) (Plan, error) {
		return importArtifact(ctx, cfg, c, in)
	}})
}
func importArtifact(ctx context.Context, cfg Config, c identity.Caller, in ImportArgs) (Plan, error) {
	fail := func(s string) (Plan, error) { return Plan{}, errors.New(s) }
	if !store.ValidPath(in.Path) {
		return fail(InvalidPath(in.Path))
	}
	if in.Expires != nil {
		exp, err := time.Parse(time.RFC3339, *in.Expires)
		if err != nil {
			return fail(InvalidExpiry(*in.Expires))
		}
		if !exp.Truncate(time.Second).After(cfg.Now().Truncate(time.Second)) {
			return fail(PastExpiry(*in.Expires))
		}
	}
	a, err := cfg.Store.Get(ctx, in.Path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fail(store.Unreachable)
	}
	if err == nil && a.OwnerID != c.UserID {
		return fail(NotOwner(in.Path))
	}
	ct := store.DefaultContentType
	if in.ContentType != nil && *in.ContentType != "" {
		ct = *in.ContentType
	}
	return Plan{URL: urls.File(cfg.Base, in.Path), Method: "PUT", Headers: PlanHeaders{Authorization: BearerPlaceholder, ContentType: ct, Expires: in.Expires}, Instructions: ImportInstructions}, nil
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
