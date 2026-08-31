//go:build darwin

package browser

import (
	"reflect"
	"testing"
)

type fakeCommand struct {
	startCalls int
}

func (c *fakeCommand) Start() error {
	c.startCalls++
	return nil
}

type commandCall struct {
	name string
	args []string
}

// R-GUWW-IXIW
func TestNewOpensURLWithOpen(t *testing.T) {
	original := newCommand
	t.Cleanup(func() { newCommand = original })

	cmd := &fakeCommand{}
	var calls []commandCall
	newCommand = func(name string, args ...string) command {
		calls = append(calls, commandCall{name: name, args: append([]string(nil), args...)})
		return cmd
	}

	err := New().Open("https://authorize.example/darwin?case=distinct")
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	if want := []commandCall{{name: "open", args: []string{"https://authorize.example/darwin?case=distinct"}}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("command calls = %#v, want %#v", calls, want)
	}
	if cmd.startCalls != 1 {
		t.Fatalf("Start() calls = %d, want 1", cmd.startCalls)
	}
}
