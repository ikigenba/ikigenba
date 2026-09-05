package session

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Resolve selects the transport, authentication, and endpoint for a session.
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
		BaseURL:  cfg.BaseURL,
	}
	if plan.AuthFile == "" {
		plan.AuthFile = filepath.Join(cfg.Home, ".agent-repl", cfg.Provider+"-auth.json")
	}
	if plan.BaseURL == "" {
		plan.BaseURL = offering.BaseURL
	}

	if cfg.Auth != "" {
		plan.AuthMode = agentkit.AuthMode(cfg.Auth)
		if !slices.Contains(offering.AuthModes, plan.AuthMode) {
			return Plan{}, fmt.Errorf("auth %q is not supported by %s", cfg.Auth, cfg.Provider)
		}
		return plan, nil
	}

	plan.AuthMode = agentkit.AuthModeAPIKey
	if slices.Contains(offering.AuthModes, agentkit.AuthModeOAuth) {
		if _, statErr := os.Stat(plan.AuthFile); statErr == nil {
			plan.AuthMode = agentkit.AuthModeOAuth
		}
	}
	return plan, nil
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
