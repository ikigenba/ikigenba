// Package suite maps the box inventory to AgentKit MCP attachments.
package suite

import (
	"context"
	"fmt"

	"appkit/inventory"
	"github.com/ikigenba/agentkit"
	"registry"
)

const (
	selfName       = "prompts"
	clientIDPrefix = "prompts:"
)

// Discover returns one MCP attachment for every non-prompts service in the
// inventory. It performs no network I/O; AgentKit resolves tools at Send time.
func Discover(_ context.Context, manifestRoot, ownerID, ownerEmail, promptID string) []agentkit.MCPServer {
	services, err := inventory.Read(manifestRoot)
	if err != nil {
		return []agentkit.MCPServer{}
	}

	servers := make([]agentkit.MCPServer, 0, len(services))
	for _, svc := range services {
		if svc.Name == selfName {
			continue
		}
		port, ok := registry.Port(svc.Name)
		if !ok {
			continue
		}
		servers = append(servers, agentkit.MCPServer{
			Name: "ikigenba_" + svc.Name,
			URL:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
			Headers: map[string]string{
				"X-Owner-Id":    ownerID,
				"X-Owner-Email": ownerEmail,
				"X-Client-Id":   clientIDPrefix + promptID,
			},
		})
	}
	return servers
}
