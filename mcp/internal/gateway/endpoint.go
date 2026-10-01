package gateway

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

type servicesInput struct{}
type serviceOutput struct {
	Name        string  `json:"name" mcp:"required" description:"The service's name."`
	Description string  `json:"description" mcp:"required" description:"What the service is for, from the services file; empty when it is not installed."`
	Available   bool    `json:"available" mcp:"required" description:"Whether the service can be used now."`
	Reason      *string `json:"reason" description:"Why the service is unavailable: disabled or not installed. Present only when available is false."`
}
type servicesOutput struct {
	Services []serviceOutput `json:"services" mcp:"required" description:"Every service this connection reaches, in name order."`
}
type describeInput struct {
	Service string  `json:"service" mcp:"required" description:"The service's name, as services lists it."`
	Tool    *string `json:"tool" description:"A tool's name, as describe lists it. Leave it out to list the service's tools."`
}
type runInput struct {
	Service string          `json:"service" mcp:"required" description:"The service's name, as services lists it."`
	Tool    string          `json:"tool" mcp:"required" description:"The tool's name, as describe lists it."`
	Args    json.RawMessage `json:"args" description:"The tool's arguments, matching its input schema. Leave it out for {}."`
}

const servicesDescription = "List the services this connection reaches, and whether each is available.\n\nAn unavailable service says why: disabled or not installed. Call describe to see a service's tools."
const describeDescription = "Show a service's tools, or one tool's full description and schemas.\n\nWithout tool, lists each tool of the service with its one-line summary and its kind: a read tool runs with call, a write tool with mutate. With tool, gives that tool's full description, its input schema, its output schema when it has one, and its kind. Call describe before call or mutate."
const callDescription = "Run a read tool of a service and return its result.\n\nName the service and the tool as describe shows them, and pass the tool's arguments in args, an object matching its input schema ({} when left out). Only a tool of kind read runs here; a write tool runs with mutate."
const mutateDescription = "Run a write tool of a service and return its result.\n\nName the service and the tool as describe shows them, and pass the tool's arguments in args, an object matching its input schema ({} when left out). Only a tool of kind write runs here; a read tool runs with call. A write tool may change or remove data."

func instructions(ctx context.Context) string {
	first := "This server reaches no services."
	if s := requestStateFrom(ctx); s != nil && len(s.reached) > 0 {
		first = "This server reaches these services: " + strings.Join(s.reached, ", ") + "."
	}
	return first + "\nCall services to see which are available, and describe before call or mutate."
}

// NewServer constructs the appkit transport and registers the gateway tools.
func NewServer(version string, stderr io.Writer) *mcp.Server {
	s := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: version, Stderr: stderr, Instructions: instructions})
	mcp.AddTool(s, mcp.Tool[servicesInput, servicesOutput]{Name: "services", Description: servicesDescription, Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, _ servicesInput) (servicesOutput, error) {
		out := servicesOutput{Services: []serviceOutput{}}
		if state := requestStateFrom(ctx); state != nil {
			for _, n := range state.reached {
				d, a, r := serviceStatus(state.entries, n)
				out.Services = append(out.Services, serviceOutput{Name: n, Description: d, Available: a, Reason: r})
			}
		}
		return out, nil
	}})
	mcp.AddRawTool(s, mcp.RawTool[describeInput]{Name: "describe", Description: describeDescription, Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, in describeInput) (mcp.Result, error) {
		return serveBackend(ctx, c, in.Service, in.Tool, nil, "describe", version)
	}})
	for _, registration := range []struct {
		name, description string
		effect            mcp.Effect
	}{{"call", callDescription, mcp.Read}, {"mutate", mutateDescription, mcp.Destructive}} {
		mcp.AddRawTool(s, mcp.RawTool[runInput]{Name: registration.name, Description: registration.description, Effect: registration.effect, Handler: func(ctx context.Context, c identity.Caller, in runInput) (mcp.Result, error) {
			return serveBackend(ctx, c, in.Service, &in.Tool, in.Args, registration.name, version)
		}})
	}
	return s
}
