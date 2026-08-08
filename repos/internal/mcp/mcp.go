// Package mcp exposes repository custody through the shared appkit MCP
// transport.
package mcp

import (
	"context"
	"fmt"
	"net/http"

	"appkit"
	appkitmcp "appkit/mcp"
	"repos/internal/repos"
)

const Instructions = "Repos manages git repositories, version history, branches, commits, clones, and merges. Create a repository, work through its clone door, then merge a ready branch; call guide for conventions and examples."

// Service is the domain seam used by the MCP surface.
type Service interface {
	CreateRepository(context.Context, repos.Repository) (repos.Repository, error)
	ListRepositories(context.Context, string, *string) ([]repos.Repository, error)
	GetRepository(context.Context, string, string, string) (repos.RepositoryDetail, error)
	RenameRepository(context.Context, string, string, string, string) (repos.Repository, error)
	DeleteRepository(context.Context, string, string, string) (repos.Repository, error)
	Merge(context.Context, string, string, string, string) (repos.MergeResult, error)
	SetStatus(context.Context, repos.Status) (repos.Status, error)
	ListStatuses(context.Context, string, string, string) ([]repos.Status, error)
}

func NewHandler(svc Service, rt *appkit.Router) (http.Handler, error) {
	if svc == nil {
		return nil, fmt.Errorf("mcp: repos service is required")
	}
	if rt == nil {
		return nil, fmt.Errorf("mcp: router is required")
	}
	return appkitmcp.New(appkitmcp.Options{
		Service:       rt.Service(),
		Version:       rt.Version(),
		Instructions:  Instructions,
		Tools:         Tools(svc),
		Health:        rt.Health(),
		Events:        rt.Events(),
		Publishes:     rt.Publishes(),
		Subscriptions: rt.Subscriptions(),
	})
}
