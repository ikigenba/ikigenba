// Package mcp exposes repository custody through the shared appkit MCP
// transport.
package mcp

import (
	"fmt"
	"net/http"

	"appkit"
	appkitmcp "appkit/mcp"
)

const Instructions = "Manage repositories held by the repos service."

// Service is the domain seam used by the MCP surface. It remains empty until
// the v2 tools are introduced.
type Service interface{}

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
