package agentkit

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSavepointRecordsHistoryWithoutProviderCall(t *testing.T) {
	// R-7MTX-1N6D
	transportCalls := 0
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(&transportCalls), Config{})
	if err := conversation.AddSystem("instructions"); err != nil {
		t.Fatalf("AddSystem() error = %v", err)
	}
	before := cloneHistory(conversation.history)

	if _, err := conversation.Savepoint(); err != nil {
		t.Fatalf("Savepoint() error = %v, want nil", err)
	}
	if transportCalls != 0 {
		t.Fatalf("Savepoint() transport calls = %d, want 0", transportCalls)
	}
	if !reflect.DeepEqual(conversation.history, before) {
		t.Fatalf("History after Savepoint() = %#v, want unchanged %#v", conversation.history, before)
	}
}

func TestConversationAllowsOnlyOneLiveSavepoint(t *testing.T) {
	// R-7O1T-FEX2
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
	if err := conversation.AddSystem("instructions"); err != nil {
		t.Fatalf("AddSystem() error = %v", err)
	}
	first, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("first Savepoint() error = %v, want nil", err)
	}
	before := cloneHistory(conversation.history)

	second, err := conversation.Savepoint()
	if !reflect.DeepEqual(second, Savepoint{}) {
		t.Fatalf("second Savepoint() = %#v, want zero value", second)
	}
	if !errors.Is(err, ErrSavepointActive) {
		t.Fatalf("second Savepoint() error = %v, want ErrSavepointActive", err)
	}
	if !reflect.DeepEqual(conversation.history, before) {
		t.Fatalf("History after second Savepoint() = %#v, want unchanged %#v", conversation.history, before)
	}
	if err := conversation.Release(first); err != nil {
		t.Fatalf("Release(first) error = %v, want nil", err)
	}
}

func TestRestoreAndReleaseRejectNonLiveSavepoint(t *testing.T) {
	// R-7P9P-T6NR
	newTestConversation := func() *Conversation {
		conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
		if err := conversation.AddSystem("instructions"); err != nil {
			t.Fatalf("AddSystem() error = %v", err)
		}
		conversation.toolCallsDispatched = 3
		conversation.lastRoundContext = 5
		return conversation
	}
	assertUnchanged := func(t *testing.T, conversation *Conversation, history History, toolCalls int, contextTokens int64) {
		t.Helper()
		if !reflect.DeepEqual(conversation.history, history) ||
			conversation.toolCallsDispatched != toolCalls || conversation.lastRoundContext != contextTokens {
			t.Fatalf("state changed: History=%#v toolCalls=%d context=%d; want %#v, %d, %d",
				conversation.history, conversation.toolCallsDispatched, conversation.lastRoundContext,
				history, toolCalls, contextTokens)
		}
	}
	assertRejected := func(t *testing.T, conversation *Conversation, sp Savepoint) {
		t.Helper()
		before := cloneHistory(conversation.history)
		toolCalls := conversation.toolCallsDispatched
		contextTokens := conversation.lastRoundContext
		if err := conversation.Restore(sp); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("Restore() error = %v, want ErrInvalidArgument", err)
		}
		assertUnchanged(t, conversation, before, toolCalls, contextTokens)
		if err := conversation.Release(sp); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("Release() error = %v, want ErrInvalidArgument", err)
		}
		assertUnchanged(t, conversation, before, toolCalls, contextTokens)
	}

	t.Run("zero value", func(t *testing.T) {
		assertRejected(t, newTestConversation(), Savepoint{})
	})

	t.Run("already released", func(t *testing.T) {
		conversation := newTestConversation()
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("Savepoint() error = %v, want nil", err)
		}
		if err := conversation.Release(sp); err != nil {
			t.Fatalf("initial Release() error = %v, want nil", err)
		}
		assertRejected(t, conversation, sp)
	})

	t.Run("another conversation", func(t *testing.T) {
		conversation := newTestConversation()
		other := newTestConversation()
		local, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("local Savepoint() error = %v, want nil", err)
		}
		sp, err := other.Savepoint()
		if err != nil {
			t.Fatalf("other Savepoint() error = %v, want nil", err)
		}
		assertRejected(t, conversation, sp)
		if err := conversation.Release(local); err != nil {
			t.Fatalf("Release(local) after rejected foreign handle = %v, want nil", err)
		}
	})
}

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
