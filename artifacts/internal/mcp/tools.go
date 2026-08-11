// Package mcp exposes the artifact lifecycle through the chassis MCP server.
package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"appkit"
	appkitmcp "appkit/mcp"
	"artifacts/internal/artifacts"
	"artifacts/internal/db"
	"registry"
)

const Instructions = "Manage files with upload links and public/private download links. Call guide for examples and the complete artifacts vocabulary."

var Guide = `Artifacts stores files without putting their bytes in MCP calls.

Fields: id, filename, description, visibility, size, content_hash, download_count, url, content_url, created_at, updated_at, owner_id, owner_email.

Upload a local file: call upload with filename and public or private visibility, then run the returned command, for example:
  curl -T ./report.pdf 'https://files.example/srv/artifacts/u/TOKEN'
Then call list or get to obtain its download URL and immutable content reference.

Import from another suite service: call import with source_url such as ` + registry.BaseURL("artifacts") + `/content?id=FILE_ID, filename, and visibility. The service fetches and stores the bytes; MCP never carries them.

Use update to set or clear a description, set_visibility to move a file between public and private download tiers, and delete to remove both its record and stored content.`

type toolset struct {
	service *artifacts.Service
}

// Tools returns the complete artifacts domain tool table.
func Tools(service *artifacts.Service) []appkitmcp.Tool {
	t := &toolset{service: service}
	return []appkitmcp.Tool{
		t.structuredTool("upload", "Mint a 24-hour upload link for local file bytes; never put bytes in this call.", objectSchema(map[string]any{
			"filename": stringSchema(), "visibility": visibilitySchema(), "description": stringSchema(),
		}, "filename", "visibility"), uploadSchema(), t.upload),
		t.structuredTool("import", "Import bytes from a loopback content-plane URL; use this instead of passing bytes.", objectSchema(map[string]any{
			"source_url": stringSchema(), "filename": stringSchema(), "visibility": visibilitySchema(), "description": stringSchema(),
		}, "source_url", "filename", "visibility"), recordSchema(), t.importArtifact),
		t.structuredTool("list", "List all files in this box, including public and private download links.", objectSchema(nil), objectSchema(map[string]any{"artifacts": map[string]any{"type": "array", "items": recordSchema()}}, "artifacts"), t.list),
		t.structuredTool("get", "Get one file's metadata, download link, and content-plane reference; no bytes are returned.", idSchema(), recordSchema(), t.get),
		t.structuredTool("update", "Patch a file description: omit it to preserve the value or pass an empty string to clear it.", objectSchema(map[string]any{"id": stringSchema(), "description": stringSchema()}, "id"), recordSchema(), t.update),
		t.structuredTool("set_visibility", "Move a file between public and private download tiers.", objectSchema(map[string]any{"id": stringSchema(), "visibility": visibilitySchema()}, "id", "visibility"), recordSchema(), t.setVisibility),
		t.structuredTool("delete", "Delete a file record and its stored bytes, making its links unusable.", idSchema(), objectSchema(map[string]any{"deleted": map[string]any{"type": "boolean"}, "id": stringSchema()}, "deleted", "id"), t.delete),
		{Name: "guide", Description: "Read the field catalog and worked upload/import examples.", InputSchema: objectSchema(nil), Handler: t.guide},
	}
}

// New creates a ready-to-serve MCP handler with built-in health and reflection.
func New(service *artifacts.Service, version string) (*appkitmcp.Handler, error) {
	return appkitmcp.New(appkitmcp.Options{
		Service: "artifacts", Version: version, Instructions: Instructions, Tools: Tools(service),
		Health: func(context.Context) (map[string]any, error) { return map[string]any{"ok": true}, nil },
	})
}

func (t *toolset) structuredTool(name, description string, input, output map[string]any, handler func(context.Context, json.RawMessage, appkit.Identity) (any, map[string]any)) appkitmcp.Tool {
	return appkitmcp.Tool{Name: name, Description: description, InputSchema: input, OutputSchema: output,
		Handler: func(ctx context.Context, raw json.RawMessage, identity appkit.Identity) (map[string]any, error) {
			value, failure := handler(ctx, raw, identity)
			if failure != nil {
				return failure, nil
			}
			return appkitmcp.StructuredResult(value)
		},
	}
}

