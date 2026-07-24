package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/openai"
	"github.com/ikigenba/agentkit/openai/subscription"

	"prompts/internal/prompt"
)

// ResolveAuthPath returns the explicit subscription credential path or the
// auth.json file beside the prompts database.
func ResolveAuthPath(getenv func(string) string) string {
	if path := getenv("PROMPTS_OPENAI_AUTH_PATH"); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(getenv("PROMPTS_DB_PATH")), "auth.json")
}

// SubAuth owns the single refresh-token lineage shared by this process.
type SubAuth struct {
	path  string
	mu    sync.Mutex
	store *subscription.Store
}

func NewSubAuth(path string) *SubAuth { return &SubAuth{path: path} }

// Available reports whether the operator-provisioned credential exists.
func (a *SubAuth) Available() bool {
	if a == nil {
		return false
	}
	_, err := os.Stat(a.path)
	return err == nil
}

// Store lazily loads and caches the credential store. Failed loads are retried
// so an operator can repair or provision the file without restarting.
func (a *SubAuth) Store() (*subscription.Store, error) {
	if a == nil {
		return nil, fmt.Errorf("load OpenAI subscription credential: no SubAuth configured")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
		return a.store, nil
	}
	store, err := subscription.Load(a.path)
	if err != nil {
		return nil, fmt.Errorf("load OpenAI subscription credential %q: %w", a.path, err)
	}
	a.store = store
	return store, nil
}

// NewBuilder returns the common provider factory used by runs and completions.
func NewBuilder(sub *SubAuth) func(prompt.Config, func(string) string) (agentkit.Provider, error) {
	return func(cfg prompt.Config, getenv func(string) string) (agentkit.Provider, error) {
		if cfg.Auth != "sub" {
			return Build(cfg, getenv)
		}
		store, err := sub.Store()
		if err != nil {
			return nil, err
		}
		return openai.New(openai.Subscription(store)), nil
	}
}
