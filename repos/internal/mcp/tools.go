package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appkitmcp "appkit/mcp"
	"appkit/server"
	"repos/internal/repos"
)

func object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func kindNameProperties() map[string]any {
	return map[string]any{
		"kind": stringProperty("repository kind: sites, scripts, prompts, or code; defaults to code"),
		"name": stringProperty("repository key"),
	}
}

var repositorySchema = object(map[string]any{
	"kind":           map[string]any{"type": "string"},
	"name":           map[string]any{"type": "string"},
	"owner_id":       map[string]any{"type": "string"},
	"owner_email":    map[string]any{"type": "string"},
	"default_branch": map[string]any{"type": "string"},
	"archived":       map[string]any{"type": "boolean"},
}, "kind", "name", "owner_id", "owner_email", "default_branch", "archived")

var statusSchema = object(map[string]any{
	"kind":       map[string]any{"type": "string"},
	"name":       map[string]any{"type": "string"},
	"sha":        map[string]any{"type": "string"},
	"check":      map[string]any{"type": "string"},
	"state":      map[string]any{"type": "string"},
	"detail":     map[string]any{"type": "string"},
	"actor":      map[string]any{"type": "string"},
	"updated_at": map[string]any{"type": "string"},
}, "kind", "name", "sha", "check", "state", "detail", "actor", "updated_at")

// Tools returns the complete owner-scoped repository verb table.
func Tools(svc Service) []appkitmcp.Tool {
	return []appkitmcp.Tool{
		tool("create", "Create a clonable git repository; an existing key returns conflict.",
			object(kindNameProperties(), "name"), repositorySchema, createHandler(svc)),
		tool("list", "List this owner's live repositories, optionally filtered by kind.",
			object(map[string]any{"kind": stringProperty("optional repository kind")}),
			object(map[string]any{"repositories": map[string]any{"type": "array", "items": repositorySchema}}, "repositories"), listHandler(svc)),
		tool("get", "Get one repository's live git head, branches, and clone door URL.",
			object(kindNameProperties(), "name"),
			object(map[string]any{
				"repository": repositorySchema,
				"head":       map[string]any{"type": "string"},
				"branches":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"clone_url":  map[string]any{"type": "string"},
			}, "repository", "head", "branches", "clone_url"), getHandler(svc)),
		tool("rename", "Rename a repository without changing its git history.",
			object(mergeProperties(kindNameProperties(), map[string]any{"to": stringProperty("new repository key")}), "name", "to"), repositorySchema, renameHandler(svc)),
		tool("delete", "Archive a repository without destroying history; retrying an archived key succeeds.",
			object(kindNameProperties(), "name"), repositorySchema, deleteHandler(svc)),
		tool("merge", "Merge a branch into main; red or pending statuses refuse the merge.",
			object(mergeProperties(kindNameProperties(), map[string]any{"branch": stringProperty("branch to merge")}), "name", "branch"),
			object(map[string]any{
				"merged": map[string]any{"type": "boolean"}, "rev": map[string]any{"type": "string"}, "strategy": map[string]any{"type": "string"},
			}, "merged", "rev", "strategy"), mergeHandler(svc)),
		tool("status_set", "Store one check state for a repository commit.",
			object(mergeProperties(kindNameProperties(), map[string]any{
				"sha": stringProperty("commit sha"), "check": stringProperty("check name"), "state": stringProperty("pending, success, or failure"), "detail": stringProperty("optional detail"),
			}), "name", "sha", "check", "state"), statusSchema, statusSetHandler(svc)),
		tool("status_list", "List all stored check states for one repository commit.",
			object(mergeProperties(kindNameProperties(), map[string]any{"sha": stringProperty("commit sha")}), "name", "sha"),
			object(map[string]any{"statuses": map[string]any{"type": "array", "items": statusSchema}}, "statuses"), statusListHandler(svc)),
		{
			Name:        "guide",
			Description: "Read repository kinds, keys, branch and status conventions, and worked examples.",
			InputSchema: object(map[string]any{}),
			Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
				return appkitmcp.TextResult(Guide), nil
			},
		},
	}
}

func tool(name, description string, input, output map[string]any, handler func(context.Context, json.RawMessage, server.Identity) (map[string]any, error)) appkitmcp.Tool {
	return appkitmcp.Tool{Name: name, Description: description, InputSchema: input, OutputSchema: output, Handler: handler}
}

func mergeProperties(groups ...map[string]any) map[string]any {
	result := map[string]any{}
	for _, group := range groups {
		for key, value := range group {
			result[key] = value
		}
	}
	return result
}

type commonArgs struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	To     string `json:"to"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	Check  string `json:"check"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

