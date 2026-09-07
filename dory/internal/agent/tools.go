package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/toolkit"
)

type emptyInput struct{}

type delegateInput struct {
	Role   Role   `json:"role" jsonschema:"required,enum=supervisor|worker"`
	Prompt string `json:"prompt" jsonschema:"required,minLength=1"`
}

func (h *harness) tools(address string, role Role) ([]agentkit.Tool, error) {
	switch role {
	case RoleSupervisor:
		return h.supervisorTools(address)
	case RoleWorker:
		return workerTools(h.cfg.Root)
	default:
		return nil, fmt.Errorf("agent: unknown role %q", role)
	}
}

func (h *harness) supervisorTools(address string) ([]agentkit.Tool, error) {
	search, err := agentkit.NewTool("search", "Search stored session entries", func(context.Context, emptyInput) (string, error) {
		return "not implemented", nil
	})
	if err != nil {
		return nil, err
	}
	fetch, err := agentkit.NewTool("fetch", "Fetch one stored session entry", func(context.Context, emptyInput) (string, error) {
		return "not implemented", nil
	})
	if err != nil {
		return nil, err
	}
	remember, err := agentkit.NewTool("remember", "Store a note for this agent", func(context.Context, emptyInput) (string, error) {
		return "not implemented", nil
	})
	if err != nil {
		return nil, err
	}

	var children struct {
		sync.Mutex
		next int
	}
	delegate, err := agentkit.NewTool("delegate", "Run a child agent to completion", func(ctx context.Context, input delegateInput) (string, error) {
		children.Lock()
		children.next++
		childAddress := fmt.Sprintf("%s.%d", address, children.next)
		children.Unlock()
		return h.runAgent(ctx, childAddress, input.Role, input.Prompt)
	})
	if err != nil {
		return nil, err
	}
	return []agentkit.Tool{search, fetch, remember, delegate}, nil
}

func workerTools(root string) ([]agentkit.Tool, error) {
	constructors := []func() (agentkit.Tool, error){
		func() (agentkit.Tool, error) { return toolkit.Bash(root) },
		func() (agentkit.Tool, error) { return toolkit.Read(root) },
		func() (agentkit.Tool, error) { return toolkit.Write(root) },
		func() (agentkit.Tool, error) { return toolkit.Edit(root) },
		func() (agentkit.Tool, error) { return toolkit.Glob(root, toolkit.WithSkip(".git")) },
		func() (agentkit.Tool, error) { return toolkit.Grep(root, toolkit.WithSkip(".git")) },
	}
	tools := make([]agentkit.Tool, 0, len(constructors))
	for _, construct := range constructors {
		tool, err := construct()
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}