func (t *toolset) upload(ctx context.Context, raw json.RawMessage, identity appkit.Identity) (any, map[string]any) {
	var args struct {
		Filename    string `json:"filename"`
		Visibility  string `json:"visibility"`
		Description string `json:"description"`
	}
	if failure := decode(raw, &args); failure != nil {
		return nil, failure
	}
	upload, err := t.service.MintUpload(ctx, identity, args.Filename, args.Visibility, args.Description)
	if err != nil {
		return nil, resultError(err)
	}
	return map[string]any{"upload_url": upload.URL, "expires_at": upload.ExpiresAt, "curl": upload.Curl}, nil
}

func (t *toolset) importArtifact(ctx context.Context, raw json.RawMessage, identity appkit.Identity) (any, map[string]any) {
	var args struct {
		SourceURL   string `json:"source_url"`
		Filename    string `json:"filename"`
		Visibility  string `json:"visibility"`
		Description string `json:"description"`
	}
	if failure := decode(raw, &args); failure != nil {
		return nil, failure
	}
	artifact, err := t.service.Import(ctx, identity, args.SourceURL, args.Filename, args.Visibility, args.Description)
	if err != nil {
		return nil, resultError(err)
	}
	return t.record(artifact), nil
}

func (t *toolset) list(ctx context.Context, raw json.RawMessage, _ appkit.Identity) (any, map[string]any) {
	var args struct{}
	if failure := decode(raw, &args); failure != nil {
		return nil, failure
	}
	stored, err := t.service.Store.ListArtifacts(ctx)
	if err != nil {
		return nil, resultError(err)
	}
	records := make([]record, 0, len(stored))
	for _, artifact := range stored {
		records = append(records, t.record(artifact))
	}
	return map[string]any{"artifacts": records}, nil
}

func (t *toolset) get(ctx context.Context, raw json.RawMessage, _ appkit.Identity) (any, map[string]any) {
	id, failure := decodeID(raw)
	if failure != nil {
		return nil, failure
	}
	artifact, err := t.service.Store.GetArtifact(ctx, id)
	if err != nil {
		return nil, resultError(err)
	}
	return t.record(artifact), nil
}

func (t *toolset) update(ctx context.Context, raw json.RawMessage, _ appkit.Identity) (any, map[string]any) {
	var args struct {
		ID          string  `json:"id"`
		Description *string `json:"description"`
	}
	if failure := decode(raw, &args); failure != nil || args.ID == "" {
		if failure != nil {
			return nil, failure
		}
		return nil, validation("id is required")
	}
	artifact, err := t.service.Store.GetArtifact(ctx, args.ID)
	if err != nil {
		return nil, resultError(err)
	}
	description := artifact.Description
	if args.Description != nil {
		description = *args.Description
	}
	updated, changed, err := t.service.UpdateArtifact(ctx, artifact.ID, db.UpdateArtifactParams{
		Filename: artifact.Filename, Description: description, Visibility: artifact.Visibility, UpdatedAt: t.service.Clock().UTC(),
	})
	if err != nil || !changed {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, resultError(err)
	}
	return t.record(updated), nil
}

func (t *toolset) setVisibility(ctx context.Context, raw json.RawMessage, _ appkit.Identity) (any, map[string]any) {
	var args struct {
		ID         string `json:"id"`
		Visibility string `json:"visibility"`
	}
	if failure := decode(raw, &args); failure != nil {
		return nil, failure
	}
	if args.ID == "" {
		return nil, validation("id is required")
	}
	if args.Visibility != "public" && args.Visibility != "private" {
		return nil, validation("visibility must be public or private")
	}
	artifact, err := t.service.Store.GetArtifact(ctx, args.ID)
	if err != nil {
		return nil, resultError(err)
	}
	updated, changed, err := t.service.UpdateArtifact(ctx, artifact.ID, db.UpdateArtifactParams{
		Filename: artifact.Filename, Description: artifact.Description, Visibility: args.Visibility, UpdatedAt: t.service.Clock().UTC(),
	})
	if err != nil || !changed {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, resultError(err)
	}
	return t.record(updated), nil
}

