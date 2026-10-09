package mcp_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
)

func registeredToolsFixture(t *testing.T) (*mcp.Server, []mcp.RegisteredTool) {
	t.Helper()
	s := toolsServer(t)
	typed := toolsTyped("read_zebra")
	typed.Description = "Read zebra.\nDetails with café, \"quotes\", a \\ and\tspacing."
	raw := toolsRaw("read_alpha")
	raw.Description = "Read alpha.\nA second line.\n"
	mcp.AddTool(s, typed)
	mcp.AddRawTool(s, raw)
	return s, []mcp.RegisteredTool{
		{Name: typed.Name, Description: typed.Description},
		{Name: raw.Name, Description: raw.Description},
	}
}

func registeredToolsWire(s *mcp.Server) []mcp.RegisteredTool {
	list := toolsList(s)
	tools := make([]mcp.RegisteredTool, len(list))
	for i, entry := range list {
		var metadata struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(entry, &metadata); err != nil {
			panic(err)
		}
		tools[i] = mcp.RegisteredTool{Name: metadata.Name, Description: metadata.Description}
	}
	return tools
}

func TestRegisteredToolPublicShape(t *testing.T) {
	// R-2488-KM5K: unkeyed construction proves exactly two string fields in order.
	name, description := "read_shape", "Read shape."
	tool := mcp.RegisteredTool{name, description}
	if tool.Name != name || tool.Description != description {
		t.Fatalf("metadata = %#v", tool)
	}
	// R-25G4-YDW9: passing the method expression checks its declared signature.
	list := func(method func(*mcp.Server) []mcp.RegisteredTool) []mcp.RegisteredTool {
		return method(toolsServer(t))
	}
	if got := list((*mcp.Server).Tools); len(got) != 0 {
		t.Fatalf("empty server tools = %#v", got)
	}
}

func TestRegisteredToolsOrderAndValues(t *testing.T) {
	// R-26O1-C5MY
	empty := toolsServer(t)
	if got := empty.Tools(); len(got) != 0 {
		t.Fatalf("empty server tools = %#v", got)
	}
	s, want := registeredToolsFixture(t)
	if got := s.Tools(); !slices.Equal(got, want) {
		t.Fatalf("tools = %#v, want %#v", got, want)
	}
}

func TestRegisteredToolsMatchWireList(t *testing.T) {
	// R-27VX-PXDN
	for _, populated := range []bool{false, true} {
		s := toolsServer(t)
		if populated {
			s, _ = registeredToolsFixture(t)
		}
		before := s.Tools()
		wire := registeredToolsWire(s)
		after := s.Tools()
		if !slices.Equal(before, wire) || !slices.Equal(after, wire) {
			t.Fatalf("before = %#v, wire = %#v, after = %#v", before, wire, after)
		}
	}
}

func TestRegisteredToolsCopies(t *testing.T) {
	// R-293U-3P4C
	s, want := registeredToolsFixture(t)
	returned := s.Tools()
	returned[0], returned[1] = returned[1], returned[0]
	returned[0].Name = "changed_name"
	returned[1].Description = "Changed description."
	if got := s.Tools(); !slices.Equal(got, want) {
		t.Fatalf("edited slice changed tools: %#v", got)
	}
	if got := registeredToolsWire(s); !slices.Equal(got, want) {
		t.Fatalf("edited slice changed wire list: %#v", got)
	}
}

func TestRegisteredToolsLeaveRegistrationOpen(t *testing.T) {
	// R-2ABQ-HGV1
	s := toolsServer(t)
	_ = s.Tools()
	typed := toolsTyped("read_typed")
	mcp.AddTool(s, typed)
	_ = s.Tools()
	raw := toolsRaw("read_raw")
	mcp.AddRawTool(s, raw)
	want := []mcp.RegisteredTool{{Name: typed.Name, Description: typed.Description}, {Name: raw.Name, Description: raw.Description}}
	if got := s.Tools(); !slices.Equal(got, want) {
		t.Fatalf("tools after registration = %#v, want %#v", got, want)
	}
}

func TestRegisteredToolsConcurrent(t *testing.T) {
	// R-2BJM-V8LQ
	s := toolsServer(t)
	const registrations = 32
	run := func(workers ...func()) {
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, worker := range workers {
			wg.Go(func() { <-start; worker() })
		}
		close(start)
		wg.Wait()
	}
	read := func() {
		for range 128 {
			list := s.Tools()
			if len(list) > 0 {
				list[0] = mcp.RegisteredTool{Name: "edited", Description: "Edited copy."}
			}
		}
	}
	// Readers race with both registration forms and with each other.
	run(func() {
		for i := range registrations {
			mcp.AddTool(s, toolsTyped(fmt.Sprintf("read_typed_%d", i)))
		}
	}, func() {
		for i := range registrations {
			mcp.AddRawTool(s, toolsRaw(fmt.Sprintf("read_raw_%d", i)))
		}
	}, read, read, read, read)
	if got := len(s.Tools()); got != 2*registrations {
		t.Fatalf("registered %d tools, want %d", got, 2*registrations)
	}
	want := s.Tools()
	serve := func() {
		for range 32 {
			if got := registeredToolsWire(s); !slices.Equal(got, want) {
				t.Errorf("concurrent wire list = %#v, want %#v", got, want)
			}
		}
	}
	// Serving has closed registration; readers race with ServeHTTP and each other.
	run(read, read, read, read, serve, serve)
	if got := s.Tools(); !slices.Equal(got, want) {
		t.Fatalf("concurrent copies changed tools: %#v", got)
	}
}
