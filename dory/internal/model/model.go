// Package model resolves role-specific model configuration.
package model

import (
	"errors"
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
	plan Plan
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
	return &Factory{plan: plan}, nil
}

// Plan returns the factory's resolved model plan.
func (f *Factory) Plan() Plan {
	return f.plan
}

// New builds a fresh conversation. Conversation construction is completed in
// the next phase; the method is present now to establish the package surface.
func (f *Factory) New(_ []agentkit.Tool, _ *agentkit.Log) (*agentkit.Conversation, error) {
	return nil, errors.New("model conversation construction is not implemented")
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
