package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// R-CZZ8-ZOQD
// R-D175-DGH2
func TestAccessConstructorsAndConflictGeometry(t *testing.T) {
	if reflect.TypeFor[Access]().NumField() != 2 || reflect.TypeFor[Access]().Field(0).PkgPath == "" || reflect.TypeFor[Access]().Field(1).PkgPath == "" {
		t.Fatal("Access must be an opaque struct with no exported fields")
	}
	none := BlocksNone()
	empty := BlocksPaths()
	all := BlocksAll()
	cases := []struct {
		left, right Access
		want        bool
	}{
		{none, none, false}, {none, empty, false}, {none, all, true}, {empty, all, true},
		{BlocksPaths("tree/child/.."), BlocksPaths("tree/file"), true},
		{BlocksPaths("tree/file"), BlocksPaths("tree/file"), true},
		{BlocksPaths("tree/a"), BlocksPaths("tree/ab"), false},
		{BlocksPaths("left"), BlocksPaths("right"), false},
	}
	for index, test := range cases {
		if got := accessesConflict(test.left, test.right); got != test.want {
			t.Fatalf("case %d conflict = %t, want %t", index, got, test.want)
		}
		if got := accessesConflict(test.right, test.left); got != test.want {
			t.Fatalf("case %d reverse conflict = %t, want %t", index, got, test.want)
		}
	}
}

type parallelInput struct {
	Name string `json:"name" jsonschema:"required"`
}

