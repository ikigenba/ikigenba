// Package gateway exposes suite peers through a small, fixed set of agent tools.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/agentkit"

	"prompts/internal/mcpclient"
	"prompts/internal/suite"
)

const (
	servicesToolName = "suite_services"
	toolsToolName    = "suite_tools"
	callToolName     = "suite_call"
)

// CatalogFunc obtains the run's service catalog. The composition root closes
// this function over suite.Catalog and the run's peer clients.
type CatalogFunc func(context.Context) []suite.CatalogEntry

// Client is the portion of the peer MCP client used by the gateway.
type Client interface {
	Instructions(context.Context) (string, error)
	ListTools(context.Context) ([]mcpclient.Tool, error)
	CallTool(context.Context, string, json.RawMessage) (mcpclient.Result, error)
}

// ClientFunc yields the client for a peer.
type ClientFunc func(suite.Peer) Client

type suiteServicesInput struct{}

type serviceEntry struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type suiteToolsInput struct {
	Service string `json:"service" jsonschema:"description=A service name from suite_services"`
}

type suiteCallInput struct {
	Service string `json:"service" jsonschema:"description=A service name from suite_services"`
	Tool    string `json:"tool" jsonschema:"description=Bare tool name as listed by suite_tools (for example save)"`
	Args    string `json:"args" jsonschema:"description=The tool's arguments as one JSON object literal (for example {\"id\":\"X\"})"`
}

// Tools returns the three gateway tools over the run's peer set.
func Tools(peers []suite.Peer, catalog CatalogFunc, clients ClientFunc) []agentkit.Tool {
	peerByName := make(map[string]suite.Peer, len(peers))
	validNames := make([]string, 0, len(peers))
	for _, peer := range peers {
		peerByName[peer.Name] = peer
		validNames = append(validNames, peer.Name)
	}

	return []agentkit.Tool{
		agentkit.NewTool(servicesToolName, "Discover the account's services here, then load a service's tools with suite_tools before calling it.", func(ctx context.Context, _ suiteServicesInput) (string, error) {
			catalogEntries := catalog(ctx)
			entries := make([]serviceEntry, len(catalogEntries))
			for i, entry := range catalogEntries {
				entries[i] = serviceEntry{Name: entry.Name, Summary: entry.Summary}
			}
			encoded, err := json.Marshal(entries)
			if err != nil {
				return "could not encode the suite service catalog", err
			}
			return string(encoded), nil
		}),
		agentkit.NewTool(toolsToolName, "Load a suite service's instructions and current tool schemas before calling its tools.", func(ctx context.Context, in suiteToolsInput) (string, error) {
			peer, ok := peerByName[in.Service]
			if !ok {
				return unknownServiceError(in.Service, toolsToolName, validNames)
			}
			client := clients(peer)
			instructions, err := client.Instructions(ctx)
			if err != nil {
				return framedError(in.Service, toolsToolName, err)
			}
			listed, err := client.ListTools(ctx)
			if err != nil {
				return framedError(in.Service, toolsToolName, err)
			}
			encoded, err := encodeToolList(instructions, listed)
			if err != nil {
				return framedError(in.Service, toolsToolName, err)
			}
			return encoded, nil
		}),
		agentkit.NewTool(callToolName, "Invoke one suite tool after inspecting its schema with suite_tools; args must be one JSON object literal.", func(ctx context.Context, in suiteCallInput) (string, error) {
			args, err := parseArgs(in.Args)
			if err != nil {
				return framedError(in.Service, in.Tool, err)
			}
			peer, ok := peerByName[in.Service]
			if !ok {
				return unknownServiceError(in.Service, in.Tool, validNames)
			}
			result, err := clients(peer).CallTool(ctx, in.Tool, args)
			if err != nil {
				return framedError(in.Service, in.Tool, err)
			}
			if result.IsError {
				return framedError(in.Service, in.Tool, errors.New(result.Text))
			}
			return result.Text, nil
		}),
	}
}

func parseArgs(input string) (json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(input))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("args is not valid JSON: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, fmt.Errorf("args is not one JSON value: %w", err)
	}
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("args must be a JSON object")
	}
	return json.RawMessage(trimmed), nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("found a second JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func encodeToolList(instructions string, tools []mcpclient.Tool) (string, error) {
	encodedInstructions, err := json.Marshal(instructions)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	out.WriteString(`{"instructions":`)
	out.Write(encodedInstructions)
	out.WriteString(`,"tools":[`)
	for i, tool := range tools {
		if i > 0 {
			out.WriteByte(',')
		}
		name, err := json.Marshal(tool.Name)
		if err != nil {
			return "", err
		}
		description, err := json.Marshal(tool.Description)
		if err != nil {
			return "", err
		}
		if !json.Valid(tool.InputSchema) {
			return "", fmt.Errorf("tool %q published an invalid input schema", tool.Name)
		}
		out.WriteString(`{"name":`)
		out.Write(name)
		out.WriteString(`,"description":`)
		out.Write(description)
		out.WriteString(`,"inputSchema":`)
		out.Write(tool.InputSchema)
		out.WriteByte('}')
	}
	out.WriteString(`]}`)
	return out.String(), nil
}

func unknownServiceError(service, tool string, valid []string) (string, error) {
	detail := "valid services: " + strings.Join(valid, ", ")
	if len(valid) == 0 {
		detail = "there are no valid services"
	}
	return framedError(service, tool, errors.New("unknown service; "+detail))
}

func framedError(service, tool string, cause error) (string, error) {
	message := fmt.Sprintf("service %q tool %q failed: %v; inspect suite_tools(%q) for the schema", service, tool, cause, service)
	return message, errors.New(message)
}
