package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func serveBackend(ctx context.Context, caller identity.Caller, service string, tool *string, args json.RawMessage, operation, version string) (mcp.Result, error) {
	state := requestStateFrom(ctx)
	if state == nil {
		return mcp.ErrorResult("The gateway request has no connection."), nil
	}
	if !slices.Contains(state.reached, service) {
		return mcp.ErrorResult(fmt.Sprintf("Unknown service: %s. Call services to see the services you can use.", service)), nil
	}
	_, available, reason := serviceStatus(state.entries, service)
	if !available {
		return mcp.ErrorResult(fmt.Sprintf("Service %s is unavailable: %s. Do not retry; call services to see the services you can use.", service, *reason)), nil
	}
	entry, _ := state.entries.Find(service)
	budget := state.cfg.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	callCtx, cancel := context.WithDeadline(ctx, state.received.Add(budget))
	defer cancel()
	transport := telemetry.SocketTransport(entry.Socket)
	defer transport.CloseIdleConnections()
	httpClient := telemetry.SiblingClient(state.cfg.Telemetry, service, transport)
	httpClient.Transport = backendStatusTransport{httpClient.Transport}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend/mcp", HTTPClient: httpClient, Name: ServiceName, Version: version})
	tools, err := client.ListTools(callCtx, caller)
	outcome := backendOutcome(ctx, callCtx, err, false)
	if err != nil {
		return backendRefusal(service, budgetSeconds(budget.Seconds()), false, outcome, err), nil
	}
	if callCtx.Err() != nil {
		return backendRefusal(service, budgetSeconds(budget.Seconds()), false, backendOutcome(ctx, callCtx, callCtx.Err(), false), callCtx.Err()), nil
	}
	var selected *mcp.ToolInfo
	if tool != nil {
		for i := range tools {
			if tools[i].Name == *tool {
				selected = &tools[i]
				break
			}
		}
		if selected == nil {
			return mcp.ErrorResult(fmt.Sprintf("Service %s has no tool %s. Call describe with service %s to see its tools.", service, *tool, service)), nil
		}
	}
	if operation == "describe" {
		return describeResult(service, tools, selected), nil
	}
	if selected == nil {
		return mcp.ErrorResult("The gateway call has no tool."), nil
	}
	kind := backendKind(*selected)
	if operation == "call" && kind == "write" {
		return mcp.ErrorResult(fmt.Sprintf("Tool %s of service %s is a write tool. Use mutate to run it.", *tool, service)), nil
	}
	if operation == "mutate" && kind == "read" {
		return mcp.ErrorResult(fmt.Sprintf("Tool %s of service %s is a read tool. Use call to run it.", *tool, service)), nil
	}
	if args == nil {
		args = json.RawMessage(`{}`)
	}
	result, err := client.CallTool(callCtx, caller, *tool, args)
	outcome = backendOutcome(ctx, callCtx, err, result.IsError())
	if err != nil {
		return backendRefusal(service, budgetSeconds(budget.Seconds()), true, outcome, err), nil
	}
	return result, nil
}

type backendStatusTransport struct{ base http.RoundTripper }

func (tr backendStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := tr.base.RoundTrip(r)
	if err == nil && response.StatusCode >= 300 && response.StatusCode < 400 {
		_ = response.Body.Close()
		return nil, &mcp.HTTPError{StatusCode: response.StatusCode}
	}
	return response, err
}

func budgetSeconds(seconds float64) string { return strconv.FormatFloat(seconds, 'f', -1, 64) }
func backendKind(tool mcp.ToolInfo) string {
	if tool.Effect() == mcp.Read {
		return "read"
	}
	return "write"
}
func backendOutcome(parent, ctx context.Context, err error, toolError bool) string {
	if err != nil {
		if parent.Err() != nil {
			return "cancelled"
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "timed out"
		}
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return "unreachable"
		}
		var rpc *mcp.RPCError
		if errors.As(err, &rpc) {
			return fmt.Sprintf("rpc error %d: %s", rpc.Code, backendSingleLine(rpc.Message))
		}
		var httpError *mcp.HTTPError
		if errors.As(err, &httpError) {
			return fmt.Sprintf("bad response (status %d)", httpError.StatusCode)
		}
		return "bad response"
	}
	if toolError {
		return "tool error"
	}
	return "ok"
}
func backendRefusal(service, seconds string, called bool, outcome string, err error) mcp.Result {
	switch outcome {
	case "cancelled":
		return mcp.ErrorResult("The caller cancelled the request.")
	case "unreachable":
		return mcp.ErrorResult(fmt.Sprintf("Service %s could not be reached. Retry later.", service))
	case "timed out":
		if called {
			return mcp.ErrorResult(fmt.Sprintf("Service %s did not answer within %s s; the call may still have completed.", service, seconds))
		}
		return mcp.ErrorResult(fmt.Sprintf("Service %s did not answer within %s s. Retry later.", service, seconds))
	default:
		var rpc *mcp.RPCError
		if errors.As(err, &rpc) {
			return mcp.ErrorResult(fmt.Sprintf("Service %s answered with an error: %s", service, rpc.Message))
		}
		if called {
			return mcp.ErrorResult(fmt.Sprintf("Service %s gave an answer the gateway could not read; the call may still have completed.", service))
		}
		return mcp.ErrorResult(fmt.Sprintf("Service %s gave an answer the gateway could not read. Retry later.", service))
	}
}
func backendSingleLine(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}
func describeResult(service string, tools []mcp.ToolInfo, selected *mcp.ToolInfo) mcp.Result {
	type summary struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
		Kind    string `json:"kind"`
	}
	type detail struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		Kind         string          `json:"kind"`
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	}
	var object any
	if selected == nil {
		summaries := make([]summary, 0, len(tools))
		for _, tool := range tools {
			first, _, _ := strings.Cut(tool.Description, "\n")
			summaries = append(summaries, summary{tool.Name, first, backendKind(tool)})
		}
		object = struct {
			Service string    `json:"service"`
			Tools   []summary `json:"tools"`
		}{service, summaries}
	} else {
		object = struct {
			Service string `json:"service"`
			Tool    detail `json:"tool"`
		}{service, detail{selected.Name, selected.Description, backendKind(*selected), selected.InputSchema, selected.OutputSchema}}
	}
	raw, _ := json.Marshal(object)
	text, _ := json.Marshal(string(raw))
	var result mcp.Result
	_ = result.UnmarshalJSON([]byte(`{"content":[{"type":"text","text":` + string(text) + `}],"structuredContent":` + string(raw) + `}`))
	return result
}
