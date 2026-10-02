// Package mcp serves and calls Model Context Protocol tools over HTTP.
package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// MissingCallerBody, the error codes, and ProtocolVersion name wire values consumers use.
const (
	MissingCallerBody              = "identity middleware missing\n"
	CodeParseError                 = -32700
	CodeInvalidRequest             = -32600
	CodeMethodNotFound             = -32601
	CodeInvalidParams              = -32602
	CodeInternalError              = -32603
	CodeHeaderMismatch             = -32020
	CodeUnsupportedProtocolVersion = -32022
	ProtocolVersion                = "2026-07-28"
)

const serverVersionKey = "io.modelcontextprotocol/protocolVersion"
const serverCapabilitiesKey = "io.modelcontextprotocol/clientCapabilities"
const serverInfoKey = "io.modelcontextprotocol/serverInfo"
const serverSupported = `["2026-07-28","2025-11-25","2025-06-18"]`

// ServerConfig supplies the service identity and telemetry writer.
type ServerConfig struct {
	Name, Version string
	Telemetry     *telemetry.Writer
	Instructions  func(ctx context.Context) string
}

// Server is a stateless HTTP tool server. Register tools before serving.
type Server struct {
	cfg          ServerConfig
	servicesPath string
	mu           sync.Mutex
	started      bool
	tools        []registeredTool
	index        map[string]registeredTool
}

// NewServer constructs a server and captures its services-file path.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Name == "" {
		panic("mcp: server name is empty")
	}
	if cfg.Telemetry == nil {
		panic("mcp: telemetry writer is nil")
	}
	return &Server{cfg: cfg, servicesPath: os.Getenv(services.Variable), index: make(map[string]registeredTool)}
}

func (s *Server) registerTool(t registeredTool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("server has already served a request")
	}
	if _, exists := s.index[t.name]; exists {
		return errors.New("duplicate tool name")
	}
	s.tools = append(s.tools, t)
	s.index[t.name] = t
	return nil
}

// ServeHTTP answers one protocol message, independently of earlier messages.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	caller, ok := identity.FromContext(r.Context())
	if !ok || caller.UserID == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, MissingCallerBody)
		}
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	for _, origin := range r.Header.Values("Origin") {
		scheme, host, found := strings.Cut(origin, "://")
		if !found || scheme == "" || !strings.EqualFold(host, r.Host) {
			serverError(w, http.StatusForbidden, nil, CodeInvalidRequest, "Origin not allowed", nil)
			return
		}
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		serverError(w, http.StatusUnsupportedMediaType, nil, CodeInvalidRequest, "Content-Type must be application/json", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1048577))
	if len(body) > 1048576 {
		serverError(w, http.StatusRequestEntityTooLarge, nil, CodeInvalidRequest, "Request body too large", nil)
		return
	}
	if err != nil || !utf8.Valid(body) || !json.Valid(body) {
		serverError(w, http.StatusBadRequest, nil, CodeParseError, "Parse error", nil)
		return
	}
	msg, err := serverObject(body)
	id := serverID(msg["id"])
	rpcVersion, rpcOK := serverString(msg["jsonrpc"])
	if err != nil || !rpcOK || rpcVersion != "2.0" {
		serverError(w, http.StatusBadRequest, id, CodeInvalidRequest, "Invalid Request", nil)
		return
	}
	methodRaw, hasMethod := msg["method"]
	if !hasMethod {
		_, result := msg["result"]
		_, failure := msg["error"]
		if result || failure {
			version := r.Header.Get("MCP-Protocol-Version")
			if version == "" && len(r.Header.Values("MCP-Protocol-Version")) == 0 || version == "2025-11-25" || version == "2025-06-18" {
				w.WriteHeader(http.StatusAccepted)
			} else {
				serverError(w, http.StatusBadRequest, nil, CodeInvalidRequest, "Invalid Request", nil)
			}
			return
		}
		serverError(w, http.StatusBadRequest, id, CodeInvalidRequest, "Invalid Request", nil)
		return
	}
	method, validMethod := serverString(methodRaw)
	params := map[string]json.RawMessage{}
	if raw, present := msg["params"]; present {
		params, err = serverObject(raw)
	}
	_, hasID := msg["id"]
	if !validMethod || err != nil || hasID && id == nil {
		serverError(w, http.StatusBadRequest, id, CodeInvalidRequest, "Invalid Request", nil)
		return
	}
	if !hasID {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	modern, stopped := s.serverRevision(w, r, id, method, params)
	if stopped {
		return
	}
	s.dispatch(w, r, caller, id, method, params, modern)
}

func serverObject(data []byte) (map[string]json.RawMessage, error) {
	members, err := parseJSONObject(data)
	if err != nil {
		return nil, err
	}
	obj := make(map[string]json.RawMessage, len(members))
	for _, member := range members {
		obj[member.name] = bytes.TrimSpace(member.value)
	}
	return obj, nil
}

