package session

import (
	"context"
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/toolkit"
)

// Open constructs an authenticated conversation and its rooted tools.
func Open(cfg Config) (*Session, error) {
	plan, err := Resolve(cfg)
	if err != nil {
		return nil, err
	}

	rotator, err := credential(cfg, plan)
	if err != nil {
		return nil, err
	}
	authenticator, err := plan.Offering.Authenticator(rotator)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	endpoint, err := agentkit.NewEndpoint(authenticator, agentkit.WithBaseURL(plan.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("endpoint %q: %w", plan.BaseURL, err)
	}
	tools, err := sessionTools(cfg.Root)
	if err != nil {
		return nil, err
	}
	conversation, err := agentkit.New(plan.Offering.WireFormat, endpoint, plan.Model, agentkit.Config{
		Tools: tools,
		Settings: agentkit.Settings{
			Options: agentkit.Options(cfg.Settings),
		},
		Log: cfg.Log,
	})
	if err != nil {
		return nil, fmt.Errorf("conversation: %w", err)
	}
	return &Session{plan: plan, conversation: conversation}, nil
}

func credential(cfg Config, plan Plan) (agentkit.Rotator, error) {
	switch plan.AuthMode {
	case agentkit.AuthModeAPIKey:
		if cfg.Getenv == nil {
			return nil, fmt.Errorf("environment variable %q cannot be read", plan.EnvVar)
		}
		key := cfg.Getenv(plan.EnvVar)
		if key == "" {
			return nil, fmt.Errorf("environment variable %q is empty", plan.EnvVar)
		}
		return agentkit.APIKeyRotator(key), nil
	case agentkit.AuthModeOAuth:
		rotator := agentkit.OAuthRotator(agentkit.FileTokenStore(plan.AuthFile))
		if _, err := rotator.Token(context.Background()); err != nil {
			return nil, fmt.Errorf("oauth token file %q: %w", plan.AuthFile, err)
		}
		return rotator, nil
	default:
		return nil, fmt.Errorf("auth %q is not supported", plan.AuthMode)
	}
}

func sessionTools(root string) ([]agentkit.Tool, error) {
	constructors := []struct {
		name string
		new  func() (agentkit.Tool, error)
	}{
		{name: "Bash", new: func() (agentkit.Tool, error) { return toolkit.Bash(root) }},
		{name: "Read", new: func() (agentkit.Tool, error) { return toolkit.Read(root) }},
		{name: "Write", new: func() (agentkit.Tool, error) { return toolkit.Write(root) }},
		{name: "Edit", new: func() (agentkit.Tool, error) { return toolkit.Edit(root) }},
		{name: "Glob", new: func() (agentkit.Tool, error) { return toolkit.Glob(root, toolkit.WithSkip(".git")) }},
		{name: "Grep", new: func() (agentkit.Tool, error) { return toolkit.Grep(root, toolkit.WithSkip(".git")) }},
	}

	tools := make([]agentkit.Tool, 0, len(constructors))
	for _, constructor := range constructors {
		tool, err := constructor.new()
		if err != nil {
			return nil, fmt.Errorf("construct %s tool: %w", constructor.name, err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}
