# D10-mcp-client

The `mcp` client is what the gateway uses to reach a backend service, and what a service's own tests use to drive its server end to end. It speaks only the stateless 2026-07-28 revision, `ProtocolVersion` (D07): every request stands alone and carries its protocol version and client capabilities, so the client holds no connection state and needs no handshake (S26/changelog; S26/basic/versioning: "There is no negotiation handshake"). It does two things, list tools and call one, always on behalf of a caller: each request carries the caller's identity headers through `identity.Forward` (D06), so the backend's `identity.Require` sees the same person, email, and request id nginx established for the gateway.

A `Client` is built from a `ClientConfig`. The endpoint is the full URL of the backend's MCP endpoint; the HTTP client decides how to get there, which is how the gateway reaches a service over its unix socket (`http://<name>/mcp` with a socket dialer) and how a test reaches an `httptest` server. `Name` and `Version` are the client's own `Implementation` (TS26), sent as `clientInfo`, which "Clients SHOULD include ... on every request" (S26/basic#meta).

## The request

Every request is a POST of a single JSON-RPC request object (S26/basic/transports/streamable-http: "The body of the HTTP POST MUST be a single JSON-RPC request or notification"). It carries `Accept` listing both `application/json` and `text/event-stream` (same, #sending-messages), `MCP-Protocol-Version` (#protocol-version-header), and the standard headers `Mcp-Method` and, for `tools/call`, `Mcp-Name` (#standard-request-headers). The params carry `_meta` with `io.modelcontextprotocol/protocolVersion` and `io.modelcontextprotocol/clientCapabilities` (both required; S26/basic#meta) and `io.modelcontextprotocol/clientInfo`. The client asks for no optional capability, so capabilities are `{}` ("an empty object means the client supports no optional capabilities", TS26 `RequestMetaObject`).

A tool name the header cannot carry verbatim is sent in the Base64 sentinel form `=?base64?...?=` that servers must decode (#value-encoding). The wire reference does not say which Base64 alphabet; the client uses the standard alphabet with padding (RFC 4648 section 4), the plain meaning of "Base64", and also uses the sentinel for a name that would otherwise look like one. Tools registered through D08 never need it.

## The response

A server answers a request either with one `application/json` object or with a `text/event-stream` stream, and "The client MUST support both" (S26/basic/transports/streamable-http). In a stream the client reads events, as the HTML Living Standard's server-sent events section interprets an event stream, until the JSON-RPC response to its own request arrives; related notifications are ignored, and so is an event with no data (2025-11-25 servers prime a stream that way). A server's JSON-RPC error is reported as an `*RPCError` whatever HTTP status carried it, since the 2026 revision pairs its errors with 400, 403, and 404 statuses. Anything else that is not a successful JSON-RPC response — a proxy's plain-text 401, a 502, a body that is not JSON-RPC — is an `*HTTPError` holding the status and the start of the body. A result whose `resultType` is absent or `"complete"` is accepted ("clients MUST treat an absent `resultType` as `"complete"`"); any other value, `"input_required"` included, is an error, since "A `resultType` of any value unrecognized by the client MUST be considered invalid" and this client implements no multi-round-trip input (S26/basic).

Decisions recorded: the `HTTPError` body is capped at 4096 bytes; a response that is well-formed JSON-RPC but breaks the protocol (another request's id, a `tools` member that is not an array, a repeated cursor) is a plain error, neither `*RPCError` nor `*HTTPError`, with text beginning `mcp: `.

## Pagination

`tools/list` may be paginated. The client follows `nextCursor` until a page has none and returns every page's tools in order. An empty string is a cursor like any other ("an empty string is a valid cursor and thus MUST NOT be treated as the end of results", S26/server/utilities/pagination). A server that hands back a cursor already used in the same listing would loop forever, so that is an error.

## Tools as the gateway sees them

`ToolInfo` is the part of a Tool object the gateway uses. Its `Effect` reads the annotations back into the three effects of D08, applying the hint defaults every revision states (`readOnlyHint` default false, `destructiveHint` default true; TS0618, TS1125, TS26 `ToolAnnotations`): a tool that does not say it is harmless is treated as destructive. Annotations are untrusted unless the server is trusted (server/tools, all revisions); the gateway trusts the suite's own services.

`CallTool` returns the tool's `Result` (D08) with every member preserved except `resultType`, and an `isError` result is a result, not a Go error: the tool answered.

## Errors from the transport

A failure to reach the server at all is returned so that `errors.Is` and `errors.As` see the HTTP client's own error: a deadline or cancellation of the caller's context, or a dial failure on a missing socket. The gateway uses this to tell "unreachable" from "timed out".

## REQUIREMENTS

- R-N93H-E7LX: Package `mcp` MUST export `type ClientConfig struct { Endpoint string; HTTPClient *http.Client; Name, Version string }`, with exactly these fields in this order.
- R-NABD-RZCM: Package `mcp` MUST export type `Client` and `func NewClient(cfg ClientConfig) *Client`.
- R-NBJA-5R3B: Package `mcp` MUST export the method `func (c *Client) ListTools(ctx context.Context, caller identity.Caller) ([]ToolInfo, error)`.
- R-NCR6-JIU0: Package `mcp` MUST export the method `func (c *Client) CallTool(ctx context.Context, caller identity.Caller, name string, args json.RawMessage) (Result, error)`.
- R-NDZ2-XAKP: Package `mcp` MUST export `type ToolInfo struct { Name, Description string; InputSchema, OutputSchema json.RawMessage; Annotations Annotations }`, with exactly these fields in this order.
- R-NF6Z-B2BE: Package `mcp` MUST export `type Annotations struct { ReadOnlyHint, DestructiveHint, IdempotentHint, OpenWorldHint *bool }`, with exactly these fields in this order.
- R-NGEV-OU23: Package `mcp` MUST export the method `func (t ToolInfo) Effect() Effect`.
- R-NHMS-2LSS: Package `mcp` MUST export `type RPCError struct { Code int; Message string; Data json.RawMessage }`, with exactly these fields in this order, and the method `func (e *RPCError) Error() string`.
- R-NIUO-GDJH: Package `mcp` MUST export `type HTTPError struct { StatusCode int; Body string }`, with exactly these fields in this order, and the method `func (e *HTTPError) Error() string`.
- R-NLAH-7X0V: `NewClient` MUST NOT panic, perform I/O, or fail for any `ClientConfig`; an unusable `Endpoint` MUST instead make every `ListTools` and `CallTool` call return a non-nil error.
- R-NMID-LORK: Every HTTP request a `Client` sends MUST be sent through `ClientConfig.HTTPClient`, or through `http.DefaultClient` when it is nil.
- R-NNQ9-ZGI9: Every HTTP request a `Client` sends MUST use the method `POST` and the URL `ClientConfig.Endpoint` unaltered, and MUST carry the context passed to the `ListTools` or `CallTool` call that sent it.
- R-NOY6-D88Y: Every HTTP request a `Client` sends MUST carry the headers `Content-Type: application/json`, `Accept: application/json, text/event-stream`, `MCP-Protocol-Version` equal to `ProtocolVersion`, and `Mcp-Method` equal to the JSON-RPC `method` of its body.
- R-NQ62-QZZN: Every HTTP request a `Client` sends MUST carry the `X-User-Id`, `X-User-Email`, and `X-Request-Id` headers exactly as `identity.Forward` sets them from the `caller` passed to the call.
- R-NRDZ-4RQC: A request `CallTool` sends MUST carry the header `Mcp-Name` equal to `name` when `name` consists only of bytes 0x21 through 0x7E and does not begin with `=?base64?`, and otherwise equal to `=?base64?` followed by the standard padded Base64 encoding (RFC 4648 section 4) of `name` and `?=`; a request `ListTools` sends MUST NOT carry `Mcp-Name`.
- R-NSLV-IJH1: The body of every request a `Client` sends MUST be one JSON object with exactly the members `jsonrpc` equal to `"2.0"`, `id` a JSON integer, `method`, and `params`, where no two requests sent by the same `Client` have the same `id`.
- R-NTTR-WB7Q: The `params` of every request a `Client` sends MUST hold a member `_meta`, an object with `io.modelcontextprotocol/protocolVersion` equal to `ProtocolVersion`, `io.modelcontextprotocol/clientCapabilities` equal to `{}`, and, only when `ClientConfig.Name` is non-empty, `io.modelcontextprotocol/clientInfo` equal to `{"name":<Name>,"version":<Version>}`.
- R-NV1O-A2YF: `ListTools` MUST send requests with `method` `tools/list`, the first with `params` holding only `_meta`, and each later one with `params` holding only `_meta` and `cursor` equal to the previous page's `nextCursor`.
- R-NW9K-NUP4: `ListTools` MUST request a further page exactly when the previous page's result holds a `nextCursor` member, including when its value is the empty string, and MUST return the tools of every page, in page order and in each page's order.
- R-NXHH-1MFT: `ListTools` MUST return a non-nil error and send no further request when a page's `nextCursor` is not a string or equals a cursor already sent in the same `ListTools` call.
- R-NYPD-FE6I: `CallTool` MUST send one request with `method` `tools/call` and `params` holding exactly `name` equal to `name`, `arguments`, and `_meta`, where `arguments` is `{}` when `args` is nil and otherwise a JSON value equal to `args`.
- R-NZX9-T5X7: `CallTool` MUST return a non-nil error without sending any request when `args` is non-nil and is not exactly one valid JSON value.
- R-O156-6XNW: A `Client` MUST accept a 200 response whose `Content-Type` media type is `application/json` and whose body is one JSON-RPC response object.
- R-O2D2-KPEL: A `Client` MUST accept a 200 response whose `Content-Type` media type is `text/event-stream`, reading its events as the HTML Living Standard interprets an event stream, ignoring events whose data is empty and events whose data is a JSON-RPC notification or request, using the first event whose data is the JSON-RPC response with the request's `id`, and reading no further.
- R-O3KY-YH5A: A `Client` MUST return a non-nil error that is neither an `*RPCError` nor an `*HTTPError` when an event stream ends without the response with the request's `id`, or holds an event whose non-empty data is not a JSON-RPC message or is a response with another `id`.
- R-O4SV-C8VZ: When a response body (or the stream event a `Client` uses) is a JSON object with `jsonrpc` equal to `"2.0"` and a member `error` that is an object with an integer `code` and a string `message`, the call MUST return an `*RPCError` whose `Code` and `Message` are those values and whose `Data` is the bytes of the `data` member, or nil when it is absent, whatever the HTTP status.
- R-O78O-3SDD: A call MUST return an `*HTTPError` whose `StatusCode` is the response status and whose `Body` is the first 4096 bytes of the response body (all of it when shorter) when the response is not a JSON-RPC error and its status is not 200, its `Content-Type` media type is neither `application/json` nor `text/event-stream`, or its `application/json` body is not a JSON object with `jsonrpc` equal to `"2.0"` and a `result` or `error` member.
- R-O8GK-HK42: A call MUST return a non-nil error that is neither an `*RPCError` nor an `*HTTPError` when a 200 response is a JSON-RPC result whose `id` differs from the request's `id`, or whose result is not an object.
- R-O9OG-VBUR: A `Client` MUST accept a result whose `resultType` member is absent or equal to `"complete"`, and MUST return a non-nil error that is neither an `*RPCError` nor an `*HTTPError` for a result whose `resultType` has any other value.
- R-OAWD-93LG: `ListTools` MUST return a non-nil error that is neither an `*RPCError` nor an `*HTTPError` when a page's result has no `tools` array, or an element of it is not an object with a string `name` and an object `inputSchema`, or has a `description` that is not a string, an `outputSchema` that is not an object, `annotations` that is not an object, or an annotation hint that is not a boolean.
- R-OC49-MVC5: `ListTools` MUST return one `ToolInfo` per Tool object, with `Name` its `name`; `Description` its `description`, or empty when absent; `InputSchema` a JSON value equal to its `inputSchema`; `OutputSchema` a JSON value equal to its `outputSchema`, or nil when absent; and each `Annotations` field pointing to the value of the hint of the same name (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`), or nil when that hint is absent.
- R-ODC6-0N2U: `ToolInfo.Effect` MUST return `Read` when `ReadOnlyHint` points to true; otherwise `Additive` when `DestructiveHint` points to false; otherwise `Destructive`.
- R-OEK2-EETJ: `CallTool` MUST return, with a nil error, the `Result` that `Result.UnmarshalJSON` produces from the response's `result` object, whether or not that result's `isError` is true.
- R-OFRY-S6K8: When `ListTools` or `CallTool` returns a non-nil error, `ListTools` MUST return a nil slice and `CallTool` MUST return the zero `Result`.
- R-OGZV-5YAX: When the HTTP client's request fails, the error a call returns MUST wrap that failure so that `errors.Is` and `errors.As` reach it, including `context.Canceled` or `context.DeadlineExceeded` when the call's context ends first and a `*net.OpError` whose `Op` is `"dial"` when the endpoint cannot be connected to.
- R-OI7R-JQ1M: `RPCError.Error` MUST return `mcp: rpc error <code>: <message>`, with `<code>` the decimal `Code` and `<message>` the `Message`.
- R-OJFN-XHSB: `HTTPError.Error` MUST return `mcp: unexpected HTTP response (status <code>)`, with `<code>` the decimal `StatusCode`.
- R-OKNK-B9J0: A `Client` MUST be safe for concurrent use: `ListTools` and `CallTool` calls on the same `Client` from multiple goroutines MUST each behave as if made alone.
