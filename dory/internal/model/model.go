// Package model resolves role-specific model configuration.
package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Config contains the choices used to resolve and open a role's model.
type Config struct {
	Provider   string
	Model      string
	Wire       string
	Auth       string
	AuthFile   string
	BaseURL    string
	MaxContext int64
	Settings   map[string]string
	Home       string
	Getenv     func(string) string
}

// Plan is the model configuration selected before any network access.
type Plan struct {
	Offering   agentkit.Offering
	Model      string
	AuthMode   agentkit.AuthMode
	EnvVar     string
	AuthFile   string
	BaseURL    string
	MaxContext int64
}

// Factory holds the fixed configuration for fresh role conversations.
type Factory struct {
	plan     Plan
	endpoint agentkit.Endpoint
	settings agentkit.Options
}

// Resolve turns a role configuration into a concrete model plan.
func Resolve(cfg Config) (Plan, error) {
	offering, model, err := resolveOffering(cfg)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		Offering: offering,
		Model:    model,
		EnvVar:   strings.ToUpper(cfg.Provider) + "_API_KEY",
		AuthFile: cfg.AuthFile,
	}
	if plan.AuthFile == "" {
		plan.AuthFile = filepath.Join(cfg.Home, ".dory", cfg.Provider+"-auth.json")
	}

	plan.AuthMode = resolveAuthMode(cfg.Auth, offering, plan.AuthFile)
	endpoint, ok := endpointFor(offering, plan.AuthMode)
	if !ok {
		return Plan{}, fmt.Errorf("auth %q is unavailable for model %q on provider %q", plan.AuthMode, cfg.Model, cfg.Provider)
	}

	plan.BaseURL = endpoint.BaseURL
	if cfg.BaseURL != "" {
		plan.BaseURL = cfg.BaseURL
	}
	plan.MaxContext = offering.Context
	if cfg.MaxContext >= 0 {
		plan.MaxContext = cfg.MaxContext
	}

	return plan, nil
}

// Open resolves cfg and returns a factory for the selected model plan.
func Open(cfg Config) (*Factory, error) {
	plan, err := Resolve(cfg)
	if err != nil {
		return nil, err
	}
	rotator, err := openRotator(cfg, plan)
	if err != nil {
		return nil, err
	}
	authenticator, err := plan.Offering.Authenticator(rotator)
	if err != nil {
		return nil, fmt.Errorf("create authenticator: %w", err)
	}
	endpoint, err := agentkit.NewEndpoint(authenticator, agentkit.WithBaseURL(plan.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}
	return &Factory{plan: plan, endpoint: endpoint, settings: cloneOptions(cfg.Settings)}, nil
}

func openRotator(cfg Config, plan Plan) (agentkit.Rotator, error) {
	switch plan.AuthMode {
	case agentkit.AuthModeAPIKey:
		getenv := cfg.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		key := getenv(plan.EnvVar)
		if key == "" {
			return nil, fmt.Errorf("environment variable %q is empty", plan.EnvVar)
		}
		return agentkit.APIKeyRotator(key), nil
	case agentkit.AuthModeOAuth:
		rotator := agentkit.OAuthRotator(agentkit.FileTokenStore(plan.AuthFile))
		if _, err := rotator.Token(context.Background()); err != nil {
			return nil, fmt.Errorf("read OAuth token file %q: %w", plan.AuthFile, err)
		}
		return rotator, nil
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", plan.AuthMode)
	}
}

func cloneOptions(settings map[string]string) agentkit.Options {
	options := make(agentkit.Options, len(settings))
	for key, value := range settings {
		options[key] = value
	}
	return options
}

// Plan returns the factory's resolved model plan.
func (f *Factory) Plan() Plan {
	return f.plan
}

// New builds a fresh conversation with the role's fixed model configuration.
func (f *Factory) New(tools []agentkit.Tool, log *agentkit.Log) (*agentkit.Conversation, error) {
	return agentkit.New(f.plan.Offering.WireFormat, f.endpoint, f.plan.Model, agentkit.Config{
		Tools:    tools,
		Settings: agentkit.Settings{Options: f.settings},
		Log:      log,
		Limits:   agentkit.Limits{MaxContextTokens: f.plan.MaxContext},
	})
}

func resolveOffering(cfg Config) (agentkit.Offering, string, error) {
	cataloged := false
	for _, entry := range agentkit.Catalog() {
		if entry.Model == cfg.Model {
			cataloged = true
			break
		}
	}
	if cataloged {
		offering, err := agentkit.Lookup(cfg.Model, agentkit.Host(cfg.Provider), agentkit.WireName(cfg.Wire))
		if err != nil {
			return agentkit.Offering{}, "", fmt.Errorf("look up model %q: %w", cfg.Model, err)
		}
		return offering, offering.WireModel, nil
	}

	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.Host != agentkit.Host(cfg.Provider) {
				continue
			}
			if cfg.Wire != "" && offering.WireName != agentkit.WireName(cfg.Wire) {
				continue
			}
			offering.WireModel = cfg.Model
			offering.Context = 0
			return offering, cfg.Model, nil
		}
	}

	return agentkit.Offering{}, "", fmt.Errorf("no catalog offering for provider %q and wire %q", cfg.Provider, cfg.Wire)
}

func resolveAuthMode(configured string, offering agentkit.Offering, authFile string) agentkit.AuthMode {
	if configured != "" {
		return agentkit.AuthMode(configured)
	}
	if _, ok := endpointFor(offering, agentkit.AuthModeOAuth); ok {
		if _, err := os.Stat(authFile); err == nil {
			return agentkit.AuthModeOAuth
		}
	}
	return agentkit.AuthModeAPIKey
}

func endpointFor(offering agentkit.Offering, mode agentkit.AuthMode) (agentkit.EndpointSpec, bool) {
	for _, endpoint := range offering.Endpoints {
		if endpoint.AuthMode == mode {
			return endpoint, true
		}
	}
	return agentkit.EndpointSpec{}, false
}
