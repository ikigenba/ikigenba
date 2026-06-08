// Package suite owns prompts' suite-specific discovery and identity policy: at
// run spawn it snapshots the box's other loopback MCP services and exposes their
// tools to the in-run agent as an agent.ToolSource, talking to each peer on
// behalf of the run's owner.
//
// Discovery is best-effort by design (decision: a down peer must not break a
// run). An unreadable inventory, an unreachable peer, or a garbled tools/list is
// logged loud and skipped; Discover never returns an error and never panics, and
// a downstream tool failure becomes an is_error tool_result rather than a
// run-crashing Go error (decision #9).
//
// Identity flows in as arguments (composition-root pattern): Discover takes
// manifestRoot/owner/promptID and reads no environment. The runner reads
// PROMPTS_MANIFEST_ROOT and threads it here.
package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"agentkit/agent"
	"agentkit/mcpclient"
	"agentkit/provider"
	"agentkit/wire"

	"appkit/inventory"
)

// selfName is the service excluded from discovery: a run must not spawn another
// run, so the prompts service never lists its own tools to the in-run agent.
const selfName = "prompts"

// perCallTimeout bounds every outbound MCP call (tools/list during discovery,
// tools/call during dispatch). Generous but well under any run TTL, so a wedged
// peer can never hang discovery or a tool dispatch forever.
const perCallTimeout = 30 * time.Second

// clientIDPrefix is prepended to the run's prompt id to form the X-Client-Id the
// peers see, marking the call as originating from a prompts run.
const clientIDPrefix = "prompts:"

// Discover snapshots, at run spawn, the suite's loopback MCP tools available to
// the in-run agent on behalf of owner. It reads the box inventory under
// manifestRoot, excludes the prompts service itself, and concurrently lists each
// remaining peer's tools, attaching the run's identity headers. Best-effort:
// unreachable or garbled peers are logged and skipped; it never returns an error
// and never panics, always returning a non-nil ToolSource (possibly empty).
func Discover(ctx context.Context, manifestRoot, owner, promptID string) agent.ToolSource {
	headers := map[string]string{
		"X-Owner-Email": owner,
		"X-Client-Id":   clientIDPrefix + promptID,
	}

	src := &source{
		tools: map[string]provider.Tool{},
		owner: map[string]*mcpclient.Client{},
	}

	services, err := inventory.Read(manifestRoot)
	if err != nil {
		// Treat an unreadable inventory as zero services: discovery degrades to an
		// empty source rather than failing the run.
		slog.Error("suite discovery: inventory read failed, no suite tools",
			"manifest_root", manifestRoot, "err", err)
		return src
	}

	// Concurrently list tools from every non-self peer. Each peer's result lands
	// in its own slot; a guarding mutex serializes the index build.
	type listing struct {
		svc    inventory.Service
		client *mcpclient.Client
		tools  []mcpclient.Tool
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	var listings []listing

	for _, svc := range services {
		if svc.Name == selfName {
			continue // self-exclusion: no run-spawns-run.
		}
		if svc.Port == "" {
			slog.Error("suite discovery: peer has no port, skipping", "service", svc.Name)
			continue
		}

		endpoint := fmt.Sprintf("http://127.0.0.1:%s/mcp", svc.Port)
		client := mcpclient.New(endpoint, headers, perCallTimeout)

		wg.Add(1)
		go func(svc inventory.Service, client *mcpclient.Client) {
			defer wg.Done()
			tools, lerr := client.ListTools(ctx)
			if lerr != nil {
				// A down or garbled peer must not break discovery: log loud, skip it.
				slog.Error("suite discovery: tools/list failed, skipping peer",
					"service", svc.Name, "endpoint", endpoint, "err", lerr)
				return
			}
			mu.Lock()
			listings = append(listings, listing{svc: svc, client: client, tools: tools})
			mu.Unlock()
		}(svc, client)
	}
	wg.Wait()

	// Build the index name -> (tool, owning client). Names are service-prefixed
	// (ikigenba_<svc>_*) so cross-peer collisions are structurally impossible; if
	// one ever appears, log loud and keep the first rather than silently shadow.
	for _, l := range listings {
		for _, t := range l.tools {
			if _, dup := src.owner[t.Name]; dup {
				slog.Error("suite discovery: duplicate tool name across peers, keeping first",
					"tool", t.Name, "service", l.svc.Name)
				continue
			}
			src.tools[t.Name] = provider.Tool{
				Name:        t.Name,
				InputSchema: t.InputSchema,
			}
			src.owner[t.Name] = l.client
		}
	}

	return src
}

// source is the concrete agent.ToolSource returned by Discover. It holds the
// snapshot taken at run spawn: a name->descriptor map and a name->owning-client
// index. It is read-only after construction and safe for concurrent dispatch.
type source struct {
	tools map[string]provider.Tool
	owner map[string]*mcpclient.Client
}

// Descriptors returns the provider-neutral advertisement for every discovered
// tool. provider.Tool carries Name and InputSchema only (no description field),
// so the upstream description is not advertised; the schema round-trips as raw
// JSON unchanged.
func (s *source) Descriptors() []provider.Tool {
	out := make([]provider.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t)
	}
	return out
}

// Owns reports whether name is one of the discovered suite tools (exact match).
func (s *source) Owns(name string) bool {
	_, ok := s.owner[name]
	return ok
}

// Dispatch routes the named tool to its owning peer's tools/call and flattens
// the result into a wire.ToolResultBlock. It NEVER returns a non-nil Go error
// (decision #9): a transport failure or a downstream isError becomes an is_error
// block so the in-run agent loop continues. The tool_use id is attached by the
// agent loop, so the returned block carries an empty ToolUseID.
func (s *source) Dispatch(ctx context.Context, name string, input json.RawMessage) (wire.ToolResultBlock, error) {
	client, ok := s.owner[name]
	if !ok {
		// Owns gates Dispatch, so this should be unreachable; surface it as an
		// is_error block rather than a Go error to honor the never-crash contract.
		return errBlock(fmt.Sprintf("suite: no peer owns tool %q", name)), nil
	}

	text, isError, err := client.CallTool(ctx, name, input)
	if err != nil {
		// Transport / JSON-RPC failure: surface the message as an is_error result.
		return errBlock(fmt.Sprintf("suite: tool %q failed: %v", name, err)), nil
	}

	block, berr := wire.NewToolResultBlock("", isError, text)
	if berr != nil {
		// Construction failure should never happen for a plain string; fall back to
		// a minimal is_error block, still without a Go error.
		return errBlock(fmt.Sprintf("suite: tool %q result encode failed: %v", name, berr)), nil
	}
	return block, nil
}

// errBlock builds a minimal is_error tool_result carrying msg. It tolerates a
// (practically impossible) marshal failure of a plain string by hand-building
// the block so the never-crash contract holds even in the degenerate case.
func errBlock(msg string) wire.ToolResultBlock {
	block, err := wire.NewToolResultBlock("", true, msg)
	if err != nil {
		return wire.ToolResultBlock{
			Type:    "tool_result",
			IsError: true,
			Content: json.RawMessage(`""`),
		}
	}
	return block
}
