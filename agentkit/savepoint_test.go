package agentkit

import (
	"context"
	"errors"
	"testing"
)

func TestConversationCloseMarksConversationClosed(t *testing.T) {
	// R-7KE4-A3OZ
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
	if err := conversation.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if err := conversation.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if err := conversation.AddSystem("after close"); !errors.Is(err, ErrClosed) {
		t.Fatalf("AddSystem after Close error = %v, want ErrClosed", err)
	}
	stream := conversation.Send(context.Background(), Text{Text: "after close"})
	drainStream(stream)
	if !errors.Is(stream.Err(), ErrClosed) {
		t.Fatalf("Send after Close error = %v, want ErrClosed", stream.Err())
	}
}