func (t *toolset) delete(ctx context.Context, raw json.RawMessage, _ appkit.Identity) (any, map[string]any) {
	id, failure := decodeID(raw)
	if failure != nil {
		return nil, failure
	}
	if _, err := t.service.Store.GetArtifact(ctx, id); err != nil {
		return nil, resultError(err)
	}
	if err := t.service.Blobs.Remove(id); err != nil {
		return nil, resultError(err)
	}
	_, deleted, err := t.service.DeleteArtifact(ctx, id)
	if err != nil || !deleted {
		if err == nil {
			err = sql.ErrNoRows
		}
		return nil, resultError(err)
	}
	return map[string]any{"deleted": true, "id": id}, nil
}

func (t *toolset) guide(context.Context, json.RawMessage, appkit.Identity) (map[string]any, error) {
	return appkitmcp.TextResult(Guide), nil
}

type record struct {
	ID            string    `json:"id"`
	Filename      string    `json:"filename"`
	Description   string    `json:"description"`
	Visibility    string    `json:"visibility"`
	Size          int64     `json:"size"`
	ContentHash   string    `json:"content_hash"`
	DownloadCount int64     `json:"download_count"`
	URL           string    `json:"url"`
	ContentURL    string    `json:"content_url"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	OwnerID       string    `json:"owner_id"`
	OwnerEmail    string    `json:"owner_email"`
}

func (t *toolset) record(a db.Artifact) record {
	reference := t.service.Reference(a)
	return record{a.ID, a.Filename, a.Description, a.Visibility, a.Size, a.ContentHash, a.DownloadCount,
		t.service.DownloadURL(a.ID, a.Filename, a.Visibility), reference.ContentURL,
		a.CreatedAt, a.UpdatedAt, a.OwnerID, a.OwnerEmail}
}

func decode(raw json.RawMessage, destination any) map[string]any {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return validation("invalid arguments: " + err.Error())
	}
	return nil
}

func decodeID(raw json.RawMessage) (string, map[string]any) {
	var args struct {
		ID string `json:"id"`
	}
	if failure := decode(raw, &args); failure != nil {
		return "", failure
	}
	if args.ID == "" {
		return "", validation("id is required")
	}
	return args.ID, nil
}

func validation(message string) map[string]any {
	return appkitmcp.ErrorResult(appkitmcp.ErrValidation, message)
}

func resultError(err error) map[string]any {
	var validationErr *artifacts.ValidationError
	if errors.As(err, &validationErr) {
		return validation(validationErr.Error())
	}
	if errors.Is(err, sql.ErrNoRows) {
		return appkitmcp.ErrorResult(appkitmcp.ErrNotFound, "artifact not found")
	}
	var importErr *artifacts.ImportError
	if errors.As(err, &importErr) {
		switch importErr.Code {
		case artifacts.ImportTooLarge:
			return appkitmcp.ErrorResult(appkitmcp.ErrTooLarge, importErr.Error())
		case artifacts.ImportSourceUnavailable:
			return appkitmcp.ErrorResult(appkitmcp.ErrSourceUnavailable, importErr.Error())
		}
	}
	return appkitmcp.ErrorResult(appkitmcp.ErrInternal, fmt.Sprintf("internal error: %v", err))
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }
func visibilitySchema() map[string]any {
	return map[string]any{"type": "string", "enum": []string{"public", "private"}}
}
func objectSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
func idSchema() map[string]any { return objectSchema(map[string]any{"id": stringSchema()}, "id") }
func uploadSchema() map[string]any {
	return objectSchema(map[string]any{"upload_url": stringSchema(), "expires_at": stringSchema(), "curl": stringSchema()}, "upload_url", "expires_at", "curl")
}
func recordSchema() map[string]any {
	properties := map[string]any{}
	for _, name := range []string{"id", "filename", "description", "visibility", "content_hash", "url", "content_url", "created_at", "updated_at", "owner_id", "owner_email"} {
		properties[name] = stringSchema()
	}
	properties["size"] = map[string]any{"type": "integer"}
	properties["download_count"] = map[string]any{"type": "integer"}
	return objectSchema(properties, "id", "filename", "description", "visibility", "size", "content_hash", "download_count", "url", "content_url", "created_at", "updated_at", "owner_id", "owner_email")
}
