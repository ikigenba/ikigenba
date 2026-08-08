package mcp

import appkitmcp "appkit/mcp"

// Tools returns the v2 domain tool table. The teardown phase intentionally
// leaves it empty while retaining the shared MCP transport.
func Tools(Service) []appkitmcp.Tool { return []appkitmcp.Tool{} }
