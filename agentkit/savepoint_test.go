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

func TestReleaseKeepsHistoryAndLimitsAndEndsSavepoint(t *testing.T) {
	// R-7WL4-3T3X
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
	if err := conversation.AddSystem("instructions"); err != nil {
		t.Fatalf("AddSystem() error = %v", err)
	}
	conversation.toolCallsDispatched = 3
	conversation.lastRoundContext = 5

	sp, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("Savepoint() error = %v, want nil", err)
	}
	wantHistory := cloneHistory(conversation.history)
	wantToolCalls := conversation.toolCallsDispatched
	wantContextTokens := conversation.lastRoundContext

	if err := conversation.Release(sp); err != nil {
		t.Fatalf("Release() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(conversation.history, wantHistory) {
		t.Fatalf("History after Release() = %#v, want unchanged %#v", conversation.history, wantHistory)
	}
	if conversation.toolCallsDispatched != wantToolCalls {
		t.Fatalf("toolCallsDispatched after Release() = %d, want unchanged %d", conversation.toolCallsDispatched, wantToolCalls)
	}
	if conversation.lastRoundContext != wantContextTokens {
		t.Fatalf("lastRoundContext after Release() = %d, want unchanged %d", conversation.lastRoundContext, wantContextTokens)
	}
	if err := conversation.Restore(sp); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Restore() after Release() error = %v, want ErrInvalidArgument", err)
	}
	if err := conversation.Release(sp); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("second Release() error = %v, want ErrInvalidArgument", err)
	}
}

func TestRestoreRewindsHistoryAndKeepsSavepointLive(t *testing.T) {
	// R-7RPI-KQ55
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
	if err := conversation.AddSystem("at savepoint"); err != nil {
		t.Fatalf("AddSystem() error = %v", err)
	}
	sp, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("Savepoint() error = %v, want nil", err)
	}
	want := cloneHistory(conversation.history)
	if err := conversation.AddSystem("after savepoint"); err != nil {
		t.Fatalf("AddSystem() after Savepoint error = %v", err)
	}

	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("first Restore() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(conversation.history, want) {
		t.Fatalf("History after first Restore() = %#v, want %#v", conversation.history, want)
	}

	conversation.history[0].Blocks[0] = Text{Text: "caller mutation"}
	conversation.history = append(conversation.history, Message{
		Role:   RoleSystem,
		Blocks: []Block{Text{Text: "caller append"}},
	})
	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("second Restore() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(conversation.history, want) {
		t.Fatalf("History after second Restore() = %#v, want %#v", conversation.history, want)
	}
}

func TestRestoreZeroesContextCheckpoint(t *testing.T) {
	// R-7SXE-YHVU
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
	conversation.lastRoundContext = 5
	sp, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("Savepoint() error = %v, want nil", err)
	}
	conversation.lastRoundContext = 8

	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("Restore() error = %v, want nil", err)
	}
	if conversation.lastRoundContext != 0 {
		t.Fatalf("lastRoundContext after Restore() = %d, want 0", conversation.lastRoundContext)
	}
}

func TestRestoreRewindsToolCallCheckpoint(t *testing.T) {
	// R-7VD7-Q1D8
	conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
	conversation.toolCallsDispatched = 2
	sp, err := conversation.Savepoint()
	if err != nil {
		t.Fatalf("Savepoint() error = %v, want nil", err)
	}
	conversation.toolCallsDispatched = 5

	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("first Restore() error = %v, want nil", err)
	}
	if conversation.toolCallsDispatched != 2 {
		t.Fatalf("toolCallsDispatched after first Restore() = %d, want 2", conversation.toolCallsDispatched)
	}

	conversation.toolCallsDispatched = 7
	if err := conversation.Restore(sp); err != nil {
		t.Fatalf("second Restore() error = %v, want nil", err)
	}
	if conversation.toolCallsDispatched != 2 {
		t.Fatalf("toolCallsDispatched after second Restore() = %d, want 2", conversation.toolCallsDispatched)
	}
}

func TestSavepointAndRestoreBeforeAnySend(t *testing.T) {
	// R-6OEI-KH07
	t.Run("empty history", func(t *testing.T) {
		conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("Savepoint() error = %v, want nil", err)
		}
		if err := conversation.AddSystem("after savepoint"); err != nil {
			t.Fatalf("AddSystem() error = %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("Restore() error = %v, want nil", err)
		}
		if len(conversation.history) != 0 {
			t.Fatalf("History length after Restore() = %d, want 0", len(conversation.history))
		}
	})

	t.Run("system messages only", func(t *testing.T) {
		conversation := newConversation(&phase15Provider{model: "model"}, successfulPhase15Client(new(int)), Config{})
		for _, text := range []string{"first", "second"} {
			if err := conversation.AddSystem(text); err != nil {
				t.Fatalf("AddSystem(%q) error = %v", text, err)
			}
		}
		want := cloneHistory(conversation.history)
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("Savepoint() error = %v, want nil", err)
		}
		if err := conversation.AddSystem("after savepoint"); err != nil {
			t.Fatalf("AddSystem() after Savepoint error = %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("Restore() error = %v, want nil", err)
		}
		if !reflect.DeepEqual(conversation.history, want) {
			t.Fatalf("History after Restore() = %#v, want %#v", conversation.history, want)
		}
		for index, message := range conversation.history {
			if message.Role != RoleSystem {
				t.Errorf("History[%d].Role = %v, want RoleSystem", index, message.Role)
			}
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
