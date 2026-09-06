package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Resolve selects the transport, authentication, and endpoint for a session.
func Resolve(cfg Config) (Plan, error) {
	offering, model, err := resolveOffering(cfg)
	if err != nil {
		return Plan{}, err
	}

	authFile := cfg.AuthFile
	if authFile == "" {
		authFile = filepath.Join(cfg.Home, ".agent-repl", cfg.Provider+"-auth.json")
	}
	authMode, err := resolveAuthMode(cfg, offering, authFile)
	if err != nil {
		return Plan{}, err
	}
	baseURL, err := resolveBaseURL(cfg, offering, authMode)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Offering: offering,
		Model:    model,
		AuthMode: authMode,
		EnvVar:   strings.ToUpper(cfg.Provider) + "_API_KEY",
		AuthFile: authFile,
		BaseURL:  baseURL,
	}, nil
}

func resolveAuthMode(cfg Config, offering agentkit.Offering, authFile string) (agentkit.AuthMode, error) {
	if cfg.Auth != "" {
		mode := agentkit.AuthMode(cfg.Auth)
		if _, ok := endpointForAuthMode(offering, mode); !ok {
			return "", fmt.Errorf("auth %q is not supported by %q", cfg.Auth, cfg.Provider)
		}
		return mode, nil
	}

	_, supportsOAuth := endpointForAuthMode(offering, agentkit.AuthModeOAuth)
	if _, err := os.Stat(authFile); err == nil && supportsOAuth {
		return agentkit.AuthModeOAuth, nil
	}
	return agentkit.AuthModeAPIKey, nil
}

func resolveBaseURL(cfg Config, offering agentkit.Offering, mode agentkit.AuthMode) (string, error) {
	if cfg.BaseURL != "" {
		return cfg.BaseURL, nil
	}
	endpoint, ok := endpointForAuthMode(offering, mode)
	if !ok {
		return "", fmt.Errorf("auth %q is not supported by %q", mode, cfg.Provider)
	}
	return endpoint.BaseURL, nil
}

func endpointForAuthMode(offering agentkit.Offering, mode agentkit.AuthMode) (agentkit.EndpointSpec, bool) {
	for _, endpoint := range offering.Endpoints {
		if endpoint.AuthMode == mode {
			return endpoint, true
		}
	}
	return agentkit.EndpointSpec{}, false
}

func resolveOffering(cfg Config) (agentkit.Offering, string, error) {
	host := agentkit.Host(cfg.Provider)
	wire := agentkit.WireName(cfg.Wire)
	offering, err := agentkit.Lookup(cfg.Model, host, wire)
	if err == nil {
		return offering, offering.WireModel, nil
	}

	for _, entry := range agentkit.Catalog() {
		for _, candidate := range entry.Offerings {
			if candidate.Host != host || (wire != "" && candidate.WireName != wire) {
				continue
			}
			candidate.WireModel = cfg.Model
			return candidate, cfg.Model, nil
		}
	}
	return agentkit.Offering{}, "", fmt.Errorf("model %q: no offering for provider %q and wire %q", cfg.Model, cfg.Provider, cfg.Wire)
}
