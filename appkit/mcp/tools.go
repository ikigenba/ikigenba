package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Effect declares how a tool changes the service data.
type Effect int

// Tool effects determine the annotations advertised to clients.
const (
	Read        Effect = 1
	Additive    Effect = 2
	Destructive Effect = 3
)

// Enumerator supplies the allowed values of a string schema.
type Enumerator interface{ Enum() []string }

// Tool describes a typed handler and its advertised metadata.
type Tool[In, Out any] struct {
	Name, Description string
	Effect            Effect
	Handler           func(ctx context.Context, c identity.Caller, in In) (Out, error)
}

// RawTool describes a handler that returns an opaque wire result.
type RawTool[In any] struct {
	Name, Description string
	Effect            Effect
	Handler           func(ctx context.Context, c identity.Caller, in In) (Result, error)
}

type registeredTool struct {
	name string
	info json.RawMessage
	kind string
	call func(context.Context, identity.Caller, json.RawMessage) toolCall
}

type toolCall struct {
	result   Result
	outcome  string
	duration int64
}

func toolKind(effect Effect) string {
	switch effect {
	case Read:
		return "read"
	case Additive:
		return "additive"
	default:
		return "destructive"
	}
}

func invokeHandler[T any](w *telemetry.Writer, handler func() (T, error)) (value T, panicked bool, duration int64, err error) {
	start := w.Now()
	defer func() {
		end := w.Now()
		duration = int64(end.Sub(start) / time.Microsecond)
		if duration < 0 {
			duration = 0
		}
		if recover() != nil {
			panicked = true
		}
	}()
	value, err = handler()
	return
}

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func registrationPanic(name string) {
	if p := recover(); p != nil {
		reason := fmt.Sprint(p)
		if reason == "" {
			reason = "registration failed"
		}
		panic("mcp: tool " + strconv.Quote(name) + ": " + reason)
	}
}

func validateTool(name, description string, effect Effect, handlerNil bool) {
	if len(name) > 64 || !toolNamePattern.MatchString(name) {
		panic("invalid name")
	}
	if handlerNil {
		panic("handler is nil")
	}
	if effect != Read && effect != Additive && effect != Destructive {
		panic("invalid effect")
	}
	summary, _, _ := strings.Cut(description, "\n")
	if !utf8.ValidString(description) || summary == "" {
		panic("invalid description")
	}
	first, _ := utf8.DecodeRuneInString(summary)
	last, _ := utf8.DecodeLastRuneInString(summary)
	if unicode.IsSpace(first) || unicode.IsSpace(last) || utf8.RuneCountInString(summary) > 120 || !strings.HasSuffix(summary, ".") || strings.Contains(summary, ". ") {
		panic("invalid description summary")
	}
}

func toolInfo(name, description string, effect Effect, in, out *valueSchema) json.RawMessage {
	n, _ := json.Marshal(name)
	d, _ := json.Marshal(description)
	members := []jsonMember{{name: "name", value: n}, {name: "description", value: d}, {name: "inputSchema", value: in.json()}}
	if out != nil {
		members = append(members, jsonMember{name: "outputSchema", value: out.json()})
	}
	annotations := []byte(fmt.Sprintf(`{"readOnlyHint":%t,"destructiveHint":%t,"openWorldHint":false}`, effect == Read, effect == Destructive))
	members = append(members, jsonMember{name: "annotations", value: annotations})
	b, err := marshalJSONObject(members)
	if err != nil {
		panic(err)
	}
	return b
}

// AddTool registers a typed tool, panicking on invalid registration.
func AddTool[In, Out any](s *Server, t Tool[In, Out]) {
	defer registrationPanic(t.Name)
	validateTool(t.Name, t.Description, t.Effect, t.Handler == nil)
	in, err := deriveSchema(reflect.TypeFor[In](), false)
	if err != nil {
		panic(err)
	}
	out, err := deriveSchema(reflect.TypeFor[Out](), true)
	if err != nil {
		panic(err)
	}
	tool := registeredTool{name: t.Name, kind: toolKind(t.Effect), info: toolInfo(t.Name, t.Description, t.Effect, in, out)}
	tool.call = func(ctx context.Context, c identity.Caller, args json.RawMessage) toolCall {
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		v, text := in.decodeArguments(args)
		if text != "" {
			return toolCall{result: ErrorResult(text), outcome: "invalid_arguments"}
		}
		value, panicked, duration, err := invokeHandler(s.cfg.Telemetry, func() (Out, error) { return t.Handler(ctx, c, v.Interface().(In)) })
		if panicked {
			return toolCall{ErrorResult(PanicText), "panicked", duration}
		}
		if err != nil {
			return toolCall{ErrorResult(err.Error()), "error", duration}
		}
		encoded, err := out.encode(reflect.ValueOf(value))
		if err != nil {
			return toolCall{ErrorResult(PanicText), "unencodable_output", duration}
		}
		result := TextResult(string(encoded))
		result.members = append(result.members, jsonMember{name: "structuredContent", value: encoded})
		return toolCall{result, "ok", duration}
	}
	if err := s.registerTool(tool); err != nil {
		panic(err)
	}
}

// AddRawTool registers a raw tool, panicking on invalid registration.
func AddRawTool[In any](s *Server, t RawTool[In]) {
	defer registrationPanic(t.Name)
	validateTool(t.Name, t.Description, t.Effect, t.Handler == nil)
	in, err := deriveSchema(reflect.TypeFor[In](), false)
	if err != nil {
		panic(err)
	}
	tool := registeredTool{name: t.Name, kind: toolKind(t.Effect), info: toolInfo(t.Name, t.Description, t.Effect, in, nil)}
	tool.call = func(ctx context.Context, c identity.Caller, args json.RawMessage) toolCall {
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		v, text := in.decodeArguments(args)
		if text != "" {
			return toolCall{result: ErrorResult(text), outcome: "invalid_arguments"}
		}
		result, panicked, duration, err := invokeHandler(s.cfg.Telemetry, func() (Result, error) { return t.Handler(ctx, c, v.Interface().(In)) })
		if panicked {
			return toolCall{ErrorResult(PanicText), "panicked", duration}
		}
		if err != nil {
			return toolCall{ErrorResult(err.Error()), "error", duration}
		}
		outcome := "ok"
		if result.IsError() {
			outcome = "error"
		}
		return toolCall{result, outcome, duration}
	}
	if err := s.registerTool(tool); err != nil {
		panic(err)
	}
}
