// Package mcp exposes cron's crontab CRUD tools through the shared appkit MCP
// transport.
package mcp

import (
	"errors"
	"fmt"
	"net/http"

	"appkit"
	appkitmcp "appkit/mcp"

	"cron/internal/crontab"
)

// Instructions describes cron's MCP surface to clients during initialize.
const Instructions = "Named UTC cron schedules that publish a cron:tick/<name> event on " +
	"a timer. Create a schedule, then wire consumers to its event."

// NewHandler builds the POST /mcp handler from the appkit Router seam. The
// shared transport owns JSON-RPC, health, and reflection; cron declares only its
// crontab domain tools.
func NewHandler(store *crontab.Store, rt *appkit.Router) (http.Handler, error) {
	if store == nil {
		panic("mcp: crontab store is required")
	}
	if rt == nil {
		return nil, fmt.Errorf("mcp: router is required")
	}
	return appkitmcp.New(appkitmcp.Options{
		Service:       rt.Service(),
		Version:       rt.Version(),
		Instructions:  Instructions,
		Tools:         Tools(store),
		Health:        rt.Health(),
		Events:        rt.Events(),
		Publishes:     rt.Publishes(),
		Subscriptions: rt.Subscriptions(),
	})
}

// toolErr maps domain failures onto appkit's closed MCP error vocabulary.
func toolErr(err error) map[string]any {
	var pe *parseError
	switch {
	case errors.As(err, &pe):
		return appkitmcp.ErrorResult(appkitmcp.ErrValidation, err.Error())
	case errors.Is(err, crontab.ErrExists):
		return appkitmcp.ErrorResult(appkitmcp.ErrConflict, err.Error())
	case errors.Is(err, crontab.ErrNotFound):
		return appkitmcp.ErrorResult(appkitmcp.ErrNotFound, err.Error())
	case errors.Is(err, crontab.ErrInvalid):
		return appkitmcp.ErrorResult(appkitmcp.ErrValidation, err.Error())
	default:
		return appkitmcp.ErrorResult(appkitmcp.ErrInternal, "internal error")
	}
}
