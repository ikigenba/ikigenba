package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/store"
	"github.com/ikigenba/ikigenba/toolkit"
)

type searchInput struct {
	Query   string `json:"query" jsonschema:"required"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Page    int    `json:"page" jsonschema:"minimum=1"`
}

type fetchInput struct {
	ID int64 `json:"id" jsonschema:"required"`
}

type rememberInput struct {
	Text string `json:"text" jsonschema:"required,minLength=1"`
}

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
	search, err := agentkit.NewTool("search", "Search stored session entries", func(_ context.Context, input searchInput) (string, error) {
		filter, err := searchFilter(input)
		if err != nil {
			return "", err
		}
		page, err := h.cfg.Store.Search(input.Query, filter)
		if err != nil {
			return "", err
		}
		return marshalToolResult(page)
	})
	if err != nil {
		return nil, err
	}
	fetch, err := agentkit.NewTool("fetch", "Fetch one stored session entry", func(_ context.Context, input fetchInput) (string, error) {
		entry, err := h.cfg.Store.Fetch(input.ID)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("fetch entry %d: %w", input.ID, err)
		}
		if err != nil {
			return "", err
		}
		return marshalToolResult(entry)
	})
	if err != nil {
		return nil, err
	}
	remember, err := agentkit.NewTool("remember", "Store a note for this agent", func(_ context.Context, input rememberInput) (string, error) {
		if _, err := h.cfg.Store.Add(address, store.KindNote, input.Text, nil); err != nil {
			return "", err
		}
		return "ok", nil
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

func searchFilter(input searchInput) (store.Filter, error) {
	filter := store.Filter{Address: input.Address, Page: input.Page}
	if input.Kind == "" {
		return filter, nil
	}
	for _, value := range strings.Split(input.Kind, ",") {
		excluded := strings.HasPrefix(value, "-")
		kindName := value
		if excluded {
			kindName = strings.TrimPrefix(value, "-")
		}
		kind, ok := storeKind(kindName)
		if !ok {
			return store.Filter{}, fmt.Errorf("unknown store kind %q", kindName)
		}
		if excluded {
			filter.Exclude = append(filter.Exclude, kind)
		} else {
			filter.Kinds = append(filter.Kinds, kind)
		}
	}
	return filter, nil
}

func storeKind(value string) (store.Kind, bool) {
	switch store.Kind(value) {
	case store.KindNote, store.KindPrompt, store.KindReport, store.KindTranscript:
		return store.Kind(value), true
	default:
		return "", false
	}
}

func marshalToolResult(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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