func decode(args json.RawMessage) (commonArgs, error) {
	var value commonArgs
	if len(args) != 0 {
		if err := json.Unmarshal(args, &value); err != nil {
			return value, fmt.Errorf("%w: invalid arguments", repos.ErrValidation)
		}
	}
	if value.Kind == "" {
		value.Kind = "code"
	}
	return value, nil
}

func createHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		repository, err := svc.CreateRepository(ctx, repos.Repository{Kind: args.Kind, Name: args.Name, OwnerID: id.OwnerID, OwnerEmail: id.OwnerEmail})
		return structured(repositoryMap(repository), err)
	}
}

func listHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		var kind *string
		if len(raw) > 0 && strings.Contains(string(raw), `"kind"`) {
			kind = &args.Kind
		}
		values, err := svc.ListRepositories(ctx, id.OwnerID, kind)
		if err != nil {
			return domainError(err)
		}
		repositories := make([]map[string]any, 0, len(values))
		for _, value := range values {
			repositories = append(repositories, repositoryMap(value))
		}
		return appkitmcp.StructuredResult(map[string]any{"repositories": repositories})
	}
}

func getHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		detail, err := svc.GetRepository(ctx, id.OwnerID, args.Kind, args.Name)
		if err != nil {
			return domainError(err)
		}
		return appkitmcp.StructuredResult(map[string]any{
			"repository": repositoryMap(detail.Repository), "head": detail.Head, "branches": detail.Branches,
			"clone_url": "/srv/repos/git/" + detail.Repository.Kind + "/" + detail.Repository.Name + ".git",
		})
	}
}

func renameHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		value, err := svc.RenameRepository(ctx, id.OwnerID, args.Kind, args.Name, args.To)
		return structured(repositoryMap(value), err)
	}
}

func deleteHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		value, err := svc.DeleteRepository(ctx, id.OwnerID, args.Kind, args.Name)
		return structured(repositoryMap(value), err)
	}
}

func mergeHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		if _, err := svc.GetRepository(ctx, id.OwnerID, args.Kind, args.Name); err != nil {
			return domainError(err)
		}
		value, err := svc.Merge(ctx, args.Kind, args.Name, args.Branch, id.ClientID)
		return structured(map[string]any{"merged": value.Merged, "rev": value.Rev, "strategy": value.Strategy}, err)
	}
}

func statusSetHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		if _, err := svc.GetRepository(ctx, id.OwnerID, args.Kind, args.Name); err != nil {
			return domainError(err)
		}
		var detail *string
		if args.Detail != "" {
			detail = &args.Detail
		}
		value, err := svc.SetStatus(ctx, repos.Status{Kind: args.Kind, Name: args.Name, SHA: args.SHA, CheckName: args.Check, State: args.State, Detail: detail, Actor: id.ClientID})
		return structured(statusMap(value), err)
	}
}

func statusListHandler(svc Service) func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return func(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
		args, err := decode(raw)
		if err != nil {
			return domainError(err)
		}
		if _, err := svc.GetRepository(ctx, id.OwnerID, args.Kind, args.Name); err != nil {
			return domainError(err)
		}
		values, err := svc.ListStatuses(ctx, args.Kind, args.Name, args.SHA)
		if err != nil {
			return domainError(err)
		}
		statuses := make([]map[string]any, 0, len(values))
		for _, value := range values {
			statuses = append(statuses, statusMap(value))
		}
		return appkitmcp.StructuredResult(map[string]any{"statuses": statuses})
	}
}

func structured(value map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return domainError(err)
	}
	return appkitmcp.StructuredResult(value)
}

func domainError(err error) (map[string]any, error) {
	code := appkitmcp.ErrInternal
	switch {
	case errors.Is(err, repos.ErrValidation):
		code = appkitmcp.ErrValidation
	case errors.Is(err, repos.ErrNotFound):
		code = appkitmcp.ErrNotFound
	case errors.Is(err, repos.ErrConflict), errors.Is(err, repos.ErrForcePush):
		code = appkitmcp.ErrConflict
	case errors.Is(err, repos.ErrTooLarge):
		code = appkitmcp.ErrTooLarge
	}
	return appkitmcp.ErrorResult(code, err.Error()), nil
}

func repositoryMap(value repos.Repository) map[string]any {
	return map[string]any{
		"kind": value.Kind, "name": value.Name, "owner_id": value.OwnerID, "owner_email": value.OwnerEmail,
		"default_branch": value.DefaultBranch, "archived": value.ArchivedAt != nil,
	}
}

func statusMap(value repos.Status) map[string]any {
	detail := ""
	if value.Detail != nil {
		detail = *value.Detail
	}
	return map[string]any{
		"kind": value.Kind, "name": value.Name, "sha": value.SHA, "check": value.CheckName,
		"state": value.State, "detail": detail, "actor": value.Actor, "updated_at": value.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
}
