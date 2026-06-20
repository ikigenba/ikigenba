// Package wiki wires the service skeleton into the shared appkit chassis.
package wiki

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"appkit"
	agentkit "github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/anthropic"

	"wiki/internal/db"
	"wiki/internal/llm"
	"wiki/internal/mcp"
	"wiki/internal/worker"
)

const (
	App   = "wiki"
	Mount = "/srv/wiki/"
	Port  = 3006

	ModelID           = anthropic.ModelSonnet46
	WorkerConcurrency = 1
	SearchDefault     = 8
	SearchCap         = 32
)

// Config is wiki's service-side runtime configuration.
type Config struct {
	ModelID           string
	WorkerConcurrency int
	SearchDefault     int
	SearchCap         int
	Provider          agentkit.Provider
	LLM               *llm.Client
}

// NewConfig reads wiki's secret and constructs the shared LLM provider/client.
func NewConfig(getenv func(string) string) (Config, error) {
	apiKey := strings.TrimSpace(getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		return Config{}, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	provider := anthropic.New(apiKey)
	return Config{
		ModelID:           ModelID,
		WorkerConcurrency: WorkerConcurrency,
		SearchDefault:     SearchDefault,
		SearchCap:         SearchCap,
		Provider:          provider,
		LLM:               llm.NewClient(provider, ModelID),
	}, nil
}

// Spec returns the production-shaped appkit service declaration.
func Spec() appkit.Spec {
	return appkit.Spec{
		App:   App,
		Mount: Mount,
		Port:  Port,
		MCP:   true,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "MODEL_ID", Value: ModelID},
			{Key: "WORKER_CONCURRENCY", Value: strconv.Itoa(WorkerConcurrency)},
			{Key: "SEARCH_DEFAULT", Value: strconv.Itoa(SearchDefault)},
			{Key: "SEARCH_CAP", Value: strconv.Itoa(SearchCap)},
		},
		Migrations: db.FS,
		Config: func(getenv func(string) string) (any, error) {
			return NewConfig(getenv)
		},
		Handlers: func(rt *appkit.Router) error {
			if rt.DB() == nil {
				return fmt.Errorf("wiki: no DB handle on router")
			}
			rt.Handle("POST /mcp", rt.RequireIdentity(
				mcp.NewHandler(rt.Version(), rt.Service(), rt.Health())))
			return nil
		},
		Workers: []func(ctx context.Context) error{worker.Run},
	}
}

// Main enters the shared appkit dispatcher.
func Main() {
	if serveCommand(os.Args[1:]) {
		if _, err := NewConfig(os.Getenv); err != nil {
			fmt.Fprintf(os.Stderr, "wiki: %v\n", err)
			os.Exit(1)
		}
	}
	appkit.Main(Spec())
}

func serveCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return args[0] == "serve"
}
