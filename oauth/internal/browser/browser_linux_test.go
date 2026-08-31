//go:build linux

package browser

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeCommand struct {
	startCalls int
	startErr   error
	childDone  <-chan struct{}
}

func (c *fakeCommand) Start() error {
	c.startCalls++
	return c.startErr
}

type commandCall struct {
	name string
	args []string
}

func installCommandFactory(t *testing.T, cmd *fakeCommand) *[]commandCall {
	t.Helper()

	original := newCommand
	t.Cleanup(func() { newCommand = original })

	var calls []commandCall
	newCommand = func(name string, args ...string) command {
		calls = append(calls, commandCall{name: name, args: append([]string(nil), args...)})
		return cmd
	}
	return &calls
}

// R-GTP0-55S7
func TestNewOpensURLWithXDGOpen(t *testing.T) {
	cmd := &fakeCommand{}
	calls := installCommandFactory(t, cmd)

	err := New().Open("https://authorize.example/linux?case=distinct")
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	if want := []commandCall{{name: "xdg-open", args: []string{"https://authorize.example/linux?case=distinct"}}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("command calls = %#v, want %#v", *calls, want)
	}
	if cmd.startCalls != 1 {
		t.Fatalf("Start() calls = %d, want 1", cmd.startCalls)
	}
}

// R-GYKL-O8QZ
func TestOpenReturnsStartError(t *testing.T) {
	startErr := errors.New("unique start failure")
	cmd := &fakeCommand{startErr: startErr}
	calls := installCommandFactory(t, cmd)

	err := New().Open("https://authorize.example/start-error")
	if !errors.Is(err, startErr) {
		t.Fatalf("Open() error = %v, want start error %v", err, startErr)
	}
	if want := []commandCall{{name: "xdg-open", args: []string{"https://authorize.example/start-error"}}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("command calls = %#v, want %#v", *calls, want)
	}
	if cmd.startCalls != 1 {
		t.Fatalf("Start() calls = %d, want 1", cmd.startCalls)
	}
}

// R-GZSI-20HO
func TestOpenReturnsWithoutWaitingForChild(t *testing.T) {
	childDone := make(chan struct{})
	cmd := &fakeCommand{childDone: childDone}
	installCommandFactory(t, cmd)

	returned := make(chan error, 1)
	go func() {
		returned <- New().Open("https://authorize.example/nonblocking")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Open() error = %v, want nil", err)
		}
	case <-ctx.Done():
		t.Fatal("Open() waited for child completion")
	}
	if cmd.startCalls != 1 {
		t.Fatalf("Start() calls = %d, want 1", cmd.startCalls)
	}
	select {
	case <-cmd.childDone:
		t.Fatal("modeled child unexpectedly completed")
	default:
	}
}