// R-D2F1-R87R
// R-DFTX-YPDE
func TestToolAccessUsesValidatedConstructorArgumentsExactlyOnce(t *testing.T) {
	var typedInputs []parallelInput
	var typedAccessCalls atomic.Int32
	var typedAccessed atomic.Bool
	typed := MustTool("typed", "", func(context.Context, parallelInput) (string, error) {
		if !typedAccessed.Load() {
			t.Fatal("typed Call started before Access")
		}
		return "ok", nil
	}, func(input parallelInput) Access {
		typedAccessCalls.Add(1)
		typedAccessed.Store(true)
		typedInputs = append(typedInputs, input)
		return BlocksPaths(input.Name)
	})
	var rawInputs []string
	var rawAccessCalls atomic.Int32
	var rawAccessed atomic.Bool
	rawWant := BlocksPaths("raw/path")
	raw, err := NewToolFromSchema("raw", "", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`), func(context.Context, json.RawMessage) (string, error) {
		if !rawAccessed.Load() {
			t.Fatal("raw Call started before Access")
		}
		return "ok", nil
	}, func(input json.RawMessage) Access {
		rawAccessCalls.Add(1)
		rawAccessed.Store(true)
		rawInputs = append(rawInputs, string(input))
		return rawWant
	})
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := newOrchestrator([]Tool{typed, raw}, nil, nil)
	valid := ToolUse{ID: "1", Name: "typed", Input: json.RawMessage(`{"name":"clean"}`)}
	if result := orchestrator.prepareDispatch(valid, false).run(context.Background()); result.IsError {
		t.Fatalf("valid dispatch failed: %#v", result)
	}
	orchestrator.prepareDispatch(ToolUse{ID: "2", Name: "typed", Input: json.RawMessage(`{"name":1}`)}, false).run(context.Background())
	orchestrator.prepareDispatch(ToolUse{ID: "3", Name: "missing", Input: json.RawMessage(`{}`)}, false).run(context.Background())
	if typedAccessCalls.Load() != 1 || !reflect.DeepEqual(typedInputs, []parallelInput{{Name: "clean"}}) {
		t.Fatalf("typed Access calls=%d inputs=%#v", typedAccessCalls.Load(), typedInputs)
	}
	rawDispatch := orchestrator.prepareDispatch(ToolUse{ID: "4", Name: "raw", Input: json.RawMessage(`{"name":"raw"}`)}, false)
	if !reflect.DeepEqual(rawDispatch.access, rawWant) {
		t.Fatalf("NewToolFromSchema access = %#v, want callback value %#v", rawDispatch.access, rawWant)
	}
	rawDispatch.run(context.Background())
	if rawAccessCalls.Load() != 1 || !reflect.DeepEqual(rawInputs, []string{`{"name":"raw"}`}) {
		t.Fatalf("raw Access calls=%d inputs=%q", rawAccessCalls.Load(), rawInputs)
	}
	if got := typed.Access(json.RawMessage(`{"name":"folder/file"}`)); !accessesConflict(got, BlocksPaths("folder")) {
		t.Fatal("MustTool did not retain its typed access function")
	}
	created, err := NewTool("new", "", func(context.Context, parallelInput) (string, error) { return "", nil }, func(input parallelInput) Access {
		return BlocksPaths(input.Name)
	})
	if err != nil || !accessesConflict(created.Access(json.RawMessage(`{"name":"folder/file"}`)), BlocksPaths("folder")) {
		t.Fatalf("NewTool access: tool=%v err=%v", created, err)
	}
}

// R-D3MY-4ZYG
// R-D4UU-IRP5
func TestParallelDispatchCompletionOrderAndStableTranscript(t *testing.T) {
	calls := []ToolUse{
		{ID: "slow", Name: "slow", Input: json.RawMessage(`{"name":"slow"}`)},
		{ID: "fast", Name: "fast", Input: json.RawMessage(`{"name":"fast"}`)},
	}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{calls[0], calls[1]}}}},
		{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}}},
	}}
	started := make(chan string, 2)
	var startMu sync.Mutex
	var startOrder []string
	releaseSlow := make(chan struct{})
	fastFinished := make(chan struct{})
	tool := func(name string) Tool {
		return MustTool(name, "", func(context.Context, parallelInput) (string, error) {
			startMu.Lock()
			startOrder = append(startOrder, name)
			startMu.Unlock()
			started <- name
			if name == "slow" {
				<-releaseSlow
				<-fastFinished
			} else {
				close(fastFinished)
			}
			return name, nil
		}, func(_ parallelInput) Access { return BlocksNone() })
	}
	transportCalls := 0
	var output lockedBuffer
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Tools: []Tool{tool("slow"), tool("fast")}, Log: NewLog(&output, func() time.Time { return time.Time{} }, ""),
	})
	stream := conversation.Send(context.Background(), Text{Text: "go"})
	eventStream := make(chan Event, 16)
	var events []Event
	go func() {
		for event := range stream.Events() {
			eventStream <- event
		}
		close(eventStream)
	}()
	seen := map[string]bool{}
	for range 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("non-conflicting calls did not both start")
		}
	}
	if !seen["slow"] || !seen["fast"] {
		t.Fatalf("started = %v", seen)
	}
	startMu.Lock()
	gotStartOrder := append([]string(nil), startOrder...)
	startMu.Unlock()
	if !reflect.DeepEqual(gotStartOrder, []string{"slow", "fast"}) {
		t.Fatalf("Call start order = %v, want model order", gotStartOrder)
	}
	for {
		select {
		case event := <-eventStream:
			events = append(events, event)
			if returned, ok := event.(ToolReturn); ok && returned.Result.ToolUseID == "fast" {
				goto fastReturned
			}
		case <-time.After(time.Second):
			t.Fatal("fast ToolReturn was withheld while slow call was running")
		}
	}

fastReturned:
	preReleaseRecords := decodeLogRecords(t, output.Bytes())
	var sawFastLog, sawToolMessage bool
	for _, record := range preReleaseRecords {
		if record.Type == RecordToolResult && record.ToolResult.ToolUseID == "fast" {
			sawFastLog = true
		}
		if record.Type == RecordMessage && record.Message.Role == RoleTool {
			sawToolMessage = true
		}
	}
	if !sawFastLog || sawToolMessage || transportCalls != 1 {
		t.Fatalf("while slow runs: fast log=%t RoleTool log=%t provider calls=%d", sawFastLog, sawToolMessage, transportCalls)
	}
	close(releaseSlow)
	for event := range eventStream {
		events = append(events, event)
	}
	var returns []string
	for _, event := range events {
		if returned, ok := event.(ToolReturn); ok {
			returns = append(returns, returned.Result.ToolUseID)
		}
	}
	if !reflect.DeepEqual(returns, []string{"fast", "slow"}) {
		t.Fatalf("ToolReturn order = %v, want completion order", returns)
	}
	toolMessage := provider.states[1].History[len(provider.states[1].History)-1]
	if toolMessage.Role != RoleTool || toolMessage.Blocks[0].(ToolResult).ToolUseID != "slow" || toolMessage.Blocks[1].(ToolResult).ToolUseID != "fast" {
		t.Fatalf("RoleTool message = %#v, want model call order", toolMessage)
	}
	var loggedReturns []string
	for _, record := range decodeLogRecords(t, output.Bytes()) {
		if record.Type == RecordToolResult {
			loggedReturns = append(loggedReturns, record.ToolResult.ToolUseID)
		}
	}
	if !reflect.DeepEqual(loggedReturns, returns) {
		t.Fatalf("tool_result log order = %v, want %v", loggedReturns, returns)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}

// R-D3MY-4ZYG
func TestConflictingDispatchesNeverOverlap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var running atomic.Int32
		var overlap atomic.Bool
		firstStarted := make(chan struct{})
		secondStarted := make(chan struct{})
		releaseFirst := make(chan struct{})
		tools := make([]Tool, 2)
		for index, name := range []string{"first", "second"} {
			name := name
			tools[index] = MustTool(name, "", func(context.Context, parallelInput) (string, error) {
				if running.Add(1) != 1 {
					overlap.Store(true)
				}
				if name == "first" {
					close(firstStarted)
					<-releaseFirst
				} else {
					close(secondStarted)
				}
				running.Add(-1)
				return name, nil
			}, func(_ parallelInput) Access { return BlocksPaths("tree") })
		}
		calls := []ToolUse{{ID: "1", Name: "first", Input: json.RawMessage(`{"name":"x"}`)}, {ID: "2", Name: "second", Input: json.RawMessage(`{"name":"y"}`)}}
		provider := &phase15Provider{responses: [][]Event{{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{calls[0], calls[1]}}}}, {MessageDone{Message: Message{Role: RoleAssistant}}}}}
		transportCalls := 0
		conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Tools: tools})
		stream := conversation.Send(context.Background(), Text{Text: "go"})
		done := make(chan struct{})
		go func() { drainStream(stream); close(done) }()
		<-firstStarted
		synctest.Wait()
		select {
		case <-secondStarted:
			t.Fatal("conflicting second call started before first returned")
		default:
		}
		close(releaseFirst)
		<-done
		if stream.Err() != nil || overlap.Load() {
			t.Fatalf("err=%v overlap=%t", stream.Err(), overlap.Load())
		}
	})
}

// R-D62Q-WJFU
func TestCancelledSendCancelsAndWaitsForEveryTool(t *testing.T) {
	var exited atomic.Int32
	started := make(chan struct{}, 2)
	cancelled := make(chan struct{}, 2)
	releaseExit := make(chan struct{})
	tool := func(name string) Tool {
		return MustTool(name, "", func(ctx context.Context, _ parallelInput) (string, error) {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			<-releaseExit
			exited.Add(1)
			return "", ctx.Err()
		}, func(_ parallelInput) Access { return BlocksNone() })
	}
	calls := []ToolUse{{ID: "1", Name: "a", Input: json.RawMessage(`{"name":"a"}`)}, {ID: "2", Name: "b", Input: json.RawMessage(`{"name":"b"}`)}}
	provider := &phase15Provider{responses: [][]Event{{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{calls[0], calls[1]}}}}}}
	var transportCalls atomic.Int32
	client := successfulPhase15Client(new(int))
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	var output lockedBuffer
	conversation := newConversation(provider, client, Config{Tools: []Tool{tool("a"), tool("b")}, Log: NewLog(&output, func() time.Time { return time.Time{} }, "")})
	ctx, cancel := context.WithCancel(context.Background())
	stream := conversation.Send(ctx, Text{Text: "go"})
	done := make(chan struct{})
	go func() { drainStream(stream); close(done) }()
	<-started
	<-started
	cancel()
	<-cancelled
	<-cancelled
	select {
	case <-done:
		t.Fatal("Send ended while cancelled tools were still running")
	default:
	}
	for _, record := range decodeLogRecords(t, output.Bytes()) {
		if record.Type == RecordMessage && record.Message.Role == RoleTool {
			t.Fatal("RoleTool message logged while cancelled tools were still running")
		}
	}
	if transportCalls.Load() != 1 {
		t.Fatalf("provider calls=%d while cancelled tools still run, want 1", transportCalls.Load())
	}
	close(releaseExit)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled Send did not finish")
	}
	if exited.Load() != 2 {
		t.Fatalf("exited=%d err=%v", exited.Load(), stream.Err())
	}
}

// R-D7AN-AB6J
func TestDeferredToolMutationsArePreparedAsBlocksAll(t *testing.T) {
	deferred := MustTool("later", "", func(context.Context, parallelInput) (string, error) { return "", nil }, func(_ parallelInput) Access {
		t.Fatal("unloaded deferred access function must not be consulted")
		return BlocksNone()
	})
	loaded := []string{}
	orchestrator := newOrchestrator(nil, []DeferredGroup{{Name: "group", Tools: []Tool{deferred}}}, &loaded)
	loader := orchestrator.prepareDispatch(ToolUse{ID: "1", Name: loadToolsName, Input: json.RawMessage(`{"names":["group"]}`)}, false)
	direct := orchestrator.prepareDispatch(ToolUse{ID: "2", Name: "later", Input: json.RawMessage(`{"name":"x"}`)}, false)
	if !accessesConflict(loader.access, BlocksNone()) || !accessesConflict(direct.access, BlocksNone()) {
		t.Fatalf("loader/direct access = %#v/%#v, want BlocksAll", loader.access, direct.access)
	}
	if result := orchestrator.dispatch(context.Background(), ToolUse{ID: "3", Name: "later", Input: json.RawMessage(`{"name":"x"}`)}, true); !result.IsError {
		t.Fatalf("savepoint-suspended direct deferred call = %#v, want in-band error", result)
	}
}
