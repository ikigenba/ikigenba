// Package agent defines the agents that participate in a pass and the public
// boundary for running one.
package agent

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/model"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

// Role identifies an agent's responsibility within a pass.
type Role string

const (
	// RoleSupervisor identifies the root agent that directs a pass.
	RoleSupervisor Role = "supervisor"
	// RoleWorker identifies an agent performing delegated work.
	RoleWorker Role = "worker"
)

// SupervisorPrompt is the fixed system prompt for a supervisor.
const SupervisorPrompt = `You are the supervisor for a single pass. Direct the work toward the user's request, delegate focused tasks when useful, and return a clear final report.`

// WorkerPrompt is the fixed system prompt for a worker.
const WorkerPrompt = `You are a worker within a single pass. Complete the task assigned by the supervisor and return a concise, accurate report of the result.`

// Trace is what a pass tells the outside as it runs.
type Trace interface {
	Event(address string, ev agentkit.Event)
	Error(address string, err error)
	Record(rec agentkit.LogRecord) string
}

// Config contains the dependencies and settings for a pass.
type Config struct {
	Store      *store.Store
	Supervisor *model.Factory
	Worker     *model.Factory
	Root       string
	Now        func() time.Time
	Trace      Trace
}

// Result describes the root agent's output and the resources used by a pass.
type Result struct {
	Address string
	Report  string
	Usage   agentkit.Usage
	Cost    agentkit.Cost
}

// RunPass runs one root supervisor and everything it delegates.
func RunPass(ctx context.Context, cfg Config, prompt string) (Result, error) {
	return runPass(ctx, cfg, prompt)
}
