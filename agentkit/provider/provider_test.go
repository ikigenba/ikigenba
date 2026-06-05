package provider_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"agentkit/provider"
)

// TestR_G0EH_D2SW_InterfaceShape pins the provider abstraction's
// surface: the operations the agent loop needs, the request fields
// the loop must be able to supply, the normalized event variants
// backends must be able to emit, and the typed-error contract.
//
// R-G0EH-D2SW: the provider interface is (issue streaming
// generation request, stream normalized events, report typed
// errors).
func TestR_G0EH_D2SW_InterfaceShape(t *testing.T) {
	t.Run("Client_has_Stream_method", func(t *testing.T) {
		ct := reflect.TypeOf((*provider.Client)(nil)).Elem()
		m, ok := ct.MethodByName("Stream")
		if !ok {
			t.Fatalf("provider.Client must declare Stream(ctx, Request) method")
		}
		// Stream(ctx context.Context, req Request) (<-chan Event, error)
		mt := m.Type
		if mt.NumIn() != 2 {
			t.Fatalf("Stream must take (ctx, Request); got %d params", mt.NumIn())
		}
		if mt.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() {
			t.Errorf("Stream first param must be context.Context, got %v", mt.In(0))
		}
		if mt.In(1) != reflect.TypeOf(provider.Request{}) {
			t.Errorf("Stream second param must be provider.Request, got %v", mt.In(1))
		}
		if mt.NumOut() != 2 {
			t.Fatalf("Stream must return (<-chan Event, error); got %d returns", mt.NumOut())
		}
		ch := mt.Out(0)
		if ch.Kind() != reflect.Chan || ch.ChanDir() != reflect.RecvDir {
			t.Errorf("Stream first return must be <-chan Event, got %v", ch)
		}
		if ch.Elem() != reflect.TypeOf((*provider.Event)(nil)).Elem() {
			t.Errorf("Stream channel element must be provider.Event, got %v", ch.Elem())
		}
		if mt.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
			t.Errorf("Stream second return must be error, got %v", mt.Out(1))
		}
	})

	t.Run("Request_has_required_fields", func(t *testing.T) {
		want := map[string]reflect.Type{
			"Model":          reflect.TypeOf(""),
			"Effort":         reflect.TypeOf(""),
			"Messages":       reflect.TypeOf([]provider.Message{}),
			"Tools":          reflect.TypeOf([]provider.Tool{}),
			"ResponseSchema": reflect.TypeOf(json.RawMessage{}),
		}
		rt := reflect.TypeOf(provider.Request{})
		for name, typ := range want {
			f, ok := rt.FieldByName(name)
			if !ok {
				t.Errorf("provider.Request missing field %q", name)
				continue
			}
			if f.Type != typ {
				t.Errorf("provider.Request.%s type = %v, want %v", name, f.Type, typ)
			}
		}
	})

	t.Run("Event_variants_satisfy_interface", func(t *testing.T) {
		// All five normalized event kinds the agent loop consumes:
		// assistant text deltas, tool_use blocks, thinking blocks,
		// usage totals, completion signal.
		var (
			_ provider.Event = provider.EventTextDelta{}
			_ provider.Event = provider.EventToolUse{}
			_ provider.Event = provider.EventThinking{}
			_ provider.Event = provider.EventUsage{}
			_ provider.Event = provider.EventDone{}
		)
	})

	t.Run("Block_variants_satisfy_interface", func(t *testing.T) {
		var (
			_ provider.Block = provider.TextBlock{}
			_ provider.Block = provider.ToolUseBlock{}
			_ provider.Block = provider.ToolResultBlock{}
			_ provider.Block = provider.ThinkingBlock{}
		)
	})

	t.Run("Error_is_typed_not_raw_HTTP", func(t *testing.T) {
		// *Error must satisfy the error interface and carry a
		// classified Kind so callers can route on it without
		// parsing strings or HTTP responses.
		var err error = &provider.Error{Kind: provider.ErrRateLimit, Msg: "x"}
		if err.Error() != "x" {
			t.Errorf("Error.Error() = %q, want %q", err.Error(), "x")
		}
		// A representative spread of kinds must be defined; the
		// agent loop maps these to result events per R-E2W7-K5JB.
		kinds := []provider.ErrorKind{
			provider.ErrUnknown,
			provider.ErrAuth,
			provider.ErrInvalidRequest,
			provider.ErrRateLimit,
			provider.ErrTimeout,
			provider.ErrServer,
		}
		seen := map[provider.ErrorKind]bool{}
		for _, k := range kinds {
			if seen[k] {
				t.Errorf("ErrorKind %v duplicated", k)
			}
			seen[k] = true
		}
	})
}