func serverString(raw []byte) (string, bool) {
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func serverID(raw []byte) json.RawMessage {
	if _, ok := serverString(raw); ok {
		return raw
	}
	if len(raw) == 0 {
		return nil
	}
	start := 0
	if raw[0] == '-' {
		start = 1
	}
	if start == len(raw) {
		return nil
	}
	for _, c := range raw[start:] {
		if c < '0' || c > '9' {
			return nil
		}
	}
	return raw
}

func (s *Server) serverRevision(w http.ResponseWriter, r *http.Request, id json.RawMessage, method string, params map[string]json.RawMessage) (bool, bool) {
	meta, _ := serverObject(params["_meta"])
	versionRaw, modern := meta[serverVersionKey]
	if !modern {
		if method == "initialize" {
			return false, false
		}
		version := r.Header.Get("MCP-Protocol-Version")
		if version == "2025-11-25" || version == "2025-06-18" || len(r.Header.Values("MCP-Protocol-Version")) == 0 {
			return false, false
		}
		if version == ProtocolVersion {
			serverError(w, 400, id, CodeInvalidParams, "Missing protocol metadata", nil)
		} else {
			serverVersionError(w, id, version)
		}
		return false, true
	}
	version, ok := serverString(versionRaw)
	if !ok {
		serverError(w, 400, id, CodeInvalidParams, "Invalid protocol version", nil)
		return true, true
	}
	if version != ProtocolVersion {
		serverVersionError(w, id, version)
		return true, true
	}
	if _, err := serverObject(meta[serverCapabilitiesKey]); err != nil {
		serverError(w, 400, id, CodeInvalidParams, "Invalid client capabilities", nil)
		return true, true
	}
	nameHeader := r.Header.Get("Mcp-Name")
	for _, header := range []string{"MCP-Protocol-Version", "Mcp-Method", "Mcp-Name"} {
		for _, value := range r.Header.Values(header) {
			for _, c := range []byte(value) {
				if c < 0x20 || c > 0x7e {
					serverError(w, 400, id, CodeHeaderMismatch, "Header mismatch", nil)
					return true, true
				}
			}
		}
	}
	if method == "tools/call" && strings.HasPrefix(nameHeader, "=?base64?") && strings.HasSuffix(nameHeader, "?=") {
		decoded, err := base64.StdEncoding.DecodeString(nameHeader[9 : len(nameHeader)-2])
		if err != nil || !utf8.Valid(decoded) {
			serverError(w, 400, id, CodeHeaderMismatch, "Header mismatch", nil)
			return true, true
		}
		nameHeader = string(decoded)
	}
	if r.Header.Get("MCP-Protocol-Version") != version || r.Header.Get("Mcp-Method") != method {
		serverError(w, 400, id, CodeHeaderMismatch, "Header mismatch", nil)
		return true, true
	}
	if method != "server/discover" && method != "tools/list" && method != "tools/call" {
		serverError(w, 404, id, CodeMethodNotFound, "Method not found", nil)
		return true, true
	}
	if name, ok := serverString(params["name"]); method == "tools/call" && ok && (len(r.Header.Values("Mcp-Name")) == 0 || nameHeader != name) {
		serverError(w, 400, id, CodeHeaderMismatch, "Header mismatch", nil)
		return true, true
	}
	return true, false
}

func serverVersionError(w http.ResponseWriter, id json.RawMessage, requested string) {
	data := []jsonMember{{"supported", json.RawMessage(serverSupported)}, {"requested", serverJSON(requested)}}
	encoded, _ := marshalJSONObject(data)
	serverError(w, 400, id, CodeUnsupportedProtocolVersion, "Unsupported protocol version", encoded)
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, caller identity.Caller, id json.RawMessage, method string, params map[string]json.RawMessage, modern bool) {
	var result []byte
	code, message := 0, ""
	switch method {
	case "initialize":
		version, _ := serverString(params["protocolVersion"])
		if version != "2025-11-25" && version != "2025-06-18" {
			version = "2025-11-25"
		}
		members := []jsonMember{{"protocolVersion", serverJSON(version)}, {"capabilities", json.RawMessage(`{"tools":{}}`)}, {"serverInfo", s.serverInfo()}}
		result = s.withInstructions(r.Context(), members)
	case "server/discover":
		if !modern {
			code, message = CodeMethodNotFound, "Method not found"
			break
		}
		members := []jsonMember{{"supportedVersions", json.RawMessage(serverSupported)}, {"capabilities", json.RawMessage(`{"tools":{}}`)}, {"ttlMs", json.RawMessage(`0`)}, {"cacheScope", json.RawMessage(`"private"`)}}
		result = s.withInstructions(r.Context(), members)
	case "ping":
		result = []byte(`{}`)
	case "tools/list":
		if _, exists := params["cursor"]; exists {
			code, message = CodeInvalidParams, "Invalid cursor"
			break
		}
		s.mu.Lock()
		tools := make([]json.RawMessage, len(s.tools))
		for i, tool := range s.tools {
			tools[i] = tool.info
		}
		s.mu.Unlock()
		members := []jsonMember{{"tools", serverJSON(tools)}}
		if modern {
			members = append(members, jsonMember{"ttlMs", json.RawMessage(`0`)}, jsonMember{"cacheScope", json.RawMessage(`"private"`)})
		}
		result, _ = marshalJSONObject(members)
	case "tools/call":
		name, ok := serverString(params["name"])
		if !ok {
			code, message = CodeInvalidParams, "Invalid tool name"
			break
		}
		s.mu.Lock()
		tool, exists := s.index[name]
		s.mu.Unlock()
		if !exists {
			code, message = CodeInvalidParams, "Unknown tool: "+name
			break
		}
		callResult := tool.call(r.Context(), caller, params["arguments"])
		result, _ = callResult.result.MarshalJSON()
		s.cfg.Telemetry.Emit(r.Context(), "tool.called", telemetry.Attrs{
			"tool": name, "kind": tool.kind, "outcome": callResult.outcome, "duration_us": callResult.duration,
		})
	default:
		code, message = CodeMethodNotFound, "Method not found"
	}
	if code != 0 {
		status := 200
		if modern {
			status = 400
			if code == CodeMethodNotFound {
				status = 404
			}
			if code == CodeInternalError {
				status = 500
			}
		}
		serverError(w, status, id, code, message, nil)
		return
	}
	result = s.envelope(result, modern)
	serverResponse(w, 200, []jsonMember{{"jsonrpc", json.RawMessage(`"2.0"`)}, {"id", id}, {"result", result}})
}

func (s *Server) withInstructions(ctx context.Context, members []jsonMember) []byte {
	text := ""
	if s.cfg.Instructions != nil {
		text = s.cfg.Instructions(ctx)
	} else if list, err := services.Read(s.servicesPath); err == nil {
		if entry, found := list.Find(s.cfg.Name); found {
			text = entry.Description
		}
	}
	if text != "" {
		members = append(members, jsonMember{"instructions", serverJSON(text)})
	}
	data, _ := marshalJSONObject(members)
	return data
}

func (s *Server) serverInfo() json.RawMessage {
	data, _ := marshalJSONObject([]jsonMember{{"name", serverJSON(s.cfg.Name)}, {"version", serverJSON(s.cfg.Version)}})
	return data
}

func (s *Server) envelope(result []byte, modern bool) []byte {
	members, _ := parseJSONObject(result)
	out := make([]jsonMember, 0, len(members)+2)
	metaSeen := false
	for _, member := range members {
		if member.name == "resultType" || !modern && (member.name == "ttlMs" || member.name == "cacheScope") {
			continue
		}
		if member.name != "_meta" {
			out = append(out, member)
			continue
		}
		metaSeen = true
		meta, err := parseJSONObject(member.value)
		if err != nil && !modern {
			out = append(out, member)
			continue
		}
		filtered := make([]jsonMember, 0, len(meta)+1)
		for _, item := range meta {
			if item.name != serverInfoKey {
				filtered = append(filtered, item)
			}
		}
		if modern {
			filtered = append(filtered, jsonMember{serverInfoKey, s.serverInfo()})
		}
		if len(filtered) > 0 {
			member.value, _ = marshalJSONObject(filtered)
			out = append(out, member)
		}
	}
	if modern {
		out = append(out, jsonMember{"resultType", json.RawMessage(`"complete"`)})
		if !metaSeen {
			meta, _ := marshalJSONObject([]jsonMember{{serverInfoKey, s.serverInfo()}})
			out = append(out, jsonMember{"_meta", meta})
		}
	}
	data, _ := marshalJSONObject(out)
	return data
}

func serverJSON(value any) json.RawMessage { data, _ := json.Marshal(value); return data }

func serverError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string, data json.RawMessage) {
	failure := []jsonMember{{"code", serverJSON(code)}, {"message", serverJSON(message)}}
	if data != nil {
		failure = append(failure, jsonMember{"data", data})
	}
	errorJSON, _ := marshalJSONObject(failure)
	members := []jsonMember{{"jsonrpc", json.RawMessage(`"2.0"`)}}
	if id != nil {
		members = append(members, jsonMember{"id", id})
	}
	members = append(members, jsonMember{"error", errorJSON})
	serverResponse(w, status, members)
}

func serverResponse(w http.ResponseWriter, status int, members []jsonMember) {
	data, _ := marshalJSONObject(members)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
